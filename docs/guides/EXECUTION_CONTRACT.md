# Execution Contract and Capability Status

[English](EXECUTION_CONTRACT.md) | [中文](EXECUTION_CONTRACT_CN.md)

## Recommended Entry Point

New orchestration uses `core.Runnable[I, O]` throughout. The old `core.Agent`/`BaseAgent` and `interfaces.Agent/Runnable` have been removed; agents are built by the Builder and plugged in through `AsRunnable()`. Runnables with different types need explicit input/output conversion; messages are never dropped implicitly.

The Builder joins the same contract through `AsRunnable()`:

```go
agent, err := builder.NewSimpleBuilder(client).
    WithRetriever(retriever).
    WithTools(lookupTool).
    Build()
if err != nil { return err }
runnable := agent.AsRunnable()
output, err := runnable.Invoke(ctx, "What is the refund policy?")
// output is the *builder.AgentOutput after retrieval, tool execution, middleware and completion callbacks.
```

`Invoke` and `Stream` share the `ExecuteStream` execution source; Invoke only collects the final output and **does not make an extra synchronous model call**. The Builder accepts any `llm.Client` (which only has a streaming interface); when tools are configured it also requires `llm.ToolStreamClient`, and Build fails otherwise.

The table below helps choose an entry point; old API examples do not mean new capabilities also work on the old path.

| Builder entry | Tool and context governance | Concurrency protection for the same Builder | Use |
|---|---|---|---|
| `AsRunnable().Invoke/Stream` | Runs configured tools automatically; the native `ToolStreamClient` path supports `WithOffload` and ReturnDirect | All entries share one serial execution gate | Recommended entry for new code and Runnable orchestration |
| `ExecuteStream` | Shares the execution source and capabilities with AsRunnable | Acquires the same serial execution gate | Consume Builder events directly |

Implementing only `llm.Client` does not mean native tool streaming is supported. Configuring `WithOffload` or ReturnDirect also requires passing build checks such as `ToolStreamClient`; a provider's capabilities cannot be inferred from method names.

```go
ctx, cancel := context.WithCancel(ctx)
defer cancel()
chunks, err := runnable.Stream(ctx, "What is the refund policy?")
if err != nil { return err }
for chunk := range chunks {
    if chunk.Error != nil { return chunk.Error }
    if chunk.Event != nil {
        // Event.Type: sources, step, text, usage, done.
        // Event.Delta is newly added text; Event.Usage is a cumulative usage snapshot, do not sum it again.
        fmt.Print(chunk.Event.Delta)
    }
    if chunk.Done {
        // Data is the final output and middleware may modify it; intermediate text is for transient display only.
        fmt.Println(chunk.Data.Result)
    }
}
if err := ctx.Err(); err != nil { return err }
```

A standard stream ends with one `Done=true` chunk: `Error != nil` means failure, and only a chunk without an error means success. On cancellation the channel closes and the terminal chunk is sent on a best-effort basis; `ctx.Err()` is authoritative. If you stop reading early, you must cancel the context.

`NewStreamRunnable` binds an execution source explicitly and uniformly supports Batch, Pipe, WithConfig and WithCallbacks. The execution source must emit serially, submit exactly one final chunk, and propagate emit errors; a missing final chunk, duplicate termination, errors after termination and callback failures never produce a success chunk.

The state and memory of one Builder belong to one session. ExecuteStream, Start and all AsRunnable adapters share a cancellable serial execution gate; the configured Timeout includes queueing time, and Batch preserves input order. For concurrency across sessions, create separate Builders. Tools or Builder callbacks must not re-enter the same session; if events are not fully consumed yet, cancel the original call before starting a new one. Initialize, Shutdown and configuration changes must still be scheduled by the caller before or after execution. Runnable callbacks are an additional orchestration-layer callback; callbacks configured on the Builder still run independently, so do not register the same side-effecting callback twice.

The current semantics of `Pipe` are to wait for the upstream final output and then run the downstream; it does not transform the upstream stream token by token. For general orchestration use the new [workflow.Graph[T]](../../workflow/README.md), which supports conditional branches, explicit parallel Clone/Merge and bounded loops; it still does not provide cross-node chunk-by-chunk Transform.

Orchestration and recovery by entry point:

| Component | Provided | Should not be inferred |
|---|---|---|
| `workflow.Graph[T].Compile` | Branches, parallel joins and bounded loops over the same state type; node event forwarding | Automatic mapping of heterogeneous fields, cross-node stream transforms, automatic persistence |
| `workflow.Graph[T].CompileDurable` | Node and parallel-branch checkpoints, conditional branches, loop budgets, approval and restart recovery; unified AsRunnable entry | Recovery of a node's internal call stack, replay of persisted stream events, automatic resume scheduling |
| `core/durable` | Persistence, approval and process-restart recovery for registered sequential steps | Automatically saving an arbitrary Agent call stack, automatically recovering workflow graphs |

`core/durable` persists through `durable.Store`: the main module's `durable.FileStore` targets a single-host local file system; for runs shared by multiple instances use goagent-contrib's [`durable/pgstore`](https://github.com/kart-io/goagent-contrib/tree/master/durable/pgstore) (lease locks; another instance can take over after the holder crashes). Steps execute with at-least-once semantics; external writes need server-side idempotency. `CompileDurable` saves state and node position through the same Store; topology changes reject old checkpoints, and function or state-structure changes require a version bump. Business writes must be deduplicated with `workflow.DurableKey(ctx)` or a business request ID; the host is responsible for approval authorization. See the [workflow recovery contract](../../workflow/README.md#durable-recovery-at-node-boundaries).

## Optional Context Governance

`WithOffload` adds large-result offloading and paged reading to the native tool stream (a PreStep component); history compaction is provided by the `compaction` component of goagent-harness `preset`. Off by default; the last request restores offloaded originals so the model is not left with references it can no longer read. See [Tool Context](CONTEXT_POLICY.md).

Starter code is in [Quickstart and Migration](QUICKSTART.md). For complex flows in the main module use [workflow.Graph[T]](../../workflow/README.md).

## Reducing Model Requests and Tokens

The OpenAI, DeepSeek, Gemini and Anthropic providers implement the optional `llm.ToolStreamClient`: in each turn the model can choose a tool or stream an answer directly. A typical single-tool task drops from three requests (call tool, confirm READY, generate answer) to two; when no tool is needed there is only one request. When tools are configured the Builder uses only this capability, and `AsRunnable().Invoke` and `ExecuteStream` share the same path.

The new path puts system instructions only in the system message, keeps history as native role messages, and links tool results to assistant.tool_calls through tool_call_id. Retrieval instructions and reference material are each sent once. reasoning_content required by the model is kept for tool continuation but is not shown as answer text. History and tool results are not silently truncated, and no answer cache is introduced.

`MaxIterations` limits all model requests in the native tool stream (including the final answer); when the budget runs out it fails explicitly instead of running extra tools whose results no next turn could use. An explicitly enabled single-tool direct return needs no next turn, so it may run after the last request. Cumulative usage covers all turns; the metadata `model_calls`, `usage_reports` and `usage_complete` indicate the call count and whether usage is complete; unknown usage is never fabricated.

When tool results already exist and only one model request is left, the Builder stops offering ordinary tools and spends the remaining budget answering from the existing results; if no direct-return tool is configured it marks `final_answer_only=true`. Explicitly configured direct-return tools remain selectable because they need no extra request to finish. A first-turn single-request direct return is not affected; with more budget, consecutive queries are still possible. If the last turn returns a tool call that cannot finish within that turn, it fails explicitly and does not execute it. This strategy does not guarantee the existing evidence is sufficient; business prompts must still require the model to say so honestly when evidence is missing.

Application system prompts should distinguish "return the tool result as is" from "reason from evidence": in the former, a legitimate status value must not be turned into a refusal just because it looks empty; in the latter, a successful tool return does not mean the evidence is sufficient. The framework does not override the caller's `WithSystemPrompt`.

The `model_start` step of the native stream marks a new model turn. The model may output explanatory text before a tool call, so all text deltas are transient; the final answer must be read from done.Output.Result. To display turn by turn, reset the current text panel on model_start. Interruptions, output truncation, corrupted arguments or duplicate call IDs never execute an incomplete tool call.

The dedicated OpenAI and DeepSeek providers share native SSE decoding and completion-status checks. DeepSeek can be wired directly with `providers.NewDeepSeekWithOptions` without borrowing the OpenAI constructor; the new streaming path performs no implicit retries. Other providers that do not implement ToolStreamClient keep their existing compatibility capabilities, and declaring streaming does not imply support for the native tool loop.

At runtime use `llm.HasToolStreaming(client)` to check the actual interface. The declared `structured_streaming` and `tool_streaming` correspond to `llm.Client`'s `CompleteStream` and ToolStreamClient respectively; the old `streaming` may mean string streaming only. An available interface does not guarantee that the selected model or a custom endpoint supports the same protocol; test against the business endpoint.

## Stream Capability Declaration

Query via the optional `core.StreamCapability.StreamMode()`:

| Entry | Capability | Boundary |
|---|---|---|
| Builder.AsRunnable | tokens | Native answer deltas; with ToolStreamClient, tools and answers share the native conversation loop, other providers keep the compatibility path |
| workflow.Graph.Compile | steps | Node events and final state; does not mean cross-node token-by-token transforms |
| DurableGraph.AsRunnable | steps | Waiting or completed snapshot of this call; check Data.Done for business terminal state |

Removed modules such as ReAct, ToT and self-reflection are no longer listed in the current capability matrix. Pipe/Sequence still execute stage by stage, and the last stage keeps its actual events; there is no cross-node token-by-token transform.

## Capability Status

"Verified" here means local regression tests exist; it does not mean production-load validation has been done.

| Status | Capability | Verified scope and limits |
|---|---|---|
| Verified entry | Builder RAG, native event stream, Runnable adapter | Source limits, usage, errors, cancellation and composed execution; does not automatically prove factual correctness |
| Verified entry | Official MCP client adapter | HTTP/stdio tool interoperability; interactive authorization is still handled by the application |
| Verified entry | core/durable | Explicit sequential steps, approval and real process restarts; at-least-once execution, external services deduplicate by idempotency key |
| Lexical baseline | LexicalOverlapReranker (goagent-contrib/retrieval) | Lexical overlap scoring, no cross-encoder inference |
| Deprecated | CrossEncoderReranker (goagent-contrib/retrieval) | Kept as a migration entry, no longer recommended for new development |

The `reflection` package containing the self-reflection Agent was removed in v0.17.0 without replacement; the code remains at `v0.16.1`.

The old `NewCrossEncoderReranker(model, topN)` migrates to `NewLexicalOverlapReranker(topN)`; for real model-based ranking use `NewLLMRerankerWithClient` or `CohereReranker`. All three are in goagent-contrib: the first two in `retrieval`, `CohereReranker` in `retrieval/cohere`.

OpenAI-compatible providers now send `tools` and parse `tool_calls`, while still parsing old `function_call` responses. The tool stream merges concurrent calls by index, and in-stream errors are returned as error events. Services that only accept the old request protocol need to upgrade to a compatible endpoint; the model is never called twice through hidden retries.

## Comparison and Next Steps

A terminal tool can use `WithReturnDirectTools("order_status")` to save one model request:

```go
agent, err := builder.NewSimpleBuilder(client).
    WithTools(orderStatusTool).
    WithReturnDirectTools("order_status").
    WithMaxIterations(1).
    Build()
// After checking err, run it with agent.AsRunnable().Invoke(ctx, question).
```

This applies only to the native tool path of `ExecuteStream/AsRunnable`; Build verifies that the provider supports `ToolStreamClient` and that the names are registered uniquely. When the turn has exactly one tool call and it is on the list, a successful tool's string is output directly and other results are JSON-encoded; tool failures, empty results and cancellation are never turned into success. The final answer must already meet business display requirements: the model will not summarize, add citations or apply output-format instructions afterwards. Multiple tool calls continue the model loop, so they need a larger MaxIterations.

A direct return still passes through memory, state, checkpoints, middleware and callbacks; the metadata marks `answer_source=tool` and `return_direct_tool`. Model explanations before the tool remain transient text; `done.Output` is authoritative. [Cost-control research on other frameworks](../evaluation/2026-10-26-framework-cost-research.md) explains the rationale and follow-up work.

[GoAgent evaluation](../../evaluation/agentbench/README.md) uses a fixed question set and request budget to validate the Builder's native tool path. The evaluation is part of the main module; it is not a pure framework-overhead benchmark.

The framework does not provide cross-instance scheduling, task distribution or automatic resume: in multi-instance deployments share a Store such as `pgstore`, and the host discovers interrupted runs and triggers recovery with the same execution key.


## General Graph Execution

`workflow.NewGraph[T]().Compile(ctx)` produces a standard StreamRunnable; single calls, step streams, batching, composition and callbacks share the same execution source. AddRunnable plugs in nodes of the same type, and Adapt explicitly converts the input and output of heterogeneous Agents. Default MaxSteps=100; each node entry counts as one step; exceeding it can be checked with errors.Is(err, agentErrors.ErrWorkflowMaxSteps).

Conditional routing can only return predeclared targets and selects one successor at a time; ordinary multiple in-edges do not wait for unselected paths. Parallel nodes must provide Clone and Merge and join in registration order; MaxConcurrency defaults to 4 and bounds the workers of each parallel node. A parallel failure cancels sibling branches in the same group and waits for them to exit; functions must respond to context.

Node events are passed through with a Step scope; a node's Done is not the graph's Done. The graph mode is declared as steps and does not mean token-by-token execution along edges. Missing/duplicate terminal chunks and late errors from custom Runnables block downstream nodes. Pointers/maps/slices in the original data are not deep-copied automatically; `Compile` has no implicit retry, transactional rollback, automatic persistence or cross-host takeover; JSON state and recovery limits of the persistent entry `CompileDurable` are described above.

See the [full API and state boundaries](../../workflow/README.md) and the [offline example](../../examples/workflow/main.go).

## Native Tool Execution and Durable Graph Events

`tools.Dispatcher` provides a single dispatch entry for graph nodes and Builder tools; see [Native Orchestration and Tooling](NATIVE_ORCHESTRATION.md). No retries by default, failed results cannot become success, and the tool execution key is injected from the host context.

The Stream Done of `DurableGraph.AsRunnable` means this call has ended; check `Data.Done` for business completion. An approval wait returns `Data.Waiting=true`, `Data.Done=false`, and the compatibility `Run` keeps `agentErrors.ErrApprovalRequired`. Saved parallel branches are skipped on recovery; Merge stays side-effect free and may be redone; the model loop inside a node still runs as a whole.

## Composable Orchestration R2

Adds root budgets, controlled Runnable tools, OTel observation and immutable context result references; see the [Composable Orchestration guide](COMPOSABLE_ORCHESTRATION.md). Context result offloading uses `WithOffload`; in durable runs the read tool goes through the host Dispatcher together with business tools. Stream previews still do not represent business terminal state; core/flow is only an experimental prototype.

The root budget applies to all execution entries of the Builder.

## Execution Reliability and Governance Updates

Initialization no longer ignores checkpoint read failures; memory can opt into strict mode. Pipe/Sequence reuse the common stream terminal and cancellation rules; explicit parent callbacks and parent Batch concurrency configuration take effect. Adds budget limits, read-only run diagnostics, two-phase tool admission and bounded asynchronous observation.

For interfaces and migration details see the [Extension Contract](EXTENSION_CONTRACT.md) and the [Execution Governance guide](EXECUTION_GOVERNANCE.md). The old synchronous entry has been removed; full results are collected uniformly through `AsRunnable().Invoke`; for migration see the [Quickstart](QUICKSTART.md#migrating-from-legacy-apis).

## Final Result Acceptance

`WithOutputValidator` checks the final output at the middleware and completion-callback boundary, covering plain text, tool direct returns, early termination and replay; it is not affected by TurnStopping's correction budget. Callbacks can rewrite the output, so it is validated again before final delivery; on failure no successful terminal state is published; effects that already happened are not rolled back. For signatures, plain-text capability migration and the recovery checklist see the [Acceptance and Governance guide](EXECUTION_ACCEPTANCE.md).
