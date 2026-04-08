# 05 - 集中配置默认值

## 问题描述

当前默认值散布在代码各处：

```go
// client.go
if sec <= 0 {
    to = 30 * time.Second
}

// tools.go - toolListBuilds
limit := intArg(args, "limit")
if limit <= 0 {
    limit = 10
}

// tools.go - toolListBuilds (全局模式)
runningLimit := intArg(args, "running_limit")
if runningLimit <= 0 {
    runningLimit = 10
}
queueLimit := intArg(args, "queue_limit")
if queueLimit <= 0 {
    queueLimit = 20
}

// tools.go - toolGetConsoleText
maxChars := intArg(args, "max_chars")
if maxChars <= 0 {
    maxChars = 65536
}
```

问题：
1. 用户无法覆盖默认值
2. 维护困难（魔法数字遍布代码）
3. 不同工具默认值不一致

## 解决方案

### 1. 扩展配置结构

```go
// config.go
type JenkinsPluginConfig struct {
    DefaultInstance string                  `json:"default_instance"`
    Instances       []JenkinsInstanceConfig `json:"instances"`
    ConfigBaseDir   string                  `json:"config_base_dir"`
    Workspace       string                  `json:"workspace"`
    PluginName      string                  `json:"plugin_name"`
    
    // 新增：全局默认配置
    Defaults DefaultConfig `json:"defaults"`
}

type DefaultConfig struct {
    // 超时配置
    RequestTimeoutSeconds int `json:"request_timeout_seconds"`
    ConnectTimeoutSeconds int `json:"connect_timeout_seconds"`
    
    // 分页/限制配置
    MaxChars   int `json:"max_chars"`    // console 输出默认限制
    BuildLimit int `json:"build_limit"`  // build 列表默认限制
    
    // 全局查询配置
    RunningLimit int `json:"running_limit"` // 运行中 build 默认限制
    QueueLimit   int `json:"queue_limit"`   // 队列中 build 默认限制
}

func defaultConfig() JenkinsPluginConfig {
    return JenkinsPluginConfig{
        Defaults: DefaultConfig{
            RequestTimeoutSeconds: 60,
            ConnectTimeoutSeconds: 10,
            MaxChars:              65536,
            BuildLimit:            10,
            RunningLimit:          10,
            QueueLimit:            20,
        },
    }
}
```

### 2. Init 中合并配置

```go
func (p *JenkinsPlugin) Init(req *proto.InitRequest, resp *proto.InitResponse) error {
    // 先设置默认值
    p.cfg = defaultConfig()
    
    // 再解析用户配置（覆盖默认值）
    if req.ConfigJSON != "" && req.ConfigJSON != "null" {
        if err := json.Unmarshal([]byte(req.ConfigJSON), &p.cfg); err != nil {
            return fmt.Errorf("parse config: %w", err)
        }
    }
    
    // 验证配置
    if err := validateConfig(&p.cfg); err != nil {
        return err
    }
    
    return nil
}
```

### 3. 实例级别配置覆盖

允许每个实例有自己的超时配置：

```go
type JenkinsInstanceConfig struct {
    ID              string `json:"id"`
    BaseURL         string `json:"base_url"`
    Username        string `json:"username"`
    APIToken        string `json:"api_token"`
    VerifyTLS       *bool  `json:"verify_tls"`
    TimeoutSeconds  int    `json:"timeout_seconds"`  // 实例级别超时
    HTTPProxy       string `json:"http_proxy"`
    
    // 新增：实例级别缓存
    CacheTTLSeconds int `json:"cache_ttl_seconds"`
}

// 获取实例有效超时（实例配置 > 全局配置 > 硬编码）
func (inst *JenkinsInstanceConfig) effectiveTimeout(cfg *JenkinsPluginConfig) time.Duration {
    if inst.TimeoutSeconds > 0 {
        return time.Duration(inst.TimeoutSeconds) * time.Second
    }
    if cfg.Defaults.RequestTimeoutSeconds > 0 {
        return time.Duration(cfg.Defaults.RequestTimeoutSeconds) * time.Second
    }
    return 60 * time.Second
}
```

### 4. 工具中使用统一默认值

```go
func (p *JenkinsPlugin) toolGetConsoleText(ctx context.Context, client JenkinsClient, args map[string]any) (*GetConsoleTextResponse, error) {
    parser := NewRequestParser(args)
    req := &GetConsoleTextRequest{
        Instance:    parser.String("instance"),
        Job:         parser.String("job"),
        BuildNumber: parser.Int("build_number"),
        MaxChars:    parser.Int("max_chars"),
    }
    
    // 使用配置默认值
    if req.MaxChars <= 0 {
        req.MaxChars = p.cfg.Defaults.MaxChars
    }
    
    // ... 后续逻辑
}

func (p *JenkinsPlugin) toolListBuilds(ctx context.Context, client JenkinsClient, args map[string]any) (*ListBuildsResponse, error) {
    parser := NewRequestParser(args)
    req := &ListBuildsRequest{
        Instance:       parser.String("instance"),
        Job:            parser.String("job"),
        Limit:          parser.Int("limit"),
        IncludeRunning: parser.Bool("include_running", true),
        IncludeQueued:  parser.Bool("include_queued", true),
        RunningLimit:   parser.Int("running_limit"),
        QueueLimit:     parser.Int("queue_limit"),
    }
    
    // 使用配置默认值
    if req.Limit <= 0 {
        req.Limit = p.cfg.Defaults.BuildLimit
    }
    if req.RunningLimit <= 0 {
        req.RunningLimit = p.cfg.Defaults.RunningLimit
    }
    if req.QueueLimit <= 0 {
        req.QueueLimit = p.cfg.Defaults.QueueLimit
    }
    
    // ... 后续逻辑
}
```

### 5. 配置示例

```yaml
plugins:
  - name: jenkins
    enabled: true
    path: /opt/dmr/plugins/dmr-plugin-jenkins
    config:
      default_instance: ci-prod
      
      # 全局默认值
      defaults:
        request_timeout_seconds: 60
        connect_timeout_seconds: 10
        max_chars: 65536
        build_limit: 10
        running_limit: 10
        queue_limit: 20
      
      instances:
        - id: ci-prod
          base_url: "https://jenkins.prod.example.com"
          username: "svc-dmr"
          api_token: "enc:..."
          verify_tls: true
          timeout_seconds: 60          # 覆盖全局超时
          cache_ttl_seconds: 30        # 该实例启用缓存
          
        - id: ci-lab
          base_url: "https://jenkins.lab.local:8080"
          username: "dmr"
          api_token: "..."
          verify_tls: false
          timeout_seconds: 30          # 更快的超时
```

## 实施步骤

1. 修改 `config.go`，添加 `DefaultConfig` 结构体和 `defaultConfig()` 函数
2. 修改 `Init` 方法，实现"默认值 → 用户配置"的合并逻辑
3. 逐步替换各工具中的硬编码默认值为配置读取
4. 更新 README，提供完整的配置示例

## 好处

1. **用户可控**：用户可以根据环境调整默认值
2. **一处配置，全局生效**：不需要在每个工具调用中指定参数
3. **环境适配**：
   - 慢速网络环境：增加超时
   - 大型 Jenkins 实例：增加限制值
   - 开发环境：减少缓存时间
4. **向后兼容**：不提供新配置时使用合理的默认值

## 默认值策略

| 配置项 | 当前硬编码 | 建议默认值 | 说明 |
|--------|-----------|-----------|------|
| RequestTimeoutSeconds | 30/60 | 60 | API 调用超时 |
| ConnectTimeoutSeconds | 无 | 10 | 连接建立超时 |
| MaxChars | 65536 | 65536 | Console 输出截断 |
| BuildLimit | 10 | 10 | Job build 列表 |
| RunningLimit | 10 | 10 | 全局运行中 build |
| QueueLimit | 20 | 20 | 全局队列中 build |

## 参考实现

详见 [vsphere plugin config.go](https://github.com/seanly/dmr-plugin-vsphere/blob/main/config.go) 的 `defaultConfig()` 实现。
