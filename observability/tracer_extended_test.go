package observability

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func TestAgentTracer_StartMemorySpan(t *testing.T) {
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

	tests := []struct {
		name      string
		operation string
		attrs     []attribute.KeyValue
	}{
		{
			name:      "save operation",
			operation: "save",
			attrs: []attribute.KeyValue{
				attribute.String("memory.type", "conversation"),
			},
		},
		{
			name:      "load operation",
			operation: "load",
			attrs: []attribute.KeyValue{
				attribute.String("memory.type", "long_term"),
			},
		},
		{
			name:      "delete operation",
			operation: "delete",
			attrs: []attribute.KeyValue{
				attribute.String("memory.type", "temporary"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx, span := tracer.StartMemorySpan(ctx, tt.operation, tt.attrs...)

			assert.NotNil(t, span)
			assert.NotNil(t, ctx)

			span.End()
		})
	}
}

func TestAgentTracer_StartChainSpan(t *testing.T) {
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

	tests := []struct {
		name      string
		chainName string
		attrs     []attribute.KeyValue
	}{
		{
			name:      "simple chain",
			chainName: "simple-chain",
			attrs: []attribute.KeyValue{
				attribute.Int("step_count", 3),
			},
		},
		{
			name:      "complex chain",
			chainName: "complex-chain",
			attrs: []attribute.KeyValue{
				attribute.Int("step_count", 10),
				attribute.Bool("parallel", true),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx, span := tracer.StartChainSpan(ctx, tt.chainName, tt.attrs...)

			assert.NotNil(t, span)
			assert.NotNil(t, ctx)

			span.End()
		})
	}
}

func TestAgentTracer_SetStatus_Various(t *testing.T) {
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

	tests := []struct {
		name        string
		code        codes.Code
		description string
	}{
		{
			name:        "ok status",
			code:        codes.Ok,
			description: "operation completed successfully",
		},
		{
			name:        "error status",
			code:        codes.Error,
			description: "operation failed",
		},
		{
			name:        "unset status",
			code:        codes.Unset,
			description: "status not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, span := tracer.StartSpan(ctx, "test-operation")
			defer span.End()

			tracer.SetStatus(ctx, tt.code, tt.description)
		})
	}
}

func TestAgentTracer_AddEvent_WithComplexAttributes(t *testing.T) {
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
	_, span := tracer.StartSpan(ctx, "test-operation")
	defer span.End()

	// Add events with various attribute combinations
	tracer.AddEvent(ctx, "event1")

	tracer.AddEvent(ctx, "event2",
		attribute.String("type", "info"),
		attribute.Int("count", 5),
	)

	tracer.AddEvent(ctx, "event3",
		attribute.String("type", "warning"),
		attribute.Float64("severity", 0.8),
		attribute.Bool("requires_action", true),
	)

	tracer.AddEvent(ctx, "event4",
		attribute.StringSlice("tags", []string{"important", "urgent"}),
		attribute.IntSlice("ids", []int{1, 2, 3}),
	)
}

func TestAgentTracer_NestedSpans(t *testing.T) {
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

	// Parent span
	ctx, parentSpan := tracer.StartAgentSpan(ctx, "parent-agent")
	defer parentSpan.End()

	tracer.AddEvent(ctx, "parent_started")

	// Child span 1
	ctx, childSpan1 := tracer.StartToolSpan(ctx, "child-tool-1")
	defer childSpan1.End()
	tracer.AddEvent(ctx, "child1_event")

	// Grandchild span
	ctx, grandchildSpan := tracer.StartLLMSpan(ctx, "grandchild-llm")
	defer grandchildSpan.End()
	tracer.AddEvent(ctx, "grandchild_event")

	// Back to child 1
	childSpan1.End()

	// Child span 2
	ctx2 := context.Background()
	ctx2, childSpan2 := tracer.StartMemorySpan(ctx2, "memory-op")
	defer childSpan2.End()
	tracer.AddEvent(ctx2, "child2_event")
}

func TestAgentTracer_ErrorRecording_MultipleErrors(t *testing.T) {
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

	_, span := tracer.StartSpan(ctx, "test-operation")
	defer span.End()

	// Record multiple errors
	err1 := errors.New("error 1")
	err2 := errors.New("error 2")
	err3 := errors.New("error 3")

	tracer.RecordError(ctx, err1)
	tracer.RecordError(ctx, err2)
	tracer.RecordError(ctx, err3)
}

func TestAgentTracer_WithSpanContext_Success(t *testing.T) {
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

	err = tracer.WithSpanContext(ctx, "operation", func(ctx context.Context) error {
		// Perform operations within span
		return nil
	})

	assert.NoError(t, err)
}

func TestAgentTracer_WithSpanContext_Error(t *testing.T) {
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

	testErr := errors.New("operation failed")
	err = tracer.WithSpanContext(ctx, "operation", func(ctx context.Context) error {
		return testErr
	})

	assert.Error(t, err)
	assert.Equal(t, testErr, err)
}

func TestAgentTracer_WithSpanContext_WithAttributes(t *testing.T) {
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

	attrs := []attribute.KeyValue{
		attribute.String("operation.type", "analysis"),
		attribute.Int("attempt", 1),
	}

	err = tracer.WithSpanContext(ctx, "analysis", func(ctx context.Context) error {
		tracer.SetAttributes(ctx, attrs...)
		tracer.AddEvent(ctx, "analysis_started")
		return nil
	}, attrs...)

	assert.NoError(t, err)
}

func TestAgentTracer_Concurrent_SpanCreation(t *testing.T) {
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

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			ctx := context.Background()
			_, span := tracer.StartSpan(ctx, "operation-"+string(rune('0'+(idx%10))))
			tracer.AddEvent(ctx, "event-"+string(rune('0'+(idx%5))))
			span.End()
		}(i)
	}

	wg.Wait()
}

func TestAgentTracer_Concurrent_AttributeUpdates(t *testing.T) {
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

	_, span := tracer.StartSpan(ctx, "shared-span")
	defer span.End()

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tracer.SetAttributes(ctx,
				attribute.Int("iteration", idx),
				attribute.String("worker", "worker-"+string(rune('0'+(idx%10)))),
			)
		}(i)
	}

	wg.Wait()
}

func TestAgentTracer_SpanContextManagement(t *testing.T) {
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

	// Create first span
	ctx1, span1 := tracer.StartSpan(ctx, "operation1")

	// Get span from context
	retrievedSpan1 := tracer.SpanFromContext(ctx1)
	assert.NotNil(t, retrievedSpan1)

	// Create nested span
	ctx2, span2 := tracer.StartSpan(ctx1, "operation2")

	// Verify nested context
	retrievedSpan2 := tracer.SpanFromContext(ctx2)
	assert.NotNil(t, retrievedSpan2)

	span2.End()
	span1.End()
}

func TestAttributeHelpers_Combinations(t *testing.T) {
	// Test all attribute helper functions
	agentAttrs := AgentAttributes("test-agent", "react")
	assert.Len(t, agentAttrs, 2)

	toolAttrs := ToolAttributes("calculator", "math")
	assert.Len(t, toolAttrs, 2)

	llmAttrs := LLMAttributes("gpt-4", "openai", 1000)
	assert.Len(t, llmAttrs, 3)

	memoryAttrs := MemoryAttributes("save", "conversation", 100)
	assert.Len(t, memoryAttrs, 3)

	errorAttrs := ErrorAttributes("RuntimeError", "test error message")
	assert.Len(t, errorAttrs, 2)

	// Verify attribute values
	assert.Equal(t, "agent.name", string(agentAttrs[0].Key))
	assert.Equal(t, "test-agent", agentAttrs[0].Value.AsString())

	assert.Equal(t, "tool.name", string(toolAttrs[0].Key))
	assert.Equal(t, "calculator", toolAttrs[0].Value.AsString())

	assert.Equal(t, "llm.model", string(llmAttrs[0].Key))
	assert.Equal(t, "gpt-4", llmAttrs[0].Value.AsString())
}

func TestAgentTracer_GetTracer_Returns_Underlying(t *testing.T) {
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

	underlying := tracer.GetTracer()
	assert.NotNil(t, underlying)
	assert.Equal(t, tracer.tracer, underlying)
}

func TestAgentTracer_FullWorkflow(t *testing.T) {
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

	// Start main operation
	ctx, mainSpan := tracer.StartAgentSpan(ctx, "main-agent")
	defer mainSpan.End()

	tracer.AddEvent(ctx, "operation_started")
	tracer.SetAttributes(ctx, attribute.String("status", "in_progress"))

	// Perform work with nested spans
	for i := 0; i < 3; i++ {
		ctx2, toolSpan := tracer.StartToolSpan(ctx, "tool-"+string(rune('0'+i)))
		tracer.AddEvent(ctx2, "tool_started")

		// Simulate tool work
		if i == 2 {
			tracer.RecordError(ctx2, errors.New("tool error"))
		}

		toolSpan.End()
	}

	// Final status
	tracer.SetAttributes(ctx, attribute.String("status", "completed"))
	tracer.SetStatus(ctx, codes.Ok, "operation completed successfully")
	tracer.AddEvent(ctx, "operation_completed")
}

func BenchmarkAgentTracer_StartAgentSpan(b *testing.B) {
	provider, _ := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "bench-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "bench-tracer")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, span := tracer.StartAgentSpan(ctx, "bench-agent")
		span.End()
	}
}

func BenchmarkAgentTracer_WithSpanContext_Success(b *testing.B) {
	provider, _ := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "bench-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "bench-tracer")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracer.WithSpanContext(ctx, "operation", func(ctx context.Context) error {
			return nil
		})
	}
}

func BenchmarkAgentTracer_Concurrent_SpanOperations(b *testing.B) {
	provider, _ := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:    "bench-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	})
	defer provider.Shutdown(context.Background())

	tracer := NewAgentTracer(provider, "bench-tracer")
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := tracer.StartSpan(ctx, "operation")
			tracer.AddEvent(ctx, "event")
			tracer.SetAttributes(ctx, attribute.String("key", "value"))
			span.End()
		}
	})
}
