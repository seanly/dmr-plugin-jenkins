package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/seanly/dmr/pkg/plugin/proto"
)

// JenkinsPlugin implements proto.DMRPluginInterface.
type JenkinsPlugin struct {
	cfg     JenkinsPluginConfig
	clients map[string]*jenkinsClient
}

// NewJenkinsPlugin creates an empty plugin (config applied in Init).
func NewJenkinsPlugin() *JenkinsPlugin {
	return &JenkinsPlugin{
		clients: make(map[string]*jenkinsClient),
	}
}

func (p *JenkinsPlugin) Init(req *proto.InitRequest, resp *proto.InitResponse) error {
	if req.ConfigJSON != "" {
		if err := json.Unmarshal([]byte(req.ConfigJSON), &p.cfg); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}
	if err := validateConfig(&p.cfg); err != nil {
		return err
	}

	p.clients = make(map[string]*jenkinsClient)
	for i := range p.cfg.Instances {
		inst := &p.cfg.Instances[i]
		id := strings.TrimSpace(inst.ID)
		base, err := NormalizeBaseURL(inst.BaseURL)
		if err != nil {
			return fmt.Errorf("instance %q: %w", id, err)
		}
		sec := inst.TimeoutSeconds
		to := time.Duration(sec) * time.Second
		if sec <= 0 {
			to = 30 * time.Second
		}
		cl, err := newJenkinsClient(
			base,
			strings.TrimSpace(inst.Username),
			strings.TrimSpace(inst.APIToken),
			inst.normalizedVerifyTLS(),
			to,
			inst.HTTPProxy,
		)
		if err != nil {
			return fmt.Errorf("instance %q: %w", id, err)
		}
		p.clients[id] = cl
	}
	return nil
}

func (p *JenkinsPlugin) Shutdown(req *proto.ShutdownRequest, resp *proto.ShutdownResponse) error {
	return nil
}

func (p *JenkinsPlugin) RequestApproval(req *proto.ApprovalRequest, resp *proto.ApprovalResult) error {
	resp.Choice = 0
	resp.Comment = "jenkins plugin does not handle approvals"
	return nil
}

func (p *JenkinsPlugin) RequestBatchApproval(req *proto.BatchApprovalRequest, resp *proto.BatchApprovalResult) error {
	resp.Choice = 0
	return nil
}

func (p *JenkinsPlugin) ProvideTools(req *proto.ProvideToolsRequest, resp *proto.ProvideToolsResponse) error {
	resp.Tools = []proto.ToolDef{
		{
			Name:        "jenkinsInstances",
			Description: "列出本插件已配置的 Jenkins 实例 id 与 host（不含密钥）",
			ParametersJSON: `{
				"type": "object",
				"properties": {}
			}`,
		},
		{
			Name:        "jenkinsGetJob",
			Description: "获取 Job 元数据（GET api/json）；job 为 Jenkins UI 完整名称（含 Folder），如 team/android/build",
			ParametersJSON: `{
				"type": "object",
				"properties": {
					"instance": {"type": "string", "description": "实例 id；省略时使用 default_instance"},
					"job": {"type": "string", "description": "Job full name"},
					"tree": {"type": "string", "description": "可选 api/json tree 参数"}
				},
				"required": ["job"]
			}`,
		},
		{
			Name:        "jenkinsListBuilds",
			Description: "列出 Job 最近若干次构建（api/json tree=builds[...]）",
			ParametersJSON: `{
				"type": "object",
				"properties": {
					"instance": {"type": "string"},
					"job": {"type": "string"},
					"limit": {"type": "integer", "description": "数量，默认 10"}
				},
				"required": ["job"]
			}`,
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
		},
	}
	return nil
}

func (p *JenkinsPlugin) CallTool(req *proto.CallToolRequest, resp *proto.CallToolResponse) error {
	var args map[string]any
	if err := json.Unmarshal([]byte(req.ArgsJSON), &args); err != nil {
		resp.Error = fmt.Sprintf("parse args: %v", err)
		return nil
	}
	out, err := p.executeTool(req.Name, args)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		resp.Error = fmt.Sprintf("marshal result: %v", err)
		return nil
	}
	resp.ResultJSON = string(b)
	return nil
}
