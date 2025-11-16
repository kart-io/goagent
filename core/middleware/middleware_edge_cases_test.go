package middleware

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===== MiddlewareChain Edge Cases =====

func TestMiddlewareChain_Execute_HandlerReturnsNilResponse(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return nil, nil // Handler returns nil response with no error
	}

	chain := NewMiddlewareChain(handler)

	req := &MiddlewareRequest{Input: "test"}
	resp, err := chain.Execute(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Output)
	assert.Greater(t, resp.Duration, time.Duration(0))
}

func TestMiddlewareChain_Execute_HandlerReturnsError(t *testing.T) {
	expectedErr := errors.New("handler failed")
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return nil, expectedErr
	}

	chain := NewMiddlewareChain(handler)
	req := &MiddlewareRequest{Input: "test"}

	_, err := chain.Execute(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestMiddlewareChain_Execute_MiddlewareShortCircuitsBeforePhase(t *testing.T) {
	var handlerCalled bool

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		handlerCalled = true
		return &MiddlewareResponse{Output: "result"}, nil
	}

	shortCircuitMW := NewMiddlewareFunc(
		"short-circuit",
		func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
			return nil, errors.New("short circuit")
		},
		nil,
		nil,
	)

	chain := NewMiddlewareChain(handler)
	chain.Use(shortCircuitMW)

	req := &MiddlewareRequest{Input: "test"}
	_, err := chain.Execute(context.Background(), req)

	require.Error(t, err)
	assert.False(t, handlerCalled)
}

func TestMiddlewareChain_Execute_ErrorSuppressedInOnBefore(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: "success"}, nil
	}

	// Middleware that generates and suppresses its own error
	suppressorMW := NewMiddlewareFunc(
		"suppressor",
		func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
			return nil, errors.New("generated error")
		},
		nil,
		func(ctx context.Context, err error) error {
			// Suppress the error
			return nil
		},
	)

	chain := NewMiddlewareChain(handler)
	chain.Use(suppressorMW)

	req := &MiddlewareRequest{Input: "test"}
	resp, err := chain.Execute(context.Background(), req)

	// Error was suppressed by the same middleware's OnError, so execution continues
	// But since request returned nil before suppression, handler never executes
	// The continue statement means we skip to next middleware (there is none)
	// So no error is returned
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestMiddlewareChain_Execute_ErrorInOnAfterPhase(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: "result"}, nil
	}

	errorInAfter := NewMiddlewareFunc(
		"error-in-after",
		nil,
		func(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error) {
			return nil, errors.New("error in after")
		},
		func(ctx context.Context, err error) error {
			// Suppress the error
			return nil
		},
	)

	chain := NewMiddlewareChain(handler)
	chain.Use(errorInAfter)

	req := &MiddlewareRequest{Input: "test"}
	_, err := chain.Execute(context.Background(), req)

	require.NoError(t, err)
}

func TestMiddlewareChain_Execute_MultipleErrorHandlers(t *testing.T) {
	var handledByCount int
	mu := &sync.Mutex{}

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return nil, errors.New("handler error")
	}

	mw1 := NewMiddlewareFunc(
		"mw1",
		nil,
		nil,
		func(ctx context.Context, err error) error {
			mu.Lock()
			handledByCount++
			mu.Unlock()
			return fmt.Errorf("handled-by-mw1: %w", err)
		},
	)

	mw2 := NewMiddlewareFunc(
		"mw2",
		nil,
		nil,
		func(ctx context.Context, err error) error {
			mu.Lock()
			handledByCount++
			mu.Unlock()
			return fmt.Errorf("handled-by-mw2: %w", err)
		},
	)

	chain := NewMiddlewareChain(handler)
	chain.Use(mw1, mw2)

	req := &MiddlewareRequest{Input: "test"}
	_, err := chain.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, 2, handledByCount)
}

func TestMiddlewareChain_Use_FluentInterface(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: "result"}, nil
	}

	chain := NewMiddlewareChain(handler).
		Use(NewBaseMiddleware("mw1")).
		Use(NewBaseMiddleware("mw2")).
		Use(NewBaseMiddleware("mw3"))

	assert.Equal(t, 3, len(chain.middlewares))
}

func TestMiddlewareChain_ConcurrentUse(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: "result"}, nil
	}

	chain := NewMiddlewareChain(handler)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			mw := NewBaseMiddleware(fmt.Sprintf("mw-%d", idx))
			chain.Use(mw)
		}(i)
	}

	wg.Wait()

	assert.Equal(t, 10, len(chain.middlewares))
}

// ===== Logging Middleware Edge Cases =====

func TestLoggingMiddleware_DefaultLogger(t *testing.T) {
	mw := NewLoggingMiddleware(nil)

	// Should use default logger (println) without panicking
	req := &MiddlewareRequest{Input: "test"}
	_, err := mw.OnBefore(context.Background(), req)
	assert.NoError(t, err)
}

func TestLoggingMiddleware_LogsComplexMetadata(t *testing.T) {
	var capturedLogs []string
	logger := func(msg string) {
		capturedLogs = append(capturedLogs, msg)
	}

	mw := NewLoggingMiddleware(logger)

	req := &MiddlewareRequest{
		Input: "test",
		Metadata: map[string]interface{}{
			"key1": "value1",
			"key2": 123,
			"key3": []string{"a", "b"},
		},
	}

	mw.OnBefore(context.Background(), req)

	assert.Len(t, capturedLogs, 1)
	assert.Contains(t, capturedLogs[0], "Request")
	assert.Contains(t, capturedLogs[0], "test")
}

// ===== Timing Middleware Edge Cases =====

func TestTimingMiddleware_OnAfter_NoMetadata(t *testing.T) {
	mw := NewTimingMiddleware()

	resp := &MiddlewareResponse{
		Output: "result",
		// No metadata
	}

	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	require.NoError(t, err)
	// Should create metadata if needed
	assert.NotNil(t, modifiedResp.Metadata)
}

func TestTimingMiddleware_OnAfter_MissingStartTime(t *testing.T) {
	mw := NewTimingMiddleware()

	resp := &MiddlewareResponse{
		Output:   "result",
		Metadata: make(map[string]interface{}),
		// No timing_start in metadata
	}

	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	require.NoError(t, err)
	// Should handle gracefully
	assert.NotContains(t, modifiedResp.Metadata, "timing_duration")
}

func TestTimingMiddleware_GetAverageLatency_Empty(t *testing.T) {
	mw := NewTimingMiddleware()

	// No timings recorded
	avgLatency := mw.GetAverageLatency()
	assert.Equal(t, time.Duration(0), avgLatency)
}

func TestTimingMiddleware_ConcurrentTimings(t *testing.T) {
	mw := NewTimingMiddleware()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &MiddlewareRequest{Metadata: make(map[string]interface{})}
			modifiedReq, _ := mw.OnBefore(context.Background(), req)

			time.Sleep(10 * time.Millisecond)

			resp := &MiddlewareResponse{
				Metadata: modifiedReq.Metadata,
			}
			mw.OnAfter(context.Background(), resp)
		}()
	}

	wg.Wait()

	timings := mw.GetTimings()
	assert.Equal(t, 10, len(timings))

	avgLatency := mw.GetAverageLatency()
	assert.Greater(t, avgLatency, time.Duration(0))
}

// ===== Cache Middleware Edge Cases =====

func TestCacheMiddleware_OnAfter_WithError(t *testing.T) {
	mw := NewCacheMiddleware(1 * time.Second)

	resp := &MiddlewareResponse{
		Output:   "result",
		Error:    errors.New("request error"),
		Metadata: map[string]interface{}{"original_input": "test"},
	}

	mw.OnAfter(context.Background(), resp)

	// Error responses should not be cached
	assert.Equal(t, 0, mw.Size())
}

func TestCacheMiddleware_GetCacheKeyFromResponse_NoMetadata(t *testing.T) {
	mw := NewCacheMiddleware(1 * time.Second)

	resp := &MiddlewareResponse{
		Output: "result",
		// No metadata
	}

	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	require.NoError(t, err)
	// Cache should still work, just with empty key
	assert.NotNil(t, modifiedResp)
}

func TestCacheMiddleware_GetCacheKeyFromResponse_NoOriginalInput(t *testing.T) {
	mw := NewCacheMiddleware(1 * time.Second)

	resp := &MiddlewareResponse{
		Output:   "result",
		Metadata: map[string]interface{}{"other_key": "value"},
	}

	mw.OnAfter(context.Background(), resp)

	// Cache key will be empty, but response is still cached with empty key
	assert.Equal(t, 1, mw.Size())
}

func TestCacheMiddleware_OnBefore_NilMetadata(t *testing.T) {
	mw := NewCacheMiddleware(1 * time.Second)

	// First populate cache
	resp := &MiddlewareResponse{
		Output:   "result",
		Metadata: map[string]interface{}{"original_input": "test"},
	}
	mw.OnAfter(context.Background(), resp)

	// Now try to get from cache with nil metadata
	req := &MiddlewareRequest{
		Input: "test",
		// No metadata
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	// Should create metadata
	assert.NotNil(t, modifiedReq.Metadata)
}

// ===== Retry Middleware Edge Cases =====

func TestRetryMiddleware_WithNilRetryCondition(t *testing.T) {
	mw := NewRetryMiddleware(3, 100*time.Millisecond)

	// Default retry condition should be used
	err := errors.New("test error")
	handledErr := mw.OnError(context.Background(), err)
	assert.Contains(t, handledErr.Error(), "retry needed")
}

func TestRetryMiddleware_OnError_WithNilError(t *testing.T) {
	mw := NewRetryMiddleware(3, 100*time.Millisecond)

	// Nil error should not trigger retry
	handledErr := mw.OnError(context.Background(), nil)
	assert.Nil(t, handledErr)
}

// ===== MiddlewareRequest & Response Edge Cases =====

func TestMiddlewareRequest_NilState(t *testing.T) {
	req := &MiddlewareRequest{
		Input:    "test",
		State:    nil,
		Metadata: make(map[string]interface{}),
	}

	assert.Nil(t, req.State)
	assert.Equal(t, "test", req.Input)
}

func TestMiddlewareResponse_ZeroDuration(t *testing.T) {
	resp := &MiddlewareResponse{
		Output:   "result",
		Duration: 0,
	}

	assert.Equal(t, time.Duration(0), resp.Duration)
}

func TestMiddlewareFunc_WithNilFunctions(t *testing.T) {
	mw := NewMiddlewareFunc("test", nil, nil, nil)

	req := &MiddlewareRequest{Input: "test"}
	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, req, modifiedReq)

	resp := &MiddlewareResponse{Output: "result"}
	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	require.NoError(t, err)
	assert.Equal(t, resp, modifiedResp)

	testErr := errors.New("test error")
	handledErr := mw.OnError(context.Background(), testErr)
	assert.Equal(t, testErr, handledErr)
}

func TestMiddlewareFunc_OnError_PartialNil(t *testing.T) {
	mw := NewMiddlewareFunc(
		"test",
		func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
			return req, nil
		},
		nil,
		nil,
	)

	testErr := errors.New("test")
	handledErr := mw.OnError(context.Background(), testErr)
	assert.Equal(t, testErr, handledErr)
}

// ===== Race Condition Tests =====

func TestMiddlewareChain_RaceCondition_UseAndExecute(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: "result"}, nil
	}

	chain := NewMiddlewareChain(handler)

	done := make(chan bool)

	// Goroutine that adds middleware
	go func() {
		for i := 0; i < 5; i++ {
			mw := NewBaseMiddleware(fmt.Sprintf("mw-%d", i))
			chain.Use(mw)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine that executes
	go func() {
		for i := 0; i < 5; i++ {
			req := &MiddlewareRequest{Input: fmt.Sprintf("request-%d", i)}
			chain.Execute(context.Background(), req)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	<-done
	<-done
}

func TestRateLimiterMiddleware_RaceCondition_ConcurrentRequests(t *testing.T) {
	mw := NewRateLimiterMiddleware(100, 1*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &MiddlewareRequest{
				Input:    fmt.Sprintf("request-%d", idx),
				Metadata: make(map[string]interface{}),
			}
			mw.OnBefore(context.Background(), req)
		}(i)
	}

	wg.Wait()
}

func TestCircuitBreakerMiddleware_RaceCondition_ConcurrentErrors(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(10, 1*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mw.OnError(context.Background(), errors.New("test error"))
		}()
	}

	wg.Wait()
}

// ===== Context Handling Tests =====

func TestMiddlewareChain_Execute_WithCancelledContext(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: "result"}, nil
	}

	chain := NewMiddlewareChain(handler)
	req := &MiddlewareRequest{Input: "test"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	resp, err := chain.Execute(ctx, req)
	// Should still execute because middleware doesn't check context
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestRandomDelayMiddleware_OnBefore_ImmediateCancel(t *testing.T) {
	mw := NewRandomDelayMiddleware(100*time.Millisecond, 500*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &MiddlewareRequest{Input: "test"}
	_, err := mw.OnBefore(ctx, req)

	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

// ===== Type Assertion Edge Cases =====

func TestCacheMiddleware_OnAfter_InvalidCachedResponseType(t *testing.T) {
	mw := NewCacheMiddleware(1 * time.Second)

	resp := &MiddlewareResponse{
		Output: "result",
		Metadata: map[string]interface{}{
			"cached_response": "not a response", // Wrong type
		},
	}

	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	require.NoError(t, err)
	// Should handle gracefully and cache the response
	assert.NotNil(t, modifiedResp)
}

func TestTimingMiddleware_OnAfter_InvalidStartTimeType(t *testing.T) {
	mw := NewTimingMiddleware()

	resp := &MiddlewareResponse{
		Output: "result",
		Metadata: map[string]interface{}{
			"timing_start": "not a time", // Wrong type
		},
	}

	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	require.NoError(t, err)
	// Should handle gracefully
	assert.NotContains(t, modifiedResp.Metadata, "timing_duration")
}

// ===== Large Input/Output Handling =====

func TestMiddlewareChain_Execute_LargePayload(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: req.Input}, nil
	}

	chain := NewMiddlewareChain(handler)

	// Create a large input
	largeInput := make([]byte, 1<<20) // 1MB
	for i := range largeInput {
		largeInput[i] = byte(i % 256)
	}

	req := &MiddlewareRequest{Input: string(largeInput)}
	resp, err := chain.Execute(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// ===== Middleware Chain Stability Tests =====

func TestMiddlewareChain_LongChain(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: req.Input}, nil
	}

	chain := NewMiddlewareChain(handler)

	// Add many middleware
	for i := 0; i < 50; i++ {
		mw := NewBaseMiddleware(fmt.Sprintf("mw-%d", i))
		chain.Use(mw)
	}

	req := &MiddlewareRequest{Input: "test"}
	resp, err := chain.Execute(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, "test", resp.Output)
}

func TestMiddlewareChain_ConcurrentExecutions_WithModifications(t *testing.T) {
	var requestLogs []string
	mu := &sync.Mutex{}

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		mu.Lock()
		requestLogs = append(requestLogs, fmt.Sprintf("%v", req.Input))
		mu.Unlock()
		return &MiddlewareResponse{Output: req.Input}, nil
	}

	modifyingMW := NewMiddlewareFunc(
		"modifier",
		func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
			req.Input = fmt.Sprintf("modified-%v", req.Input)
			return req, nil
		},
		nil,
		nil,
	)

	chain := NewMiddlewareChain(handler)
	chain.Use(modifyingMW)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &MiddlewareRequest{Input: fmt.Sprintf("request-%d", idx)}
			resp, err := chain.Execute(context.Background(), req)
			require.NoError(t, err)
			assert.NotNil(t, resp)
		}(i)
	}

	wg.Wait()
	assert.Equal(t, 10, len(requestLogs))
}
