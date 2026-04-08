package main

import (
	"context"
)

// JenkinsClient defines the interface for Jenkins API operations.
// This abstraction allows for testing, caching, and retry implementations.
type JenkinsClient interface {
	// Job operations
	GetJob(ctx context.Context, job string, tree string) ([]byte, error)
	ListBuilds(ctx context.Context, job string, limit int) ([]byte, error)
	GetBuild(ctx context.Context, job string, buildNumber int) ([]byte, error)
	TriggerBuild(ctx context.Context, job string, params map[string]string) error
	GetConsoleText(ctx context.Context, job string, buildNumber int) (string, error)

	// Global operations
	GetComputers(ctx context.Context) ([]byte, error)
	GetQueue(ctx context.Context) ([]byte, error)

	// Lifecycle
	Close() error
}

// Ensure httpJenkinsClient implements JenkinsClient
var _ JenkinsClient = (*httpJenkinsClient)(nil)
