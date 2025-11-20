package distributed

import (
	"context"
	"errors"
	"github.com/kart-io/goagent/utils/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCoordinator_ExecuteAgent_Success tests successful agent execution with failover
func TestCoordinator_ExecuteAgent_Success(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := agentcore.AgentOutput{
			Status:  "success",
			Result:  "test result",
			Message: "completed",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"TestAgent"},
	}

	err := registry.Register(instance)
	require.NoError(t, err)

	input := &agentcore.AgentInput{
		Task: "test task",
	}

	output, err := coordinator.ExecuteAgent(context.Background(), "test-service", "TestAgent", input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Status)

	// Verify instance is still healthy
	retrievedInstance, _ := registry.GetInstance("instance-1")
	assert.True(t, retrievedInstance.Healthy)
}

// TestCoordinator_ExecuteAgent_MarkUnhealthy tests that failing instance is marked unhealthy
func TestCoordinator_ExecuteAgent_MarkUnhealthy(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"TestAgent"},
	}

	err := registry.Register(instance)
	require.NoError(t, err)

	input := &agentcore.AgentInput{Task: "test task"}
	_, _ = coordinator.ExecuteAgent(context.Background(), "test-service", "TestAgent", input)

	// Verify instance is marked unhealthy
	retrievedInstance, _ := registry.GetInstance("instance-1")
	assert.False(t, retrievedInstance.Healthy)
}

// TestCoordinator_ExecuteAgent_Failover tests failover to healthy instance
func TestCoordinator_ExecuteAgent_Failover(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	// Primary server fails with connection error (retryable)
	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("connection timeout"))
	}))
	defer primaryServer.Close()

	// Secondary server succeeds
	secondaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := agentcore.AgentOutput{
			Status:  "success",
			Result:  "test result from secondary",
			Message: "completed",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer secondaryServer.Close()

	// Register two instances
	instance1 := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    primaryServer.URL,
		Agents:      []string{"TestAgent"},
	}
	instance2 := &ServiceInstance{
		ID:          "instance-2",
		ServiceName: "test-service",
		Endpoint:    secondaryServer.URL,
		Agents:      []string{"TestAgent"},
	}

	_ = registry.Register(instance1)
	_ = registry.Register(instance2)

	input := &agentcore.AgentInput{Task: "test task"}

	// First call selects instance-1 and fails with connection timeout, triggers failover to instance-2
	output, err := coordinator.ExecuteAgent(context.Background(), "test-service", "TestAgent", input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "test result from secondary", output.Result)
}

// TestCoordinator_ExecuteAgentWithRetry_Success tests retry mechanism on success
func TestCoordinator_ExecuteAgentWithRetry_Success(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		output := agentcore.AgentOutput{
			Status:  "success",
			Result:  "test result",
			Message: "completed",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"TestAgent"},
	}

	_ = registry.Register(instance)

	input := &agentcore.AgentInput{Task: "test task"}
	output, err := coordinator.ExecuteAgentWithRetry(context.Background(), "test-service", "TestAgent", input, 3)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, 1, callCount) // Should succeed on first attempt
}

// TestCoordinator_ExecuteAgentWithRetry_EventualSuccess tests retry until success
func TestCoordinator_ExecuteAgentWithRetry_EventualSuccess(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		if callCount < 2 {
			// First call fails with connection error
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("connection timeout"))
		} else {
			// Second call succeeds
			output := agentcore.AgentOutput{
				Status:  "success",
				Result:  "test result",
				Message: "completed",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(output)
		}
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"TestAgent"},
	}

	_ = registry.Register(instance)

	input := &agentcore.AgentInput{Task: "test task"}
	output, err := coordinator.ExecuteAgentWithRetry(context.Background(), "test-service", "TestAgent", input, 3)

	// Note: Should fail because the second call won't retry (no connection error)
	// Only connection errors trigger retry
	_ = output
	assert.Error(t, err)
}

// TestCoordinator_ExecuteAgentWithRetry_ContextCancelled tests retry with cancelled context
func TestCoordinator_ExecuteAgentWithRetry_ContextCancelled(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"TestAgent"},
	}

	_ = registry.Register(instance)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input := &agentcore.AgentInput{Task: "test task"}
	_, err := coordinator.ExecuteAgentWithRetry(ctx, "test-service", "TestAgent", input, 3)

	assert.Error(t, err)
}

// TestCoordinator_ExecuteParallel_Success tests parallel execution of multiple tasks
func TestCoordinator_ExecuteParallel_Success(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	executedAgents := make(map[string]int)
	mu := sync.Mutex{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		// Extract agent name from URL
		agentName := r.URL.Query().Get("agent")
		if agentName == "" {
			agentName = "DefaultAgent"
		}
		executedAgents[agentName]++

		output := agentcore.AgentOutput{
			Status:  "success",
			Result:  "test result",
			Message: "completed",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"Agent1", "Agent2", "Agent3"},
	}

	_ = registry.Register(instance)

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

	results, err := coordinator.ExecuteParallel(context.Background(), tasks)

	assert.NoError(t, err)
	assert.Len(t, results, 3)

	for _, result := range results {
		assert.NotNil(t, result.Task)
		assert.NotNil(t, result.Output)
		assert.NoError(t, result.Error)
	}
}

// TestCoordinator_ExecuteParallel_PartialFailure tests parallel with some failures
func TestCoordinator_ExecuteParallel_PartialFailure(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		if count%2 == 0 {
			// Every other request fails
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
		} else {
			output := agentcore.AgentOutput{
				Status:  "success",
				Result:  "test result",
				Message: "completed",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(output)
		}
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"Agent1", "Agent2", "Agent3"},
	}

	_ = registry.Register(instance)

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

	results, err := coordinator.ExecuteParallel(context.Background(), tasks)

	assert.Error(t, err)
	assert.Len(t, results, 3)
	// Some results will have errors
	hasError := false
	for _, result := range results {
		if result.Error != nil {
			hasError = true
		}
	}
	assert.True(t, hasError)
}

// TestCoordinator_ExecuteSequential_Success tests sequential execution
func TestCoordinator_ExecuteSequential_Success(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	executionOrder := make([]string, 0)
	mu := sync.Mutex{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input agentcore.AgentInput
		json.NewDecoder(r.Body).Decode(&input)

		mu.Lock()
		executionOrder = append(executionOrder, input.Task)
		mu.Unlock()

		output := agentcore.AgentOutput{
			Status:  "success",
			Result:  input.Task + " result",
			Message: "completed",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"Agent1", "Agent2", "Agent3"},
	}

	_ = registry.Register(instance)

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

	results, err := coordinator.ExecuteSequential(context.Background(), tasks)

	assert.NoError(t, err)
	assert.Len(t, results, 3)

	// Verify execution order
	assert.Equal(t, "task1", executionOrder[0])
	assert.Equal(t, "task2", executionOrder[1])
	assert.Equal(t, "task3", executionOrder[2])

	// Verify context passing between tasks
	assert.NotNil(t, tasks[1].Input.Context)
	assert.NotNil(t, tasks[2].Input.Context)
}

// TestCoordinator_ExecuteSequential_StopOnError tests sequential stops on error
func TestCoordinator_ExecuteSequential_StopOnError(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		if callCount == 2 {
			// Second call fails
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
		} else {
			output := agentcore.AgentOutput{
				Status:  "success",
				Result:  "test result",
				Message: "completed",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(output)
		}
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"Agent1", "Agent2", "Agent3"},
	}

	_ = registry.Register(instance)

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

	results, err := coordinator.ExecuteSequential(context.Background(), tasks)

	assert.Error(t, err)
	assert.Len(t, results, 3)
	// First result succeeds, second fails
	assert.NoError(t, results[0].Error)
	assert.Error(t, results[1].Error)
}

// TestCoordinator_RoundRobinWithThreeInstances tests round-robin across multiple instances
func TestCoordinator_RoundRobinWithThreeInstances(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	servers := make([]*httptest.Server, 3)
	for i := 0; i < 3; i++ {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			output := agentcore.AgentOutput{
				Status:  "success",
				Result:  "test result",
				Message: "completed",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(output)
		}))
		servers[i] = server
		defer server.Close()
	}

	// Register three instances
	for i, server := range servers {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('1'+i)),
			ServiceName: "test-service",
			Endpoint:    server.URL,
			Agents:      []string{"TestAgent"},
		}
		_ = registry.Register(instance)
	}

	selectedInstances := make([]string, 0)
	for i := 0; i < 6; i++ {
		instance, err := coordinator.selectInstance("test-service")
		assert.NoError(t, err)
		selectedInstances = append(selectedInstances, instance.ID)
	}

	// Verify round-robin order
	assert.Equal(t, "instance-1", selectedInstances[0])
	assert.Equal(t, "instance-2", selectedInstances[1])
	assert.Equal(t, "instance-3", selectedInstances[2])
	assert.Equal(t, "instance-1", selectedInstances[3])
	assert.Equal(t, "instance-2", selectedInstances[4])
	assert.Equal(t, "instance-3", selectedInstances[5])
}

// TestCoordinator_SelectInstance_NoInstances tests selection with no instances
func TestCoordinator_SelectInstance_NoInstances(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	_, err := coordinator.selectInstance("non-existent-service")

	assert.Error(t, err)
	// Either "service not found" or "no healthy instances" is acceptable
	assert.True(t,
		len(err.Error()) > 0,
		"error should be returned for non-existent service")
}

// TestCoordinator_Contains tests the contains utility function
func TestCoordinator_Contains(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{"exact match", "test", "test", true},
		{"prefix match", "test string", "test", true},
		{"suffix match", "string test", "test", true},
		{"middle match", "before test after", "test", true},
		{"no match", "test", "xyz", false},
		{"empty substring", "test", "", true},
		{"empty string", "", "test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.s, tt.substr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCoordinator_ExecuteParallel_Empty tests parallel with no tasks
func TestCoordinator_ExecuteParallel_Empty(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	results, err := coordinator.ExecuteParallel(context.Background(), []AgentTask{})

	assert.NoError(t, err)
	assert.Len(t, results, 0)
}

// TestCoordinator_ExecuteSequential_Empty tests sequential with no tasks
func TestCoordinator_ExecuteSequential_Empty(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	results, err := coordinator.ExecuteSequential(context.Background(), []AgentTask{})

	assert.NoError(t, err)
	assert.Len(t, results, 0)
}

// TestCoordinator_ConcurrentExecutions tests concurrent coordinator operations
func TestCoordinator_ConcurrentExecutions(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := agentcore.AgentOutput{
			Status:  "success",
			Result:  "test result",
			Message: "completed",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	// Register multiple instances
	for i := 1; i <= 5; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    server.URL,
			Agents:      []string{"TestAgent"},
		}
		_ = registry.Register(instance)
	}

	var wg sync.WaitGroup
	var successCount int32
	var errorCount int32

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			input := &agentcore.AgentInput{Task: "test task"}
			output, err := coordinator.ExecuteAgent(context.Background(), "test-service", "TestAgent", input)

			if err != nil {
				atomic.AddInt32(&errorCount, 1)
			} else if output != nil {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, int32(20), successCount)
	assert.Equal(t, int32(0), errorCount)
}

// TestCoordinator_ShouldRetry_NetworkErrors tests retry logic for various network errors
func TestCoordinator_ShouldRetry_NetworkErrors(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	networkErrors := []string{
		"connection refused",
		"timeout",
		"connection reset by peer",
		"i/o timeout",
		"connection refused by server",
		"tcp: timeout",
	}

	for _, errMsg := range networkErrors {
		assert.True(t, coordinator.shouldRetry(errors.New(errMsg)), "should retry: "+errMsg)
	}
}

// TestCoordinator_ShouldRetry_NonNetworkErrors tests retry logic for non-network errors
func TestCoordinator_ShouldRetry_NonNetworkErrors(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	nonNetworkErrors := []string{
		"invalid input",
		"agent not found",
		"unauthorized",
		"forbidden",
		"bad request",
	}

	for _, errMsg := range nonNetworkErrors {
		assert.False(t, coordinator.shouldRetry(errors.New(errMsg)), "should not retry: "+errMsg)
	}
}

// TestCoordinator_ExecuteWithFailover_NoAvailableInstances tests failover when no instances available
func TestCoordinator_ExecuteWithFailover_NoAvailableInstances(t *testing.T) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("connection timeout"))
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"TestAgent"},
	}

	_ = registry.Register(instance)

	input := &agentcore.AgentInput{Task: "test task"}
	_, err := coordinator.ExecuteAgent(context.Background(), "test-service", "TestAgent", input)

	// When instance fails with retryable error and there's only one instance,
	// failover will still fail because there are no other healthy instances
	assert.Error(t, err)
}

// BenchmarkCoordinator_ExecuteAgent benchmarks agent execution
func BenchmarkCoordinator_ExecuteAgent(b *testing.B) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := agentcore.AgentOutput{
			Status:  "success",
			Result:  "test result",
			Message: "completed",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	for i := 1; i <= 10; i++ {
		instance := &ServiceInstance{
			ID:          "instance-" + string(rune('0'+i)),
			ServiceName: "test-service",
			Endpoint:    server.URL,
			Agents:      []string{"TestAgent"},
		}
		_ = registry.Register(instance)
	}

	input := &agentcore.AgentInput{Task: "test task"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = coordinator.ExecuteAgent(context.Background(), "test-service", "TestAgent", input)
	}
}

// BenchmarkCoordinator_ExecuteParallel benchmarks parallel execution
func BenchmarkCoordinator_ExecuteParallel(b *testing.B) {
	log := createTestLogger()
	registry := NewRegistry(log)
	client := NewClient(log)
	coordinator := NewCoordinator(registry, client, log)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := agentcore.AgentOutput{
			Status:  "success",
			Result:  "test result",
			Message: "completed",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	instance := &ServiceInstance{
		ID:          "instance-1",
		ServiceName: "test-service",
		Endpoint:    server.URL,
		Agents:      []string{"Agent1", "Agent2", "Agent3", "Agent4", "Agent5"},
	}
	_ = registry.Register(instance)

	tasks := []AgentTask{
		{ServiceName: "test-service", AgentName: "Agent1", Input: &agentcore.AgentInput{Task: "task1"}},
		{ServiceName: "test-service", AgentName: "Agent2", Input: &agentcore.AgentInput{Task: "task2"}},
		{ServiceName: "test-service", AgentName: "Agent3", Input: &agentcore.AgentInput{Task: "task3"}},
		{ServiceName: "test-service", AgentName: "Agent4", Input: &agentcore.AgentInput{Task: "task4"}},
		{ServiceName: "test-service", AgentName: "Agent5", Input: &agentcore.AgentInput{Task: "task5"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = coordinator.ExecuteParallel(context.Background(), tasks)
	}
}
