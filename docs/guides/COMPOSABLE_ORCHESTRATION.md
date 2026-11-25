# Composable Orchestration R2

[English](COMPOSABLE_ORCHESTRATION.md) | [中文](COMPOSABLE_ORCHESTRATION_CN.md)

This round implements a root model-request budget, controlled Runnable tools, correlated OTel observation, immutable result references and context result offloading. `core/flow` is an experimental synchronous incremental prototype. The required contracts of the existing Runnable interface, Pipe, Graph and Adapt are unchanged.

## Running Offline

The examples live in the [goagent-examples](https://github.com/kart-io/goagent-examples) repository; run the commands below from its root. All examples use a local deterministic model, need no keys, and have no side effects such as real refunds:

```sh
go run ./orchestration/runnable-tool
go run ./orchestration/agent-tool
go run ./orchestration/field-adapter
go run ./orchestration/parallel-diagnostics
go run ./orchestration/stream-pipeline
go run ./orchestration/context-recovery /tmp/goagent-evidence-demo
go run ./orchestration/composed-order /tmp/goagent-order-demo
# Run composed-order again with the same directory: new model requests should be 0.
```

The order example registers an ordinary rules graph and a separate policy sub-Agent as tools, and maps order fields with a typed Adapt. The first run makes 4 actual model requests against a root quota of 4, with 8 tokens of known usage; replaying with the same directory creates no new sub-Agent and calls no model. Long evidence is saved as an immutable reference before the next request. The JSONL output contains only execution-correlation metadata.

## Root Budget and Usage

The host builds a ledger with `budget.New(Config{RootKey, Version, MaxModelRequests})` or `budget.NewDurable(config, store)` and passes it to the root call with `budget.WithController(ctx, controller)`, or with `budget.Bind`, which also propagates the deadline; for token, tool-call and deadline limits see [Execution Governance](EXECUTION_GOVERNANCE.md#root-budget). RootKey is a trusted business run ID and must not come from model arguments. Parent and child calls inherit the same context; a separate budget cannot be created to reset the quota.

The root budget applies to the Builder's unified entry, and every model request counts against the quota.

The unified model entry reserves before the actual request; a successful replay skips the reservation. Reservations are not refunded, and after a crash it may be impossible to tell whether the request was sent. A durable journal combined with an in-memory budget is rejected before the request. Changing the version or limit for the same RootKey is an error. The ledger and the model operation are not a cross-file transaction; if the result was saved but usage registration failed, an error is returned, and later reads repair usage from the existing record without paying again to fetch evidence.

`Snapshot` reports ReservedRequests, KnownUsageEntries, UnknownUsageEntries and KnownTokens separately. Unknown includes entries reserved without evidence afterwards and must not be treated as zero cost; a child output's TokenUsage is no longer added to the root ledger as another request. The output `root_budget` is the root snapshot, `new_model_invocations` is the number of actual calls newly made within this execution scope, and `model_calls` is the number of logical turns at this layer. With parent and child running in parallel, the root ledger and transport counts are authoritative. Provider-internal retries, host custom clients or hidden model calls must be governed separately by the host; this budget limits the number of requests through the unified entry.

The root ledger uniformly supports attribution queries: WithAgentID identifies the role, Attribution gives Self, Inclusive and unknown fields per call/role, and the Observer's model_attempt_start/model_usage provide per-attempt metadata. There is no format switch or degraded mode; for integration see [Multi-Agent Usage](MULTI_AGENT_USAGE.md). The original audit records the state before implementation.

`Remaining(ctx)` returns the number of model requests that can still be reserved (`MaxModelRequests` minus reserved entries). A durable budget reads it under the Store lock and can see reservations from other processes. The result is only a point-in-time snapshot; admission is still decided by `Reserve`. When no budget is bound, `budget.FromContext` returns a nil controller, and calling `Remaining` on it returns `constants.BudgetUnlimited` (`math.MaxInt` on the target architecture). Child budgets are not provided yet.

## Runnable Tools and Child Sessions

`tools.NewRunnableTool` wraps a shared stateless Runnable; the host provides the name, object Schema, Version, Decode and Encode. Resources of the original object are still closed by the host. The adapter gains durable-call capability only through `Dispatcher.Tools()`.

Stateful sub-Agents use `NewRunnableToolFactory`, which returns `OwnedRunnable{Runnable, Close}` each time. Close must be provided explicitly, even if it is a no-op. Resources already acquired are cleaned up even if construction fails partway; cleanup happens on success, execution failure and cancellation. The cleanup context is independent of parent cancellation and lasts at most 5 seconds; implementations that ignore context cannot be forcibly terminated. A cleanup error prevents the tool from being recorded as completed.

The host is responsible for giving each child its own SessionID, long-term memory namespace (`memory.WithNamespace`) and Dispatcher. Different object pointers do not prove isolation; the `agent-tool` example tests real concurrent memory isolation and rejects reused SessionIDs. Parent history is not copied automatically, and input decoding passes only the necessary business fields. Model-backed tools must declare `ModelInvoking: true`; a missing root budget is rejected before the factory runs. This declaration is not automatic detection of hidden model calls.

The default maximum nesting depth is 8, and smaller limits from ancestors still apply. Re-entering the same Dispatcher within the same execution chain is rejected before queueing for a concurrency slot; separate child Dispatchers may nest. Deadlines are inherited layer by layer; long-running tools need the host to configure the Dispatcher Timeout explicitly.

By default composite tools are opaque and single-attempt: a completed call with the same version can be replayed; pending/failed returns attention and does not automatically enter the child checkpoint. Encode must reject waiting or incomplete business output. For nested approvals, explicitly use `RunnableToolConfig.Resumable` with `DurableGraph.AsChildRunnable()` and run through a Dispatcher with a journal, so the same Store and a stable execution key carry the wait and the resume; ordinary adapters do not get this capability automatically. Exactly-once guarantees for handoffs and arbitrary external side effects are outside this contract. See the [Tool API](../api/TOOL_API.md) and the [subgraph adapter source](../../workflow/durable_runnable.go).

## Context Result Offloading

Context rewriting is unified as a PreStep hook (see [Tool Context](CONTEXT_POLICY.md)). `WithOffload(offload)` installs a `contextpolicy.Offload` as a PreStep with an identity; the read tool is `offload` itself and is registered by the host together with business tools. It requires a native `llm.ToolStreamClient` and configured business tools.

`contextpolicy.NewOffload` consumes a shared ResultStore; the threshold, retained turns, paging, exclusions and Namespace all go into `Identity()`, which enters the durable journal as the PreStep's identity. In-memory instances use an independent nonce; durable instances derive stable references from the execution key, turn and tool call ID. Namespace must identify the real backend configuration; switching backends requires changing it or the Version. Evidence is kept until `ResultStore.Release` is called for its run key: for non-durable execution without a provided scope, the Builder creates a temporary scope and releases it automatically when execution ends; when the host provides a scope via `WithResultScope` (including durable execution), the host releases it once the run will no longer be resumed. After release that run key cannot be reused, and late writes return `agentErrors.ErrResultScopeReleased`. For a durable ResultStore to support release, the underlying store must implement `durable.Scoped` (`durable.FileStore` does): one scope per run, deleted as a whole on release, with an 8-byte tombstone kept for each released run. Tombstones (in the in-memory version, the run key and release time) are reclaimed by the host periodically calling `ResultStore.PruneReleased(ctx, olderThan)`: it deletes only tombstones released earlier than `olderThan` and is mutually exclusive with writes; late writes within the threshold are still rejected, while late writes after reclamation are no longer rejected and may leave residue, so `olderThan` must exceed the longest retry or recovery window (process crash and restart, retries). Cross-process write rejection, automatic lock release after the writing process is killed, and retrying cleanup after a crash mid-release are covered by real child-process tests (`builder/contextpolicy/store_process_test.go`, `core/durable/file_store_test.go`), and apply only to local file systems on the same host with reliable flock/rename.

Durable mode uses `NewDurableResultStore(store)`; the read tool is registered with the host Dispatcher together with business tools and is journaled like any other tool (journalTools in `composed-order` is a complete example). A missing or duplicate registration of the read tool fails at Build; a non-durable result store or a missing execution key fails before the first model request. On recovery the PreStep's result is replayed, and the Builder rebuilds the reference table from the replayed conversation.

The store requires PutIfAbsent before a request carrying the reference is sent; different content under the same logical key is rejected, and reads re-verify the digest. ResultScope must be bound through the host context and defaults to the current stable execution key (in-memory execution uses a random scope). The model can only read aliases of references already present in the conversation and cannot choose a RunKey itself; the independent store lock does not reuse the current operation lock. The last request first restores originals and then removes the read tool, which may bring back a large context; there is no guarantee the input never exceeds the model window.

## Observation and Reporting

`observe.WithTracer(ctx, existingTracer)` reuses the host's OTel; no global Provider is set. `WithObserver` receives narrow metadata events. OperationKey is used for correlation across recoveries; VisitID/EventID include an independent identity for this call to distinguish repeated visits from recoveries; the business RunID and OperationKey stay stable, and recovery creates new spans. Without observation configured, no run identity or span is created. Custom observers must return promptly; panics are isolated.

`devtools.NewRecorder` defaults to a queue of 64 and at most 10,000 events; writes are asynchronous and overflow accumulates in Dropped. New JSONL files have permission 0600 and directories 0700; Close returns write errors. Custom Writers must cooperate and return. The event structure contains no input, output or raw exception text; hosts should use node names and run IDs without sensitive information.

Graph.Describe or the optional Description of a compiled artifact returns a sorted, independent `goagent-graph-v1` topology. `devtools.WriteReport(writer, description, events)` outputs escaped HTML with no scripts and no external resources, showing loops, branches, waits, failures, replays and observation drops. It cannot execute, approve or resume runs, and missing events do not allow inferring business state.

## Stream Prototype and Future Scope

`core/flow.Source` yields serially and synchronously, and Map starts no goroutines; Fold.Init creates an independent accumulator for each execution. Default limit is 1024 items; bytes can be limited explicitly with a Size provided. Any Source, Map, Add, preview or Finish error prevents the final Done, so even if Source swallows a yield error there is no false success. Preview can only emit events and cannot advance the graph. Cooperative sources must respond to context and yield errors, and stop the producer before returning.

Adopted conclusion: keep the experimental package for read-only incremental demos; it is not a stable API covering all four paradigms. Reflection-based Mapping, durable summaries and a general Debugger.Run remain deferred and need separate requirements and a recovery protocol. Nested resumable workflows are already supported through AsChildRunnable and Resumable tools; see the [composition guide](AGENT_COMPOSITION.md); independent background sub-task scheduling is outside this capability. Typed mapping should prefer the existing Adapt/Clone/Merge.

## Migration, Verification and Rollback

Without new configuration, no budget/policy intent fields are added. Original intent fixtures of ordinary tools and existing execution tests still pass. Changes to a new tool version, policy configuration or root budget configuration reject old runs; you cannot delete pending records, refunds or increase retry counts to force recovery.

Stop starting runs on the new version first, and keep workers of the matching version to process existing runs. Ledgers, references and checkpoints are retained per business run; active references must not be cleaned up early. Tests and performance measurements are in the delivery verification record. Real models are only wired in explicitly by the host with a budget set; the delivery evidence for this round uses a local model. Runnable cases wiring DeepSeek, MiniMax and Gemini are in [orchestration/live](https://github.com/kart-io/goagent-examples/blob/main/orchestration/live/README.md), with the verification record in real-model orchestration cases.

## Durable Agent Scopes and Upgrades

Every Builder Agent executing in a durable context must configure `WithDurableScope("stable-name")`; logical call names within the same parent node must differ and stay stable across restarts. `composed-order` uses `order-review` and `policy` respectively; model journals, tool journals and default context result references live in the corresponding Agent sub-scope, while the root budget and OTel context remain shared. A tool-less `llm.Client` keeps R2's `model/answer/0` journal and budget accounting, and the native tool loop uses `model/turn/N`.

When old unscoped `model/turn/0` or `model/answer/0` records are detected, the run is rejected early to avoid re-running under a different key. An existing directory cannot simply be treated as a new run; verify business effects first, then plan a version migration. Demo runs can use a new empty directory, but migration problems must not be hidden by deleting old evaluations or checkpoints. For the detailed contract see [Multi-Agent Scopes and Entry Constraints](NATIVE_ORCHESTRATION.md#multi-agent-scopes-and-entry-constraints).
