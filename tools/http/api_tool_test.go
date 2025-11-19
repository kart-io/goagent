package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAPITool tests API tool creation
func TestNewAPITool(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		timeout  time.Duration
		headers  map[string]string
		expected struct {
			timeout time.Duration
			headers int
		}
	}{
		{
			name:    "Default timeout",
			baseURL: "https://api.example.com",
			timeout: 0,
			headers: nil,
			expected: struct {
				timeout time.Duration
				headers int
			}{timeout: 30 * time.Second, headers: 0},
		},
		{
			name:    "Custom timeout and headers",
			baseURL: "https://api.example.com",
			timeout: 10 * time.Second,
			headers: map[string]string{"X-Custom": "value"},
			expected: struct {
				timeout time.Duration
				headers int
			}{timeout: 10 * time.Second, headers: 1},
		},
		{
			name:    "Empty baseURL",
			baseURL: "",
			timeout: 5 * time.Second,
			headers: map[string]string{},
			expected: struct {
				timeout time.Duration
				headers int
			}{timeout: 5 * time.Second, headers: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewAPITool(tt.baseURL, tt.timeout, tt.headers)

			assert.NotNil(t, tool)
			assert.Equal(t, "api", tool.Name())
			assert.Contains(t, tool.Description(), "HTTP API")
			assert.Equal(t, tt.baseURL, tool.baseURL)
			assert.Equal(t, tt.expected.timeout, tool.client.GetClient().Timeout)
			assert.Len(t, tool.headers, tt.expected.headers)
		})
	}
}

// TestAPITool_GET tests GET requests
func TestAPITool_GET(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/test", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success","data":{"id":123}}`))
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 5*time.Second, nil)
	ctx := context.Background()

	output, err := tool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method": "GET",
			"url":    "/test",
		},
		Context: ctx,
	})

	require.NoError(t, err)
	assert.True(t, output.Success)

	result, ok := output.Result.(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, 200, result["status_code"])
	assert.NotNil(t, result["body"])
	assert.NotNil(t, result["duration"])

	// Check JSON body parsing
	body, ok := result["body"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "success", body["message"])
}

// TestAPITool_POST tests POST requests
func TestAPITool_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "test data", body["data"])

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":456,"status":"created"}`))
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 5*time.Second, nil)
	ctx := context.Background()

	output, err := tool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method": "POST",
			"url":    "/create",
			"body": map[string]interface{}{
				"data": "test data",
			},
		},
		Context: ctx,
	})

	require.NoError(t, err)
	assert.True(t, output.Success)

	result, ok := output.Result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 201, result["status_code"])
}

// TestAPITool_PUT tests PUT requests
func TestAPITool_PUT(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"updated":true}`))
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 5*time.Second, nil)
	ctx := context.Background()

	output, err := tool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method": "PUT",
			"url":    "/update",
			"body":   map[string]interface{}{"name": "updated"},
		},
		Context: ctx,
	})

	require.NoError(t, err)
	assert.True(t, output.Success)
}

// TestAPITool_DELETE tests DELETE requests
func TestAPITool_DELETE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 5*time.Second, nil)
	ctx := context.Background()

	output, err := tool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method": "DELETE",
			"url":    "/delete/123",
		},
		Context: ctx,
	})

	require.NoError(t, err)
	assert.True(t, output.Success)
}

// TestAPITool_PATCH tests PATCH requests
func TestAPITool_PATCH(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PATCH", r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"patched":true}`))
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 5*time.Second, nil)
	ctx := context.Background()

	output, err := tool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method": "PATCH",
			"url":    "/patch",
			"body":   map[string]interface{}{"field": "value"},
		},
		Context: ctx,
	})

	require.NoError(t, err)
	assert.True(t, output.Success)
}

// TestAPITool_CustomHeaders tests custom headers
func TestAPITool_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom-Header"))
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	// Create tool with default headers
	defaultHeaders := map[string]string{
		"X-Custom-Header": "custom-value",
	}
	tool := NewAPITool(server.URL, 5*time.Second, defaultHeaders)
	ctx := context.Background()

	output, err := tool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method": "GET",
			"url":    "/test",
			"headers": map[string]interface{}{
				"Authorization": "Bearer token123",
			},
		},
		Context: ctx,
	})

	require.NoError(t, err)
	assert.True(t, output.Success)
}

// TestAPITool_Non2xxStatus tests non-2xx status codes
func TestAPITool_Non2xxStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"Bad Request", http.StatusBadRequest},
		{"Unauthorized", http.StatusUnauthorized},
		{"Not Found", http.StatusNotFound},
		{"Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"error":"something went wrong"}`))
			}))
			defer server.Close()

			tool := NewAPITool(server.URL, 5*time.Second, nil)
			ctx := context.Background()

			output, err := tool.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{
					"url": "/test",
				},
				Context: ctx,
			})

			require.Error(t, err)
			assert.False(t, output.Success)
			assert.Contains(t, output.Error, "failed with status")

			result, ok := output.Result.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, tt.statusCode, result["status_code"])
		})
	}
}

// TestAPITool_NonJSONResponse tests non-JSON responses
func TestAPITool_NonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Plain text response"))
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 5*time.Second, nil)
	ctx := context.Background()

	output, err := tool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": "/text",
		},
		Context: ctx,
	})

	require.NoError(t, err)
	assert.True(t, output.Success)

	result, ok := output.Result.(map[string]interface{})
	require.True(t, ok)

	// Should be returned as string
	body, ok := result["body"].(string)
	require.True(t, ok)
	assert.Equal(t, "Plain text response", body)
}

// TestAPITool_Timeout tests request timeout
func TestAPITool_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 100*time.Millisecond, nil)
	ctx := context.Background()

	output, err := tool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": "/slow",
		},
		Context: ctx,
	})

	require.Error(t, err)
	assert.False(t, output.Success)
	assert.Contains(t, output.Error, "http request failed")
}

// TestAPITool_CustomTimeout tests per-request timeout
func TestAPITool_CustomTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // Sleep longer than timeout
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 10*time.Second, nil)
	ctx := context.Background()

	output, err := tool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":     "/test",
			"timeout": 0.1, // 100ms - much shorter than server response time
		},
		Context: ctx,
	})

	require.Error(t, err)
	assert.False(t, output.Success)
	assert.Contains(t, output.Error, "context deadline exceeded")
}

// TestAPITool_ErrorCases tests error handling
func TestAPITool_ErrorCases(t *testing.T) {
	tool := NewAPITool("", 5*time.Second, nil)
	ctx := context.Background()

	tests := []struct {
		name           string
		args           map[string]interface{}
		expectedErrMsg string
	}{
		{
			name:           "Missing URL",
			args:           map[string]interface{}{},
			expectedErrMsg: "url is required",
		},
		{
			name: "Empty URL",
			args: map[string]interface{}{
				"url": "",
			},
			expectedErrMsg: "url is required",
		},
		{
			name: "Invalid URL type",
			args: map[string]interface{}{
				"url": 123,
			},
			expectedErrMsg: "url is required",
		},
		{
			name: "Invalid URL",
			args: map[string]interface{}{
				"url": "://invalid-url",
			},
			expectedErrMsg: "http request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := tool.Invoke(ctx, &interfaces.ToolInput{
				Args:    tt.args,
				Context: ctx,
			})

			require.Error(t, err)
			assert.False(t, output.Success)
			assert.Contains(t, output.Error, tt.expectedErrMsg)
		})
	}
}

// TestAPITool_ConvenienceMethods tests convenience methods
func TestAPITool_ConvenienceMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 5*time.Second, nil)
	ctx := context.Background()

	t.Run("Get method", func(t *testing.T) {
		output, err := tool.Get(ctx, "/test", nil)
		require.NoError(t, err)
		assert.True(t, output.Success)
	})

	t.Run("Post method", func(t *testing.T) {
		output, err := tool.Post(ctx, "/test", map[string]interface{}{"data": "value"}, nil)
		require.NoError(t, err)
		assert.True(t, output.Success)
	})

	t.Run("Put method", func(t *testing.T) {
		output, err := tool.Put(ctx, "/test", map[string]interface{}{"data": "value"}, nil)
		require.NoError(t, err)
		assert.True(t, output.Success)
	})

	t.Run("Delete method", func(t *testing.T) {
		output, err := tool.Delete(ctx, "/test", nil)
		require.NoError(t, err)
		assert.True(t, output.Success)
	})

	t.Run("Patch method", func(t *testing.T) {
		output, err := tool.Patch(ctx, "/test", map[string]interface{}{"data": "value"}, nil)
		require.NoError(t, err)
		assert.True(t, output.Success)
	})
}

// TestAPITool_AbsoluteURL tests absolute vs relative URL handling
func TestAPITool_AbsoluteURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tool := NewAPITool("https://base.example.com", 5*time.Second, nil)
	ctx := context.Background()

	// Absolute URL should override baseURL
	output, err := tool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL + "/absolute",
		},
		Context: ctx,
	})

	require.NoError(t, err)
	assert.True(t, output.Success)
}

// TestAPIToolBuilder tests API tool builder
func TestAPIToolBuilder(t *testing.T) {
	builder := NewAPIToolBuilder().
		WithBaseURL("https://api.example.com").
		WithTimeout(10*time.Second).
		WithHeader("X-Custom", "value").
		WithHeaders(map[string]string{"X-Another": "value2"}).
		WithAuth("token123")

	tool := builder.Build()

	assert.NotNil(t, tool)
	assert.Equal(t, "https://api.example.com", tool.baseURL)
	assert.Equal(t, 10*time.Second, tool.client.GetClient().Timeout)
	assert.Len(t, tool.headers, 3)
	assert.Equal(t, "value", tool.headers["X-Custom"])
	assert.Equal(t, "value2", tool.headers["X-Another"])
	assert.Equal(t, "Bearer token123", tool.headers["Authorization"])
}

// TestAPIToolBuilder_Defaults tests builder defaults
func TestAPIToolBuilder_Defaults(t *testing.T) {
	builder := NewAPIToolBuilder()
	tool := builder.Build()

	assert.NotNil(t, tool)
	assert.Equal(t, "", tool.baseURL)
	assert.Equal(t, 30*time.Second, tool.client.GetClient().Timeout)
	assert.Len(t, tool.headers, 0)
}

// BenchmarkAPITool_GET benchmarks GET requests
func BenchmarkAPITool_GET(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 5*time.Second, nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tool.Get(ctx, "/test", nil)
	}
}

// BenchmarkAPITool_POST benchmarks POST requests
func BenchmarkAPITool_POST(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tool := NewAPITool(server.URL, 5*time.Second, nil)
	ctx := context.Background()
	body := map[string]interface{}{"data": "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tool.Post(ctx, "/test", body, nil)
	}
}
