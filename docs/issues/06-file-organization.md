# 06 - 文件组织优化

## 现行目录结构（已实现）

采用 Go 惯例 **`cmd/` + `internal/`**：

```
dmr-plugin-jenkins/
├── cmd/dmr-plugin-jenkins/main.go   # HashiCorp go-plugin 入口
├── internal/jenkins/                # package jenkins：插件、工具、HTTP 客户端
├── policies/
├── docs/
├── go.mod
├── go.work.example                  # 本地复制为 go.work（已 .gitignore）
└── Makefile
```

以下「当前问题 / 目标结构」为历史拆分设想与迁移参考，不代表当前源码仍集中为单文件 `tools.go`。

## 当前问题

当前所有工具实现集中在 `tools.go`（587 行），随着功能增加会越来越难维护：

```
dmr-plugin-jenkins/
├── client.go          # HTTP 客户端 (247 行)
├── config.go          # 配置 (84 行)
├── paths.go           # URL 工具 (33 行)
├── plugin.go          # 插件结构、Init、工具路由 (193 行)
└── tools.go           # 所有工具实现 (587 行！)
```

## 目标结构

按功能模块组织，参考 vSphere 插件：

```
dmr-plugin-jenkins/
├── client/
│   ├── interface.go       # JenkinsClient 接口定义
│   ├── http_client.go     # HTTP 客户端实现
│   └── cached_client.go   # 缓存装饰器（可选）
├── types/
│   └── types.go           # 请求/响应结构定义
├── tools/
│   ├── instances.go       # jenkinsInstances
│   ├── jobs.go            # jenkinsGetJob
│   ├── builds.go          # jenkinsGetBuild, jenkinsListBuilds
│   ├── trigger.go         # jenkinsTriggerBuild
│   └── console.go         # jenkinsGetConsoleText
├── config.go              # 配置结构
├── parser.go              # 参数解析辅助函数
├── paths.go               # URL 工具函数
├── plugin.go              # 插件主结构、Init、路由
└── main.go                # 入口
```

## 迁移方案

### Phase 1：提取类型定义（低风险）

创建 `types.go`：

```go
package main

// 从 tools.go 中提取的所有请求/响应结构
// GetJobRequest, GetJobResponse, ListBuildsRequest, ...
```

### Phase 2：拆分工具文件（中风险）

#### `tools/instances.go`
```go
package main

func (p *JenkinsPlugin) toolInstances() (*InstancesResponse, error) {
    // jenkinsInstances 实现
}
```

#### `tools/jobs.go`
```go
package main

func (p *JenkinsPlugin) toolGetJob(ctx context.Context, client JenkinsClient, args map[string]any) (*GetJobResponse, error) {
    // jenkinsGetJob 实现
}
```

#### `tools/builds.go`
```go
package main

func (p *JenkinsPlugin) toolListBuilds(ctx context.Context, client JenkinsClient, args map[string]any) (*ListBuildsResponse, error) {
    // jenkinsListBuilds 实现（包含 Job 模式和全局模式）
}

func (p *JenkinsPlugin) toolGetBuild(ctx context.Context, client JenkinsClient, args map[string]any) (*GetBuildResponse, error) {
    // jenkinsGetBuild 实现
}
```

#### `tools/trigger.go`
```go
package main

func (p *JenkinsPlugin) toolTriggerBuild(ctx context.Context, client JenkinsClient, args map[string]any) (*TriggerBuildResponse, error) {
    // jenkinsTriggerBuild 实现
}
```

#### `tools/console.go`
```go
package main

func (p *JenkinsPlugin) toolGetConsoleText(ctx context.Context, client JenkinsClient, args map[string]any) (*GetConsoleTextResponse, error) {
    // jenkinsGetConsoleText 实现
}
```

### Phase 3：提取参数解析器（低风险）

创建 `parser.go`：

```go
package main

type RequestParser struct {
    args map[string]any
}

func NewRequestParser(args map[string]any) *RequestParser
func (p *RequestParser) String(key string) string
func (p *RequestParser) Int(key string) int
func (p *RequestParser) Bool(key string, defaultVal bool) bool
```

### Phase 4：客户端接口抽象（高风险，见 03-client-interface.md）

## 文件命名约定

遵循 DMR 工具命名规范：

| 文件 | 说明 |
|------|------|
| `*_test.go` | 对应文件的测试 |
| `mock_*.go` | Mock 实现（用于测试） |

## 代码统计对比

重构前：
```
client.go          247 行
config.go           84 行
paths.go            33 行
plugin.go          193 行
tools.go           587 行
-------------------------
总计              1144 行
```

重构后：
```
client/
  interface.go      30 行
  http_client.go   220 行
  cached_client.go  80 行（可选）
tools/
  instances.go      30 行
  jobs.go           60 行
  builds.go        180 行
  trigger.go        60 行
  console.go        60 行
config.go           90 行
parser.go           50 行
paths.go            35 行
plugin.go          120 行
types.go           120 行
-------------------------
总计              1135 行（相当）
```

虽然总行数相当，但：
- 每个文件职责单一，易于理解
- 修改一个工具不会影响其他工具
- 新开发者可以快速定位相关代码

## 实施建议

### 选项 A：激进重构（推荐新项目）

一次性完成所有文件拆分。

**风险**：
- 大量文件变更
- git 历史追踪困难

### 选项 B：渐进重构（推荐现有项目）

1. 保持现有文件结构
2. 新增功能时创建单独文件
3. 逐步将旧代码迁移到新文件
4. 最终删除 `tools.go`

**步骤**：
1. 创建 `types.go`，移动所有结构定义
2. 创建 `parser.go`，提取参数解析逻辑
3. 创建 `jobs.go`，将 `jenkinsGetJob` 移入
4. 创建 `builds.go`，将 build 相关工具移入
5. 重复直到 `tools.go` 为空
6. 删除 `tools.go`

## 包结构考虑

当前所有代码都在 `main` 包，这是 go-plugin 的推荐做法。

如果需要更严格的隔离，可以考虑内部包：

```
dmr-plugin-jenkins/
├── internal/
│   ├── client/         # 内部客户端实现
│   ├── tools/          # 内部工具实现
│   └── types/          # 内部类型定义
├── plugin.go           # main 包，导出插件
└── main.go             # 入口
```

但这样会增加复杂性，建议保持简单结构。

## 参考实现

[vsphere plugin 文件结构](https://github.com/seanly/dmr-plugin-vsphere/tree/main)：

```
plugin.go        # 工具路由（160 行）
types.go         # 类型定义（270 行）
client.go        # 客户端接口+实现（260 行）
filter.go        # 过滤逻辑（64 行）
formatter.go     # 格式化工具（82 行）
overview.go      # overview 工具（58 行）
hosts.go         # host 工具（247 行）
vms.go           # VM 工具（304 行）
```

每个文件职责清晰，易于维护。
