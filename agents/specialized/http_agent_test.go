package specialized

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/logger"
)

func TestNewHTTPAgent(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)

	assert.Equal(t, "http-agent", agent.Name())
	assert.NotEmpty(t, agent.Description())
	assert.Contains(t, agent.Capabilities(), "http_get")
	assert.Contains(t, agent.Capabilities(), "http_post")
	assert.Contains(t, agent.Capabilities(), "http_put")
	assert.Contains(t, agent.Capabilities(), "http_delete")
	assert.Contains(t, agent.Capabilities(), "http_patch")
}

func TestHTTPAgent_Execute_GET_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"method": "GET",
			"url":    server.URL,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	assert.NotNil(t, output.Result)

	result := output.Result.(map[string]interface{})
	assert.Equal(t, 200, result["status_code"])
	assert.NotNil(t, result["body"])
	assert.Len(t, output.ToolCalls, 1)
	assert.True(t, output.ToolCalls[0].Success)
}

func TestHTTPAgent_Execute_GET_WithHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"authenticated": "true"})
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"method": "GET",
			"url":    server.URL,
			"headers": map[string]string{
				"Authorization": "Bearer token123",
				"Accept":        "application/json",
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Execute_POST_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "test", body["name"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]int{"id": 123})
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"method": "POST",
			"url":    server.URL,
			"body": map[string]string{
				"name": "test",
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.Equal(t, 201, result["status_code"])
}

func TestHTTPAgent_Execute_PUT_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"updated": "true"})
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"method": "PUT",
			"url":    server.URL,
			"body": map[string]string{
				"id": "123",
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Execute_DELETE_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"method": "DELETE",
			"url":    server.URL,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Execute_DefaultMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should default to GET
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Execute_MissingURL(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "url is required")
}

func TestHTTPAgent_Execute_InvalidURL(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": "http://invalid.url.that.doesnt.exist.test:99999",
		},
	}

	output, err := agent.Execute(ctx, input)

	// Either error or failed status is acceptable
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.NotNil(t, output)
		assert.Equal(t, "failed", output.Status)
	}
}

func TestHTTPAgent_Execute_HTTPError_4xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid request"}`))
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := agent.Execute(ctx, input)

	// Should return output with error status
	assert.NoError(t, err)
	assert.Equal(t, "failed", output.Status)
	result := output.Result.(map[string]interface{})
	assert.Equal(t, 400, result["status_code"])
}

func TestHTTPAgent_Execute_HTTPError_5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "failed", output.Status)
}

func TestHTTPAgent_Execute_JSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   1,
			"name": "test",
			"tags": []string{"a", "b"},
		})
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	body := result["body"].(map[string]interface{})
	assert.Equal(t, float64(1), body["id"])
	assert.Equal(t, "test", body["name"])
}

func TestHTTPAgent_Execute_TextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("This is plain text response"))
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	result := output.Result.(map[string]interface{})
	// Non-JSON responses are returned as strings
	assert.IsType(t, "", result["body"])
}

func TestHTTPAgent_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	output, err := agent.Get(ctx, server.URL, map[string]string{
		"X-Custom": "value",
	})

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Post(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	output, err := agent.Post(ctx, server.URL, map[string]string{"key": "value"}, nil)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Put(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	output, err := agent.Put(ctx, server.URL, map[string]string{"key": "value"}, nil)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	output, err := agent.Delete(ctx, server.URL, nil)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Execute_WithTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": server.URL,
		},
		Options: agentcore.AgentOptions{
			Timeout: 5 * time.Second,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Execute_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel immediately
	cancel()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": server.URL,
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
}

func TestHTTPAgent_Execute_ComplexJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "user", body["type"])
		assert.Equal(t, float64(30), body["age"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"method": "POST",
			"url":    server.URL,
			"body": map[string]interface{}{
				"type": "user",
				"age":  30,
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Execute_OutputStructure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "custom-value")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := agent.Execute(ctx, input)

	require.NoError(t, err)

	// Verify output structure
	assert.NotZero(t, output.Latency)
	assert.NotZero(t, output.Timestamp)
	assert.Len(t, output.ToolCalls, 1)

	toolCall := output.ToolCalls[0]
	assert.Equal(t, "http", toolCall.ToolName)
	assert.NotZero(t, toolCall.Duration)
	assert.NotEmpty(t, toolCall.Input)
	assert.NotEmpty(t, toolCall.Output)

	result := output.Result.(map[string]interface{})
	assert.NotEmpty(t, result["headers"])
	assert.NotEmpty(t, result["body"])
}

func TestHTTPAgent_Execute_EmptyResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestHTTPAgent_Execute_LargeJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		data := map[string]interface{}{
			"items": make([]map[string]string, 100),
		}
		for i := 0; i < 100; i++ {
			data["items"].([]map[string]string)[i] = map[string]string{
				"id":   "id" + string(rune(i)),
				"name": "item" + string(rune(i)),
			}
		}
		json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()

	l, _ := logger.NewWithDefaults()
	agent := NewHTTPAgent(l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	result := output.Result.(map[string]interface{})
	assert.NotEmpty(t, result["body"])
}

func TestHTTPAgent_Execute_ContentTypeDetection(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        interface{}
		checkBody   func(t *testing.T, body interface{})
	}{
		{
			name:        "JSON content type",
			contentType: "application/json",
			body:        `{"key":"value"}`,
			checkBody: func(t *testing.T, body interface{}) {
				m, ok := body.(map[string]interface{})
				assert.True(t, ok)
				assert.NotEmpty(t, m)
			},
		},
		{
			name:        "plain text content type",
			contentType: "text/plain",
			body:        "plain text",
			checkBody: func(t *testing.T, body interface{}) {
				s, ok := body.(string)
				assert.True(t, ok)
				assert.Equal(t, "plain text", s)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.Write([]byte(tt.body.(string)))
			}))
			defer server.Close()

			l, _ := logger.NewWithDefaults()
			agent := NewHTTPAgent(l)
			ctx := context.Background()

			input := &agentcore.AgentInput{
				Context: map[string]interface{}{
					"url": server.URL,
				},
			}

			output, err := agent.Execute(ctx, input)

			assert.NoError(t, err)
			result := output.Result.(map[string]interface{})
			tt.checkBody(t, result["body"])
		})
	}
}
