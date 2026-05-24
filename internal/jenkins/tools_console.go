package jenkins

import (
	"context"
	"fmt"
	"unicode/utf8"
)

// toolGetConsoleText implements jenkinsGetConsoleText tool
func (p *JenkinsPlugin) toolGetConsoleText(args map[string]any, client JenkinsClient) (*GetConsoleTextResponse, error) {
	parser := NewRequestParser(args)
	req := &GetConsoleTextRequest{
		Instance:    parser.String("instance"),
		Job:         parser.String("job"),
		BuildNumber: parser.Int("build_number"),
		MaxChars:    parser.Int("max_chars"),
	}

	if req.Job == "" {
		return nil, fmt.Errorf("job is required")
	}
	if req.BuildNumber <= 0 {
		return nil, fmt.Errorf("build_number is required")
	}

	// Apply default max_chars
	if req.MaxChars <= 0 {
		req.MaxChars = p.cfg.Defaults.MaxChars
	}

	text, err := client.GetConsoleText(context.Background(), req.Job, req.BuildNumber)
	if err != nil {
		return nil, err
	}

	// Truncate if needed
	truncated := false
	if req.MaxChars > 0 && utf8.RuneCountInString(text) > req.MaxChars {
		runes := []rune(text)
		text = string(runes[:req.MaxChars])
		truncated = true
	}

	return &GetConsoleTextResponse{
		Text:        text,
		Truncated:   truncated,
		MaxChars:    req.MaxChars,
		Job:         req.Job,
		BuildNumber: req.BuildNumber,
	}, nil
}
