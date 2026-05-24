package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// toolListBuilds implements jenkinsListBuilds tool
func (p *JenkinsPlugin) toolListBuilds(args map[string]any, client JenkinsClient) (any, error) {
	parser := NewRequestParser(args)
	req := &ListBuildsRequest{
		Instance:       parser.Instance(&p.cfg),
		Job:            parser.String("job"),
		Limit:          parser.Int("limit"),
		IncludeRunning: parser.Bool("include_running", true),
		IncludeQueued:  parser.Bool("include_queued", true),
		RunningLimit:   parser.Int("running_limit"),
		QueueLimit:     parser.Int("queue_limit"),
	}

	// Apply defaults
	if req.Limit <= 0 {
		req.Limit = p.cfg.Defaults.BuildLimit
	}
	if req.RunningLimit <= 0 {
		req.RunningLimit = p.cfg.Defaults.RunningLimit
	}
	if req.QueueLimit <= 0 {
		req.QueueLimit = p.cfg.Defaults.QueueLimit
	}

	// Job mode
	if req.Job != "" {
		return p.toolListBuildsJobMode(req, client)
	}

	// Global mode
	return p.toolListBuildsGlobalMode(req, client)
}

// toolListBuildsJobMode handles job-specific build listing
func (p *JenkinsPlugin) toolListBuildsJobMode(req *ListBuildsRequest, client JenkinsClient) (*ListBuildsJobModeResponse, error) {
	data, err := client.ListBuilds(context.Background(), req.Job, req.Limit)
	if err != nil {
		return nil, err
	}

	var wrap struct {
		Builds []json.RawMessage `json:"builds"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return nil, err
	}

	return &ListBuildsJobModeResponse{
		Builds: wrap.Builds,
		Limit:  req.Limit,
	}, nil
}

// toolListBuildsGlobalMode handles global running/queued listing
func (p *JenkinsPlugin) toolListBuildsGlobalMode(req *ListBuildsRequest, client JenkinsClient) (*ListBuildsGlobalModeResponse, error) {
	resp := &ListBuildsGlobalModeResponse{
		Running:      make([]RunningBuild, 0),
		Queued:       make([]QueuedBuild, 0),
		RunningLimit: req.RunningLimit,
		QueueLimit:   req.QueueLimit,
		Errors:       make(map[string]string),
	}

	// Get running builds from /computer
	if req.IncludeRunning && req.RunningLimit > 0 {
		data, err := client.GetComputers(context.Background())
		if err != nil {
			resp.Errors["computer"] = err.Error()
		} else {
			running, err := parseRunningBuilds(data, req.RunningLimit)
			if err != nil {
				resp.Errors["computer"] = err.Error()
			} else {
				resp.Running = running
			}
		}
	}

	// Get queued builds from /queue
	if req.IncludeQueued && req.QueueLimit > 0 {
		data, err := client.GetQueue(context.Background())
		if err != nil {
			resp.Errors["queue"] = err.Error()
		} else {
			queued, err := parseQueuedBuilds(data, req.QueueLimit)
			if err != nil {
				resp.Errors["queue"] = err.Error()
			} else {
				resp.Queued = queued
			}
		}
	}

	if len(resp.Errors) == 0 {
		resp.Errors = nil
	}

	return resp, nil
}

// parseRunningBuilds parses the /computer API response
func parseRunningBuilds(data []byte, limit int) ([]RunningBuild, error) {
	type execInfo struct {
		CurrentExecutable *struct {
			URL             string `json:"url"`
			Number          int    `json:"number"`
			FullDisplayName string `json:"fullDisplayName"`
		} `json:"currentExecutable"`
	}

	var wrap struct {
		Computers []struct {
			DisplayName string     `json:"displayName"`
			Executors   []execInfo `json:"executors"`
		} `json:"computer"`
	}

	if err := json.Unmarshal(data, &wrap); err != nil {
		return nil, err
	}

	var running []RunningBuild
	count := 0
	for _, comp := range wrap.Computers {
		for _, ex := range comp.Executors {
			if ex.CurrentExecutable == nil {
				continue
			}

			u := ex.CurrentExecutable.URL
			fdn := ex.CurrentExecutable.FullDisplayName
			jobName, buildNumber, parsed := resolveRunningJobBuild(u, fdn, ex.CurrentExecutable.Number)

			entry := RunningBuild{
				URL:             u,
				FullDisplayName: fdn,
				Node:            comp.DisplayName,
			}

			if parsed {
				entry.Job = jobName
				entry.BuildNumber = buildNumber
			} else {
				entry.Unparsed = true
				entry.Job = jobName
				bn := buildNumber
				if bn <= 0 {
					bn = ex.CurrentExecutable.Number
				}
				entry.BuildNumber = bn
			}

			running = append(running, entry)
			count++
			if count >= limit {
				break
			}
		}
		if count >= limit {
			break
		}
	}

	return running, nil
}

// parseQueuedBuilds parses the /queue API response
func parseQueuedBuilds(data []byte, limit int) ([]QueuedBuild, error) {
	type taskInfo struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}

	var wrap struct {
		Items []struct {
			ID           int       `json:"id"`
			Why          string    `json:"why"`
			Stuck        bool      `json:"stuck"`
			InQueueSince any       `json:"inQueueSince"`
			Task         *taskInfo `json:"task"`
		} `json:"items"`
	}

	if err := json.Unmarshal(data, &wrap); err != nil {
		return nil, err
	}

	var queued []QueuedBuild
	for _, it := range wrap.Items {
		job := ""
		if it.Task != nil {
			job2, ok := parseJobFullNameFromJobURL(it.Task.URL)
			if ok {
				job = job2
			} else {
				job = strings.TrimSpace(it.Task.Name)
			}
		}
		if job == "" {
			continue
		}

		inQueueSince, _ := coerceInt64(it.InQueueSince)

		taskName := ""
		taskURL := ""
		if it.Task != nil {
			taskName = strings.TrimSpace(it.Task.Name)
			taskURL = it.Task.URL
		}

		queued = append(queued, QueuedBuild{
			Job:          job,
			QueueID:      it.ID,
			Why:          it.Why,
			Stuck:        it.Stuck,
			InQueueSince: inQueueSince,
			TaskName:     taskName,
			TaskURL:      taskURL,
		})

		if len(queued) >= limit {
			break
		}
	}

	return queued, nil
}

// resolveRunningJobBuild derives job full name and build number from executor metadata
func resolveRunningJobBuild(u, fullDisplayName string, numberField int) (jobName string, buildNumber int, parsed bool) {
	if j, n, ok := parseBuildRefFromBuildURL(u); ok {
		return j, n, strings.TrimSpace(j) != "" && n > 0
	}
	if j, ok := parseJobFullNameFromJobURL(u); ok && numberField > 0 {
		return j, numberField, strings.TrimSpace(j) != ""
	}
	if j, n, ok := parseBuildFromFullDisplayName(fullDisplayName); ok {
		return j, n, strings.TrimSpace(j) != "" && n > 0
	}
	return "", 0, false
}

// parseBuildFromFullDisplayName parses run titles like "folder » job #42"
func parseBuildFromFullDisplayName(s string) (job string, buildNumber int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, false
	}
	idx := strings.LastIndex(s, " #")
	if idx < 0 {
		return "", 0, false
	}
	numPart := strings.TrimSpace(s[idx+len(" #"):])
	n, err := strconv.Atoi(numPart)
	if err != nil {
		return "", 0, false
	}
	job = strings.TrimSpace(s[:idx])
	job = strings.ReplaceAll(job, " » ", "/")
	job = strings.ReplaceAll(job, "\u00bb", "/")
	job = strings.ReplaceAll(job, "»", "/")
	job = strings.Trim(job, "/")
	return job, n, job != ""
}

// parseJobFullNameFromJobURL converts Jenkins "job URL" path into full job name
func parseJobFullNameFromJobURL(jobURL string) (string, bool) {
	p := strings.Trim(pathFromPossiblyURL(jobURL), "/")
	if p == "" {
		return "", false
	}
	parts := strings.Split(p, "/")

	names := make([]string, 0, 4)
	for i := 0; i+1 < len(parts); {
		if parts[i] != "job" {
			break
		}
		name, err := url.PathUnescape(parts[i+1])
		if err != nil {
			name = parts[i+1]
		}
		if strings.TrimSpace(name) == "" {
			return "", false
		}
		names = append(names, name)
		i += 2
	}
	if len(names) == 0 {
		return "", false
	}
	return strings.Join(names, "/"), true
}

// parseBuildRefFromBuildURL converts Jenkins build URL path into full job name and build number
func parseBuildRefFromBuildURL(buildURL string) (job string, buildNumber int, ok bool) {
	p := strings.Trim(pathFromPossiblyURL(buildURL), "/")
	if p == "" {
		return "", 0, false
	}
	parts := strings.Split(p, "/")

	names := make([]string, 0, 4)
	for i := 0; i+1 < len(parts); {
		if parts[i] != "job" {
			break
		}
		if i+2 >= len(parts) {
			return "", 0, false
		}
		name, err := url.PathUnescape(parts[i+1])
		if err != nil {
			name = parts[i+1]
		}
		if strings.TrimSpace(name) == "" {
			return "", 0, false
		}
		names = append(names, name)
		i += 2
	}
	if len(names) == 0 {
		return "", 0, false
	}

	idx := len(names) * 2
	if idx >= len(parts) {
		return "", 0, false
	}
	n, err := strconv.Atoi(parts[idx])
	if err != nil {
		return "", 0, false
	}
	return strings.Join(names, "/"), n, true
}

func pathFromPossiblyURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err == nil && u.Path != "" {
		return u.Path
	}
	if strings.HasPrefix(s, "/") {
		return s
	}
	return "/" + s
}

func coerceInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	case json.Number:
		i, err := x.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

// toolGetBuild implements jenkinsGetBuild tool
func (p *JenkinsPlugin) toolGetBuild(args map[string]any, client JenkinsClient) (*GetBuildResponse, error) {
	parser := NewRequestParser(args)
	req := &GetBuildRequest{
		Instance:    parser.String("instance"),
		Job:         parser.String("job"),
		BuildNumber: parser.Int("build_number"),
	}

	if req.Job == "" {
		return nil, fmt.Errorf("job is required")
	}
	if req.BuildNumber <= 0 {
		return nil, fmt.Errorf("build_number is required")
	}

	data, err := client.GetBuild(context.Background(), req.Job, req.BuildNumber)
	if err != nil {
		return nil, err
	}

	return &GetBuildResponse{Data: data}, nil
}
