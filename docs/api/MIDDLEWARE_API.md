# GoAgent Middleware API 参考

本文介绍 `core/middleware` 的请求/响应中间件。完整签名见 `go doc -all github.com/kart-io/goagent/core/middleware`。工具级中间件（`tools/middleware`）见 [Tool API](TOOL_API.md#工具中间件) 与 [工具中间件指南](../guides/TOOL_MIDDLEWARE.md)。

## 与 Builder 的关系

`builder.WithMiddleware(...middleware.Middleware)` 注册的中间件包裹整次 Agent 执行（`Execute`、`ExecuteWithTools`、`ExecuteStream` 与 `AsRunnable` 都经过同一条链），不是每次模型调用都执行一次。请求中：

- `Input` 是调用方传入的输入；
- `State` 是 Agent 状态，`Runtime` 是 `*core/execution.Runtime[C, S]`；
- `Metadata` 预先填入 `WithMetadata` 设置的键值。

处理器返回的 `Output` 是最终答案，`TokenUsage` 是本次执行的用量。`WithVerbose(true)` 会在用户中间件之前自动加入 `LoggingMiddleware` 与 `TimingMiddleware`。中间件副作用不会自动幂等，契约见 [扩展契约](../guides/EXTENSION_CONTRACT.md)。

## 接口与类型

```go
type Middleware interface {
    Name() string
    OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error)
    OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error)
    OnError(ctx context.Context, err error) error
}

type Handler func(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error)

type MiddlewareRequest struct {
    Input     interface{}
    State     state.State
    Runtime   interface{}
    Metadata  map[string]interface{}
    Headers   map[string]string
    Timestamp time.Time
}

type MiddlewareResponse struct {
    Output     interface{}
    State      state.State
    Metadata   map[string]interface{}
    Headers    map[string]string
    Duration   time.Duration
    TokenUsage *interfaces.TokenUsage
    Error      error
}
```

`BaseMiddleware`（`NewBaseMiddleware(name)`）提供三个钩子的空实现，自定义中间件嵌入它后只覆盖需要的方法。`MiddlewareFunc`（`NewMiddlewareFunc(name, before, after, onError)`）用函数构造中间件，任一函数可为 nil。

`GetMiddlewareRequest` / `PutMiddlewareRequest`、`GetMiddlewareResponse` / `PutMiddlewareResponse` 是对象池。

## 中间件链

```go
func NewMiddlewareChain(handler Handler) *MiddlewareChain
func (c *MiddlewareChain) Use(middleware ...Middleware) *MiddlewareChain
func (c *MiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error)
func (c *MiddlewareChain) WithHandler(handler Handler) *MiddlewareChain // 共享中间件、替换处理器
func (c *MiddlewareChain) Size() int
```

`MiddlewareChain.Execute` 的顺序：

1. 按添加顺序调用 `OnBefore`。某个 `OnBefore` 返回错误时只调用该中间件的 `OnError`：返回非 nil 则中止，返回 nil 则继续（此时请求可能是 nil）。
2. 若 `request.Metadata["cached_response"]` 是 `*MiddlewareResponse`，跳过处理器直接使用它。
3. 调用处理器。处理器出错时按添加顺序调用所有中间件的 `OnError`，非 nil 返回值替换当前错误；最终错误非 nil 则返回该错误，不再执行 `OnAfter`。
4. 按添加顺序的逆序调用 `OnAfter`。某个 `OnAfter` 出错时调用该中间件的 `OnError`，非 nil 则中止。

`NewImmutableMiddlewareChain(handler, mws...)` 是构造后不可变的链；`NewFastMiddlewareChain(handler, mws...)` 只执行 `OnBefore`。

## 内置中间件

| 构造函数 | 行为 |
|---|---|
| `NewLoggingMiddleware(logger func(string))` | 记录请求、响应与错误；logger 为 nil 时用 `fmt.Println` |
| `NewTimingMiddleware()` | 记录耗时；`GetTimings()`、`GetAverageLatency()` |
| `NewCacheMiddleware(ttl)`、`NewCacheMiddlewareWithShards(ttl, n)` | 以 `fmt.Sprint(request.Input)` 为键，命中时通过 `cached_response` 跳过处理器；只有响应 `Metadata["original_input"]` 存在时才写入缓存，Builder 的处理器不设置该键。`Clear()`、`Size()` |
| `NewRateLimiterMiddleware(maxRequests, window)` | 固定窗口计数，按 `State` 或 `Metadata` 中的 `user_id` 分组，超限时 `OnBefore` 返回错误；`Reset()` |
| `NewCircuitBreakerMiddleware(maxFailures, resetTimeout)` | 累计失败达到阈值后打开并拒绝请求；超过 resetTimeout 后半开，半开时成功则关闭、失败则重新打开 |
| `NewRetryMiddleware(maxRetries, backoff)` | 不重新执行处理器，只在 `OnError` 中把满足条件的错误包装为 "retry needed"；`WithRetryCondition(func(error) bool)`。模型请求重试请用 Builder 的 `WithRequestError` |
| `NewValidationMiddleware(validators...)` | `OnBefore` 中依次执行校验函数 |
| `NewTransformMiddleware(inputFn, outputFn)` | 转换 `Input` 与 `Output` |
| `NewAuthenticationMiddleware(authFunc)` | `authFunc` 返回 false 或错误时拒绝请求 |
| `NewDynamicPromptMiddleware(modifier)` | `Input` 为字符串时，用 modifier 的结果并附加 `State` 中的 `user_name`、`conversation_history` |
| `NewToolSelectorMiddleware(tools, maxTools)` | 按输入选出工具名写入 `Metadata["selected_tools"]`；`WithSelector` 自定义选择函数。不改变 Builder 实际提供给模型的工具 |
| `NewRandomDelayMiddleware(min, max)` | 随机延迟，用于测试 |

根包 `github.com/kart-io/goagent/middleware` 另有 `ObservabilityMiddleware`（实现上面的 `Middleware` 接口，基于 `observability.TelemetryProvider`），以及 `AdaptiveMiddleware`、`ContextEnrichmentMiddleware`、`DynamicPromptMiddleware`、`LLMToolSelectorMiddleware`、`LLMToolEmulatorMiddleware`。后五者提供 `Process(ctx, core.State) (core.State, error)`，不实现 `Middleware` 接口，不能传给 `WithMiddleware`。

## 自定义中间件

```go
type auditMiddleware struct {
    *middleware.BaseMiddleware
    record func(input, output interface{})
}

func newAuditMiddleware(record func(input, output interface{})) *auditMiddleware {
    return &auditMiddleware{BaseMiddleware: middleware.NewBaseMiddleware("audit"), record: record}
}

func (m *auditMiddleware) OnBefore(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
    req.Metadata["audit_input"] = req.Input
    return req, nil
}

func (m *auditMiddleware) OnAfter(ctx context.Context, resp *middleware.MiddlewareResponse) (*middleware.MiddlewareResponse, error) {
    m.record(resp.Metadata["audit_input"], resp.Output)
    return resp, nil
}
```

`OnAfter` 只能看到响应；需要请求信息时在 `OnBefore` 写入 `Metadata`，并确认处理器会把请求元数据带到响应（Builder 的处理器会带上）。函数式写法：

```go
requireInput := middleware.NewMiddlewareFunc("require_input",
    func(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
        if s, ok := req.Input.(string); !ok || s == "" {
            return nil, errors.New("输入不能为空")
        }
        return req, nil
    },
    nil, nil,
)

agent, err := builder.NewSimpleBuilder(client).
    WithMiddleware(requireInput, newAuditMiddleware(record)).
    Build()
```

上例中 `requireInput` 的 `OnError` 为 nil，`OnBefore` 的错误原样中止执行。

## 相关文档

- [核心 API 参考](CORE_API.md)
- [工具中间件指南](../guides/TOOL_MIDDLEWARE.md)
- [设计概述](../design/DESIGN_OVERVIEW.md)
