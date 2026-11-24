# middleware Agent 请求中间件链

[English](README.md) | [中文](README_CN.md)

本包提供 Agent 请求的中间件链，以及日志、计时、限流、校验、转换、认证与熔断等内置中间件。每个中间件有 `OnBefore`（检查或替换请求，或中止链）、`OnAfter`（检查或替换响应）与 `OnError`（处理、包装或吞掉错误）三个钩子。Builder 通过 `WithMiddleware` 接收它们；包装自己的 `Handler` 时直接使用链类型。工具准入与 Around/Post 钩子是 `tools.Pipeline` 中的另一套机制。

## 用法

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

// 用普通函数构造中间件；为 nil 的钩子直接放行。
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

中间件集合固定时可用 `NewImmutableMiddlewareChain(handler, mws...)`；配合 Builder 时传给 `WithMiddleware(...)`（见用户指南）。

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Middleware` | 接口：`Name`、`OnBefore`、`OnAfter`、`OnError` |
| `Handler` | `func(ctx, *MiddlewareRequest) (*MiddlewareResponse, error)`，被包装的执行体 |
| `MiddlewareRequest` / `MiddlewareResponse` | `Input`/`Output`、`State`、`Metadata`、`Headers`；响应另有 `Duration`、`TokenUsage`、`Error` |
| `NewMiddlewareChain(handler)` | 可变链；`Use` 追加，`Execute` 执行，`WithHandler` 以另一个 handler 快照当前中间件 |
| `NewImmutableMiddlewareChain(handler, mws...)` | 中间件列表固定的链 |
| `NewFastMiddlewareChain(handler, mws...)` | 只执行 `OnBefore`、从不执行 `OnAfter` 的链 |
| `NewBaseMiddleware(name)` | 空实现，供自定义中间件嵌入 |
| `NewMiddlewareFunc(name, before, after, onError)` | 由函数构造的中间件 |
| `RequestFromContext(ctx)` | 取得执行失败的请求，供 `OnError` 使用 |
| `NewLoggingMiddleware(logger)` | 记录请求、响应与错误行 |
| `NewTimingMiddleware()` | 记录耗时；`GetTimings`、`GetAverageLatency` |
| `NewRateLimiterMiddleware(max, window)` | 按用户 ID 的固定窗口限流 |
| `NewCircuitBreakerMiddleware(maxFailures, resetTimeout)` | closed / open / half-open 熔断器 |
| `NewValidationMiddleware`、`NewAuthenticationMiddleware`、`NewTransformMiddleware` | 请求校验、认证检查、输入/输出转换 |
| `NewDynamicPromptMiddleware`、`NewToolSelectorMiddleware` | 改写提示词、筛选工具列表 |

## 行为说明

- 顺序：`OnBefore` 按注册顺序执行，然后执行 handler，`OnAfter` 再按逆序执行。
- `OnBefore` 返回的错误先交给该中间件的 `OnError`。`OnError` 返回 nil 时链以它收到的请求继续；否则错误还会传给此前各中间件的 `OnError`，`Execute` 返回该错误。
- handler 的错误按顺序交给各中间件的 `OnError`，直到某个返回 nil（吞掉）。handler 返回 nil 响应时（例如错误被吞掉之后），使用一个带请求 `State` 的空响应。
- 无错误却返回 nil 请求或响应时，链以 `CodeMiddlewareExecution` 失败。
- handler 未设置 `Duration` 时，`Execute` 用 handler 耗时填充。
- `MiddlewareChain.Use` 采用写时复制，`Execute` 无锁读取中间件列表。中间件实例在多次执行间共享（包括 `WithHandler` 的副本），其自身状态必须并发安全。
- `NewLoggingMiddleware(nil)` 以 info 级别经 `logging.Default()`（即 `slog.Default()`）写出每一行。
- `TimingMiddleware` 只保留最近 `constants.DefaultTimingWindow` 条记录；平均值按全部记录累计。
- `RateLimiterMiddleware` 依次以 `State["user_id"]`、`Metadata["user_id"]` 作为键，均无时用 `"default"`，并在 `Metadata` 中写入 `rate_limit_remaining` 与 `rate_limit_reset`。
- `CircuitBreakerMiddleware` 在 `OnError` 累计 `maxFailures` 次错误后打开，`resetTimeout` 后放行一次试探请求，收到成功响应后关闭。
- 中间件的副作用不会自动幂等。
- `GetMiddlewareRequest`/`PutMiddlewareRequest`（及响应对应的一对）复用池化对象；放回后不要再使用该对象。

## 另见

- [用户指南：使用中间件](../../docs/guides/USER_GUIDE_CN.md)
- [Builder API 参考](../../docs/guides/BUILDER_API_REFERENCE_CN.md)：`WithMiddleware`
- [扩展契约](../../docs/guides/EXTENSION_CONTRACT_CN.md)：中间件的副作用与错误语义
