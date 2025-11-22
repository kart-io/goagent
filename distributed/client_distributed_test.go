package distributed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/utils/json"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


// TestClient_ExecuteAgent_Success tests successful agent execution
func TestClient_ExecuteAgent_Success(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/agents/TestAgent/execute", r.URL.Path)

		var input agentcore.AgentInput
		err := json.NewDecoder(r.Body).Decode(&input)
		assert.NoError(t, err)

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

	client := NewClient(log)
	input := &agentcore.AgentInput{
		Task: "test task",
	}

	output, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Status)
	assert.Equal(t, "test result", output.Result)
}

// TestClient_ExecuteAgent_RequestCreationError tests request creation error
func TestClient_ExecuteAgent_RequestCreationError(t *testing.T) {
	log := createTestLogger()
	client := NewClient(log)

	// Use an invalid context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input := &agentcore.AgentInput{Task: "test"}
	_, err := client.ExecuteAgent(ctx, "http://localhost:8080", "TestAgent", input)

	assert.Error(t, err)
}

// TestClient_ExecuteAgent_BadStatusCode tests agent execution with bad status code
func TestClient_ExecuteAgent_BadStatusCode(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := NewClient(log)
	input := &agentcore.AgentInput{Task: "test task"}

	output, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "agent execution failed")
}

// TestClient_ExecuteAgent_InvalidJSON tests agent execution with invalid JSON response
func TestClient_ExecuteAgent_InvalidJSON(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewClient(log)
	input := &agentcore.AgentInput{Task: "test task"}

	output, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to unmarshal response")
}

// TestClient_ExecuteAgent_Timeout tests agent execution with timeout
func TestClient_ExecuteAgent_Timeout(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(log)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	input := &agentcore.AgentInput{Task: "test task"}
	_, err := client.ExecuteAgent(ctx, server.URL, "TestAgent", input)

	assert.Error(t, err)
}

// TestClient_ExecuteAgentAsync_Success tests async agent execution
func TestClient_ExecuteAgentAsync_Success(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/agents/TestAgent/execute/async", r.URL.Path)

		result := map[string]string{"task_id": "task-123"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()

	client := NewClient(log)
	input := &agentcore.AgentInput{Task: "test task"}

	taskID, err := client.ExecuteAgentAsync(context.Background(), server.URL, "TestAgent", input)

	assert.NoError(t, err)
	assert.Equal(t, "task-123", taskID)
}

// TestClient_ExecuteAgentAsync_BadStatusCode tests async with bad status code
func TestClient_ExecuteAgentAsync_BadStatusCode(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := NewClient(log)
	input := &agentcore.AgentInput{Task: "test task"}

	taskID, err := client.ExecuteAgentAsync(context.Background(), server.URL, "TestAgent", input)

	assert.Error(t, err)
	assert.Empty(t, taskID)
	assert.Contains(t, err.Error(), "async execution failed")
}

// TestClient_ExecuteAgentAsync_InvalidJSON tests async with invalid response
func TestClient_ExecuteAgentAsync_InvalidJSON(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewClient(log)
	input := &agentcore.AgentInput{Task: "test task"}

	taskID, err := client.ExecuteAgentAsync(context.Background(), server.URL, "TestAgent", input)

	assert.Error(t, err)
	assert.Empty(t, taskID)
}

// TestClient_GetAsyncResult_Completed tests retrieving completed async result
func TestClient_GetAsyncResult_Completed(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/agents/tasks/task-123", r.URL.Path)

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

	client := NewClient(log)
	output, completed, err := client.GetAsyncResult(context.Background(), server.URL, "task-123")

	assert.NoError(t, err)
	assert.True(t, completed)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Status)
}

// TestClient_GetAsyncResult_Pending tests retrieving pending async result
func TestClient_GetAsyncResult_Pending(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewClient(log)
	output, completed, err := client.GetAsyncResult(context.Background(), server.URL, "task-123")

	assert.NoError(t, err)
	assert.False(t, completed)
	assert.Nil(t, output)
}

// TestClient_GetAsyncResult_BadStatusCode tests async result with error
func TestClient_GetAsyncResult_BadStatusCode(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := NewClient(log)
	output, completed, err := client.GetAsyncResult(context.Background(), server.URL, "task-123")

	assert.Error(t, err)
	assert.False(t, completed)
	assert.Nil(t, output)
}

// TestClient_WaitForAsyncResult_Completes tests waiting for async result completion
func TestClient_WaitForAsyncResult_Completes(t *testing.T) {
	log := createTestLogger()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		if callCount < 2 {
			// First call returns pending
			w.WriteHeader(http.StatusAccepted)
		} else {
			// Second call returns completed
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

	client := NewClient(log)
	output, err := client.WaitForAsyncResult(context.Background(), server.URL, "task-123", 10*time.Millisecond)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Status)
	assert.GreaterOrEqual(t, callCount, 2)
}

// TestClient_WaitForAsyncResult_ContextCancelled tests waiting with cancelled context
func TestClient_WaitForAsyncResult_ContextCancelled(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewClient(log)
	_, err := client.WaitForAsyncResult(ctx, server.URL, "task-123", 10*time.Millisecond)

	assert.Error(t, err)
}

// TestClient_Ping_Success tests successful health check
func TestClient_Ping_Success(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(log)
	err := client.Ping(context.Background(), server.URL)

	assert.NoError(t, err)
}

// TestClient_Ping_Failure tests failed health check
func TestClient_Ping_Failure(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(log)
	err := client.Ping(context.Background(), server.URL)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "health check failed")
}

// TestClient_Ping_ConnectionError tests health check with connection error
func TestClient_Ping_ConnectionError(t *testing.T) {
	log := createTestLogger()
	client := NewClient(log)

	err := client.Ping(context.Background(), "http://invalid-host-that-does-not-exist.example.com:99999")

	assert.Error(t, err)
}

// TestClient_ListAgents_Success tests listing agents
func TestClient_ListAgents_Success(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/agents", r.URL.Path)

		result := map[string][]string{
			"agents": {"Agent1", "Agent2", "Agent3"},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()

	client := NewClient(log)
	agents, err := client.ListAgents(context.Background(), server.URL)

	assert.NoError(t, err)
	assert.Len(t, agents, 3)
	assert.Contains(t, agents, "Agent1")
	assert.Contains(t, agents, "Agent2")
	assert.Contains(t, agents, "Agent3")
}

// TestClient_ListAgents_BadStatusCode tests list agents with bad status
func TestClient_ListAgents_BadStatusCode(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(log)
	agents, err := client.ListAgents(context.Background(), server.URL)

	assert.Error(t, err)
	assert.Nil(t, agents)
}

// TestClient_ListAgents_InvalidJSON tests list agents with invalid response
func TestClient_ListAgents_InvalidJSON(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewClient(log)
	agents, err := client.ListAgents(context.Background(), server.URL)

	assert.Error(t, err)
	assert.Nil(t, agents)
}

// TestClient_Headers tests that correct headers are sent
func TestClient_Headers(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(agentcore.AgentOutput{Status: "success"})
	}))
	defer server.Close()

	client := NewClient(log)
	input := &agentcore.AgentInput{Task: "test"}
	_, _ = client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
}

// TestClient_ExecuteAgent_WithContext tests agent execution with context
func TestClient_ExecuteAgent_WithContext(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(agentcore.AgentOutput{Status: "success"})
	}))
	defer server.Close()

	client := NewClient(log)
	input := &agentcore.AgentInput{
		Task: "test",
		Context: map[string]interface{}{
			"pod":       "test-pod",
			"namespace": "default",
		},
	}

	output, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
}

// TestClient_ExecuteAgent_LargeResponse tests with large response
func TestClient_ExecuteAgent_LargeResponse(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		largeResult := make([]byte, 10000)
		for i := range largeResult {
			largeResult[i] = 'a'
		}

		output := agentcore.AgentOutput{
			Status:  "success",
			Result:  string(largeResult),
			Message: "completed",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	client := NewClient(log)
	input := &agentcore.AgentInput{Task: "test"}

	output, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	resultStr, ok := output.Result.(string)
	assert.True(t, ok)
	assert.Equal(t, 10000, len(resultStr))
}

// TestClient_MultipleExecutions tests multiple concurrent executions
func TestClient_MultipleExecutions(t *testing.T) {
	log := createTestLogger()

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

	client := NewClient(log)
	input := &agentcore.AgentInput{Task: "test"}

	for i := 0; i < 5; i++ {
		output, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
		assert.NoError(t, err)
		assert.NotNil(t, output)
	}
}

// TestClient_ExecuteAgent_EmptyResponse tests with empty result
func TestClient_ExecuteAgent_EmptyResponse(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := agentcore.AgentOutput{
			Status: "success",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	client := NewClient(log)
	input := &agentcore.AgentInput{Task: "test"}

	output, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Status)
}

// TestClient_ResponseBodyClose tests response body is properly closed
func TestClient_ResponseBodyClose(t *testing.T) {
	log := createTestLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(agentcore.AgentOutput{Status: "success"})
	}))
	defer server.Close()

	client := NewClient(log)
	input := &agentcore.AgentInput{Task: "test"}

	// Execute multiple times to ensure cleanup
	for i := 0; i < 10; i++ {
		_, _ = client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
	}
}

// TestClient_NewClient_Configuration tests client configuration
func TestClient_NewClient_Configuration(t *testing.T) {
	log := createTestLogger()
	client := NewClient(log)

	assert.NotNil(t, client.client)
	assert.NotNil(t, client.logger)
	assert.Equal(t, 60*time.Second, client.client.Config().Timeout)
}

// TestClient_CircuitBreaker_OpensOnFailures tests circuit breaker opens after threshold failures
func TestClient_CircuitBreaker_OpensOnFailures(t *testing.T) {
	log := createTestLogger()

	// Configure circuit breaker with low threshold for testing
	cbConfig := &CircuitBreakerConfig{
		MaxFailures: 3,
		Timeout:     1 * time.Second,
	}
	client := NewClientWithCircuitBreaker(log, cbConfig)

	// Create server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Service unavailable"))
	}))
	defer server.Close()

	input := &agentcore.AgentInput{Task: "test"}

	// Execute requests until circuit opens
	for i := 0; i < 3; i++ {
		_, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
		assert.Error(t, err)
		assert.NotContains(t, err.Error(), "circuit breaker is open")
	}

	// Circuit should now be open
	assert.Equal(t, StateOpen, client.CircuitBreaker().State())

	// Next request should be blocked by circuit breaker
	_, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

// TestClient_CircuitBreaker_ClosesOnSuccess tests circuit breaker closes after successful request
func TestClient_CircuitBreaker_ClosesOnSuccess(t *testing.T) {
	log := createTestLogger()

	cbConfig := &CircuitBreakerConfig{
		MaxFailures: 2,
		Timeout:     100 * time.Millisecond,
	}
	client := NewClientWithCircuitBreaker(log, cbConfig)

	failCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failCount++
		if failCount <= 2 {
			// First 2 requests fail
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Subsequent requests succeed
		output := agentcore.AgentOutput{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	input := &agentcore.AgentInput{Task: "test"}

	// Execute failing requests
	for i := 0; i < 2; i++ {
		_, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
		assert.Error(t, err)
	}

	// Circuit should be open
	assert.Equal(t, StateOpen, client.CircuitBreaker().State())

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Next request should transition to half-open and succeed
	output, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
	assert.NoError(t, err)
	assert.NotNil(t, output)

	// Circuit should be closed
	assert.Equal(t, StateClosed, client.CircuitBreaker().State())
}

// TestClient_CircuitBreaker_ReopensOnFailureInHalfOpen tests circuit reopens on failure in half-open state
func TestClient_CircuitBreaker_ReopensOnFailureInHalfOpen(t *testing.T) {
	log := createTestLogger()

	cbConfig := &CircuitBreakerConfig{
		MaxFailures: 2,
		Timeout:     100 * time.Millisecond,
	}
	client := NewClientWithCircuitBreaker(log, cbConfig)

	// Create server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	input := &agentcore.AgentInput{Task: "test"}

	// Execute failing requests to open circuit
	for i := 0; i < 2; i++ {
		client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
	}
	require.Equal(t, StateOpen, client.CircuitBreaker().State())

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Next request should fail and reopen circuit
	_, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
	assert.Error(t, err)

	// Circuit should be open again
	assert.Equal(t, StateOpen, client.CircuitBreaker().State())
}

// TestClient_CircuitBreaker_ConnectionFailures tests circuit breaker handles connection failures
func TestClient_CircuitBreaker_ConnectionFailures(t *testing.T) {
	log := createTestLogger()

	cbConfig := &CircuitBreakerConfig{
		MaxFailures: 2,
		Timeout:     100 * time.Millisecond,
	}
	client := NewClientWithCircuitBreaker(log, cbConfig)

	input := &agentcore.AgentInput{Task: "test"}

	// Use invalid endpoint to trigger connection failures
	invalidEndpoint := "http://invalid-host-12345.example.com:99999"

	// Execute requests until circuit opens
	for i := 0; i < 2; i++ {
		_, err := client.ExecuteAgent(context.Background(), invalidEndpoint, "TestAgent", input)
		assert.Error(t, err)
	}

	// Circuit should be open
	assert.Equal(t, StateOpen, client.CircuitBreaker().State())

	// Next request should be blocked
	_, err := client.ExecuteAgent(context.Background(), invalidEndpoint, "TestAgent", input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

// TestClient_CircuitBreaker_SuccessResetsFailureCount tests success resets failure count
func TestClient_CircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	log := createTestLogger()

	cbConfig := &CircuitBreakerConfig{
		MaxFailures: 3,
		Timeout:     1 * time.Second,
	}
	client := NewClientWithCircuitBreaker(log, cbConfig)

	failNext := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failNext {
			w.WriteHeader(http.StatusInternalServerError)
			failNext = false
			return
		}

		output := agentcore.AgentOutput{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	input := &agentcore.AgentInput{Task: "test"}

	// Execute 2 failing requests
	for i := 0; i < 2; i++ {
		failNext = true
		_, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
		assert.Error(t, err)
	}

	assert.Equal(t, uint32(2), client.CircuitBreaker().Failures())
	assert.Equal(t, StateClosed, client.CircuitBreaker().State())

	// Execute successful request
	output, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
	assert.NoError(t, err)
	assert.NotNil(t, output)

	// Failure count should be reset
	assert.Equal(t, uint32(0), client.CircuitBreaker().Failures())
	assert.Equal(t, StateClosed, client.CircuitBreaker().State())
}

// TestClient_CircuitBreaker_ConcurrentRequests tests circuit breaker thread safety
func TestClient_CircuitBreaker_ConcurrentRequests(t *testing.T) {
	log := createTestLogger()

	cbConfig := &CircuitBreakerConfig{
		MaxFailures: 5,
		Timeout:     1 * time.Second,
	}
	client := NewClientWithCircuitBreaker(log, cbConfig)

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)

		// Fail first 5 requests
		if count <= 5 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		output := agentcore.AgentOutput{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	input := &agentcore.AgentInput{Task: "test"}

	// Execute concurrent requests
	const numGoroutines = 20
	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
			results <- err
		}()
	}

	// Collect results
	var circuitOpenErrors int
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		if err != nil && assert.Contains(t, err.Error(), "circuit breaker is open") {
			circuitOpenErrors++
		}
	}

	// Circuit should have opened and blocked some requests
	assert.True(t, circuitOpenErrors > 0, "some requests should be blocked by circuit breaker")
}

// TestClient_CircuitBreaker_StateChangeCallback tests state change notifications
func TestClient_CircuitBreaker_StateChangeCallback(t *testing.T) {
	log := createTestLogger()

	var stateChanges []string
	cbConfig := &CircuitBreakerConfig{
		MaxFailures: 2,
		Timeout:     100 * time.Millisecond,
		OnStateChange: func(from, to CircuitState) {
			stateChanges = append(stateChanges, from.String()+"->"+to.String())
		},
	}
	client := NewClientWithCircuitBreaker(log, cbConfig)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	input := &agentcore.AgentInput{Task: "test"}

	// Execute failing requests to open circuit
	for i := 0; i < 2; i++ {
		client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
	}

	// Wait for callback
	time.Sleep(50 * time.Millisecond)

	// Should see closed->open transition
	assert.Contains(t, stateChanges, "closed->open")
}

// TestClient_NewClientWithCircuitBreaker_NilConfig tests creation with nil config
func TestClient_NewClientWithCircuitBreaker_NilConfig(t *testing.T) {
	log := createTestLogger()
	client := NewClientWithCircuitBreaker(log, nil)

	assert.NotNil(t, client)
	assert.NotNil(t, client.CircuitBreaker())
	assert.Equal(t, uint32(5), client.CircuitBreaker().config.MaxFailures)
	assert.Equal(t, 60*time.Second, client.CircuitBreaker().config.Timeout)
}

// TestClient_CircuitBreaker_IntegrationWithRetries tests realistic failure scenarios
func TestClient_CircuitBreaker_IntegrationWithRetries(t *testing.T) {
	log := createTestLogger()

	cbConfig := &CircuitBreakerConfig{
		MaxFailures: 3,
		Timeout:     200 * time.Millisecond,
	}
	client := NewClientWithCircuitBreaker(log, cbConfig)

	var requestNum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		num := requestNum.Add(1)

		// Fail first 3 requests to open circuit
		if num <= 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		// Requests 4+ succeed (after circuit opens and recovers)
		output := agentcore.AgentOutput{Status: "success", Result: "recovered"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(output)
	}))
	defer server.Close()

	input := &agentcore.AgentInput{Task: "test"}

	// Phase 1: Execute requests until circuit opens
	for i := 0; i < 3; i++ {
		_, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
		assert.Error(t, err)
	}
	assert.Equal(t, StateOpen, client.CircuitBreaker().State())

	// Phase 2: Requests blocked by open circuit
	for i := 0; i < 2; i++ {
		_, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circuit breaker is open")
	}

	// Phase 3: Wait for timeout and recover
	time.Sleep(250 * time.Millisecond)

	output, err := client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Status)
	assert.Equal(t, StateClosed, client.CircuitBreaker().State())

	// Phase 4: Verify normal operation after recovery
	output, err = client.ExecuteAgent(context.Background(), server.URL, "TestAgent", input)
	assert.NoError(t, err)
	assert.NotNil(t, output)
}
