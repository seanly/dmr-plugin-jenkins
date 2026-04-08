package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/seanly/dmr/pkg/plugin/proto"
)

// JenkinsPlugin implements proto.DMRPluginInterface.
type JenkinsPlugin struct {
	mu      sync.RWMutex
	cfg     JenkinsPluginConfig
	clients map[string]JenkinsClient
}

// NewJenkinsPlugin creates a new plugin instance.
func NewJenkinsPlugin() *JenkinsPlugin {
	return &JenkinsPlugin{
		cfg:     defaultConfig(),
		clients: make(map[string]JenkinsClient),
	}
}

// Init initializes the plugin with configuration.
// Uses lazy initialization - doesn't connect to Jenkins here.
func (p *JenkinsPlugin) Init(req *proto.InitRequest, resp *proto.InitResponse) error {
	// Parse config (will override defaults)
	if req.ConfigJSON != "" && req.ConfigJSON != "null" {
		if err := json.Unmarshal([]byte(req.ConfigJSON), &p.cfg); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}

	if err := validateConfig(&p.cfg); err != nil {
		return err
	}

	// Lazy initialization - don't create clients here
	p.clients = make(map[string]JenkinsClient)
	return nil
}

// Shutdown cleans up the plugin.
func (p *JenkinsPlugin) Shutdown(req *proto.ShutdownRequest, resp *proto.ShutdownResponse) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, client := range p.clients {
		_ = client.Close()
	}
	p.clients = make(map[string]JenkinsClient)
	return nil
}

// RequestApproval is not implemented by this plugin.
func (p *JenkinsPlugin) RequestApproval(req *proto.ApprovalRequest, resp *proto.ApprovalResult) error {
	resp.Choice = 0
	resp.Comment = "jenkins plugin does not handle approvals"
	return nil
}

// RequestBatchApproval is not implemented by this plugin.
func (p *JenkinsPlugin) RequestBatchApproval(req *proto.BatchApprovalRequest, resp *proto.BatchApprovalResult) error {
	resp.Choice = 0
	return nil
}

// ProvideTools returns the list of tools provided by this plugin.
func (p *JenkinsPlugin) ProvideTools(req *proto.ProvideToolsRequest, resp *proto.ProvideToolsResponse) error {
	resp.Tools = []proto.ToolDef{
		{
			Name:           "jenkinsInstances",
			Description:    "列出本插件已配置的 Jenkins 实例 id 与 host（不含密钥）",
			ParametersJSON: `{"type": "object", "properties": {}}`,
			Group:          "extended",
			SearchHint:     "jenkins, instance, list, ci, 实例, 列表",
		},
		{
			Name:        "jenkinsGetJob",
			Description: "获取 Job 元数据（GET api/json）。job 为 Jenkins UI 完整名称（含 Folder），如 team/android/build。省略、空或单个句点 . 表示列出实例根下顶层 Job/Folder",
			ParametersJSON: `{
				"type": "object",
				"properties": {
					"instance": {"type": "string", "description": "实例 id；省略时使用 default_instance"},
					"job": {"type": "string", "description": "Job full name；省略、空或单个句点 . 表示列出实例根下顶层 Job/Folder"},
					"tree": {"type": "string", "description": "可选 api/json tree 参数"}
				}
			}`,
			Group:      "extended",
			SearchHint: "jenkins, job, get, metadata, folder, ci, 任务, 获取, 元数据",
		},
		{
			Name:        "jenkinsListBuilds",
			Description: "列出 Job 构建列表（有 job 时）或全局正在运行/排队（等待资源）（无 job 时）",
			ParametersJSON: `{
				"type": "object",
				"properties": {
					"instance": {"type": "string"},
					"job": {"type": "string", "description": "Job full name；省略/为空则查询全局正在运行/排队"},
					"limit": {"type": "integer", "description": "仅当 job 存在时生效：返回该 job 最近若干次构建，默认 10"},
					"include_running": {"type": "boolean", "description": "是否包含正在执行的构建；默认 true"},
					"include_queued": {"type": "boolean", "description": "是否包含排队中/等待资源的构建；默认 true"},
					"running_limit": {"type": "integer", "description": "仅当 job 为空时生效：running 返回条数上限，默认 10"},
					"queue_limit": {"type": "integer", "description": "仅当 job 为空时生效：queued 返回条数上限，默认 20"}
				}
			}`,
			Group:      "extended",
			SearchHint: "jenkins, build, list, running, queued, ci, 构建, 列表, 运行中, 排队",
		},
		{
			Name:        "jenkinsGetBuild",
			Description: "获取指定构建的 api/json",
			ParametersJSON: `{
				"type": "object",
				"properties": {
					"instance": {"type": "string"},
					"job": {"type": "string"},
					"build_number": {"type": "integer"}
				},
				"required": ["job", "build_number"]
			}`,
			Group:      "extended",
			SearchHint: "jenkins, build, get, detail, ci, 构建, 获取, 详情",
		},
		{
			Name:        "jenkinsTriggerBuild",
			Description: "触发构建；无 parameters 则 POST build；有 parameters 则 buildWithParameters（form）",
			ParametersJSON: `{
				"type": "object",
				"properties": {
					"instance": {"type": "string"},
					"job": {"type": "string"},
					"parameters": {
						"type": "object",
						"additionalProperties": true,
						"description": "可选；非空时走 buildWithParameters，值为 string/number/bool"
					}
				},
				"required": ["job"]
			}`,
			Group:      "extended",
			SearchHint: "jenkins, build, trigger, start, ci, 构建, 触发, 启动",
		},
		{
			Name:        "jenkinsGetConsoleText",
			Description: "获取某次构建的控制台日志文本；可按 max_chars 截断 UTF-8",
			ParametersJSON: `{
				"type": "object",
				"properties": {
					"instance": {"type": "string"},
					"job": {"type": "string"},
					"build_number": {"type": "integer"},
					"max_chars": {"type": "integer", "description": "默认 65536"}
				},
				"required": ["job", "build_number"]
			}`,
			Group:      "extended",
			SearchHint: "jenkins, build, console, log, output, ci, 构建, 控制台, 日志, 输出",
		},
	}
	return nil
}

// CallTool executes a tool call.
func (p *JenkinsPlugin) CallTool(req *proto.CallToolRequest, resp *proto.CallToolResponse) error {
	var args map[string]any
	if err := json.Unmarshal([]byte(req.ArgsJSON), &args); err != nil {
		resp.Error = fmt.Sprintf("parse args: %v", err)
		return nil
	}

	// Create context with timeout
	timeout := time.Duration(p.cfg.Defaults.RequestTimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	result, err := p.executeToolWithTimeout(req.Name, args, timeout)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}

	b, err := json.Marshal(result)
	if err != nil {
		resp.Error = fmt.Sprintf("marshal result: %v", err)
		return nil
	}
	resp.ResultJSON = string(b)
	return nil
}

// executeToolWithTimeout executes a tool with the given timeout.
func (p *JenkinsPlugin) executeToolWithTimeout(name string, args map[string]any, timeout time.Duration) (any, error) {
	// Special case: jenkinsInstances doesn't need a client
	if name == "jenkinsInstances" {
		return p.toolInstances()
	}

	// Parse instance from args
	parser := NewRequestParser(args)
	instanceID := parser.Instance(&p.cfg)
	if instanceID == "" {
		return nil, fmt.Errorf("instance required (or set default_instance in plugin config)")
	}

	// Get or create client for this instance
	client, err := p.ensureClient(instanceID)
	if err != nil {
		return nil, err
	}

	// Execute the tool
	switch name {
	case "jenkinsGetJob":
		return p.toolGetJob(args, client)
	case "jenkinsListBuilds":
		return p.toolListBuilds(args, client)
	case "jenkinsGetBuild":
		return p.toolGetBuild(args, client)
	case "jenkinsTriggerBuild":
		return p.toolTriggerBuild(args, client)
	case "jenkinsGetConsoleText":
		return p.toolGetConsoleText(args, client)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// ensureClient ensures a client exists for the given instance.
// Uses lazy initialization with double-checked locking.
func (p *JenkinsPlugin) ensureClient(instanceID string) (JenkinsClient, error) {
	// Fast path: client exists
	p.mu.RLock()
	if client, ok := p.clients[instanceID]; ok {
		p.mu.RUnlock()
		return client, nil
	}
	p.mu.RUnlock()

	// Slow path: create client
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring lock
	if client, ok := p.clients[instanceID]; ok {
		return client, nil
	}

	// Find instance config
	var instCfg *JenkinsInstanceConfig
	for i := range p.cfg.Instances {
		if p.cfg.Instances[i].ID == instanceID {
			instCfg = &p.cfg.Instances[i]
			break
		}
	}
	if instCfg == nil {
		return nil, fmt.Errorf("unknown instance: %s", instanceID)
	}

	// Create HTTP client
	var client JenkinsClient
	httpClient, err := newHTTPJenkinsClient(context.Background(), instCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", instanceID, err)
	}
	client = httpClient

	// Wrap with cache if configured
	ttl := instCfg.effectiveCacheTTL(&p.cfg)
	if ttl > 0 {
		client = NewCachedClient(client, ttl)
	}

	p.clients[instanceID] = client
	return client, nil
}

// toolInstances returns the list of configured instances.
func (p *JenkinsPlugin) toolInstances() (*InstancesResponse, error) {
	resp := &InstancesResponse{
		Instances: make([]InstanceInfo, 0, len(p.cfg.Instances)),
	}

	for _, in := range p.cfg.Instances {
		host := in.BaseURL
		if u, err := url.Parse(strings.TrimSpace(in.BaseURL)); err == nil && u.Host != "" {
			host = u.Host
		}
		resp.Instances = append(resp.Instances, InstanceInfo{
			ID:   in.ID,
			Host: host,
		})
	}

	return resp, nil
}
