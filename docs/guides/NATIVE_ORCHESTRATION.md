# GoAgent Native Orchestration and Tooling

[English](NATIVE_ORCHESTRATION.md) | [中文](NATIVE_ORCHESTRATION_CN.md)

Graph orchestration, tool execution, approval and recovery are all implemented by GoAgent; the main module does not pull in any other orchestration framework. The Builder's model tool loop continues to use the existing providers; deterministic business flows are scheduled by `workflow.Graph`, and the two are composed through Runnable and explicit state mapping.

| Component | Responsibility |
|---|---|
| `workflow.Graph[T]` | Conditional routing, parallel branches, deterministically ordered joins, bounded loops |
| `Compile` | In-memory execution, Invoke/Stream/Batch and node events |
| `CompileDurable` | Node and parallel-branch checkpoints, approval, restart recovery |
| `DurableGraph.AsRunnable` | Invoke/Stream/Batch for durable flows, unified event entry |
| `tools.Registry` | Tool registration and lookup |
| `tools.Dispatcher` | Schema/custom validation, authorization hook, `Pipeline` admission and approval, timeout, concurrency limit, call journal, failure propagation |
| `workflow/refund.Service` | Business idempotency, approval expiry, order-fact and cumulative-amount protection |

## Wiring Tools into Graphs and the Builder

```go
registry := tools.NewRegistry()
if err := registry.Register(lookupTool); err != nil { return err }
dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{
    MaxConcurrency: 4,
    Timeout: 30 * time.Second,
    Authorize: authorizeToolFromHostContext,
})
if err != nil { return err }

node, err := workflow.Adapt(dispatcher.AsRunnable(),
    func(s State) (tools.ToolRequest, error) {
        return tools.ToolRequest{
            Name: "lookup_order",
            Input: &interfaces.ToolInput{Args: map[string]any{"order_id": s.OrderID}},
        }, nil
    },
    func(s State, out *interfaces.ToolOutput) (State, error) {
        s.Evidence = fmt.Sprint(out.Result)
        return s, nil
    },
)
if err != nil { return err }
if err := graph.AddRunnable("lookup", node); err != nil { return err }

// The model can also choose to call it; the same validation, authorization and concurrency gate apply.
agent, err := builder.NewSimpleBuilder(client).
    WithDurableScope("assistant").
    WithTools(dispatcher.Tools()...).
    WithMaxIterations(3).
    Build()
```

The Dispatcher freezes tool names, descriptions and Schemas at construction; later additions or removals in the Registry do not affect an existing instance. Tool implementations must still support concurrency; if they do not, set MaxConcurrency=1. The Schema root type must be object; nested objects, additional properties, numeric constraints and so on are validated with a pinned-version JSON Schema implementation; external `$ref` has no loader and is rejected at construction. Default arguments are not filled in automatically, and there are no retries.

`DispatchConfig.Timeout` is the default timeout for a single call (30 seconds by default). When a tool implements `tools.DispatchTimeoutProvider` (`DispatchTimeout() time.Duration`), a declared positive value replaces the default, longer or shorter; returning 0 keeps the default, and negative values are rejected when the Dispatcher is constructed. Sub-Agents usually need far more than 30 seconds; set `RunnableToolConfig.Timeout` on the composite tool rather than raising the whole Dispatcher's default; it only takes effect when called through a Dispatcher. Timing starts after a concurrency slot is acquired and excludes queueing; queueing is bounded only by the caller's ctx, so hosts that need to limit total duration should set a deadline on ctx. Running tools must respond to context; the executor never starts an unreclaimable background goroutine to fake a timeout. nil output, Success=false, a non-empty Error field, panics and cancellation all return errors. Authorize may be empty (no extra dispatch-layer authorization); anything involving business permissions must be authorized by the host or checked by the business service. CallerID and approval identity must not come from model arguments.

### Returning Argument Errors to the Model

By default any tool error aborts the Agent. With `DispatchConfig{ReturnArgumentErrors: true}`, model calls made through `Dispatcher.Tools()` that hit the following errors instead return a result with `Success=false`, an empty `Error` and `Result={"error":"invalid_arguments","message":...}`, so the model can correct them in the next turn:

- Schema validation failures;
- `*tools.ArgumentError` returned by a custom `Validate` or by the tool's `Invoke` (may be wrapped with `%w`; with `errors.Join` it counts only if every branch is an argument error), including `ValidationError` returned by built-in tools and `*errors.AgentError` marked with `MarkModelArgument()`. `Message` is shown to the model as is; `Err` is not shown;
- Schema errors of `NewRunnableTool`/`NewRunnableToolFactory`, and `*tools.ArgumentError` returned by `Decode`; in that case no child session is created. Other `Decode` errors still abort;
- An argument-error result returned by another Dispatcher tool registered directly into this Dispatcher that has the option enabled itself is passed through unchanged. Results of the same shape returned by other tools are still treated as failures and abort.

`Dispatcher.Invoke` and `AsRunnable` (host paths such as workflow nodes) are not affected by the switch and still report argument errors as errors. Authorization denial, timeout and cancellation, exceeding the nesting depth, Dispatcher re-entry, waiting for the host to continue, unknown tools, and arguments that are not a JSON object still abort; with invalid arguments `Authorize` still runs first and a denial aborts, so `Authorize` may receive arguments that do not satisfy the Schema. Any error reported by a child session or an inner Dispatcher (including the inner layer's argument errors) is a failure of this tool's execution, and the outer layer does not treat it as its own model's argument error. Execution entries of inner Dispatchers, composite tools and Builder Agents register automatically; wrappers that call other inner components themselves must pass through the ctx they received and register the returned error with `tools.Settle(ctx, err)`; if ctx is dropped, the barrier, re-entry and depth protections stop working. A tool that re-wraps an inner error into a new argument error is treated as reclassifying it at this layer; this decision is registered by error-value identity within the call and does not wrap the error, so the error returned through the Dispatcher and composite tools is still the very same value the tool originally returned. If the same package-level sentinel is both returned by an inner layer and returned by the tool itself within the same call, it conservatively aborts as an inner error. Other valid calls in the same turn run normally.

Argument-error results use the same call intent and journal record as normal execution and are written as completed; on recovery the original result is replayed without running the tool. The switch is not part of the intent: after toggling it, valid calls replay normally, but replaying an argument-error record on a path where the option is not enabled aborts. For failure records written while the switch was off, after turning it on only operations that allow retrying failures (the default tool policy) are re-evaluated; composite tools (`MaxAttempts=1`) and rejected model turns still need host handling. In Builder durable mode, for tools implementing `interfaces.ArgumentErrorTool` and returning true, the model turn is committed normally when arguments are invalid; wrappers must forward this interface explicitly. When a direct-return tool hits an argument error, the error is returned to the model as an ordinary tool result to correct and the loop continues; the error is never treated as the answer. If the last turn still passes invalid arguments, it reports budget exhaustion with that argument error attached; other failures of direct-return tools still abort. Repeated invalid arguments are bounded by `WithMaxIterations`; on exhaustion the error includes the last tool failure message (not included if later calls succeeded; truncated beyond 500 characters).

## Unified Entry for Durable Graphs

```go
run, err := graph.CompileDurable(ctx, workflow.DurableConfig{
    Store: store,
    Version: "order-flow-v1",
    ApprovalNodes: []string{"refund"},
})
if err != nil { return err }
snapshot, err := run.AsRunnable().Invoke(ctx, workflow.RunRequest[State]{
    RunID: runID, Initial: initial,
})
if err != nil { return err }
if snapshot.Waiting {
    // After the host completes identity and business authorization, resume with the same RunID and snapshot.Key.
}
```

An approval pause in `AsRunnable` is a normally completed call: `snapshot.Waiting=true`, `snapshot.Done=false`. The final Done of Stream only means this call has ended; check Data.Done for whether the business flow is complete. The compatibility method `Run` still returns `agentErrors.ErrApprovalRequired`. `Snapshot(ctx, runID)` loads and validates the persisted state read-only and needs to acquire the run lock; it may wait during active execution, so callers should set a timeout.

Events include node_start/node_end, branch_start/branch_end, tool and node native events, and approval_required. Branch and child events carry the node scope. They are live observations of this execution, not a recoverable event log; a consumer disconnecting does not undo business effects already committed. If you stop consuming Stream early, you must cancel the context.

## Parallel Recovery and Effect Deduplication

1. Before a parallel node starts, the Clone'd input of each branch is persisted as JSON before tools or other effects are scheduled.
2. Each successful branch saves its result separately. A branch failure cancels unfinished work in the same group and waits for already-started functions to exit; saved successful branches are skipped on recovery.
3. The join always runs in branch registration order. Merge must be side-effect free; if it fails or saving its output checkpoint fails, recovery redoes only the Merge and keeps the branch results.
4. The node and each branch have their own stable execution key, and different visits in a loop use different keys. If an effect was committed but its result was not saved, it may still run again, and the business service provides idempotency.

`workflow.DurableKey(ctx)` and `durable.ExecutionKey(ctx)` return the same execution key. The Dispatcher injects that key into ToolInput.ExecutionKey; `core/durable.Runner` likewise injects the key into the step context. The model's Args cannot override this metadata.

One key corresponds to one logical effect. Hand-written multi-tool nodes set a stable CallID for each operation, and the Dispatcher derives child keys automatically; the Builder's native tool loop generates CallIDs from persisted model turns and call IDs. A bare Tool does not promise call-level recovery, and a durable Builder rejects it before the model request. See below for details.

State and branch results in the graph must round-trip through JSON. Serial v1 checkpoints remain compatible; changing parallel branch names or order changes the plan fingerprint and rejects old checkpoints. Semantic changes to functions, Merge, Clone or the state structure still require a Version bump. FileStore only provides a run lock and atomic saves on a reliable single-host file system, with no cross-host lease or takeover; for runs shared by multiple instances use goagent-contrib's `durable/pgstore`. The framework does no automatic compensation.

## Runnable Refund Case

The case lives in [goagent-examples](https://github.com/kart-io/goagent-examples/blob/main/workflow/refund-recovery/README.md); run the commands in this section from the root of that repository.

```sh
# Use a new directory; the first run stops before approval, with no model calls.
go run ./workflow/refund-recovery -engine graph -dir /tmp/refund-native
# Replace KEY with snapshot.Key from the previous step.
go run ./workflow/refund-recovery -engine graph -dir /tmp/refund-native \
  -approve KEY -approver demo-reviewer -crash-after-refund
# The previous step simulates exit code 86. Restarting returns the original receipt, and the ledger has only one refund.
go run ./workflow/refund-recovery -engine graph -dir /tmp/refund-native
```

Graph flow: recover the existing request → query the order / validate the request in parallel → register the review snapshot → approval → Dispatcher refund tool → done. The tool calls a real local SQLite transactional adapter; order and approval identities are still demo data, and no payment provider is contacted. `-engine steps` is the old default entry; the two executors use separate checkpoint subdirectories and share business ledger deduplication.

```sh
# goagent repository
go test -race ./workflow ./tools ./core/durable
# goagent-examples repository
go test -race ./workflow/refund-recovery
```

Covers skipping after branch success, failure/cancellation, join retries, stable keys when branch saving fails, real process crashes, old checkpoint compatibility, tool Schema/authorization/result failures, the Builder native tool path, and cross-process recovery of the refund graph.

## DevOps, Article Review, Auto-Reply

[Three business cases](https://github.com/kart-io/goagent-examples/blob/main/workflow/business-cases/README.md) live in the goagent-examples repository; run the commands in this section from the root of that repository. The cases provide a complete CLI, JSON input fixtures and automated tests: DevOps parallel diagnosis with a simulated restart after approval; article parallel review, rejection and human re-review; auto-reply based on a local FAQ, replying only when there is a source and handing unknown questions to a human.

```sh
go run ./workflow/business-cases -case devops -dir /tmp/goagent-devops
go run ./workflow/business-cases -case article-review -dir /tmp/goagent-article
go run ./workflow/business-cases -case auto-reply -dir /tmp/goagent-reply
go test -race ./workflow/business-cases
```

All three cases use offline rules by default and can be wired to real models with `-live -provider gemini|deepseek|minimax -model MODEL`; business effects are always simulated locally. Once the model's judgment is persisted, approval recovery and flow replay no longer call the model. Tests cover zero effects before approval, Schema validation, failure/cancellation recovery, cross-process crashes and business ID deduplication.

Real-model tests use `bash scripts/business_cases_live.sh gemini NEW_REPORT_FILE` (or `deepseek`, `minimax`), reading the corresponding API key and model name from the environment; at most 6 requests per provider, failures are kept, and there are no automatic retries. See the [Gemini / DeepSeek report](../evaluation/2026-10-27-business-cases-live/REPORT.md) and the [MiniMax report](../evaluation/2026-10-27-business-cases-minimax/REPORT.md).

## Capability Boundaries and Follow-up Work

The same graph state type is kept for now; heterogeneous data is converted explicitly with Adapt. Cross-node Collect/Transform, a field-mapping DSL, a visual debugger and distributed scheduling are not implemented yet. Native orchestration and tool execution being connected does not mean all general orchestration capabilities are covered.


## Tool-Call Journaling and Recovery

```go
// ctx comes from a durable graph node and contains the store and the parent execution key of this node visit.
out, err := dispatcher.Invoke(ctx, tools.ToolRequest{
    Name: "reserve_stock",
    Input: &interfaces.ToolInput{
        CallID: "reserve", // stable logical ID defined by the host; a second effect must use a different ID
        Args: map[string]any{"sku": "demo", "quantity": 1},
    },
})
```

Each operation runs in order: acquire the child operation lock → verify the intent digest → save pending and the attempt count → execute → save completed/failed. A repeated call to a completed operation returns the saved result; if the arguments, schema, caller or attempt budget for the same ID change, recovery is rejected. Call output crosses a JSON boundary and is decoded as JSON for both fresh runs and replays: objects become maps and numbers become json.Number; custom Go types must be converted explicitly at the application boundary. An old empty CallID does not enable this journal and does not change the original tool output type.

`DispatchConfig.MaxAttempts` defaults to 3, including the first execution; a failed or indeterminate operation is retried only on the next explicit call. Each recovery still runs schema validation, custom validation and authorization first. Authorization functions must have no side effects; they may be called on both first execution and replay. The journal does not provide exactly-once external effects: for tools inside a crash window, the target system must put the deduplication key and the business write in the same transaction. Save failures are never treated as success.

Replay returns the result from the journal directly, without passing through the tool itself or wrappers inside the Dispatcher. If the host maintains state in execution order (for example an evidence ledger that assigns citation numbers in arrival order), put it in `DispatchConfig.OnResult`: it is called synchronously after the result is obtained and before `tool_end`, and is triggered by first execution, journal replay and argument-error results returned to the model; its parameter is `tools.DispatchResult{Name, CallID, ExecutionKey, Args, Output, Replayed}`. For durable calls `Output` is always decoded from the journal, so first-run and replay values are the same; `ExecutionKey` is derived from the parent key and the CallID, is stable across restarts, and can serve as the host's deduplication key. During crash recovery, already completed calls are replayed and trigger the callback again. Sequentially executed calls trigger in call order; calls executed concurrently within the same Builder turn are deferred via `tools.DeferResults` until the whole turn ends and then triggered in the order given by the model, so first execution and recovery replay have the same order and an arrival-ordered ledger can be rebuilt exactly. When the host calls `Invoke` concurrently itself, trigger order is nondeterministic and triggers happen concurrently, so it must lock on its own, and numbering should be derived from `ExecutionKey` rather than assigned in arrival order. If the callback returns an error or panics, the call fails and the already written journal is unchanged. The callback is not part of the intent digest, is not bounded by `Timeout`, and must not modify `Args` or `Output`.

The Builder requires explicitly configuring `WithDurableScope("assistant")`; it first derives the Agent call key from the node key, then under it saves complete model turns with `model/turn/N` and each tool with `tool/turn/N/call/ID`. As a result Gemini's ProviderState and DeepSeek/MiniMax's reasoning_content can continue after a restart, and executed call IDs do not depend on regeneration. Native continuation data is used only in local checkpoints, and public test reports do not include it. The Store may contain business data and model content; the host is responsible for access control and retention.

A model turn first validates all tool names, IDs, JSON arguments and Schemas; if any call is invalid, no tool in the turn runs. Network errors, truncation and invalid plans also save the usage already obtained. Apart from verbatim retries within the same turn record by a `WithRequestError` hook (such as `builder.RetryTransient`), a model operation requests at most once; recovery decisions that replace messages or the client end with an error in durable runs. If the process crashes after the request but before the result is persisted, pending/ambiguous is kept, and recovery returns `ErrOperationAttention` without paying again; usage not yet obtained at that point cannot be reconstructed. Call `InspectOperation(ctx, "model/turn/0")` on the corresponding Agent-scope context to read the detailed state; the node's parent context cannot be used directly to query a child scope. After handling the indeterminate effect, the business host decides whether to compensate or start a new workflow; do not delete the journal directly to force a retry.

`durable.Do` lets other nodes save named child operations, and `WithJournal` and `WithExecutionKey` let the host wire them explicitly. Inputs are saved as digests, outputs as JSON, and errors only as categories. One attempt by default; `RetryFailed: true` together with MaxAttempts allows explicit recovery. Without a bound journal it executes directly with no durability.

When the model, prompts, tool implementations, policies or input structure change, `DurableConfig.Version` must be bumped. Provider, messages and tool definitions in model requests have separate intent checks, but the framework cannot infer every change to closure configuration. Recovery requires fixed input and context; PreStep results (including `WithOffload`) are replayed with the journal; see [Tool Context](CONTEXT_POLICY.md). External memory, retrieval, middleware and completion callbacks are also not arbitrary call-stack checkpoints; the host must keep them stable or persist them itself.

## Typed Stream Adaptation and Capability Declaration

`workflow.AdaptStream` adds an explicit intermediate chunk mapping that keeps typed previews and business events; the final mapping still runs after a valid terminal state and EOF. Previews do not commit graph state, do not schedule downstream nodes, and may repeat on recovery. Mapping errors, cancellation and upstream protocol errors terminate execution.

The Builder reads the model's optional `core.StreamCapability`. The MiniMax case adapter currently supports real native tool calls but delivers only after the HTTP response is complete: the model is final and the Agent is steps, with no claim of token-by-token streaming. Cross-node Collect/Transform is still not implemented, and no external orchestration framework dependency has been introduced.

## Multi-Tool Real-Process Recovery Verification

Run the following commands from the root of the goagent-examples repository.

```sh
# Offline by default: simulate the native protocol and verify real child-process exit and continuation.
go test -race ./workflow/business-cases -run TestNativeToolLoopProcessRecovery
# Explicit real model: choose one provider, at most 4 requests each time.
DEEPSEEK_MODEL=deepseek-flash bash scripts/business_tool_loop_live.sh deepseek /tmp/deepseek-loop.json
```

The same Agent node runs two tools, A and B, and calls `os.Exit(86)` after B's effect is persisted; the new process should skip A, recover B and request only the final answer; a third process start should make zero model/tool calls. Effects are deduplicated by child execution key, asserting there are only two records, two distinct keys, and that the final answer contains both receipts. See [three-model measurements and first-failure records](../evaluation/2026-10-27-durable-tool-loop/REPORT.md).

## Composable Orchestration R2

Adds root budgets, controlled Runnable tools, OTel observation and immutable context result references; see the [Composable Orchestration guide](COMPOSABLE_ORCHESTRATION.md). Context result offloading uses `WithOffload`; in durable runs the read tool goes through the host Dispatcher together with business tools. Stream previews still do not represent business terminal state; core/flow is only an experimental prototype.


## Multi-Agent Scopes and Entry Constraints

Each logical Agent call in the same node must have a distinct scope that is stable across restarts:

```go
author, err := builder.NewSimpleBuilder(client).
    WithDurableScope("author").WithTools(dispatcher.Tools()...).Build()
// After checking err, build the other component:
reviewer, err := builder.NewSimpleBuilder(client).
    WithDurableScope("reviewer").WithTools(dispatcher.Tools()...).Build()
```

The call key is derived as `durable.ChildKey(nodeKey, "agent/" + scope)`, and both model and tool journals live under this key. Reusing the same scope means resuming **the same logical call**, not starting new work; when the same Agent handles multiple business items within one node, the host should derive different parent keys from stable business IDs, or use different scopes/graph nodes. Do not use random IDs, process pointers or concurrent completion order. A missing scope, a blank scope or old unscoped journals all error before the model request.

A tool-less `llm.Client` also runs through the same model operation journal: successful results can be replayed, and failures keep known usage and stop repeated requests. A durable Agent with tools must use `ToolStreamClient` and `Dispatcher.Tools()` (or an equivalent DurableCallTool); these checks complete before middleware, retrieval and the model request, and lock waits for journal checks count toward the Agent timeout.

Multiple tool calls in the same turn run sequentially by default. When `WithParallelToolCalls(n)` is set to n>=2 (pass `math.MaxInt` for no separate cap) and every tool implements `interfaces.ConcurrentCallTool` (`Dispatcher.Tools()` already does; wrappers must forward it explicitly), they run concurrently with a peak of `min(n, MaxConcurrency)`; if any tool does not implement the interface, the whole turn runs sequentially (a `contextpolicy.Offload` read tool registered directly rather than through a Dispatcher does not implement it). When concurrent, execution order is not guaranteed to match the order given by the model, even with `MaxConcurrency=1`. Tool messages and `tool_start`/`tool_end` events follow the order given by the model, regardless of completion order; `OnResult` is deferred until the whole turn ends and triggered in the same order. A failing call does not cancel the others; after all finish, the earliest error in order is returned, so sibling calls are not recorded as cancellation failures that the host would have to handle on recovery. Inside tools, do not assign numbers by execution order or depend on the order of calls in the same turn. A direct-return tool only takes effect when it is the only call in the turn, so it does not participate in concurrency.

`WithEmptyAnswerRetries(n)` is off by default. When enabled, a turn in which the model ends with stop and has neither body text nor tool calls is written to the journal as completed; instead of appending an empty assistant message, a fixed user message is appended to ask again. Re-asks count toward MaxIterations and consume one unit of budget. Beyond n times, or on the last turn, it errors as before and the durable turn is recorded as failed.

This is a change to the durable integration contract. Existing hosts need to add scopes and bump `DurableConfig.Version`; verify the business effects that old runs already produced before deciding whether to migrate or start new logical work, and do not delete checkpoints directly to retry. When old `model/turn/0` or `model/answer/0` journals are detected under a node, execution never silently switches keys. The native multi-tool case has been bumped to native-loop-v3 to avoid treating historical v2 runs as new keys to continue.

## Typed Previews in Durable Streams

Durable graph `AsRunnable().Stream` keeps intermediate node Data: read it via `chunk.Data.State`, with `chunk.Data.Preview=true` and `chunk.Done=false`. Zero-value data and data chunks without an Event are also forwarded; events still keep the node scope. The State of a pure event chunk may be the zero value of T, so do not infer business state from it.

Preview is a temporary observation of this execution, not a checkpoint; it does not drive downstream nodes, does not commit durable state, and does not represent approval or completion. Consumers must not use it as recovery state. The durable state returned by `Snapshot`/`Run` and the final success chunk all have Preview=false. If an error or cancellation happens after a preview, recovery still starts from the last committed state; re-execution may produce previews again.
