package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// AgentMetrics Agent 指标
type AgentMetrics struct {
	meter metric.Meter

	// Counters
	requestsTotal  metric.Int64Counter
	errorsTotal    metric.Int64Counter
	toolCallsTotal metric.Int64Counter

	// Histograms
	requestDuration metric.Float64Histogram
	toolDuration    metric.Float64Histogram

	// Gauges
	activeAgents metric.Int64UpDownCounter
}

// NewAgentMetrics 创建指标
func NewAgentMetrics(provider *TelemetryProvider, name string) (*AgentMetrics, error) {
	meter := provider.GetMeter(name)

	m := &AgentMetrics{
		meter: meter,
	}

	var err error

	// 创建 Counters
	m.requestsTotal, err = meter.Int64Counter(
		"agent.requests.total",
		metric.WithDescription("Total number of agent requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	m.errorsTotal, err = meter.Int64Counter(
		"agent.errors.total",
		metric.WithDescription("Total number of agent errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, err
	}

	m.toolCallsTotal, err = meter.Int64Counter(
		"agent.tool_calls.total",
		metric.WithDescription("Total number of tool calls"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		return nil, err
	}

	// 创建 Histograms
	m.requestDuration, err = meter.Float64Histogram(
		"agent.request.duration",
		metric.WithDescription("Agent request duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	m.toolDuration, err = meter.Float64Histogram(
		"agent.tool.duration",
		metric.WithDescription("Tool call duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	// 创建 Gauges
	m.activeAgents, err = meter.Int64UpDownCounter(
		"agent.active.count",
		metric.WithDescription("Number of active agents"),
		metric.WithUnit("{agent}"),
	)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// RecordRequest 记录请求
func (m *AgentMetrics) RecordRequest(ctx context.Context, durationSeconds float64, success bool, attrs ...attribute.KeyValue) {
	// 添加 success 属性
	attrs = append(attrs, attribute.Bool("success", success))

	// 记录计数
	m.requestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))

	// 记录时长
	m.requestDuration.Record(ctx, durationSeconds, metric.WithAttributes(attrs...))
}

// RecordError 记录错误
func (m *AgentMetrics) RecordError(ctx context.Context, errorType string, attrs ...attribute.KeyValue) {
	attrs = append(attrs, attribute.String("error.type", errorType))
	m.errorsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordToolCall 记录工具调用
func (m *AgentMetrics) RecordToolCall(ctx context.Context, toolName string, durationSeconds float64, success bool) {
	attrs := []attribute.KeyValue{
		attribute.String("tool.name", toolName),
		attribute.Bool("success", success),
	}

	// 记录计数
	m.toolCallsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))

	// 记录时长
	m.toolDuration.Record(ctx, durationSeconds, metric.WithAttributes(attrs...))
}

// IncrementActiveAgents 增加活跃 Agent
func (m *AgentMetrics) IncrementActiveAgents(ctx context.Context, delta int64) {
	m.activeAgents.Add(ctx, delta)
}

// RecordAgentExecution 记录 Agent 执行 (便利方法)
func (m *AgentMetrics) RecordAgentExecution(ctx context.Context, agentName, agentType string, durationSeconds float64, success bool) {
	attrs := []attribute.KeyValue{
		attribute.String("agent.name", agentName),
		attribute.String("agent.type", agentType),
	}
	m.RecordRequest(ctx, durationSeconds, success, attrs...)
}

// RecordLLMCall 记录 LLM 调用
func (m *AgentMetrics) RecordLLMCall(ctx context.Context, model, provider string, tokens int, durationSeconds float64, success bool) {
	attrs := []attribute.KeyValue{
		attribute.String("llm.model", model),
		attribute.String("llm.provider", provider),
		attribute.Int("llm.tokens", tokens),
		attribute.Bool("success", success),
	}
	m.RecordRequest(ctx, durationSeconds, success, attrs...)
}

// RecordMemoryOperation 记录内存操作
func (m *AgentMetrics) RecordMemoryOperation(ctx context.Context, operation, memoryType string, size int, durationSeconds float64, success bool) {
	attrs := []attribute.KeyValue{
		attribute.String("memory.operation", operation),
		attribute.String("memory.type", memoryType),
		attribute.Int("memory.size", size),
	}
	m.RecordRequest(ctx, durationSeconds, success, attrs...)
}

// RecordChainExecution 记录链执行
func (m *AgentMetrics) RecordChainExecution(ctx context.Context, chainName string, steps int, durationSeconds float64, success bool) {
	attrs := []attribute.KeyValue{
		attribute.String("chain.name", chainName),
		attribute.Int("chain.steps", steps),
	}
	m.RecordRequest(ctx, durationSeconds, success, attrs...)
}
