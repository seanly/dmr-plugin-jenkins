# Jenkins 插件重构方案概述

## 背景

通过对比 vSphere 插件的设计，发现 Jenkins 插件在架构层面存在多个可优化点。本方案系列旨在提升插件的可维护性、可测试性和稳定性。

## 核心问题

| 问题 | 当前状态 | 目标状态 |
|------|----------|----------|
| 初始化时机 | Init 时创建所有客户端 | 延迟初始化，首次调用时连接 |
| 客户端抽象 | 直接依赖具体类型 | 定义接口，支持装饰器模式 |
| 超时控制 | 使用 `context.Background()` | 可配置的超时控制 |
| 类型安全 | 使用 `map[string]any` | 强类型化的请求/响应结构 |
| 默认值 | 硬编码散布各处 | 集中配置，用户可覆盖 |
| 缓存 | 无 | 可选的缓存层 |

## 重构里程碑

```
Phase 1: 基础架构改进（稳定性）
├── 01-context-timeout.md      # Context 超时控制
├── 02-lazy-init.md            # 延迟初始化
└── 03-client-interface.md     # 客户端接口抽象

Phase 2: 代码质量提升（可维护性）
├── 04-strong-types.md         # 强类型化
├── 05-config-defaults.md      # 集中配置默认值
└── 06-file-organization.md    # 文件组织优化

Phase 3: 性能优化（可选）
├── 07-caching-layer.md        # 缓存层设计
└── 08-structured-errors.md    # 结构化错误处理
```

## 设计原则

1. **向后兼容**：所有变更保持工具接口不变
2. **渐进式重构**：每个阶段可独立实施
3. **测试优先**：接口抽象后应易于 mock 测试
4. **配置驱动**：新增功能都可通过配置开关

## 参考实现

- [dmr-plugin-vsphere](https://github.com/seanly/dmr-plugin-vsphere) - 客户端接口、缓存层、延迟初始化
- [dmr-plugin-gitlab](https://github.com/seanly/dmr-plugin-gitlab) - 多实例管理、错误处理
