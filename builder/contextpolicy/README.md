# contextpolicy: Context Governance

[English](README.md) | [中文](README_CN.md)

`contextpolicy` keeps long agent conversations within the model context. `Offload` replaces oversized tool results with references that the model reads back page by page, `Compaction` replaces old conversation history with a summary, and `RecallTool` lets the model fetch original messages that were rewritten away. `ResultStore` holds the immutable evidence behind offloaded results. The Builder installs `Offload` and `Compaction` as PreSteps with an identity via `WithOffload` and `WithCompaction`.

## Usage

```go
offload, err := contextpolicy.NewOffload(contextpolicy.OffloadConfig{
    Store:                contextpolicy.NewMemoryStore(), // durable runs: contextpolicy.NewDurableResultStore(store)
    MaxToolResultRunes:   8000,
    KeepRecentToolRounds: 2,
    ReadPageRunes:        1024,
})
if err != nil {
    return err
}
sessions := session.NewMemoryStore()
compaction, err := contextpolicy.NewCompaction(contextpolicy.CompactionConfig{
    MaxTokens:  100000,
    KeepTokens: 25000,
    Model:      summaryClient, // nil: omit old messages with a note instead of summarizing
})
if err != nil {
    return err
}
recall, err := contextpolicy.NewRecallTool(sessions)
if err != nil {
    return err
}
agent, err := builder.NewSimpleBuilder(client).
    WithTools(searchTool, offload, recall). // offload is also the read_context_result tool
    WithOffload(offload).
    WithSessionLog(sessions).
    WithCompaction(compaction).
    Build()
```

When the host owns the run identity, it supplies the result scope and releases it:

```go
ctx = contextpolicy.WithResultScope(ctx, runKey)
// ... run the agent with ctx; resume it as often as needed ...
// Once the run will never resume, release its evidence:
return offload.Release(ctx)
```

## Key API

| Identifier | Description |
|---|---|
| `NewOffload(OffloadConfig)` | Validates the configuration and returns an `Offload`; it is both a PreStep component and the `read_context_result` tool |
| `Offload.Release(ctx)` | Deletes all evidence and the reference table of the current result scope |
| `Offload.Stats()` | Cumulative number of results this instance replaced with references |
| `NewCompaction(CompactionConfig)` | Validates the configuration, fills in defaults, and returns a `Compaction` |
| `NewRecallTool(store)` / `NewRecallToolConfig(store, RecallConfig)` | `recall_history` tool reading the session log; must use the same store as `WithSessionLog` |
| `ResultStore` | Evidence store interface: `PutIfAbsent`, `Get`, `Release`, `PruneReleased` |
| `NewMemoryStore()` | Concurrency-safe in-memory `ResultStore` |
| `NewDurableResultStore(durable.Store)` | `ResultStore` backed by a durable store |
| `WithResultScope(ctx, runKey)` / `ResultScope(ctx)` | Supplies / reads the trusted run ownership for evidence |
| `EstimateTokens(msg)` | Default estimate: one third of the UTF-8 byte count |
| `IsSummary`, `SummaryBody`, `KeepUserMessages` | Helpers for summary messages and custom `Keep` functions |

## Behavior Notes

- Limits are caller policy. In `CompactionConfig` and `RecallConfig`, 0 means unlimited and negative values are rejected. `CompactionConfig.MaxTokens` 0 never compacts proactively; `KeepTokens` 0 means `MaxTokens/4`, so it is required when `MaxTokens` is 0.
- `OffloadConfig.MaxToolResultRunes` must be positive. `ReadPageRunes` 0 returns the rest of the result in one page. Empty `ID`, `Version`, and `Namespace` default to `offload`, `v3`, and `results`; change `Namespace` when switching storage backends.
- `Offload.Reduce` depends only on the conversation and configuration, so repeated calls give byte-identical results. On the final request (`final` true) references are restored to the original text.
- When a request fails because it exceeds the context, the Builder forces one compaction (`Recover`, keeping only the most recent exchange) and retries.
- `Identity()` of both components covers the ID, version, and limits and enters the durable journal; resuming an old run after a configuration change is rejected. Change `CompactionConfig.Version` when the behavior of `Keep` or `Annotate` changes.
- One `Offload` is shared concurrently by multiple executions, with reference tables separated by result scope. Temporary scopes of non-durable executions are released when execution ends; scopes supplied via `WithResultScope` (including durable runs) are released by the host, and the run key must not be reused afterward.
- `ResultStore` never overwrites a logical key with different evidence. After `Release`, late reads and writes for that run key return `errors.ErrResultScopeReleased` until `PruneReleased` reclaims the tombstone; set its `olderThan` above the longest retry or recovery window and call it periodically.
- `NewDurableResultStore` gives per-run release only when the underlying store implements `durable.Scoped` (such as `durable.FileStore`); otherwise `Release` and `PruneReleased` return errors.
- `RecallTool` takes the session ID from the request context (`tools.WithSessionID`, filled in by the Builder by default), so only the current session can be read.

## See Also

- [Tool Context: Result Offloading and History Compaction](../../docs/guides/CONTEXT_POLICY.md)
- [Composable Orchestration: Context Result Offloading](../../docs/guides/COMPOSABLE_ORCHESTRATION.md#context-result-offloading)
- [Builder API Reference](../../docs/guides/BUILDER_API_REFERENCE.md)
