# 07 - 缓存层设计（可选）

## 背景

Jenkins API 调用可能存在以下特点：
- Job 配置变化不频繁，适合缓存
- Build 列表变化较快，短时缓存即可
- Console 输出实时变化，不应缓存

添加缓存层可以减少：
- Jenkins 服务器负载
- 网络延迟
- API 限流风险

## 设计目标

1. **可选性**：默认禁用，用户显式启用
2. **细粒度**：支持按实例、按 API 端点配置
3. **透明性**：对工具逻辑完全透明
4. **一致性**：遵循与其他 DMR 插件相同的模式

## 架构设计

### 1. 缓存键设计

```go
// cacheKey 唯一标识缓存条目
type cacheKey struct {
    InstanceID string // 实例 ID
    Method     string // API 方法，如 "GetJob", "GetBuild"
    Params     string // 序列化的参数，如 "my-job/main:tree=name,url"
}

func (k cacheKey) String() string {
    return fmt.Sprintf("%s:%s:%s", k.InstanceID, k.Method, k.Params)
}
```

### 2. 缓存条目

```go
// cacheEntry 带过期时间的缓存条目
type cacheEntry struct {
    data      []byte
    timestamp time.Time
}

func (e *cacheEntry) expired(ttl time.Duration) bool {
    return time.Since(e.timestamp) > ttl
}
```

### 3. 缓存管理器

```go
// cacheManager 提供线程安全的缓存操作
type cacheManager struct {
    mu   sync.RWMutex
    data map[string]cacheEntry
    ttl  time.Duration
}

func newCacheManager(ttl time.Duration) *cacheManager {
    return &cacheManager{
        data: make(map[string]cacheEntry),
        ttl:  ttl,
    }
}

func (c *cacheManager) get(key string) ([]byte, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    entry, ok := c.data[key]
    if !ok || entry.expired(c.ttl) {
        return nil, false
    }
    return entry.data, true
}

func (c *cacheManager) set(key string, data []byte) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    c.data[key] = cacheEntry{
        data:      data,
        timestamp: time.Now(),
    }
}

func (c *cacheManager) clear() {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    c.data = make(map[string]cacheEntry)
}

// 清理过期条目（可选的定期任务）
func (c *cacheManager) evictExpired() {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    for key, entry := range c.data {
        if entry.expired(c.ttl) {
            delete(c.data, key)
        }
    }
}
```

### 4. 缓存装饰器

```go
// cachedJenkinsClient 为 JenkinsClient 添加缓存
type cachedJenkinsClient struct {
    inner JenkinsClient
    cache *cacheManager
}

func NewCachedClient(inner JenkinsClient, ttl time.Duration) JenkinsClient {
    return &cachedJenkinsClient{
        inner: inner,
        cache: newCacheManager(ttl),
    }
}

func (c *cachedJenkinsClient) GetJob(ctx context.Context, job string, tree string) ([]byte, error) {
    key := cacheKey{
        Method: "GetJob",
        Params: fmt.Sprintf("%s:tree=%s", job, tree),
    }.String()
    
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

// 不支持缓存的方法直接透传
func (c *cachedJenkinsClient) TriggerBuild(ctx context.Context, job string, params map[string]string) error {
    return c.inner.TriggerBuild(ctx, job, params)
}

func (c *cachedJenkinsClient) GetConsoleText(ctx context.Context, job string, buildNumber int) (string, error) {
    return c.inner.GetConsoleText(ctx, job, buildNumber)
}

// ... 其他方法
```

## 配置设计

### 1. 全局缓存配置

```go
type DefaultConfig struct {
    // ... 其他配置 ...
    CacheTTLSeconds int `json:"cache_ttl_seconds"` // 默认 0（禁用）
}
```

### 2. 实例级缓存配置

```go
type JenkinsInstanceConfig struct {
    // ... 其他字段 ...
    CacheTTLSeconds int `json:"cache_ttl_seconds"` // 覆盖全局配置
}
```

### 3. 配置示例

```yaml
plugins:
  - name: jenkins
    enabled: true
    config:
      # 全局默认缓存 60 秒
      defaults:
        cache_ttl_seconds: 60
      
      instances:
        - id: ci-prod
          base_url: "https://jenkins.prod.example.com"
          # 使用全局缓存配置
          
        - id: ci-lab
          base_url: "https://jenkins.lab.local:8080"
          # 该实例禁用缓存（敏感环境）
          cache_ttl_seconds: 0
          
        - id: ci-readonly
          base_url: "https://jenkins.readonly.example.com"
          # 该实例使用更长缓存（只读镜像）
          cache_ttl_seconds: 300
```

## 缓存策略建议

### 缓存时长建议

| API 端点 | 建议 TTL | 原因 |
|---------|----------|------|
| GetJob (无 tree) | 30-60s | Job 配置变化不频繁 |
| GetJob (有 tree) | 10-30s | 取决于 tree 字段 |
| ListBuilds (Job) | 5-10s | Build 列表变化快 |
| GetBuild | 0s | 不应缓存 |
| GetComputer | 5s | 实时性要求高 |
| GetQueue | 5s | 实时性要求高 |
| GetConsoleText | 0s | 实时数据，不缓存 |

### 动态缓存控制

可以在工具参数中添加 `nocache` 选项强制跳过缓存：

```go
type GetJobRequest struct {
    Instance string `json:"instance,omitempty"`
    Job      string `json:"job,omitempty"`
    Tree     string `json:"tree,omitempty"`
    NoCache  bool   `json:"nocache,omitempty"` // 强制跳过缓存
}
```

## 高级功能（可选）

### 1. 缓存预热

在插件启动时预加载常用数据：

```go
func (p *JenkinsPlugin) warmCache(ctx context.Context) {
    for _, inst := range p.cfg.Instances {
        client, _ := p.ensureClient(ctx, inst.ID)
        if cached, ok := client.(*cachedJenkinsClient); ok {
            // 预加载根 Job 列表
            cached.GetJob(ctx, ".", "jobs[name,url]")
        }
    }
}
```

### 2. 缓存统计

添加缓存命中率监控：

```go
type cacheStats struct {
    hits   uint64
    misses uint64
}

func (c *cacheManager) stats() (hits, misses uint64) {
    // 返回统计信息
}
```

### 3. 条件缓存

根据响应头决定是否缓存：

```go
func shouldCache(statusCode int, headers http.Header) bool {
    // 只缓存 200 响应
    // 检查 Cache-Control 头
}
```

## 实施步骤

1. 创建 `cached_client.go` 实现缓存装饰器
2. 修改配置结构，添加 `CacheTTLSeconds` 字段
3. 修改 `ensureClient`，根据配置包装缓存层
4. 更新工具文档，说明缓存行为

## 风险与注意事项

1. **数据一致性**：缓存可能导致数据延迟
   - 提供 `nocache` 选项绕过缓存
   - 合理设置 TTL

2. **内存使用**：无限制的缓存会消耗内存
   - 考虑添加最大条目限制
   - 定期清理过期条目

3. **并发安全**：确保缓存操作线程安全
   - 使用 `sync.RWMutex`
   - 注意锁的粒度

## 参考实现

详见 [vsphere plugin client.go](https://github.com/seanly/dmr-plugin-vsphere/blob/main/client.go) 的 `cachedClient` 实现。
