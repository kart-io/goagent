package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func TestNewAgentTracer(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	assert.NotNil(t, tracer)
	assert.NotNil(t, tracer.tracer)
}

func TestAgentTracer_StartSpan(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	// 启动 span
	ctx, span := tracer.StartSpan(ctx, "test-operation")
	assert.NotNil(t, span)
	assert.NotNil(t, ctx)

	// 结束 span
	span.End()
}

func TestAgentTracer_SpanFromContext(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	// 启动 span
	ctx, span1 := tracer.StartSpan(ctx, "test-operation")
	defer span1.End()

	// 从上下文获取 span
	span2 := tracer.SpanFromContext(ctx)
	assert.NotNil(t, span2)
}

func TestAgentTracer_AddEvent(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	ctx, span := tracer.StartSpan(ctx, "test-operation")
	defer span.End()

	// 添加事件
	tracer.AddEvent(ctx, "test-event",
		attribute.String("event.type", "test"),
		attribute.Int("event.count", 1),
	)
}

func TestAgentTracer_SetAttributes(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	ctx, span := tracer.StartSpan(ctx, "test-operation")
	defer span.End()

	// 设置属性
	tracer.SetAttributes(ctx,
		attribute.String("attr.key1", "value1"),
		attribute.Int("attr.key2", 42),
	)
}

func TestAgentTracer_RecordError(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	ctx, span := tracer.StartSpan(ctx, "test-operation")
	defer span.End()

	// 记录错误
	testErr := errors.New("test error")
	tracer.RecordError(ctx, testErr)
}

func TestAgentTracer_SetStatus(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	ctx, span := tracer.StartSpan(ctx, "test-operation")
	defer span.End()

	// 设置状态
	tracer.SetStatus(ctx, codes.Ok, "operation completed")
}

func TestAgentTracer_StartAgentSpan(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	_, span := tracer.StartAgentSpan(ctx, "test-agent",
		attribute.String("agent.type", "test"),
	)
	assert.NotNil(t, span)
	span.End()
}

func TestAgentTracer_StartToolSpan(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	_, span := tracer.StartToolSpan(ctx, "test-tool",
		attribute.String("tool.version", "1.0"),
	)
	assert.NotNil(t, span)
	span.End()
}

func TestAgentTracer_StartLLMSpan(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	_, span := tracer.StartLLMSpan(ctx, "gpt-4",
		attribute.String("llm.provider", "openai"),
	)
	assert.NotNil(t, span)
	span.End()
}

func TestAgentTracer_WithSpanContext(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		err := tracer.WithSpanContext(ctx, "test-operation", func(ctx context.Context) error {
			// 操作成功
			return nil
		})
		assert.NoError(t, err)
	})

	t.Run("error", func(t *testing.T) {
		testErr := errors.New("test error")
		err := tracer.WithSpanContext(ctx, "test-operation", func(ctx context.Context) error {
			// 操作失败
			return testErr
		})
		assert.Error(t, err)
		assert.Equal(t, testErr, err)
	})
}

func TestAgentAttributes(t *testing.T) {
	attrs := AgentAttributes("test-agent", "react")
	assert.Len(t, attrs, 2)
	assert.Equal(t, "agent.name", string(attrs[0].Key))
	assert.Equal(t, "test-agent", attrs[0].Value.AsString())
}

func TestToolAttributes(t *testing.T) {
	attrs := ToolAttributes("calculator", "math")
	assert.Len(t, attrs, 2)
	assert.Equal(t, "tool.name", string(attrs[0].Key))
	assert.Equal(t, "calculator", attrs[0].Value.AsString())
}

func TestLLMAttributes(t *testing.T) {
	attrs := LLMAttributes("gpt-4", "openai", 1000)
	assert.Len(t, attrs, 3)
	assert.Equal(t, "llm.model", string(attrs[0].Key))
	assert.Equal(t, "gpt-4", attrs[0].Value.AsString())
	assert.Equal(t, "llm.tokens", string(attrs[2].Key))
	assert.Equal(t, int64(1000), attrs[2].Value.AsInt64())
}

func TestMemoryAttributes(t *testing.T) {
	attrs := MemoryAttributes("save", "conversation", 5)
	assert.Len(t, attrs, 3)
	assert.Equal(t, "memory.operation", string(attrs[0].Key))
	assert.Equal(t, "save", attrs[0].Value.AsString())
}

func TestErrorAttributes(t *testing.T) {
	attrs := ErrorAttributes("RuntimeError", "test error message")
	assert.Len(t, attrs, 2)
	assert.Equal(t, "error.type", string(attrs[0].Key))
	assert.Equal(t, "RuntimeError", attrs[0].Value.AsString())
}

func BenchmarkAgentTracer_StartSpan(b *testing.B) {
	provider, _ := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, span := tracer.StartSpan(ctx, "test-operation")
		span.End()
	}
}

func BenchmarkAgentTracer_WithSpanContext(b *testing.B) {
	provider, _ := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "test-tracer")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracer.WithSpanContext(ctx, "test-operation", func(ctx context.Context) error {
			return nil
		})
	}
}
