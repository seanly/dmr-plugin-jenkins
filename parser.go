package main

import (
	"fmt"
	"strconv"
	"strings"
)

// RequestParser parses map[string]any arguments into typed values
type RequestParser struct {
	args map[string]any
}

// NewRequestParser creates a new RequestParser
func NewRequestParser(args map[string]any) *RequestParser {
	if args == nil {
		args = make(map[string]any)
	}
	return &RequestParser{args: args}
}

// String parses a string argument
func (p *RequestParser) String(key string) string {
	if v, ok := p.args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// Int parses an integer argument
func (p *RequestParser) Int(key string) int {
	if v, ok := p.args[key].(float64); ok {
		return int(v)
	}
	return 0
}

// Bool parses a boolean argument with default value
func (p *RequestParser) Bool(key string, defaultVal bool) bool {
	if v, ok := p.args[key].(bool); ok {
		return v
	}
	return defaultVal
}

// StringMap parses a map[string]any into map[string]string
func (p *RequestParser) StringMap(key string) map[string]string {
	raw, ok := p.args[key].(map[string]any)
	if !ok {
		return nil
	}

	result := make(map[string]string)
	for k, v := range raw {
		switch val := v.(type) {
		case string:
			result[k] = val
		case float64:
			result[k] = strconv.FormatInt(int64(val), 10)
		case bool:
			result[k] = strconv.FormatBool(val)
		default:
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}

// Instance returns the instance ID from args or default
func (p *RequestParser) Instance(cfg *JenkinsPluginConfig) string {
	inst := p.String("instance")
	if inst == "" {
		inst = cfg.DefaultInstance
	}
	return inst
}
