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

const defaultJobTree = "name,url,buildable,lastBuild[number,url,result,timestamp]"

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
	if job == "" {
		return nil, fmt.Errorf("job required")
	}
	tree := strArg(args, "tree")
	if tree == "" {
		tree = defaultJobTree
	}
	path := jobURLPath(job) + "/api/json?tree=" + url.QueryEscape(tree)
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
	if job == "" {
		return nil, fmt.Errorf("job required")
	}
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
		"text":       text,
		"truncated":  truncated,
		"max_chars":  maxChars,
		"job":        job,
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
