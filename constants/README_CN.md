# 共享常量

[English](README.md) | [中文](README_CN.md)

`constants/` 是共享协议常量、默认值与 `Provider` 类型的唯一公开入口，按领域分文件；调用方统一导入 `github.com/kart-io/goagent/constants`。

| 文件 | 管理内容 |
|---|---|
| `common.go` | 消息角色 |
| `fields.go` | 公共字段名 |
| `http.go` | HTTP 方法、头和内容类型 |
| `llm.go` | 提供商标识、环境变量、模型参数和协议字段 |
| `tools.go` | 工具名称、描述与执行默认值 |
| `parsers.go` | 解析字段、标记和模式 |
| `defaults.go` | 共享超时、清理期限及各调用层默认策略 |
| `cache.go` | 中间件缓存默认值 |
| `execution.go` | 模型重发、收件箱写入和暂时性请求重试策略 |
| `budget.go` | Agent 拓扑预算默认值与架构相关无限余量 |
| `durable.go` | 中断续跑协议、原因及重入/去重边界 |
| `session.go` | 会话事件类型；事件数据结构仍由 `core/session` 定义 |
| `workflow.go` | 图起止标记、多 Agent 完成标记和续跑拒绝原因 |
| `memory.go` | 记忆默认配置与长期记忆工具名 |
| `revisions.go` | Agent 修订归档的大小与数量上限 |
| `management.go` | management 必需限额默认值与适配器入站请求上限 |
| `skills.go` | `load_skill` 工具名与技能资源上限 |
| `doc.go` | 包文档 |

```go
import shared "github.com/kart-io/goagent/constants"

requestTimeout := shared.DefaultToolTimeout // time.Duration
role := shared.RoleAssistant
```

错误码、错误分类、错误提示及错误上下文字段统一放在 [`errors/`](../errors/README_CN.md)，按 `codes.go`、`sentinels.go`、`llm.go` 管理。HTTP 方法、头等协议约定保留在本目录。

原 `interfaces`、`llm/constants`、`llm/common`、`llm`、`tools`、`store`、`parsers`、`core` 的共享常量转发入口已删除；`llm/constants` 包也已删除，不保留兼容别名。调用方直接使用本包，错误入口统一使用 `errors`。

`Provider` 是本包定义的 string 底层具名类型，8 个内置提供商枚举都显式使用该类型。自定义提供商继续使用 `shared.Provider("minimax")`；字符串接口需要 `string(provider)`。Client 实现的 `Provider()` 返回类型须同步迁移，详见[常量迁移指南](../docs/guides/CONSTANTS_MIGRATION.md)。

管理规则：

- 本包是稳定包（见 [API 稳定性政策](../docs/API_STABILITY_CN.md)），删除或改值须走弃用期。
- 只依赖标准库，禁止导入框架实现或第三方 SDK；由 `scripts/apicheck` 检查。
- 共享协议值、公开扩展约定和默认策略放在本目录。模块专用枚举、状态机内部标记、持久格式版本与测试夹具留在所属包。
- 数字相同不代表语义相同。旧补全接口 `DefaultCompletionMaxTokens=2048` 与 Anthropic 必填 `max_tokens` 的默认 `DefaultClientMaxTokens=2000` 分别管理；`DefaultProviderMaxTokens=1000` 只为旧值保留，provider 不再套用。限额类常量（如 `DefaultAgentMaxIterations`、`DefaultDispatcherConcurrency`、`DefaultMaxAgentCalls`）记录 v0.60 之前的默认值，框架不再自动填入，调用方需要时显式传。
- 显式区分单位：`DefaultToolTimeoutSeconds` 是整数秒，`DefaultToolTimeout` 是 `time.Duration`，原接口类型不变。
- 不为每个字面量创建通用常量，也不按相同字符串强行合并不同领域概念。迁移验证保留精确值、单位与调用行为，并单独检查 Provider 具名类型位置的有意变化。
- `BudgetUnlimited` 保留 `math.MaxInt`，随目标架构确定上限；不得固化为 64 位十进制字面量。`DefaultApprovalTimeout`、`DefaultInboxWriteTimeout`、`MaxModelRetryAfter` 等仍为 `time.Duration`。
- `DefaultTransientRetryBaseDelay=500ms`、`DefaultTransientRetryMaxDelay=8s` 专属于 `builder.RetryTransient`；提供商层 `DefaultBaseDelay=1s`、`DefaultMaxDelay=30s` 是另一策略。
- 不预留没有调用方的常量：新增常量须同时有引用。
