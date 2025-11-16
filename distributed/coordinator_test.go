package distributed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/logger"
	"github.com/kart-io/logger/core"
	"github.com/kart-io/logger/option"
)

// createTestLogger creates a logger for testing
func createTestLogger() core.Logger {
	log, _ := logger.New(&option.LogOption{
		Engine: "zap",
		Level:  "ERROR",
	})
	return log
}

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
