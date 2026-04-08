# 04 - 强类型化请求/响应

## 问题描述

当前工具使用 `map[string]any` 处理参数和结果：

```go
func (p *JenkinsPlugin) toolGetJob(ctx context.Context, c *jenkinsClient, args map[string]any) (any, error) {
    job := strArg(args, "job")  // 运行时才能发现拼写错误
    tree := strArg(args, "tree")
    // ...
}
```

问题：
1. 编译期无法检查字段名拼写错误
2. IDE 无法提供自动补全
3. 参数类型转换分散在各处
4. 返回结构不清晰

## 解决方案

### 1. 定义请求/响应类型

创建 `types.go` 文件：

```go
package main

// ============ Jobs ============

type GetJobRequest struct {
    Instance string `json:"instance,omitempty"`
    Job      string `json:"job,omitempty"`      // 空或 "." 表示根
    Tree     string `json:"tree,omitempty"`
}

type GetJobResponse struct {
    // 直接返回 API 原始数据或包装结构
    Data json.RawMessage `json:"data"`
}

// ============ Builds ============

type ListBuildsRequest struct {
    Instance       string `json:"instance,omitempty"`
    Job            string `json:"job,omitempty"`            // 空表示全局
    Limit          int    `json:"limit,omitempty"`          // Job 模式使用
    IncludeRunning bool   `json:"include_running,omitempty"` // 全局模式使用
    IncludeQueued  bool   `json:"include_queued,omitempty"`  // 全局模式使用
    RunningLimit   int    `json:"running_limit,omitempty"`   // 全局模式使用
    QueueLimit     int    `json:"queue_limit,omitempty"`     // 全局模式使用
}

type ListBuildsResponse struct {
    // Job 模式
    Builds []json.RawMessage `json:"builds,omitempty"`
    Limit  int               `json:"limit,omitempty"`
    
    // 全局模式
    Running      []RunningBuild `json:"running,omitempty"`
    Queued       []QueuedBuild  `json:"queued,omitempty"`
    RunningLimit int            `json:"running_limit,omitempty"`
    QueueLimit   int            `json:"queue_limit,omitempty"`
    Errors       map[string]string `json:"errors,omitempty"`
}

type RunningBuild struct {
    Job          string `json:"job,omitempty"`
    BuildNumber  int    `json:"build_number"`
    URL          string `json:"url"`
    FullDisplayName string `json:"full_display_name"`
    Node         string `json:"node"`
    Unparsed     bool   `json:"unparsed,omitempty"`
}

type QueuedBuild struct {
    Job          string `json:"job"`
    QueueID      int    `json:"queue_id"`
    Why          string `json:"why"`
    Stuck        bool   `json:"stuck"`
    InQueueSince int64  `json:"in_queue_since"`
    TaskName     string `json:"task_name"`
    TaskURL      string `json:"task_url"`
}

type GetBuildRequest struct {
    Instance    string `json:"instance,omitempty"`
    Job         string `json:"job"`
    BuildNumber int    `json:"build_number"`
}

type GetBuildResponse struct {
    Data json.RawMessage `json:"data"`
}

type TriggerBuildRequest struct {
    Instance   string            `json:"instance,omitempty"`
    Job        string            `json:"job"`
    Parameters map[string]string `json:"parameters,omitempty"`
}

type TriggerBuildResponse struct {
    Status         int  `json:"status"`
    Triggered      bool `json:"triggered"`
    WithParameters bool `json:"with_parameters,omitempty"`
}

type GetConsoleTextRequest struct {
    Instance    string `json:"instance,omitempty"`
    Job         string `json:"job"`
    BuildNumber int    `json:"build_number"`
    MaxChars    int    `json:"max_chars,omitempty"`
}

type GetConsoleTextResponse struct {
    Text       string `json:"text"`
    Truncated  bool   `json:"truncated"`
    MaxChars   int    `json:"max_chars"`
    Job        string `json:"job"`
    BuildNumber int  `json:"build_number"`
}

// ============ Instances ============

type InstanceInfo struct {
    ID   string `json:"id"`
    Host string `json:"host"`
}

type InstancesResponse struct {
    Instances []InstanceInfo `json:"instances"`
}
```

### 2. 参数解析辅助函数

```go
// parseRequest 将 map[string]any 解析为强类型结构
type RequestParser struct {
    args map[string]any
}

func NewRequestParser(args map[string]any) *RequestParser {
    return &RequestParser{args: args}
}

func (p *RequestParser) String(key string) string {
    if v, ok := p.args[key].(string); ok {
        return strings.TrimSpace(v)
    }
    return ""
}

func (p *RequestParser) Int(key string) int {
    if v, ok := p.args[key].(float64); ok {
        return int(v)
    }
    return 0
}

func (p *RequestParser) Bool(key string, defaultVal bool) bool {
    if v, ok := p.args[key].(bool); ok {
        return v
    }
    return defaultVal
}

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
```

### 3. 工具方法改造示例

```go
func (p *JenkinsPlugin) toolGetJob(ctx context.Context, client JenkinsClient, args map[string]any) (*GetJobResponse, error) {
    parser := NewRequestParser(args)
    req := &GetJobRequest{
        Instance: parser.String("instance"),
        Job:      parser.String("job"),
        Tree:     parser.String("tree"),
    }
    
    if req.Tree == "" {
        if jenkinsGetJobIsRootListing(req.Job) {
            req.Tree = defaultRootJobTree
        } else {
            req.Tree = defaultJobTree
        }
    }
    
    data, err := client.GetJob(ctx, req.Job, req.Tree)
    if err != nil {
        return nil, err
    }
    
    return &GetJobResponse{Data: data}, nil
}

func (p *JenkinsPlugin) toolTriggerBuild(ctx context.Context, client JenkinsClient, args map[string]any) (*TriggerBuildResponse, error) {
    parser := NewRequestParser(args)
    req := &TriggerBuildRequest{
        Instance:   parser.String("instance"),
        Job:        parser.String("job"),
        Parameters: parser.StringMap("parameters"),
    }
    
    if req.Job == "" {
        return nil, fmt.Errorf("job is required")
    }
    
    err := client.TriggerBuild(ctx, req.Job, req.Parameters)
    if err != nil {
        return nil, err
    }
    
    return &TriggerBuildResponse{
        Status:         httpStatusCreated,
        Triggered:      true,
        WithParameters: len(req.Parameters) > 0,
    }, nil
}
```

## 实施步骤

1. 创建 `types.go` 定义所有请求/响应结构
2. 创建 `parser.go` 实现参数解析辅助函数
3. 逐个改造工具方法，从 `map[string]any` 改为强类型
4. 更新工具路由逻辑

## 好处

1. **编译期检查**：字段名拼写错误在编译时发现
2. **IDE 支持**：自动补全、跳转到定义、重构支持
3. **自文档化**：类型定义本身就是文档
4. **易于测试**：可以直接构造请求/响应结构进行测试
5. **一致的默认值**：可以在 Parser 中统一处理默认值逻辑

## 与 JSON Schema 的关系

强类型结构应与 `ProvideTools` 中声明的 JSON Schema 保持一致：

```go
{
    Name:        "jenkinsGetJob",
    ParametersJSON: `{
        "type": "object",
        "properties": {
            "instance": {"type": "string"},
            "job": {"type": "string"},
            "tree": {"type": "string"}
        }
    }`,
    // 对应 GetJobRequest 结构
}
```

## 兼容性说明

- 工具接口（JSON Schema）保持不变
- 返回的 JSON 结构保持不变
- 内部实现从弱类型改为强类型
