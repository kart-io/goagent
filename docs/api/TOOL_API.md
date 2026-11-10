<!-- generated-by: gsd-doc-writer -->
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

`interfaces` 还定义了 `ToolCall`（调用记录）。

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

## 内置工具

| 包 | 构造函数 |
|---|---|
| `tools/compute` | `NewCalculatorTool()`、`NewAdvancedCalculatorTool(CalculatorOperations)` |
| `tools/http` | `NewAPITool(baseURL, timeout, headers)`、`NewAPIToolBuilder()`（`WithBaseURL`、`WithTimeout`、`WithHeader(s)`、`WithAuth`、`Build`） |
| `tools/search` | `NewSearchTool(engine SearchEngine)`；引擎：`NewDuckDuckGoSearchEngine()`、`NewGoogleSearchEngine(apiKey, cx)`、`NewAggregatedSearchEngine(engines...)`、`NewMockSearchEngine()` |
| `tools/shell` | `NewShellTool(allowedCommands, timeout)`（只执行白名单命令）、`NewShellToolBuilder()`、`CommonShellTools()` |

```go
shellTool := shell.NewShellTool([]string{"ls", "cat", "grep"}, 10*time.Second)
calculator := compute.NewCalculatorTool()
apiTool := http.NewAPITool("https://api.example.com", 30*time.Second,
    map[string]string{"Authorization": "Bearer token"})
searchTool := search.NewSearchTool(search.NewDuckDuckGoSearchEngine())
```

不在本模块的工具：编码工具（`tools/coding`）在 `github.com/kart-io/goagent-harness`；网页爬取与文件操作工具在 [`github.com/kart-io/goagent-contrib/tools/practical`](https://github.com/kart-io/goagent-contrib/tree/master/tools/practical)，数据库查询工具在其子模块 `tools/database`。

## 工具中间件

`tools/middleware` 通过统一函数包装单个工具调用：

```go
type ToolInvoker func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error)
type ToolMiddlewareFunc func(interfaces.Tool, ToolInvoker) ToolInvoker

func Wrap(tool interfaces.Tool, middlewares ...ToolMiddlewareFunc) interfaces.Tool
```

| 中间件 | 有状态对象 | 装配函数 |
|---|---|---|
| 日志 | `NewLoggingMiddleware(WithLogger, WithMaxArgBytes, WithoutInputLogging, WithoutOutputLogging)` | `Logging(...)` |
| 缓存 | `NewCachingMiddleware(WithCache, WithTTL, WithCacheKeyFunc, WithCacheLogger)` | `Caching(...)` |
| 限流 | `NewRateLimitMiddleware(WithQPS, WithBurst, WithPerToolRateLimit, WithWaitTimeout, WithRateLimitLogger)` | `RateLimit(...)` |

对象通过显式的 `.Wrap` 方法装配；函数按列表顺序从外向内调用，返回时反向经过各层。调用方自定义函数应保留结果与错误的组合。日志计时不写入输入参数。空函数或返回空调用器在装配时失败，其他类型不能通过编译。

```go
wrapped := middleware.Wrap(searchTool,
    toolmw.Logging(),
    toolmw.RateLimit(toolmw.WithQPS(5)),
)
```

详见 [工具中间件指南](../guides/TOOL_MIDDLEWARE.md)。

## 注册与执行

```go
func NewRegistry() *Registry
func (r *Registry) Register(tool interfaces.Tool) error
func (r *Registry) Get(name string) interfaces.Tool
func (r *Registry) List() []interfaces.Tool
func (r *Registry) Names() []string
func (r *Registry) Clear()
func (r *Registry) Size() int

func NewDispatcher(registry *Registry, cfg DispatchConfig) (*Dispatcher, error)
func (d *Dispatcher) Invoke(ctx context.Context, req ToolRequest) (*interfaces.ToolOutput, error)
func (d *Dispatcher) Tools() []interfaces.Tool
func (d *Dispatcher) AsRunnable() *core.StreamRunnable[ToolRequest, *interfaces.ToolOutput]
```

Get 未找到返回 nil；List/Names 不保证顺序。Dispatcher 快照 Registry；后续注册变更不影响既有 Dispatcher。底层工具须能承受配置并发，否则设 MaxConcurrency=1。ToolRequest 包含 Name 和 Input。

| DispatchConfig | 默认值与作用 |
| --- | --- |
| MaxConcurrency | 4；执行并发，审批等待不占此槽 |
| MaxAdmissionConcurrency | 0；零值不限准入并发，正值单独限制校验和管线准入 |
| MaxQueued | 0；零值不限排队，正值限制两阶段合计等待，超限返回 ErrDispatcherBusy |
| Timeout | 30秒；校验、宿主检查、日志与执行累计计时，准入/审批和排队暂停单次计时 |
| MaxAttempts | 3；持久操作的显式恢复上限，不是一次 Invoke 中自动重试 |
| ReturnArgumentErrors | false；只影响 Tools() 模型调用路径 |
| Authorize | 宿主检查，可能收到不合 Schema 的参数；error 中止 |
| OnResult | 首次与完成回放均调用，宿主应据稳定 ExecutionKey 幂等重建状态 |
| Pipeline | 可选调用管线 |

实现 DispatchTimeoutProvider 的工具可用正值覆盖默认单次超时，零值沿用默认；直接 Tool.Invoke 仍只受宿主 context 约束。宿主及根预算期限始终有效。OnResult 不受工具单次 Timeout 限制，应合作处理宿主 context。框架不能强杀忽略 context 的同步工具。

## 参数错误、拒绝与恢复

- error 非 nil 表示调用未正常完成，例如网络中断、取消或内部失败。
- `tools.Rejected(message, data)` 是明确业务拒绝，作为结果交给模型；持久日志记为 completed，恢复只回放。用 RejectedResult 判断，自行构造同形状 map 不会被识别为拒绝。
- ReturnArgumentErrors=true 时，Tools() 把 Schema 错误或本层 *tools.ArgumentError 转为 Success=false、Error为空的结果，让模型修正；不转换授权、超时、取消和子流程执行错误。
- 直接 Dispatcher.Invoke/AsRunnable 不启用上述参数结果恢复，仍返回 error。参数修复不等于向用户自动澄清。

持久调用要求宿主日志、执行键、稳定 CallID 和支持持久调用的入口。completed 回放不重新调用工具，也跳过 Pipeline 准入；Schema/宿主检查与 OnResult 有各自调用契约。首次与回放均经过 JSON 边界，数字解码为 json.Number，不能依赖原 Go 指针类型。

pending/failed 不证明外部效果未发生。服务须实现幂等，或由 ReconcilableTool.Reconcile 核对效果；框架不承诺任意工具 exactly-once。见 [执行治理](../guides/EXECUTION_GOVERNANCE.md)和[拒绝/核对源码](../../tools/rejected.go)。

## Pipeline

Pipeline 是工具调用扩展点，不是 Agent 外围中间件：

```text
Schema/自定义验证与宿主检查 → PreHook → 审批 → Guard → Around → 工具 → PostHook
```

- Pre：[]PreHook，返回 PreDecision；优先级 Stop > Deny > Substitute > Ask > Allow。
- Approver：Approve(ctx, ApprovalRequest) (ApprovalResult,error)；ApprovalTimeout 默认10分钟。
- Guards：[]Guard，非空原因即拒绝；人的批准不能覆盖 Guard。
- Around：[]Around，包装执行；Post：[]PostHook，处理成功及 Rejected，可替换输出、追加上下文。
- GrantTTL：会话批准有效期，非正值不设过期；Grants、RevokeGrants、PruneGrants 查询和回收范围。

参数非法不进入 Pipeline。Pre/审批/Guard 在操作日志之外；准入错误不留下 pending，明确拒绝结果会写日志。completed 回放跳过准入。Pipeline 审批不等价于可恢复工作流暂停。

## Runnable、Workflow 与子 Agent

```go
func NewRunnableTool[I, O any](
    run core.Runnable[I, O], cfg RunnableToolConfig[I, O],
) (interfaces.Tool, error)

type OwnedRunnable[I, O any] struct {
    Runnable core.Runnable[I, O]
    Close func(context.Context) error
}

func NewRunnableToolFactory[I, O any](
    factory func(context.Context) (OwnedRunnable[I, O], error),
    cfg RunnableToolConfig[I, O],
) (interfaces.Tool, error)
```

Config 必填 Name、Version、对象 ArgsSchema、Decode、Encode，Description 描述业务用途。共享无状态 Runnable 用 NewRunnableTool；有状态子 Agent 用工厂，宿主分配独立会话、记忆命名空间及 Dispatcher。

| Config 字段 | 契约 |
| --- | --- |
| ModelInvoking | 模型型工具设 true；缺根预算时在工厂前拒绝，不自动检测隐藏模型请求 |
| MaxDepth | 默认8，祖先更小限制继续生效 |
| CleanupTimeout | 默认5秒，Close 必填（可为 no-op），成功、失败和取消均清理 |
| Timeout | 经 Dispatcher 的单次超时声明；直接调用受宿主 context 约束 |
| Resumable | 默认 false；开启须经带日志 Dispatcher，子步骤在同一 Store 和稳定派生键下记录 |

默认不透明复合工具只尝试一次：completed 可回放，未完成不能假定自动进入子检查点。嵌套审批/恢复显式使用 Resumable 与 DurableGraph.AsChildRunnable；等待经 WaitError 传播，不能 Encode 为成功。

工厂部分失败仍应提供已取得资源的 Close。清理 context 不继承父取消，受 CleanupTimeout 约束；不合作的清理不能强制终止，清理失败阻止工具完成。见 [编排指南](../guides/COMPOSABLE_ORCHESTRATION.md)。

## 其他入口与工具位置

| 核心子包 | 当前构造器 |
| --- | --- |
| tools/compute | NewCalculatorTool() *CalculatorTool |
| tools/http | NewAPITool(baseURL string, timeout time.Duration, headers map[string]string) *APITool |
| tools/search | NewSearchTool(engine SearchEngine) *SearchTool |
| tools/shell | NewShellTool(allowedCommands []string, timeout time.Duration) *ShellTool |

编码工具已移至 github.com/kart-io/goagent-harness/tools/coding，不再从核心导入。参见 [Harness 迁移](../guides/HARNESS_MIGRATION.md)。外部模块、示例层业务集成与核心工具应分别说明。
