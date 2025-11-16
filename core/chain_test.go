package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockStep 模拟步骤实现
type MockStep struct {
	name        string
	description string
	executeFunc func(ctx context.Context, input interface{}) (interface{}, error)
}

// NewMockStep 创建模拟步骤
func NewMockStep(name, description string, executeFunc func(context.Context, interface{}) (interface{}, error)) *MockStep {
	return &MockStep{
		name:        name,
		description: description,
		executeFunc: executeFunc,
	}
}

// Execute 实现 Step 接口
func (s *MockStep) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	if s.executeFunc != nil {
		return s.executeFunc(ctx, input)
	}
	return input, nil
}

// Name 返回步骤名称
func (s *MockStep) Name() string {
	return s.name
}

// Description 返回步骤描述
func (s *MockStep) Description() string {
	return s.description
}

func TestBaseChain_Name(t *testing.T) {
	steps := []Step{
		NewMockStep("step1", "description1", nil),
	}
	chain := NewBaseChain("TestChain", steps)

	assert.Equal(t, "TestChain", chain.Name())
}

func TestBaseChain_Steps(t *testing.T) {
	tests := []struct {
		name      string
		stepCount int
	}{
		{
			name:      "single step",
			stepCount: 1,
		},
		{
			name:      "multiple steps",
			stepCount: 5,
		},
		{
			name:      "no steps",
			stepCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := make([]Step, tt.stepCount)
			for i := 0; i < tt.stepCount; i++ {
				steps[i] = NewMockStep("step", "description", nil)
			}

			chain := NewBaseChain("TestChain", steps)
			assert.Equal(t, tt.stepCount, chain.Steps())
		})
	}
}

func TestBaseChain_Invoke_Success(t *testing.T) {
	// 创建一个简单的处理链
	steps := []Step{
		NewMockStep("uppercase", "convert to uppercase", func(ctx context.Context, input interface{}) (interface{}, error) {
			if str, ok := input.(string); ok {
				return str + "_UPPER", nil
			}
			return input, nil
		}),
		NewMockStep("addprefix", "add prefix", func(ctx context.Context, input interface{}) (interface{}, error) {
			if str, ok := input.(string); ok {
				return "PREFIX_" + str, nil
			}
			return input, nil
		}),
		NewMockStep("addsuffix", "add suffix", func(ctx context.Context, input interface{}) (interface{}, error) {
			if str, ok := input.(string); ok {
				return str + "_SUFFIX", nil
			}
			return input, nil
		}),
	}

	chain := NewBaseChain("ProcessingChain", steps)

	input := &ChainInput{
		Data:    "test",
		Options: DefaultChainOptions(),
	}

	output, err := chain.Invoke(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	assert.Equal(t, "PREFIX_test_UPPER_SUFFIX", output.Data)
	assert.Len(t, output.StepsExecuted, 3)
	assert.True(t, output.StepsExecuted[0].Success)
	assert.True(t, output.StepsExecuted[1].Success)
	assert.True(t, output.StepsExecuted[2].Success)
}

func TestBaseChain_Invoke_WithError(t *testing.T) {
	expectedError := errors.New("step2 error")

	steps := []Step{
		NewMockStep("step1", "successful step", func(ctx context.Context, input interface{}) (interface{}, error) {
			return "step1_result", nil
		}),
		NewMockStep("step2", "failing step", func(ctx context.Context, input interface{}) (interface{}, error) {
			return nil, expectedError
		}),
		NewMockStep("step3", "should not execute", func(ctx context.Context, input interface{}) (interface{}, error) {
			return "step3_result", nil
		}),
	}

	chain := NewBaseChain("FailingChain", steps)

	input := &ChainInput{
		Data: "initial_data",
		Options: ChainOptions{
			StopOnError: true,
			Timeout:     60 * time.Second,
		},
	}

	output, err := chain.Invoke(context.Background(), input)

	assert.Error(t, err)
	assert.Equal(t, "failed", output.Status)
	assert.Len(t, output.StepsExecuted, 2)
	assert.True(t, output.StepsExecuted[0].Success)
	assert.False(t, output.StepsExecuted[1].Success)
	assert.Equal(t, expectedError.Error(), output.StepsExecuted[1].Error)
}

func TestBaseChain_Invoke_ContinueOnError(t *testing.T) {
	steps := []Step{
		NewMockStep("step1", "successful step", func(ctx context.Context, input interface{}) (interface{}, error) {
			return "step1_result", nil
		}),
		NewMockStep("step2", "failing step", func(ctx context.Context, input interface{}) (interface{}, error) {
			return nil, errors.New("step2 failed")
		}),
		NewMockStep("step3", "should execute", func(ctx context.Context, input interface{}) (interface{}, error) {
			return "step3_result", nil
		}),
	}

	chain := NewBaseChain("ContinueChain", steps)

	input := &ChainInput{
		Data: "initial_data",
		Options: ChainOptions{
			StopOnError: false,
			Timeout:     60 * time.Second,
		},
	}

	output, err := chain.Invoke(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "partial", output.Status)
	assert.Len(t, output.StepsExecuted, 3)
	assert.True(t, output.StepsExecuted[0].Success)
	assert.False(t, output.StepsExecuted[1].Success)
	assert.True(t, output.StepsExecuted[2].Success)
}

func TestBaseChain_Invoke_SkipSteps(t *testing.T) {
	executionOrder := []string{}

	steps := []Step{
		NewMockStep("step1", "step1", func(ctx context.Context, input interface{}) (interface{}, error) {
			executionOrder = append(executionOrder, "step1")
			return input, nil
		}),
		NewMockStep("step2", "step2", func(ctx context.Context, input interface{}) (interface{}, error) {
			executionOrder = append(executionOrder, "step2")
			return input, nil
		}),
		NewMockStep("step3", "step3", func(ctx context.Context, input interface{}) (interface{}, error) {
			executionOrder = append(executionOrder, "step3")
			return input, nil
		}),
	}

	chain := NewBaseChain("SkipChain", steps)

	input := &ChainInput{
		Data: "test",
		Options: ChainOptions{
			StopOnError: true,
			SkipSteps:   []int{2}, // Skip step 2
		},
	}

	output, err := chain.Invoke(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	assert.Len(t, output.StepsExecuted, 3)
	assert.False(t, output.StepsExecuted[0].Skipped) // Step 1 should execute
	assert.True(t, output.StepsExecuted[1].Skipped)  // Step 2 should be skipped
	assert.False(t, output.StepsExecuted[2].Skipped) // Step 3 should execute
	assert.Equal(t, []string{"step1", "step3"}, executionOrder)
}

func TestBaseChain_Invoke_OnlySteps(t *testing.T) {
	executionOrder := []string{}

	steps := []Step{
		NewMockStep("step1", "step1", func(ctx context.Context, input interface{}) (interface{}, error) {
			executionOrder = append(executionOrder, "step1")
			return input, nil
		}),
		NewMockStep("step2", "step2", func(ctx context.Context, input interface{}) (interface{}, error) {
			executionOrder = append(executionOrder, "step2")
			return input, nil
		}),
		NewMockStep("step3", "step3", func(ctx context.Context, input interface{}) (interface{}, error) {
			executionOrder = append(executionOrder, "step3")
			return input, nil
		}),
	}

	chain := NewBaseChain("OnlyStepsChain", steps)

	input := &ChainInput{
		Data: "test",
		Options: ChainOptions{
			StopOnError: true,
			OnlySteps:   []int{1, 3}, // Only execute steps 1 and 3
		},
	}

	output, err := chain.Invoke(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	assert.Len(t, output.StepsExecuted, 3)
	assert.False(t, output.StepsExecuted[0].Skipped)
	assert.True(t, output.StepsExecuted[1].Skipped)
	assert.False(t, output.StepsExecuted[2].Skipped)
	assert.Equal(t, []string{"step1", "step3"}, executionOrder)
}

func TestBaseChain_Invoke_WithTimeout(t *testing.T) {
	steps := []Step{
		NewMockStep("slow_step", "takes too long", func(ctx context.Context, input interface{}) (interface{}, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
				return "result", nil
			}
		}),
	}

	chain := NewBaseChain("TimeoutChain", steps)

	input := &ChainInput{
		Data: "test",
		Options: ChainOptions{
			StopOnError: true,
			Timeout:     10 * time.Millisecond, // Short timeout
		},
	}

	output, err := chain.Invoke(context.Background(), input)

	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.Equal(t, "failed", output.Status)
}

func TestBaseChain_Invoke_EmptyChain(t *testing.T) {
	chain := NewBaseChain("EmptyChain", []Step{})

	input := &ChainInput{
		Data:    "test_data",
		Options: DefaultChainOptions(),
	}

	output, err := chain.Invoke(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	assert.Equal(t, "test_data", output.Data)
	assert.Len(t, output.StepsExecuted, 0)
}

func TestDefaultChainOptions(t *testing.T) {
	opts := DefaultChainOptions()

	assert.True(t, opts.StopOnError)
	assert.Equal(t, 60*time.Second, opts.Timeout)
	assert.False(t, opts.Parallel)
	assert.NotNil(t, opts.Extra)
	assert.Empty(t, opts.SkipSteps)
	assert.Empty(t, opts.OnlySteps)
}

func TestChainInput_Structure(t *testing.T) {
	input := &ChainInput{
		Data: map[string]interface{}{
			"key1": "value1",
			"key2": 123,
		},
		Vars: map[string]interface{}{
			"var1": "test",
			"var2": 456,
		},
		Options: ChainOptions{
			StopOnError: false,
			Timeout:     30 * time.Second,
		},
	}

	assert.NotNil(t, input.Data)
	assert.Len(t, input.Vars, 2)
	assert.False(t, input.Options.StopOnError)
	assert.Equal(t, 30*time.Second, input.Options.Timeout)
}

func TestChainOutput_Structure(t *testing.T) {
	output := &ChainOutput{
		Data: "processed_data",
		StepsExecuted: []StepExecution{
			{
				StepNumber:  1,
				StepName:    "step1",
				Description: "description1",
				Success:     true,
				Duration:    50 * time.Millisecond,
			},
		},
		TotalLatency: 100 * time.Millisecond,
		Status:       "success",
		Metadata: map[string]interface{}{
			"meta1": "value1",
		},
	}

	assert.Equal(t, "processed_data", output.Data)
	assert.Len(t, output.StepsExecuted, 1)
	assert.Equal(t, "success", output.Status)
	assert.Equal(t, 100*time.Millisecond, output.TotalLatency)
	assert.Len(t, output.Metadata, 1)
}

func TestShouldSkipStep(t *testing.T) {
	tests := []struct {
		name       string
		stepNum    int
		options    ChainOptions
		shouldSkip bool
	}{
		{
			name:       "no skip or only steps",
			stepNum:    1,
			options:    ChainOptions{},
			shouldSkip: false,
		},
		{
			name:    "step in skip list",
			stepNum: 2,
			options: ChainOptions{
				SkipSteps: []int{1, 2, 3},
			},
			shouldSkip: true,
		},
		{
			name:    "step not in skip list",
			stepNum: 4,
			options: ChainOptions{
				SkipSteps: []int{1, 2, 3},
			},
			shouldSkip: false,
		},
		{
			name:    "step in only steps list",
			stepNum: 2,
			options: ChainOptions{
				OnlySteps: []int{1, 2, 3},
			},
			shouldSkip: false,
		},
		{
			name:    "step not in only steps list",
			stepNum: 4,
			options: ChainOptions{
				OnlySteps: []int{1, 2, 3},
			},
			shouldSkip: true,
		},
		{
			name:    "only steps takes precedence",
			stepNum: 2,
			options: ChainOptions{
				SkipSteps: []int{2},
				OnlySteps: []int{1, 2, 3},
			},
			shouldSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSkipStep(tt.stepNum, tt.options)
			assert.Equal(t, tt.shouldSkip, result)
		})
	}
}

// Benchmark tests
func BenchmarkBaseChain_Invoke_SimpleChain(b *testing.B) {
	steps := []Step{
		NewMockStep("step1", "step1", func(ctx context.Context, input interface{}) (interface{}, error) {
			return input, nil
		}),
		NewMockStep("step2", "step2", func(ctx context.Context, input interface{}) (interface{}, error) {
			return input, nil
		}),
		NewMockStep("step3", "step3", func(ctx context.Context, input interface{}) (interface{}, error) {
			return input, nil
		}),
	}

	chain := NewBaseChain("BenchChain", steps)
	input := &ChainInput{
		Data:    "test_data",
		Options: DefaultChainOptions(),
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chain.Invoke(ctx, input)
	}
}

func BenchmarkBaseChain_Invoke_LongChain(b *testing.B) {
	steps := make([]Step, 10)
	for i := 0; i < 10; i++ {
		steps[i] = NewMockStep("step", "description", func(ctx context.Context, input interface{}) (interface{}, error) {
			return input, nil
		})
	}

	chain := NewBaseChain("LongChain", steps)
	input := &ChainInput{
		Data:    "test_data",
		Options: DefaultChainOptions(),
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chain.Invoke(ctx, input)
	}
}

// Test Runnable interface methods

func TestBaseChain_Stream(t *testing.T) {
	steps := []Step{
		NewMockStep("step1", "first step", func(ctx context.Context, input interface{}) (interface{}, error) {
			return "step1_output", nil
		}),
		NewMockStep("step2", "second step", func(ctx context.Context, input interface{}) (interface{}, error) {
			return "step2_output", nil
		}),
	}

	chain := NewBaseChain("StreamChain", steps)
	input := &ChainInput{
		Data:    "initial",
		Options: DefaultChainOptions(),
	}

	stream, err := chain.Stream(context.Background(), input)
	require.NoError(t, err)

	chunks := []StreamChunk[*ChainOutput]{}
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}

	// Should have intermediate chunks + final chunk
	assert.GreaterOrEqual(t, len(chunks), 2)

	// Last chunk should be done
	lastChunk := chunks[len(chunks)-1]
	assert.True(t, lastChunk.Done)
	assert.NoError(t, lastChunk.Error)
	assert.Equal(t, "success", lastChunk.Data.Status)
}

func TestBaseChain_Batch(t *testing.T) {
	steps := []Step{
		NewMockStep("step1", "transform", func(ctx context.Context, input interface{}) (interface{}, error) {
			if str, ok := input.(string); ok {
				return str + "_transformed", nil
			}
			return input, nil
		}),
	}

	chain := NewBaseChain("BatchChain", steps)
	inputs := []*ChainInput{
		{Data: "input1", Options: DefaultChainOptions()},
		{Data: "input2", Options: DefaultChainOptions()},
		{Data: "input3", Options: DefaultChainOptions()},
	}

	outputs, err := chain.Batch(context.Background(), inputs)

	require.NoError(t, err)
	assert.Len(t, outputs, 3)
	assert.Equal(t, "input1_transformed", outputs[0].Data)
	assert.Equal(t, "input2_transformed", outputs[1].Data)
	assert.Equal(t, "input3_transformed", outputs[2].Data)
}

func TestBaseChain_WithCallbacks(t *testing.T) {
	callbackCalled := false
	callback := &testCallback{
		onChainStart: func(ctx context.Context, chainName string, input interface{}) error {
			callbackCalled = true
			return nil
		},
	}

	steps := []Step{
		NewMockStep("step1", "step", func(ctx context.Context, input interface{}) (interface{}, error) {
			return input, nil
		}),
	}

	chain := NewBaseChain("CallbackChain", steps)
	chainWithCallback := chain.WithCallbacks(callback)

	input := &ChainInput{
		Data:    "test",
		Options: DefaultChainOptions(),
	}

	_, err := chainWithCallback.Invoke(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, callbackCalled)
}

// testCallback is a minimal callback implementation for testing
type testCallback struct {
	*BaseCallback
	onChainStart func(ctx context.Context, chainName string, input interface{}) error
	onChainEnd   func(ctx context.Context, chainName string, output interface{}) error
	onChainError func(ctx context.Context, chainName string, err error) error
}

func (t *testCallback) OnChainStart(ctx context.Context, chainName string, input interface{}) error {
	if t.onChainStart != nil {
		return t.onChainStart(ctx, chainName, input)
	}
	return nil
}

func (t *testCallback) OnChainEnd(ctx context.Context, chainName string, output interface{}) error {
	if t.onChainEnd != nil {
		return t.onChainEnd(ctx, chainName, output)
	}
	return nil
}

func (t *testCallback) OnChainError(ctx context.Context, chainName string, err error) error {
	if t.onChainError != nil {
		return t.onChainError(ctx, chainName, err)
	}
	return nil
}
