# Tool Context: Result Offloading and History Compaction

[English](CONTEXT_POLICY.md) | [中文](CONTEXT_POLICY_CN.md)

Context governance has only one mechanism: a PreStep hook rewrites the conversation before each model request, and the rewritten conversation becomes the history from then on; in durable runs each step's result is recorded in the journal and replayed on recovery. Result offloading (`contextpolicy.Offload`) and history compaction (`contextpolicy.Compaction`) are both PreStep components. Native tool streaming is required: configure tools and use an `llm.ToolStreamClient`.

## Offloading Large Results

```go
offload, err := contextpolicy.NewOffload(contextpolicy.OffloadConfig{
    Store:                contextpolicy.NewMemoryStore(), // for durable runs use contextpolicy.NewDurableResultStore(store)
    MaxToolResultRunes:   8000,
    KeepRecentToolRounds: 2,
    ReadPageRunes:        1024,
    ExcludeTools:         []string{"payment_receipt"},
})
agent, err := builder.NewSimpleBuilder(client).
    WithTools(searchTool, offload). // the read tool is offload itself; in durable runs it goes through the Dispatcher with the other tools
    WithMaxIterations(12).
    WithOffload(offload).
    Build()
```

Before each model request, the most recent `KeepRecentToolRounds` complete tool rounds are kept (multiple tools in one round count as the same round); older tool bodies exceeding `MaxToolResultRunes` are written to `Store` and replaced in the conversation with a reference:

```json
{"context_result":"r1","runes":12000,"read_tool":"read_context_result","stored":{"key":"…","sha256":"…"}}
```

The assistant's tool calls, reasoning_content, result roles, and tool_call_id are unchanged. The model reads the original text page by page with `read_context_result`, passing `key` (the value of `context_result`) and an optional `offset`; offset counts Unicode characters, each page defaults to 1024 characters, with a maximum of 16384. It returns `text`, `next_offset`, and `done`. Aliases are numbered by the count of references already in the conversation (`r1`, `r2`, …) so the model does not mistype long hashes. An unknown key returns `{"error":"unknown context_result key","valid_keys":[...]}`, and an invalid `offset` returns `{"error":"invalid offset","min_offset":0,"max_offset":<original character count>}`; neither aborts execution. The read tool itself and tools on the exclusion list are not offloaded.

The last model request (`Step.Final`) no longer provides tools, so the model cannot read references; all references are therefore restored to the original text. The final request's context is not guaranteed to be smaller; if needed, extract business fields with tools before that point or increase the budget. `WithReturnDirectTools` returns the full original tool text.

### Instances, Scopes, and Recovery

- One `Offload` is shared concurrently by multiple executions, with reference tables separated by result scope; an Agent can have only one, and the read tool must be registered exactly once (validated at Build).
- Scope: when the host supplies one via `contextpolicy.WithResultScope`, the host calls `offload.Release` after the run will no longer be resumed; otherwise durable executions use the execution key (also released by the host), and non-durable executions use a temporary scope released when execution ends (success or failure).
- Evidence is written to the store before the reference is emitted; the same logical result with different content is rejected, and the digest is verified on read-back.
- Durable runs require a durable result store and a stable execution key. On recovery the PreStep results are replayed, not recomputed; the Builder rebuilds the reference table from the replayed conversation, and reads are unaffected.
- `Offload.Identity()` is computed from the ID, Version, Namespace, and all limits, and enters the journal as the PreStep identity: if you change the configuration and then resume an old run, it fails with `intent changed` before the model is requested. `Namespace` must identify the actual storage backend; change it when switching backends.
- `offload.Stats()["offloaded_results"]` is the instance's cumulative offload count.

For releasing durable result stores, tombstones, and `PruneReleased`, see the [Composable Orchestration guide](COMPOSABLE_ORCHESTRATION.md#context-result-offloading).

## History Compaction

```go
sessions := session.NewMemoryStore()
compaction, err := contextpolicy.NewCompaction(contextpolicy.CompactionConfig{
    MaxTokens:  100000,        // compact when the estimated length exceeds this
    KeepTokens: 25000,         // recent messages kept verbatim; defaults to MaxTokens/4
    Model:      summaryClient, // when nil, no model is called; old messages are omitted with a note on what was omitted
})
recall, err := contextpolicy.NewRecallTool(sessions)
agent, err := builder.NewSimpleBuilder(client).
    WithTools(searchTool, recall).
    WithSessionLog(sessions).
    WithCompaction(compaction).
    Build()
```

- When the conversation's estimated length exceeds `MaxTokens`, the messages after the system message and before roughly the last `KeepTokens` are replaced by a single user message starting with `[此前对话的摘要]`. The kept region starts at a non-tool message and never splits a tool call from its result; input newly received in this step is always kept; the first and most recent user utterances in the compacted range are kept verbatim within `KeepTokens/2` (`Keep` can replace this).
- On subsequent compaction, the old summary is given to the summary model together with the new messages, without truncating the old summary; notes returned by `Annotate` are appended after the summary and are not given to the summary model.
- When summarization fails, it falls back to an omission note, with the old summary appended unchanged; cancellation and root budget exhaustion return errors. Summary requests count against the root budget.
- When a request fails because it exceeds the context, compaction is forced once, keeping only the most recent interaction, and the request is retried.
- `Estimate` by default estimates tokens as one third of the UTF-8 byte count, which is conservative for English.
- `Compaction.Identity()` is computed from the ID, Version, and all limits, and enters the durable journal as the PreStep identity; change `Version` when the behavior of `Keep` or `Annotate` changes.

The session log records the compacted conversation as a `history/rewrite` with a `Surface`: the original text remains in the log, `session/rewind` undoes the summary, and durable runs replay the summary on recovery without requesting the model again. `recall_history` retrieves original text in this session that was rewritten away: `query` filters by substring, `before` pages backwards using the previous result's `next_before`, and `limit` defaults to 20 with a maximum of 100. The session ID is taken from the request context; other sessions and undone ranges cannot be read.

## Custom Rewrites

Write other rewrites directly as a PreStep:

```go
b := builder.NewSimpleBuilder(client)
b.WithIdentifiedPreStep("glossary/"+version+"/"+hash, func(ctx context.Context, step *builder.Step) (builder.PreStepResult, error) {
    step.Messages = withGlossary(step.Messages) // the rewritten messages become the history from then on; must be idempotent
    return builder.PreStepResult{}, nil
})
```

`identity` identifies the hook's configuration; when it changes, old durable journals are not reused. Hooks that do not affect results use `WithPreStep`. A hook may rewrite any message, but it must keep tool calls paired with their results and must not modify messages carrying `ProviderState` (native signatures would become invalid).

## Migration

| Old | New |
|---|---|
| `WithContextPolicy(ContextPolicy{MaxToolResultRunes, …})` | `WithOffload(offload)`, and register `offload` as a tool; fields such as `ExcludeTools` move to `OffloadConfig` |
| `ContextPolicy.HistorySummary` | `WithCompaction(contextpolicy.NewCompaction(...))` |
| `WithContextPolicies(factories...)` + `WithPolicyToolAdapter` | Offload: `WithOffload`, with the read tool going through the Dispatcher together with business tools; custom policies: `WithIdentifiedPreStep` |
| `contextpolicy.Policy`, `Descriptor`, `Turn`, `Factory` | Removed; the role of `Descriptor.ConfigHash` is taken over by the PreStep identity |
| Metadata `context_offloaded_results`, `policy.<id>.*`, `summary_calls` | Removed; use `offload.Stats()` |

Durable runs recorded with the old version must finish before upgrading: model request intents no longer include policy descriptors, and Offload's reference format has also changed.
