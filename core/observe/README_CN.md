# observe 执行事件

[English](README.md) | [中文](README_CN.md)

`observe` 提供可选的执行事件，并通过 context 传播已有的 OpenTelemetry tracer。Builder、Dispatcher 等生产方把生命周期事件（Agent、模型调用、重试、工具调用、缓存、用量）发送给 context 上绑定的 `Observer`。事件只是观察，绝不是业务检查点或记账证据；可靠的 Token 累计应查询 `core/budget` 账本。需要把事件接入日志、指标或链路追踪，或从自己的工作流代码中发出事件时使用本包。

## 用法

```go
type sink struct{}

func (sink) Observe(ctx context.Context, e observe.Event) {
    if e.Type == "model_usage" && e.Usage != nil {
        _ = e.Usage.Tokens // 投影到指标系统；不作为账单记录
    }
}

func run(ctx context.Context, logger logging.Logger) error {
    async, err := observe.NewAsyncObserver(sink{}, 1024, 1)
    if err != nil {
        return err
    }
    defer func() {
        closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        _ = async.Close(closeCtx)
    }()
    ctx = observe.WithObserver(ctx, async)
    ctx = observe.AddObserver(ctx, observe.NewLogObserver(logger)) // 两者都接收全部事件
    ctx = observe.WithRunID(ctx, "business-run")

    stepCtx, end := observe.Start(ctx, observe.Event{Type: "step", Path: "step"})
    err = doStep(stepCtx)
    end(err, false) // 恰好调用一次；true 表示回放
    return err
}
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Observer` | `Observe(context.Context, Event)`；必须支持并发调用并及时返回 |
| `Event` | 身份（`RunID`、`OperationKey`、`VisitID`、`EventID`、trace/span ID）、`Type`、`Path`、`ErrorKind`、时间、`Replayed`、`Cached`，以及可选的 `Agent` 与 `Usage` |
| `WithObserver(ctx, o)` / `AddObserver(ctx, o)` | 绑定一个 sink / 在已有 sink 之外再绑定一个 |
| `NewAsyncObserver(sink, capacity, workers)` | 有界队列适配器，不阻塞生产方 |
| `NewLogObserver(l)` | 把每个事件作为一条结构化记录写入 `logging.Logger` |
| `WithTracer(ctx, t)` / `TracerFromContext(ctx)` | 注入已有 tracer 而不设置全局 Provider / 读取它（默认 noop） |
| `WithRunID(ctx, id)` / `RunIDFromContext(ctx)` | 绑定 / 读取业务运行身份 |
| `Start(ctx, e)` | 开始一个生命周期及其 span；返回子 context 与 `end(err, replayed)` 函数 |
| `Emit(ctx, e)` | 发送不创建 span 的独立事件 |
| `Enabled(ctx)`、`MarkCached(ctx)`、`ErrorKind(err)` | 是否配置了 sink 或 tracer；把当前生命周期标为命中缓存；有界错误类别 |

## 行为说明

- `Event` 有意不包含原始输入、输出和 Provider 错误消息。`ErrorKind` 只返回类别（`panic`、`canceled`、`deadline`、`waiting`、`attention`、`execution`）；panic 值与调用栈不会进入事件或 span。
- `Start` 发出 `<Type>_start`；`end` 发出 `<Type>_end`、`<Type>_replayed`、`<Type>_error` 或 `<Type>_waiting`。`end` 必须恰好调用一次。
- 既无 sink 也无 tracer 时，`Start` 与 `Emit` 不做任何事，`Emit` 不分配内存。投递是尽力而为的。
- `WithObserver` 隔离 sink 的 panic，但不防护阻塞的 sink。`NewAsyncObserver` 要求 `capacity` 与 `workers` 为正数；多于一个 worker 时 sink 必须支持并发调用。`Dropped()` 统计因队列溢出、`Close` 之后到达、以及 `Close` 的 context 取消时丢弃的待投递事件。
- 入队的异步事件保留 context 中的值，但不继承调用方的取消信号。`Close` 等待已入队事件投递完毕；sink 不得在自身适配器上调用 `Close`。
- `NewLogObserver` 的级别：执行失败（`execution` 或 `panic`）为 Error；其他失败、取消和模型重试为 Warn；结束、回放和等待的生命周期为 Info；开始事件与独立事件为 Debug。logger 为 nil 时写入 `logging.Default()`。

## 另见

- [可组合编排：观察与报告](../../docs/guides/COMPOSABLE_ORCHESTRATION_CN.md#观察与报告)
- [根预算、资源治理与运行诊断](../../docs/guides/EXECUTION_GOVERNANCE_CN.md#异步观测与授权回收)
- [宿主集成：日志](../../docs/guides/HOST_INTEGRATION_CN.md#日志)
- [多 Agent 钩子与 Token 统计](../../docs/guides/MULTI_AGENT_USAGE_CN.md)
