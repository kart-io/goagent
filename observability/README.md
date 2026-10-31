# observability 可观测性

基于 OpenTelemetry 的追踪与指标，以及基于 Prometheus 客户端的全局指标。包含三组相互独立的能力：

| 能力 | 入口 | Tracer / 注册表 |
|---|---|---|
| Provider 追踪与 OTel 指标 | `TelemetryProvider`、`AgentTracer`、`AgentMetrics` | Provider 自有的 TracerProvider / MeterProvider |
| 包级追踪函数与分布式传播 | `StartAgentSpan` 等函数、`DistributedTracer`、`CrossServiceTracer`、`InstrumentedAgent` | OTel 全局 TracerProvider 与 propagator |
| Prometheus 指标 | `GetMetrics`、`RecordAgentExecution` 等函数 | Prometheus 默认注册表（`promauto`） |

`NewTelemetryProvider` 不会设置 OTel 全局 TracerProvider；需要包级函数的 span 也导出时，由宿主调用 `otel.SetTracerProvider`。

## TelemetryProvider

```go
cfg := observability.DefaultTelemetryConfig()
cfg.ServiceName = "my-agent-service"
cfg.TraceEndpoint = "localhost:4317"

provider, err := observability.NewTelemetryProvider(cfg) // cfg 为 nil 时使用默认配置
if err != nil {
    return err
}
defer provider.Shutdown(context.Background())

tracer := observability.NewAgentTracer(provider, "my-agent")
metrics, err := observability.NewAgentMetrics(provider, "my-agent")
```

`DefaultTelemetryConfig()` 的主要取值：

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `ServiceName` / `ServiceVersion` / `Environment` | `agent-service` / `1.0.0` / `development` | 写入 resource |
| `TraceEnabled` | true | |
| `TraceExporter` | `otlp` | `otlp` 为 OTLP gRPC 导出；`stdout` 与 `noop` 都不导出 |
| `TraceEndpoint` | `localhost:4317` | |
| `TraceSampleRate` | 1.0 | |
| `MetricsEnabled` / `MetricsInterval` | true / 60s | |
| `MetricsExporter` | `prometheus` | 当前不影响行为：MeterProvider 使用 ManualReader，OTel 指标不会被自动导出 |

方法：`GetTracer(name)`、`GetMeter(name)`、`ForceFlush(ctx)`、`Shutdown(ctx)`。

## AgentTracer

```go
ctx, span := tracer.StartAgentSpan(ctx, "recommendation-engine", attribute.String("user_id", "123"))
defer span.End()

tracer.AddEvent(ctx, "search_started", attribute.String("algorithm", "cf"))
tracer.SetAttributes(ctx, attribute.Int("items_returned", 10))
if err != nil {
    tracer.RecordError(ctx, err)
}

// 在 span 中执行，fn 的错误自动记入 span
err = tracer.WithSpanContext(ctx, "find_recommendations", func(ctx context.Context) error {
    return findRecommendations(ctx)
})
```

领域 span：`StartAgentSpan`、`StartToolSpan`、`StartLLMSpan`、`StartMemorySpan`、`StartChainSpan`，通用的 `StartSpan`；另有 `SetStatus`、`SpanFromContext`、`ExecutionContext`、`GetTracer`。

## AgentMetrics（OTel）

```go
metrics.RecordRequest(ctx, 0.25, true, attribute.String("agent.name", "analyst"))
metrics.RecordLLMCall(ctx, "gpt-4", "openai", 500, 2.5, true)
metrics.RecordMemoryOperation(ctx, "store", "vector", 1024, 0.05, true)
metrics.IncrementActiveAgents(ctx, 1)
```

另有 `RecordAgentExecution`、`RecordToolCall`、`RecordChainExecution`、`RecordError`。

## 包级追踪与分布式传播

```go
ctx, span := observability.StartAgentSpan(ctx, "search-agent")
defer span.End()
observability.AddEvent(span, "cache_miss")
observability.RecordError(span, err)

// 跨服务：HTTP 头与消息元数据
cst := observability.NewCrossServiceTracer("api-gateway")
ctx, span = cst.TraceHTTPRequest(ctx, req) // 把追踪上下文注入 req.Header
resp, err := http.DefaultClient.Do(req)
if err == nil {
    _ = cst.TraceHTTPResponse(ctx, resp)
}

ctx, msgSpan, carrier := cst.TraceMessage(ctx, "orders.created", payload)
headers := carrier.GetMetadata() // 随消息发送
```

其余函数：`StartToolSpan`、`StartRemoteAgentSpan`、`AddAttributes`。`DistributedTracer`（`NewDistributedTracer()`）提供 `InjectContext`、`ExtractContext`、`StartRemoteSpan`，载体可用 `NewHTTPCarrier(http.Header)` 或 `NewMessageCarrier(map[string]string)`。

属性构造：`AgentAttributes`、`ToolAttributes`、`LLMAttributes`、`MemoryAttributes`、`ErrorAttributes`。

## InstrumentedAgent

```go
wrapped := observability.NewInstrumentedAgent(agent, "search-service", logger) // 或 WrapAgent
output, err := wrapped.Invoke(ctx, input)
```

`agent` 为 `core.Agent`，`logger` 为 `github.com/kart-io/logger/core.Logger`。`Invoke` 增减并发计数、以包级 `StartAgentSpan` 创建 span、写开始与结束日志，并记录 Prometheus 的 Agent 执行、错误与输出中的工具调用指标。

## Prometheus 指标

```go
observability.RecordAgentExecution("search-agent", "search-service", "success", time.Since(start))
observability.RecordToolCall("web-search", "search-agent", "success", duration)
observability.UpdateServiceInstances("payment-service", 5, 5)

observability.IncrementConcurrentExecutions()
defer observability.DecrementConcurrentExecutions()
```

指标在首次 `GetMetrics()` 时注册到 Prometheus 默认注册表，由宿主通过 `promhttp` 暴露：`agent_executions_total`、`agent_execution_duration_seconds`、`agent_errors_total`、`tool_calls_total`、`tool_call_duration_seconds`、`tool_errors_total`、`remote_agent_calls_total`、`remote_agent_call_duration_seconds`、`remote_agent_errors_total`、`service_instances_total`、`healthy_instances_total`、`concurrent_executions`。其余函数：`RecordAgentError`、`RecordToolError`、`RecordRemoteAgentCall`、`RecordRemoteAgentError`。

## 代码结构

```text
observability/
├── telemetry.go            # TelemetryProvider
├── tracer.go               # AgentTracer
├── agent_metrics.go        # AgentMetrics（OTel）
├── tracing.go              # 包级追踪函数
├── tracing_distributed.go  # DistributedTracer、CrossServiceTracer、载体
├── metrics.go              # Prometheus 指标
└── logging.go              # InstrumentedAgent
```

## 扩展阅读

- [middleware](../middleware/README.md) - 基于 TelemetryProvider 的 ObservabilityMiddleware
- [core](../core/README.md) - Agent 接口
