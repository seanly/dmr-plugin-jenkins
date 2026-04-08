package main

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultJobTree     = "name,url,buildable,lastBuild[number,url,result,timestamp]"
	defaultRootJobTree = "jobs[name,url,buildable,_class,lastBuild[number,url,result,timestamp]]"
)

// jenkinsGetJobIsRootListing checks if job parameter indicates root listing
func jenkinsGetJobIsRootListing(job string) bool {
	j := strings.TrimSpace(job)
	return j == "" || j == "."
}

// toolGetJob implements jenkinsGetJob tool
func (p *JenkinsPlugin) toolGetJob(args map[string]any, client JenkinsClient) (*GetJobResponse, error) {
	parser := NewRequestParser(args)
	req := &GetJobRequest{
		Instance: parser.String("instance"),
		Job:      parser.String("job"),
		Tree:     parser.String("tree"),
	}

	// Set default tree if not provided
	if req.Tree == "" {
		if jenkinsGetJobIsRootListing(req.Job) {
			req.Tree = defaultRootJobTree
		} else {
			req.Tree = defaultJobTree
		}
	}

	// Normalize job name
	job := req.Job
	if jenkinsGetJobIsRootListing(job) {
		job = "."
	}

	data, err := client.GetJob(context.Background(), job, req.Tree)
	if err != nil {
		return nil, err
	}

	return &GetJobResponse{Data: data}, nil
}

// jobURLPath converts Jenkins full name ("folder/sub/job") to URL path "/job/folder/job/sub/job/job"
func jobURLPath(job string) string {
	job = strings.Trim(job, "/")
	if job == "" {
		return "/job"
	}
	parts := strings.Split(job, "/")
	escaped := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(p))
	}
	if len(escaped) == 0 {
		return "/job"
	}
	return "/job/" + strings.Join(escaped, "/job/")
}

// jobConsolePath returns path to build console text: .../job/.../N/consoleText
func jobConsolePath(job string, buildNumber int) string {
	return jobURLPath(job) + "/" + strconv.Itoa(buildNumber) + "/consoleText"
}
