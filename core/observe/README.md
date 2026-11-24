# observe: Execution Events

[English](README.md) | [中文](README_CN.md)

`observe` provides optional execution events and propagation of an existing OpenTelemetry tracer through the context. The Builder, Dispatcher, and other producers emit lifecycle events (agent, model call, retry, tool call, cache, usage) to the `Observer` bound to the context. Events are observations, never business checkpoints or accounting evidence; for reliable token totals query the `core/budget` ledger. Reach for this package to feed events into logs, metrics, or traces, or to emit events from your own workflow code.

## Usage

```go
type sink struct{}

func (sink) Observe(ctx context.Context, e observe.Event) {
    if e.Type == "model_usage" && e.Usage != nil {
        _ = e.Usage.Tokens // project into metrics; not a billing record
    }
}

func run(ctx context.Context, logger logging.Logger) error {
    async, err := observe.NewAsyncObserver(sink{}, 1024, 1)
    if err != nil {
        return err
    }
    defer func() {
        closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        _ = async.Close(closeCtx)
    }()
    ctx = observe.WithObserver(ctx, async)
    ctx = observe.AddObserver(ctx, observe.NewLogObserver(logger)) // both receive every event
    ctx = observe.WithRunID(ctx, "business-run")

    stepCtx, end := observe.Start(ctx, observe.Event{Type: "step", Path: "step"})
    err = doStep(stepCtx)
    end(err, false) // exactly once; true marks a replay
    return err
}
```

## Key API

| Identifier | Description |
|---|---|
| `Observer` | `Observe(context.Context, Event)`; must support concurrent calls and return promptly |
| `Event` | Identities (`RunID`, `OperationKey`, `VisitID`, `EventID`, trace/span IDs), `Type`, `Path`, `ErrorKind`, timing, `Replayed`, `Cached`, optional `Agent` and `Usage` |
| `WithObserver(ctx, o)` / `AddObserver(ctx, o)` | Binds a sink / binds one in addition to the existing sink |
| `NewAsyncObserver(sink, capacity, workers)` | Bounded-queue adapter that never blocks the producer |
| `NewLogObserver(l)` | Writes each event to a `logging.Logger` as one structured record |
| `WithTracer(ctx, t)` / `TracerFromContext(ctx)` | Injects an existing tracer without setting a global provider / reads it (noop by default) |
| `WithRunID(ctx, id)` / `RunIDFromContext(ctx)` | Binds / reads the business run identity |
| `Start(ctx, e)` | Starts a lifecycle and its span; returns the child context and an `end(err, replayed)` function |
| `Emit(ctx, e)` | Delivers a standalone event without a span |
| `Enabled(ctx)`, `MarkCached(ctx)`, `ErrorKind(err)` | Whether a sink or tracer is configured; mark the current lifecycle as cached; bounded error category |

## Behavior Notes

- `Event` deliberately excludes raw inputs, outputs, and provider error messages. `ErrorKind` returns only a category (`panic`, `canceled`, `deadline`, `waiting`, `attention`, `execution`); panic values and stacks never enter events or spans.
- `Start` emits `<Type>_start`; `end` emits `<Type>_end`, `<Type>_replayed`, `<Type>_error`, or `<Type>_waiting`. `end` must be called exactly once.
- With no sink and no tracer, `Start` and `Emit` do nothing and `Emit` performs no allocation. Delivery is best effort.
- `WithObserver` isolates panics from the sink but does not protect against a blocking sink. `NewAsyncObserver` requires positive `capacity` and `workers`; with more than one worker the sink must support concurrent calls. `Dropped()` counts events lost to queue overflow, events after `Close`, and pending events dropped when `Close`'s context is canceled.
- Enqueued async events keep context values but not the caller's cancellation. `Close` waits for enqueued events; the sink must not call `Close` on its own adapter.
- `NewLogObserver` levels: failed executions (`execution` or `panic`) are Error; other failures, cancellations, and model retries are Warn; finished, replayed, and waiting lifecycles are Info; starts and standalone events are Debug. A nil logger writes through `logging.Default()`.

## See Also

- [Composable Orchestration: Observation and Reporting](../../docs/guides/COMPOSABLE_ORCHESTRATION.md#observation-and-reporting)
- [Root Budget, Resource Governance, and Run Diagnostics](../../docs/guides/EXECUTION_GOVERNANCE.md#asynchronous-observation-and-grant-reclamation)
- [Host Integration: Logging](../../docs/guides/HOST_INTEGRATION.md#logging)
- [Multi-Agent Hooks and Token Usage](../../docs/guides/MULTI_AGENT_USAGE.md) (Chinese only)
