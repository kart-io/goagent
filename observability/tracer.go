package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// AgentTracer Agent 追踪器
type AgentTracer struct {
	tracer trace.Tracer
}

// NewAgentTracer 创建 Agent 追踪器
func NewAgentTracer(provider *TelemetryProvider, name string) *AgentTracer {
	return &AgentTracer{
		tracer: provider.GetTracer(name),
	}
}

// StartSpan 开始 Span
func (t *AgentTracer) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, name, opts...)
}

// SpanFromContext 从上下文获取 Span
func (t *AgentTracer) SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// AddEvent 添加事件
func (t *AgentTracer) AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes 设置属性
func (t *AgentTracer) SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attrs...)
}

// RecordError 记录错误
func (t *AgentTracer) RecordError(ctx context.Context, err error, opts ...trace.EventOption) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err, opts...)
	span.SetStatus(codes.Error, err.Error())
}

// SetStatus 设置状态
func (t *AgentTracer) SetStatus(ctx context.Context, code codes.Code, description string) {
	span := trace.SpanFromContext(ctx)
	span.SetStatus(code, description)
}

// StartAgentSpan 启动 Agent 执行 span
func (t *AgentTracer) StartAgentSpan(ctx context.Context, agentName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs = append(attrs, attribute.String("agent.name", agentName))
	return t.StartSpan(ctx, "agent.execute", trace.WithAttributes(attrs...))
}

// StartToolSpan 启动工具调用 span
func (t *AgentTracer) StartToolSpan(ctx context.Context, toolName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs = append(attrs, attribute.String("tool.name", toolName))
	return t.StartSpan(ctx, "tool.call", trace.WithAttributes(attrs...))
}

// StartLLMSpan 启动 LLM 调用 span
func (t *AgentTracer) StartLLMSpan(ctx context.Context, model string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs = append(attrs, attribute.String("llm.model", model))
	return t.StartSpan(ctx, "llm.call", trace.WithAttributes(attrs...))
}

// StartMemorySpan 启动内存操作 span
func (t *AgentTracer) StartMemorySpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs = append(attrs, attribute.String("memory.operation", operation))
	return t.StartSpan(ctx, "memory.access", trace.WithAttributes(attrs...))
}

// StartChainSpan 启动链执行 span
func (t *AgentTracer) StartChainSpan(ctx context.Context, chainName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs = append(attrs, attribute.String("chain.name", chainName))
	return t.StartSpan(ctx, "chain.execute", trace.WithAttributes(attrs...))
}

// WithSpanContext 在新 span 中执行函数
func (t *AgentTracer) WithSpanContext(ctx context.Context, spanName string, fn func(context.Context) error, attrs ...attribute.KeyValue) error {
	ctx, span := t.StartSpan(ctx, spanName, trace.WithAttributes(attrs...))
	defer span.End()

	err := fn(ctx)
	if err != nil {
		t.RecordError(ctx, err)
	}

	return err
}

// GetTracer 获取底层 tracer
func (t *AgentTracer) GetTracer() trace.Tracer {
	return t.tracer
}

// Helper functions for common trace attributes

// AgentAttributes 创建 Agent 属性
func AgentAttributes(agentName, agentType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("agent.name", agentName),
		attribute.String("agent.type", agentType),
	}
}

// ToolAttributes 创建工具属性
func ToolAttributes(toolName, toolType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("tool.name", toolName),
		attribute.String("tool.type", toolType),
	}
}

// LLMAttributes 创建 LLM 属性
func LLMAttributes(model, provider string, tokens int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("llm.model", model),
		attribute.String("llm.provider", provider),
		attribute.Int("llm.tokens", tokens),
	}
}

// MemoryAttributes 创建内存属性
func MemoryAttributes(operation, memoryType string, size int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("memory.operation", operation),
		attribute.String("memory.type", memoryType),
		attribute.Int("memory.size", size),
	}
}

// ErrorAttributes 创建错误属性
func ErrorAttributes(errorType, errorMessage string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("error.type", errorType),
		attribute.String("error.message", errorMessage),
	}
}
