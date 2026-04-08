# 03 - 客户端接口抽象

## 问题描述

当前 `jenkinsClient` 是具体类型，直接依赖 `net/http`：

```go
type jenkinsClient struct {
    baseURL string
    user    string
    token   string
    http    *http.Client
    // ... crumb 相关字段
}
```

这导致：
1. **难以测试**：无法 mock Jenkins API 响应
2. **难以扩展**：无法轻松添加缓存、重试、指标收集
3. **难以替换**：无法切换到其他 Jenkins 客户端库

## 解决方案

### 1. 定义接口

```go
// JenkinsClient 定义 Jenkins API 操作接口
type JenkinsClient interface {
    // Job 操作
    GetJob(ctx context.Context, job string, tree string) ([]byte, error)
    GetBuild(ctx context.Context, job string, buildNumber int) ([]byte, error)
    ListBuilds(ctx context.Context, job string, limit int) ([]byte, error)
    TriggerBuild(ctx context.Context, job string, params map[string]string) error
    GetConsoleText(ctx context.Context, job string, buildNumber int) (string, error)
    
    // 全局操作
    GetComputers(ctx context.Context) ([]byte, error)
    GetQueue(ctx context.Context) ([]byte, error)
    
    // 生命周期
    Close() error
}
```

### 2. 重命名现有实现

将现有的 `jenkinsClient` 重命名为 `httpJenkinsClient`：

```go
type httpJenkinsClient struct {
    baseURL string
    user    string
    token   string
    http    *http.Client
    mu      sync.Mutex
    crumb   *crumbData
}

func newHTTPJenkinsClient(ctx context.Context, cfg *JenkinsInstanceConfig) (*httpJenkinsClient, error) {
    // ... 创建逻辑
}

// 实现 JenkinsClient 接口
func (c *httpJenkinsClient) GetJob(ctx context.Context, job string, tree string) ([]byte, error) {
    // ... 实现
}

// ... 其他方法
```

### 3. 支持装饰器模式

添加缓存装饰器：

```go
// cachedJenkinsClient 为 JenkinsClient 添加缓存
type cachedJenkinsClient struct {
    inner JenkinsClient
    cache *inventoryCache  // 类似 vsphere 的实现
    ttl   time.Duration
}

func NewCachedClient(inner JenkinsClient, ttl time.Duration) JenkinsClient {
    return &cachedJenkinsClient{
        inner: inner,
        cache: newInventoryCache(ttl),
        ttl:   ttl,
    }
}

func (c *cachedJenkinsClient) GetJob(ctx context.Context, job string, tree string) ([]byte, error) {
    key := fmt.Sprintf("job:%s:%s", job, tree)
    if data, ok := c.cache.get(key); ok {
        return data, nil
    }
    
    data, err := c.inner.GetJob(ctx, job, tree)
    if err != nil {
        return nil, err
    }
    
    c.cache.set(key, data)
    return data, nil
}

// ... 其他方法
```

### 4. 插件结构体使用接口

```go
type JenkinsPlugin struct {
    mu      sync.RWMutex
    cfg     JenkinsPluginConfig
    clients map[string]JenkinsClient  // 使用接口而非具体类型
}
```

### 5. 支持缓存配置

```go
type JenkinsInstanceConfig struct {
    // ... 现有字段 ...
    CacheTTLSeconds int `json:"cache_ttl_seconds"`
}

func (p *JenkinsPlugin) ensureClient(ctx context.Context, instanceID string) (JenkinsClient, error) {
    // ... 创建 httpJenkinsClient ...
    
    // 包装缓存层（如果配置启用）
    if instCfg.CacheTTLSeconds > 0 {
        client = NewCachedClient(client, time.Duration(instCfg.CacheTTLSeconds)*time.Second)
    }
    
    return client, nil
}
```

## 实施步骤

1. 创建 `client_interface.go` 定义 `JenkinsClient` 接口
2. 重命名 `client.go` 为 `http_client.go`，结构体重命名为 `httpJenkinsClient`
3. 确保 `httpJenkinsClient` 实现 `JenkinsClient` 接口（编译期检查）
4. 修改 `plugin.go`，`clients` 映射改为 `map[string]JenkinsClient`
5. （可选）创建 `cached_client.go` 实现缓存装饰器

## 好处

1. **可测试性**：可以 mock `JenkinsClient` 进行单元测试
2. **可扩展性**：可以轻松添加：
   - 缓存层（缓存 Job 信息、Build 列表）
   - 重试逻辑（自动重试 5xx 错误）
   - 指标收集（记录 API 调用次数和延迟）
   - 断路器（Jenkins 故障时快速失败）
3. **解耦**：工具逻辑与 HTTP 实现解耦

## 缓存策略建议

对于 Jenkins API，建议缓存：

| 端点 | 缓存时间 | 原因 |
|------|----------|------|
| Job 元数据 | 30-60s | 相对静态 |
| Build 列表 | 10-30s | 变化较快 |
| Build 详情 | 永不缓存 | 经常变化 |
| Console 输出 | 永不缓存 | 实时数据 |
| Computer/Queue | 5-10s | 实时性要求高 |

## 参考代码

详见 [vsphere plugin client.go](https://github.com/seanly/dmr-plugin-vsphere/blob/main/client.go) 的接口定义和缓存实现。
