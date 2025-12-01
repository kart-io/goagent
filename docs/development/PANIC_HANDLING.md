# Panic 处理开发指南

## 概述

GoAgent 使用基于接口的 panic 处理系统，支持：

- ✅ **完全可定制** - 自定义错误转换、指标收集、日志记录
- ✅ **热插拔** - 运行时无缝切换实现
- ✅ **线程安全** - 原子操作保证并发安全
- ✅ **零侵入** - 现有代码无需修改

## 核心接口

### PanicHandler - 错误转换

处理 panic 恢复并转换为错误：

```go
type PanicHandler interface {
    HandlePanic(
        ctx context.Context,
        component string,      // 发生 panic 的组件（如 "runnable", "lifecycle_manager"）
        operation string,      // 正在执行的操作（如 "invoke", "init", "start"）
        panicValue interface{}, // panic 的值
        stackTrace string,     // 完整堆栈追踪
    ) error
}
```

**使用场景**：
- 转换为领域特定的错误类型
- 添加业务上下文信息
- 实现重试逻辑
- 优雅降级策略

### PanicMetricsCollector - 指标收集

收集 panic 统计信息用于监控：

```go
type PanicMetricsCollector interface {
    RecordPanic(
        ctx context.Context,
        component string,
        operation string,
        panicValue interface{},
    )
}
```

**使用场景**：
- Prometheus counters/histograms
- StatsD metrics
- DataDog APM
- 自定义遥测系统

### PanicLogger - 日志记录

记录 panic 事件到日志系统：

```go
type PanicLogger interface {
    LogPanic(
        ctx context.Context,
        component string,
        operation string,
        panicValue interface{},
        stackTrace string,
        recoveredError error,  // HandlePanic 返回的错误
    )
}
```

**使用场景**：
- 结构化日志（JSON、logfmt）
- 不同级别的日志记录
- 集中式日志聚合
- 告警触发

## 默认实现

系统提供开箱即用的默认实现，与原有行为完全兼容：

```go
// DefaultPanicHandler - 转换 panic 为 AgentError
&DefaultPanicHandler{}

// NoOpMetricsCollector - 不执行任何操作（占位符）
&NoOpMetricsCollector{}

// NoOpPanicLogger - 不执行任何操作（占位符）
&NoOpPanicLogger{}
```

**默认行为**：
- 将所有 panic 转换为 `AgentError`，错误码为 `CodeInternal`
- 保留完整的堆栈追踪和 panic 值
- 不记录指标
- 不输出日志

## 使用方式

### 基础使用

使用默认实现，无需任何配置：

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/core"
)

func main() {
    // 使用默认实现，无需配置
    agent := core.NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
        // 即使这里 panic，系统也会自动恢复
        var ptr *string
        return *ptr, nil  // panic: nil pointer dereference
    })

    result, err := agent.Invoke(context.Background(), "test")
    // err 是 AgentError，包含完整的堆栈信息
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    }
}
```

### 自定义实现

#### 1. Prometheus 指标集成

```go
package monitoring

import (
    "context"
    "fmt"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "github.com/kart-io/goagent/core"
)

type PrometheusMetricsCollector struct {
    panicCounter *prometheus.CounterVec
}

func NewPrometheusMetricsCollector(reg prometheus.Registerer) *PrometheusMetricsCollector {
    return &PrometheusMetricsCollector{
        panicCounter: promauto.With(reg).NewCounterVec(
            prometheus.CounterOpts{
                Name: "goagent_panic_total",
                Help: "Total number of panics recovered",
            },
            []string{"component", "operation"},
        ),
    }
}

func (c *PrometheusMetricsCollector) RecordPanic(
    ctx context.Context,
    component, operation string,
    panicValue interface{},
) {
    c.panicCounter.WithLabelValues(component, operation).Inc()
}

// 在 main() 中注册
func main() {
    registry := prometheus.NewRegistry()
    collector := NewPrometheusMetricsCollector(registry)

    core.SetGlobalMetricsCollector(collector)

    // 现在所有 panic 都会记录到 Prometheus
}
```

#### 2. 结构化日志

```go
package logging

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "github.com/kart-io/goagent/core"
)

type StructuredPanicLogger struct {
    logger *slog.Logger
}

func NewStructuredPanicLogger() *StructuredPanicLogger {
    return &StructuredPanicLogger{
        logger: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
            Level: slog.LevelError,
        })),
    }
}

func (l *StructuredPanicLogger) LogPanic(
    ctx context.Context,
    component, operation string,
    panicValue interface{},
    stackTrace string,
    recoveredError error,
) {
    l.logger.LogAttrs(
        ctx,
        slog.LevelError,
        "Panic recovered",
        slog.String("component", component),
        slog.String("operation", operation),
        slog.Any("panic_value", panicValue),
        slog.String("error", recoveredError.Error()),
        slog.String("stack_trace", stackTrace),
    )
}

// 在 main() 中注册
func main() {
    logger := NewStructuredPanicLogger()
    core.SetGlobalPanicLogger(logger)

    // 所有 panic 现在以 JSON 格式记录
}
```

#### 3. 自定义错误转换

```go
package errors

import (
    "context"
    "fmt"
    agentErrors "github.com/kart-io/goagent/errors"
    "github.com/kart-io/goagent/core"
)

type CustomPanicHandler struct {
    serviceName string
}

func NewCustomPanicHandler(serviceName string) *CustomPanicHandler {
    return &CustomPanicHandler{serviceName: serviceName}
}

func (h *CustomPanicHandler) HandlePanic(
    ctx context.Context,
    component, operation string,
    panicValue interface{},
    stackTrace string,
) error {
    // 自定义错误码选择
    errorCode := h.selectErrorCode(component)

    // 提取业务上下文
    userID := ctx.Value("user_id")
    requestID := ctx.Value("request_id")

    return agentErrors.New(
        errorCode,
        fmt.Sprintf("[%s] %s.%s panic: %v", h.serviceName, component, operation, panicValue),
    ).
        WithComponent(component).
        WithOperation(operation).
        WithContext("service", h.serviceName).
        WithContext("user_id", userID).
        WithContext("request_id", requestID).
        WithContext("panic_value", panicValue).
        WithContext("stack_trace", stackTrace)
}

func (h *CustomPanicHandler) selectErrorCode(component string) agentErrors.ErrorCode {
    switch component {
    case "lifecycle_manager":
        return agentErrors.CodeAgentInitialization
    case "runnable":
        return agentErrors.CodeAgentExecution
    default:
        return agentErrors.CodeInternal
    }
}

// 在 main() 中注册
func main() {
    handler := NewCustomPanicHandler("my-service")
    core.SetGlobalPanicHandler(handler)

    // panic 错误现在包含业务上下文
}
```

### 运行时热插拔

最强大的特性：无需重启即可切换实现。

```go
package main

import (
    "time"
    "github.com/kart-io/goagent/core"
)

func main() {
    // 启动时使用默认实现
    go runApplication()

    // 5 分钟后切换到 Prometheus
    time.Sleep(5 * time.Minute)
    core.SetGlobalMetricsCollector(NewPrometheusCollector(registry))

    // 10 分钟后切换到结构化日志
    time.Sleep(5 * time.Minute)
    core.SetGlobalPanicLogger(NewStructuredLogger())

    // 应用持续运行，无需重启
}
```

## 开发最佳实践

### 1. 实现接口时的注意事项

#### 线程安全

所有接口实现必须是线程安全的：

```go
// ✅ 正确：使用锁保护
type SafeCollector struct {
    mu     sync.Mutex
    counts map[string]int64
}

func (c *SafeCollector) RecordPanic(ctx context.Context, component, operation string, panicValue interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.counts[component]++
}

// ✅ 正确：使用原子操作
type AtomicCollector struct {
    count atomic.Int64
}

func (c *AtomicCollector) RecordPanic(ctx context.Context, component, operation string, panicValue interface{}) {
    c.count.Add(1)
}
```

#### 避免阻塞

RecordPanic 和 LogPanic 应该尽快返回：

```go
// ✅ 正确：异步处理
type AsyncLogger struct {
    logChan chan LogEntry
}

func (l *AsyncLogger) LogPanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string, err error) {
    entry := LogEntry{Component: component, Operation: operation}

    select {
    case l.logChan <- entry:
    case <-ctx.Done():
        // 超时，丢弃
    default:
        // Channel 满，丢弃
    }
}

// ❌ 错误：阻塞调用
func (l *BadLogger) LogPanic(...) {
    l.remoteAPI.SendLog(...)  // 可能阻塞很久
}
```

#### 错误处理

HandlePanic 不应该再次 panic：

```go
// ✅ 正确：捕获所有错误
func (h *SafeHandler) HandlePanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string) error {
    defer func() {
        if r := recover(); r != nil {
            // Handler 自身 panic，返回基本错误
            return fmt.Errorf("handler panic: %v", r)
        }
    }()

    // 处理逻辑
    return h.process(panicValue)
}

// ❌ 错误：可能再次 panic
func (h *BadHandler) HandlePanic(...) error {
    return h.riskyOperation()  // 可能 panic
}
```

### 2. 组合多个实现

使用装饰器模式组合多个实现：

```go
// CompositeMetricsCollector 组合多个 collector
type CompositeMetricsCollector struct {
    collectors []core.PanicMetricsCollector
}

func NewCompositeMetricsCollector(collectors ...core.PanicMetricsCollector) *CompositeMetricsCollector {
    return &CompositeMetricsCollector{collectors: collectors}
}

func (c *CompositeMetricsCollector) RecordPanic(ctx context.Context, component, operation string, panicValue interface{}) {
    for _, collector := range c.collectors {
        collector.RecordPanic(ctx, component, operation, panicValue)
    }
}

// 使用
composite := NewCompositeMetricsCollector(
    NewPrometheusCollector(registry),
    NewStatsDCollector("localhost:8125"),
    NewDataDogCollector(apiKey),
)
core.SetGlobalMetricsCollector(composite)
```

### 3. 环境区分

根据不同环境使用不同配置：

```go
func setupPanicHandling() {
    env := os.Getenv("ENVIRONMENT")

    switch env {
    case "production":
        // 生产：Prometheus + 告警
        core.SetGlobalMetricsCollector(NewPrometheusCollector(registry))
        core.SetGlobalPanicLogger(NewAlertingLogger(pagerDuty))

    case "staging":
        // 预发：Prometheus + 结构化日志
        core.SetGlobalMetricsCollector(NewPrometheusCollector(registry))
        core.SetGlobalPanicLogger(NewStructuredLogger())

    case "development":
        // 开发：使用默认（或添加调试日志）
        core.SetGlobalPanicLogger(NewDebugLogger())

    default:
        // 测试：使用默认 NoOp
    }
}

func main() {
    setupPanicHandling()
    runApplication()
}
```

## 测试指南

### 单元测试

使用 Mock 实现进行测试：

```go
package mypackage_test

import (
    "context"
    "testing"
    "github.com/kart-io/goagent/core"
    "github.com/stretchr/testify/assert"
)

// MockPanicHandler 用于测试
type MockPanicHandler struct {
    calls []PanicCall
}

type PanicCall struct {
    Component  string
    Operation  string
    PanicValue interface{}
}

func (m *MockPanicHandler) HandlePanic(
    ctx context.Context,
    component, operation string,
    panicValue interface{},
    stackTrace string,
) error {
    m.calls = append(m.calls, PanicCall{
        Component:  component,
        Operation:  operation,
        PanicValue: panicValue,
    })
    return fmt.Errorf("mock panic: %v", panicValue)
}

func TestMyComponent_PanicHandling(t *testing.T) {
    // 保存原始 handler
    originalHandler := core.GlobalPanicHandlerRegistry().GetHandler()
    defer core.GlobalPanicHandlerRegistry().SetHandler(originalHandler)

    // 使用 mock handler
    mockHandler := &MockPanicHandler{}
    core.SetGlobalPanicHandler(mockHandler)

    // 执行会 panic 的代码
    component := NewMyComponent()
    _, err := component.Execute(context.Background())

    // 验证
    assert.Error(t, err)
    assert.Len(t, mockHandler.calls, 1)
    assert.Equal(t, "my_component", mockHandler.calls[0].Component)
}
```

### 集成测试

测试自定义实现：

```go
func TestPrometheusCollector_Integration(t *testing.T) {
    registry := prometheus.NewRegistry()
    collector := NewPrometheusMetricsCollector(registry)

    // 记录几个 panic
    ctx := context.Background()
    collector.RecordPanic(ctx, "comp1", "op1", "panic1")
    collector.RecordPanic(ctx, "comp1", "op1", "panic2")
    collector.RecordPanic(ctx, "comp2", "op2", "panic3")

    // 验证 Prometheus 指标
    metrics, err := registry.Gather()
    require.NoError(t, err)

    // 查找我们的 counter
    var panicTotal *dto.MetricFamily
    for _, mf := range metrics {
        if mf.GetName() == "goagent_panic_total" {
            panicTotal = mf
            break
        }
    }

    require.NotNil(t, panicTotal)
    assert.Equal(t, 3, int(getTotalCount(panicTotal)))
}
```

## 性能考虑

### 基准测试结果

```
无 panic 时（最常见路径）：
BenchmarkPanicHandlerRegistry_Read-8     1000000000    0.5 ns/op

热插拔写入（罕见操作）：
BenchmarkPanicHandlerRegistry_Write-8      50000000   30 ns/op

HandlePanic 调用（panic 发生时）：
BenchmarkPanicHandlerRegistry_HandlePanic-8  5000000  250 ns/op
```

### 优化建议

1. **轻量级 RecordPanic**
   ```go
   // ✅ 快速返回
   func (c *Collector) RecordPanic(...) {
       c.counter.Add(1)  // 原子操作，极快
   }

   // ❌ 避免阻塞
   func (c *BadCollector) RecordPanic(...) {
       c.sendToRemoteServer(...)  // 可能很慢
   }
   ```

2. **异步日志**
   ```go
   // ✅ 使用 buffered channel
   type AsyncLogger struct {
       logChan chan LogEntry  // buffered
   }

   func (l *AsyncLogger) LogPanic(...) {
       select {
       case l.logChan <- entry:
       default:
           // Channel 满，丢弃（不阻塞）
       }
   }
   ```

3. **批量处理**
   ```go
   // ✅ 批量发送指标
   type BatchCollector struct {
       events []Event
       mu     sync.Mutex
   }

   func (c *BatchCollector) RecordPanic(...) {
       c.mu.Lock()
       c.events = append(c.events, event)
       if len(c.events) >= batchSize {
           go c.flush()
           c.events = nil
       }
       c.mu.Unlock()
   }
   ```

## 常见问题

### Q1: 如何查看当前使用的实现？

```go
handler := core.GlobalPanicHandlerRegistry().GetHandler()
fmt.Printf("Current handler: %T\n", handler)

collector := core.GlobalPanicHandlerRegistry().GetMetricsCollector()
fmt.Printf("Current collector: %T\n", collector)

logger := core.GlobalPanicHandlerRegistry().GetLogger()
fmt.Printf("Current logger: %T\n", logger)
```

### Q2: 可以在运行时多次切换吗？

可以！这就是热插拔的意义：

```go
// 可以随时切换，立即生效
core.SetGlobalPanicHandler(handler1)
// ... 运行一段时间 ...
core.SetGlobalPanicHandler(handler2)
// ... 再运行一段时间 ...
core.SetGlobalPanicHandler(handler3)
```

### Q3: 会影响性能吗？

几乎不会。无 panic 时额外开销 < 1ns。

### Q4: 如何调试自定义实现？

使用包装器添加日志：

```go
type DebugHandler struct {
    inner core.PanicHandler
}

func (h *DebugHandler) HandlePanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string) error {
    fmt.Printf("DEBUG: HandlePanic: component=%s, op=%s, panic=%v\n",
        component, operation, panicValue)

    err := h.inner.HandlePanic(ctx, component, operation, panicValue, stackTrace)

    fmt.Printf("DEBUG: Returned error: %v\n", err)
    return err
}

core.SetGlobalPanicHandler(&DebugHandler{inner: NewMyHandler()})
```

### Q5: 接口实现需要处理 context 取消吗？

建议处理，尤其是在可能阻塞的操作中：

```go
func (l *Logger) LogPanic(ctx context.Context, ...) {
    select {
    case l.logChan <- entry:
    case <-ctx.Done():
        // Context 已取消，放弃记录
        return
    }
}
```

## 相关文档

- **接口定义**: `core/panic_handler.go`
- **默认实现**: `core/panic_handler.go:DefaultPanicHandler`
- **测试示例**: `core/panic_handler_test.go`

## 总结

接口化的 panic 处理系统提供了：

✅ **灵活性** - 完全可定制的行为
✅ **可扩展性** - 通过接口添加新功能
✅ **热插拔** - 运行时无缝切换
✅ **高性能** - < 1ns 额外开销
✅ **线程安全** - 原子操作保证
✅ **易测试** - 易于 mock 和验证

这使得 GoAgent 能够适应从简单的开发环境到复杂的企业级生产环境的各种需求。