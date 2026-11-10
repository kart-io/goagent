# core 核心执行引擎

提供统一的可执行接口 `Runnable[I, O]`、Agent 基础类型、回调、Generator 流处理和 panic 隔离等基础设施。

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

### Callback

`Callback` 接口包含 `OnStart/OnEnd/OnError`、`OnLLMStart/OnLLMEnd/OnLLMError`、`OnChainStart/OnChainEnd/OnChainError`、`OnToolStart/OnToolEnd/OnToolError`、`OnAgentAction/OnAgentFinish`。返回非 nil 错误会中止执行。嵌入 `BaseCallback` 只需实现关心的方法。

内置：`NewLoggingCallback(logger Logger, verbose bool)`、`NewMetricsCallback(MetricsCollector)`、`NewTracingCallback(Tracer)`、`NewCostTrackingCallback(pricing)`、`NewStdoutCallback(color)`，以及组合多个回调的 `NewCallbackManager(...)`。

### State

`core/state.State`（`core.State` 为别名）：`Get`、`Set`、`Update`、`Snapshot`、`Clone`、`Delete`、`Clear`、`Keys`、`Size`。`NewAgentState()` 返回线程安全实现，另有 `GetString`/`GetInt`/`GetBool`/`GetFloat64`/`GetMap`/`GetSlice`。`state` 包另提供带 schema 校验的 `TypedState`。

会话持久化与恢复使用 [core/session](session/README.md) 的会话日志与 `core/durable`（builder 的 `WithSessionLog`、`WithDurableScope`）。

## 使用方法

人工介入的跨进程暂停和恢复使用 `workflow.CompileDurable` 的检查点与 `Resume`，见 [HITL 指南](../docs/guides/HUMAN_IN_THE_LOOP.md)。

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

### Generator

`Generator[T]` 即 `iter.Seq2[T, error]`，可直接 `range`，提前 `break` 即停止：

```go
gen := core.GeneratorFunc(func(yield func(int, error) bool) {
    for i := range 100 {
        if !yield(i, nil) {
            return
        }
    }
})

for n, err := range gen {
    if err != nil {
        return err
    }
    fmt.Println(n)
}

even := core.Filter(gen, func(n int) bool { return n%2 == 0 })
results, err := core.Collect(core.Take(even, 10))
```

另有 `Map`、`GeneratorFunc`、`FromChannel`、`ToChannel`。

### 状态

```go
st := core.NewAgentState()
st.Set("user_id", "12345")
snapshot := st.Snapshot()
```

## 中间件与辅助执行契约

- 三种中间件链共用错误恢复规则；`FastMiddlewareChain` 保持不调用 `OnAfter`。`OnBefore` / `OnAfter` 返回 `(nil, err)` 且自己的 `OnError` 抑制错误时，保留进入该阶段的有效对象；处理器错误被抑制时保留处理器提供的部分响应，没有响应则构造空响应并保留请求状态。`OnError` 可通过 `RequestFromContext` 读取当前请求；错误清除后停止错误传播。
- `CacheMiddleware` 在链内保存处理器原始响应，命中后每次各执行一次正常后处理，因此与非幂等转换的顺序无关。写入和读取均创建保留具体类型的快照，支持普通容器及导出结构字段；默认 `AgentState` 通过其状态 map 快照，其他不透明状态和宿主对象须实现 `CloneSnapshot() (any, error)`。无法隔离的响应会跳过缓存。宿主不能在快照期间并发修改输出中的可变值；已归还池的响应不能继续使用。单独调用 `OnAfter` 时，调用方须提供 `original_input` 元数据且负责原始输出边界。
- 默认工具选择器按不区分大小写的完整单词或工具名匹配，`maxTools == 0` 表示不选择，负数表示不限数量。自定义选择器不能引入候选集之外的工具。Builder 消费每次请求的 `selected_tools`，进一步与最后一轮直接返回工具取交集；选择仅控制模型可见工具，宿主授权仍由 Dispatcher 执行。
- `RunnableFunc` 完成回调的错误会作为执行错误返回。
- `CostTrackingCallback` 需要宿主显式发送成对的模型开始/结束事件。每个并发模型调用须使用独立 context，同一 context 的重叠调用会被拒绝；失败调用须发送 `OnLLMError` 清理关联。缺少起始事件或模型单价时只累计 token。Builder 模型用量应使用其 UsageHook；旧回调不会自动接收 Builder 的模型事件。
- `TypedState` 的整数范围为 `int64`，JSON 浮点整数必须有限、无小数且不溢出；数组逐项验证，嵌套对象只接受字符串键，字节与时间分别要求字节切片和 `time.Time`。未知字段类型与必需字段空值会被拒绝。

## 代码结构

```text
core/
├── runnable.go / stream_runnable.go  # Runnable 接口与实现
├── callback.go                       # 回调
├── generator.go                      # Generator[T]
├── panic_handler.go
├── budget/       # 宿主级模型请求预算
├── durable/      # 显式版本化步骤的持久运行与恢复
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
