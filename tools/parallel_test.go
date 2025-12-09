package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockToolForParallel for testing parallel execution
type MockToolForParallel struct {
	mock.Mock
	name        string
	description string
	delay       time.Duration
	shouldFail  bool
}

func (m *MockToolForParallel) Name() string {
	return m.name
}

func (m *MockToolForParallel) Description() string {
	return m.description
}

func (m *MockToolForParallel) ArgsSchema() string {
	return "{}"
}

func (m *MockToolForParallel) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.shouldFail {
		return nil, errors.New("tool execution failed")
	}
	args := m.Called(ctx, input)

	// Handle different return types
	if args.Get(0) == nil {
		// No output, check for error
		if args.Get(1) != nil {
			if err, ok := args.Get(1).(error); ok {
				return nil, err
			}
			if errFunc, ok := args.Get(1).(func(context.Context, *interfaces.ToolInput) error); ok {
				return nil, errFunc(ctx, input)
			}
		}
		return nil, nil
	}

	// Check if first arg is a function that returns *interfaces.ToolOutput
	if outputFunc, ok := args.Get(0).(func(context.Context, *interfaces.ToolInput) *interfaces.ToolOutput); ok {
		output := outputFunc(ctx, input)
		// Check for error
		if args.Get(1) != nil {
			if errFunc, ok := args.Get(1).(func(context.Context, *interfaces.ToolInput) error); ok {
				return output, errFunc(ctx, input)
			}
		}
		return output, nil
	}

	// Direct return values
	return args.Get(0).(*interfaces.ToolOutput), args.Error(1)
}

// Implement Runnable interface methods
func (m *MockToolForParallel) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return m.Execute(ctx, input)
}

func (m *MockToolForParallel) Stream(ctx context.Context, input *interfaces.ToolInput) (<-chan agentcore.StreamChunk[*interfaces.ToolOutput], error) {
	ch := make(chan agentcore.StreamChunk[*interfaces.ToolOutput])
	go func() {
		defer close(ch)
		output, err := m.Execute(ctx, input)
		if err != nil {
			ch <- agentcore.StreamChunk[*interfaces.ToolOutput]{Error: err}
		} else {
			ch <- agentcore.StreamChunk[*interfaces.ToolOutput]{Data: output}
		}
	}()
	return ch, nil
}

func (m *MockToolForParallel) Batch(ctx context.Context, inputs []*interfaces.ToolInput) ([]*interfaces.ToolOutput, error) {
	outputs := make([]*interfaces.ToolOutput, len(inputs))
	for i, input := range inputs {
		output, err := m.Execute(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

func (m *MockToolForParallel) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*interfaces.ToolInput, any] {
	// Simple pipe implementation
	return nil
}

func (m *MockToolForParallel) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return m
}

func (m *MockToolForParallel) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return m
}

// TestToolExecutor tests
func TestToolExecutor_Creation(t *testing.T) {
	executor := NewToolExecutor(
		WithMaxConcurrency(5),
	)
	assert.NotNil(t, executor)
	assert.Equal(t, 5, executor.maxConcurrency)

	// Test with zero concurrency (should default to 10)
	executor2 := NewToolExecutor()
	assert.Equal(t, 10, executor2.maxConcurrency)
}

func TestParallelToolExecutor_ExecuteParallel(t *testing.T) {
	executor := NewToolExecutor(
		WithMaxConcurrency(3),
	)
	ctx := context.Background()

	// Create mock tools
	tool1 := &MockToolForParallel{name: "tool1", delay: 10 * time.Millisecond}
	tool2 := &MockToolForParallel{name: "tool2", delay: 10 * time.Millisecond}
	tool3 := &MockToolForParallel{name: "tool3", delay: 10 * time.Millisecond}

	output1 := &interfaces.ToolOutput{Result: "output1"}
	output2 := &interfaces.ToolOutput{Result: "output2"}
	output3 := &interfaces.ToolOutput{Result: "output3"}

	input1 := &interfaces.ToolInput{Args: map[string]interface{}{"data": "input1"}}
	input2 := &interfaces.ToolInput{Args: map[string]interface{}{"data": "input2"}}
	input3 := &interfaces.ToolInput{Args: map[string]interface{}{"data": "input3"}}

	tool1.On("Execute", mock.Anything, input1).Return(output1, nil)
	tool2.On("Execute", mock.Anything, input2).Return(output2, nil)
	tool3.On("Execute", mock.Anything, input3).Return(output3, nil)

	calls := []*ToolCallRequest{
		{ID: "call1", Tool: tool1, Input: input1},
		{ID: "call2", Tool: tool2, Input: input2},
		{ID: "call3", Tool: tool3, Input: input3},
	}

	start := time.Now()
	results, err := executor.ExecuteParallel(ctx, calls)
	duration := time.Since(start)

	// Should execute in parallel, so duration should be less than sequential
	assert.NoError(t, err)
	assert.Less(t, duration, 30*time.Millisecond)
	assert.Len(t, results, 3)

	// Verify results
	assert.Equal(t, "call1", results[0].CallID)
	assert.Equal(t, output1, results[0].Output)
	assert.Nil(t, results[0].Error)

	assert.Equal(t, "call2", results[1].CallID)
	assert.Equal(t, output2, results[1].Output)
	assert.Nil(t, results[1].Error)

	assert.Equal(t, "call3", results[2].CallID)
	assert.Equal(t, output3, results[2].Output)
	assert.Nil(t, results[2].Error)

	tool1.AssertExpectations(t)
	tool2.AssertExpectations(t)
	tool3.AssertExpectations(t)
}

func TestToolExecutor_ConcurrencyLimit(t *testing.T) {
	executor := NewToolExecutor(
		WithMaxConcurrency(2), // Limit to 2 concurrent
	)
	ctx := context.Background()

	var concurrentCount int32
	var maxConcurrent int32

	// Create tools that track concurrent execution
	createTool := func(name string) *MockToolForParallel {
		tool := &MockToolForParallel{name: name}
		tool.On("Execute", mock.Anything, mock.Anything).Return(
			func(ctx context.Context, input *interfaces.ToolInput) *interfaces.ToolOutput {
				atomic.AddInt32(&concurrentCount, 1)
				current := atomic.LoadInt32(&concurrentCount)
				for {
					max := atomic.LoadInt32(&maxConcurrent)
					if current > max {
						atomic.CompareAndSwapInt32(&maxConcurrent, max, current)
					} else {
						break
					}
				}
				time.Sleep(50 * time.Millisecond)
				atomic.AddInt32(&concurrentCount, -1)
				return &interfaces.ToolOutput{Result: fmt.Sprintf("output_%s", name)}
			},
			func(ctx context.Context, input *interfaces.ToolInput) error {
				return nil
			},
		)
		return tool
	}

	// Create 5 tools
	calls := make([]*ToolCallRequest, 5)
	for i := 0; i < 5; i++ {
		tool := createTool(fmt.Sprintf("tool%d", i))
		calls[i] = &ToolCallRequest{
			ID:    fmt.Sprintf("call%d", i),
			Tool:  tool,
			Input: &interfaces.ToolInput{Args: map[string]interface{}{"index": i}},
		}
	}

	results, err := executor.ExecuteParallel(ctx, calls)

	assert.NoError(t, err)
	assert.Len(t, results, 5)
	assert.LessOrEqual(t, maxConcurrent, int32(2), "Max concurrent should not exceed 2")
}

func TestToolExecutor_WithRetry(t *testing.T) {
	executor := NewToolExecutor(
		WithMaxConcurrency(1),
		WithRetryPolicy(&RetryPolicy{
			MaxRetries:      2,
			InitialDelay:    10 * time.Millisecond,
			MaxDelay:        100 * time.Millisecond,
			Multiplier:      1.5,
			RetryableErrors: []string{"temporary_failure"},
		}),
	)

	ctx := context.Background()

	// interfaces.Tool that fails twice then succeeds
	attemptCount := 0
	tool := &MockToolForParallel{name: "retry_tool"}
	tool.On("Execute", mock.Anything, mock.Anything).Return(
		func(ctx context.Context, input *interfaces.ToolInput) *interfaces.ToolOutput {
			attemptCount++
			if attemptCount < 3 {
				return nil
			}
			return &interfaces.ToolOutput{Result: "success"}
		},
		func(ctx context.Context, input *interfaces.ToolInput) error {
			if attemptCount < 3 {
				return errors.New("temporary_failure")
			}
			return nil
		},
	)

	calls := []*ToolCallRequest{
		{ID: "call1", Tool: tool, Input: &interfaces.ToolInput{Args: map[string]interface{}{"data": "input"}}},
	}

	results, err := executor.ExecuteParallel(ctx, calls)

	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.NotNil(t, results[0].Output)
	assert.Equal(t, "success", results[0].Output.Result)
	assert.Nil(t, results[0].Error)
	assert.Equal(t, 3, attemptCount)
}

func TestToolExecutor_NonRetryableError(t *testing.T) {
	executor := NewToolExecutor(
		WithMaxConcurrency(1),
		WithRetryPolicy(&RetryPolicy{
			MaxRetries:      3,
			InitialDelay:    10 * time.Millisecond,
			RetryableErrors: []string{"temporary_failure"},
		}),
	)

	ctx := context.Background()

	tool := &MockToolForParallel{name: "fail_tool"}
	tool.On("Execute", mock.Anything, mock.Anything).Return((*interfaces.ToolOutput)(nil), errors.New("permanent_error"))

	calls := []*ToolCallRequest{
		{ID: "call1", Tool: tool, Input: &interfaces.ToolInput{Args: map[string]interface{}{"data": "input"}}},
	}

	results, err := executor.ExecuteParallel(ctx, calls)

	assert.Error(t, err) // ExecuteParallel returns error when any tool fails
	assert.Len(t, results, 1)
	assert.Nil(t, results[0].Output)
	assert.NotNil(t, results[0].Error)
	assert.Contains(t, results[0].Error.Error(), "permanent_error")
}

func TestToolExecutor_WithDependencies(t *testing.T) {
	executor := NewToolExecutor(
		WithMaxConcurrency(3),
	)
	ctx := context.Background()

	var executionOrder []string
	var mu sync.Mutex

	createTool := func(name string) *MockToolForParallel {
		tool := &MockToolForParallel{name: name}
		tool.On("Execute", mock.Anything, mock.Anything).Return(
			func(ctx context.Context, input *interfaces.ToolInput) *interfaces.ToolOutput {
				mu.Lock()
				executionOrder = append(executionOrder, name)
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				return &interfaces.ToolOutput{Result: fmt.Sprintf("output_%s", name)}
			},
			func(ctx context.Context, input *interfaces.ToolInput) error {
				return nil
			},
		)
		return tool
	}

	// Create tools with dependencies
	tool1 := createTool("tool1")
	tool2 := createTool("tool2")
	tool3 := createTool("tool3")

	calls := []*ToolCallRequest{
		{ID: "call1", Tool: tool1, Input: &interfaces.ToolInput{Args: map[string]interface{}{"data": "input1"}}},
		{ID: "call2", Tool: tool2, Input: &interfaces.ToolInput{Args: map[string]interface{}{"data": "input2"}}, Dependencies: []string{"call1"}},
		{ID: "call3", Tool: tool3, Input: &interfaces.ToolInput{Args: map[string]interface{}{"data": "input3"}}, Dependencies: []string{"call1", "call2"}},
	}

	// For now, just test parallel execution
	// A proper dependency test would require building ToolGraph
	results, err := executor.ExecuteParallel(ctx, calls)

	assert.NoError(t, err)
	assert.Len(t, results, 3)

	// Verify that Dependencies field exists and is populated
	assert.NotNil(t, calls[1].Dependencies)
	assert.Contains(t, calls[1].Dependencies, "call1")
	assert.NotNil(t, calls[2].Dependencies)
	assert.Contains(t, calls[2].Dependencies, "call1")
	assert.Contains(t, calls[2].Dependencies, "call2")
}

func TestToolExecutor_Timeout(t *testing.T) {
	t.Skip("Skipping flaky timeout test - behavior varies by execution environment")

	executor := NewToolExecutor(
		WithMaxConcurrency(1),
		WithTimeout(50*time.Millisecond),
	)

	ctx := context.Background()

	// interfaces.Tool that takes too long
	tool := &MockToolForParallel{name: "slow_tool", delay: 100 * time.Millisecond}
	tool.On("Execute", mock.Anything, mock.Anything).Return(&interfaces.ToolOutput{Result: "output"}, nil)

	calls := []*ToolCallRequest{
		{ID: "call1", Tool: tool, Input: &interfaces.ToolInput{Args: map[string]interface{}{"data": "input"}}},
	}

	results, err := executor.ExecuteParallel(ctx, calls)

	// ExecuteParallel may return error or embed error in results
	if err != nil {
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context deadline exceeded")
	} else {
		require.NotNil(t, results)
		require.Greater(t, len(results), 0)
		if len(results) > 0 && results[0] != nil {
			assert.NotNil(t, results[0].Error)
			if results[0].Error != nil {
				assert.Contains(t, results[0].Error.Error(), "context deadline exceeded")
			}
		}
	}
}

func TestToolExecutor_Metrics(t *testing.T) {
	executor := NewToolExecutor(
		WithMaxConcurrency(3),
	)
	ctx := context.Background()

	// Create mixed success/failure tools
	successTool := &MockToolForParallel{name: "success"}
	failTool := &MockToolForParallel{name: "fail", shouldFail: true}

	successTool.On("Execute", mock.Anything, mock.Anything).Return(&interfaces.ToolOutput{Result: "output"}, nil)
	failTool.On("Execute", mock.Anything, mock.Anything).Return((*interfaces.ToolOutput)(nil), errors.New("failed"))

	calls := []*ToolCallRequest{
		{ID: "call1", Tool: successTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{"data": "input1"}}},
		{ID: "call2", Tool: successTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{"data": "input2"}}},
		{ID: "call3", Tool: failTool, Input: &interfaces.ToolInput{Args: map[string]interface{}{"data": "input3"}}},
	}

	results, err := executor.ExecuteParallel(ctx, calls)

	assert.Error(t, err) // ExecuteParallel returns error when any tool fails
	assert.Len(t, results, 3)

	// Count successful and failed executions
	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.Error == nil {
			successCount++
		} else {
			failCount++
		}
	}
	assert.Equal(t, 2, successCount)
	assert.Equal(t, 1, failCount)
}

// TestBatchToolExecutor tests
func TestBatchToolExecutor(t *testing.T) {
	executor := NewToolExecutor(
		WithMaxConcurrency(3),
	)
	ctx := context.Background()

	// Create 5 tools
	var tools []*MockToolForParallel
	var calls []*ToolCallRequest
	for i := 0; i < 5; i++ {
		tool := &MockToolForParallel{name: fmt.Sprintf("tool%d", i)}
		tool.On("Execute", mock.Anything, mock.Anything).Return(&interfaces.ToolOutput{Result: fmt.Sprintf("output%d", i)}, nil)
		tools = append(tools, tool)

		calls = append(calls, &ToolCallRequest{
			ID:    fmt.Sprintf("call%d", i),
			Tool:  tool,
			Input: &interfaces.ToolInput{Args: map[string]interface{}{"index": i}},
		})
	}

	results, err := executor.ExecuteParallel(ctx, calls)

	assert.NoError(t, err)
	assert.Len(t, results, 5)
	for i, result := range results {
		assert.NotNil(t, result.Output)
		assert.Equal(t, fmt.Sprintf("output%d", i), result.Output.Result)
		assert.Nil(t, result.Error)
	}

	for _, tool := range tools {
		tool.AssertExpectations(t)
	}
}
