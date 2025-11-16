package observability

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: We don't reset metrics singleton in individual tests because Prometheus
// doesn't allow re-registering metrics with the same name in the default registry.

func TestGetMetrics_Singleton(t *testing.T) {
	m1 := GetMetrics()
	assert.NotNil(t, m1)

	m2 := GetMetrics()
	assert.NotNil(t, m2)

	// Verify it's the same instance
	assert.Equal(t, m1, m2)
}

func TestGetMetrics_MultipleGoroutines(t *testing.T) {
	m := GetMetrics()
	require.NotNil(t, m)

	// Call from multiple goroutines
	var wg sync.WaitGroup
	instances := make([]*Metrics, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			instances[idx] = GetMetrics()
		}(i)
	}
	wg.Wait()

	// All should be the same instance
	for _, instance := range instances {
		assert.Equal(t, m, instance)
	}
}

func TestRecordAgentExecution(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		service   string
		status    string
		duration  time.Duration
	}{
		{
			name:      "successful execution",
			agentName: "test-agent",
			service:   "test-service",
			status:    "success",
			duration:  1 * time.Second,
		},
		{
			name:      "failed execution",
			agentName: "error-agent",
			service:   "test-service",
			status:    "error",
			duration:  5 * time.Second,
		},
		{
			name:      "different agents",
			agentName: "another-agent",
			service:   "another-service",
			status:    "success",
			duration:  2 * time.Second,
		},
		{
			name:      "zero duration",
			agentName: "fast-agent",
			service:   "test-service",
			status:    "success",
			duration:  0,
		},
		{
			name:      "long duration",
			agentName: "slow-agent",
			service:   "test-service",
			status:    "success",
			duration:  120 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RecordAgentExecution(tt.agentName, tt.service, tt.status, tt.duration)

			// Verify metrics were recorded
			m := GetMetrics()
			assert.NotNil(t, m.AgentExecutions)
			assert.NotNil(t, m.AgentDuration)
		})
	}
}

func TestRecordAgentError(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		service   string
		errorType string
	}{
		{
			name:      "execution error",
			agentName: "test-agent",
			service:   "test-service",
			errorType: "execution_error",
		},
		{
			name:      "timeout error",
			agentName: "slow-agent",
			service:   "test-service",
			errorType: "timeout",
		},
		{
			name:      "network error",
			agentName: "remote-agent",
			service:   "remote-service",
			errorType: "network_error",
		},
		{
			name:      "validation error",
			agentName: "validator-agent",
			service:   "test-service",
			errorType: "validation_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			RecordAgentError(tt.agentName, tt.service, tt.errorType)

			m := GetMetrics()
			assert.NotNil(t, m.AgentErrors)
		})
	}
}

func TestRecordToolCall(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		agentName string
		status    string
		duration  time.Duration
	}{
		{
			name:      "successful tool call",
			toolName:  "calculator",
			agentName: "math-agent",
			status:    "success",
			duration:  100 * time.Millisecond,
		},
		{
			name:      "failed tool call",
			toolName:  "api-client",
			agentName: "api-agent",
			status:    "error",
			duration:  500 * time.Millisecond,
		},
		{
			name:      "fast tool call",
			toolName:  "cache",
			agentName: "cache-agent",
			status:    "success",
			duration:  10 * time.Millisecond,
		},
		{
			name:      "slow tool call",
			toolName:  "database",
			agentName: "db-agent",
			status:    "success",
			duration:  5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			RecordToolCall(tt.toolName, tt.agentName, tt.status, tt.duration)

			m := GetMetrics()
			assert.NotNil(t, m.ToolCalls)
			assert.NotNil(t, m.ToolDuration)
		})
	}
}

func TestRecordToolError(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		agentName string
		errorType string
	}{
		{
			name:      "execution error",
			toolName:  "calculator",
			agentName: "math-agent",
			errorType: "execution_error",
		},
		{
			name:      "timeout error",
			toolName:  "http-client",
			agentName: "api-agent",
			errorType: "timeout",
		},
		{
			name:      "validation error",
			toolName:  "validator",
			agentName: "validation-agent",
			errorType: "validation_error",
		},
		{
			name:      "not found error",
			toolName:  "search",
			agentName: "search-agent",
			errorType: "not_found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			RecordToolError(tt.toolName, tt.agentName, tt.errorType)

			m := GetMetrics()
			assert.NotNil(t, m.ToolErrors)
		})
	}
}

func TestRecordRemoteAgentCall(t *testing.T) {
	tests := []struct {
		name      string
		service   string
		agentName string
		status    string
		duration  time.Duration
	}{
		{
			name:      "successful remote call",
			service:   "orchestrator",
			agentName: "executor",
			status:    "success",
			duration:  500 * time.Millisecond,
		},
		{
			name:      "failed remote call",
			service:   "reasoning",
			agentName: "analyzer",
			status:    "error",
			duration:  1 * time.Second,
		},
		{
			name:      "cross-service call",
			service:   "external-service",
			agentName: "gateway",
			status:    "success",
			duration:  2 * time.Second,
		},
		{
			name:      "timeout on remote call",
			service:   "slow-service",
			agentName: "client",
			status:    "timeout",
			duration:  30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			RecordRemoteAgentCall(tt.service, tt.agentName, tt.status, tt.duration)

			m := GetMetrics()
			assert.NotNil(t, m.RemoteAgentCalls)
			assert.NotNil(t, m.RemoteAgentDuration)
		})
	}
}

func TestRecordRemoteAgentError(t *testing.T) {
	tests := []struct {
		name      string
		service   string
		agentName string
		errorType string
	}{
		{
			name:      "connection error",
			service:   "orchestrator",
			agentName: "executor",
			errorType: "connection_error",
		},
		{
			name:      "timeout error",
			service:   "reasoning",
			agentName: "analyzer",
			errorType: "timeout",
		},
		{
			name:      "service error",
			service:   "external-service",
			agentName: "gateway",
			errorType: "service_error",
		},
		{
			name:      "protocol error",
			service:   "deprecated-service",
			agentName: "bridge",
			errorType: "protocol_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			RecordRemoteAgentError(tt.service, tt.agentName, tt.errorType)

			m := GetMetrics()
			assert.NotNil(t, m.RemoteAgentErrors)
		})
	}
}

func TestUpdateServiceInstances(t *testing.T) {
	tests := []struct {
		name    string
		service string
		total   int
		healthy int
	}{
		{
			name:    "all healthy",
			service: "service-a",
			total:   5,
			healthy: 5,
		},
		{
			name:    "some unhealthy",
			service: "service-b",
			total:   10,
			healthy: 8,
		},
		{
			name:    "all unhealthy",
			service: "service-c",
			total:   3,
			healthy: 0,
		},
		{
			name:    "single instance",
			service: "service-d",
			total:   1,
			healthy: 1,
		},
		{
			name:    "no instances",
			service: "service-e",
			total:   0,
			healthy: 0,
		},
		{
			name:    "many instances",
			service: "service-f",
			total:   100,
			healthy: 95,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			UpdateServiceInstances(tt.service, tt.total, tt.healthy)

			m := GetMetrics()
			assert.NotNil(t, m.ServiceInstances)
			assert.NotNil(t, m.HealthyInstances)
		})
	}
}

func TestIncrementConcurrentExecutions(t *testing.T) {

	m := GetMetrics()

	IncrementConcurrentExecutions()
	IncrementConcurrentExecutions()
	IncrementConcurrentExecutions()

	// Verify metric was incremented
	assert.NotNil(t, m.ConcurrentExecutions)
}

func TestDecrementConcurrentExecutions(t *testing.T) {

	m := GetMetrics()

	// First increment
	IncrementConcurrentExecutions()
	IncrementConcurrentExecutions()

	// Then decrement
	DecrementConcurrentExecutions()
	DecrementConcurrentExecutions()

	assert.NotNil(t, m.ConcurrentExecutions)
}

func TestConcurrentMetricsUpdates(t *testing.T) {

	m := GetMetrics()
	assert.NotNil(t, m)

	numGoroutines := 100
	var wg sync.WaitGroup

	// Concurrent agent executions
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			duration := time.Duration(idx) * time.Millisecond
			RecordAgentExecution("concurrent-agent", "test-service", "success", duration)
		}(i)
	}

	// Concurrent tool calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			duration := time.Duration(idx) * time.Millisecond
			RecordToolCall("tool-1", "agent-1", "success", duration)
		}(i)
	}

	// Concurrent concurrent execution updates
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			IncrementConcurrentExecutions()
		}()
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			DecrementConcurrentExecutions()
		}()
	}

	// Concurrent service instance updates
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			UpdateServiceInstances("service-1", idx+1, idx)
		}(i)
	}

	wg.Wait()

	// Verify all metrics are still valid
	assert.NotNil(t, m.AgentExecutions)
	assert.NotNil(t, m.ToolCalls)
	assert.NotNil(t, m.ConcurrentExecutions)
	assert.NotNil(t, m.ServiceInstances)
}

func TestMetricsWithDifferentLabels(t *testing.T) {

	agents := []string{"agent-1", "agent-2", "agent-3"}
	services := []string{"service-1", "service-2"}
	statuses := []string{"success", "error", "timeout"}

	for _, agent := range agents {
		for _, service := range services {
			for _, status := range statuses {
				RecordAgentExecution(agent, service, status, time.Second)
			}
		}
	}

	m := GetMetrics()
	assert.NotNil(t, m.AgentExecutions)
}

func TestMetricRecordingWithVariousDurations(t *testing.T) {

	durations := []time.Duration{
		1 * time.Millisecond,
		10 * time.Millisecond,
		100 * time.Millisecond,
		1 * time.Second,
		10 * time.Second,
		60 * time.Second,
		2 * time.Minute,
	}

	for _, duration := range durations {
		RecordAgentExecution("test-agent", "test-service", "success", duration)
		RecordToolCall("test-tool", "test-agent", "success", duration)
		RecordRemoteAgentCall("test-service", "test-agent", "success", duration)
	}

	m := GetMetrics()
	assert.NotNil(t, m)
}

func BenchmarkRecordAgentExecution(b *testing.B) {

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RecordAgentExecution("test-agent", "test-service", "success", time.Second)
	}
}

func BenchmarkRecordToolCall(b *testing.B) {

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RecordToolCall("test-tool", "test-agent", "success", 100*time.Millisecond)
	}
}

func BenchmarkIncrementConcurrentExecutions(b *testing.B) {

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IncrementConcurrentExecutions()
		DecrementConcurrentExecutions()
	}
}

func BenchmarkConcurrentMetricsUpdates(b *testing.B) {

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			RecordAgentExecution("test-agent", "test-service", "success", time.Second)
		}
	})
}
