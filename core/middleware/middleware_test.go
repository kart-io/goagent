package middleware

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/state"
)

// Alias for convenience in tests
var NewAgentState = state.NewAgentState

func TestNewMiddlewareChain(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: "result"}, nil
	}

	chain := NewMiddlewareChain(handler)
	require.NotNil(t, chain)
	assert.NotNil(t, chain.handler)
	assert.Empty(t, chain.middlewares)
}

func TestMiddlewareChain_Use(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: "result"}, nil
	}

	chain := NewMiddlewareChain(handler)

	mw1 := NewBaseMiddleware("mw1")
	mw2 := NewBaseMiddleware("mw2")

	chain.Use(mw1, mw2)
	assert.Equal(t, 2, len(chain.middlewares))
}

func TestMiddlewareChain_Execute(t *testing.T) {
	var executionOrder []string
	mu := &sync.Mutex{}

	recordExecution := func(name string) {
		mu.Lock()
		executionOrder = append(executionOrder, name)
		mu.Unlock()
	}

	// Main handler
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		recordExecution("handler")
		return &MiddlewareResponse{
			Output: "result",
			State:  req.State,
		}, nil
	}

	// Middleware that records execution
	createRecordingMiddleware := func(name string) Middleware {
		return NewMiddlewareFunc(
			name,
			func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
				recordExecution(name + "-before")
				return req, nil
			},
			func(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error) {
				recordExecution(name + "-after")
				return resp, nil
			},
			nil,
		)
	}

	chain := NewMiddlewareChain(handler)
	chain.Use(
		createRecordingMiddleware("mw1"),
		createRecordingMiddleware("mw2"),
		createRecordingMiddleware("mw3"),
	)

	request := &MiddlewareRequest{
		Input:    "test",
		Metadata: make(map[string]interface{}),
	}

	resp, err := chain.Execute(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "result", resp.Output)

	// Verify execution order: before hooks in order, handler, after hooks in reverse
	expected := []string{
		"mw1-before",
		"mw2-before",
		"mw3-before",
		"handler",
		"mw3-after",
		"mw2-after",
		"mw1-after",
	}
	assert.Equal(t, expected, executionOrder)
}

func TestMiddlewareChain_ErrorHandling(t *testing.T) {
	expectedErr := errors.New("handler error")

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return nil, expectedErr
	}

	// Middleware that handles errors
	errorHandler := NewMiddlewareFunc(
		"error-handler",
		nil,
		nil,
		func(ctx context.Context, err error) error {
			// Wrap the error
			return fmt.Errorf("handled: %w", err)
		},
	)

	chain := NewMiddlewareChain(handler)
	chain.Use(errorHandler)

	request := &MiddlewareRequest{Input: "test"}
	_, err := chain.Execute(context.Background(), request)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handled")
	assert.Contains(t, err.Error(), expectedErr.Error())
}

func TestBaseMiddleware(t *testing.T) {
	mw := NewBaseMiddleware("test-middleware")

	assert.Equal(t, "test-middleware", mw.Name())

	// Test default no-op behaviors
	req := &MiddlewareRequest{Input: "test"}
	modifiedReq, err := mw.OnBefore(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, req, modifiedReq)

	resp := &MiddlewareResponse{Output: "result"}
	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	assert.NoError(t, err)
	assert.Equal(t, resp, modifiedResp)

	testErr := errors.New("test error")
	handledErr := mw.OnError(context.Background(), testErr)
	assert.Equal(t, testErr, handledErr)
}

func TestMiddlewareFunc(t *testing.T) {
	var beforeCalled, afterCalled, errorCalled bool

	mw := NewMiddlewareFunc(
		"test-func",
		func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
			beforeCalled = true
			req.Metadata = map[string]interface{}{"modified": true}
			return req, nil
		},
		func(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error) {
			afterCalled = true
			resp.Metadata = map[string]interface{}{"processed": true}
			return resp, nil
		},
		func(ctx context.Context, err error) error {
			errorCalled = true
			return fmt.Errorf("wrapped: %w", err)
		},
	)

	// Test OnBefore
	req := &MiddlewareRequest{Input: "test"}
	modifiedReq, err := mw.OnBefore(context.Background(), req)
	assert.NoError(t, err)
	assert.True(t, beforeCalled)
	assert.Equal(t, true, modifiedReq.Metadata["modified"])

	// Test OnAfter
	resp := &MiddlewareResponse{Output: "result"}
	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	assert.NoError(t, err)
	assert.True(t, afterCalled)
	assert.Equal(t, true, modifiedResp.Metadata["processed"])

	// Test OnError
	testErr := errors.New("test error")
	handledErr := mw.OnError(context.Background(), testErr)
	assert.True(t, errorCalled)
	assert.Contains(t, handledErr.Error(), "wrapped")
}

func TestLoggingMiddleware(t *testing.T) {
	var logs []string
	logger := func(msg string) {
		logs = append(logs, msg)
	}

	mw := NewLoggingMiddleware(logger)

	// Test logging request
	req := &MiddlewareRequest{
		Input:    "test input",
		Metadata: map[string]interface{}{"key": "value"},
	}
	_, err := mw.OnBefore(context.Background(), req)
	assert.NoError(t, err)
	assert.Len(t, logs, 1)
	assert.Contains(t, logs[0], "Request")
	assert.Contains(t, logs[0], "test input")

	// Test logging response
	resp := &MiddlewareResponse{
		Output:   "test output",
		Duration: 100 * time.Millisecond,
	}
	_, err = mw.OnAfter(context.Background(), resp)
	assert.NoError(t, err)
	assert.Len(t, logs, 2)
	assert.Contains(t, logs[1], "Response")
	assert.Contains(t, logs[1], "test output")
	assert.Contains(t, logs[1], "100ms")

	// Test logging error
	testErr := errors.New("test error")
	handledErr := mw.OnError(context.Background(), testErr)
	assert.Equal(t, testErr, handledErr)
	assert.Len(t, logs, 3)
	assert.Contains(t, logs[2], "Error")
	assert.Contains(t, logs[2], "test error")
}

func TestTimingMiddleware(t *testing.T) {
	mw := NewTimingMiddleware()

	// Test timing tracking
	req := &MiddlewareRequest{Input: "test"}
	modifiedReq, err := mw.OnBefore(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, modifiedReq.Metadata["timing_start"])

	// Simulate some processing time
	time.Sleep(50 * time.Millisecond)

	resp := &MiddlewareResponse{
		Metadata: modifiedReq.Metadata, // Pass metadata from request
	}
	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	assert.NoError(t, err)
	assert.NotNil(t, modifiedResp.Metadata["timing_duration"])

	// Check recorded timings
	timings := mw.GetTimings()
	assert.Greater(t, len(timings), 0)

	// Check average latency
	avgLatency := mw.GetAverageLatency()
	assert.Greater(t, avgLatency, time.Duration(0))
}

func TestRetryMiddleware(t *testing.T) {
	mw := NewRetryMiddleware(3, 100*time.Millisecond)

	// Test default retry condition (retry on all errors)
	testErr := errors.New("test error")
	handledErr := mw.OnError(context.Background(), testErr)
	assert.Contains(t, handledErr.Error(), "retry needed")

	// Test custom retry condition
	mw.WithRetryCondition(func(err error) bool {
		return errors.Is(err, context.DeadlineExceeded)
	})

	// Should not retry for non-deadline errors
	handledErr = mw.OnError(context.Background(), testErr)
	assert.Equal(t, testErr, handledErr)

	// Should retry for deadline errors
	deadlineErr := context.DeadlineExceeded
	handledErr = mw.OnError(context.Background(), deadlineErr)
	assert.Contains(t, handledErr.Error(), "retry needed")
}

func TestCacheMiddleware(t *testing.T) {
	mw := NewCacheMiddleware(1 * time.Second)

	// First request (cache miss)
	req1 := &MiddlewareRequest{
		Input:    "test-key",
		Metadata: make(map[string]interface{}),
	}
	modifiedReq1, err := mw.OnBefore(context.Background(), req1)
	assert.NoError(t, err)
	assert.NotContains(t, modifiedReq1.Metadata, "cache_hit")

	// Cache the response
	resp1 := &MiddlewareResponse{
		Output:   "cached result",
		Metadata: map[string]interface{}{"original_input": "test-key"},
	}
	_, err = mw.OnAfter(context.Background(), resp1)
	assert.NoError(t, err)
	assert.Equal(t, 1, mw.Size())

	// Second request (cache hit)
	req2 := &MiddlewareRequest{
		Input:    "test-key",
		Metadata: make(map[string]interface{}),
	}
	modifiedReq2, err := mw.OnBefore(context.Background(), req2)
	assert.NoError(t, err)
	assert.True(t, modifiedReq2.Metadata["cache_hit"].(bool))
	assert.NotNil(t, modifiedReq2.Metadata["cached_response"])

	// Clear cache
	mw.Clear()
	assert.Equal(t, 0, mw.Size())
}

func TestCacheMiddleware_Expiration(t *testing.T) {
	mw := NewCacheMiddleware(100 * time.Millisecond)

	// Cache a response
	req := &MiddlewareRequest{
		Input:    "test-key",
		Metadata: make(map[string]interface{}),
	}
	_, _ = mw.OnBefore(context.Background(), req)

	resp := &MiddlewareResponse{
		Output:   "cached result",
		Metadata: map[string]interface{}{"original_input": "test-key"},
	}
	_, _ = mw.OnAfter(context.Background(), resp)

	// Immediate check (should be cached)
	req2 := &MiddlewareRequest{
		Input:    "test-key",
		Metadata: make(map[string]interface{}),
	}
	modifiedReq2, _ := mw.OnBefore(context.Background(), req2)
	assert.True(t, modifiedReq2.Metadata["cache_hit"].(bool))

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Check again (should be expired)
	req3 := &MiddlewareRequest{
		Input:    "test-key",
		Metadata: make(map[string]interface{}),
	}
	modifiedReq3, _ := mw.OnBefore(context.Background(), req3)
	assert.NotContains(t, modifiedReq3.Metadata, "cache_hit")
}

func TestMiddlewareChain_ConcurrentExecution(t *testing.T) {
	var counter int32

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		atomic.AddInt32(&counter, 1)
		return &MiddlewareResponse{
			Output: fmt.Sprintf("result-%d", atomic.LoadInt32(&counter)),
		}, nil
	}

	// Middleware that adds delay
	delayMiddleware := NewMiddlewareFunc(
		"delay",
		func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
			time.Sleep(10 * time.Millisecond)
			return req, nil
		},
		nil,
		nil,
	)

	chain := NewMiddlewareChain(handler)
	chain.Use(delayMiddleware)

	// Execute multiple requests concurrently
	var wg sync.WaitGroup
	numRequests := 10
	errors := make([]error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &MiddlewareRequest{
				Input: fmt.Sprintf("request-%d", idx),
			}
			_, err := chain.Execute(context.Background(), req)
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// Check all requests succeeded
	for _, err := range errors {
		assert.NoError(t, err)
	}

	// Check counter
	assert.Equal(t, int32(numRequests), atomic.LoadInt32(&counter))
}

func TestMiddlewareRequest(t *testing.T) {
	state := NewAgentState()
	state.Set("key", "value")

	req := &MiddlewareRequest{
		Input:     "test input",
		State:     state,
		Runtime:   "runtime",
		Metadata:  map[string]interface{}{"meta": "data"},
		Headers:   map[string]string{"header": "value"},
		Timestamp: time.Now(),
	}

	assert.Equal(t, "test input", req.Input)
	assert.NotNil(t, req.State)
	val, ok := req.State.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
	assert.Equal(t, "runtime", req.Runtime)
	assert.Equal(t, "data", req.Metadata["meta"])
	assert.Equal(t, "value", req.Headers["header"])
	assert.NotZero(t, req.Timestamp)
}

func TestMiddlewareResponse(t *testing.T) {
	state := NewAgentState()
	testErr := errors.New("test error")

	resp := &MiddlewareResponse{
		Output:   "test output",
		State:    state,
		Metadata: map[string]interface{}{"meta": "data"},
		Headers:  map[string]string{"header": "value"},
		Duration: 100 * time.Millisecond,
		Error:    testErr,
	}

	assert.Equal(t, "test output", resp.Output)
	assert.NotNil(t, resp.State)
	assert.Equal(t, "data", resp.Metadata["meta"])
	assert.Equal(t, "value", resp.Headers["header"])
	assert.Equal(t, 100*time.Millisecond, resp.Duration)
	assert.Equal(t, testErr, resp.Error)
}

func TestMiddlewareChain_ModifyRequestResponse(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		// Echo back the input
		return &MiddlewareResponse{
			Output: req.Input,
			State:  req.State,
		}, nil
	}

	// Middleware that modifies request
	requestModifier := NewMiddlewareFunc(
		"request-modifier",
		func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
			// Modify input
			req.Input = fmt.Sprintf("modified-%v", req.Input)
			// Add metadata
			if req.Metadata == nil {
				req.Metadata = make(map[string]interface{})
			}
			req.Metadata["modified_by"] = "request-modifier"
			return req, nil
		},
		nil,
		nil,
	)

	// Middleware that modifies response
	responseModifier := NewMiddlewareFunc(
		"response-modifier",
		nil,
		func(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error) {
			// Modify output
			resp.Output = fmt.Sprintf("wrapped-%v", resp.Output)
			// Add metadata
			if resp.Metadata == nil {
				resp.Metadata = make(map[string]interface{})
			}
			resp.Metadata["wrapped_by"] = "response-modifier"
			return resp, nil
		},
		nil,
	)

	chain := NewMiddlewareChain(handler)
	chain.Use(requestModifier, responseModifier)

	req := &MiddlewareRequest{
		Input: "original",
	}

	resp, err := chain.Execute(context.Background(), req)
	require.NoError(t, err)

	// Check modifications
	assert.Equal(t, "wrapped-modified-original", resp.Output)
	assert.Equal(t, "response-modifier", resp.Metadata["wrapped_by"])
}

func BenchmarkMiddlewareChain_Execute(b *testing.B) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: "result"}, nil
	}

	chain := NewMiddlewareChain(handler)
	chain.Use(
		NewLoggingMiddleware(nil),
		NewTimingMiddleware(),
		NewCacheMiddleware(1*time.Minute),
	)

	req := &MiddlewareRequest{
		Input:    "test",
		Metadata: make(map[string]interface{}),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chain.Execute(context.Background(), req)
	}
}

func BenchmarkCacheMiddleware(b *testing.B) {
	mw := NewCacheMiddleware(1 * time.Minute)

	req := &MiddlewareRequest{
		Input:    "test",
		Metadata: make(map[string]interface{}),
	}

	// Populate cache
	mw.OnBefore(context.Background(), req)
	resp := &MiddlewareResponse{
		Output:   "cached",
		Metadata: map[string]interface{}{"original_input": "test"},
	}
	mw.OnAfter(context.Background(), resp)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mw.OnBefore(context.Background(), req)
	}
}
