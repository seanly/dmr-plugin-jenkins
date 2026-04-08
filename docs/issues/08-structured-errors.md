# 08 - 结构化错误处理（可选）

## 问题描述

当前错误处理只返回字符串：

```go
func (p *JenkinsPlugin) CallTool(req *proto.CallToolRequest, resp *proto.CallToolResponse) error {
    // ...
    if err != nil {
        resp.Error = err.Error()  // 只传递字符串，丢失所有类型信息
        return nil
    }
    // ...
}
```

这导致：
1. DMR 无法根据错误类型做智能决策（重试、放弃、提示用户）
2. 错误信息不统一
3. 难以程序化地处理特定错误

## 目标

1. **分类错误**：区分网络、认证、权限、服务器错误
2. **支持重试策略**：让 DMR 知道哪些错误可以重试
3. **用户友好的消息**：提供清晰的操作建议
4. **保留原始信息**：便于调试

## 错误分类体系

```go
// JenkinsError 是 Jenkins 插件的基错误类型
type JenkinsError struct {
    Code    ErrorCode // 错误分类
    Message string    // 用户友好的消息
    Cause   error     // 原始错误（可选）
    Details map[string]any // 附加信息
}

func (e *JenkinsError) Error() string {
    if e.Cause != nil {
        return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
    }
    return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *JenkinsError) Unwrap() error {
    return e.Cause
}

// ErrorCode 定义错误分类
type ErrorCode string

const (
    // 网络层错误
    ErrNetwork       ErrorCode = "NETWORK"       // 连接失败、超时
    ErrTimeout       ErrorCode = "TIMEOUT"       // 请求超时
    ErrDNS           ErrorCode = "DNS"           // 域名解析失败
    
    // 认证/授权错误
    ErrAuth          ErrorCode = "AUTH"          // 认证失败（401）
    ErrForbidden     ErrorCode = "FORBIDDEN"     // 权限不足（403）
    
    // 资源错误
    ErrNotFound      ErrorCode = "NOT_FOUND"     // Job/Build 不存在（404）
    ErrAlreadyExists ErrorCode = "ALREADY_EXISTS" // 资源已存在（409）
    
    // 服务器错误
    ErrServer        ErrorCode = "SERVER"        // 服务器内部错误（5xx）
    ErrUnavailable   ErrorCode = "UNAVAILABLE"   // 服务不可用（503）
    
    // 客户端错误
    ErrInvalidInput  ErrorCode = "INVALID_INPUT" // 参数错误
    ErrInvalidConfig ErrorCode = "INVALID_CONFIG" // 配置错误
    
    // Jenkins 特有错误
    ErrCrumb         ErrorCode = "CRUMB"         // CSRF crumb 失效
    ErrQueueFull     ErrorCode = "QUEUE_FULL"    // 构建队列已满
)

// IsRetryable 判断错误是否值得重试
func (e *JenkinsError) IsRetryable() bool {
    switch e.Code {
    case ErrNetwork, ErrTimeout, ErrServer, ErrUnavailable, ErrCrumb:
        return true
    default:
        return false
    }
}

// RetryAfter 建议的重试等待时间
func (e *JenkinsError) RetryAfter() time.Duration {
    switch e.Code {
    case ErrTimeout:
        return 5 * time.Second
    case ErrServer:
        return 10 * time.Second
    case ErrUnavailable:
        return 30 * time.Second
    default:
        return 1 * time.Second
    }
}
```

## 错误转换函数

```go
// wrapHTTPError 将 HTTP 错误转换为结构化错误
func wrapHTTPError(status int, body []byte, endpoint string) error {
    bodyStr := truncateBody(body)
    
    switch status {
    case http.StatusUnauthorized:
        return &JenkinsError{
            Code:    ErrAuth,
            Message: fmt.Sprintf("认证失败，请检查用户名和 API Token（端点: %s）", endpoint),
            Details: map[string]any{
                "status":   status,
                "endpoint": endpoint,
            },
        }
        
    case http.StatusForbidden:
        // 检查是否是 Crumb 错误
        if looksLikeInvalidCrumb(body) {
            return &JenkinsError{
                Code:    ErrCrumb,
                Message: "CSRF Crumb 已过期，将自动重试",
                Details: map[string]any{
                    "status":   status,
                    "endpoint": endpoint,
                },
            }
        }
        return &JenkinsError{
            Code:    ErrForbidden,
            Message: fmt.Sprintf("权限不足，无法访问 %s", endpoint),
            Details: map[string]any{
                "status":   status,
                "endpoint": endpoint,
            },
        }
        
    case http.StatusNotFound:
        return &JenkinsError{
            Code:    ErrNotFound,
            Message: fmt.Sprintf("资源不存在: %s", endpoint),
            Details: map[string]any{
                "status":   status,
                "endpoint": endpoint,
            },
        }
        
    case http.StatusConflict:
        return &JenkinsError{
            Code:    ErrAlreadyExists,
            Message: fmt.Sprintf("资源冲突: %s", endpoint),
            Details: map[string]any{
                "status":   status,
                "endpoint": endpoint,
            },
        }
        
    case http.StatusServiceUnavailable:
        return &JenkinsError{
            Code:    ErrUnavailable,
            Message: "Jenkins 服务暂时不可用，请稍后重试",
            Details: map[string]any{
                "status":   status,
                "endpoint": endpoint,
            },
        }
    }
    
    if status >= 500 {
        return &JenkinsError{
            Code:    ErrServer,
            Message: fmt.Sprintf("Jenkins 服务器错误 (%d): %s", status, bodyStr),
            Details: map[string]any{
                "status":   status,
                "endpoint": endpoint,
                "body":     bodyStr,
            },
        }
    }
    
    return &JenkinsError{
        Code:    ErrNetwork,
        Message: fmt.Sprintf("HTTP %d: %s", status, bodyStr),
        Details: map[string]any{
            "status":   status,
            "endpoint": endpoint,
            "body":     bodyStr,
        },
    }
}

// wrapNetworkError 包装网络错误
func wrapNetworkError(err error, operation string) error {
    if urlErr, ok := err.(*url.Error); ok {
        if urlErr.Timeout() {
            return &JenkinsError{
                Code:    ErrTimeout,
                Message: fmt.Sprintf("请求超时: %s", operation),
                Cause:   err,
                Details: map[string]any{
                    "operation": operation,
                },
            }
        }
    }
    
    if netErr, ok := err.(net.Error); ok {
        if netErr.Temporary() || netErr.Timeout() {
            return &JenkinsError{
                Code:    ErrNetwork,
                Message: fmt.Sprintf("临时网络错误: %s", operation),
                Cause:   err,
                Details: map[string]any{
                    "operation": operation,
                    "temporary": true,
                },
            }
        }
    }
    
    return &JenkinsError{
        Code:    ErrNetwork,
        Message: fmt.Sprintf("网络错误: %s", operation),
        Cause:   err,
        Details: map[string]any{
            "operation": operation,
        },
    }
}
```

## 自动重试逻辑

```go
// executeWithRetry 带重试的执行
func (c *jenkinsClient) executeWithRetry(ctx context.Context, operation string, fn func() error) error {
    maxRetries := 3
    
    for i := 0; i < maxRetries; i++ {
        err := fn()
        if err == nil {
            return nil
        }
        
        jErr, ok := err.(*JenkinsError)
        if !ok || !jErr.IsRetryable() {
            return err // 不可重试的错误
        }
        
        if i < maxRetries-1 {
            retryAfter := jErr.RetryAfter()
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(retryAfter):
                // 继续重试
            }
        }
    }
    
    return err
}
```

## Crumb 自动刷新

```go
func (c *jenkinsClient) postWithCrumbRetry(ctx context.Context, path string, contentType string, body []byte) (int, []byte, error) {
    doPost := func() (int, []byte, error) {
        // ... 实际 POST 逻辑
    }
    
    status, respBody, err := doPost()
    if err != nil {
        return 0, nil, err
    }
    
    if status == http.StatusForbidden {
        if jErr, ok := wrapHTTPError(status, respBody, path).(*JenkinsError); ok && jErr.Code == ErrCrumb {
            // 清除缓存的 crumb
            c.clearCrumb()
            
            // 重试一次
            return doPost()
        }
    }
    
    return status, respBody, nil
}
```

## 工具响应中的错误格式化

```go
func (p *JenkinsPlugin) CallTool(req *proto.CallToolRequest, resp *proto.CallToolResponse) error {
    // ...
    result, err := p.executeTool(req.Name, args)
    if err != nil {
        resp.Error = formatErrorForLLM(err)
        return nil
    }
    // ...
}

// formatErrorForLLM 将错误格式化为 LLM 友好的消息
func formatErrorForLLM(err error) string {
    if jErr, ok := err.(*JenkinsError); ok {
        msg := jErr.Message
        
        // 添加操作建议
        switch jErr.Code {
        case ErrAuth:
            msg += "\n\n建议: 请检查插件配置中的 username 和 api_token 是否正确"
        case ErrNotFound:
            if job, ok := jErr.Details["job"].(string); ok {
                msg += fmt.Sprintf("\n\n建议: 请确认 Job '%s' 是否存在，注意使用完整路径（如 folder/job）", job)
            }
        case ErrQueueFull:
            msg += "\n\n建议: Jenkins 构建队列已满，请稍后重试或联系管理员清理队列"
        }
        
        return msg
    }
    
    return err.Error()
}
```

## 实施步骤

1. 创建 `errors.go` 定义错误类型和分类
2. 创建 `error_wrapper.go` 实现错误转换函数
3. 修改 `client.go`，在 HTTP 调用中使用结构化错误
4. 修改 `CallTool`，在返回错误前格式化

## 好处

1. **智能重试**：自动重试临时错误（网络、超时、503）
2. **清晰反馈**：用户知道是配置错误、权限问题还是服务器问题
3. **调试友好**：保留原始错误和上下文信息
4. **LLM 友好**：错误消息包含可操作的建议

## 优先级

此优化属于 **P2（可选）**，建议在完成基础架构改进（Context、延迟初始化、接口抽象）后再实施。
