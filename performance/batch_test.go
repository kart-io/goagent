package performance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewBatchExecutor tests batch executor creation
func TestNewBatchExecutor(t *testing.T) {
	agent := NewMockAgent("test", 10*time.Millisecond)
	config := BatchConfig{
		MaxConcurrency: 5,
		Timeout:        1 * time.Minute,
		ErrorPolicy:    ErrorPolicyContinue,
		EnableStats:    true,
	}

	executor := NewBatchExecutor(agent, config)
	require.NotNil(t, executor)
	assert.Equal(t, 5, executor.config.MaxConcurrency)
	assert.Equal(t, 1*time.Minute, executor.config.Timeout)
	assert.Equal(t, ErrorPolicyContinue, executor.config.ErrorPolicy)
}

// TestNewBatchExecutor_DefaultConfig tests default config values
func TestNewBatchExecutor_DefaultConfig(t *testing.T) {
	agent := NewMockAgent("test", 10*time.Millisecond)
	config := BatchConfig{} // Empty config

	executor := NewBatchExecutor(agent, config)
	require.NotNil(t, executor)
	assert.Equal(t, 10, executor.config.MaxConcurrency) // Default
	assert.Equal(t, 5*time.Minute, executor.config.Timeout)
	assert.Equal(t, ErrorPolicyContinue, executor.config.ErrorPolicy)
}

// TestDefaultBatchConfig tests default config function
func TestDefaultBatchConfig(t *testing.T) {
	config := DefaultBatchConfig()
	assert.Equal(t, 10, config.MaxConcurrency)
	assert.Equal(t, 5*time.Minute, config.Timeout)
	assert.Equal(t, ErrorPolicyContinue, config.ErrorPolicy)
	assert.True(t, config.EnableStats)
}

// TestBatchExecutor_ExecuteSuccess tests successful batch execution
func TestBatchExecutor_ExecuteSuccess(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 5*time.Millisecond)
	config := DefaultBatchConfig()
	executor := NewBatchExecutor(agent, config)

	inputs := make([]*core.AgentInput, 10)
	for i := 0; i < 10; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}

	result := executor.Execute(ctx, inputs)

	assert.NotNil(t, result)
	assert.Equal(t, 10, result.Stats.TotalCount)
	assert.Equal(t, 10, result.Stats.SuccessCount)
	assert.Equal(t, 0, result.Stats.FailureCount)
	assert.Equal(t, 0, len(result.Errors))
	assert.Len(t, result.Results, 10)

	// Verify all results are non-nil
	for i := 0; i < 10; i++ {
		assert.NotNil(t, result.Results[i])
	}
}

// TestBatchExecutor_ExecuteEmpty tests execution with empty input
func TestBatchExecutor_ExecuteEmpty(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 5*time.Millisecond)
	config := DefaultBatchConfig()
	executor := NewBatchExecutor(agent, config)

	inputs := make([]*core.AgentInput, 0)
	result := executor.Execute(ctx, inputs)

	assert.NotNil(t, result)
	assert.Equal(t, 0, result.Stats.TotalCount)
	assert.Equal(t, 0, result.Stats.SuccessCount)
	assert.Equal(t, 0, result.Stats.FailureCount)
}

// TestBatchExecutor_ExecuteLargeBatch tests execution with large batch
func TestBatchExecutor_ExecuteLargeBatch(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := BatchConfig{
		MaxConcurrency: 20,
		Timeout:        30 * time.Second,
		ErrorPolicy:    ErrorPolicyContinue,
		EnableStats:    true,
	}
	executor := NewBatchExecutor(agent, config)

	inputs := make([]*core.AgentInput, 100)
	for i := 0; i < 100; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}

	result := executor.Execute(ctx, inputs)

	assert.Equal(t, 100, result.Stats.TotalCount)
	assert.Equal(t, 100, result.Stats.SuccessCount)
	assert.Greater(t, result.Stats.AvgDuration, time.Duration(0))
}

// TestBatchExecutor_ErrorPolicyContinue tests continue error policy
func TestBatchExecutor_ErrorPolicyContinue(t *testing.T) {
	ctx := context.Background()
	agent := NewFailingAgentAfterN("test", 5*time.Millisecond, 3)
	config := BatchConfig{
		MaxConcurrency: 5,
		Timeout:        10 * time.Second,
		ErrorPolicy:    ErrorPolicyContinue,
		EnableStats:    true,
	}
	executor := NewBatchExecutor(agent, config)

	inputs := make([]*core.AgentInput, 10)
	for i := 0; i < 10; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}

	result := executor.Execute(ctx, inputs)

	// Should continue executing despite errors
	assert.Greater(t, result.Stats.SuccessCount, 0)
	assert.Greater(t, result.Stats.FailureCount, 0)
	assert.Equal(t, 10, result.Stats.TotalCount)
}

// TestBatchExecutor_ErrorPolicyFailFast tests fail fast error policy
func TestBatchExecutor_ErrorPolicyFailFast(t *testing.T) {
	ctx := context.Background()
	agent := NewFailingAgentAfterN("test", 5*time.Millisecond, 2)
	config := BatchConfig{
		MaxConcurrency: 5,
		Timeout:        10 * time.Second,
		ErrorPolicy:    ErrorPolicyFailFast,
		EnableStats:    true,
	}
	executor := NewBatchExecutor(agent, config)

	inputs := make([]*core.AgentInput, 20)
	for i := 0; i < 20; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}

	result := executor.Execute(ctx, inputs)

	// Should stop early on error
	assert.Greater(t, result.Stats.FailureCount, 0)
}

// TestBatchExecutor_ExecuteTimeout tests batch execution timeout
func TestBatchExecutor_ExecuteTimeout(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 100*time.Millisecond)
	config := BatchConfig{
		MaxConcurrency: 2,
		Timeout:        100 * time.Millisecond,
		ErrorPolicy:    ErrorPolicyContinue,
		EnableStats:    true,
	}
	executor := NewBatchExecutor(agent, config)

	inputs := make([]*core.AgentInput, 20)
	for i := 0; i < 20; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}

	result := executor.Execute(ctx, inputs)

	// Some tasks may fail due to timeout
	assert.Equal(t, 20, result.Stats.TotalCount)
}

// TestBatchExecutor_Stats tests executor statistics
func TestBatchExecutor_Stats(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultBatchConfig()
	executor := NewBatchExecutor(agent, config)

	// First batch
	inputs1 := make([]*core.AgentInput, 5)
	for i := 0; i < 5; i++ {
		inputs1[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}
	result1 := executor.Execute(ctx, inputs1)
	assert.Equal(t, 5, result1.Stats.TotalCount)

	// Second batch
	inputs2 := make([]*core.AgentInput, 3)
	for i := 0; i < 3; i++ {
		inputs2[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i+5),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}
	_ = executor.Execute(ctx, inputs2)

	// Check cumulative stats
	stats := executor.Stats()
	assert.Equal(t, int64(2), stats.TotalExecutions)
	assert.Equal(t, int64(8), stats.TotalTasks)
	assert.Equal(t, int64(8), stats.SuccessTasks)
	assert.Greater(t, stats.AvgTasksPerExec, 0.0)
	assert.Equal(t, 100.0, stats.SuccessRate)
}

// TestBatchExecutor_ExecuteWithCallback tests execution with callback
func TestBatchExecutor_ExecuteWithCallback(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultBatchConfig()
	executor := NewBatchExecutor(agent, config)

	inputs := make([]*core.AgentInput, 5)
	for i := 0; i < 5; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}

	callbackCount := 0
	var callbackMu sync.Mutex

	result := executor.ExecuteWithCallback(ctx, inputs, func(index int, output *core.AgentOutput, err error) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		callbackCount++
	})

	assert.NotNil(t, result)
	assert.Equal(t, 5, callbackCount)
	assert.Equal(t, 5, result.Stats.SuccessCount)
}

// TestBatchExecutor_ExecuteStream tests streaming execution
func TestBatchExecutor_ExecuteStream(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 5*time.Millisecond)
	config := DefaultBatchConfig()
	executor := NewBatchExecutor(agent, config)

	inputs := make([]*core.AgentInput, 10)
	for i := 0; i < 10; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}

	resultChan, errorChan := executor.ExecuteStream(ctx, inputs)

	successCount := 0
	errorCount := 0

	done := false
	for !done {
		select {
		case output, ok := <-resultChan:
			if !ok {
				resultChan = nil
				break
			}
			if output != nil {
				successCount++
			}
		case err, ok := <-errorChan:
			if !ok {
				errorChan = nil
				break
			}
			if err.Error != nil {
				errorCount++
			}
		}

		if resultChan == nil && errorChan == nil {
			done = true
		}
	}

	assert.Equal(t, 10, successCount)
	assert.Equal(t, 0, errorCount)
}

// TestBatchExecutor_ConcurrentAccess tests concurrent batch executions
func TestBatchExecutor_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := BatchConfig{
		MaxConcurrency: 10,
		Timeout:        10 * time.Second,
		ErrorPolicy:    ErrorPolicyContinue,
		EnableStats:    true,
	}
	executor := NewBatchExecutor(agent, config)

	inputs := make([]*core.AgentInput, 20)
	for i := 0; i < 20; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}

	var wg sync.WaitGroup
	results := make(chan *BatchResult, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := executor.Execute(ctx, inputs)
			results <- result
		}()
	}

	wg.Wait()
	close(results)

	totalSuccess := 0
	for result := range results {
		totalSuccess += result.Stats.SuccessCount
	}

	assert.Equal(t, 100, totalSuccess)
}

// TestBatchStats tests batch stats calculation
func TestBatchStats(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 10*time.Millisecond)
	config := DefaultBatchConfig()
	executor := NewBatchExecutor(agent, config)

	inputs := make([]*core.AgentInput, 15)
	for i := 0; i < 15; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}

	result := executor.Execute(ctx, inputs)

	// Check stats calculations
	assert.Equal(t, 15, result.Stats.TotalCount)
	assert.Equal(t, 15, result.Stats.SuccessCount)
	assert.Equal(t, 0, result.Stats.FailureCount)
	assert.Greater(t, result.Stats.AvgDuration, time.Duration(0))
	assert.Greater(t, result.Stats.MinDuration, time.Duration(0))
	assert.Greater(t, result.Stats.MaxDuration, time.Duration(0))
	assert.Greater(t, result.Stats.Duration, time.Duration(0))
}

// TestBatchExecutor_MaxConcurrency tests concurrency limits
func TestBatchExecutor_MaxConcurrency(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 50*time.Millisecond)

	// Track concurrent executions
	maxConcurrent := int32(0)
	currentConcurrent := int32(0)
	trackingAgent := &trackingMockAgent{
		delegate: agent,
		onExecuteStart: func() {
			newVal := atomic.AddInt32(&currentConcurrent, 1)
			for {
				old := atomic.LoadInt32(&maxConcurrent)
				if newVal <= old {
					break
				}
				if atomic.CompareAndSwapInt32(&maxConcurrent, old, newVal) {
					break
				}
			}
		},
		onExecuteEnd: func() {
			atomic.AddInt32(&currentConcurrent, -1)
		},
	}

	config := BatchConfig{
		MaxConcurrency: 5,
		Timeout:        10 * time.Second,
		ErrorPolicy:    ErrorPolicyContinue,
		EnableStats:    true,
	}
	executor := NewBatchExecutor(trackingAgent, config)

	inputs := make([]*core.AgentInput, 20)
	for i := 0; i < 20; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Execute task",
			Timestamp:   time.Now(),
		}
	}

	result := executor.Execute(ctx, inputs)

	assert.Equal(t, 20, result.Stats.TotalCount)
	assert.Equal(t, 20, result.Stats.SuccessCount)
	assert.LessOrEqual(t, maxConcurrent, int32(5))
}

// TestBatchError tests batch error structure
func TestBatchError(t *testing.T) {
	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test",
		Timestamp:   time.Now(),
	}
	err := errors.New("test error")

	batchErr := BatchError{
		Index: 5,
		Input: input,
		Error: err,
	}

	assert.Equal(t, 5, batchErr.Index)
	assert.Equal(t, input, batchErr.Input)
	assert.Equal(t, err, batchErr.Error)
}

// TestExecutorStats tests executor stats
func TestExecutorStats(t *testing.T) {
	stats := ExecutorStats{
		TotalExecutions: 10,
		TotalTasks:      100,
		SuccessTasks:    90,
		FailedTasks:     10,
		AvgTasksPerExec: 10.0,
		SuccessRate:     90.0,
		AvgDuration:     50 * time.Millisecond,
	}

	assert.Equal(t, int64(10), stats.TotalExecutions)
	assert.Equal(t, int64(100), stats.TotalTasks)
	assert.Equal(t, int64(90), stats.SuccessTasks)
	assert.Equal(t, int64(10), stats.FailedTasks)
}

// FailingAgentAfterN is a mock agent that fails after N executions
type FailingAgentAfterN struct {
	*core.BaseAgent
	executeDelay time.Duration
	executeCount int32
	failureCount int32
	mu           sync.Mutex
}

// NewFailingAgentAfterN creates a new failing agent
func NewFailingAgentAfterN(name string, delay time.Duration, failAfterN int32) *FailingAgentAfterN {
	return &FailingAgentAfterN{
		BaseAgent:    core.NewBaseAgent(name, "Mock agent that fails after N executions", []string{"test"}),
		executeDelay: delay,
		failureCount: failAfterN,
	}
}

func (m *FailingAgentAfterN) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	count := atomic.AddInt32(&m.executeCount, 1)
	if m.executeDelay > 0 {
		time.Sleep(m.executeDelay)
	}

	if count > m.failureCount {
		return nil, errors.New("simulated failure")
	}

	return &core.AgentOutput{
		Result:    fmt.Sprintf("Result for task: %s", input.Task),
		Status:    "success",
		Message:   "Execution completed",
		Latency:   m.executeDelay,
		Timestamp: time.Now(),
		Metadata:  map[string]interface{}{"count": count},
	}, nil
}

// trackingMockAgent wraps a mock agent to track concurrent executions
type trackingMockAgent struct {
	delegate          *MockAgent
	onExecuteStart    func()
	onExecuteEnd      func()
	executeCount      int32
	maxConcurrent     int32
	currentConcurrent int32
}

func (t *trackingMockAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	if t.onExecuteStart != nil {
		t.onExecuteStart()
	}
	defer func() {
		if t.onExecuteEnd != nil {
			t.onExecuteEnd()
		}
	}()
	return t.delegate.Invoke(ctx, input)
}

func (t *trackingMockAgent) Name() string {
	return t.delegate.Name()
}

func (t *trackingMockAgent) Description() string {
	return t.delegate.Description()
}

func (t *trackingMockAgent) Capabilities() []string {
	return t.delegate.Capabilities()
}

func (t *trackingMockAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	return t.delegate.Stream(ctx, input)
}

func (t *trackingMockAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	return t.delegate.Batch(ctx, inputs)
}

func (t *trackingMockAgent) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return t.delegate.Pipe(next)
}

func (t *trackingMockAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return t.delegate.WithCallbacks(callbacks...)
}

func (t *trackingMockAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return t.delegate.WithConfig(config)
}
