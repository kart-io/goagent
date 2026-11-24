# API 稳定性政策

[English](API_STABILITY.md) | [中文](API_STABILITY_CN.md)

GoAgent 正在走向 v1.0。本文说明哪些包有兼容承诺、v1.0 前后如何处理破坏性变更，以及如何强制执行。

## 稳定包

[`scripts/stable-packages.txt`](../scripts/stable-packages.txt) 所列的包为**稳定包**，以该文件为唯一来源。概括来说覆盖执行主路径：

- `builder`、`builder/contextpolicy`
- `core` 及其执行子包（`durable`、`budget`、`session`、`observe`、`middleware`、`state`、`usage`）
- `workflow`
- `llm`、`llm/common`、`llm/providers`
- `tools` 及其治理子包（`policy`、`spill`、`toolargs`、`middleware`）
- `interfaces`、`errors`、`execution`、`mcp/remote`、`constants`

清单按导入路径精确匹配，子包只有自己被列出时才算稳定。

## 实验性包

其余公开包均为**实验性**，包括 `memory`、`management`、`multiagent`、`a2a`、`harness`、`cache`、`codec`、`parsers`、`store`、`skills`、`plugin`、`revision`、`observability`、`middleware`、`utils`、`testing`、`core/flow`、`tools/` 下的内置工具实现（`compute`、`http`、`search`、`shell`、`skill`）以及 `workflow/refund`。

实验性包可在任意次版本中变化，破坏性变更仍须在 [CHANGELOG.md](../CHANGELOG.md) 中写明迁移步骤。

## 稳定包的规则

**v1.0 之前**

1. 导出 API 不能一步删除或不兼容地修改。先标注 `// Deprecated: use X instead.`，并**至少保留一个次版本**后再删除。
2. 确实无法走弃用期的破坏性变更（如安全修复），PR 须加 `breaking-change` 标签，并在 CHANGELOG“破坏性变更”下写迁移步骤。

**v1.0 起**

1. v1 内稳定包的导出 API 不做不兼容变更，删除只在新主版本（`/v2`）中进行。
2. 已弃用的 API 保留到下一个主版本。

**始终适用**

- 给导出接口新增方法会破坏实现方，按同样规则处理。
- 提高 `go.mod` 的最低 Go 版本只在次版本中进行，并写入 CHANGELOG。
- 未导出代码、`internal/` 包、示例与测试不在承诺范围内。

## 传递暴露

稳定包的导出 API 有时会用到实验包的类型，例如 `builder.WithLongTermMemory`（`*memory.Memory`）、`llm/common.BaseProvider.NewHTTPClient`（`*httpclient.Client`）。调用方经稳定包依赖到的部分同样受稳定承诺约束，分三种模式：

| 模式 | 适用 | 冻结内容 |
|---|---|---|
| `full` | 调用方实现或赋值的接口、函数类型，以及调用方构造的具名类型 | 底层类型与方法集 |
| `identity` | 原样透传的不透明句柄，如由 `memory.New` 构造的 `*memory.Memory` | 只保证类型名与所在包不变，方法集随实验包演进 |
| `alias` | 类型别名，如 `codec/json.RawMessage` | 别名指向，永不改变 |

所有此类暴露都登记在 [`scripts/api-exposure.txt`](../scripts/api-exposure.txt)。新增暴露未登记、或已登记条目不再被暴露，检查都会失败。

## 强制执行

`scripts/check_api_compat.sh` 用 Go 团队的 [`apidiff`](https://pkg.go.dev/golang.org/x/exp/cmd/apidiff) 比较工作区与最近发布 tag 的导出 API，稳定包出现不兼容变更即失败。每个 PR 都会运行；PR 带 `breaking-change` 标签时只报告不失败。

```bash
bash scripts/check_api_compat.sh            # 与最近的 tag 比较
bash scripts/check_api_compat.sh v0.55.0    # 与指定版本比较
```

`apidiff` 逐包比较，看不到稳定签名所引用的实验接口新增方法、实验函数类型改签名、别名改指向，以及标准库类型之间的互换。因此同一脚本还会运行 `scripts/exposurecheck`，按上述模式比较两个版本，把这些变化报为不兼容。

## v1.0 路线

同时满足以下条件时发布 v1.0：

- [ ] 连续两个次版本对稳定包没有不兼容变更。
- [ ] 稳定包的包注释与导出标识符有英文 godoc。
- [ ] 面向用户的指南有英文版。
- [ ] 稳定包覆盖率持续满足 `scripts/check_coverage.sh` 的门槛。
