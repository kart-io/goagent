# GoAgent Tool API 参考

本文概述工具接口、`tools` 包的构造方式、执行器与内置工具。完整签名见：

```bash
go doc -all github.com/kart-io/goagent/interfaces
go doc -all github.com/kart-io/goagent/tools
go doc -all github.com/kart-io/goagent/tools/middleware
```

## Tool 接口

```go
type Tool interface {
    Name() string        // 唯一标识，建议小写加下划线
    Description() string // 供模型判断何时使用
    Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error)
    ArgsSchema() string  // 参数的 JSON Schema
}
```

可选接口（包装器须显式转发，否则视为未声明）：

| 接口 | 方法 | 作用 |
|---|---|---|
| `ValidatableTool` | `Validate(ctx, *ToolInput) error` | 执行前的自定义校验 |
| `ArgumentErrorTool` | `ReturnsArgumentErrors() bool` | 参数错误作为结果交还模型改正，宿主不预先拒绝 |
| `ConcurrentCallTool` | `SupportsConcurrentCalls() bool` | 允许同一轮多个调用并发；还需 Builder `WithParallelToolCalls(n>=2)` |
| `DurableCallTool` | `SupportsDurableCalls() bool` | `Invoke` 按 `ToolInput.CallID` 记日志并回放已完成结果 |

`tools.Dispatcher.Tools()` 返回的工具实现后三个接口。

### ToolInput / ToolOutput

```go
type ToolInput struct {
    CallID       string // 宿主分配的稳定子操作 ID，不从模型参数读取
    ExecutionKey string // 宿主执行键，副作用服务据此实现幂等；为空表示没有持久身份
    Args         map[string]interface{}
    Context      context.Context // 不序列化
    CallerID     string
    TraceID      string
}

type ToolOutput struct {
    Result            interface{}
    Success           bool
    Error             string
    Metadata          map[string]interface{}
    AdditionalContext []string          // 调用后钩子追加，Builder 在本批工具结果后作为 user 消息注入
    Labels            map[string]string // 预留的来源/可信度标签，目前不传播
}
```

`interfaces` 还定义了 `ToolCall`（调用记录）、`ToolResult`（`ToolName`、`Output`、`ExecutionTime`）和 `ToolExecutor` 接口（`ExecuteTool`、`ListTools`）；仓库内没有 `interfaces.ToolExecutor` 的实现。

## 构造工具

### BaseTool

```go
func NewBaseTool(
    name, description, argsSchema string,
    runFunc func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error),
) *BaseTool
```

```go
echoTool := tools.NewBaseTool(
    "echo",
    "原样返回输入文本",
    `{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`,
    func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
        text, _ := input.Args["text"].(string)
        return &interfaces.ToolOutput{Result: text, Success: true}, nil
    },
)
```

### FunctionTool

```go
func NewFunctionTool(name, description, argsSchema string,
    fn func(context.Context, map[string]interface{}) (interface{}, error)) *FunctionTool
func NewSimpleFunctionTool(name, description string, fn SimpleFunction) *FunctionTool // SimpleFunction: func(context.Context) (interface{}, error)
func NewTypedFunctionTool[I, O any](name, description, argsSchema string, fn TypedFunction[I, O]) *FunctionTool
```

`FunctionToolBuilder`（`NewFunctionToolBuilder(name)`）提供 `WithDescription`、`WithArgsSchema`、`WithArgsSchemaFromStruct`、`WithFunction`、`Build`、`MustBuild`。

```go
addTool := tools.NewFunctionTool(
    "add", "加法计算",
    `{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        a, _ := args["a"].(float64)
        b, _ := args["b"].(float64)
        return a + b, nil
    },
)

type AddInput struct {
    A float64 `json:"a"`
    B float64 `json:"b"`
}

typedAdd := tools.NewTypedFunctionTool[AddInput, float64](
    "typed_add", "类型安全的加法",
    `{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`,
    func(ctx context.Context, in AddInput) (float64, error) { return in.A + in.B, nil },
)

searchTool := tools.NewFunctionToolBuilder("search").
    WithDescription("搜索信息").
    WithArgsSchema(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        return "结果：" + fmt.Sprint(args["query"]), nil
    }).
    MustBuild()
```

其他构造方式：`NewRunnableTool` / `NewRunnableToolFactory` 把 `core.Runnable[I, O]` 包装成工具；`NewCachedTool(tool, ttl)` 为工具结果加缓存。

## Registry

```go
func NewRegistry() *Registry
func (r *Registry) Register(tool interfaces.Tool) error
func (r *Registry) Get(name string) interfaces.Tool // 不存在时返回 nil
func (r *Registry) List() []interfaces.Tool
func (r *Registry) Names() []string
func (r *Registry) Size() int
func (r *Registry) Clear()
```

## Dispatcher 与 Pipeline

`NewDispatcher(registry, DispatchConfig)` 在注册表之上提供治理后的调用：并发与排队上限（`MaxConcurrency` 默认 4）、单次 `Timeout`、`Authorize`、`OnResult`，以及 `Pipeline`（PreHook → 审批 → Guard → Around → 工具 → PostHook）。`Dispatcher.Tools()` 返回可交给 `builder.WithTools` 的工具，`Invoke` / `AsRunnable` 供直接调用。没有隐式重试。行为契约见 [执行治理](../guides/EXECUTION_GOVERNANCE.md) 与 [扩展契约](../guides/EXTENSION_CONTRACT.md)；`tools/policy` 提供基于规则的放行/询问/拒绝策略，`tools/spill` 把超长结果写入文件并提供 `read_spill` 读取工具。

## ToolExecutor

`tools.ToolExecutor` 直接执行一组已解析的调用：

```go
func NewToolExecutor(opts ...ExecutorOption) *ToolExecutor // WithMaxConcurrency、WithTimeout、WithRetryPolicy、WithErrorHandler

func (e *ToolExecutor) ExecuteParallel(ctx context.Context, calls []*ToolCallRequest) ([]*ToolResult, error)
func (e *ToolExecutor) ExecuteSequential(ctx context.Context, calls []*ToolCallRequest) ([]*ToolResult, error)
func (e *ToolExecutor) ExecuteBatch(ctx context.Context, tool interfaces.Tool, inputs []*interfaces.ToolInput) ([]*ToolResult, error)
func (e *ToolExecutor) ExecuteWithDependencies(ctx context.Context, graph *ToolGraph) ([]*ToolResult, error)
```

`ToolCallRequest` 含 `Tool`、`Input`、`ID`、`Dependencies`；`tools.ToolResult` 含 `CallID`、`Output`、`Duration`、`Error`。`RetryPolicy` 字段：`MaxRetries`、`InitialDelay`、`MaxDelay`、`Multiplier`、`Jitter`、`RetryableErrors`。依赖图用 `NewToolGraph` / `NewToolGraphBuilder` 构建。

```go
executor := tools.NewToolExecutor(
    tools.WithMaxConcurrency(10),
    tools.WithTimeout(30*time.Second),
    tools.WithRetryPolicy(&tools.RetryPolicy{
        MaxRetries:   3,
        InitialDelay: 100 * time.Millisecond,
        MaxDelay:     5 * time.Second,
        Multiplier:   2.0,
    }),
)

results, err := executor.ExecuteParallel(ctx, []*tools.ToolCallRequest{
    {ID: "1", Tool: addTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{"a": 1.0, "b": 2.0}}},
    {ID: "2", Tool: echoTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{"text": "hi"}}},
})
```

## 内置工具

| 包 | 构造函数 |
|---|---|
| `tools/compute` | `NewCalculatorTool()`、`NewAdvancedCalculatorTool(CalculatorOperations)` |
| `tools/http` | `NewAPITool(baseURL, timeout, headers)`、`NewAPIToolBuilder()`（`WithBaseURL`、`WithTimeout`、`WithHeader(s)`、`WithAuth`、`Build`） |
| `tools/search` | `NewSearchTool(engine SearchEngine)`；引擎：`NewDuckDuckGoSearchEngine()`、`NewGoogleSearchEngine(apiKey, cx)`、`NewAggregatedSearchEngine(engines...)`、`NewMockSearchEngine()` |
| `tools/shell` | `NewShellTool(allowedCommands, timeout)`（只执行白名单命令）、`NewShellToolBuilder()`、`CommonShellTools()` |
| `tools` | `NewSavePreferenceTool`、`NewUpdateStateTool`、`NewUserInfoTool`：`RuntimeTool`，通过 `ToolRuntime` 读写状态与存储，可用 `NewRuntimeToolAdapter(tool, runtime)` 绑定运行时 |

```go
shellTool := shell.NewShellTool([]string{"ls", "cat", "grep"}, 10*time.Second)
calculator := compute.NewCalculatorTool()
apiTool := http.NewAPITool("https://api.example.com", 30*time.Second,
    map[string]string{"Authorization": "Bearer token"})
searchTool := search.NewSearchTool(search.NewDuckDuckGoSearchEngine())
```

不在本模块的工具：编码工具（`tools/coding`）在 `github.com/kart-io/goagent-harness`；网页爬取与文件操作工具在 [`github.com/kart-io/goagent-contrib/tools/practical`](https://github.com/kart-io/goagent-contrib/tree/master/tools/practical)，数据库查询工具在其子模块 `tools/database`。

## 工具中间件

`tools/middleware` 在单个工具的调用前后插入逻辑：

```go
type ToolMiddleware interface {
    Name() string
    OnBeforeInvoke(ctx context.Context, tool interfaces.Tool, input *interfaces.ToolInput) (*interfaces.ToolInput, error)
    OnAfterInvoke(ctx context.Context, tool interfaces.Tool, output *interfaces.ToolOutput) (*interfaces.ToolOutput, error)
    OnError(ctx context.Context, tool interfaces.Tool, err error) error
}

type ToolMiddlewareFunc func(interfaces.Tool, ToolInvoker) ToolInvoker
```

| 中间件 | 接口式构造 | 函数式构造 |
|---|---|---|
| 日志 | `NewLoggingMiddleware(WithLogger, WithMaxArgBytes, WithoutInputLogging, WithoutOutputLogging)` | - |
| 缓存 | `NewCachingMiddleware(WithCache, WithTTL, WithCacheKeyFunc, WithCacheLogger)` | `Caching(...)` |
| 限流 | `NewRateLimitMiddleware(WithQPS, WithBurst, WithPerToolRateLimit, WithWaitTimeout, WithRateLimitLogger)` | `RateLimit(...)` |

`BaseToolMiddleware`（`NewBaseToolMiddleware(name)`）提供空实现。用 `tools.WithMiddleware(tool, mws...)` 包装工具，参数可混用 `ToolMiddleware` 与 `ToolMiddlewareFunc`；`middleware.Chain` 按洋葱模型组合（`OnBeforeInvoke` 正序、`OnAfterInvoke` 逆序）。

```go
wrapped := tools.WithMiddleware(searchTool,
    toolmw.NewLoggingMiddleware(),
    toolmw.RateLimit(toolmw.WithQPS(5)),
)
```

详见 [工具中间件指南](../guides/TOOL_MIDDLEWARE.md)。

## 相关文档

- [核心 API 参考](CORE_API.md)
- [LLM API 参考](LLM_API.md)
- [工具中间件指南](../guides/TOOL_MIDDLEWARE.md)
