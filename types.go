package main

import (
	"encoding/json"
)

// ============ Instances ============

// InstanceInfo represents a Jenkins instance
type InstanceInfo struct {
	ID   string `json:"id"`
	Host string `json:"host"`
}

// InstancesResponse is the response for jenkinsInstances tool
type InstancesResponse struct {
	Instances []InstanceInfo `json:"instances"`
}

// ============ Jobs ============

// GetJobRequest parameters for jenkinsGetJob
type GetJobRequest struct {
	Instance string `json:"instance,omitempty"`
	Job      string `json:"job,omitempty"` // Empty or "." means root
	Tree     string `json:"tree,omitempty"`
}

// GetJobResponse is the response for jenkinsGetJob
type GetJobResponse struct {
	Data json.RawMessage `json:"data"`
}

// ============ Builds ============

// ListBuildsRequest parameters for jenkinsListBuilds
type ListBuildsRequest struct {
	Instance       string `json:"instance,omitempty"`
	Job            string `json:"job,omitempty"`            // Empty means global mode
	Limit          int    `json:"limit,omitempty"`          // For job mode
	IncludeRunning bool   `json:"include_running,omitempty"` // For global mode
	IncludeQueued  bool   `json:"include_queued,omitempty"`  // For global mode
	RunningLimit   int    `json:"running_limit,omitempty"`   // For global mode
	QueueLimit     int    `json:"queue_limit,omitempty"`     // For global mode
}

// RunningBuild represents a running build in global mode
type RunningBuild struct {
	Job             string `json:"job,omitempty"`
	BuildNumber     int    `json:"build_number"`
	URL             string `json:"url"`
	FullDisplayName string `json:"full_display_name"`
	Node            string `json:"node"`
	Unparsed        bool   `json:"unparsed,omitempty"`
}

// QueuedBuild represents a queued build in global mode
type QueuedBuild struct {
	Job          string `json:"job"`
	QueueID      int    `json:"queue_id"`
	Why          string `json:"why"`
	Stuck        bool   `json:"stuck"`
	InQueueSince int64  `json:"in_queue_since"`
	TaskName     string `json:"task_name"`
	TaskURL      string `json:"task_url"`
}

// ListBuildsJobModeResponse is the response for jenkinsListBuilds in job mode
type ListBuildsJobModeResponse struct {
	Builds []json.RawMessage `json:"builds"`
	Limit  int               `json:"limit"`
}

// ListBuildsGlobalModeResponse is the response for jenkinsListBuilds in global mode
type ListBuildsGlobalModeResponse struct {
	Running      []RunningBuild    `json:"running"`
	Queued       []QueuedBuild     `json:"queued"`
	RunningLimit int               `json:"running_limit"`
	QueueLimit   int               `json:"queue_limit"`
	Errors       map[string]string `json:"errors,omitempty"`
}

// GetBuildRequest parameters for jenkinsGetBuild
type GetBuildRequest struct {
	Instance    string `json:"instance,omitempty"`
	Job         string `json:"job"`
	BuildNumber int    `json:"build_number"`
}

// GetBuildResponse is the response for jenkinsGetBuild
type GetBuildResponse struct {
	Data json.RawMessage `json:"data"`
}

// ============ Trigger ============

// TriggerBuildRequest parameters for jenkinsTriggerBuild
type TriggerBuildRequest struct {
	Instance   string            `json:"instance,omitempty"`
	Job        string            `json:"job"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// TriggerBuildResponse is the response for jenkinsTriggerBuild
type TriggerBuildResponse struct {
	Status         int  `json:"status"`
	Triggered      bool `json:"triggered"`
	WithParameters bool `json:"with_parameters,omitempty"`
}

// ============ Console ============

// GetConsoleTextRequest parameters for jenkinsGetConsoleText
type GetConsoleTextRequest struct {
	Instance    string `json:"instance,omitempty"`
	Job         string `json:"job"`
	BuildNumber int    `json:"build_number"`
	MaxChars    int    `json:"max_chars,omitempty"`
}

// GetConsoleTextResponse is the response for jenkinsGetConsoleText
type GetConsoleTextResponse struct {
	Text        string `json:"text"`
	Truncated   bool   `json:"truncated"`
	MaxChars    int    `json:"max_chars"`
	Job         string `json:"job"`
	BuildNumber int    `json:"build_number"`
}
