# 共享常量与 Provider 迁移

共享协议值和默认参数统一通过 `github.com/kart-io/goagent/constants` 使用。原来位于 `interfaces`、`llm/constants`、`llm/common`、`llm`、`tools`、`store`、`parsers` 和 `core` 的460个公开转发常量已删除，`llm/constants` 包也已删除；没有兼容别名。内部清理期限也直接引用中央值。

`interfaces.Tool` 等领域接口、`llm.ToolCall` 及其方法仍在原包。只迁移共享常量和 Provider 类型，不改变这些领域对象的使用方式。

## 导入与名称

按旧符号所属包逐项迁移，不能只按名字替换。大部分常量在中央保持原名称，以下默认策略需要特别区分：

| 旧入口 | 新入口 |
|---|---|
| `llm/constants.DefaultMaxTokens` | `constants.DefaultProviderMaxTokens`（1000） |
| `llm.DefaultMaxTokens`、`llm/common.DefaultMaxTokens` | `constants.DefaultCompletionMaxTokens`（2048） |
| `tools.DefaultToolTimeout` | `constants.DefaultToolTimeoutSeconds`（整数秒） |
| `tools.MaxConcurrentToolCalls` | `constants.DefaultToolConcurrency` |
| `tools.MaxRetries` | `constants.DefaultToolRetries` |
| `core.DefaultAgentExecutionTimeout` 等8个默认超时 | `constants` 中同名常量 |

Client/Agent 的2000 token默认策略仍独立管理。`constants.DefaultToolTimeout` 是 `time.Duration`，与整数秒的 `DefaultToolTimeoutSeconds` 不可互换。不能因为字面值相同而合并不同协议字段、状态或默认策略。

```go
import shared "github.com/kart-io/goagent/constants"

method := shared.MethodGet
contentType := shared.ContentTypeJSON
providerTokens := shared.DefaultProviderMaxTokens
timeout := shared.DefaultToolTimeout
```

## Provider 的具名类型

`Provider` 的声明位置从 `llm/constants.Provider` 改为 `constants.Provider`，底层仍为 string。这会改变 Go 的具名类型身份，所有 `llm.Client` 实现、配置字段、`WithProvider` 参数及模拟客户端方法都须同步使用新类型。

```go
import shared "github.com/kart-io/goagent/constants"

func (c *CustomClient) Provider() shared.Provider {
    return shared.Provider("minimax")
}

provider := shared.ProviderOpenAI // 推断为 shared.Provider
label := string(provider)         // 传给 string 参数或赋给 string 变量
custom := shared.Provider("offline-demo")
```

8个内置枚举 `ProviderOpenAI`、`ProviderGemini`、`ProviderDeepSeek`、`ProviderOllama`、`ProviderSiliconFlow`、`ProviderKimi`、`ProviderCustom`、`ProviderAnthropic` 均显式声明为 `Provider`，字符串值不变。Provider 取值空间保持开放，自定义提供商不受枚举集合限制。

原中央枚举是 untyped string；现在直接使用中央枚举时，传给 string 参数、赋给 string 变量或与 string 变量比较，需要显式 `string(...)` 转换。原来使用旧包 typed 枚举的代码保持同样的类型约束，但必须更新导入路径和方法签名。

## 错误与协议字段

错误码、分类、错误提示、错误上下文字段和哨兵使用 `errors` 包，详见[错误处理迁移指南](ERROR_HANDLING_MIGRATION.md)。`FieldError`、`StreamEventError`、`StatusError` 和 HTTP 状态码是协议值，继续在 `constants` 中。

## 领域共享入口补齐

以下 50 项原声明和兼容别名均已移除。按包和符号迁移引用，同时删除无用导入；字段名、局部变量及同名领域常量不参与改名。下表覆盖公开入口和框架内私有默认值；私有项便于维护者追踪归属。

| 原入口 | 中央入口 |
|---|---|
| `core/middleware.defaultNumShards` | `constants.DefaultMiddlewareCacheShards` |
| `tools.defaultApprovalTimeout` | `constants.DefaultApprovalTimeout` |
| `builder.modelResendAttempts` | `constants.DefaultModelResendAttempts` |
| `builder.inboxWriteTimeout` | `constants.DefaultInboxWriteTimeout` |
| `builder.maxRetryAfter` | `constants.MaxModelRetryAfter` |
| `core/budget.DefaultMaxAgentCalls` | `constants.DefaultMaxAgentCalls` |
| `core/budget.Unlimited` | `constants.BudgetUnlimited` |
| `llm/providers.OllamaBaseURL` | `constants.OllamaBaseURL` |
| `tools.resumableAttempts` | `constants.DefaultResumableToolAttempts` |
| `core/durable.interruptAttempts` | `constants.DefaultInterruptAttempts` |
| `memory.DefaultMaxConversationLength` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |
| `memory.DefaultSearchLimit` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |
| `memory.DefaultSimilarityThreshold` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |
| `memory.DefaultEmbeddingDimension` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |
| `core/durable.inboxSeenLimit` | `constants.DurableInboxSeenLimit` |
| `tools/spill.previewRunes` | `constants.SpillPreviewRunes` |
| `tools/spill.defaultReadLimit` | `constants.DefaultSpillReadLimitLines` |
| `tools/spill.maxLineRunes` | `constants.SpillMaxLineRunes` |
| `core/durable.maxReasonRunes` | `constants.InterruptMaxReasonRunes` |
| `workflow.START` | `constants.WorkflowStart` |
| `workflow.END` | `constants.WorkflowEnd` |
| `multiagent.Finish` | `constants.MultiagentFinish` |
| `builder/contextpolicy.ReadToolName` | `constants.ToolReadContextResult` |
| `tools/spill.ToolReadSpill` | `constants.ToolReadSpill` |
| `core/durable.ContinuationKindInterrupt` | `constants.ContinuationKindInterrupt` |
| `core/durable.ReasonInvalidJSON` | `constants.InterruptReasonInvalidJSON` |
| `core/durable.ReasonValidatePanic` | `constants.InterruptReasonValidatePanic` |
| `core/durable.ReasonRejected` | `constants.InterruptReasonRejected` |
| `workflow.RejectApprovalMismatch` | `constants.WorkflowRejectApprovalMismatch` |
| `workflow.RejectBeforeApprove` | `constants.WorkflowRejectBeforeApprove` |
| `workflow.RejectResumeInput` | `constants.WorkflowRejectResumeInput` |
| `core/session.TypeTurnStart` | `constants.SessionEventTurnStart` |
| `core/session.TypeTurnEnd` | `constants.SessionEventTurnEnd` |
| `core/session.TypeSystemMessage` | `constants.SessionEventSystemMessage` |
| `core/session.TypeToolDefinitions` | `constants.SessionEventToolDefinitions` |
| `core/session.TypeUserMessage` | `constants.SessionEventUserMessage` |
| `core/session.TypeAssistantMessage` | `constants.SessionEventAssistantMessage` |
| `core/session.TypeToolResult` | `constants.SessionEventToolResult` |
| `core/session.TypeHistoryRewrite` | `constants.SessionEventHistoryRewrite` |
| `core/session.TypeRewind` | `constants.SessionEventRewind` |
| `core/session.TypeBoundarySnapshot` | `constants.SessionEventBoundarySnapshot` |
| `memory.VectorStoreTypeMemory` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |
| `memory.VectorStoreTypeChroma` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |
| `memory.VectorStoreTypePinecone` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |
| `memory.VectorStoreTypeWeaviate` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |
| `memory.EmbedderProviderOpenAI` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |
| `memory.EmbedderProviderLocal` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |
| `memory.EmbedderProviderMock` | 已删除（记忆系统重写，见 COMPATIBILITY_CLEANUP） |

上述常量保留原来的具名/无类型状态和值。时间值仍为 `time.Duration`，不能改成整数秒；`BudgetUnlimited` 仍用 `math.MaxInt` 随目标架构求值。`DefaultModelResendAttempts` 和 `DefaultInterruptAttempts` 会参与持久操作策略校验，默认数字没有变化；不需要改写既有日志。

续跑原因属于正常的等待协议，使用 `InterruptReason*`、`WorkflowReject*` 和 `ContinuationKindInterrupt`，不作为业务错误码迁入 `errors`。会话事件数据结构、图构造器、预算控制器及具名领域枚举保持在原包。

同一消费路径的内联默认也集中管理：Kimi/SiliconFlow/Ollama 模型、Ollama 首块超时、记忆嵌入模型、RetryTransient 的 500ms/8s 退避、工具缓存默认及独立预设、检查点健康检查和切换次数。工具缓存性能配置与负载配置的 CPU 倍数分别命名，不与中间件缓存或其他当前同值策略合并。

`VectorStoreTypeChroma`、`VectorStoreTypePinecone`、`VectorStoreTypeWeaviate`、`EmbedderProviderLocal`、`EmbedderProviderMock` 只是保留的配置词汇；常量存在不表示后端已实现。持久格式版本、算法约束、领域状态机枚举及独立 codec/env 边界仍由原包维护。
