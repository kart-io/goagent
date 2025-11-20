package distributed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kart-io/goagent/utils/json"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/assert"
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
