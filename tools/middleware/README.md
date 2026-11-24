# tools/middleware: Tool Middleware

[English](README.md) | [中文](README_CN.md)

Wraps a single `interfaces.Tool` with caller-independent logic: logging, result caching and rate limiting, plus `Wrap` to assemble your own middleware. Use it when the logic belongs to the tool itself, wherever it is called from. For admission, approval and result post-processing of model-visible calls, use the Dispatcher pipeline (`tools.Pipeline`) instead.

## Usage

```go
import (
    "log/slog"
    "time"

    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools/middleware"
)

cachingMW := middleware.NewCachingMiddleware(
    middleware.WithTTL(10*time.Minute),
    middleware.WithCacheMaxEntries(1000),
)

wrapped := middleware.Wrap(myTool,
    middleware.Logging(middleware.WithContextLogger(slog.Default())), // outermost
    cachingMW.Wrap, // keep the object to read GetStats
    middleware.RateLimit(middleware.WithQPS(10), middleware.WithBurst(5)), // innermost
)

out, err := wrapped.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"a": 1, "b": 2}})
hits, misses := cachingMW.GetStats()
```

Custom middleware is a `ToolMiddlewareFunc`:

```go
func Audit(log func(string, ...any)) middleware.ToolMiddlewareFunc {
    return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
        return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
            log("tool start", "tool", tool.Name())
            out, err := next(ctx, input)
            log("tool end", "tool", tool.Name(), "error", err)
            return out, err
        }
    }
}
```

## Key API

| Identifier | Description |
|---|---|
| `Wrap(tool, middlewares...)` | Assembles middleware; the first one is outermost |
| `ToolMiddlewareFunc`, `ToolInvoker` | Middleware function type and the call it wraps |
| `WrappedTool` | Result of `Wrap`; `Unwrap()` returns the original tool |
| `Logging(opts...)`, `NewLoggingMiddleware` | Logs tool name, CallerID, TraceID, duration and errors |
| `WithContextLogger`, `WithInputLogging`, `WithOutputLogging`, `WithMaxArgBytes` | Logging options |
| `Caching(opts...)`, `NewCachingMiddleware` | Caches successful results; `GetStats()` returns hits and misses |
| `WithResultCache`, `WithTTL`, `WithCacheMaxEntries`, `WithCacheKeyFunc`, `WithCacheContextLogger` | Caching options |
| `ResultCache` | Result store interface; any `cache.Cache` satisfies it |
| `RateLimit(opts...)`, `NewRateLimitMiddleware` | Token bucket limit; `GetStats()` returns allowed and rejected counts |
| `WithQPS`, `WithBurst`, `WithPerToolRateLimit`, `WithWaitTimeout`, `WithRateLimitContextLogger` | Rate limit options |

`WithCache`, `WithLogger`, `WithCacheLogger` and `WithRateLimitLogger` are deprecated; use the replacements named in their godoc.

## Behavior Notes

- `Wrap` with no middleware returns the original tool. A nil middleware, or one that returns a nil invoker, panics with an `errors.CodeInvalidConfig` error at assembly time.
- `WrappedTool` forwards `interfaces.ConcurrentCallTool`. Other optional interfaces of the original tool are not forwarded.
- Logging: without `WithContextLogger`, records go to `logging.Default()` (`slog.Default()`); passing nil disables logging. Arguments and results are not logged unless enabled; serialized arguments are truncated at 1024 bytes by default. Errors and outputs pass through unchanged, except that a non-nil output gets `Metadata["execution_time"]` (milliseconds).
- Caching: only results with `err == nil` and `Success == true` are stored. Without a store, a built-in in-memory cache is used; `TTL` 0 means no expiry and `MaxEntries` 0 means no cap, so set a cap when keys come from model input. Keys are scoped by the session ID from `tools.WithSessionID` and by `ToolInput.CallerID`; the default key ignores arguments starting with `__`. A hit returns a copy with `Metadata["cache_hit"]` and `Metadata["cached"]` set to true, without calling inner layers. `ResultCache` values are `*interfaces.ToolOutput` copies, so the store must be in-process.
- RateLimit: defaults are 10 QPS, burst 20, one global limiter, and no waiting. Rejections return an error with code `errors.CodeMiddlewareExecution`. `WithWaitTimeout(d)` with `d > 0` waits up to `d` for a token.
- Put caching outside rate limiting so cache hits do not consume quota.

## See Also

- [Tool Middleware guide](../../docs/guides/TOOL_MIDDLEWARE.md)
- [Caching guide](../../docs/guides/CACHING_GUIDE.md)
