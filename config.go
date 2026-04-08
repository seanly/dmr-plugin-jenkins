package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// JenkinsPluginConfig is the plugin Init JSON (DMR-injected keys included).
type JenkinsPluginConfig struct {
	DefaultInstance string                  `json:"default_instance"`
	Instances       []JenkinsInstanceConfig `json:"instances"`
	ConfigBaseDir   string                  `json:"config_base_dir"`
	Workspace       string                  `json:"workspace"`
	PluginName      string                  `json:"plugin_name"`

	// Defaults holds global default configuration
	Defaults DefaultConfig `json:"defaults"`
}

// DefaultConfig holds default values for various operations
type DefaultConfig struct {
	// Timeout configurations
	RequestTimeoutSeconds int `json:"request_timeout_seconds"`
	ConnectTimeoutSeconds int `json:"connect_timeout_seconds"`

	// Limit configurations
	MaxChars     int `json:"max_chars"`
	BuildLimit   int `json:"build_limit"`
	RunningLimit int `json:"running_limit"`
	QueueLimit   int `json:"queue_limit"`

	// Cache configuration
	CacheTTLSeconds int `json:"cache_ttl_seconds"`
}

// defaultConfig returns the default configuration
func defaultConfig() JenkinsPluginConfig {
	return JenkinsPluginConfig{
		Defaults: DefaultConfig{
			RequestTimeoutSeconds: 60,
			ConnectTimeoutSeconds: 10,
			MaxChars:              65536,
			BuildLimit:            10,
			RunningLimit:          10,
			QueueLimit:            20,
			CacheTTLSeconds:       0, // Disabled by default
		},
	}
}

// JenkinsInstanceConfig describes one Jenkins server.
type JenkinsInstanceConfig struct {
	ID              string `json:"id"`
	BaseURL         string `json:"base_url"`
	Username        string `json:"username"`
	APIToken        string `json:"api_token"`
	VerifyTLS       *bool  `json:"verify_tls"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	HTTPProxy       string `json:"http_proxy"`
	CacheTTLSeconds int    `json:"cache_ttl_seconds"`
}

// effectiveTimeout returns the effective timeout for this instance
func (inst *JenkinsInstanceConfig) effectiveTimeout(cfg *JenkinsPluginConfig) time.Duration {
	if inst.TimeoutSeconds > 0 {
		return time.Duration(inst.TimeoutSeconds) * time.Second
	}
	if cfg.Defaults.RequestTimeoutSeconds > 0 {
		return time.Duration(cfg.Defaults.RequestTimeoutSeconds) * time.Second
	}
	return 60 * time.Second
}

// effectiveCacheTTL returns the effective cache TTL for this instance
func (inst *JenkinsInstanceConfig) effectiveCacheTTL(cfg *JenkinsPluginConfig) time.Duration {
	if inst.CacheTTLSeconds >= 0 {
		return time.Duration(inst.CacheTTLSeconds) * time.Second
	}
	return time.Duration(cfg.Defaults.CacheTTLSeconds) * time.Second
}

// NormalizeBaseURL trims trailing slashes from Jenkins root URL.
func NormalizeBaseURL(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "", fmt.Errorf("base_url is empty")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", fmt.Errorf("base_url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("base_url must include scheme and host")
	}
	return strings.TrimRight(u, "/"), nil
}

func (c *JenkinsInstanceConfig) normalizedVerifyTLS() bool {
	if c.VerifyTLS == nil {
		return true
	}
	return *c.VerifyTLS
}

func validateConfig(cfg *JenkinsPluginConfig) error {
	if len(cfg.Instances) < 1 {
		return fmt.Errorf("instances: at least one entry is required")
	}
	seen := make(map[string]struct{})
	for i := range cfg.Instances {
		inst := &cfg.Instances[i]
		id := strings.TrimSpace(inst.ID)
		if id == "" {
			return fmt.Errorf("instances[%d].id is required", i)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("duplicate instance id %q", id)
		}
		seen[id] = struct{}{}

		if _, err := NormalizeBaseURL(inst.BaseURL); err != nil {
			return fmt.Errorf("instances[%d].base_url: %w", i, err)
		}
		if strings.TrimSpace(inst.Username) == "" {
			return fmt.Errorf("instances[%d].username is required", i)
		}
		if strings.TrimSpace(inst.APIToken) == "" {
			return fmt.Errorf("instances[%d].api_token is required", i)
		}
	}
	if cfg.DefaultInstance != "" {
		if _, ok := seen[cfg.DefaultInstance]; !ok {
			return fmt.Errorf("default_instance %q is not in instances[].id", cfg.DefaultInstance)
		}
	}
	return nil
}
