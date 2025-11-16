package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("agent")

// StartAgentSpan 启动 Agent span
func StartAgentSpan(ctx context.Context, agentName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs = append(attrs, attribute.String("agent.name", agentName))
	return tracer.Start(ctx, "agent.execute", trace.WithAttributes(attrs...))
}

// StartToolSpan 启动工具 span
func StartToolSpan(ctx context.Context, toolName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs = append(attrs, attribute.String("tool.name", toolName))
	return tracer.Start(ctx, "tool.call", trace.WithAttributes(attrs...))
}

// StartRemoteAgentSpan 启动远程 Agent span
func StartRemoteAgentSpan(ctx context.Context, service, agentName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs = append(attrs,
		attribute.String("service.name", service),
		attribute.String("agent.name", agentName),
	)
	return tracer.Start(ctx, "remote_agent.call", trace.WithAttributes(attrs...))
}

// RecordError 记录错误到 span
func RecordError(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	}
}

// AddAttributes 添加属性到 span
func AddAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	span.SetAttributes(attrs...)
}

// AddEvent 添加事件到 span
func AddEvent(span trace.Span, name string, attrs ...attribute.KeyValue) {
	span.AddEvent(name, trace.WithAttributes(attrs...))
}
