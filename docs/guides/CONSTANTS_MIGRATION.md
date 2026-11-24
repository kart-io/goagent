# Shared Constants and Provider Migration

[English](CONSTANTS_MIGRATION.md) | [中文](CONSTANTS_MIGRATION_CN.md)

Shared protocol values and default parameters are all used through `github.com/kart-io/goagent/constants`. The 460 public forwarding constants that used to live in `interfaces`, `llm/constants`, `llm/common`, `llm`, `tools`, `store`, `parsers`, and `core` have been deleted, and the `llm/constants` package has been deleted as well; there are no compatibility aliases. Internal cleanup deadlines also reference the central values directly.

Domain interfaces such as `interfaces.Tool`, as well as `llm.ToolCall` and its methods, remain in their original packages. Only shared constants and the Provider type are migrated; how these domain objects are used does not change.

## Imports and Names

Migrate each old symbol according to the package it belonged to; do not replace by name alone. Most constants keep their original names in the central package, but the following default policies need to be distinguished:

| Old entry | New entry |
|---|---|
| `llm/constants.DefaultMaxTokens` | `constants.DefaultProviderMaxTokens` (1000) |
| `llm.DefaultMaxTokens`, `llm/common.DefaultMaxTokens` | `constants.DefaultCompletionMaxTokens` (2048) |
| `tools.DefaultToolTimeout` | `constants.DefaultToolTimeoutSeconds` (integer seconds) |
| `tools.MaxConcurrentToolCalls` | `constants.DefaultToolConcurrency` |
| `tools.MaxRetries` | `constants.DefaultToolRetries` |
| `core.DefaultAgentExecutionTimeout` and the other 8 default timeouts | Constants of the same name in `constants` |

The 2000-token default policy of the Client/Agent is still managed separately. `constants.DefaultToolTimeout` is a `time.Duration` and is not interchangeable with the integer-seconds `DefaultToolTimeoutSeconds`. Do not merge different protocol fields, states, or default policies just because their literal values are the same.

```go
import shared "github.com/kart-io/goagent/constants"

method := shared.MethodGet
contentType := shared.ContentTypeJSON
providerTokens := shared.DefaultProviderMaxTokens
timeout := shared.DefaultToolTimeout
```

## The Named Provider Type

`Provider` is now declared as `constants.Provider` instead of `llm/constants.Provider`; its underlying type is still string. This changes the named type identity in Go, so every `llm.Client` implementation, configuration field, `WithProvider` argument, and mock client method must switch to the new type together.

```go
import shared "github.com/kart-io/goagent/constants"

func (c *CustomClient) Provider() shared.Provider {
    return shared.Provider("minimax")
}

provider := shared.ProviderOpenAI // inferred as shared.Provider
label := string(provider)         // pass to a string parameter or assign to a string variable
custom := shared.Provider("offline-demo")
```

The 9 built-in enums `ProviderOpenAI`, `ProviderGemini`, `ProviderDeepSeek`, `ProviderOllama`, `ProviderSiliconFlow`, `ProviderKimi`, `ProviderMiniMax`, `ProviderCustom`, and `ProviderAnthropic` are all explicitly declared as `Provider`, with unchanged string values. The Provider value space stays open; custom providers are not limited to the enum set.

The original central enums were untyped strings. When you now use the central enums directly, passing them to a string parameter, assigning them to a string variable, or comparing them with a string variable requires an explicit `string(...)` conversion. Code that used the typed enums of the old package keeps the same type constraints, but must update its import paths and method signatures.

## Errors and Protocol Fields

Error codes, categories, error hints, error context fields, and sentinels use the `errors` package; see the [Error Handling Migration Guide](ERROR_HANDLING_MIGRATION.md). Field names and HTTP protocol values such as `FieldStatus` and `MethodGet` remain in `constants`.

## Filling in Shared Domain Entries

The 47 original declarations and compatibility aliases below have all been removed. Migrate references by package and symbol, and remove unused imports at the same time; field names, local variables, and domain constants with the same name are not renamed. The table covers public entries and private defaults inside the framework; the private items help maintainers track ownership.

| Original entry | Central entry |
|---|---|
| `tools.defaultApprovalTimeout` | `constants.DefaultApprovalTimeout` |
| `builder.modelResendAttempts` | `constants.DefaultModelResendAttempts` |
| `builder.inboxWriteTimeout` | `constants.DefaultInboxWriteTimeout` |
| `builder.maxRetryAfter` | `constants.MaxModelRetryAfter` |
| `core/budget.DefaultMaxAgentCalls` | `constants.DefaultMaxAgentCalls` |
| `core/budget.Unlimited` | `constants.BudgetUnlimited` |
| `llm/providers.OllamaBaseURL` | `constants.OllamaBaseURL` |
| `tools.resumableAttempts` | `constants.DefaultResumableToolAttempts` |
| `core/durable.interruptAttempts` | `constants.DefaultInterruptAttempts` |
| `memory.DefaultMaxConversationLength` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |
| `memory.DefaultSearchLimit` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |
| `memory.DefaultSimilarityThreshold` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |
| `memory.DefaultEmbeddingDimension` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |
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
| `memory.VectorStoreTypeMemory` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |
| `memory.VectorStoreTypeChroma` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |
| `memory.VectorStoreTypePinecone` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |
| `memory.VectorStoreTypeWeaviate` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |
| `memory.EmbedderProviderOpenAI` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |
| `memory.EmbedderProviderLocal` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |
| `memory.EmbedderProviderMock` | Deleted (memory system rewritten, see COMPATIBILITY_CLEANUP) |

The constants above keep their original named/untyped status and values. Time values are still `time.Duration` and must not be changed to integer seconds; `BudgetUnlimited` still uses `math.MaxInt`, evaluated for the target architecture. `DefaultModelResendAttempts` and `DefaultInterruptAttempts` take part in durable operation policy validation, and their default numbers have not changed; existing logs do not need to be rewritten.

Continuation reasons belong to the normal waiting protocol. They use `InterruptReason*`, `WorkflowReject*`, and `ContinuationKindInterrupt`, and are not moved into `errors` as business error codes. Session event data structures, the graph builder, the budget controller, and named domain enums stay in their original packages.

Inline defaults on the same consumption path are also centralized: the Kimi/SiliconFlow/Ollama models, the Ollama timeout, the 500ms/8s backoff of RetryTransient, and the default capacity of the tool caching middleware. They are not merged with other policies that currently have the same values.

Durable format versions, algorithm constraints, domain state machine enums, and standalone codec/env boundaries are still maintained by their original packages.
