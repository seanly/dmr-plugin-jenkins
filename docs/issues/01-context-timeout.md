# 01 - Context 超时控制

## 问题描述

当前工具执行使用 `context.Background()`，导致请求可能无限挂起：

```go
func (p *JenkinsPlugin) executeTool(name string, args map[string]any) (any, error) {
    ctx := context.Background()  // 永不超时！
    // ...
}
```

## 影响

1. Jenkins 服务器不响应时，工具调用永久阻塞
2. DMR 无法优雅地取消进行中的请求
3. 插件 Shutdown 时无法终止进行中的 HTTP 请求

## 解决方案

### 1. 配置扩展

在 `config.go` 中添加超时配置：

```go
type JenkinsInstanceConfig struct {
    // ... 现有字段 ...
    RequestTimeoutSeconds int `json:"request_timeout_seconds"`
    ConnectTimeoutSeconds int `json:"connect_timeout_seconds"`
}

func defaultConfig() JenkinsPluginConfig {
    return JenkinsPluginConfig{
        // ...
        RequestTimeoutSeconds: 60,
        ConnectTimeoutSeconds: 10,
    }
}
```

### 2. executeTool 改造

```go
func (p *JenkinsPlugin) executeTool(name string, args map[string]any) (any, error) {
    timeout := time.Duration(p.cfg.RequestTimeoutSeconds) * time.Second
    if timeout == 0 {
        timeout = 60 * time.Second
    }
    
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    // ... 后续逻辑使用 ctx
}
```

### 3. HTTP 客户端支持 Context

`client.go` 中的方法需要接受 `context.Context`：

```go
func (jc *jenkinsClient) get(ctx context.Context, path string) (int, []byte, error) {
    return jc.doReq(ctx, http.MethodGet, path, nil, nil, "")
}

func (jc *jenkinsClient) doReq(ctx context.Context, method, path string, ...) (int, []byte, error) {
    reqURL, err := jc.urlFor(path)
    if err != nil {
        return 0, nil, err
    }
    
    req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
    // ...
}
```

## 实施步骤

1. 修改 `config.go` 添加超时配置字段和默认值
2. 修改 `client.go` 所有方法接受 `context.Context` 参数
3. 修改 `tools.go` 中的 `executeTool` 创建带超时的 Context
4. 将所有工具方法签名改为接受 `context.Context`

## 兼容性说明

- 工具接口（参数/返回）保持不变
- 新增配置项有默认值，现有配置无需修改
- HTTP 客户端内部实现变更，对外透明

## 参考代码

详见 [vsphere plugin client.go](https://github.com/seanly/dmr-plugin-vsphere/blob/main/client.go) 的实现方式。
