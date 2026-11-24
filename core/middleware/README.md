# middleware: Agent Request Middleware Chain

[English](README.md) | [中文](README_CN.md)

This package provides the agent request middleware chain and built-in middleware for logging, timing, rate limiting, validation, transformation, authentication and circuit breaking. Each middleware gets `OnBefore` (inspect or replace the request, or stop the chain), `OnAfter` (inspect or replace the response) and `OnError` (handle, wrap or suppress an error). The Builder accepts these through `WithMiddleware`; use the chain types directly when wrapping your own `Handler`. Tool admission and Around/Post hooks are a separate mechanism in `tools.Pipeline`.

## Usage

```go
import (
    "context"
    "fmt"
    "strings"

    "github.com/kart-io/goagent/core/middleware"
    "github.com/kart-io/goagent/core/state"
)

handler := func(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareResponse, error) {
    return &middleware.MiddlewareResponse{Output: fmt.Sprintf("echo: %v", req.Input)}, nil
}

// Middleware from plain functions; nil hooks pass through.
upper := middleware.NewMiddlewareFunc("upper",
    func(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
        if s, ok := req.Input.(string); ok {
            req.Input = strings.ToUpper(s)
        }
        return req, nil
    },
    nil, nil,
)

timing := middleware.NewTimingMiddleware()
chain := middleware.NewMiddlewareChain(handler).
    Use(middleware.NewLoggingMiddleware(nil), timing, upper)

resp, err := chain.Execute(ctx, &middleware.MiddlewareRequest{
    Input:    "hello",
    State:    state.NewAgentState(),
    Metadata: map[string]interface{}{"user_id": "u1"},
})
if err != nil {
    return err
}
fmt.Println(resp.Output, timing.GetAverageLatency())
```

A fixed set of middleware can use `NewImmutableMiddlewareChain(handler, mws...)`; with the Builder, pass them to `WithMiddleware(...)` (see the User Guide).

## Key API

| Identifier | Description |
|---|---|
| `Middleware` | Interface: `Name`, `OnBefore`, `OnAfter`, `OnError` |
| `Handler` | `func(ctx, *MiddlewareRequest) (*MiddlewareResponse, error)`, the wrapped execution |
| `MiddlewareRequest` / `MiddlewareResponse` | `Input`/`Output`, `State`, `Metadata`, `Headers`; the response also has `Duration`, `TokenUsage`, `Error` |
| `NewMiddlewareChain(handler)` | Mutable chain; `Use` appends, `Execute` runs, `WithHandler` snapshots the middleware with another handler |
| `NewImmutableMiddlewareChain(handler, mws...)` | Chain with a fixed middleware list |
| `NewFastMiddlewareChain(handler, mws...)` | Chain that runs `OnBefore` only, never `OnAfter` |
| `NewBaseMiddleware(name)` | No-op implementation to embed in custom middleware |
| `NewMiddlewareFunc(name, before, after, onError)` | Middleware built from functions |
| `RequestFromContext(ctx)` | Request whose execution failed, for use in `OnError` |
| `NewLoggingMiddleware(logger)` | Logs request, response and error lines |
| `NewTimingMiddleware()` | Records durations; `GetTimings`, `GetAverageLatency` |
| `NewRateLimiterMiddleware(max, window)` | Fixed-window limit per user ID |
| `NewCircuitBreakerMiddleware(maxFailures, resetTimeout)` | closed / open / half-open breaker |
| `NewValidationMiddleware`, `NewAuthenticationMiddleware`, `NewTransformMiddleware` | Request validators, auth check, input/output transforms |
| `NewDynamicPromptMiddleware`, `NewToolSelectorMiddleware` | Prompt rewriting and tool list selection |

## Behavior Notes

- Order: `OnBefore` runs in registration order, the handler runs, then `OnAfter` runs in reverse order.
- An `OnBefore` error goes to that middleware's `OnError`. If `OnError` returns nil the chain continues with the request it received; otherwise the error is also passed to the earlier middleware's `OnError` and `Execute` returns it.
- A handler error is passed to each middleware's `OnError` in order until one returns nil (suppressed). When the handler returns a nil response (for example after a suppressed error), an empty response carrying the request `State` is used.
- Returning a nil request or response without an error fails the chain with `CodeMiddlewareExecution`.
- `Execute` fills `Duration` with the handler time when the handler left it zero.
- `MiddlewareChain.Use` is copy-on-write; `Execute` reads the middleware list without locking. Middleware instances are shared across executions (including `WithHandler` clones), so their own state must be concurrency-safe.
- `NewLoggingMiddleware(nil)` writes each line at info level through `logging.Default()`, that is `slog.Default()`.
- `TimingMiddleware` keeps only the most recent `constants.DefaultTimingWindow` records; the average covers all records.
- `RateLimiterMiddleware` keys requests by `State["user_id"]`, then `Metadata["user_id"]`, else `"default"`, and writes `rate_limit_remaining` and `rate_limit_reset` into `Metadata`.
- `CircuitBreakerMiddleware` opens after `maxFailures` errors seen in `OnError`, allows a trial request after `resetTimeout`, and closes on a successful response.
- Middleware side effects are not automatically idempotent.
- `GetMiddlewareRequest`/`PutMiddlewareRequest` (and the response pair) reuse pooled objects; do not use an object after putting it back.

## See Also

- [User Guide: Using Middleware](../../docs/guides/USER_GUIDE.md#using-middleware)
- [Builder API Reference](../../docs/guides/BUILDER_API_REFERENCE.md): `WithMiddleware`
- [Extension Contract](../../docs/guides/EXTENSION_CONTRACT.md): middleware side effects and error semantics
