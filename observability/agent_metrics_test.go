package observability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestNewAgentMetrics(t *testing.T) {
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
	assert.NotNil(t, metrics)
	assert.NotNil(t, metrics.meter)
	assert.NotNil(t, metrics.requestsTotal)
	assert.NotNil(t, metrics.errorsTotal)
	assert.NotNil(t, metrics.toolCallsTotal)
	assert.NotNil(t, metrics.requestDuration)
	assert.NotNil(t, metrics.toolDuration)
	assert.NotNil(t, metrics.activeAgents)
}

func TestAgentMetrics_RecordRequest(t *testing.T) {
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

	// 记录成功请求
	metrics.RecordRequest(ctx, 1.5, true,
		attribute.String("agent.name", "test-agent"),
	)

	// 记录失败请求
	metrics.RecordRequest(ctx, 0.5, false,
		attribute.String("agent.name", "test-agent"),
	)
}

func TestAgentMetrics_RecordError(t *testing.T) {
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

	// 记录错误
	metrics.RecordError(ctx, "RuntimeError",
		attribute.String("agent.name", "test-agent"),
	)
}

func TestAgentMetrics_RecordToolCall(t *testing.T) {
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

	// 记录工具调用
	metrics.RecordToolCall(ctx, "calculator", 0.1, true)
	metrics.RecordToolCall(ctx, "search", 2.5, true)
	metrics.RecordToolCall(ctx, "invalid-tool", 0.01, false)
}

func TestAgentMetrics_IncrementActiveAgents(t *testing.T) {
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

	// 增加
	metrics.IncrementActiveAgents(ctx, 1)
	metrics.IncrementActiveAgents(ctx, 2)

	// 减少
	metrics.IncrementActiveAgents(ctx, -1)
}

func TestAgentMetrics_RecordAgentExecution(t *testing.T) {
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

	// 记录 Agent 执行
	metrics.RecordAgentExecution(ctx, "research-agent", "react", 3.5, true)
	metrics.RecordAgentExecution(ctx, "chat-agent", "conversational", 1.2, true)
	metrics.RecordAgentExecution(ctx, "analysis-agent", "react", 5.0, false)
}

func TestAgentMetrics_RecordLLMCall(t *testing.T) {
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

	// 记录 LLM 调用
	metrics.RecordLLMCall(ctx, "gpt-4", "openai", 1000, 2.5, true)
	metrics.RecordLLMCall(ctx, "claude-3", "anthropic", 800, 1.8, true)
	metrics.RecordLLMCall(ctx, "gemini-pro", "google", 1200, 3.2, false)
}

func TestAgentMetrics_RecordMemoryOperation(t *testing.T) {
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

	// 记录内存操作
	metrics.RecordMemoryOperation(ctx, "save", "conversation", 5, 0.1, true)
	metrics.RecordMemoryOperation(ctx, "load", "conversation", 10, 0.2, true)
	metrics.RecordMemoryOperation(ctx, "delete", "conversation", 3, 0.05, true)
}

func TestAgentMetrics_RecordChainExecution(t *testing.T) {
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

	// 记录链执行
	metrics.RecordChainExecution(ctx, "research-chain", 5, 10.5, true)
	metrics.RecordChainExecution(ctx, "analysis-chain", 3, 6.2, true)
	metrics.RecordChainExecution(ctx, "workflow-chain", 8, 15.0, false)
}

func BenchmarkAgentMetrics_RecordRequest(b *testing.B) {
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordRequest(ctx, 1.5, true,
			attribute.String("agent.name", "test-agent"),
		)
	}
}

func BenchmarkAgentMetrics_RecordToolCall(b *testing.B) {
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordToolCall(ctx, "calculator", 0.1, true)
	}
}
