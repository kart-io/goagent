# GoAgent User Guide

[English](USER_GUIDE.md) | [中文](USER_GUIDE_CN.md)

New applications compose business logic with Builder.AsRunnable and typed Workflows. For the required Go version see [go.mod](../../go.mod); for exact signatures see [Core API](../api/CORE_API.md) and the [Builder Reference](BUILDER_API_REFERENCE.md).

## Choosing an Entry Point

| Need | Entry point |
| --- | --- |
| Model answers, retrieval, native tool loop | `builder.NewSimpleBuilder(client)` → `BuildContext(ctx)` → `AsRunnable().Invoke/Stream` |
| Fixed steps, branches, loops, and parallelism | `workflow.Graph[T]` → Compile |
| Node/branch-level recovery, approvals | CompileDurable with durable model/tool records |
| Sub-agent or subgraph as a capability | NewRunnableTool / NewRunnableToolFactory, called through the Dispatcher |

## Running the First Example

The following commands use the in-repo simulated model and need no API key:

```bash
go run ./examples/execution-scope
go run ./examples/workflow
```

To connect a real model see the [Quickstart](QUICKSTART.md), [LLM API](../api/LLM_API.md), and [Provider Capabilities](PROVIDER_CAPABILITIES.md). Implementing `llm.Client` does not mean supporting ToolStreamClient; for tool scenarios, first check the specific provider's native capabilities.

## Tools and Business Effects

Tools implement interfaces.Tool; their Invoke input is *interfaces.ToolInput, not a bare map. Arguments are declared by ArgsSchema and the result is *interfaces.ToolOutput. Register tools with the Builder via Dispatcher.Tools() to get validation, call identity, and the durable replay path; bare tool calls are not written to the operation log automatically.

Tool failures, argument rejections the model can revise, approval waits, and unsupported business cases must be handled separately. ReturnArgumentErrors applies only to a specific path of model-issued calls; it is not a dialog-management feature that automatically asks the user. See [Tool API](../api/TOOL_API.md).

## Sessions and Recovery

WithSessionID sets the session ID; WithSessionLog derives model history from the session log; WithLongTermMemory adds cross-session long-term memory (requires WithSessionLog). None of these replace the host's management of tasks awaiting clarification.

Session history, agent session state checkpoints, and the workflow durable.Store have different responsibilities. RunID/CallID/agent scope come from the host and stay stable across recovery; a new task must not reuse the identity of a completed run. For unified wiring see [Host Integration](HOST_INTEGRATION.md).

## Unified Entry Point

```go
// 1. Create the LLM client
client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4o-mini"),
)
if err != nil {
    return err
}

// 2. Build the agent
agent, err := builder.NewSimpleBuilder(client).Build()
if err != nil {
    return err
}

// 3. Execute
output, err := agent.AsRunnable().Invoke(ctx, "Hello")
if err != nil {
    return err
}
fmt.Println(output.Result)
```

`AsRunnable().Invoke` is the unified entry point; configured tools, retrieval, middleware, and callbacks all go through the same path. The Builder accepts any `llm.Client` and requires `llm.ToolStreamClient` when tools are configured. For an entry point mapping see the [Quickstart](QUICKSTART.md#migrating-from-legacy-apis).

## Context, Retries, and Observation

- Long tool results are handled through WithOffload and a registered read tool; the old ContextPolicy/WithContextPolicies has been removed, see the [Context Guide](CONTEXT_POLICY.md) for migration.
- Custom pre-request rewrites use WithIdentifiedPreStep to manage durable identity; role and business dispatch do not belong in generic context hooks.
- Transient model errors use WithRequestError with RetryTransient; WithModelRetry has been removed. Each attempt consumes budget separately, and sub-agents do not automatically inherit the parent's retry hooks.
- WithMiddleware accepts the handler-chain protocol from core/middleware; tool admission and Around/Post are handled by tools.Pipeline, see [Middleware API](../api/MIDDLEWARE_API.md).
- Observers are read-only and are not business state or checkpoints; the host is responsible for closing borrowed resources.

---

## Using Tools

### Creating Tools

```go
import "github.com/kart-io/goagent/tools"

weatherTool := tools.NewFunctionTool(
    "get_weather",
    "Look up the weather for a city",
    `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        city, _ := args["city"].(string)
        return map[string]string{"city": city, "weather": "sunny"}, nil
    },
)
```

You can also use the builder and generate the argument schema from a struct:

```go
type WeatherArgs struct {
    City string `json:"city"`
}

weatherTool, err := tools.NewFunctionToolBuilder("get_weather").
    WithDescription("Look up the weather for a city").
    WithArgsSchemaFromStruct(WeatherArgs{}).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        return "sunny", nil
    }).
    Build()
```

### Using Built-in Tools

```go
import (
    "github.com/kart-io/goagent/tools/compute"
    "github.com/kart-io/goagent/tools/http"
    "github.com/kart-io/goagent/tools/shell"
)

shellTool := shell.NewShellTool([]string{"ls", "cat"}, 10*time.Second)
calculator := compute.NewCalculatorTool()
apiTool := http.NewAPITool("https://api.example.com", 30*time.Second, nil)
```

### Letting the Agent Call Tools

```go
agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt("You can look up the weather and do calculations").
    WithTools(weatherTool, calculator).
    Build()
if err != nil {
    return err
}

output, err := agent.AsRunnable().Invoke(ctx, "What's the weather like in Beijing?")
```

---

## Memory Management

### Conversation Memory

```go
sessions := session.NewMemoryStore() // For persistence use session.NewFileStore(dir)
compaction, err := contextpolicy.NewCompaction(contextpolicy.CompactionConfig{MaxTokens: 100000, Model: summaryClient})
// check err
recall, err := contextpolicy.NewRecallTool(sessions)
// check err

agent, err := builder.NewSimpleBuilder(client).
    WithSessionLog(sessions).
    WithSessionID("session-001").
    WithCompaction(compaction).
    WithTools(recall).
    Build()
```

Multi-turn history is derived from the [core/session](../../core/session/README.md) session log; when the request context supplies a session ID via `tools.WithSessionID(ctx, id)`, the request's ID wins. When the conversation exceeds the threshold, earlier messages are compressed into a summary; the original text stays in the log and the model can retrieve it with `recall_history`, see [Context Governance](CONTEXT_POLICY.md#history-compaction).

### Long-Term Memory

Cross-session facts, experiences, and lessons are stored by the `memory` package, isolated by a namespace supplied by the host:

```go
mem, err := memory.New(memory.Config{
    Durable:  fs,                      // A store implementing durable.Scoped, e.g. durable.NewFileStore(dir)
    Index:    memory.NewMemoryIndex(), // Optional; enables vector recall when provided together with Embedder
    Embedder: embedClient,             // llm.EmbeddingClient
    Model:    extractClient,           // Model used by the Worker for extraction, consolidation, and reflection
    Sessions: sessions,                // Same as WithSessionLog
})
go mem.Run(ctx, nil) // Asynchronously processes extraction tasks registered at the end of each turn; run mem.RunMaintenance if no Worker runs

agent, err := builder.NewSimpleBuilder(client).
    WithSessionLog(sessions).
    WithLongTermMemory(mem).
    WithTools(mem.Tools()...). // Optional: memory_search, memory_save, memory_replace, memory_forget
    Build()

ctx = memory.WithNamespace(ctx, tenantID, userID) // Must come from a trusted identity
out, err := agent.AsRunnable().Invoke(ctx, "Recommend a coffee")
```

Before the first request of each turn, relevant records are recalled based on the input and, together with pinned core memories, placed into every step's request as one low-trust user message (before the run context); the recall block is not written to session history. At the end of each turn an extraction task is registered; the Worker extracts facts and experiences, compares them with similar existing memories, and then adds, merges, or invalidates old memories. For the design see [Memory System Design](../design/MEMORY_DESIGN.md).

### Session Log

```go
import "github.com/kart-io/goagent/core/session"

store, err := session.NewFileStore(dir) // or session.NewMemoryStore()
if err != nil {
    return err
}
agent, err := builder.NewSimpleBuilder(client).
    WithSessionID("session-001").
    WithSessionLog(store).
    Build()
```

Session history is derived from the log, so after a process restart the conversation continues with the same SessionID; see [core/session](../../core/session/README.md).

Multi-step tasks that must resume after a process restart use `core/durable`; see [Native Orchestration and Tooling](NATIVE_ORCHESTRATION.md).

---

## Using Middleware

### Adding Middleware

```go
import "github.com/kart-io/goagent/core/middleware"

agent, err := builder.NewSimpleBuilder(client).
    WithMiddleware(
        middleware.NewLoggingMiddleware(func(msg string) { log.Println(msg) }),
        middleware.NewTimingMiddleware(),
        middleware.NewRateLimiterMiddleware(10, time.Second),
    ).
    Build()
```

Cache model answers with `WithModelCache` (`Scope` returns a host-trusted isolation identity; the default `EraseAuto` requires `TagKey` when the cache supports tags):

```go
import (
    "context"
    "time"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/cache"
)

// client is an llm.Client; see QUICKSTART.md and docs/api/LLM_API.md for how to create it.
// tenantUser is the host's own function that extracts the "tenant/user" identity from the authenticated request context, returning false when unavailable.
// tagKey is a []byte of at least 32 bytes; in production read it from secret management, never hard-code it.
agent, err := builder.NewSimpleBuilder(client).
    WithModelCache(builder.ModelCache{
        Cache:  cache.NewSimpleCache(5 * time.Minute),
        TTL:    5 * time.Minute,
        Scope:  func(ctx context.Context) (string, bool) { return tenantUser(ctx) },
        TagKey: tagKey,
    }).
    Build()
```

For trust boundaries, hit scenarios, and deletion guarantees see the [Caching Guide](CACHING_GUIDE.md#model-call-caching).

### Custom Middleware

Embed `*middleware.BaseMiddleware` and override only the hooks you need:

```go
type TraceMiddleware struct {
    *middleware.BaseMiddleware
}

func NewTraceMiddleware() *TraceMiddleware {
    return &TraceMiddleware{BaseMiddleware: middleware.NewBaseMiddleware("trace")}
}

func (m *TraceMiddleware) OnBefore(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
    if req.Metadata == nil {
        req.Metadata = map[string]interface{}{}
    }
    req.Metadata["start_time"] = time.Now()
    return req, nil
}
```

---

## Streaming

`agent.AsRunnable().Stream` is recommended; see the [Quickstart](QUICKSTART.md#streaming). `ExecuteStream` is the channel form of the same event source: `text` events carry incremental text, the `done` event carries the final output, and `error` events carry errors.

```go
events, err := agent.ExecuteStream(ctx, "Please write a poem")
if err != nil {
    return err
}

var full strings.Builder
for event := range events {
    switch event.Type {
    case "text":
        full.WriteString(event.Delta)
        fmt.Print(event.Delta)
    case "error":
        return event.Error
    case "done":
        fmt.Println("\nFull content:", full.String())
    }
}
```

Consumers must drain the channel or cancel ctx; otherwise the upstream hangs due to backpressure.

---

## Error Handling

### Checking Error Codes

```go
import agentErrors "github.com/kart-io/goagent/errors"

output, err := agent.AsRunnable().Invoke(ctx, "Hello")
if err != nil {
    switch {
    case agentErrors.IsCode(err, agentErrors.CodeInvalidInput):
        log.Printf("Input error: %v", err)
    case agentErrors.IsCode(err, agentErrors.CodeLLMRequest),
        agentErrors.IsCode(err, agentErrors.CodeLLMTimeout):
        log.Printf("LLM error: %v", err)
    case agentErrors.IsCode(err, agentErrors.CodeToolExecution):
        log.Printf("Tool error: %v", err)
    default:
        log.Printf("Other error: %v", err)
    }
}
```

### Custom Error Handling

```go
agent, err := builder.NewSimpleBuilder(client).
    WithErrorHandler(func(err error) error {
        log.Printf("Agent error: %v", err)
        return fmt.Errorf("wrapped: %w", err)
    }).
    Build()
```

### Retrying Model Calls

```go
agent, err := builder.NewSimpleBuilder(client).
    WithRequestError(3, builder.RetryTransient(3, func(retry int) time.Duration {
        return time.Duration(retry) * 100 * time.Millisecond
    })).
    Build()
```

`RetryTransient` retries unchanged only on 429, 5xx, and connection errors, and does not retry once text has been emitted; in durable runs the retry happens within the same turn record.

---

## Best Practices

### 1. Control Timeouts and Cancellation with Context

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

output, err := agent.AsRunnable().Invoke(ctx, "Hello")
```

### 2. Release Resources on Shutdown

```go
if err := agent.Shutdown(ctx); err != nil {
    log.Printf("Failed to shut down agent: %v", err)
}
```

### 3. Use a Mock Client in Tests

```go
import "github.com/kart-io/goagent/testing/mocks"

mockClient := mocks.NewMockLLMClient()
mockClient.SetResponses(llm.CompletionResponse{Content: "Canned answer"})

agent, err := builder.NewSimpleBuilder(mockClient).Build()
// MockLLMClient implements llm.ToolStreamClient and can be called directly via AsRunnable.
```
