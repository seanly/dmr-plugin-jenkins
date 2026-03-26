package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	defaultJobTree = "name,url,buildable,lastBuild[number,url,result,timestamp]"
	// defaultRootJobTree lists top-level jobs/folders from Jenkins root /api/json.
	defaultRootJobTree = "jobs[name,url,buildable,_class,lastBuild[number,url,result,timestamp]]"
)

func jenkinsGetJobIsRootListing(job string) bool {
	j := strings.TrimSpace(job)
	return j == "" || j == "."
}

func (p *JenkinsPlugin) resolveInstance(args map[string]any) (string, *jenkinsClient, error) {
	inst := ""
	if v, ok := args["instance"].(string); ok {
		inst = strings.TrimSpace(v)
	}
	if inst == "" {
		inst = strings.TrimSpace(p.cfg.DefaultInstance)
	}
	if inst == "" {
		return "", nil, fmt.Errorf("instance required (or set default_instance in plugin config)")
	}
	c := p.clients[inst]
	if c == nil {
		return inst, nil, fmt.Errorf("unknown instance %q", inst)
	}
	return inst, c, nil
}

func strArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func intArg(args map[string]any, key string) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return 0
}

func (p *JenkinsPlugin) toolInstances() (any, error) {
	out := make([]map[string]any, 0, len(p.cfg.Instances))
	for _, in := range p.cfg.Instances {
		host := in.BaseURL
		if u, err := url.Parse(strings.TrimSpace(in.BaseURL)); err == nil && u.Host != "" {
			host = u.Host
		}
		out = append(out, map[string]any{
			"id":   in.ID,
			"host": host,
		})
	}
	return map[string]any{"instances": out}, nil
}

func (p *JenkinsPlugin) toolGetJob(ctx context.Context, c *jenkinsClient, args map[string]any) (any, error) {
	job := strArg(args, "job")
	tree := strArg(args, "tree")
	var path string
	if jenkinsGetJobIsRootListing(job) {
		if tree == "" {
			tree = defaultRootJobTree
		}
		path = "/api/json?tree=" + url.QueryEscape(tree)
	} else {
		if tree == "" {
			tree = defaultJobTree
		}
		path = jobURLPath(job) + "/api/json?tree=" + url.QueryEscape(tree)
	}
	st, body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, httpError(st, body)
	}
	return json.RawMessage(body), nil
}

func (p *JenkinsPlugin) toolListBuilds(ctx context.Context, c *jenkinsClient, args map[string]any) (any, error) {
	job := strArg(args, "job")
	if job != "" {
		limit := intArg(args, "limit")
		if limit <= 0 {
			limit = 10
		}
		tree := fmt.Sprintf("builds[number,url,result,timestamp,building]{0,%d}", limit)
		path := jobURLPath(job) + "/api/json?tree=" + url.QueryEscape(tree)
		st, body, err := c.get(ctx, path)
		if err != nil {
			return nil, err
		}
		if st != 200 {
			return nil, httpError(st, body)
		}
		var wrap struct {
			Builds []json.RawMessage `json:"builds"`
		}
		if err := json.Unmarshal(body, &wrap); err != nil {
			return nil, err
		}
		return map[string]any{"builds": wrap.Builds, "limit": limit}, nil
	}

	// Global mode (no job): query Jenkins /computer (running) and /queue (queued).
	includeRunning := true
	if v, ok := args["include_running"].(bool); ok {
		includeRunning = v
	}
	includeQueued := true
	if v, ok := args["include_queued"].(bool); ok {
		includeQueued = v
	}

	runningLimit := intArg(args, "running_limit")
	if runningLimit <= 0 {
		runningLimit = intArg(args, "limit")
	}
	if runningLimit <= 0 {
		runningLimit = 10
	}

	queueLimit := intArg(args, "queue_limit")
	if queueLimit <= 0 {
		queueLimit = intArg(args, "limit")
	}
	if queueLimit <= 0 {
		queueLimit = 20
	}

	running := make([]map[string]any, 0)
	queued := make([]map[string]any, 0)
	apiErrs := make(map[string]string)

	if includeRunning && runningLimit > 0 {
		runningTree := "computer[displayName,executors[currentExecutable[url,number,fullDisplayName]]]"
		path := "/computer/api/json?tree=" + url.QueryEscape(runningTree)
		st, body, err := c.get(ctx, path)
		if err != nil {
			apiErrs["computer"] = err.Error()
		} else if st != 200 {
			apiErrs["computer"] = httpError(st, body).Error()
		} else {
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
			if err := json.Unmarshal(body, &wrap); err != nil {
				apiErrs["computer"] = err.Error()
			} else {
				running = make([]map[string]any, 0, runningLimit)
			outerRunning:
				for _, comp := range wrap.Computers {
					for _, ex := range comp.Executors {
						if ex.CurrentExecutable == nil {
							continue
						}
						u := ex.CurrentExecutable.URL
						fdn := ex.CurrentExecutable.FullDisplayName
						jobName, buildNumber, parsed := resolveRunningJobBuild(u, fdn, ex.CurrentExecutable.Number)

						entry := map[string]any{
							"url":               u,
							"full_display_name": fdn,
							"node":              comp.DisplayName,
						}
						if parsed {
							entry["job"] = jobName
							entry["build_number"] = buildNumber
						} else {
							entry["unparsed"] = true
							if strings.TrimSpace(jobName) != "" {
								entry["job"] = jobName
							} else {
								entry["job"] = ""
							}
							bn := buildNumber
							if bn <= 0 {
								bn = ex.CurrentExecutable.Number
							}
							entry["build_number"] = bn
						}

						running = append(running, entry)
						if len(running) >= runningLimit {
							break outerRunning
						}
					}
				}
			}
		}
	}

	if includeQueued && queueLimit > 0 {
		queueTree := "items[id,why,stuck,inQueueSince,task[name,url]]"
		path := "/queue/api/json?tree=" + url.QueryEscape(queueTree)
		st, body, err := c.get(ctx, path)
		if err != nil {
			apiErrs["queue"] = err.Error()
		} else if st != 200 {
			apiErrs["queue"] = httpError(st, body).Error()
		} else {
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
			if err := json.Unmarshal(body, &wrap); err != nil {
				apiErrs["queue"] = err.Error()
			} else {
				queued = make([]map[string]any, 0, queueLimit)
			outerQueued:
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

					queued = append(queued, map[string]any{
						"job":            job,
						"queue_id":       it.ID,
						"why":            it.Why,
						"stuck":          it.Stuck,
						"in_queue_since": inQueueSince,
						"task_name": func() string {
							if it.Task == nil {
								return ""
							}
							return strings.TrimSpace(it.Task.Name)
						}(),
						"task_url": func() string {
							if it.Task == nil {
								return ""
							}
							return it.Task.URL
						}(),
					})
					if len(queued) >= queueLimit {
						break outerQueued
					}
				}
			}
		}
	}

	out := map[string]any{
		"running":       running,
		"queued":        queued,
		"running_limit": runningLimit,
		"queue_limit":   queueLimit,
	}
	if len(apiErrs) > 0 {
		out["errors"] = apiErrs
	}
	return out, nil
}

func (p *JenkinsPlugin) toolGetBuild(ctx context.Context, c *jenkinsClient, args map[string]any) (any, error) {
	job := strArg(args, "job")
	if job == "" {
		return nil, fmt.Errorf("job required")
	}
	n := intArg(args, "build_number")
	if n <= 0 {
		return nil, fmt.Errorf("build_number required")
	}
	path := fmt.Sprintf("%s/%d/api/json", jobURLPath(job), n)
	st, body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, httpError(st, body)
	}
	return json.RawMessage(body), nil
}

func (p *JenkinsPlugin) toolTriggerBuild(ctx context.Context, c *jenkinsClient, args map[string]any) (any, error) {
	job := strArg(args, "job")
	if job == "" {
		return nil, fmt.Errorf("job required")
	}
	base := jobURLPath(job)
	var params map[string]any
	if raw, ok := args["parameters"].(map[string]any); ok && len(raw) > 0 {
		params = raw
	}

	if len(params) == 0 {
		st, body, err := c.post(ctx, base+"/build", "", nil)
		if err != nil {
			return nil, err
		}
		if st != httpStatusCreated && st != 200 && st != 204 {
			return nil, httpError(st, body)
		}
		return map[string]any{"status": st, "triggered": true}, nil
	}

	vals := url.Values{}
	for k, v := range params {
		switch x := v.(type) {
		case string:
			vals.Set(k, x)
		case float64:
			vals.Set(k, strconv.FormatInt(int64(x), 10))
		case bool:
			vals.Set(k, strconv.FormatBool(x))
		default:
			vals.Set(k, fmt.Sprintf("%v", v))
		}
	}
	st, body, err := c.post(ctx, base+"/buildWithParameters", "application/x-www-form-urlencoded", []byte(vals.Encode()))
	if err != nil {
		return nil, err
	}
	if st != httpStatusCreated && st != 200 && st != 204 {
		return nil, httpError(st, body)
	}
	return map[string]any{"status": st, "triggered": true, "with_parameters": true}, nil
}

const httpStatusCreated = 201

func (p *JenkinsPlugin) toolGetConsoleText(ctx context.Context, c *jenkinsClient, args map[string]any) (any, error) {
	job := strArg(args, "job")
	if job == "" {
		return nil, fmt.Errorf("job required")
	}
	n := intArg(args, "build_number")
	if n <= 0 {
		return nil, fmt.Errorf("build_number required")
	}
	maxChars := intArg(args, "max_chars")
	if maxChars <= 0 {
		maxChars = 65536
	}
	path := jobConsolePath(job, n)
	st, body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, httpError(st, body)
	}
	text := string(body)
	truncated := false
	if maxChars > 0 && utf8.RuneCountInString(text) > maxChars {
		runes := []rune(text)
		text = string(runes[:maxChars])
		truncated = true
	}
	return map[string]any{
		"text":         text,
		"truncated":    truncated,
		"max_chars":    maxChars,
		"job":          job,
		"build_number": n,
	}, nil
}

func (p *JenkinsPlugin) executeTool(name string, args map[string]any) (any, error) {
	ctx := context.Background()

	switch name {
	case "jenkinsInstances":
		return p.toolInstances()
	}

	_, c, err := p.resolveInstance(args)
	if err != nil {
		return nil, err
	}

	switch name {
	case "jenkinsGetJob":
		return p.toolGetJob(ctx, c, args)
	case "jenkinsListBuilds":
		return p.toolListBuilds(ctx, c, args)
	case "jenkinsGetBuild":
		return p.toolGetBuild(ctx, c, args)
	case "jenkinsTriggerBuild":
		return p.toolTriggerBuild(ctx, c, args)
	case "jenkinsGetConsoleText":
		return p.toolGetConsoleText(ctx, c, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
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

// resolveRunningJobBuild derives job full name and build number from executor metadata.
// Order: build URL, job URL + number field, then fullDisplayName heuristic.
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

// parseBuildFromFullDisplayName parses run titles like "folder » job #42" or "job #42".
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

// parseJobFullNameFromJobURL converts Jenkins "job URL" path into full job name, e.g.
// "/job/team/job/android/job/build/" => "team/android/build".
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

// parseBuildRefFromBuildURL converts Jenkins build URL path into full job name and build number, e.g.
// "/job/team/job/android/job/build/123/" => ("team/android/build", 123).
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

	// build number should be the next segment after the last "job/<name>" pair.
	// That means it is at index len(names)*2 (when starting from first segment 0).
	// Example: job/team/job/android/job/build/123 => segments:
	// [job,team,job,android,job,build,123]
	// len(names)=3 => idx=6 => parts[6]=123
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
