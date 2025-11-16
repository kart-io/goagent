package observability

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDistributedTracer(t *testing.T) {
	tracer := NewDistributedTracer()
	assert.NotNil(t, tracer)
	assert.NotNil(t, tracer.tracer)
	assert.NotNil(t, tracer.propagator)
}

func TestDistributedTracer_InjectContext(t *testing.T) {
	tracer := NewDistributedTracer()

	tests := []struct {
		name    string
		headers map[string]string
	}{
		{
			name:    "empty headers",
			headers: make(map[string]string),
		},
		{
			name: "existing headers",
			headers: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer token",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpHeader := http.Header{}
			for k, v := range tt.headers {
				httpHeader.Set(k, v)
			}
			carrier := NewHTTPCarrier(httpHeader)

			ctx := context.Background()
			err := tracer.InjectContext(ctx, carrier)

			assert.NoError(t, err)
			assert.NotNil(t, carrier)
		})
	}
}

func TestDistributedTracer_ExtractContext(t *testing.T) {
	tracer := NewDistributedTracer()

	ctx := context.Background()
	carrier := NewMessageCarrier(nil)

	// Inject first
	err := tracer.InjectContext(ctx, carrier)
	require.NoError(t, err)

	// Then extract
	extractedCtx := tracer.ExtractContext(context.Background(), carrier)
	assert.NotNil(t, extractedCtx)
}

func TestDistributedTracer_StartRemoteSpan(t *testing.T) {
	tracer := NewDistributedTracer()

	ctx := context.Background()
	carrier := NewMessageCarrier(nil)

	err := tracer.InjectContext(ctx, carrier)
	require.NoError(t, err)

	newCtx, span := tracer.StartRemoteSpan(context.Background(), "remote.operation", carrier)

	assert.NotNil(t, newCtx)
	assert.NotNil(t, span)
	span.End()
}

func TestHTTPCarrier_Get(t *testing.T) {
	header := http.Header{}
	header.Set("X-Custom-Header", "custom-value")
	header.Set("Content-Type", "application/json")

	carrier := NewHTTPCarrier(header)

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "existing key",
			key:      "X-Custom-Header",
			expected: "custom-value",
		},
		{
			name:     "standard header",
			key:      "Content-Type",
			expected: "application/json",
		},
		{
			name:     "non-existing key",
			key:      "X-Non-Existent",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := carrier.Get(tt.key)
			assert.Equal(t, tt.expected, value)
		})
	}
}

func TestHTTPCarrier_Set(t *testing.T) {
	header := http.Header{}
	carrier := NewHTTPCarrier(header)

	carrier.Set("X-Test", "test-value")
	carrier.Set("X-Another", "another-value")

	assert.Equal(t, "test-value", carrier.Get("X-Test"))
	assert.Equal(t, "another-value", carrier.Get("X-Another"))
}

func TestHTTPCarrier_Keys(t *testing.T) {
	header := http.Header{}
	header.Set("X-Header-1", "value1")
	header.Set("X-Header-2", "value2")
	header.Set("Content-Type", "application/json")

	carrier := NewHTTPCarrier(header)
	keys := carrier.Keys()

	assert.NotEmpty(t, keys)
	assert.True(t, len(keys) >= 3)
}

func TestHTTPCarrier_RoundTrip(t *testing.T) {
	header := http.Header{}
	carrier := NewHTTPCarrier(header)

	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	// Set values
	for k, v := range testData {
		carrier.Set(k, v)
	}

	// Get values back
	for k, expected := range testData {
		actual := carrier.Get(k)
		assert.Equal(t, expected, actual)
	}
}

func TestMessageCarrier_Create_NilMetadata(t *testing.T) {
	carrier := NewMessageCarrier(nil)
	assert.NotNil(t, carrier)
	assert.NotNil(t, carrier.metadata)
	assert.Empty(t, carrier.metadata)
}

func TestMessageCarrier_Create_WithMetadata(t *testing.T) {
	metadata := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	carrier := NewMessageCarrier(metadata)
	assert.NotNil(t, carrier)
	assert.Equal(t, metadata, carrier.metadata)
}

func TestMessageCarrier_Get(t *testing.T) {
	metadata := map[string]string{
		"trace-id": "abc123",
		"span-id":  "def456",
	}

	carrier := NewMessageCarrier(metadata)

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "existing key",
			key:      "trace-id",
			expected: "abc123",
		},
		{
			name:     "another existing key",
			key:      "span-id",
			expected: "def456",
		},
		{
			name:     "non-existing key",
			key:      "non-existent",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := carrier.Get(tt.key)
			assert.Equal(t, tt.expected, value)
		})
	}
}

func TestMessageCarrier_Set(t *testing.T) {
	carrier := NewMessageCarrier(nil)

	carrier.Set("key1", "value1")
	carrier.Set("key2", "value2")
	carrier.Set("key3", "value3")

	assert.Equal(t, "value1", carrier.Get("key1"))
	assert.Equal(t, "value2", carrier.Get("key2"))
	assert.Equal(t, "value3", carrier.Get("key3"))
}

func TestMessageCarrier_Keys(t *testing.T) {
	metadata := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	carrier := NewMessageCarrier(metadata)
	keys := carrier.Keys()

	assert.Len(t, keys, 3)
	for _, k := range keys {
		assert.Contains(t, metadata, k)
	}
}

func TestMessageCarrier_GetMetadata(t *testing.T) {
	metadata := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	carrier := NewMessageCarrier(metadata)
	retrieved := carrier.GetMetadata()

	assert.Equal(t, metadata, retrieved)
}

func TestMessageCarrier_RoundTrip(t *testing.T) {
	carrier := NewMessageCarrier(nil)

	testData := map[string]string{
		"trace-id":  "abc123def456",
		"span-id":   "xyz789",
		"parent-id": "parent123",
		"custom":    "data",
	}

	// Set values
	for k, v := range testData {
		carrier.Set(k, v)
	}

	// Verify with Get
	for k, expected := range testData {
		actual := carrier.Get(k)
		assert.Equal(t, expected, actual)
	}

	// Verify with GetMetadata
	metadata := carrier.GetMetadata()
	for k, expected := range testData {
		assert.Equal(t, expected, metadata[k])
	}
}

func TestNewCrossServiceTracer(t *testing.T) {
	tracer := NewCrossServiceTracer("my-service")
	assert.NotNil(t, tracer)
	assert.Equal(t, "my-service", tracer.serviceName)
	assert.NotNil(t, tracer.tracer)
}

func TestCrossServiceTracer_TraceHTTPRequest(t *testing.T) {
	tracer := NewCrossServiceTracer("test-service")

	tests := []struct {
		name   string
		method string
		url    string
	}{
		{
			name:   "GET request",
			method: http.MethodGet,
			url:    "http://example.com/api/v1/data",
		},
		{
			name:   "POST request",
			method: http.MethodPost,
			url:    "http://example.com/api/v1/create",
		},
		{
			name:   "PUT request",
			method: http.MethodPut,
			url:    "http://example.com/api/v1/update",
		},
		{
			name:   "DELETE request",
			method: http.MethodDelete,
			url:    "http://example.com/api/v1/delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, tt.url, nil)
			require.NoError(t, err)

			ctx := context.Background()
			newCtx, span := tracer.TraceHTTPRequest(ctx, req)

			assert.NotNil(t, newCtx)
			assert.NotNil(t, span)
			span.End()
		})
	}
}

func TestCrossServiceTracer_TraceHTTPResponse(t *testing.T) {
	tracer := NewCrossServiceTracer("test-service")

	tests := []struct {
		name       string
		statusCode int
	}{
		{
			name:       "success response",
			statusCode: http.StatusOK,
		},
		{
			name:       "created response",
			statusCode: http.StatusCreated,
		},
		{
			name:       "bad request",
			statusCode: http.StatusBadRequest,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
		},
		{
			name:       "internal error",
			statusCode: http.StatusInternalServerError,
		},
		{
			name:       "service unavailable",
			statusCode: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, span := tracer.tracer.tracer.Start(ctx, "http.request")
			ctx = context.WithValue(context.Background(), "span", span)

			resp := &http.Response{
				StatusCode: tt.statusCode,
			}

			err := tracer.TraceHTTPResponse(ctx, resp)
			assert.NoError(t, err)
			span.End()
		})
	}
}

func TestCrossServiceTracer_TraceMessage(t *testing.T) {
	tracer := NewCrossServiceTracer("test-service")

	tests := []struct {
		name    string
		topic   string
		message []byte
	}{
		{
			name:    "simple message",
			topic:   "events",
			message: []byte("test message"),
		},
		{
			name:    "JSON message",
			topic:   "data",
			message: []byte(`{"key": "value"}`),
		},
		{
			name:    "empty message",
			topic:   "notifications",
			message: []byte(""),
		},
		{
			name:    "large message",
			topic:   "logs",
			message: []byte("a long message with lots of content and details about what happened in the system"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			newCtx, span, carrier := tracer.TraceMessage(ctx, tt.topic, tt.message)

			assert.NotNil(t, newCtx)
			assert.NotNil(t, span)
			assert.NotNil(t, carrier)

			span.End()
		})
	}
}

func TestCrossServiceTracer_HTTPWorkflow(t *testing.T) {
	tracer := NewCrossServiceTracer("workflow-service")

	// Create HTTP request
	req, err := http.NewRequest(http.MethodPost, "http://api.example.com/process", nil)
	require.NoError(t, err)

	ctx := context.Background()

	// Trace the request
	ctx, reqSpan := tracer.TraceHTTPRequest(ctx, req)

	// Simulate response
	resp := &http.Response{
		StatusCode: http.StatusOK,
	}

	// Trace the response
	err = tracer.TraceHTTPResponse(ctx, resp)
	assert.NoError(t, err)

	reqSpan.End()
}

func TestCrossServiceTracer_MessageWorkflow(t *testing.T) {
	tracer := NewCrossServiceTracer("messaging-service")

	ctx := context.Background()

	// Trace message send
	newCtx, span, carrier := tracer.TraceMessage(ctx, "events", []byte("test event"))

	assert.NotNil(t, newCtx)
	assert.NotNil(t, span)
	assert.NotNil(t, carrier)

	// Metadata should be populated
	metadata := carrier.GetMetadata()
	assert.NotNil(t, metadata)

	span.End()
}

func TestHTTPCarrier_MultipleOperations(t *testing.T) {
	header := http.Header{}
	carrier := NewHTTPCarrier(header)

	// Set multiple headers
	for i := 0; i < 10; i++ {
		key := "X-Header-" + string(rune('0'+i))
		value := "value-" + string(rune('0'+i))
		carrier.Set(key, value)
	}

	// Verify all are set
	keys := carrier.Keys()
	assert.Len(t, keys, 10)

	// Verify retrieval
	for i := 0; i < 10; i++ {
		key := "X-Header-" + string(rune('0'+i))
		value := carrier.Get(key)
		assert.NotEmpty(t, value)
	}
}

func TestMessageCarrier_OverwriteValues(t *testing.T) {
	carrier := NewMessageCarrier(nil)

	carrier.Set("key", "value1")
	assert.Equal(t, "value1", carrier.Get("key"))

	carrier.Set("key", "value2")
	assert.Equal(t, "value2", carrier.Get("key"))

	carrier.Set("key", "value3")
	assert.Equal(t, "value3", carrier.Get("key"))
}

func TestDistributedTracer_ContextPropagationChain(t *testing.T) {
	tracer := NewDistributedTracer()

	ctx1 := context.Background()
	carrier1 := NewMessageCarrier(nil)

	// Inject into first carrier
	err := tracer.InjectContext(ctx1, carrier1)
	require.NoError(t, err)

	// Extract to create context for second operation
	ctx2 := tracer.ExtractContext(context.Background(), carrier1)
	carrier2 := NewMessageCarrier(nil)

	// Inject from second context
	err = tracer.InjectContext(ctx2, carrier2)
	require.NoError(t, err)

	// Both carriers should have propagation data
	assert.NotNil(t, carrier1.GetMetadata())
	assert.NotNil(t, carrier2.GetMetadata())
}

func BenchmarkDistributedTracer_InjectContext(b *testing.B) {
	tracer := NewDistributedTracer()
	ctx := context.Background()
	carrier := NewMessageCarrier(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracer.InjectContext(ctx, carrier)
	}
}

func BenchmarkDistributedTracer_ExtractContext(b *testing.B) {
	tracer := NewDistributedTracer()
	ctx := context.Background()
	carrier := NewMessageCarrier(nil)
	_ = tracer.InjectContext(ctx, carrier)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracer.ExtractContext(context.Background(), carrier)
	}
}

func BenchmarkHTTPCarrier_Get(b *testing.B) {
	header := http.Header{}
	header.Set("X-Test", "value")
	carrier := NewHTTPCarrier(header)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = carrier.Get("X-Test")
	}
}

func BenchmarkMessageCarrier_Set(b *testing.B) {
	carrier := NewMessageCarrier(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		carrier.Set("key", "value")
	}
}

func BenchmarkCrossServiceTracer_TraceMessage(b *testing.B) {
	tracer := NewCrossServiceTracer("bench-service")
	ctx := context.Background()
	msg := []byte("benchmark message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, span, _ := tracer.TraceMessage(ctx, "bench", msg)
		span.End()
	}
}
