# core: Core Execution Engine

[English](README.md) | [中文](README_CN.md)

Provides the unified executable interface `Runnable[I, O]`, Agent base types, callbacks, Generator stream processing, panic isolation, and other infrastructure.

General graph orchestration lives in [workflow](../workflow/README.md): conditional branches, parallel Clone/Merge, bounded loops, and Runnable/Agent adapters; compiled results reuse `core.StreamRunnable`. Graph orchestration and the sequential-step recovery of `core/durable` are independent capabilities; entry points, streaming, and persistence boundaries are defined by the [Execution Contract](../docs/guides/EXECUTION_CONTRACT.md).

## Core Components

### Runnable

All executable components implement `Runnable[I, O]`:

| Method | Description |
|------|------|
| `Invoke(ctx, input)` | Single execution |
| `Stream(ctx, input)` | Streaming execution, returns `<-chan StreamChunk[O]` |
| `Batch(ctx, inputs)` | Batch execution |
| `Pipe(next)` | Pipe connection, returns `Runnable[I, any]` |
| `WithCallbacks(...)` | Returns a new `Runnable[I, O]` with callbacks |
| `WithConfig(config)` | Returns a new `Runnable[I, O]` with configuration |

Implementations: `RunnableFunc` (wraps a function), `RunnablePipe`, `RunnableSequence`, `StreamRunnable` (uses a `StreamSource` as its single execution source, and `Invoke` consumes the same data stream; the builder's `Agent.AsRunnable()` and workflow compiled results both return it). `BaseRunnable` provides callback, configuration, and `Batch` helpers for concrete types to embed.

### Callback

The `Callback` interface includes `OnStart/OnEnd/OnError`, `OnLLMStart/OnLLMEnd/OnLLMError`, `OnChainStart/OnChainEnd/OnChainError`, `OnToolStart/OnToolEnd/OnToolError`, and `OnAgentAction/OnAgentFinish`. Returning a non-nil error aborts execution. Embed `BaseCallback` to implement only the methods you care about.

Built-in: `NewLoggingCallback(logger Logger, verbose bool)`, `NewMetricsCallback(MetricsCollector)`, `NewTracingCallback(Tracer)`, `NewCostTrackingCallback(pricing)`, `NewStdoutCallback(color)`, and `NewCallbackManager(...)` to combine multiple callbacks.

### State

`core/state.State` (`core.State` is an alias): `Get`, `Set`, `Update`, `Snapshot`, `Clone`, `Delete`, `Clear`, `Keys`, `Size`. `NewAgentState()` returns a thread-safe implementation, which also offers `GetString`/`GetInt`/`GetBool`/`GetFloat64`/`GetMap`/`GetSlice`. The `state` package additionally provides `TypedState` with schema validation.

Session persistence and recovery use the session log of [core/session](session/README.md) and `core/durable` (the builder's `WithSessionLog` and `WithDurableScope`).

## Usage

Cross-process pause and resume for human-in-the-loop uses the checkpoints and `Resume` of `workflow.CompileDurable`; see the [HITL guide](../docs/guides/HUMAN_IN_THE_LOOP.md).

### Function Runnables and Pipes

```go
upper := core.NewRunnableFunc(func(ctx context.Context, s string) (string, error) {
    return strings.ToUpper(s), nil
})
length := core.NewRunnableFunc(func(ctx context.Context, s string) (int, error) {
    return len(s), nil
})

pipe := core.NewRunnablePipe[string, string, int](upper, length)
n, err := pipe.Invoke(ctx, "hello") // 5
```

### Generator

`Generator[T]` is `iter.Seq2[T, error]`; you can `range` over it directly, and an early `break` stops it:

```go
gen := core.GeneratorFunc(func(yield func(int, error) bool) {
    for i := range 100 {
        if !yield(i, nil) {
            return
        }
    }
})

for n, err := range gen {
    if err != nil {
        return err
    }
    fmt.Println(n)
}

even := core.Filter(gen, func(n int) bool { return n%2 == 0 })
results, err := core.Collect(core.Take(even, 10))
```

There are also `Map`, `GeneratorFunc`, `FromChannel`, and `ToChannel`.

### State

```go
st := core.NewAgentState()
st.Set("user_id", "12345")
snapshot := st.Snapshot()
```

## Middleware and Auxiliary Execution Contract

- The three middleware chains share the same error recovery rules; `FastMiddlewareChain` still does not call `OnAfter`. When `OnBefore` / `OnAfter` returns `(nil, err)` and its own `OnError` suppresses the error, the valid object that entered that stage is kept; when a handler error is suppressed, the partial response provided by the handler is kept, and if there is no response an empty response is constructed and the request state is kept. `OnError` can read the current request through `RequestFromContext`; once the error is cleared, error propagation stops.
- The default tool selector matches case-insensitively on whole words or tool names; `maxTools == 0` means select none, and a negative value means no limit. Custom selectors must not introduce tools outside the candidate set. The Builder consumes the per-request `selected_tools` and further intersects it with the direct-return tools of the last turn; selection only controls which tools the model can see, and host authorization is still enforced by the Dispatcher.
- Errors from a `RunnableFunc` completion callback are returned as execution errors.
- `CostTrackingCallback` requires the host to explicitly send paired model start/end events. Each concurrent model call must use its own context, and overlapping calls on the same context are rejected; failed calls must send `OnLLMError` to clear the association. When the start event or the model unit price is missing, only tokens are accumulated. Builder model usage should use its UsageHook; legacy callbacks do not automatically receive the Builder's model events.
- `TypedState` integers range over `int64`; JSON float integers must be finite, have no fractional part, and not overflow; arrays are validated element by element, nested objects accept only string keys, and bytes and times require byte slices and `time.Time` respectively. Unknown field types and empty values for required fields are rejected.

## Code Layout

```text
core/
├── runnable.go / stream_runnable.go  # Runnable interface and implementations
├── callback.go                       # callbacks
├── generator.go                      # Generator[T]
├── panic_handler.go
├── budget/       # host-level model request budget
├── durable/      # durable runs and recovery for explicitly versioned steps
├── flow/         # experimental synchronous read-only incremental pipeline
├── middleware/   # request/response middleware (OnBefore/OnAfter/OnError)
├── observe/      # optional execution events
├── session/      # session event log, see session/README.md
└── state/        # state management
```

## Further Reading

- [builder](../builder/) - Agent builder
- [workflow](../workflow/README.md) - graph orchestration
- [core/session](session/README.md) - session event log
