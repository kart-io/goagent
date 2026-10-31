# tools 工具系统

`interfaces.Tool` 是 Agent 可调用的工具接口；本包提供工具的创建方式、注册表、Dispatcher 执行路径（校验、管线、日志、并发与超时）以及工具结果缓存。内置工具位于子包。

## Tool 接口

```go
type Tool interface {
    Name() string
    Description() string
    Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error)
    ArgsSchema() string // 参数 JSON Schema
}
```

`interfaces.ToolInput` 的主要字段为 `Args map[string]interface{}`、`CallerID`、`TraceID`、`CallID`、`ExecutionKey`；`interfaces.ToolOutput` 为 `Result`、`Success`、`Error`、`Metadata`、`AdditionalContext`、`Labels`。工具不是 `core.Runnable`；需要把 Runnable 暴露为工具时用 `NewRunnableTool`。

## 子包

| 子包 | 内容 |
|---|---|
| `tools/compute` | `CalculatorTool`（参数 `expression`，支持 `+ - * / ^` 与括号）、`AdvancedCalculatorTool`（参数 `operation`、`operands`，由 `CalculatorOperations` 选择开放的操作） |
| `tools/search` | `SearchTool` 与 `SearchEngine` 接口；引擎有 `MockSearchEngine`、`GoogleSearchEngine`、`DuckDuckGoSearchEngine`、`AggregatedSearchEngine` |
| `tools/shell` | 命令白名单 Shell 工具，见 [tools/shell](shell/README.md) |
| `tools/http` | `APITool`：参数 `method`、`url`、`headers`、`body`、`timeout`；另有 `Get`/`Post`/`Put`/`Delete`/`Patch` 便捷方法与 `NewAPIToolBuilder` |
| `tools/middleware` | 单个工具的包装中间件（日志、缓存、限流），见 [工具中间件](../docs/guides/TOOL_MIDDLEWARE.md) |
| `tools/policy` | 按工具名与命令前缀裁决的权限规则，作为 `Pipeline` 的 PreHook |
| `tools/spill` | 把超长结果写入宿主目录并以 `read_spill` 工具分段读取，作为 `Pipeline` 的 PostHook |

编码 Agent 的工作区工具在独立模块 `github.com/kart-io/goagent-harness/tools/coding`，见 [迁移指南](../docs/guides/HARNESS_MIGRATION.md)。

## 创建工具

```go
// 任意函数：直接处理 ToolInput/ToolOutput
hello := tools.NewBaseTool("hello", "Says hello to a name",
    `{"type":"object","properties":{"name":{"type":"string"}}}`,
    func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
        name, _ := input.Args["name"].(string)
        return &interfaces.ToolOutput{Result: "Hello, " + name, Success: true}, nil
    })

// 参数 map → 结果，自动包装为 ToolOutput
mul, err := tools.NewFunctionToolBuilder("multiplier").
    WithDescription("Multiplies two numbers").
    WithArgsSchema(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        return args["a"].(float64) * args["b"].(float64), nil
    }).
    Build()

// 无参数
now := tools.NewSimpleFunctionTool("get_time", "Gets current time",
    func(ctx context.Context) (interface{}, error) { return time.Now().String(), nil })

// 类型化参数：Args 经 JSON 解码为 I
type CalcInput struct {
    A float64 `json:"a"`
    B float64 `json:"b"`
}
add := tools.NewTypedFunctionTool("add", "Adds two numbers",
    `{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}}}`,
    func(ctx context.Context, in CalcInput) (float64, error) { return in.A + in.B, nil })
```

另有 `NewFunctionTool(name, description, schema, fn)`，以及 `FunctionToolBuilder.WithArgsSchemaFromStruct`、`MustBuild`。

### 结果约定

- `tools.Rejected(message, data)`：目标系统给出的业务结论（不存在、无权限、状态不允许等）。作为工具结果交给模型，经 Dispatcher 时记为已完成，恢复时只回放不重试；`message` 原样交给模型。`RejectedResult(out)` 识别该结果。
- `*tools.ArgumentError`：模型生成的参数不合法，`Message` 原样交给模型改正，`Err` 只用于 `errors.Is/As`。`ArgumentErrorResult(out)` 识别交还模型的参数错误结果。
- `error`：调用没有完成（网络中断、服务不可用、超时）。

## 注册表

```go
registry := tools.NewRegistry()
if err := registry.Register(compute.NewCalculatorTool()); err != nil { // 同名重复注册返回错误
    return err
}
calc := registry.Get("calculator") // 不存在时返回 nil
all := registry.List()
```

另有 `Names`、`Size`、`Clear`。

## Dispatcher

`Dispatcher` 对注册表做快照，为 Builder 工具调用与图节点提供同一执行路径：参数校验、宿主授权、调用管线、持久日志、并发与超时。

```go
d, err := tools.NewDispatcher(registry, tools.DispatchConfig{
    MaxConcurrency:       4,                // 默认 4
    Timeout:              30 * time.Second, // 单次调用默认超时，默认 30 秒
    ReturnArgumentErrors: true,             // 参数错误交还模型改正，而不是中止
    Pipeline: &tools.Pipeline{
        Pre:  []tools.PreHook{(&policy.Policy{Default: policy.Allow}).Hook()},
        Post: []tools.PostHook{spillStore.Hook()},
    },
})
if err != nil {
    return err
}

agent, err := builder.NewSimpleBuilder(client).
    WithTools(append(d.Tools(), spillStore.ReadTool())...).
    Build()

// 宿主直接调用
out, err := d.Invoke(ctx, tools.ToolRequest{Name: "calculator", Input: &interfaces.ToolInput{
    Args: map[string]interface{}{"expression": "2 + 3 * 4"},
}})
```

- `Tools()` 返回供 `WithTools` 使用的包装工具；`AsRunnable()` 返回 `*core.StreamRunnable[ToolRequest, *interfaces.ToolOutput]`；单次 `Invoke` 内不重试。
- `DispatchConfig` 另有 `MaxAttempts`（持久日志中同一 CallID 的恢复尝试上限，默认 3）、`MaxAdmissionConcurrency`、`MaxQueued`（超出返回 `ErrDispatcherBusy`）、`Authorize`（宿主授权，拒绝即中止）、`OnResult`（每次得到结果后同步回调，回放也触发）。
- `Pipeline` 依次执行 `Pre`（`PreHook` 裁决 `PreAllow`、`PreDeny`、`PreAsk`、`PreStop` 或 `PreSubstitute`）→ `Approver` 审批 → `Guards` → `Around`（计入超时）→ 工具 → `Post`（替换结果或追加上下文）。准入阶段（Pre、审批、Guard）在操作日志之外，不计入 `Timeout`。会话级批准用 `tools.WithSessionID(ctx, id)` 区分会话，`Grants`/`RevokeGrants`/`PruneGrants` 管理已授予的批准。
- 工具可实现 `DispatchTimeoutProvider`（自定义超时）、`DurablePolicyProvider`（持久运行中的操作策略）、`DurableIntentVersioner` 等可选接口。

扩展点的错误与恢复语义见 [扩展契约](../docs/guides/EXTENSION_CONTRACT.md)。

## 其他组件

- `ToolExecutor`（`NewToolExecutor(WithMaxConcurrency, WithTimeout, WithRetryPolicy, WithErrorHandler)`）：`ExecuteParallel`、`ExecuteSequential`、`ExecuteBatch`，以及按 `ToolGraph` 依赖执行的 `ExecuteWithDependencies`。不经过 Dispatcher 的日志与管线。
- `NewRunnableTool` / `NewRunnableToolFactory`：把 `core.Runnable`（如子 Agent、workflow 图）适配为工具，`RunnableToolConfig` 声明编解码、版本、超时与是否可恢复。
- `WithMiddleware(tool, middlewares...)`：用 `tools/middleware` 包装单个工具。
- 结果缓存：`NewCachedTool(tool, ttl)`，以及实现 `ToolCache` 的 `SimpleToolCache`、`MemoryToolCache`、`ShardedToolCache`（`NewShardedToolCacheWithOptions`、`NewHighPerformanceCache`、`NewLowMemoryCache`）。
- `InputValidator`、`ValidateAndInvoke`：按 Schema 校验参数。
- `ToolRuntime` 与 `UserInfoTool`、`SavePreferenceTool`、`UpdateStateTool`：可访问状态与存储的运行时工具。

## 示例与测试

示例见 [examples/tools](../examples/tools/README.md)：

```bash
go run ./examples/tools/01-calculator
go test ./tools/...
```
