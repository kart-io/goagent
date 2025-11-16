package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

func TestStartAgentSpan(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		attrs     []attribute.KeyValue
	}{
		{
			name:      "basic agent span",
			agentName: "test-agent",
			attrs:     []attribute.KeyValue{},
		},
		{
			name:      "agent span with attributes",
			agentName: "advanced-agent",
			attrs: []attribute.KeyValue{
				attribute.String("agent.type", "react"),
				attribute.Int("priority", 5),
			},
		},
		{
			name:      "agent span with multiple attributes",
			agentName: "complex-agent",
			attrs: []attribute.KeyValue{
				attribute.String("agent.type", "conversational"),
				attribute.String("model", "gpt-4"),
				attribute.Bool("streaming", true),
				attribute.Int("max_tokens", 2000),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			newCtx, span := StartAgentSpan(ctx, tt.agentName, tt.attrs...)

			assert.NotNil(t, span)
			assert.NotNil(t, newCtx)
			assert.NotEqual(t, ctx, newCtx)

			span.End()
		})
	}
}

func TestStartToolSpan(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		attrs    []attribute.KeyValue
	}{
		{
			name:     "basic tool span",
			toolName: "calculator",
			attrs:    []attribute.KeyValue{},
		},
		{
			name:     "tool span with attributes",
			toolName: "search-api",
			attrs: []attribute.KeyValue{
				attribute.String("tool.version", "1.0"),
				attribute.String("api.endpoint", "https://api.example.com"),
			},
		},
		{
			name:     "tool span with multiple attributes",
			toolName: "database-query",
			attrs: []attribute.KeyValue{
				attribute.String("db.system", "mysql"),
				attribute.String("db.name", "agents"),
				attribute.Bool("cached", true),
				attribute.Int("row_count", 100),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			newCtx, span := StartToolSpan(ctx, tt.toolName, tt.attrs...)

			assert.NotNil(t, span)
			assert.NotNil(t, newCtx)

			span.End()
		})
	}
}

func TestStartRemoteAgentSpan(t *testing.T) {
	tests := []struct {
		name      string
		service   string
		agentName string
		attrs     []attribute.KeyValue
	}{
		{
			name:      "basic remote span",
			service:   "orchestrator",
			agentName: "executor",
			attrs:     []attribute.KeyValue{},
		},
		{
			name:      "remote span with attributes",
			service:   "reasoning-service",
			agentName: "analyzer",
			attrs: []attribute.KeyValue{
				attribute.String("region", "us-west-1"),
				attribute.Bool("high_priority", true),
			},
		},
		{
			name:      "cross-region remote span",
			service:   "distributed-service",
			agentName: "coordinator",
			attrs: []attribute.KeyValue{
				attribute.String("source.region", "us-east-1"),
				attribute.String("destination.region", "eu-west-1"),
				attribute.String("protocol", "grpc"),
				attribute.Int("timeout_ms", 5000),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			newCtx, span := StartRemoteAgentSpan(ctx, tt.service, tt.agentName, tt.attrs...)

			assert.NotNil(t, span)
			assert.NotNil(t, newCtx)

			span.End()
		})
	}
}

func TestRecordError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		description string
	}{
		{
			name:        "simple error",
			err:         errors.New("test error"),
			description: "a simple test error",
		},
		{
			name:        "nil error",
			err:         nil,
			description: "nil error should be handled gracefully",
		},
		{
			name:        "wrapped error",
			err:         errors.New("wrapped: inner error"),
			description: "a wrapped error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, span := StartAgentSpan(ctx, "test-agent")
			defer span.End()

			// Should not panic even if error is nil
			RecordError(span, tt.err)
		})
	}
}

func TestAddAttributes(t *testing.T) {
	ctx := context.Background()
	_, span := StartAgentSpan(ctx, "test-agent")
	defer span.End()

	attrs := []attribute.KeyValue{
		attribute.String("key1", "value1"),
		attribute.Int("key2", 42),
		attribute.Bool("key3", true),
		attribute.Float64("key4", 3.14),
	}

	// Should not panic
	AddAttributes(span, attrs...)
}

func TestAddEvent(t *testing.T) {
	tests := []struct {
		name  string
		event string
		attrs []attribute.KeyValue
	}{
		{
			name:  "simple event",
			event: "started",
			attrs: []attribute.KeyValue{},
		},
		{
			name:  "event with attributes",
			event: "tool_called",
			attrs: []attribute.KeyValue{
				attribute.String("tool", "calculator"),
				attribute.Bool("success", true),
			},
		},
		{
			name:  "complex event",
			event: "processing_completed",
			attrs: []attribute.KeyValue{
				attribute.String("stage", "analysis"),
				attribute.Int("items_processed", 100),
				attribute.Float64("duration_seconds", 1.5),
				attribute.Bool("has_errors", false),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, span := StartAgentSpan(ctx, "test-agent")
			defer span.End()

			// Should not panic
			AddEvent(span, tt.event, tt.attrs...)
		})
	}
}

func TestSpanLifecycle(t *testing.T) {
	t.Run("span creation and closure", func(t *testing.T) {
		ctx := context.Background()

		// Create parent span
		ctx, parentSpan := StartAgentSpan(ctx, "parent-agent")
		defer parentSpan.End()

		// Create child span
		ctx, childSpan := StartToolSpan(ctx, "child-tool")
		defer childSpan.End()

		// Add events to both spans
		AddEvent(parentSpan, "parent_event")
		AddEvent(childSpan, "child_event")

		// Add attributes
		AddAttributes(parentSpan, attribute.String("parent.attr", "value"))
		AddAttributes(childSpan, attribute.String("child.attr", "value"))
	})
}

func TestTracingWithContextPropagation(t *testing.T) {
	ctx := context.Background()

	// Create initial span
	ctx, span1 := StartAgentSpan(ctx, "agent-1")
	defer span1.End()

	// Nested span should inherit context
	ctx, span2 := StartToolSpan(ctx, "tool-1")
	defer span2.End()

	// Another nested span
	ctx, span3 := StartRemoteAgentSpan(ctx, "service", "agent-2")
	defer span3.End()

	// All should work without errors
	AddEvent(span1, "event1")
	AddEvent(span2, "event2")
	AddEvent(span3, "event3")
}

func TestErrorRecordingWithAttributes(t *testing.T) {
	ctx := context.Background()
	_, span := StartAgentSpan(ctx, "error-agent")
	defer span.End()

	testErr := errors.New("operation failed")
	RecordError(span, testErr)

	// Add context after error
	AddAttributes(span, attribute.String("error.context", "recovery_attempted"))
	AddEvent(span, "error_recorded")
}

func TestMultipleAttributesAndEvents(t *testing.T) {
	ctx := context.Background()
	_, span := StartAgentSpan(ctx, "complex-agent")
	defer span.End()

	// Add multiple sets of attributes
	for i := 0; i < 5; i++ {
		AddAttributes(span,
			attribute.Int("iteration", i),
			attribute.String("phase", "processing"),
		)
	}

	// Add multiple events
	for i := 0; i < 5; i++ {
		AddEvent(span, "iteration_completed",
			attribute.Int("iteration", i),
			attribute.Bool("success", i%2 == 0),
		)
	}
}

func TestSpanWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should still work even with canceled context
	newCtx, span := StartAgentSpan(ctx, "test-agent")
	assert.NotNil(t, span)
	assert.NotNil(t, newCtx)
	span.End()
}

func TestRecordErrorMultipleTimes(t *testing.T) {
	ctx := context.Background()
	_, span := StartAgentSpan(ctx, "test-agent")
	defer span.End()

	// Record multiple errors
	err1 := errors.New("first error")
	err2 := errors.New("second error")
	err3 := errors.New("third error")

	RecordError(span, err1)
	RecordError(span, err2)
	RecordError(span, err3)
}

func TestSpanAttributeTypes(t *testing.T) {
	ctx := context.Background()
	_, span := StartAgentSpan(ctx, "test-agent")
	defer span.End()

	attrs := []attribute.KeyValue{
		attribute.String("string_attr", "value"),
		attribute.Int("int_attr", 42),
		attribute.Int64("int64_attr", 9223372036854775807),
		attribute.Float64("float_attr", 3.14159),
		attribute.Bool("bool_attr", true),
		attribute.StringSlice("string_slice", []string{"a", "b", "c"}),
		attribute.IntSlice("int_slice", []int{1, 2, 3}),
		attribute.Int64Slice("int64_slice", []int64{1, 2, 3}),
		attribute.Float64Slice("float_slice", []float64{1.1, 2.2, 3.3}),
		attribute.BoolSlice("bool_slice", []bool{true, false, true}),
	}

	AddAttributes(span, attrs...)
}

func TestAddEventWithEmptyAttributes(t *testing.T) {
	ctx := context.Background()
	_, span := StartAgentSpan(ctx, "test-agent")
	defer span.End()

	AddEvent(span, "event_with_no_attrs")
	AddEvent(span, "event_with_attrs", attribute.String("key", "value"))
}

func BenchmarkStartAgentSpan(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, span := StartAgentSpan(ctx, "benchmark-agent")
		span.End()
	}
}

func BenchmarkStartToolSpan(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, span := StartToolSpan(ctx, "benchmark-tool")
		span.End()
	}
}

func BenchmarkAddAttributes(b *testing.B) {
	ctx := context.Background()
	_, span := StartAgentSpan(ctx, "benchmark-agent")
	defer span.End()

	attrs := []attribute.KeyValue{
		attribute.String("key1", "value1"),
		attribute.Int("key2", 42),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AddAttributes(span, attrs...)
	}
}

func BenchmarkAddEvent(b *testing.B) {
	ctx := context.Background()
	_, span := StartAgentSpan(ctx, "benchmark-agent")
	defer span.End()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AddEvent(span, "benchmark-event")
	}
}

func BenchmarkRecordError(b *testing.B) {
	ctx := context.Background()
	_, span := StartAgentSpan(ctx, "benchmark-agent")
	defer span.End()

	err := errors.New("benchmark error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RecordError(span, err)
	}
}
