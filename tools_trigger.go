package main

import (
	"context"
	"fmt"
)

const httpStatusCreated = 201

// toolTriggerBuild implements jenkinsTriggerBuild tool
func (p *JenkinsPlugin) toolTriggerBuild(args map[string]any, client JenkinsClient) (*TriggerBuildResponse, error) {
	parser := NewRequestParser(args)
	req := &TriggerBuildRequest{
		Instance:   parser.String("instance"),
		Job:        parser.String("job"),
		Parameters: parser.StringMap("parameters"),
	}

	if req.Job == "" {
		return nil, fmt.Errorf("job is required")
	}

	err := client.TriggerBuild(context.Background(), req.Job, req.Parameters)
	if err != nil {
		return nil, err
	}

	return &TriggerBuildResponse{
		Status:         httpStatusCreated,
		Triggered:      true,
		WithParameters: len(req.Parameters) > 0,
	}, nil
}
