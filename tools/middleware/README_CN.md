# tools/middleware 工具中间件

[English](README.md) | [中文](README_CN.md)

为单个 `interfaces.Tool` 包装与调用方无关的逻辑：日志、结果缓存与限流，并提供 `Wrap` 组装自定义中间件。逻辑属于工具本身、无论从哪里调用都要生效时使用它。模型可见调用的准入、审批与结果后处理应改用 Dispatcher 管线（`tools.Pipeline`）。

## 用法

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
    middleware.Logging(middleware.WithContextLogger(slog.Default())), // 最外层
    cachingMW.Wrap, // 保留对象以读取 GetStats
    middleware.RateLimit(middleware.WithQPS(10), middleware.WithBurst(5)), // 最内层
)

out, err := wrapped.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"a": 1, "b": 2}})
hits, misses := cachingMW.GetStats()
```

自定义中间件是一个 `ToolMiddlewareFunc`：

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

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Wrap(tool, middlewares...)` | 组装中间件，第一个在最外层 |
| `ToolMiddlewareFunc`、`ToolInvoker` | 中间件函数类型与被包装的调用 |
| `WrappedTool` | `Wrap` 的结果；`Unwrap()` 返回原工具 |
| `Logging(opts...)`、`NewLoggingMiddleware` | 记录工具名、CallerID、TraceID、耗时与错误 |
| `WithContextLogger`、`WithInputLogging`、`WithOutputLogging`、`WithMaxArgBytes` | 日志选项 |
| `Caching(opts...)`、`NewCachingMiddleware` | 缓存成功结果；`GetStats()` 返回命中与未命中次数 |
| `WithResultCache`、`WithTTL`、`WithCacheMaxEntries`、`WithCacheKeyFunc`、`WithCacheContextLogger` | 缓存选项 |
| `ResultCache` | 结果存储接口，任何 `cache.Cache` 都满足 |
| `RateLimit(opts...)`、`NewRateLimitMiddleware` | 令牌桶限流；`GetStats()` 返回放行与拒绝次数 |
| `WithQPS`、`WithBurst`、`WithPerToolRateLimit`、`WithWaitTimeout`、`WithRateLimitContextLogger` | 限流选项 |

`WithCache`、`WithLogger`、`WithCacheLogger` 与 `WithRateLimitLogger` 已弃用，改用其 godoc 中给出的替代项。

## 行为说明

- 不传中间件时 `Wrap` 返回原工具。中间件为 nil 或返回 nil 调用器时，组装阶段以 `errors.CodeInvalidConfig` 错误 panic。
- `WrappedTool` 转发 `interfaces.ConcurrentCallTool`，原工具的其他可选接口不转发。
- 日志：未设置 `WithContextLogger` 时写入 `logging.Default()`（即 `slog.Default()`）；显式传 nil 关闭日志。参数与结果默认不记录；序列化后的参数默认截断到 1024 字节。错误与输出原样透传，仅在输出非 nil 时写入 `Metadata["execution_time"]`（毫秒）。
- 缓存：只存 `err == nil` 且 `Success == true` 的结果。未指定存储时使用内置内存缓存；`TTL` 为 0 表示不过期，`MaxEntries` 为 0 表示不限条数，键来自模型输入时应设上限。键按 `tools.WithSessionID` 的会话 ID 与 `ToolInput.CallerID` 隔离；默认键忽略以 `__` 开头的参数。命中时不调用内层，返回副本并置 `Metadata["cache_hit"]` 与 `Metadata["cached"]` 为 true。`ResultCache` 的值是 `*interfaces.ToolOutput` 副本，存储必须在进程内。
- 限流：默认 10 QPS、突发 20、全局共用一个限流器、不等待。拒绝时返回错误码为 `errors.CodeMiddlewareExecution` 的错误。`WithWaitTimeout(d)` 在 `d > 0` 时最多等待 `d` 获取令牌。
- 缓存放在限流外层时，缓存命中不消耗限流配额。

## 另见

- [工具中间件指南](../../docs/guides/TOOL_MIDDLEWARE_CN.md)
- [缓存指南](../../docs/guides/CACHING_GUIDE_CN.md)
