package main

import (
	"net/url"
	"strconv"
	"strings"
)

// jobURLPath converts Jenkins full name ("folder/sub/job") to URL path "/job/folder/job/sub/job/job".
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
