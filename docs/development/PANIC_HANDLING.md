# Panic 处理开发指南

## 概述

GoAgent 在调用宿主代码的边界恢复 panic，把它变成错误返回，而不是让进程崩溃。有两条路径：

| 路径 | 使用位置 | 结果 |
|------|----------|------|
| 全局 `core.PanicHandlerRegistry` | `core` 中的 Runnable（`RunnableFunc` 组件名 `runnable`，`StreamRunnable` 组件名 `stream_runnable`）、回调通知、生命周期管理器、`core.CallSafely` / `RecoverPanic` / `ReportPanic` 的调用点（链、生成器、中断、`memory`、`tools.ToolExecutor`、`tools/search`、`mcp/toolbox`、`devtools` 等） | 经可替换的 `PanicHandler` 转换，并调用 `PanicMetricsCollector`、`PanicLogger` |
| 直接构造 `*agentErrors.PanicError` | Builder 执行入口与宿主回调（`builder/panic.go` 的 `recoverPanic`）、`tools.Dispatcher` | 不经全局 registry，不触发自定义 handler、指标与日志 |

两条路径的错误链中都能用 `errors.As` 取到 `*agentErrors.PanicError`（字段 `Op`、`Value`、`Stack`）。`PanicError.Error()` 只含位置，不含 panic 值与调用栈：错误文本可能进入日志、事件甚至交给模型。

## 接口

定义见 `core/panic_handler.go`：

```go
type PanicHandler interface {
	HandlePanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string) error
}

type PanicMetricsCollector interface {
	RecordPanic(ctx context.Context, component, operation string, panicValue interface{})
}

type PanicLogger interface {
	LogPanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string, recoveredError error)
}
```

约定：`HandlePanic` 返回的错误文本（包括 `AgentError` 的 Message 与 Context）不应包含 `panicValue` 与 `stackTrace`；registry 不改写 handler 的文本，handler 结果中没有 `*PanicError` 时由 registry 补到错误链上，handler 返回 nil 时直接返回 `*PanicError`。

默认实现：`DefaultPanicHandler` 返回错误码 `CodeInternal`、Message 为 `panic 已恢复` 的 `AgentError`，cause 为 `*PanicError`；`NoOpMetricsCollector`、`NoOpPanicLogger` 不做任何事。

registry 用原子指针保存三个实现，`core.SetGlobalPanicHandler`、`SetGlobalMetricsCollector`、`SetGlobalPanicLogger` 可在运行时替换，`GlobalPanicHandlerRegistry().GetHandler()` 等读取当前实现。

## 使用

### 默认行为

```go
r := core.NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
	var ptr *string
	return *ptr, nil // nil 指针解引用，panic
})

_, err := r.Invoke(context.Background(), "test")
if err != nil {
	fmt.Printf("Error: %v\n", err) // 只含位置，不含 panic 值与调用栈

	var p *agentErrors.PanicError
	if errors.As(err, &p) {
		log.Printf("panic 位置=%s 值=%v\n%s", p.Op, p.Value, p.Stack)
	}
}
```

### Prometheus 指标

```go
type PrometheusMetricsCollector struct {
	panicCounter *prometheus.CounterVec
}

func NewPrometheusMetricsCollector(reg prometheus.Registerer) *PrometheusMetricsCollector {
	return &PrometheusMetricsCollector{
		panicCounter: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{Name: "goagent_panic_total", Help: "Total number of panics recovered"},
			[]string{"component", "operation"},
		),
	}
}

func (c *PrometheusMetricsCollector) RecordPanic(ctx context.Context, component, operation string, panicValue interface{}) {
	c.panicCounter.WithLabelValues(component, operation).Inc()
}

func setupMetrics() {
	core.SetGlobalMetricsCollector(NewPrometheusMetricsCollector(prometheus.NewRegistry()))
}
```

### 结构化日志

```go
type StructuredPanicLogger struct {
	logger *slog.Logger
}

func NewStructuredPanicLogger() *StructuredPanicLogger {
	return &StructuredPanicLogger{logger: slog.New(slog.NewJSONHandler(os.Stderr, nil))}
}

func (l *StructuredPanicLogger) LogPanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string, recoveredError error) {
	l.logger.LogAttrs(ctx, slog.LevelError, "panic recovered",
		slog.String("component", component),
		slog.String("operation", operation),
		slog.Any("panic_value", panicValue),
		slog.String("error", recoveredError.Error()),
		slog.String("stack_trace", stackTrace),
	)
}

func setupLogger() { core.SetGlobalPanicLogger(NewStructuredPanicLogger()) }
```

### 自定义错误转换

```go
type CustomPanicHandler struct {
	serviceName string
}

func (h *CustomPanicHandler) HandlePanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string) error {
	code := agentErrors.CodeInternal
	if component == "runnable" {
		code = agentErrors.CodeAgentExecution
	}
	// Message 与 Context 会出现在 Error() 文本中，不写入 panicValue、stackTrace。
	return agentErrors.New(code, "panic 已恢复").
		WithComponent(component).
		WithOperation(operation).
		WithContext("service", h.serviceName)
}

func setupHandler() { core.SetGlobalPanicHandler(&CustomPanicHandler{serviceName: "my-service"}) }
```

### 在新代码中恢复 panic

- 有错误出口的 goroutine：`defer core.RecoverPanic(ctx, "my_component", "run", &err)`。
- 没有调用方接收错误的 goroutine：`defer core.ReportPanic(ctx, "my_component", "loop")`，只记录指标与日志。
- 单次调用宿主代码：`panicked, err := core.CallSafely(ctx, "my_component", "callback", fn)`。

`RecoverPanic` 与 `ReportPanic` 必须直接 `defer`，`recover` 才生效、调用栈才指向 panic 发生处。

## 实现要求

- 实现必须并发安全：panic 可能在多个 goroutine 同时发生。
- `RecordPanic`、`LogPanic` 应尽快返回，需要远程发送时自行缓冲并在满时丢弃。
- `HandlePanic` 自身不应 panic；需要时在内部 `recover` 并返回不含 panic 值的错误。

## 测试

替换全局 handler 后用 `t.Cleanup` 恢复，避免影响其他测试：

```go
type recordingHandler struct{ components []string }

func (h *recordingHandler) HandlePanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string) error {
	h.components = append(h.components, component)
	return errors.New("mock panic")
}

func TestRunnablePanicUsesHandler(t *testing.T) {
	registry := core.GlobalPanicHandlerRegistry()
	original := registry.GetHandler()
	t.Cleanup(func() { registry.SetHandler(original) })

	h := &recordingHandler{}
	core.SetGlobalPanicHandler(h)

	r := core.NewRunnableFunc(func(context.Context, string) (string, error) { panic("boom") })
	_, err := r.Invoke(context.Background(), "x")

	require.Error(t, err)
	var p *agentErrors.PanicError
	require.ErrorAs(t, err, &p) // registry 在 handler 的错误之外补上 *PanicError
	require.Equal(t, []string{"runnable"}, h.components)
}
```

可能让进程直接退出的场景（如未恢复的 goroutine panic）用 `testing/panictest` 在子进程中运行。参考测试：`core/panic_handler_test.go`、`core/runnable_panic_test.go`、`core/stream_runnable_panic_test.go`、`core/callback_panic_test.go`、`core/goroutine_panic_test.go`。

## 相关文档

- [开发指南](DEVELOPMENT_GUIDE.md)
- [测试最佳实践](TESTING_BEST_PRACTICES.md)
