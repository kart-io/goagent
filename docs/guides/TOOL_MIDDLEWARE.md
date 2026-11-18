# Tool Middleware

[English](TOOL_MIDDLEWARE.md) | [中文](TOOL_MIDDLEWARE_CN.md)

Cross-cutting logic for tool calls lives in two layers:

| Layer | Entry point | Use for |
|---|---|---|
| Wrapping a single tool | `middleware.Wrap` + `tools/middleware` | Caller-independent logic such as logging, result caching, and rate limiting |
| Dispatcher pipeline | `tools.DispatchConfig.Pipeline` (`tools.Pipeline`) | Admission, approval, hard rejection, execution wrapping, and result post-processing of model-visible calls; works together with call journaling, recovery, concurrency, and timeouts |

## middleware.Wrap

```go
import (
    "time"

    "github.com/kart-io/goagent/cache"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools"
    "github.com/kart-io/goagent/tools/middleware"
)

ownedCache := cache.NewSimpleCache(10*time.Minute)
defer ownedCache.Close()
wrapped := middleware.Wrap(calculator,
    middleware.Logging(),                          // outermost
    middleware.Caching(
        middleware.WithCache(ownedCache),
        middleware.WithTTL(10*time.Minute),
    ),
    middleware.RateLimit(middleware.WithQPS(10), middleware.WithBurst(5)), // innermost
)
output, err := wrapped.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"a": 1, "b": 2}})
```

`middleware.Wrap(tool, middlewares ...middleware.ToolMiddlewareFunc)` wraps in an onion model; the first function is the outermost. `Logging`, `Caching`, and `RateLimit` return the same strongly typed function; when you need to keep the object to read statistics, pass `NewCachingMiddleware(...).Wrap` or `NewRateLimitMiddleware(...).Wrap` explicitly. Objects or any other types do not compile as arguments; a nil middleware function, or one that returns a nil invoker, fails at assembly time with `errors.CodeInvalidConfig`.
With no middleware, the original tool is returned. The wrapped result is a `*middleware.WrappedTool`, which implements only `interfaces.Tool` and `Unwrap()` and **does not forward** optional interfaces declared by the original tool (such as `DispatchTimeoutProvider`, `DurablePolicyProvider`, `interfaces.ArgumentErrorTool`); the Dispatcher does not call `Unwrap` either. Tools that depend on these declarations should not be wrapped before registration.

### Built-in Middleware

**Logging**: `Logging(opts...)` (or `NewLoggingMiddleware(opts...).Wrap`). By default it writes JSON slog to stdout, recording the tool name, CallerID, TraceID, and duration. Arguments and results often contain user data and credentials, so they are not logged by default; enable them with `WithInputLogging` and `WithOutputLogging` respectively. Other options: `WithLogger`, `WithMaxArgBytes` (default 1024 bytes; longer values are truncated). Timing is kept in per-call local variables and does not modify business input; on success it writes `Metadata["execution_time"]`. Errors and partial results are passed through unchanged, and empty input and output are not rewritten by the logging wrapper.

**Caching**: `Caching(opts...)` (or `NewCachingMiddleware(opts...).Wrap`, whose `GetStats()` returns hit/miss counts).

- The default cache is `cache.NewSimpleCache(5*time.Minute, cache.WithoutBackgroundCleanup(), cache.WithMaxEntries(constants.DefaultMiddlewareCacheCapacity))` (no background goroutine, at most 10000 entries) with a 5-minute TTL. Keys are isolated by the session from `tools.WithSessionID` and by `ToolInput.CallerID`; `WithCacheKeyFunc` only determines the key within that scope. Options: `WithCache(cache.Cache)`, `WithTTL`, `WithCacheKeyFunc(func(toolName string, args map[string]any) string)`, `WithCacheLogger`.
- The default key is `tool:<name>:<full hex digest of sha256(args JSON)>`, ignoring arguments that start with `__`; the key contains no CallerID or session, so caches shared across users need a custom key function.
- Only results with `err == nil` and `Success=true` are cached (excluding metadata starting with `__`). On a hit, the inner layer is not called; a copy is returned with `Metadata["cache_hit"]=true` and `Metadata["cached"]=true`. On a miss followed by a write, `cache_hit=false` and `cache_stored=true` are set.

**RateLimit**: `RateLimit(opts...)`, a token bucket based on `golang.org/x/time/rate`. Defaults: 10 QPS, burst 20, globally shared, no waiting. Options: `WithQPS`, `WithBurst`, `WithPerToolRateLimit()` (independent limits per tool name), `WithWaitTimeout` (when greater than 0, wait up to that long for a token), `WithRateLimitLogger`. Rejections return error code `errors.CodeMiddlewareExecution`, which can be checked with `errors.IsCode(err, errors.CodeMiddlewareExecution)`.

When the cache sits outside the rate limiter, cache hits do not consume rate-limit quota.

### Custom

```go
func Audit(log func(string, ...any)) middleware.ToolMiddlewareFunc {
    return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
        return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
            log("tool start", "tool", tool.Name())
            output, err := next(ctx, input)
            log("tool end", "tool", tool.Name(), "error", err)
            return output, err
        }
    }
}
```

## Dispatcher Pipeline

Both calls handed to the model through `Dispatcher.Tools()` and host calls through `Dispatcher.Invoke`/`AsRunnable` run in the order PreHook → approval → Guard → Around (including timeout) → tool → PostHook; calls with invalid arguments do not enter the pipeline.

```go
dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{
    Pipeline: &tools.Pipeline{
        Guards: []tools.Guard{func(ctx context.Context, call *tools.Call) string {
            if call.Tool == "delete_order" && call.CallerID == "" {
                return "missing caller identity"
            }
            return ""
        }},
        Around: []tools.Around{func(ctx context.Context, call *tools.Call, next func(context.Context) (*interfaces.ToolOutput, error)) (*interfaces.ToolOutput, error) {
            start := time.Now()
            out, err := next(ctx)
            log.Printf("%s %s", call.Tool, time.Since(start))
            return out, err
        }},
    },
})
```

| Stage | Type | Role |
|---|---|---|
| `Pre` | `PreHook` → `PreDecision` | `PreAllow`/`PreDeny` (hand back to the model for revision)/`PreAsk` (hand to the `Approver`)/`PreStop` (terminate the run)/`PreSubstitute` (call a different tool instead); after all run, decisions are aggregated as Stop > Deny > Substitute > Ask > Allow |
| `Approver` | `Approver`/`ApproverFunc` | Host-implemented human approval; `ApprovalTimeout` defaults to 10 minutes, and `GrantTTL` limits how long session-level grants stay valid |
| `Guards` | `Guard` | Can only reject; returning a non-empty reason hands a `tools.Rejected` result to the model; approval cannot bypass it |
| `Around` | `Around` | Wraps tool execution onion-style; counts toward the single-call timeout |
| `Post` | `PostHook` → `PostDecision` | Replaces the result (`Replace`) or appends context (`AdditionalContext`); also handles rejected results |

`Call.Args` is read-only. The admission stages (Pre, approval, Guard) run outside the operation journal; rejection results are written to the journal just like execution results. When a durable run replays a completed call, it returns the journaled result directly without going through admission, Around, or tool wrappers inside the Dispatcher (including `middleware.Wrap`). For authorization (`DispatchConfig.Authorize`), concurrency, queuing, and timeouts see [Execution Governance](EXECUTION_GOVERNANCE.md#two-phase-admission-in-the-dispatcher); for journaling and recovery see [Native Orchestration](NATIVE_ORCHESTRATION.md#tool-call-journaling-and-recovery).

## References

- Source: `tools/middleware/` (`Wrap` is in `wrap.go`), `tools/pipeline.go`, `tools/dispatcher.go`
- [Caching Guide](CACHING_GUIDE.md)

Middleware objects with a `Wrap(interfaces.Tool, ToolInvoker) ToolInvoker` method must pass their `Wrap` explicitly, e.g. `middleware.Wrap(tool, logging.Wrap)`. The assembly entry point accepts only `ToolMiddlewareFunc`; other types are rejected by the compiler. A nil function, or one that returns a nil invoker, fails at construction with a configuration error. Optional capabilities of the original tool must still be assembled explicitly as described above.
