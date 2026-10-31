<!-- generated-by: gsd-doc-writer -->
# GoAgent Middleware API 参考

Builder 的请求/响应中间件来自 `github.com/kart-io/goagent/core/middleware`，在一次 Agent 执行外围运行，不等同于每步模型钩子或每次工具管线。

源码：[核心接口与链](../../core/middleware/middleware.go)、[附加实现](../../core/middleware/advanced.go)、[Builder 挂载](../../builder/runtime_options.go)、[执行入口](../../builder/configurable_agent.go)。

## 选择扩展层

| 处理范围 | 入口 |
| --- | --- |
| Agent 请求输入、最终输出与错误 | core/middleware.Middleware + WithMiddleware |
| 每步模型前改写上下文 | WithPreStep / WithIdentifiedPreStep |
| 调整请求、处理模型失败 | WithRequestHook / WithRequestError |
| 每次工具调用 | tools.Pipeline |
| 只读观察 | core/observe.Observer、Tracer |

新编排用 agent.AsRunnable().Invoke/Stream；它与 ExecuteStream 共用执行源，外围中间件仍生效。流式预览可能先输出，OnAfter 只处理最终结果，不能撤回已发送文本。见 [扩展契约](../guides/EXTENSION_CONTRACT.md)。

## 接口与数据

```go
type Middleware interface {
    Name() string
    OnBefore(context.Context, *MiddlewareRequest) (*MiddlewareRequest, error)
    OnAfter(context.Context, *MiddlewareResponse) (*MiddlewareResponse, error)
    OnError(context.Context, error) error
}

type Handler func(context.Context, *MiddlewareRequest) (*MiddlewareResponse, error)
```

| 类型 | 字段 |
| --- | --- |
| MiddlewareRequest | Input interface{}、State state.State、Runtime interface{}、Metadata map[string]interface{}、Headers map[string]string、Timestamp time.Time |
| MiddlewareResponse | Output interface{}、State state.State、Metadata map[string]interface{}、Headers map[string]string、Duration time.Duration、TokenUsage *interfaces.TokenUsage、Error error |

state.State 指 core/state.State，不能与任意 workflow.Graph[T] 的 T 混为一谈。Request.State、Request.Runtime、Response.State、Response.Error 的 JSON 标签为 json:"-"，不是自动序列化的快照。Headers 是库内字段，不表示框架提供 HTTP 服务。

## MiddlewareChain

```go
func NewMiddlewareChain(handler Handler) *MiddlewareChain
func (c *MiddlewareChain) Use(middleware ...Middleware) *MiddlewareChain
func (c *MiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error)
func (c *MiddlewareChain) WithHandler(handler Handler) *MiddlewareChain
func (c *MiddlewareChain) Size() int
```

正常顺序：A.OnBefore → B.OnBefore → handler → B.OnAfter → A.OnAfter。

- OnBefore 按注册顺序，OnAfter 按逆序。WithHandler 快照列表并更换本次 handler，中间件实例仍共享。
- Use 写时复制列表，不保证实例字段、宿主 State 或复用请求对象可并发修改；自定义实例须自行处理并发。
- OnBefore/OnAfter 出错时调用同一个中间件的 OnError，非 nil 中止，nil 则继续；不能因此返回 nil 对象又假定后续组件都接受。
- handler 出错后，链按注册顺序通知各 OnError，非 nil 返回更新错误；不会仅因某个 OnError 返回 nil 就抹掉原 handler 错误，也不会自动重做 handler。
- OnBefore 可在 Metadata 放入 cached_response（非 nil *MiddlewareResponse）跳过 handler，仍运行 OnAfter；缓存不是持久检查点。
- Builder 的 Agent 执行边界把链内 panic 转为错误；裸 MiddlewareChain.Execute 本身不提供同样的 panic 隔离。

中间件错误不会回滚已发生的业务效果。

## 无网络 Builder 示例

```go
package main

import (
    "context"
    "fmt"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/core/middleware"
    "github.com/kart-io/goagent/llm"
)

func main() {
    if err := run(); err != nil {
        panic(err)
    }
}

func run() error {
    prefix := middleware.NewMiddlewareFunc("input-prefix",
        func(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
            if err := ctx.Err(); err != nil {
                return nil, err
            }
            req.Input = "请简短回答：" + fmt.Sprint(req.Input)
            return req, nil
        }, nil, nil)
    agent, err := builder.NewSimpleBuilder(llm.NewMockStreamClient()).
        WithMiddleware(prefix).
        BuildContext(context.Background())
    if err != nil {
        return err
    }
    out, err := agent.AsRunnable().Invoke(context.Background(), "演示中间件")
    if err != nil {
        return err
    }
    fmt.Println(out.Result)
    return nil
}
```

使用内置 mock 的固定流式答复，不发网络请求；示例只证明中间件挂载和执行路径，不证明真实模型对改写内容的理解。

## 创建与复用

```go
func NewBaseMiddleware(name string) *BaseMiddleware
func NewMiddlewareFunc(
    name string,
    before func(context.Context, *MiddlewareRequest) (*MiddlewareRequest, error),
    after func(context.Context, *MiddlewareResponse) (*MiddlewareResponse, error),
    onError func(context.Context, error) error,
) *MiddlewareFunc
```

BaseMiddleware 的 Before/After 原样返回对象，OnError 原样返回错误。MiddlewareFunc 的 nil 函数字段沿用对应默认行为。

| core/middleware 构造器 | 当前行为 |
| --- | --- |
| NewLoggingMiddleware(logger func(string)) | 记录输入、输出与错误；nil logger 使用标准输出 |
| NewTimingMiddleware() | 计时，提供 GetTimings/GetAverageLatency |
| NewCacheMiddleware(ttl time.Duration) | 进程内响应缓存 |
| NewCacheMiddlewareWithShards(ttl time.Duration, numShards uint32) | 分片进程内缓存 |
| NewDynamicPromptMiddleware(modifier func(*MiddlewareRequest) string) | 修改外围输入 |
| NewValidationMiddleware(validators ...func(*MiddlewareRequest) error) | 验证外围请求 |
| NewRateLimiterMiddleware(maxRequests int, windowSize time.Duration) | 进程内窗口计数，不是根模型预算 |

其他组件和准确构造签名见 [advanced.go](../../core/middleware/advanced.go)。缓存、限流等中间件不自动提供分布式一致性或持久恢复。

### RetryMiddleware 不会自动重试

NewRetryMiddleware(maxRetries int, backoff time.Duration) 和 WithRetryCondition 仍存在；当前 OnError 仅包装符合条件的错误，**不会重新调用 handler**。不能用它承诺模型或业务工具实际重试。

模型暂时失败使用 WithRequestError(n, builder.RetryTransient(n, backoff))；业务效果重试另按工具契约处理。参见 [Builder API](../guides/BUILDER_API_REFERENCE.md)。

### 链实现不能随意互换

[optimized_chain.go](../../core/middleware/optimized_chain.go) 另提供 NewImmutableMiddlewareChain(handler,middlewares...) 和 NewFastMiddlewareChain(handler,middlewares...)。FastMiddlewareChain 只运行 OnBefore 与 handler，不运行 OnAfter/OnError。Builder 当前用 MiddlewareChain，不能换成快速链后仍假定语义一致。

## 同名包与 Process 组件

顶层 github.com/kart-io/goagent/middleware 与 core/middleware 不同：

- [ObservabilityMiddleware](../../middleware/observability.go) 实现请求/响应钩子，接收已有 TelemetryProvider。
- [tool_selector_advanced.go](../../middleware/tool_selector_advanced.go) 部分组件提供 Process(ctx, core.State) (core.State,error)。该方法不会因 WithMiddleware 自动被调用；即便嵌入 BaseMiddleware 满足接口，Process 也不是 OnBefore。
- core/middleware.ToolSelectorMiddleware 将 selected_tools 写入 Metadata；不能据此推断它自动改变每次原生模型请求的工具清单。

接入时按具体扩展协议选择，而不是只看类名或包名。

## 对象池与资源

GetMiddlewareRequest/Response、PutMiddlewareRequest/Response 是可选对象池辅助函数；取出后初始化本次字段，归还后不得继续持有或并发使用。对象池不提供状态持久化。

WithHandler 和不同 Execute 共享中间件实例，不自动关闭宿主模型、存储、观察者。取消依赖实际 handler/中间件响应 context，不强制终止不合作的同步实现。
