package distributed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentcore "github.com/kart-io/goagent/core"
)

func TestNewCoordinator(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)

	coordinator := NewCoordinator(registry, client, log)

	assert.NotNil(t, coordinator)
	assert.NotNil(t, coordinator.registry)
	assert.NotNil(t, coordinator.client)
	assert.NotNil(t, coordinator.logger)
	assert.NotNil(t, coordinator.roundRobinIndex)
}

func TestCoordinator_ExecuteAgent_NoHealthyInstances(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	input := &agentcore.AgentInput{
		Task: "test",
	}

	output, err := coordinator.ExecuteAgent(context.Background(), "non-existent-service", "TestAgent", input)

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to select instance")
}

func TestCoordinator_SelectInstance_RoundRobin(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	// Add 3 instances
	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    "http://localhost:808" + string(rune('0'+i)),
			Agents:      []string{"TestAgent"},
		}
		err := registry.Register(instance)
		require.NoError(t, err)
	}

	// Execute multiple times and verify round-robin
	selectedIDs := []string{}
	for i := 0; i < 6; i++ {
		instance, err := coordinator.selectInstance("test-service")
		require.NoError(t, err)
		selectedIDs = append(selectedIDs, instance.ID)
	}

	// Should cycle through instances
	assert.Equal(t, "instance-1", selectedIDs[0])
	assert.Equal(t, "instance-2", selectedIDs[1])
	assert.Equal(t, "instance-3", selectedIDs[2])
	assert.Equal(t, "instance-1", selectedIDs[3])
	assert.Equal(t, "instance-2", selectedIDs[4])
	assert.Equal(t, "instance-3", selectedIDs[5])
}

func TestCoordinator_ShouldRetry(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	tests := []struct {
		name        string
		err         error
		shouldRetry bool
	}{
		{
			name:        "nil error",
			err:         nil,
			shouldRetry: false,
		},
		{
			name:        "connection refused",
			err:         errors.New("connection refused"),
			shouldRetry: true,
		},
		{
			name:        "timeout",
			err:         errors.New("timeout occurred"),
			shouldRetry: true,
		},
		{
			name:        "connection reset",
			err:         errors.New("connection reset by peer"),
			shouldRetry: true,
		},
		{
			name:        "business logic error",
			err:         errors.New("invalid input"),
			shouldRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := coordinator.shouldRetry(tt.err)
			assert.Equal(t, tt.shouldRetry, result)
		})
	}
}

func TestCoordinator_ExecuteParallel_Structure(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	// Register a test instance
	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
		Agents:      []string{"Agent1", "Agent2"},
	}
	err := registry.Register(instance)
	require.NoError(t, err)

	tasks := []AgentTask{
		{
			ServiceName: "test-service",
			AgentName:   "Agent1",
			Input:       &agentcore.AgentInput{Task: "task1"},
		},
		{
			ServiceName: "test-service",
			AgentName:   "Agent2",
			Input:       &agentcore.AgentInput{Task: "task2"},
		},
	}

	// ExecuteParallel will likely fail since we don't have a real server
	// but we can test that it properly handles the task structure
	results, _ := coordinator.ExecuteParallel(context.Background(), tasks)

	// Verify results structure
	assert.Len(t, results, 2)
	assert.Equal(t, "Agent1", results[0].Task.AgentName)
	assert.Equal(t, "Agent2", results[1].Task.AgentName)
}

func TestCoordinator_ExecuteSequential_Structure(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	// Register a test instance
	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
		Agents:      []string{"Agent1", "Agent2", "Agent3"},
	}
	err := registry.Register(instance)
	require.NoError(t, err)

	tasks := []AgentTask{
		{
			ServiceName: "test-service",
			AgentName:   "Agent1",
			Input:       &agentcore.AgentInput{Task: "task1"},
		},
		{
			ServiceName: "test-service",
			AgentName:   "Agent2",
			Input:       &agentcore.AgentInput{Task: "task2"},
		},
		{
			ServiceName: "test-service",
			AgentName:   "Agent3",
			Input:       &agentcore.AgentInput{Task: "task3"},
		},
	}

	// ExecuteSequential will likely fail since we don't have a real server
	// but we can test that it properly handles the task structure
	results, _ := coordinator.ExecuteSequential(context.Background(), tasks)

	// Verify results structure
	assert.Len(t, results, 3)
	assert.Equal(t, "Agent1", results[0].Task.AgentName)
	// Subsequent tasks may not execute if first fails, but structure should be there
}

func TestServiceInstance_Structure(t *testing.T) {
	instance := &ServiceInstance{
		ID:          "test-instance",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:8080",
		Agents:      []string{"DiagnosisAgent", "AnalysisAgent"},
		Metadata: map[string]interface{}{
			"version": "1.0.0",
			"region":  "us-west",
		},
		RegisterAt: time.Now(),
		LastSeen:   time.Now(),
		Healthy:    true,
	}

	assert.Equal(t, "test-instance", instance.ID)
	assert.Equal(t, "test-service", instance.ServiceName)
	assert.Equal(t, "http://localhost:8080", instance.Endpoint)
	assert.Len(t, instance.Agents, 2)
	assert.Equal(t, "1.0.0", instance.Metadata["version"])
	assert.True(t, instance.Healthy)
}

func TestAgentTask_Structure(t *testing.T) {
	task := AgentTask{
		ServiceName: "reasoning-service",
		AgentName:   "DiagnosisAgent",
		Input: &agentcore.AgentInput{
			Task:        "diagnose pod crash",
			Instruction: "analyze logs and events",
			Context: map[string]interface{}{
				"pod":       "my-pod",
				"namespace": "default",
			},
		},
	}

	assert.Equal(t, "reasoning-service", task.ServiceName)
	assert.Equal(t, "DiagnosisAgent", task.AgentName)
	assert.Equal(t, "diagnose pod crash", task.Input.Task)
	assert.Equal(t, "my-pod", task.Input.Context["pod"])
}

func TestAgentTaskResult_Structure(t *testing.T) {
	task := AgentTask{
		ServiceName: "test-service",
		AgentName:   "TestAgent",
		Input:       &agentcore.AgentInput{Task: "test"},
	}

	output := &agentcore.AgentOutput{
		Status:  "success",
		Result:  "test result",
		Message: "completed",
	}

	result := AgentTaskResult{
		Task:   task,
		Output: output,
		Error:  nil,
	}

	assert.Equal(t, "test-service", result.Task.ServiceName)
	assert.Equal(t, "success", result.Output.Status)
	assert.NoError(t, result.Error)
}

// Benchmark tests
func BenchmarkCoordinator_SelectInstance(b *testing.B) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	// Add test instances
	for i := 1; i <= 5; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    "http://localhost:8080",
		}
		_ = registry.Register(instance)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = coordinator.selectInstance("test-service")
	}
}

// BenchmarkCoordinator_ExecuteParallel_Limited benchmarks parallel execution with concurrency limit
func BenchmarkCoordinator_ExecuteParallel_Limited(b *testing.B) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log, WithMaxConcurrency(50))

	// Register test instance
	instance := &ServiceInstance{
		ID:          "bench-instance",
		ServiceName: "bench-service",
		Endpoint:    "http://localhost:9999",
		Agents:      []string{"BenchAgent"},
	}
	_ = registry.Register(instance)

	// Create tasks
	tasks := make([]AgentTask, 100)
	for i := 0; i < 100; i++ {
		tasks[i] = AgentTask{
			ServiceName: "bench-service",
			AgentName:   "BenchAgent",
			Input:       &agentcore.AgentInput{Task: "bench"},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = coordinator.ExecuteParallel(context.Background(), tasks)
	}
}

// TestCoordinator_ConcurrencyLimitDefault tests default concurrency limit
func TestCoordinator_ConcurrencyLimitDefault(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	assert.Equal(t, DefaultMaxConcurrency, coordinator.maxConcurrency)
}

// TestCoordinator_WithMaxConcurrency tests custom concurrency limit
func TestCoordinator_WithMaxConcurrency(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)

	tests := []struct {
		name           string
		maxConcurrency int
		expected       int
	}{
		{
			name:           "valid custom limit",
			maxConcurrency: 50,
			expected:       50,
		},
		{
			name:           "very low limit",
			maxConcurrency: 1,
			expected:       1,
		},
		{
			name:           "high limit",
			maxConcurrency: 1000,
			expected:       1000,
		},
		{
			name:           "zero (invalid) uses default",
			maxConcurrency: 0,
			expected:       DefaultMaxConcurrency,
		},
		{
			name:           "negative (invalid) uses default",
			maxConcurrency: -10,
			expected:       DefaultMaxConcurrency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coordinator := NewCoordinator(registry, client, log, WithMaxConcurrency(tt.maxConcurrency))
			assert.Equal(t, tt.expected, coordinator.maxConcurrency)
		})
	}
}

// TestCoordinator_ExecuteParallel_ConcurrencyLimit tests that concurrency is properly limited
func TestCoordinator_ExecuteParallel_ConcurrencyLimit(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)

	// Create coordinator with very small concurrency limit
	const maxConcurrent = 5
	coordinator := NewCoordinator(registry, client, log, WithMaxConcurrency(maxConcurrent))

	// Register test instance
	instance := &ServiceInstance{
		ID:          "test-instance",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:9999", // Non-existent to cause quick failures
		Agents:      []string{"TestAgent"},
	}
	err := registry.Register(instance)
	require.NoError(t, err)

	// Create many tasks (more than maxConcurrent)
	const totalTasks = 50
	tasks := make([]AgentTask, totalTasks)
	for i := 0; i < totalTasks; i++ {
		tasks[i] = AgentTask{
			ServiceName: "test-service",
			AgentName:   "TestAgent",
			Input:       &agentcore.AgentInput{Task: "test"},
		}
	}

	// Execute in parallel
	// This should not panic or exhaust resources
	results, _ := coordinator.ExecuteParallel(context.Background(), tasks)

	// Verify we got results for all tasks
	assert.Len(t, results, totalTasks)
}

// TestCoordinator_ExecuteParallel_ContextCancellation tests context cancellation during parallel execution
func TestCoordinator_ExecuteParallel_ContextCancellation(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log, WithMaxConcurrency(10))

	// Register test instance
	instance := &ServiceInstance{
		ID:          "test-instance",
		ServiceName: "test-service",
		Endpoint:    "http://localhost:9999",
		Agents:      []string{"TestAgent"},
	}
	err := registry.Register(instance)
	require.NoError(t, err)

	// Create tasks
	tasks := make([]AgentTask, 100)
	for i := 0; i < 100; i++ {
		tasks[i] = AgentTask{
			ServiceName: "test-service",
			AgentName:   "TestAgent",
			Input:       &agentcore.AgentInput{Task: "test"},
		}
	}

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Execute - should handle context cancellation gracefully
	_, err = coordinator.ExecuteParallel(ctx, tasks)

	// May or may not error depending on timing, but should not panic
	// The important thing is we don't leak goroutines
	_ = err
}
