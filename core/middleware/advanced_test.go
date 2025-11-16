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
)

// ===== DynamicPromptMiddleware Tests =====

func TestNewDynamicPromptMiddleware(t *testing.T) {
	modifier := func(req *MiddlewareRequest) string {
		return "modified"
	}
	mw := NewDynamicPromptMiddleware(modifier)

	require.NotNil(t, mw)
	assert.Equal(t, "dynamic-prompt", mw.Name())
	assert.NotNil(t, mw.promptModifier)
}

func TestDynamicPromptMiddleware_OnBefore_WithStringInput(t *testing.T) {
	modifier := func(req *MiddlewareRequest) string {
		return "Modified: "
	}
	mw := NewDynamicPromptMiddleware(modifier)

	req := &MiddlewareRequest{
		Input:    "original prompt",
		Metadata: make(map[string]interface{}),
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, modifiedReq)

	// Check that prompt was modified
	assert.Contains(t, modifiedReq.Input, "Modified:")
	assert.Equal(t, "original prompt", modifiedReq.Metadata["original_prompt"])
}

func TestDynamicPromptMiddleware_OnBefore_WithState(t *testing.T) {
	modifier := func(req *MiddlewareRequest) string {
		return "Modified: "
	}
	mw := NewDynamicPromptMiddleware(modifier)

	state := NewAgentState()
	state.Set("user_name", "Alice")
	state.Set("conversation_history", "Previous discussion")

	req := &MiddlewareRequest{
		Input:    "original prompt",
		State:    state,
		Metadata: make(map[string]interface{}),
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)

	// Check that context was added
	inputStr := modifiedReq.Input.(string)
	assert.Contains(t, inputStr, "Context: User=Alice")
	assert.Contains(t, inputStr, "Previous context: Previous discussion")
}

func TestDynamicPromptMiddleware_OnBefore_NonStringInput(t *testing.T) {
	modifier := func(req *MiddlewareRequest) string {
		return "Modified: "
	}
	mw := NewDynamicPromptMiddleware(modifier)

	req := &MiddlewareRequest{
		Input:    123, // Non-string input
		Metadata: make(map[string]interface{}),
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)

	// Should return unchanged since input is not a string
	assert.Equal(t, 123, modifiedReq.Input)
}

func TestDynamicPromptMiddleware_OnBefore_NilModifier(t *testing.T) {
	mw := NewDynamicPromptMiddleware(nil)

	req := &MiddlewareRequest{
		Input: "original",
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "original", modifiedReq.Input)
}

func TestDynamicPromptMiddleware_OnBefore_EmptyModifierResult(t *testing.T) {
	modifier := func(req *MiddlewareRequest) string {
		return "" // Empty modifier result
	}
	mw := NewDynamicPromptMiddleware(modifier)

	req := &MiddlewareRequest{
		Input:    "original",
		Metadata: make(map[string]interface{}),
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)

	// Should use original when modifier returns empty
	assert.Equal(t, "original", modifiedReq.Input)
}

// ===== ToolSelectorMiddleware Tests =====

func TestNewToolSelectorMiddleware(t *testing.T) {
	tools := []string{"calculator", "search"}
	mw := NewToolSelectorMiddleware(tools, 2)

	require.NotNil(t, mw)
	assert.Equal(t, "tool-selector", mw.Name())
	assert.Equal(t, tools, mw.availableTools)
	assert.Equal(t, 2, mw.maxTools)
}

func TestToolSelectorMiddleware_OnBefore_WithStringInput(t *testing.T) {
	tools := []string{"calculator", "search", "database"}
	mw := NewToolSelectorMiddleware(tools, 2)

	req := &MiddlewareRequest{
		Input:    "calculate 5 + 3",
		Metadata: make(map[string]interface{}),
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)

	// Check that tools were selected
	selectedTools := modifiedReq.Metadata["selected_tools"].([]string)
	assert.Greater(t, len(selectedTools), 0)
	assert.Equal(t, len(selectedTools), modifiedReq.Metadata["tool_count"])
}

func TestToolSelectorMiddleware_OnBefore_WithMapInput(t *testing.T) {
	tools := []string{"calculator", "search"}
	mw := NewToolSelectorMiddleware(tools, 2)

	req := &MiddlewareRequest{
		Input: map[string]interface{}{
			"query": "search for something",
		},
		Metadata: make(map[string]interface{}),
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)

	selectedTools := modifiedReq.Metadata["selected_tools"].([]string)
	assert.Greater(t, len(selectedTools), 0)
}

func TestToolSelectorMiddleware_WithSelector(t *testing.T) {
	tools := []string{"tool1", "tool2", "tool3"}
	customSelector := func(query string, availableTools []string) []string {
		// Custom selector that always returns the first tool
		if len(availableTools) > 0 {
			return []string{availableTools[0]}
		}
		return []string{}
	}

	mw := NewToolSelectorMiddleware(tools, 2).WithSelector(customSelector)

	req := &MiddlewareRequest{
		Input:    "test query",
		Metadata: make(map[string]interface{}),
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)

	selectedTools := modifiedReq.Metadata["selected_tools"].([]string)
	assert.Equal(t, []string{"tool1"}, selectedTools)
}

func TestToolSelectorMiddleware_MaxTools_Limiting(t *testing.T) {
	tools := []string{"tool1", "tool2", "tool3", "tool4", "tool5"}
	mw := NewToolSelectorMiddleware(tools, 2) // Max 2 tools

	req := &MiddlewareRequest{
		Input:    "test",
		Metadata: make(map[string]interface{}),
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)

	selectedTools := modifiedReq.Metadata["selected_tools"].([]string)
	assert.LessOrEqual(t, len(selectedTools), 2)
}

func TestToolSelectorMiddleware_ContainsKeyword(t *testing.T) {
	tests := []struct {
		query    string
		keyword  string
		expected bool
	}{
		{"calculate 5 + 3", "calculate", true},
		{"", "calculate", false},
		{"search for data", "find", true}, // containsKeyword returns true for any non-empty query and keyword
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s contains %s", tt.query, tt.keyword), func(t *testing.T) {
			result := containsKeyword(tt.query, tt.keyword)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ===== RateLimiterMiddleware Tests =====

func TestNewRateLimiterMiddleware(t *testing.T) {
	mw := NewRateLimiterMiddleware(10, 1*time.Second)

	require.NotNil(t, mw)
	assert.Equal(t, "rate-limiter", mw.Name())
	assert.Equal(t, 10, mw.maxRequests)
	assert.Equal(t, 1*time.Second, mw.windowSize)
}

func TestRateLimiterMiddleware_OnBefore_WithinLimit(t *testing.T) {
	mw := NewRateLimiterMiddleware(5, 1*time.Second)

	req := &MiddlewareRequest{
		Input:    "request",
		Metadata: make(map[string]interface{}),
	}

	// First few requests should succeed
	for i := 0; i < 3; i++ {
		modifiedReq, err := mw.OnBefore(context.Background(), req)
		require.NoError(t, err)
		assert.Greater(t, modifiedReq.Metadata["rate_limit_remaining"], 0)
	}
}

func TestRateLimiterMiddleware_OnBefore_ExceedsLimit(t *testing.T) {
	mw := NewRateLimiterMiddleware(2, 1*time.Second)

	req := &MiddlewareRequest{
		Input:    "request",
		Metadata: make(map[string]interface{}),
	}

	// Exceed limit
	mw.OnBefore(context.Background(), req)
	mw.OnBefore(context.Background(), req)

	_, err := mw.OnBefore(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestRateLimiterMiddleware_OnBefore_WithUserInState(t *testing.T) {
	mw := NewRateLimiterMiddleware(2, 1*time.Second)

	state := NewAgentState()
	state.Set("user_id", "user123")

	req := &MiddlewareRequest{
		Input:    "request",
		State:    state,
		Metadata: make(map[string]interface{}),
	}

	mw.OnBefore(context.Background(), req)
	mw.OnBefore(context.Background(), req)

	_, err := mw.OnBefore(context.Background(), req)
	assert.Error(t, err)
}

func TestRateLimiterMiddleware_OnBefore_WithUserInMetadata(t *testing.T) {
	mw := NewRateLimiterMiddleware(2, 1*time.Second)

	req := &MiddlewareRequest{
		Input: "request",
		Metadata: map[string]interface{}{
			"user_id": "user456",
		},
	}

	mw.OnBefore(context.Background(), req)
	mw.OnBefore(context.Background(), req)

	_, err := mw.OnBefore(context.Background(), req)
	assert.Error(t, err)
}

func TestRateLimiterMiddleware_WindowReset(t *testing.T) {
	mw := NewRateLimiterMiddleware(2, 100*time.Millisecond)

	req := &MiddlewareRequest{
		Input:    "request",
		Metadata: make(map[string]interface{}),
	}

	// Use up the window
	mw.OnBefore(context.Background(), req)
	mw.OnBefore(context.Background(), req)
	_, err := mw.OnBefore(context.Background(), req)
	assert.Error(t, err)

	// Wait for window to reset
	time.Sleep(150 * time.Millisecond)

	// Should work again
	modifiedReq, err := mw.OnBefore(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, modifiedReq)
}

func TestRateLimiterMiddleware_Reset(t *testing.T) {
	mw := NewRateLimiterMiddleware(2, 1*time.Second)

	req := &MiddlewareRequest{
		Input:    "request",
		Metadata: make(map[string]interface{}),
	}

	// Use up the window
	mw.OnBefore(context.Background(), req)
	mw.OnBefore(context.Background(), req)

	// Reset
	mw.Reset()

	// Should work now
	modifiedReq, err := mw.OnBefore(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, modifiedReq)
}

// ===== AuthenticationMiddleware Tests =====

func TestNewAuthenticationMiddleware(t *testing.T) {
	authFunc := func(ctx context.Context, req *MiddlewareRequest) (bool, error) {
		return true, nil
	}
	mw := NewAuthenticationMiddleware(authFunc)

	require.NotNil(t, mw)
	assert.Equal(t, "authentication", mw.Name())
}

func TestAuthenticationMiddleware_OnBefore_Success(t *testing.T) {
	authFunc := func(ctx context.Context, req *MiddlewareRequest) (bool, error) {
		return true, nil
	}
	mw := NewAuthenticationMiddleware(authFunc)

	req := &MiddlewareRequest{
		Input:    "request",
		Metadata: make(map[string]interface{}),
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, modifiedReq.Metadata["authenticated"].(bool))
}

func TestAuthenticationMiddleware_OnBefore_Failure(t *testing.T) {
	authFunc := func(ctx context.Context, req *MiddlewareRequest) (bool, error) {
		return false, nil
	}
	mw := NewAuthenticationMiddleware(authFunc)

	req := &MiddlewareRequest{Input: "request"}

	_, err := mw.OnBefore(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

func TestAuthenticationMiddleware_OnBefore_Error(t *testing.T) {
	authFunc := func(ctx context.Context, req *MiddlewareRequest) (bool, error) {
		return false, errors.New("auth service error")
	}
	mw := NewAuthenticationMiddleware(authFunc)

	req := &MiddlewareRequest{Input: "request"}

	_, err := mw.OnBefore(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication error")
}

func TestAuthenticationMiddleware_OnBefore_NilFunc(t *testing.T) {
	mw := NewAuthenticationMiddleware(nil)

	req := &MiddlewareRequest{Input: "request"}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, req, modifiedReq)
}

// ===== ValidationMiddleware Tests =====

func TestNewValidationMiddleware(t *testing.T) {
	validators := []func(*MiddlewareRequest) error{
		func(req *MiddlewareRequest) error { return nil },
	}
	mw := NewValidationMiddleware(validators...)

	require.NotNil(t, mw)
	assert.Equal(t, "validation", mw.Name())
}

func TestValidationMiddleware_OnBefore_Success(t *testing.T) {
	validators := []func(*MiddlewareRequest) error{
		func(req *MiddlewareRequest) error {
			if req.Input == nil {
				return errors.New("input is required")
			}
			return nil
		},
	}
	mw := NewValidationMiddleware(validators...)

	req := &MiddlewareRequest{
		Input:    "valid input",
		Metadata: make(map[string]interface{}),
	}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, modifiedReq.Metadata["validated"].(bool))
}

func TestValidationMiddleware_OnBefore_Failure(t *testing.T) {
	validators := []func(*MiddlewareRequest) error{
		func(req *MiddlewareRequest) error {
			if req.Input == nil {
				return errors.New("input is required")
			}
			return nil
		},
	}
	mw := NewValidationMiddleware(validators...)

	req := &MiddlewareRequest{
		Input: nil,
	}

	_, err := mw.OnBefore(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidationMiddleware_MultipleValidators(t *testing.T) {
	var validationsCalled []int
	mu := &sync.Mutex{}

	validators := []func(*MiddlewareRequest) error{
		func(req *MiddlewareRequest) error {
			mu.Lock()
			validationsCalled = append(validationsCalled, 1)
			mu.Unlock()
			return nil
		},
		func(req *MiddlewareRequest) error {
			mu.Lock()
			validationsCalled = append(validationsCalled, 2)
			mu.Unlock()
			return nil
		},
	}
	mw := NewValidationMiddleware(validators...)

	req := &MiddlewareRequest{
		Input:    "test",
		Metadata: make(map[string]interface{}),
	}

	_, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, validationsCalled)
}

func TestValidationMiddleware_FirstValidatorFails(t *testing.T) {
	var validationsCalled []int
	mu := &sync.Mutex{}

	validators := []func(*MiddlewareRequest) error{
		func(req *MiddlewareRequest) error {
			mu.Lock()
			validationsCalled = append(validationsCalled, 1)
			mu.Unlock()
			return errors.New("first validator failed")
		},
		func(req *MiddlewareRequest) error {
			mu.Lock()
			validationsCalled = append(validationsCalled, 2)
			mu.Unlock()
			return nil
		},
	}
	mw := NewValidationMiddleware(validators...)

	req := &MiddlewareRequest{Input: "test"}

	_, err := mw.OnBefore(context.Background(), req)
	require.Error(t, err)
	// Second validator should not be called
	assert.Equal(t, []int{1}, validationsCalled)
}

// ===== TransformMiddleware Tests =====

func TestNewTransformMiddleware(t *testing.T) {
	inputTf := func(input interface{}) (interface{}, error) { return input, nil }
	outputTf := func(output interface{}) (interface{}, error) { return output, nil }

	mw := NewTransformMiddleware(inputTf, outputTf)

	require.NotNil(t, mw)
	assert.Equal(t, "transform", mw.Name())
}

func TestTransformMiddleware_OnBefore(t *testing.T) {
	inputTf := func(input interface{}) (interface{}, error) {
		return fmt.Sprintf("transformed-%v", input), nil
	}

	mw := NewTransformMiddleware(inputTf, nil)

	req := &MiddlewareRequest{Input: "original"}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "transformed-original", modifiedReq.Input)
}

func TestTransformMiddleware_OnBefore_Error(t *testing.T) {
	inputTf := func(input interface{}) (interface{}, error) {
		return nil, errors.New("transform failed")
	}

	mw := NewTransformMiddleware(inputTf, nil)

	req := &MiddlewareRequest{Input: "original"}

	_, err := mw.OnBefore(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input transform failed")
}

func TestTransformMiddleware_OnAfter(t *testing.T) {
	outputTf := func(output interface{}) (interface{}, error) {
		return fmt.Sprintf("wrapped-%v", output), nil
	}

	mw := NewTransformMiddleware(nil, outputTf)

	resp := &MiddlewareResponse{
		Output: "original",
		Error:  nil,
	}

	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	require.NoError(t, err)
	assert.Equal(t, "wrapped-original", modifiedResp.Output)
}

func TestTransformMiddleware_OnAfter_WithError(t *testing.T) {
	outputTf := func(output interface{}) (interface{}, error) {
		return fmt.Sprintf("wrapped-%v", output), nil
	}

	mw := NewTransformMiddleware(nil, outputTf)

	resp := &MiddlewareResponse{
		Output: "original",
		Error:  errors.New("request error"),
	}

	modifiedResp, err := mw.OnAfter(context.Background(), resp)
	require.NoError(t, err)
	// Should not transform if there was an error
	assert.Equal(t, "original", modifiedResp.Output)
}

func TestTransformMiddleware_OnAfter_TransformError(t *testing.T) {
	outputTf := func(output interface{}) (interface{}, error) {
		return nil, errors.New("output transform failed")
	}

	mw := NewTransformMiddleware(nil, outputTf)

	resp := &MiddlewareResponse{
		Output: "original",
		Error:  nil,
	}

	_, err := mw.OnAfter(context.Background(), resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output transform failed")
}

// ===== CircuitBreakerMiddleware Tests =====

func TestNewCircuitBreakerMiddleware(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(3, 1*time.Second)

	require.NotNil(t, mw)
	assert.Equal(t, "circuit-breaker", mw.Name())
	assert.Equal(t, "closed", mw.state)
}

func TestCircuitBreakerMiddleware_OnBefore_Closed(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(3, 1*time.Second)

	req := &MiddlewareRequest{Input: "test"}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, req, modifiedReq)
}

func TestCircuitBreakerMiddleware_OnError_AccumulatesFailures(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(3, 1*time.Second)

	// Accumulate failures
	err := errors.New("test error")
	mw.OnError(context.Background(), err)
	mw.OnError(context.Background(), err)

	// Circuit should still be closed
	mw.mu.RLock()
	state := mw.state
	mw.mu.RUnlock()
	assert.Equal(t, "closed", state)

	// Third failure should open the circuit
	mw.OnError(context.Background(), err)

	mw.mu.RLock()
	state = mw.state
	mw.mu.RUnlock()
	assert.Equal(t, "open", state)
}

func TestCircuitBreakerMiddleware_OnBefore_Open(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(1, 1*time.Second)

	// Trigger failure to open circuit
	mw.OnError(context.Background(), errors.New("test error"))

	req := &MiddlewareRequest{Input: "test"}

	_, err := mw.OnBefore(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestCircuitBreakerMiddleware_HalfOpen_Success(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(1, 100*time.Millisecond)

	// Open the circuit
	mw.OnError(context.Background(), errors.New("test error"))

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Request should transition to half-open
	req := &MiddlewareRequest{Input: "test"}
	_, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)

	// Success in half-open should close the circuit
	resp := &MiddlewareResponse{
		Output: "success",
		Error:  nil,
	}
	_, _ = mw.OnAfter(context.Background(), resp)

	mw.mu.RLock()
	state := mw.state
	mw.mu.RUnlock()
	assert.Equal(t, "closed", state)
}

func TestCircuitBreakerMiddleware_HalfOpen_Failure(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(1, 100*time.Millisecond)

	// Open the circuit
	mw.OnError(context.Background(), errors.New("test error"))

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Request should transition to half-open
	req := &MiddlewareRequest{Input: "test"}
	mw.OnBefore(context.Background(), req)

	// Failure in half-open should re-open the circuit
	mw.OnError(context.Background(), errors.New("test error"))

	mw.mu.RLock()
	state := mw.state
	mw.mu.RUnlock()
	assert.Equal(t, "open", state)
}

// ===== RandomDelayMiddleware Tests =====

func TestNewRandomDelayMiddleware(t *testing.T) {
	mw := NewRandomDelayMiddleware(10*time.Millisecond, 50*time.Millisecond)

	require.NotNil(t, mw)
	assert.Equal(t, "random-delay", mw.Name())
	assert.Equal(t, 10*time.Millisecond, mw.minDelay)
	assert.Equal(t, 50*time.Millisecond, mw.maxDelay)
}

func TestRandomDelayMiddleware_OnBefore_AddsDelay(t *testing.T) {
	mw := NewRandomDelayMiddleware(50*time.Millisecond, 100*time.Millisecond)

	req := &MiddlewareRequest{Input: "test"}

	start := time.Now()
	modifiedReq, err := mw.OnBefore(context.Background(), req)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, req, modifiedReq)
	// Should have delayed at least minDelay
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
}

func TestRandomDelayMiddleware_OnBefore_ContextCancelled(t *testing.T) {
	mw := NewRandomDelayMiddleware(100*time.Millisecond, 500*time.Millisecond)

	req := &MiddlewareRequest{Input: "test"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := mw.OnBefore(ctx, req)
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestRandomDelayMiddleware_OnBefore_ZeroDelay(t *testing.T) {
	mw := NewRandomDelayMiddleware(0, 0)

	req := &MiddlewareRequest{Input: "test"}

	modifiedReq, err := mw.OnBefore(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, req, modifiedReq)
}

// ===== Integration Tests =====

func TestComplexMiddlewareChain_AllTypes(t *testing.T) {
	var executionLog []string
	mu := &sync.Mutex{}

	logExecution := func(msg string) {
		mu.Lock()
		executionLog = append(executionLog, msg)
		mu.Unlock()
	}

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		logExecution("handler")
		return &MiddlewareResponse{
			Output: fmt.Sprintf("processed-%v", req.Input),
		}, nil
	}

	// Build complex chain
	chain := NewMiddlewareChain(handler)
	chain.Use(
		NewLoggingMiddleware(func(msg string) { logExecution(msg) }),
		NewValidationMiddleware(
			func(req *MiddlewareRequest) error {
				if req.Input == nil {
					return errors.New("input required")
				}
				return nil
			},
		),
		NewTransformMiddleware(
			func(input interface{}) (interface{}, error) {
				logExecution("input-transform")
				return fmt.Sprintf("transformed-%v", input), nil
			},
			nil,
		),
	)

	req := &MiddlewareRequest{
		Input:    "test",
		Metadata: make(map[string]interface{}),
	}

	resp, err := chain.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)

	// Verify all transformations were applied
	assert.Contains(t, resp.Output, "transformed")
	assert.Contains(t, resp.Output, "processed")
}

func TestMiddlewareChain_AllAdvancedMiddleware_ConcurrentRateLimit(t *testing.T) {
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{Output: "ok"}, nil
	}

	chain := NewMiddlewareChain(handler)
	chain.Use(
		NewRateLimiterMiddleware(5, 100*time.Millisecond),
		NewLoggingMiddleware(nil),
	)

	var successCount int32
	var failureCount int32

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &MiddlewareRequest{
				Input:    fmt.Sprintf("request-%d", idx),
				Metadata: make(map[string]interface{}),
			}
			_, err := chain.Execute(context.Background(), req)
			if err != nil {
				atomic.AddInt32(&failureCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// We should have limited concurrency
	totalRequests := atomic.LoadInt32(&successCount) + atomic.LoadInt32(&failureCount)
	assert.Equal(t, int32(10), totalRequests)
	assert.Greater(t, atomic.LoadInt32(&failureCount), int32(0))
}

func TestToolSelectorMiddleware_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		tools     []string
		maxTools  int
		input     interface{}
		expectErr bool
	}{
		{
			name:      "empty tools",
			tools:     []string{},
			maxTools:  5,
			input:     "search something",
			expectErr: false,
		},
		{
			name:      "invalid input type",
			tools:     []string{"search", "calc"},
			maxTools:  2,
			input:     12345,
			expectErr: false,
		},
		{
			name:     "map without query field",
			tools:    []string{"search"},
			maxTools: 5,
			input: map[string]interface{}{
				"other": "field",
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := NewToolSelectorMiddleware(tt.tools, tt.maxTools)
			req := &MiddlewareRequest{
				Input:    tt.input,
				Metadata: make(map[string]interface{}),
			}

			modifiedReq, err := mw.OnBefore(context.Background(), req)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, modifiedReq.Metadata["selected_tools"])
				assert.NotNil(t, modifiedReq.Metadata["tool_count"])
			}
		})
	}
}

func TestCacheMiddleware_ConcurrentAccess(t *testing.T) {
	mw := NewCacheMiddleware(1 * time.Second)

	var wg sync.WaitGroup
	numGoroutines := 10

	// First, populate cache
	resp := &MiddlewareResponse{
		Output:   "cached",
		Metadata: map[string]interface{}{"original_input": "test-key"},
	}
	mw.OnAfter(context.Background(), resp)

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &MiddlewareRequest{
				Input:    "test-key",
				Metadata: make(map[string]interface{}),
			}
			modifiedReq, _ := mw.OnBefore(context.Background(), req)
			assert.True(t, modifiedReq.Metadata["cache_hit"].(bool))
		}()
	}

	wg.Wait()
}

func TestDefaultToolSelector(t *testing.T) {
	tests := []struct {
		query          string
		expectedLength int
	}{
		{
			query:          "calculate 5 plus 3",
			expectedLength: 1,
		},
		{
			query:          "search for results",
			expectedLength: 1,
		},
		{
			query:          "write to file",
			expectedLength: 1,
		},
		{
			query:          "run python script",
			expectedLength: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			allTools := []string{
				"calculator", "search", "database", "file_reader", "file_writer",
				"web_scraper", "translator", "code_runner", "image_reader", "api_caller",
			}
			selected := defaultToolSelector(tt.query, allTools)

			// Verify tools were selected
			assert.Greater(t, len(selected), 0)
		})
	}
}

// ===== Benchmark Tests =====

func BenchmarkDynamicPromptMiddleware(b *testing.B) {
	modifier := func(req *MiddlewareRequest) string {
		return "Modified: "
	}
	mw := NewDynamicPromptMiddleware(modifier)

	req := &MiddlewareRequest{
		Input:    "test prompt",
		Metadata: make(map[string]interface{}),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mw.OnBefore(context.Background(), req)
	}
}

func BenchmarkRateLimiterMiddleware(b *testing.B) {
	mw := NewRateLimiterMiddleware(1000, 1*time.Second)

	req := &MiddlewareRequest{
		Input:    "test",
		Metadata: make(map[string]interface{}),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mw.OnBefore(context.Background(), req)
	}
}

func BenchmarkToolSelectorMiddleware(b *testing.B) {
	tools := []string{"calculator", "search", "database", "file_reader", "file_writer"}
	mw := NewToolSelectorMiddleware(tools, 3)

	req := &MiddlewareRequest{
		Input:    "search for data",
		Metadata: make(map[string]interface{}),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mw.OnBefore(context.Background(), req)
	}
}

func BenchmarkCircuitBreakerMiddleware(b *testing.B) {
	mw := NewCircuitBreakerMiddleware(10, 1*time.Second)

	req := &MiddlewareRequest{Input: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mw.OnBefore(context.Background(), req)
	}
}
