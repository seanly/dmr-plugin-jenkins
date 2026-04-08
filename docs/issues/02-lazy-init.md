# 02 - 延迟初始化（Lazy Initialization）

## 问题描述

当前 `Init` 阶段创建所有 Jenkins 客户端：

```go
func (p *JenkinsPlugin) Init(req *proto.InitRequest, resp *proto.InitResponse) error {
    // ... 解析配置 ...
    
    for i := range p.cfg.Instances {
        inst := &p.cfg.Instances[i]
        // ... 验证 ...
        cl, err := newJenkinsClient(...)  // 可能失败！
        if err != nil {
            return fmt.Errorf("instance %q: %w", id, err)  // 整个插件启动失败
        }
        p.clients[id] = cl
    }
    return nil
}
```

## 影响

1. **单点故障**：一个实例配置错误（如无效 URL、网络不通）导致整个插件初始化失败
2. **启动延迟**：DMR 启动时需要等待所有 Jenkins 实例连接验证
3. **资源浪费**：配置了但未使用的实例也占用资源

## 解决方案

### 1. Init 阶段仅保存配置

```go
func (p *JenkinsPlugin) Init(req *proto.InitRequest, resp *proto.InitResponse) error {
    if req.ConfigJSON != "" && req.ConfigJSON != "null" {
        if err := json.Unmarshal([]byte(req.ConfigJSON), &p.cfg); err != nil {
            return fmt.Errorf("parse config: %w", err)
        }
    }
    if err := validateConfig(&p.cfg); err != nil {
        return err
    }
    
    // 延迟初始化 - 不在这里创建客户端
    p.clients = make(map[string]*jenkinsClient)
    return nil
}
```

### 2. 添加 ensureClient 方法

```go
func (p *JenkinsPlugin) ensureClient(ctx context.Context, instanceID string) (*jenkinsClient, error) {
    // 快速路径：已存在
    p.mu.RLock()
    if client, ok := p.clients[instanceID]; ok {
        p.mu.RUnlock()
        return client, nil
    }
    p.mu.RUnlock()
    
    // 慢速路径：创建客户端
    p.mu.Lock()
    defer p.mu.Unlock()
    
    // 双重检查
    if client, ok := p.clients[instanceID]; ok {
        return client, nil
    }
    
    // 查找实例配置
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
    
    // 创建客户端
    client, err := newJenkinsClient(ctx, instCfg)  // 需要支持 context
    if err != nil {
        return nil, fmt.Errorf("connect to %s: %w", instanceID, err)
    }
    
    p.clients[instanceID] = client
    return client, nil
}
```

### 3. 工具方法中使用

```go
func (p *JenkinsPlugin) toolGetJob(ctx context.Context, args map[string]any) (any, error) {
    instanceID, err := p.resolveInstanceID(args)
    if err != nil {
        return nil, err
    }
    
    client, err := p.ensureClient(ctx, instanceID)
    if err != nil {
        return nil, err
    }
    
    // ... 使用 client 调用 API
}
```

### 4. 线程安全

添加互斥锁保护客户端映射：

```go
type JenkinsPlugin struct {
    mu      sync.RWMutex
    cfg     JenkinsPluginConfig
    clients map[string]*jenkinsClient
}
```

## 实施步骤

1. 修改 `plugin.go` 中的 `JenkinsPlugin` 结构体，添加 `sync.RWMutex`
2. 简化 `Init` 方法，移除客户端创建逻辑
3. 新增 `ensureClient` 方法实现延迟初始化
4. 修改 `resolveInstance` 为 `resolveInstanceID`（仅返回实例 ID）
5. 更新所有工具方法，使用 `ensureClient` 获取客户端

## 好处

1. **故障隔离**：单个实例连接失败只影响该实例的工具调用
2. **快速启动**：DMR 启动时不阻塞在 Jenkins 连接上
3. **按需连接**：只有实际使用的实例才会建立连接
4. **动态重连**：可以实现连接失败后的重新创建（可选扩展）

## 兼容性说明

- 工具接口保持不变
- 配置格式保持不变
- 错误信息会有变化（从 "init failed" 变为 "connect failed"）

## 参考代码

详见 [vsphere plugin plugin.go](https://github.com/seanly/dmr-plugin-vsphere/blob/main/plugin.go) 的 `ensureClient` 实现。
