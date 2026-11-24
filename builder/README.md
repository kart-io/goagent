# builder: Agent Builder

[English](README.md) | [中文](README_CN.md)

`builder` builds a `ConfigurableAgent` through a fluent API and implements the native tool loop, loop extension points, and run handles. For a quick reference of methods and defaults see the [Builder API Reference](../docs/guides/BUILDER_API_REFERENCE.md); to get started see the [Quick Start](../docs/guides/QUICKSTART.md); for a comparison of what each execution entry point supports see the [Execution Contract](../docs/guides/EXECUTION_CONTRACT.md).

## Core Types

- `AgentBuilder[S core.State]`: generic builder; `S` is the state type. `NewSimpleBuilder(client)` returns a `SimpleAgentBuilder` (`AgentBuilder[*core.AgentState]`).
- `ConfigurableAgent[S]`: the result of `Build` / `BuildContext`. `Build` validates the configuration, assembles the middleware chain, and calls `Initialize`. Session persistence and recovery use `WithSessionLog` and `WithDurableScope`; long-term memory uses `WithLongTermMemory`.
- `AgentConfig`: configuration; `DefaultAgentConfig()` sets no limits: MaxIterations 0 and Timeout 0 mean unlimited, and MaxTokens and Temperature are not sent unless set explicitly, so the LLM client configuration decides. Limits are caller policy; pass `WithMaxIterations(10).WithTimeout(5*time.Minute)` to keep the values used before v0.60. The quick constructors (`NewRAGAgent`, `NewChatAgent`, ...) still apply those values.

## Usage

```go
agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt("You are a professional data analyst").
    WithTools(calculatorTool, searchTool).
    WithMaxIterations(5).
    WithTimeout(2 * time.Minute).
    WithTemperature(0.5).
    WithOutputFormat(builder.OutputFormatJSON).
    WithSessionLog(session.NewMemoryStore()).
    WithSessionID("session-001").
    WithMiddleware(middleware.NewTimingMiddleware()).
    Build()
if err != nil {
    return err
}
output, err := agent.AsRunnable().Invoke(ctx, "Analyze this data...")
```

Output formats: `OutputFormatPlainText`, `OutputFormatMarkdown`, and `OutputFormatJSON` append the corresponding instruction after the system prompt; `WithCustomOutputFormat("Output the result in YAML format")` uses a custom instruction.

For the exact configuration of the scenario presets `ConfigureForRAG`, `ConfigureForChatbot`, `ConfigureForAnalysis` and the quick functions `QuickAgent`, `ChatAgent`, `RAGAgent`, `AnalysisAgent`, `WorkflowAgent`, `MonitoringAgent`, `ResearchAgent`, see the [API Reference](../docs/guides/BUILDER_API_REFERENCE.md#scenario-presets-and-quick-functions).

## Execution Entry Points

| Entry point | Client requirement | Behavior |
|---|---|---|
| `AsRunnable().Invoke/Stream/Batch/Pipe` | `llm.Client` | Recommended entry point; uses the native tool loop when tools are configured and the client implements `llm.ToolStreamClient` |
| `ExecuteStream` | `llm.Client` | Channel form of the same event source (`sources`, `step`, `text`, `usage`, `error`, `done`) |
| `Start` | `llm.ToolStreamClient` with tools configured | Runs in the background and returns a `*Handle` |

All entry points execute serially per session, and time spent waiting in the queue counts toward Timeout; for concurrency across sessions, create separate Agents.

`Build` / `BuildContext` save a configuration snapshot for the generated Agent; later changes to the Builder's options, tool list, callback list, and top-level metadata do not retroactively modify existing Agents. The client, state, store, memory, callback objects, and business objects inside metadata are still shared by reference; across sessions, create separate Builders and provide separate state as needed.

## In-Run Interaction and Loop Extension Points

The following capabilities apply only to the native tool stream (`llm.ToolStreamClient` with tools configured).

```go
h := agent.Start(ctx, "Fix the login page styles")
_ = h.Steer(ctx, "Also check mobile")              // appended as a user message after the current tool batch; wakes the run to continue if the model is about to finish
_ = h.Inject(ctx, "The user prefers a dark theme") // sent with the next request; does not wake the run
_ = h.Redirect(ctx, "Don't touch the CSS yet")     // cancels the in-flight model request, keeps the text already streamed, then re-requests; degrades to Steer while tools are running
_ = h.Followup(ctx, "Write a test as well")        // used as the input of a new turn after this turn ends, continuing the conversation
out, err := h.Wait(ctx)                            // h.Interrupt(cause) aborts the run
pending := h.PendingInputs()                       // independent snapshot: inputs still queued and not yet claimed by the execution loop
```

A successful enqueue does not mean the input has been processed. Inputs can be left over because Inject does not wake the run, or because a Steer arrived too late or the quota was exhausted; unclaimed Followups are also kept on failure. `PendingInputs` neither consumes nor automatically continues, and inputs that were claimed but failed are not included. The context passed to `Wait` only controls waiting; a wait timeout does not mean the Handle has finished. The inbox is an in-process object and does not provide reliable delivery across restarts; see [Composition and Input Contract](../docs/guides/AGENT_COMPOSITION.md).

After `Interrupt`, the Handle waits for the current ExecuteStream to exit (including the tools and cleanup it runs synchronously) before completing Wait. Host tools and hooks must cooperate and return; to bound the waiting time, give Wait its own context. The cancellation cause and any execution errors already received are kept in the error chain; cancellation is not interpreted as the business effects having been undone.

| Option | When | Capability |
|---|---|---|
| `WithPreStep` / `WithIdentifiedPreStep` | After claiming inbox inputs, before sending the request | Rewrite messages (e.g. compression), or `Halt` to finish without calling the model; the identified version refuses to resume old durable runs after the configuration changes |
| `WithOffload` | Same as PreStep | Replace oversized tool results with references; see [Context Policy](../docs/guides/CONTEXT_POLICY.md) |
| `WithRequestHook` | Before sending the model request | Replace the client (routing, switching models), adjust parameters |
| `WithRequestError` | Model request failed | Decide to retry, compress (replace messages), or fall back (replace the client) based on the classification from `llm.ClassifyError`; `RetryTransient` is the component that retries transient failures unchanged |
| `WithTurnStopping` | Model gave a final answer and no Steer is pending | Return a user message to continue (goal-driven, Stop hook) |
| `WithPromptSections` | Computed once at the start of each turn | Appended to the system prompt; bytes stay unchanged within a turn to keep the prompt-cache prefix |
| `WithPromptContexts` | Recomputed every step | Attached as a user message at the end of this request; does not enter later history |

In durable runs, PreStep, TurnStopping, prompt sections and contexts, per-step claimed Steer/Inject, and wake decisions are recorded in the journal and replayed on recovery instead of being recomputed; a durable run started with `Start` must also be resumed with `Start`. In durable runs, `Steer`, `Inject`, `Redirect`, and `Followup` return success only after writing to the durable inbox, and return an error if the write fails; `SteerOnce` additionally deduplicates by key, so redelivery after the sender crashes does not produce a second message; calling `Start` again with the same execution key restores unclaimed inputs and continues Followup turns by the recorded batches, so the host does not need to rebuild them and should not resubmit. Request hooks must be deterministic; RequestError's unchanged retries happen within the same turn record, and in durable runs a decision to replace messages or the client ends with an error; Redirect only takes effect in non-durable runs (in durable runs it degrades to Steer). A model request interrupted by process stop or crash is not resent by default (it may already have been billed), and recovery reports `errors.ErrOperationAttention`; `WithInterruptedModelResends(n)` allows up to n resends (n=2 means at most 3 sends per turn), each consuming its own share of the budget, and n must stay the same for the whole lifetime of the run. When a subflow waits for the host to continue, execution pauses with `errors.ErrAgentWaiting` (`*WaitingError`); see [Composable Orchestration](../docs/guides/COMPOSABLE_ORCHESTRATION.md).

## Session Log

`WithSessionLog(store)` records the session in the event log of [`core/session`](../core/session/README.md): model history is derived from the log (memory is no longer read), the conversation is recorded before each model request wherever it differs from the log, and rewrites of history by PreStep and others are recorded as `history/rewrite`, so every request can be rebuilt from the log. Durable runs can use `session.DurableStore(store, id)` as the durable journal, so operation snapshots and the conversation live in the same log; `session.BuildTrajectory` exports the log as a training trajectory. Both plain streaming chat and the native tool loop are supported; plain chat needs no placeholder tools.

Adding `WithSessionState()` restores state across processes along with the session log: at the end of each turn the state is encoded into `turn/end`, and at the start of the next turn, if the state in the log is not the record corresponding to the current state (new process, switched session, or rewound), it replaces the current state; sessions with no record start from the state given at build time. Encoding is determined by the state type's `MarshalJSON`/`UnmarshalJSON`; `*core.AgentState` encodes as a JSON object and numbers become float64 after restoring; to preserve field types, define a custom state type that implements these two methods.

Each Agent writes only to the log it was explicitly configured with; sub-Agents and Handles called from tools do not inherit write access to the parent turn's log. When a sub-Agent needs its own log while sharing the same Store, the host should bind a separate session identity for the child call with `tools.WithSessionID(ctx, childID)`.

## Code Layout

```text
builder/
├── builder.go              # AgentBuilder, Build/BuildContext
├── agent_options.go        # prompts, state, context, callbacks, metadata
├── config_options.go       # AgentConfig and run configuration
├── runtime_options.go      # WithMiddleware
├── tool_options.go         # WithTools
├── configurable_agent.go   # middleware chain execution and lifecycle
├── runnable.go             # AsRunnable
├── events.go               # ExecuteStream and events
├── tool_events.go          # native tool loop
├── loop_hooks.go           # PreStep, Request, RequestError, TurnStopping, prompt sections and contexts
├── handle.go               # Start and Handle
├── offload.go              # WithOffload
├── compaction.go           # WithCompaction
├── long_term_memory.go     # WithLongTermMemory
├── retrieval.go            # WithRetriever
├── session_log.go          # WithSessionLog, WithSessionState
├── durable_turn.go         # durable model turns
├── model_call.go           # model call counting and usage recording
├── model_budget.go         # WithTokenEstimator
├── waiting.go              # WaitingError
├── presets.go              # ConfigureFor* scenario presets
├── quick_builders.go       # quick creation functions
└── contextpolicy/          # Offload, Compaction, recall_history, and the result store
```

## Multi-Agent Usage Hook

`WithUsageHook(func(ctx context.Context, e budget.UsageEvent))` directly receives per-attempt tokens and the available cumulative statistics; parent hooks are inherited by sub-Agents. There is no need to filter Observer events yourself; query errors and callback panics do not change the business result. Per-call usage is still provided when no budget is bound; see the guide below.

`WithAgentID` identifies the role; sub-Agents share the budget and Observer through the parent context. model_attempt_start/model_usage provide the attempt key, parent/child identity, and field evidence; reliable totals come from the Attribution query on the unified budget. Self, Inclusive, and Root must not be added together; see the [guide](../docs/guides/MULTI_AGENT_USAGE.md) and the [offline example](https://github.com/kart-io/goagent-examples/tree/main/agent-usage).

Token usage hooks can also read the provider-reported `ReasoningTokens`, `ToolUsePromptTokens`, and `ResponseModel`; check the corresponding `KnownFields` first. Use `WithReasoning(usage.ReasoningConfig{Effort: "low"})` to set explicit reasoning control for this Agent; Gemini thinking budgets use `BudgetSet: true` and `BudgetTokens`. Unsupported clients return an explicit error. See the [usage guide](../docs/guides/MULTI_AGENT_USAGE.md).
