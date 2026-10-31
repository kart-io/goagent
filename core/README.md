# core 核心执行引擎

提供统一的可执行接口 `Runnable[I, O]`、Agent/Chain/Orchestrator 基础类型、回调、Generator 流处理和 panic 隔离等基础设施。

通用图编排位于 [workflow](../workflow/README.md)：条件分支、并行 Clone/Merge、有界循环和 Runnable/Agent 适配，编译结果复用 `core.StreamRunnable`。图编排与 `core/durable` 的顺序步骤恢复是独立能力；入口、流式和持久化边界以[执行契约](../docs/guides/EXECUTION_CONTRACT.md)为准。

## 核心组件

### Runnable

所有可执行组件都实现 `Runnable[I, O]`：

| 方法 | 说明 |
|------|------|
| `Invoke(ctx, input)` | 单次执行 |
| `Stream(ctx, input)` | 流式执行，返回 `<-chan StreamChunk[O]` |
| `Batch(ctx, inputs)` | 批量执行 |
| `Pipe(next)` | 管道连接，返回 `Runnable[I, any]` |
| `WithCallbacks(...)` | 返回带回调的新 `Runnable[I, O]` |
| `WithConfig(config)` | 返回带配置的新 `Runnable[I, O]` |

实现：`RunnableFunc`（包装函数）、`RunnablePipe`、`RunnableSequence`、`StreamRunnable`（以 `StreamSource` 为唯一执行源，`Invoke` 消费同一数据流；builder 的 `Agent.AsRunnable()` 与 workflow 编译结果均返回它）。`BaseRunnable` 提供回调、配置与 `Batch` 辅助，供具体类型嵌入。

### Agent、Chain、Orchestrator

```go
type Agent interface {
    Runnable[*AgentInput, *AgentOutput]
    Name() string
    Description() string
    Capabilities() []string
}

type Chain interface {
    Runnable[*ChainInput, *ChainOutput]
    Name() string
    Steps() int
}

type Orchestrator interface {
    Execute(ctx context.Context, request *OrchestratorRequest) (*OrchestratorResponse, error)
    RegisterAgent(name string, agent Agent) error
    RegisterChain(name string, chain Chain) error
    RegisterTool(name string, tool interfaces.Tool) error
    Name() string
}
```

- `BaseAgent`：嵌入用基础实现。默认 `Invoke` 返回 `Status: "failed"` 的输出；`Stream`、`Batch`、`RunGenerator` 调用的是 `BaseAgent.Invoke`，嵌入类型重写 `Invoke` 后需一并重写这些方法。
- `ChainableAgent`：依次调用多个 Agent，前一个的输出作为下一个的输入。
- `AgentExecutor`：包装 Agent，提供 `WithMaxRetries`、`WithTimeout`、`WithStopOnError` 选项。
- `BaseChain`：按顺序执行 `Step`（`Execute(ctx, input) (interface{}, error)`、`Name()`、`Description()`）。
- `BaseOrchestrator`：管理 Agent/Chain/Tool 注册；`Execute` 返回 `ErrNotImplemented`，需嵌入后实现。

完整的 LLM Agent 由 [builder](../builder/) 构建，不直接使用 `BaseAgent`。

### Callback

`Callback` 接口包含 `OnStart/OnEnd/OnError`、`OnLLMStart/OnLLMEnd/OnLLMError`、`OnChainStart/OnChainEnd/OnChainError`、`OnToolStart/OnToolEnd/OnToolError`、`OnAgentAction/OnAgentFinish`。返回非 nil 错误会中止执行。嵌入 `BaseCallback` 只需实现关心的方法。

内置：`NewLoggingCallback(logger Logger, verbose bool)`、`NewMetricsCallback(MetricsCollector)`、`NewTracingCallback(Tracer)`、`NewCostTrackingCallback(pricing)`、`NewStdoutCallback(color)`，以及组合多个回调的 `NewCallbackManager(...)`。

### State 与 Checkpoint

`core/state.State`（`core.State` 为别名）：`Get`、`Set`、`Update`、`Snapshot`、`Clone`、`Delete`、`Clear`、`Keys`、`Size`。`NewAgentState()` 返回线程安全实现，另有 `GetString`/`GetInt`/`GetBool`/`GetFloat64`/`GetMap`/`GetSlice`。`state` 包另提供带 schema 校验的 `TypedState`。

`core/checkpoint.Checkpointer`：`Save`、`Load`、`List`、`Delete`、`Exists`。实现有 `NewInMemorySaver()`、`NewDistributedCheckpointer(config) (*DistributedCheckpointer, error)`，以及定期清理的 `NewCheckpointerWithAutoCleanup`。

## 使用方法

### 函数式 Runnable 与管道

```go
upper := core.NewRunnableFunc(func(ctx context.Context, s string) (string, error) {
    return strings.ToUpper(s), nil
})
length := core.NewRunnableFunc(func(ctx context.Context, s string) (int, error) {
    return len(s), nil
})

pipe := core.NewRunnablePipe[string, string, int](upper, length)
n, err := pipe.Invoke(ctx, "hello") // 5
```

### Chain

```go
chain := core.NewBaseChain("data-pipeline", []core.Step{collectStep, processStep})

output, err := chain.Invoke(ctx, &core.ChainInput{
    Data: rawData,
    Vars: map[string]interface{}{"format": "json"},
})
if err != nil {
    return err
}
fmt.Println(output.Data, len(output.StepsExecuted))
```

`GetChainInput`/`PutChainInput`、`GetChainOutput`/`PutChainOutput` 提供对象池复用。

### Generator

`Generator[T]` 即 `iter.Seq2[T, error]`，可直接 `range`，提前 `break` 即停止：

```go
gen := agent.RunGenerator(ctx, input) // agent 为 *core.BaseAgent 或重写了 RunGenerator 的类型

for out, err := range gen {
    if err != nil {
        return err
    }
    fmt.Println(out.Result)
}

ok := core.Filter(gen, func(o *core.AgentOutput) bool { return o.Status == "success" })
results, err := core.Collect(core.Take(ok, 10))
```

另有 `Map`、`GeneratorFunc`、`FromChannel`、`ToChannel`。

### 快速调用

`FastInvoker.InvokeFast` 跳过回调与中间件，适合内部嵌套调用；外部入口应使用 `Invoke`。

```go
output, err := core.TryInvokeFast(ctx, agent, input) // 不支持 FastInvoker 时回退到 Invoke
```

### 状态与检查点

```go
st := core.NewAgentState()
st.Set("user_id", "12345")

saver := checkpoint.NewInMemorySaver()
err := saver.Save(ctx, "session-001", st)
restored, err := saver.Load(ctx, "session-001")
```

## 代码结构

```text
core/
├── runnable.go / stream_runnable.go  # Runnable 接口与实现
├── agent.go / fast_invoker.go        # Agent 基础类型、FastInvoker
├── chain.go / orchestrator.go        # Chain、Orchestrator
├── callback.go                       # 回调
├── generator.go                      # Generator[T]
├── interrupt.go                      # 人工介入（InterruptManager、InterruptableExecutor）
├── lifecycle_manager.go              # 生命周期管理
├── plugin_bridge.go / plugin_registry_enhanced.go  # 插件注册与类型适配
├── panic_handler.go / timeout.go / errors.go
├── budget/       # 宿主级模型请求预算
├── checkpoint/   # 会话状态持久化
├── durable/      # 显式版本化步骤的持久运行与恢复
├── execution/    # Runtime 与流式类型
├── flow/         # 实验性同步只读增量管道
├── middleware/   # 请求/响应中间件（OnBefore/OnAfter/OnError）
├── observe/      # 可选执行事件
├── session/      # 会话事件日志，见 session/README.md
└── state/        # 状态管理
```

## 扩展阅读

- [builder](../builder/) - Agent 构建器
- [workflow](../workflow/README.md) - 图编排
- [core/session](session/README.md) - 会话事件日志
