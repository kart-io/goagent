# Migrating Legacy Entry Points and Ineffective Configuration

[English](COMPATIBILITY_CLEANUP.md) | [中文](COMPATIBILITY_CLEANUP_CN.md)

This round removes compatibility names, the old streaming protocol, the old tool middleware hooks, and configuration with no execution path, without keeping forwarding aliases. Callers must migrate to the interfaces that actually execute; compile failures or strict config decoding failures serve to expose calls that have not yet been migrated.

## Tool Calls and Streaming Execution

`tools.ToolCall` has been removed; tool calls go through the Dispatcher's `tools.ToolRequest`. `llm.ToolCall`, `builder.ToolCall`, and `interfaces.ToolCall` are different business objects and are not in the scope of this removal.

The types, constants, and factory variables in `core/streaming_alias.go`, as well as the old `StreamingAgent/StreamOutput/LegacyStreamChunk` system in `core/execution`, have been removed. Streaming execution uses `core.Runnable[I,O].Stream` or `core.StreamRunnable[I,O]` and consumes `Data/Error/Done/Event` of `core.StreamChunk[O]`. Callers should distinguish between completion of a single call and completion of a business process; a closed channel must not simply be treated as a durable commit or business success. See the [streaming execution notes](GROUNDED_RAG_AND_STREAMING.md).

This is not a matter of replacing `StreamOutput.Next()` with a wrapper of the same name; old streaming implementations must connect to the current generic execution source, and two sets of chunk types and factory entry points are no longer exposed.

## Strongly Typed Tool Middleware

The assembly entry point is fixed as:

```go
func Wrap(tool interfaces.Tool, middlewares ...ToolMiddlewareFunc) interfaces.Tool // package tools/middleware
```

The first function in the list is the outermost. The old `ToolMiddleware/BaseToolMiddleware/Chain` lifecycle has been removed; logging, caching, and rate limiting all wrap the invoker through functions:

```go
wrapped := middleware.Wrap(tool,
    middleware.Logging(),
    middleware.Caching(),
    middleware.RateLimit(middleware.WithQPS(10)),
)
```

To keep a stateful object and read its capabilities, pass that object's `Wrap` method explicitly:

```go
logging := middleware.NewLoggingMiddleware()
wrapped := middleware.Wrap(tool, logging.Wrap)
```

You can no longer pass arbitrary objects directly to `middleware.Wrap`, nor reconnect the old hooks through runtime type checks. Assembly with a nil middleware function or one that returns a nil invoker fails with a configuration error. Logging timing belongs to each call and is not written into the caller's `ToolInput.Args`; partial results returned by the underlying tool are kept together with the error. Tool wrapping does not thereby automatically gain the underlying optional durable capabilities; for the exact boundaries see the [Tool Middleware guide](TOOL_MIDDLEWARE.md).

## Runtime and Retries

`execution.RuntimeConfig`, `DefaultRuntimeConfig`, `RuntimeMetrics`, `NewRuntimeMetrics`, and `RuntimeManager.Metrics()` have been removed. The former auto-save, save interval, size limit, and metrics switches had no execution path and must not be migrated into hidden default behavior under the same names. Saving must be performed explicitly by the host; observation uses the actual `execution.Scope`, Observer/Tracer, and the model UsageHook. The `core/execution` package was subsequently removed entirely; see [Persistence, Human-in-the-Loop, and Runtime](#persistence-human-in-the-loop-and-runtime).

`core.RunnableConfig.RetryPolicy`, `core.RetryPolicy`, and `core.BackoffPolicy` have been removed. Transient model errors use `AgentBuilder.WithRequestError` with `builder.RetryTransient`; each request reserves budget independently, and responses already emitted are not automatically resent; see the [Builder Reference](BUILDER_API_REFERENCE.md). Tool calls are not implicitly retried; the number of recoveries is controlled by `tools.DispatchConfig.MaxAttempts` and the durable operation policy.

## Conversation Memory

`AgentBuilder.WithMemory`, `WithMaxConversationHistory`, `WithMemoryFailurePolicy`, `MemoryFailurePolicy`, and `AgentConfig.MaxConversationHistory`/`MemoryFailurePolicy` have been removed. Multi-turn history comes only from the session log:

```go
// Old
b.WithMemory(memory.NewInMemoryManager(memory.DefaultConfig())).WithSessionID(id).WithMaxConversationHistory(20)
// New
sessions := session.NewMemoryStore() // or session.NewFileStore(dir)
compaction, err := contextpolicy.NewCompaction(contextpolicy.CompactionConfig{MaxTokens: 100000, Model: summaryClient})
b.WithSessionLog(sessions).WithSessionID(id).WithCompaction(compaction)
```

History is no longer truncated by number of turns: its length is controlled by Compaction based on estimated tokens, old content is compressed into a summary, and the original text can be retrieved through `contextpolicy.NewRecallTool(sessions)`. A failed session log write fails the turn; there is no "best effort" mode. Conversations already saved in the old `MemoryManager` are not automatically migrated into the session log; if they need to be kept, the host reads them out and writes them with `session.Recorder.Sync`. The `compaction` component of goagent-harness is its own built-in implementation, and its configuration is unchanged.

`interfaces.MemoryManager`, `Conversation`, `Case`, and the original implementations in the `memory` package (`InMemoryManager`, `HierarchicalMemory` and its short-term/long-term layers, `InMemoryVectorStore`, `ChromaVectorStore`, etc.) have been removed and replaced by long-term memory:

```go
// Old
mgr.AddCase(ctx, &interfaces.Case{Title: t, Problem: p, Solution: s})
cases, _ := mgr.SearchSimilarCases(ctx, query, 5)
// New
mem, err := memory.New(memory.Config{Durable: fs, Index: memory.NewMemoryIndex(), Embedder: embedClient})
ctx = memory.WithNamespace(ctx, tenantID, userID)
_, err = mem.Remember(ctx, memory.Record{Kind: memory.KindSkill, Content: p + "：" + s, Importance: 0.7})
found, err := mem.Recall(ctx, memory.Query{Text: query, K: 5})
```

Without a namespace, all reads and writes return an error; a space shared by all callers no longer exists. Key-value data from `MemoryManager.Store/Retrieve` should use `store.Store` instead. Old data is not migrated automatically.

## Generic Cache

`cache.InMemoryCache/LRUCache/MultiTierCache`, the three corresponding constructors, and the dedicated `CacheEntry` have been removed. Use `cache.NewSimpleCache` when only TTL is needed; use `cache.NewNoOpCache` to disable caching. SimpleCache has no capacity, LRU, or multi-tier backfill contract, so you cannot claim that replacing the constructors keeps those capabilities.

`cache.CacheConfig` keeps only the effective `Enabled/DefaultTTL` and removes `Type/MaxSize/CleanupInterval`. Remove `type/max_size/cleanup_interval` from config files; strict decoding rejects these old keys. Hosts that need capacity or multi-tier semantics must explicitly inject a `cache.Cache` that implements the corresponding contract; see the [Caching Guide](CACHING_GUIDE.md).

## Checkpoint Failback

`DistributedCheckpointerConfig.FailbackDelay` and the private `tryFailback`, which had no production callers, have been removed. Remove `failback_delay` from config files. After failing over to the replica, the host still coordinates writes, verifies all data and deletion results, and then rebuilds the instance; the primary becoming healthy again does not trigger automatic failback. This change does not add cross-process fencing, CAS, or zero-RPO capabilities. The `core/checkpoint` package was subsequently removed entirely; see the next section.

## Workflow Resume and Historical Ledgers

Subgraph approvals uniformly use `workflow.WithResume(ctx, operationKey, workflow.ResumeRequest{Key: approvalKey, Input: input})`; a bare approval string is no longer recognized as a valid request. `operationKey` and the `Key` inside the request identify different boundaries and are not interchangeable. Approvals without a payload can still use a nil `Input`; an explicit JSON `null` is still a provided payload. Actual host authorization and approval validation remain required.

`durable.WithResume` continues to be used for ordinary durable operations and for the raw input of leaf interrupts; this capability is not the same as the old bare-key protocol for subgraph approvals and must not be removed along with it.

Online durable operations only read the current operation-v2. Old operation-v1 records must be converted explicitly through the standalone `core/durable/migrate` component; online execution no longer automatically supports the old version, and rejecting the old version does not cause business logic to re-execute.

Migration must stop the related writes and keep a backup; list the targets explicitly with `OperationRef{Key,ID}`, first use `migrate.OperationsV1`, which dry-runs by default, on the same Store/scope to obtain a report, and after validation passes, pass `OperationOptions{Apply:true}`. The component holds locks in storage ID order and, after all reads and validation complete, converts atomically through a single `Store.SaveAll`. The conversion only changes the snapshot version; the original Data bytes, identity, input fingerprint, result, and attempt count are unchanged, and no business function is called. Valid v2 records mixed in are not rewritten, and repeated application is safe; when the commit result is unknown, dry-run again to verify. The Store must satisfy the Lock and SaveAll contracts.

Do not resolve version rejections by deleting old ledgers; the ledger records external effects that have already happened, and rerunning after clearing it may repeat those effects. For the actual interface, error boundaries, and runnable examples of the migration, see the migration package documentation; do not let the migration tool and online execution process the same set of runs at the same time.

## Persistence, Human-in-the-Loop, and Runtime

The following packages and interfaces have been removed; state persistence, human-in-the-loop, and recovery uniformly use `core/durable` + `core/session`:

| Removed | Migration |
|---|---|
| The entire `core/checkpoint` package (`Checkpointer`, `NewInMemorySaver`, `DistributedCheckpointer`, etc.); the Builder's `WithCheckpointer`, `WithAutoSaveEnabled`, `AgentConfig.EnableAutoSave`; `errors.NewStateCheckpointError` | `Shutdown` no longer saves state. For cross-process state recovery use `WithSessionLog(store).WithSessionState()`; the state type must implement `MarshalJSON`/`UnmarshalJSON`; for session history use `WithSessionLog(session.Store)`, for crash recovery use `core/durable` and `WithDurableScope`, and for graph-level pause and resume use `workflow.CompileDurable` |
| The entire `core/execution` package (`Runtime`, `NewRuntime`, `RuntimeManager`, `ToolWithRuntime`, etc.); `core/middleware.MiddlewareRequest.Runtime` | Middleware reads `State` and `Metadata`; host assembly uses the top-level `execution.Scope`; see [Host Integration](HOST_INTEGRATION.md) |
| `core.InterruptManager`, `InterruptableExecutor`, `InterruptConfig`, etc. | `ApprovalNodes` of a durable graph or `durable.AwaitInput`; see [Human-in-the-Loop](HUMAN_IN_THE_LOOP.md) and `examples/human-in-the-loop`, `examples/human-in-the-loop-interrupt` |
| The Builder's `WithStore` | Use `WithSessionLog` for conversation history and `WithLongTermMemory` for long-term memory; other key-value data is held directly by the host in `store.Store` |
| Generic parameter `C` and `WithContext(C)`: `AgentBuilder[C, S]`, `ConfigurableAgent[C, S]` | Change to `AgentBuilder[S]`, `ConfigurableAgent[S]`, e.g. `NewAgentBuilder[any, *core.AgentState]` → `NewAgentBuilder[*core.AgentState]`; pass application data via `context.Context` or `WithMetadata` |
| Lifecycle and health status definitions in `interfaces` such as `Lifecycle`, `LifecycleManager`, `HealthStatus` | Plugins use `plugin.Starter`, `plugin.Dependent`; other components manage their lifecycle in the host |
| `tools.WithMiddleware`, `tools.MiddlewareTool` | `tools/middleware.Wrap(tool, middlewares...)`, `*middleware.WrappedTool` |
| `tools.ToolExecutor`, `ToolCallRequest`, `ToolResult`, `RetryPolicy`, `ToolGraph` | `tools.NewDispatcher` and `DispatchConfig` (concurrency, timeouts, durable replay, retry policy) |
| `tools.ToolCache`, `SimpleToolCache`, `MemoryToolCache`, `ShardedToolCache`, `NewCachedTool`, `NewHighPerformanceCache`, `NewLowMemoryCache` | `tools/middleware.Caching`, backed by any `cache.Cache` (entries can be limited with `cache.WithMaxEntries`) |
| `core.Agent`, `BaseAgent`, `AgentExecutor`, `AgentInput`/`AgentOutput`, `FastInvoker`; `observability.InstrumentedAgent`/`WrapAgent` | Build Agents with `builder` and connect to `core.Runnable` via `AsRunnable()` |
| `interfaces.Store`, `ConversationMemory`, `ToolExecutor`, `ToolResult`; `memory.ConversationStore` | Key-value storage `store.Store`; conversation history `WithSessionLog(session.Store)` |
| `llm/cache` (semantic cache) | No replacement; when needed, the host implements it scoped by tenant/session |
| `interfaces.ToolInput.Context` | Remove the assignment; tools use the ctx of `Invoke(ctx, input)` |
| `Steer(msg)`, `SteerOnce(key, msg)`, `Inject(msg)`, `Followup(msg)`, `Redirect(msg)` of `builder.Handle` | Add `ctx` as the first parameter: `Steer(ctx, msg)`, etc. |
| `tools/middleware.WithoutInputLogging`, `WithoutOutputLogging` | Arguments and results are no longer logged by default; use `WithInputLogging`, `WithOutputLogging` when logging is needed |
| Unreferenced constants in `constants` (including all constants in `store.go`) | Define them in the caller's own package |
| `testing/testutil` | Use standard library `testing` assertions, or `testing/mocks` |
| `Chain`, `BaseChain`, `ChainableAgent`, `Orchestrator`, `PluginRegistry`, `LifecycleManager`, `TimeoutConfig` in `core` | Use `core.RunnablePipe`/`RunnableSequence` or `workflow` for composition; use `multiagent` for multiple Agents |
| `ToolRuntime`, `RuntimeToolAdapter`, `UserInfoTool`, `SavePreferenceTool`, `UpdateStateTool` in `tools` | Data a tool needs is injected via `context.Context` or at construction; `StreamWriter` progress pushing has no replacement |
| Examples `examples/integration/{human-in-loop,langchain-phase1,langchain-complete}`, `examples/advanced/tool-runtime` | For human-in-the-loop, see the HITL examples above |

## Current Contracts That Are Kept

The stable encoding of the unified JSON facade, crash recovery for the current file format, the business CloneSnapshot protocol, generic convenience types, and external service protocol adapters are kept. They carry current data or capability contracts and are not renamed entry points of the removed legacy framework interfaces. This round does not automatically migrate user data, change external dependency versions, or release modules.
