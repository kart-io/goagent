# 工具中间件

工具调用的横切逻辑有两层：

| 层 | 入口 | 适用 |
|---|---|---|
| 单个工具的包装 | `tools.WithMiddleware` + `tools/middleware` | 日志、结果缓存、限流等与调用方无关的逻辑 |
| Dispatcher 管线 | `tools.DispatchConfig.Pipeline`（`tools.Pipeline`） | 模型可见调用的准入、审批、硬性拒绝、执行包装与结果后处理；与调用日志、恢复、并发和超时一起工作 |

## tools.WithMiddleware

```go
import (
    "github.com/kart-io/goagent/cache"
    "github.com/kart-io/goagent/tools"
    "github.com/kart-io/goagent/tools/middleware"
)

wrapped := tools.WithMiddleware(calculator,
    middleware.NewLoggingMiddleware(),             // 最外层
    middleware.Caching(
        middleware.WithCache(cache.NewSimpleCache(10*time.Minute)),
        middleware.WithTTL(10*time.Minute),
    ),
    middleware.RateLimit(middleware.WithQPS(10), middleware.WithBurst(5)), // 最内层
)
output, err := wrapped.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"a": 1, "b": 2}})
```

`WithMiddleware(tool, middlewares ...interface{})` 按洋葱模型包装：第一个在最外层。接受两种形式，其他类型被**静默忽略**：

- `middleware.ToolMiddlewareFunc`（`func(interfaces.Tool, ToolInvoker) ToolInvoker`），可短路，例如缓存命中时不调用内层；`Caching`、`RateLimit` 返回这种形式。
- `middleware.ToolMiddleware` 接口（`Name`、`OnBeforeInvoke`、`OnAfterInvoke`、`OnError`），经 `middleware.Chain` 转换；`NewLoggingMiddleware` 返回这种形式，自定义时可嵌入 `middleware.NewBaseToolMiddleware(name)` 只覆盖需要的方法。

不传中间件时返回原工具。包装结果是 `*tools.MiddlewareTool`，只实现 `interfaces.Tool` 与 `Unwrap()`，**不转发**原工具声明的可选接口（如 `DispatchTimeoutProvider`、`DurablePolicyProvider`、`interfaces.ArgumentErrorTool`），Dispatcher 也不会调用 `Unwrap`；依赖这些声明的工具不要先包装再注册。

### 内置中间件

**Logging**：`NewLoggingMiddleware(opts...)`，默认输出到 stdout 的 JSON slog，记录工具名、CallerID、TraceID、参数、结果与耗时。选项：`WithLogger`、`WithoutInputLogging`、`WithoutOutputLogging`、`WithMaxArgBytes`（默认 1024 字节，超出截断）。它把开始时间写入 `input.Args["__logging_start_time"]`，内层工具会收到这个参数。

**Caching**：`Caching(opts...)`（或 `NewCachingMiddleware(opts...).Wrap`，后者的 `GetStats()` 返回命中/未命中次数）。

- 默认缓存 `cache.NewSimpleCache(5*time.Minute)`、TTL 5 分钟；选项 `WithCache(cache.Cache)`、`WithTTL`、`WithCacheKeyFunc(func(toolName string, args map[string]any) string)`、`WithCacheLogger`。
- 默认键 `tool:<name>:<sha256(参数JSON) 前 8 个十六进制字符>`，忽略 `__` 开头的参数；键中不含 CallerID 或会话，跨用户共享的缓存须自定义键函数。
- 只缓存 `err == nil` 且 `Success=true` 的结果（不含 `__` 开头的元数据）。命中时不调用内层，返回副本并设置 `Metadata["cache_hit"]=true`、`Metadata["cached"]=true`；未命中且写入时设置 `cache_hit=false`、`cache_stored=true`。

**RateLimit**：`RateLimit(opts...)`，基于 `golang.org/x/time/rate` 令牌桶。默认 10 QPS、突发 20、全局共享、不等待；选项 `WithQPS`、`WithBurst`、`WithPerToolRateLimit()`（按工具名独立限流）、`WithWaitTimeout`（大于 0 时最多等待这么久取令牌）、`WithRateLimitLogger`。拒绝时返回错误码 `errors.CodeMiddlewareExecution`，可用 `errors.IsCode(err, errors.CodeMiddlewareExecution)` 判断。

缓存放在限流外层时，命中不消耗限流额度。

### 自定义

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

## Dispatcher 管线

经 `Dispatcher.Tools()` 交给模型的调用与 `Dispatcher.Invoke`/`AsRunnable` 的宿主调用都按 PreHook → 审批 → Guard → Around（含超时）→ 工具 → PostHook 执行；参数不合法的调用不进入管线。

```go
dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{
    Pipeline: &tools.Pipeline{
        Guards: []tools.Guard{func(ctx context.Context, call *tools.Call) string {
            if call.Tool == "delete_order" && call.CallerID == "" {
                return "缺少调用身份"
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

| 阶段 | 类型 | 作用 |
|---|---|---|
| `Pre` | `PreHook` → `PreDecision` | `PreAllow`/`PreDeny`（交还模型修订）/`PreAsk`（交给 `Approver`）/`PreStop`（终止运行）/`PreSubstitute`（改调另一个工具）；全部执行后按 Stop > Deny > Substitute > Ask > Allow 聚合 |
| `Approver` | `Approver`/`ApproverFunc` | 宿主实现的人工审批；`ApprovalTimeout` 默认 10 分钟，`GrantTTL` 限制会话级授权有效期 |
| `Guards` | `Guard` | 只能拒绝，返回非空原因即以 `tools.Rejected` 交给模型；审批也无法越过 |
| `Around` | `Around` | 洋葱式包住工具执行，计入单次超时 |
| `Post` | `PostHook` → `PostDecision` | 替换结果（`Replace`）或追加上下文（`AdditionalContext`）；也处理被拒绝的结果 |

`Call.Args` 只读。准入阶段（Pre、审批、Guard）在操作日志之外执行，拒绝结果与执行结果一样写入日志；持久运行回放已完成的调用时直接返回日志中的结果，不再经过准入、Around 或包在 Dispatcher 内部的工具包装（含 `WithMiddleware`）。授权（`DispatchConfig.Authorize`）、并发、排队与超时见[执行治理](EXECUTION_GOVERNANCE.md#dispatcher-的两阶段准入)，日志与恢复见[原生编排](NATIVE_ORCHESTRATION.md#工具调用级日志与恢复)。

## 参考

- 源码：`tools/middleware/`、`tools/tool_wrapper.go`、`tools/pipeline.go`、`tools/dispatcher.go`
- [缓存指南](CACHING_GUIDE.md)
