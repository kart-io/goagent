package observability

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestAgentMetrics_ConcurrentRecordRequest(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()
	numGoroutines := 100

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			duration := float64(idx) * 0.1
			success := idx%2 == 0
			metrics.RecordRequest(ctx, duration, success,
				attribute.String("agent.name", "concurrent-agent"),
				attribute.Int("iteration", idx),
			)
		}(i)
	}

	wg.Wait()

	// Verify metrics are still valid
	assert.NotNil(t, metrics.requestsTotal)
	assert.NotNil(t, metrics.requestDuration)
}

func TestAgentMetrics_ConcurrentRecordToolCall(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()
	numGoroutines := 50

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			duration := float64(idx) * 0.05
			success := idx%3 != 0
			metrics.RecordToolCall(ctx, "tool-"+string(rune('0'+(idx%10))), duration, success)
		}(i)
	}

	wg.Wait()

	assert.NotNil(t, metrics.toolCallsTotal)
	assert.NotNil(t, metrics.toolDuration)
}

func TestAgentMetrics_ConcurrentIncrementActiveAgents(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()

	var wg sync.WaitGroup

	// Increment from 50 goroutines
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metrics.IncrementActiveAgents(ctx, 1)
		}()
	}

	// Decrement from 25 goroutines
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metrics.IncrementActiveAgents(ctx, -1)
		}()
	}

	wg.Wait()

	assert.NotNil(t, metrics.activeAgents)
}

func TestAgentMetrics_RecordRequest_WithVariousAttributes(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()

	tests := []struct {
		name  string
		attrs []attribute.KeyValue
	}{
		{
			name:  "no attributes",
			attrs: []attribute.KeyValue{},
		},
		{
			name: "single attribute",
			attrs: []attribute.KeyValue{
				attribute.String("agent.name", "test"),
			},
		},
		{
			name: "multiple string attributes",
			attrs: []attribute.KeyValue{
				attribute.String("agent.name", "test"),
				attribute.String("agent.type", "react"),
				attribute.String("model", "gpt-4"),
			},
		},
		{
			name: "mixed attribute types",
			attrs: []attribute.KeyValue{
				attribute.String("agent.name", "test"),
				attribute.Int("priority", 5),
				attribute.Bool("streaming", true),
				attribute.Float64("confidence", 0.95),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics.RecordRequest(ctx, 1.5, true, tt.attrs...)
		})
	}
}

func TestAgentMetrics_RecordError_WithVariousErrorTypes(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()

	errorTypes := []string{
		"runtime_error",
		"timeout_error",
		"network_error",
		"validation_error",
		"authentication_error",
		"authorization_error",
		"not_found_error",
		"conflict_error",
		"rate_limit_error",
		"internal_error",
	}

	for _, errType := range errorTypes {
		metrics.RecordError(ctx, errType,
			attribute.String("error.type", errType),
		)
	}

	assert.NotNil(t, metrics.errorsTotal)
}

func TestAgentMetrics_RecordLLMCall_WithVariousModels(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()

	tests := []struct {
		model    string
		provider string
		tokens   int
		duration float64
	}{
		{"gpt-4", "openai", 2000, 2.5},
		{"gpt-3.5-turbo", "openai", 1000, 1.2},
		{"claude-3", "anthropic", 1500, 1.8},
		{"claude-2", "anthropic", 2000, 2.1},
		{"gemini-pro", "google", 1200, 1.5},
		{"llama-2", "meta", 1800, 2.3},
		{"mistral", "mistralai", 1600, 2.0},
	}

	for _, tt := range tests {
		metrics.RecordLLMCall(ctx, tt.model, tt.provider, tt.tokens, tt.duration, true)
		metrics.RecordLLMCall(ctx, tt.model, tt.provider, tt.tokens, tt.duration, false)
	}

	assert.NotNil(t, metrics.requestsTotal)
	assert.NotNil(t, metrics.requestDuration)
}

func TestAgentMetrics_RecordMemoryOperation_WithVariousOperations(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()

	operations := []string{"save", "load", "update", "delete", "clear", "search"}
	memoryTypes := []string{"conversation", "long_term", "short_term", "semantic", "episodic"}

	for _, op := range operations {
		for _, memType := range memoryTypes {
			metrics.RecordMemoryOperation(ctx, op, memType, 100, 0.05, true)
			metrics.RecordMemoryOperation(ctx, op, memType, 50, 0.02, false)
		}
	}

	assert.NotNil(t, metrics.requestsTotal)
	assert.NotNil(t, metrics.requestDuration)
}

func TestAgentMetrics_RecordChainExecution_WithVariousCombinations(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()

	chains := []struct {
		name  string
		steps int
	}{
		{"simple-chain", 2},
		{"linear-chain", 5},
		{"complex-chain", 10},
		{"parallel-chain", 8},
		{"recursive-chain", 15},
	}

	for _, chain := range chains {
		for steps := chain.steps; steps <= chain.steps+5; steps++ {
			duration := float64(steps) * 0.5
			metrics.RecordChainExecution(ctx, chain.name, steps, duration, true)
			metrics.RecordChainExecution(ctx, chain.name, steps, duration, false)
		}
	}

	assert.NotNil(t, metrics.requestsTotal)
	assert.NotNil(t, metrics.requestDuration)
}

func TestAgentMetrics_HighFrequencyUpdates(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()

	// Record many metrics rapidly
	for i := 0; i < 1000; i++ {
		metrics.RecordRequest(ctx, float64(i)*0.001, i%2 == 0)
		metrics.RecordToolCall(ctx, "tool", float64(i)*0.001, i%3 == 0)
		metrics.RecordError(ctx, "error_type")
	}

	assert.NotNil(t, metrics.requestsTotal)
}

func TestAgentMetrics_DurationRanges(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()

	durations := []float64{
		0.001,
		0.01,
		0.1,
		0.5,
		1.0,
		2.5,
		5.0,
		10.0,
		30.0,
		60.0,
		120.0,
	}

	for _, duration := range durations {
		metrics.RecordRequest(ctx, duration, true)
		metrics.RecordToolCall(ctx, "tool", duration, true)
	}

	assert.NotNil(t, metrics.requestDuration)
	assert.NotNil(t, metrics.toolDuration)
}

func TestAgentMetrics_SuccessFailureRates(t *testing.T) {
	provider, err := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	metrics, err := NewAgentMetrics(provider, "test-metrics")
	require.NoError(t, err)

	ctx := context.Background()

	// Record 100 requests with 80% success rate
	for i := 0; i < 100; i++ {
		success := i < 80 // First 80 successful, last 20 failed
		metrics.RecordRequest(ctx, 1.0, success)
	}

	// Record 50 tool calls with 90% success rate
	for i := 0; i < 50; i++ {
		success := i < 45
		metrics.RecordToolCall(ctx, "tool", 0.5, success)
	}

	assert.NotNil(t, metrics.requestsTotal)
	assert.NotNil(t, metrics.toolCallsTotal)
}

func BenchmarkAgentMetrics_RecordRequest_Concurrent(b *testing.B) {
	provider, _ := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	defer provider.Shutdown(context.Background())

	metrics, _ := NewAgentMetrics(provider, "test-metrics")
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			metrics.RecordRequest(ctx, 1.5, true,
				attribute.String("agent.name", "bench-agent"),
			)
		}
	})
}

func BenchmarkAgentMetrics_RecordToolCall_Concurrent(b *testing.B) {
	provider, _ := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	defer provider.Shutdown(context.Background())

	metrics, _ := NewAgentMetrics(provider, "test-metrics")
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			metrics.RecordToolCall(ctx, "bench-tool", 0.1, true)
		}
	})
}

func BenchmarkAgentMetrics_IncrementActiveAgents_Concurrent(b *testing.B) {
	provider, _ := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	defer provider.Shutdown(context.Background())

	metrics, _ := NewAgentMetrics(provider, "test-metrics")
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			metrics.IncrementActiveAgents(ctx, 1)
			metrics.IncrementActiveAgents(ctx, -1)
		}
	})
}

func BenchmarkAgentMetrics_MixedOperations(b *testing.B) {
	provider, _ := NewTelemetryProvider(&TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	})
	defer provider.Shutdown(context.Background())

	metrics, _ := NewAgentMetrics(provider, "test-metrics")
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			switch i % 5 {
			case 0:
				metrics.RecordRequest(ctx, 1.0, true)
			case 1:
				metrics.RecordToolCall(ctx, "tool", 0.5, true)
			case 2:
				metrics.RecordError(ctx, "error")
			case 3:
				metrics.IncrementActiveAgents(ctx, 1)
			case 4:
				metrics.IncrementActiveAgents(ctx, -1)
			}
			i++
		}
	})
}
