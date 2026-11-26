# GoAgent Builder API Reference

[English](BUILDER_API_REFERENCE.md) | [中文](BUILDER_API_REFERENCE_CN.md)

Quick reference for the `builder` package's methods, grouped by purpose. `go doc github.com/kart-io/goagent/builder` is authoritative for signatures; for design notes see the [builder README](../../builder/README.md), and to get started see the [Quickstart](QUICKSTART.md).

## Creation

| Function | Description |
|---|---|
| `NewSimpleBuilder(client llm.Client) *SimpleAgentBuilder` | Common entry point, equivalent to `NewAgentBuilder[*core.AgentState]` |
| `NewAgentBuilder[S core.State](client)` | Custom state type `S`; `WithState` is required when `S` is not `*core.AgentState` |
| `Build() (*ConfigurableAgent[S], error)` | Equivalent to `BuildContext(context.Background())` |
| `BuildContext(ctx) (*ConfigurableAgent[S], error)` | Passes the host context to initialization callbacks and rejects nil or canceled contexts; see [Host Integration](HOST_INTEGRATION.md) |

State is kept in process only; Build neither loads nor saves state. For session persistence use `WithSessionLog` and `WithDurableScope`.

## Basic Configuration

| Method | Default | Description |
|---|---|---|
| `WithSystemPrompt(string)` | empty | System prompt |
| `WithTools(...interfaces.Tool)` | none | Appends tools |
| `WithMaxIterations(int)` | 10 | Upper bound on model requests, including the final answer; non-positive values are ignored |
| `WithTimeout(time.Duration)` | 5 minutes | Timeout per execution; time spent waiting for the session serialization lock also counts |
| `WithMaxTokens(int)` | unset, uses client config | Maximum model output tokens |
| `WithTemperature(float64)` | unset, uses client config | Sampling temperature; an explicit `0` is supported; the Builder value takes precedence over the client config |
| `WithOutputFormat(OutputFormat)` | `OutputFormatDefault` | Appends format instructions after the system prompt: `OutputFormatPlainText`, `OutputFormatMarkdown`, `OutputFormatJSON` |
| `WithCustomOutputFormat(string)` | none | Custom format instructions (`OutputFormatCustom`) |
| `WithInputSerializer(InputSerializer)` | `DefaultInputSerializer` | Converts arbitrary input into prompt text |
| `WithVerbose(bool)` | false | Adds logging and timing middleware |

## Native Tool Streaming

The following options apply only to native tool streaming: the client implements `llm.ToolStreamClient` (OpenAI, DeepSeek, Gemini, Anthropic providers), tools are configured, and execution goes through `AsRunnable`, `ExecuteStream`, or `Start`.

| Method | Description |
|---|---|
| `WithParallelToolCalls(n int)` | Concurrency limit for tool calls within one round; default 0 means sequential. Calls run concurrently only when every tool implements `interfaces.ConcurrentCallTool` (such as the `Dispatcher`'s tools) |
| `WithReturnDirectTools(names ...string)` | A single terminal tool's result is used directly as the answer, skipping the summarization request; the tool name must be registered uniquely |
| `WithEmptyAnswerRetries(n int)` | Number of re-asks when the model returns an empty answer; default 0, and each re-ask consumes one iteration |
| `WithOffload(*contextpolicy.Offload)` | Replaces oversized tool results with references read back page by page through a read tool; `offload` must also be registered as a tool, see [Context Governance](CONTEXT_POLICY.md) |
| `WithPreStep(...PreStepHook)` | Rewrites `Step.Messages` before each model request, or ends directly with `PreStepResult{Halt: true}` |
| `WithIdentifiedPreStep(identity, hook)` | PreStep with an identity; after the identity changes, resuming an old durable run fails with `intent changed` |
| `WithRequestHook(...RequestHook)` | Replaces `ModelRequest.Client` or modifies `Request` parameters before sending |
| `WithRequestError(maxRecoveries int, ...RequestErrorHook)` | Decides on a model request failure whether to retry, replace messages, or replace the client; `maxRecoveries` is the per-step recovery limit (default 3) |
| `RetryTransient(retries, backoff)` | Retries unchanged the failures that `llm.ClassifyError` classifies as transient (429, 5xx, connection errors); does not retry once text has been emitted |
| `WithTurnStopping(...TurnStoppingHook)` | When the model gives its final reply, a non-empty returned string is sent as a user message to continue the run |
| `WithPromptSections(...PromptSection)` | Computed once at the start of each turn and placed in system, keeping the prompt cache prefix stable |
| `WithPromptContexts(...PromptContext)` | Recomputed every step and attached as one user message at the end of the current request; not added to history |
| `WithSessionLog(session.Store)` | Writes the session to the [core/session](../../core/session/README.md) event log; model history is derived from the log |
| `WithTokenEstimator(TokenEstimator)` | Request token estimation when the root budget enables a token limit |
| `WithDurableScope(string)` | Required for durable execution: a stable name for each logical call within the same durable node |

In durable runs, PreStep, TurnStopping, prompt sections, and contexts are logged per step and replayed on recovery; RequestError's unchanged retries happen within the same turn record, while decisions to replace messages or the client end with an error.

```go
offload, err := contextpolicy.NewOffload(contextpolicy.OffloadConfig{
    Store:              contextpolicy.NewMemoryStore(),
    MaxToolResultRunes: 8000,
})
if err != nil {
    return nil, err
}
return builder.NewSimpleBuilder(client).
    WithSystemPrompt("You are a customer support assistant").
    WithTools(searchTool, offload). // offload is itself a read tool and must be registered too
    WithMaxIterations(12).
    WithParallelToolCalls(4).
    WithOffload(offload).
    WithSessionLog(session.NewMemoryStore()).
    WithPromptSections(builder.PromptSection{
        Name: "rules",
        Text: func(builder.PromptScope) string { return "Only answer questions about orders." },
    }).
    WithRequestHook(func(ctx context.Context, req *builder.ModelRequest) error {
        req.Request.Temperature = 0.2
        return nil
    }).
    WithRequestError(3, builder.RetryTransient(3, func(retry int) time.Duration {
        return time.Duration(retry) * 200 * time.Millisecond
    })).
    WithTurnStopping(func(ctx context.Context, s *builder.Stopping) (string, error) {
        if s.Answer == "" && s.Continued == 0 {
            return "Please give your final answer.", nil
        }
        return "", nil
    }).
    Build()
```

## Retrieval, Memory, and State

| Method | Default | Description |
|---|---|---|
| `WithRetriever(interfaces.DocumentRetriever, ...RetrievalConfig)` | 4 documents, 12000 characters | Retrieves evidence on every request and writes sources to the output metadata `retrieval_sources`; `RetrievalConfig.Filter` builds metadata filter conditions per request and requires the retriever to implement `interfaces.RequestRetriever` |
| `WithSessionLog(session.Store)` | none | Multi-turn history comes from the session log; without it each call has no history |
| `WithCompaction(*contextpolicy.Compaction)` | none | Compresses earlier messages when the conversation exceeds the threshold; applies only to native tool streaming |
| `WithLongTermMemory(*memory.Memory)` | none | Cross-session long-term memory: recalls each turn and registers an extraction task at turn end; requires `WithSessionLog` and `memory.WithNamespace` in the request context; recall applies only to native tool streaming; Request hooks must not rewrite or move the recall message |
| `WithModelCache(builder.ModelCache)` | none | Model call cache keyed by the full request; `Scope` declares a trusted isolation identity (defaults to the long-term memory namespace when long-term memory is configured); `Erase` is `EraseOff` / `EraseAuto` (default) / `EraseRequired`; with tag-based deletion `TagKey` must be at least 32 bytes; `Build` validates the config, see the [Caching Guide](CACHING_GUIDE.md#model-call-caching) |
| `WithSessionID(string)` | `session-<nanosecond timestamp>` | Default session ID for the session log; `tools.WithSessionID` in the request context takes precedence |
| `WithState(S)` | `core.NewAgentState()` | Initial state |

Multi-step tasks that must resume after a process restart use `core/durable` and `workflow`; see [Native Orchestration](NATIVE_ORCHESTRATION.md).

## User Messages with Images

Pass a `builder.UserInput` (or a pointer to it) as the input of `ExecuteStream` to attach images to the user message; any other input type still goes through the configured `InputSerializer`.

```go
agent, err := builder.NewSimpleBuilder(client).
    WithSessionLog(sessions).
    WithImageResolver(resolve, builder.ImagePolicy{Identity: "images/v1", KeepRecentTurns: 3}).
    Build()
events, err := agent.ExecuteStream(ctx, builder.UserInput{
    Text:   "What is in this picture?",
    Images: []llm.ImagePart{{Ref: "image:42", MIME: "image/png", SHA256: sum}},
})
```

- **References, not URLs.** The history, the session log (including `TurnStart.Images`), the durable intent, and the messages seen by `PreStep` and Request hooks keep the `llm.ImagePart` as given, normally with `Ref` set and an empty `URL`. Turn identity is the pair (`Text`, `Images`).
- **Resolution.** `WithImageResolver(r, policy)` registers `ImageResolver func(ctx, ImageRequest) (url string, err error)`. It runs only while the outgoing model request is built, right before every real request, and fills `URL` (an https address or a `data:` URI) for that request only. It is not called while a recorded turn replays, and a model call cache hit does not call it again. Each call to `WithImageResolver` replaces the previous resolver and policy; a nil resolver or a negative policy limit fails `Build` with `CodeInvalidConfig`.
- **`ImageRequest`.** `Part` is the image as recorded; `TurnAge` counts session turns back from the current one (0 is the current turn's input, 1 the previous turn's, and so on). Turns are the `turn/start` entries of the session log; steering, corrections, continuations, memory recall, and summaries do not start a turn.
- **Errors.** Returning `builder.ErrImageUnavailable` (possibly wrapped) means the reference can never be resolved, for example the image was deleted or no longer matches its digest; the image becomes a placeholder and the request goes on. Any other error is treated as transient: the request fails and goes through the RequestError hooks like a provider error.
- **Without a resolver.** An image without a URL, or an empty image part, fails the run with `CodeInvalidInput` before the session log is written; this covers a part with neither `Ref` nor `URL`, and a part with only a `Ref` when no resolver is configured. A resolver that returns an empty URL fails the request with `CodeInvalidOutput`. Providers also refuse a part with `Ref` set and `URL` empty.

`ImagePolicy` fields:

| Field | Description |
|---|---|
| `Identity` | Identifies the resolver and policy configuration (for example a version and encoding settings). It enters the model call cache key, so cached turns are not shared across configurations; it is not part of the durable intent |
| `KeepRecentTurns` | Attach images only from user turns whose `TurnAge` is at most this value; older images become placeholders. 0 attaches all |
| `MaxImagesPerRequest` | Cap on attached images per request, keeping the newest; 0 means no limit |
| `Placeholder` | `func(p llm.ImagePart, reason string) string` renders the text appended to the message in place of a degraded image; `reason` is `不可用` (`ErrImageUnavailable`) or `不在当前上下文中` (outside `KeepRecentTurns` or `MaxImagesPerRequest`). When nil, the text is `[图片 <ref>：<reason>]` |

**Compaction.** `contextpolicy.CompactionConfig.ImageTokens` is the estimated token count of one attached image, added on top of `Estimate` for every image of a message; 0 means `constants.DefaultImageTokens` (1500). In the transcript given to the summary model, each image is appended after the message text as `[图片 <ref>]` (`[图片]` when it has no ref), and the summary request carries no images.

**Management adapter.** The native management adapter (`management/adapters`) keeps a JSON string task input as plain text. A task input that is a JSON object with an `images` key is decoded strictly into a `builder.UserInput`:

```json
{"text": "What is in this picture?", "images": [{"ref": "image:42", "mime": "image/png", "sha256": "..."}]}
```

`images` must be an array and every entry needs a non-empty `ref`; `mime` and `sha256` are optional. A `url` inside an image entry, or any unknown key, is rejected with `CodeInvalidInput`, so outgoing addresses come only from the host's resolver. An object without an exact lowercase `images` key is not treated as a `UserInput`; like any other JSON input it keeps its previous behavior and is rendered as a value.

Provider support:

| Provider | Images |
|---|---|
| `OpenAIProvider` | Declares `CapabilityVision`; images are sent as image parts |
| MiniMax | Declares `CapabilityVision`; a request with images sends `max_completion_tokens` (the only limit the vision endpoint reads) instead of `max_tokens` |
| Kimi, SiliconFlow, Ollama presets | Do not declare vision; image parts are forwarded, and whether they are accepted depends on the model |
| Anthropic | Rejected |
| Native Gemini | Rejected |
| DeepSeek | Rejected |

## Middleware, Callbacks, and Metadata

| Method | Description |
|---|---|
| `WithMiddleware(...middleware.Middleware)` | `core/middleware` middleware chain, e.g. `NewLoggingMiddleware`, `NewTimingMiddleware`, `NewRateLimiterMiddleware` |
| `WithCallbacks(...core.Callback)` | Execution callbacks, e.g. `core.NewStdoutCallback`, `core.NewMetricsCallback` |
| `WithErrorHandler(func(error) error)` | Wraps or replaces execution errors |
| `WithMetadata(key, value)` | Writes agent metadata |

## Scenario Presets and Quick Functions

| Method / function | Configuration |
|---|---|
| `ConfigureForRAG()` | Timing middleware, MaxTokens 3000, Temperature 0.3 |
| `ConfigureForChatbot()` | Rate limiting (20 per minute) and input length validation, Temperature 0.8 |
| `ConfigureForAnalysis()` | Timing and output structuring middleware, Temperature 0.1, MaxIterations 20 |
| `QuickAgent(client, systemPrompt)` | System prompt only |
| `ChatAgent(client, userName)` | Chat prompt + `ConfigureForChatbot` |
| `RAGAgent(client, retriever)` | `ConfigureForRAG` + retrieval; `retriever` may be a `DocumentRetriever`, an object with a `Retrieve` method, or a `VectorStore` (top 4); with nil only the prompt is set |
| `AnalysisAgent(client, dataSource)` | `ConfigureForAnalysis`, data source written to state |
| `WorkflowAgent(client, workflows)` | Logging, circuit breaker, and validation middleware, MaxIterations 15 |
| `MonitoringAgent(client, checkInterval)` | Rate limiting and timing middleware, MaxIterations 100, Temperature 0.3 |
| `ResearchAgent(client, sources)` | Timing middleware, MaxTokens 4000, Temperature 0.5, MaxIterations 15 |

Quick functions return `*SimpleAgent` (`ConfigurableAgent[*core.AgentState]`).

## ConfigurableAgent

| Method | Description |
|---|---|
| `AsRunnable() *core.StreamRunnable[any, *AgentOutput]` | Recommended entry point: `Invoke`, `Stream`, `Batch`, `Pipe`; accepts any `llm.Client` |
| `ExecuteStream(ctx, input) (<-chan AgentEvent, error)` | Channel form of the same event source |
| `Start(ctx, input) *Handle` | In-process background run; `Handle` provides `Steer`, `Inject`, `Redirect`, `Followup`, `Interrupt`, `Wait`, `PendingInputs`. Supports native tool streaming only; it is not a durable background scheduling service |
| `Initialize(ctx)`, `Shutdown(ctx)` | Lifecycle; Build has already called Initialize |
| `GetState()`, `GetMetrics()` | Current state and metrics |

All execution entry points of the same agent are serialized per session; for cross-session concurrency create separate agents. For an entry point behavior comparison see the [Execution Contract](EXECUTION_CONTRACT.md).

`Handle.PendingInputs() []builder.PendingInput` returns a snapshot of the queues not yet picked up by the loop; each item has a `Kind` (steer/inject/redirect/followup) and `Text`. Order is preserved within a queue; no global order is promised across queues. Querying does not consume, and being picked up does not mean processed successfully. Enqueued items and snapshots are not persisted; inputs remaining after the run converges are handled by the host, see the [Composition Guide](AGENT_COMPOSITION.md).

`Interrupt` requests cooperative cancellation; `Wait` still waits for the current turn's execution source to exit and for synchronous tool cleanup, and a timeout on the wait context only ends the wait. Durable Followup chains must be rebuilt by the host in their original batches; Start/WithResume does not automatically resume their scheduling.

## Custom State

```go
type CustomState struct {
    *core.AgentState
    Tenant string
}

agent, err := builder.NewAgentBuilder[*CustomState](client).
    WithState(&CustomState{AgentState: core.NewAgentState(), Tenant: "t1"}).
    WithSystemPrompt("You are an enterprise assistant").
    Build()
if err != nil {
    return err
}
fmt.Println(agent.GetState().Tenant)
```

## Agent Identity and Usage

`WithUsageHook(budget.UsageHook)` receives per-call model evidence and, when available, cumulative Attribution reports; it is read-only, concurrency-safe, and inherited by sub-agents, where a non-nil child hook replaces the inherited one. You can also bind it to a context with `budget.WithUsageHook`; this does not replace the Observer. See the [dedicated hook contract](../design/TOKEN_USAGE_HOOK.md).

`WithAgentID(id)` sets the logical role, independent of DurableScope; binding a budget automatically establishes call identity, and unnamed roles are anonymous. `observe.Event.Agent/Usage` and model_attempt_start/model_usage provide read-only metadata; `budget.Config.MaxAgentCalls` limits the number of call nodes, all budgets store field evidence uniformly, and `Controller.Attribution` returns Self/Inclusive/Root plus unknown-field statistics. `AsRunnable().Invoke` and the streaming entry points share the same metering path. See the [Multi-Agent Usage Guide](MULTI_AGENT_USAGE.md).

### Explicit Reasoning Configuration

`WithReasoning(usage.ReasoningConfig)` overrides the current agent's request configuration. OpenAI-compatible/DeepSeek use `Effort`; Gemini uses a supported level, or `BudgetSet: true` with `BudgetTokens`. Unsupported clients reject it explicitly. The configuration is copied per request, so a RequestHook changing the current settings does not pollute the default.

A UsageHook can read `Model.ResponseModel`, `Model.ReasoningConfigured`, `Model.Reasoning`, `Model.Tokens.ReasoningTokens`, and `Model.Tokens.ToolUsePromptTokens`. Check `KnownFields` first, and do not add optional sub-items into TotalTokens again. For details see the [explainability contract](../design/TOKEN_USAGE_EXPLAINABILITY.md).
