package performance

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
)

// BatchConfig 批量执行配置
type BatchConfig struct {
	// MaxConcurrency 最大并发数
	MaxConcurrency int
	// Timeout 批量执行超时时间
	Timeout time.Duration
	// ErrorPolicy 错误处理策略
	ErrorPolicy ErrorPolicy
	// EnableStats 是否启用统计
	EnableStats bool
}

// ErrorPolicy 错误处理策略
type ErrorPolicy string

const (
	// ErrorPolicyFailFast 快速失败（遇到第一个错误就停止）
	ErrorPolicyFailFast ErrorPolicy = "fail_fast"
	// ErrorPolicyContinue 继续执行（收集所有错误）
	ErrorPolicyContinue ErrorPolicy = "continue"
)

// DefaultBatchConfig 返回默认批量执行配置
func DefaultBatchConfig() BatchConfig {
	return BatchConfig{
		MaxConcurrency: 10,
		Timeout:        5 * time.Minute,
		ErrorPolicy:    ErrorPolicyContinue,
		EnableStats:    true,
	}
}

// BatchInput 批量输入
type BatchInput struct {
	Inputs []*core.AgentInput
	Config BatchConfig
}

// BatchResult 批量执行结果
type BatchResult struct {
	Results []*core.AgentOutput // 成功的结果
	Errors  []BatchError        // 错误列表
	Stats   BatchStats          // 统计信息
}

// BatchError 批量执行错误
type BatchError struct {
	Index int              // 输入索引
	Input *core.AgentInput // 输入
	Error error            // 错误
}

// BatchStats 批量执行统计
type BatchStats struct {
	TotalCount   int           // 总任务数
	SuccessCount int           // 成功数
	FailureCount int           // 失败数
	Duration     time.Duration // 总耗时
	AvgDuration  time.Duration // 平均耗时
	MinDuration  time.Duration // 最小耗时
	MaxDuration  time.Duration // 最大耗时
}

// BatchExecutor 批量执行器
type BatchExecutor struct {
	agent  core.Agent
	config BatchConfig

	// 统计信息
	stats batchStats
}

// batchStats 批量执行统计
type batchStats struct {
	totalExecutions atomic.Int64 // 总执行次数
	totalTasks      atomic.Int64 // 总任务数
	successTasks    atomic.Int64 // 成功任务数
	failedTasks     atomic.Int64 // 失败任务数
	totalDurationNs atomic.Int64 // 总耗时（纳秒）
}

// NewBatchExecutor 创建新的批量执行器
func NewBatchExecutor(agent core.Agent, config BatchConfig) *BatchExecutor {
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 10
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Minute
	}
	if config.ErrorPolicy == "" {
		config.ErrorPolicy = ErrorPolicyContinue
	}

	return &BatchExecutor{
		agent:  agent,
		config: config,
	}
}

// batchTask represents a single task in the batch work queue.
type batchTask struct {
	index int
	input *core.AgentInput
}

// Execute 执行批量任务
// Uses a worker pool pattern: launches only MaxConcurrency workers that pull
// work from a shared channel, instead of spawning len(inputs) goroutines.
func (b *BatchExecutor) Execute(ctx context.Context, inputs []*core.AgentInput) *BatchResult {
	startTime := time.Now()

	// Handle empty input case
	if len(inputs) == 0 {
		return &BatchResult{
			Results: make([]*core.AgentOutput, 0),
			Errors:  make([]BatchError, 0),
			Stats: BatchStats{
				TotalCount:   0,
				SuccessCount: 0,
				FailureCount: 0,
				Duration:     time.Since(startTime),
			},
		}
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, b.config.Timeout)
	defer cancel()

	// Update statistics
	b.stats.totalExecutions.Add(1)
	b.stats.totalTasks.Add(int64(len(inputs)))

	// Result collection
	results := make([]*core.AgentOutput, len(inputs))
	durations := make([]time.Duration, len(inputs))
	var resultsMu sync.Mutex

	// Error collection
	errors := make([]BatchError, 0)
	var errorsMu sync.Mutex

	// Stop flag for fail-fast mode
	var stopFlag atomic.Bool

	// Work queue: buffer size equals number of inputs to avoid blocking sender
	workQueue := make(chan batchTask, len(inputs))

	// Result channels for collecting outcomes
	// Buffer size is number of inputs to avoid blocking workers
	resultChan := make(chan batchTaskResult, len(inputs))
	errorChan := make(chan BatchError, len(inputs))

	// Determine actual worker count (min of MaxConcurrency and input count)
	workerCount := b.config.MaxConcurrency
	if len(inputs) < workerCount {
		workerCount = len(inputs)
	}

	// Start worker pool
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.worker(timeoutCtx, workQueue, resultChan, errorChan, &stopFlag)
		}()
	}

	// Send tasks to work queue
	go func() {
		defer close(workQueue)
		for i, input := range inputs {
			// Check if we should stop sending (fail-fast mode)
			if b.config.ErrorPolicy == ErrorPolicyFailFast && stopFlag.Load() {
				break
			}

			// Check context cancellation
			select {
			case <-timeoutCtx.Done():
				return
			case workQueue <- batchTask{index: i, input: input}:
			}
		}
	}()

	// Close result channels after all workers complete
	go func() {
		wg.Wait()
		close(resultChan)
		close(errorChan)
	}()

	// Collect results and errors concurrently
	var collectWg sync.WaitGroup
	collectWg.Add(2)

	// Collect results
	go func() {
		defer collectWg.Done()
		for result := range resultChan {
			resultsMu.Lock()
			results[result.Index] = result.Output
			durations[result.Index] = result.Duration
			resultsMu.Unlock()
		}
	}()

	// Collect errors
	go func() {
		defer collectWg.Done()
		for err := range errorChan {
			errorsMu.Lock()
			errors = append(errors, err)
			errorsMu.Unlock()
		}
	}()

	// Wait for all collection to complete
	collectWg.Wait()

	// Calculate statistics
	totalDuration := time.Since(startTime)
	b.stats.totalDurationNs.Add(int64(totalDuration))

	stats := b.calculateStats(len(inputs), len(errors), totalDuration, durations)

	return &BatchResult{
		Results: results,
		Errors:  errors,
		Stats:   stats,
	}
}

// worker processes tasks from the work queue until it's closed or stopFlag is set.
func (b *BatchExecutor) worker(
	ctx context.Context,
	workQueue <-chan batchTask,
	resultChan chan<- batchTaskResult,
	errorChan chan<- BatchError,
	stopFlag *atomic.Bool,
) {
	for task := range workQueue {
		// Check stop flag before processing
		if stopFlag.Load() {
			continue // Drain remaining tasks without processing
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			errorChan <- BatchError{
				Index: task.index,
				Input: task.input,
				Error: ctx.Err(),
			}
			continue
		default:
		}

		// Execute task
		taskStart := time.Now()
		output, err := b.agent.Invoke(ctx, task.input)
		taskDuration := time.Since(taskStart)

		if err != nil {
			b.stats.failedTasks.Add(1)
			errorChan <- BatchError{
				Index: task.index,
				Input: task.input,
				Error: err,
			}

			// Set stop flag for fail-fast mode
			if b.config.ErrorPolicy == ErrorPolicyFailFast {
				stopFlag.Store(true)
			}
			continue
		}

		b.stats.successTasks.Add(1)
		resultChan <- batchTaskResult{
			Index:    task.index,
			Output:   output,
			Duration: taskDuration,
		}
	}
}

// ExecuteWithCallback 执行批量任务（带回调）
func (b *BatchExecutor) ExecuteWithCallback(
	ctx context.Context,
	inputs []*core.AgentInput,
	callback func(index int, output *core.AgentOutput, err error),
) *BatchResult {
	result := b.Execute(ctx, inputs)

	// 调用回调
	for i, output := range result.Results {
		if output != nil {
			callback(i, output, nil)
		}
	}
	for _, batchErr := range result.Errors {
		callback(batchErr.Index, nil, batchErr.Error)
	}

	return result
}

// Stats 返回批量执行器的统计信息
func (b *BatchExecutor) Stats() ExecutorStats {
	totalExecs := b.stats.totalExecutions.Load()
	totalTasks := b.stats.totalTasks.Load()
	successTasks := b.stats.successTasks.Load()
	failedTasks := b.stats.failedTasks.Load()

	var avgTasksPerExec float64
	if totalExecs > 0 {
		avgTasksPerExec = float64(totalTasks) / float64(totalExecs)
	}

	var successRate float64
	if totalTasks > 0 {
		successRate = float64(successTasks) / float64(totalTasks) * 100
	}

	var avgDuration time.Duration
	if totalExecs > 0 {
		avgDuration = time.Duration(b.stats.totalDurationNs.Load() / totalExecs)
	}

	return ExecutorStats{
		TotalExecutions: totalExecs,
		TotalTasks:      totalTasks,
		SuccessTasks:    successTasks,
		FailedTasks:     failedTasks,
		AvgTasksPerExec: avgTasksPerExec,
		SuccessRate:     successRate,
		AvgDuration:     avgDuration,
	}
}

// ExecutorStats 执行器统计信息
type ExecutorStats struct {
	TotalExecutions int64         // 总执行次数
	TotalTasks      int64         // 总任务数
	SuccessTasks    int64         // 成功任务数
	FailedTasks     int64         // 失败任务数
	AvgTasksPerExec float64       // 平均每次执行的任务数
	SuccessRate     float64       // 成功率百分比
	AvgDuration     time.Duration // 平均执行时间
}

// batchTaskResult 批量任务结果
type batchTaskResult struct {
	Index    int
	Output   *core.AgentOutput
	Duration time.Duration
}

// calculateStats 计算统计信息
func (b *BatchExecutor) calculateStats(
	totalCount, errorCount int,
	totalDuration time.Duration,
	durations []time.Duration,
) BatchStats {
	successCount := totalCount - errorCount

	var sumDuration time.Duration
	var minDuration, maxDuration time.Duration

	for i, d := range durations {
		if d > 0 {
			sumDuration += d
			if i == 0 || d < minDuration {
				minDuration = d
			}
			if d > maxDuration {
				maxDuration = d
			}
		}
	}

	var avgDuration time.Duration
	if successCount > 0 {
		avgDuration = sumDuration / time.Duration(successCount)
	}

	return BatchStats{
		TotalCount:   totalCount,
		SuccessCount: successCount,
		FailureCount: errorCount,
		Duration:     totalDuration,
		AvgDuration:  avgDuration,
		MinDuration:  minDuration,
		MaxDuration:  maxDuration,
	}
}

// ExecuteStream 流式执行批量任务（返回结果通道）
func (b *BatchExecutor) ExecuteStream(
	ctx context.Context,
	inputs []*core.AgentInput,
) (<-chan *core.AgentOutput, <-chan BatchError) {
	resultChan := make(chan *core.AgentOutput, len(inputs))
	errorChan := make(chan BatchError, len(inputs))

	go func() {
		defer close(resultChan)
		defer close(errorChan)

		result := b.Execute(ctx, inputs)

		// 发送结果
		for _, output := range result.Results {
			if output != nil {
				select {
				case resultChan <- output:
				case <-ctx.Done():
					return
				}
			}
		}

		// 发送错误
		for _, err := range result.Errors {
			select {
			case errorChan <- err:
			case <-ctx.Done():
				return
			}
		}
	}()

	return resultChan, errorChan
}
