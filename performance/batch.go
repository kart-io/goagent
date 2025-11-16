package performance

import (
	"context"
	stderrors "errors"
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

// Execute 执行批量任务
func (b *BatchExecutor) Execute(ctx context.Context, inputs []*core.AgentInput) *BatchResult {
	startTime := time.Now()

	// 创建带超时的上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, b.config.Timeout)
	defer cancel()

	// 统计信息
	b.stats.totalExecutions.Add(1)
	b.stats.totalTasks.Add(int64(len(inputs)))

	// 结果收集
	results := make([]*core.AgentOutput, len(inputs))
	errors := make([]BatchError, 0)
	durations := make([]time.Duration, len(inputs))

	// 工作协程池
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, b.config.MaxConcurrency)
	resultChan := make(chan batchTaskResult, len(inputs))
	errorChan := make(chan BatchError, len(inputs))

	// 错误停止标志（用于 fail-fast）
	var stopFlag atomic.Bool
	stopFlag.Store(false)

	// 启动任务
	for i, input := range inputs {
		// 检查是否需要停止
		if b.config.ErrorPolicy == ErrorPolicyFailFast && stopFlag.Load() {
			break
		}

		wg.Add(1)
		go func(index int, inp *core.AgentInput) {
			defer wg.Done()

			// 获取信号量
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-timeoutCtx.Done():
				errorChan <- BatchError{
					Index: index,
					Input: inp,
					Error: stderrors.New("timeout waiting for semaphore"),
				}
				return
			}

			// 检查停止标志
			if stopFlag.Load() {
				return
			}

			// 执行任务
			taskStart := time.Now()
			output, err := b.agent.Invoke(timeoutCtx, inp)
			taskDuration := time.Since(taskStart)

			if err != nil {
				b.stats.failedTasks.Add(1)
				batchErr := BatchError{
					Index: index,
					Input: inp,
					Error: err,
				}
				errorChan <- batchErr

				// 如果是 fail-fast 模式，设置停止标志
				if b.config.ErrorPolicy == ErrorPolicyFailFast {
					stopFlag.Store(true)
				}
				return
			}

			b.stats.successTasks.Add(1)
			resultChan <- batchTaskResult{
				Index:    index,
				Output:   output,
				Duration: taskDuration,
			}
		}(i, input)
	}

	// 等待所有任务完成
	go func() {
		wg.Wait()
		close(resultChan)
		close(errorChan)
	}()

	// 收集结果
	for result := range resultChan {
		results[result.Index] = result.Output
		durations[result.Index] = result.Duration
	}

	// 收集错误
	for err := range errorChan {
		errors = append(errors, err)
	}

	// 计算统计信息
	totalDuration := time.Since(startTime)
	b.stats.totalDurationNs.Add(int64(totalDuration))

	stats := b.calculateStats(len(inputs), len(errors), totalDuration, durations)

	return &BatchResult{
		Results: results,
		Errors:  errors,
		Stats:   stats,
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
