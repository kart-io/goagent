package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// ToolExecutor 工具执行器
//
// 支持并发和顺序执行工具，提供超时、重试等功能
type ToolExecutor struct {
	// maxConcurrency 最大并发数
	maxConcurrency int

	// timeout 单个工具执行超时
	timeout time.Duration

	// retryPolicy 重试策略
	retryPolicy *RetryPolicy

	// errorHandler 错误处理器
	errorHandler ErrorHandler
}

// ToolCall 工具调用
type ToolCall struct {
	// Tool 要调用的工具
	Tool Tool

	// Input 输入参数
	Input *ToolInput

	// ID 调用标识符（用于追踪）
	ID string

	// Dependencies 依赖的其他工具调用 ID
	Dependencies []string
}

// ToolResult 工具执行结果
type ToolResult struct {
	// CallID 调用标识符
	CallID string

	// Output 输出结果
	Output *ToolOutput

	// Duration 执行耗时
	Duration time.Duration

	// Error 错误信息
	Error error
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	// MaxRetries 最大重试次数
	MaxRetries int

	// InitialDelay 初始延迟
	InitialDelay time.Duration

	// MaxDelay 最大延迟
	MaxDelay time.Duration

	// Multiplier 延迟倍数
	Multiplier float64

	// RetryableErrors 可重试的错误类型
	RetryableErrors []string
}

// ErrorHandler 错误处理器
type ErrorHandler func(call *ToolCall, err error) error

// ExecutorOption 执行器选项
type ExecutorOption func(*ToolExecutor)

// NewToolExecutor 创建工具执行器
func NewToolExecutor(opts ...ExecutorOption) *ToolExecutor {
	executor := &ToolExecutor{
		maxConcurrency: 10,
		timeout:        30 * time.Second,
		retryPolicy: &RetryPolicy{
			MaxRetries:   3,
			InitialDelay: time.Second,
			MaxDelay:     10 * time.Second,
			Multiplier:   2.0,
		},
		errorHandler: defaultErrorHandler,
	}

	for _, opt := range opts {
		opt(executor)
	}

	return executor
}

// WithMaxConcurrency 设置最大并发数
func WithMaxConcurrency(max int) ExecutorOption {
	return func(e *ToolExecutor) {
		if max > 0 {
			e.maxConcurrency = max
		}
	}
}

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) ExecutorOption {
	return func(e *ToolExecutor) {
		if timeout > 0 {
			e.timeout = timeout
		}
	}
}

// WithRetryPolicy 设置重试策略
func WithRetryPolicy(policy *RetryPolicy) ExecutorOption {
	return func(e *ToolExecutor) {
		if policy != nil {
			e.retryPolicy = policy
		}
	}
}

// WithErrorHandler 设置错误处理器
func WithErrorHandler(handler ErrorHandler) ExecutorOption {
	return func(e *ToolExecutor) {
		if handler != nil {
			e.errorHandler = handler
		}
	}
}

// ExecuteParallel 并行执行多个工具
func (e *ToolExecutor) ExecuteParallel(ctx context.Context, calls []*ToolCall) ([]*ToolResult, error) {
	if len(calls) == 0 {
		return []*ToolResult{}, nil
	}

	// 创建结果数组，保持顺序
	results := make([]*ToolResult, len(calls))

	// 创建信号量控制并发
	semaphore := make(chan struct{}, e.maxConcurrency)

	// 使用 WaitGroup 等待所有任务完成
	var wg sync.WaitGroup

	// 并发执行工具
	for i, call := range calls {
		wg.Add(1)
		go func(index int, c *ToolCall) {
			defer wg.Done()

			// Check if context is already cancelled
			select {
			case <-ctx.Done():
				results[index] = &ToolResult{
					CallID: c.ID,
					Error:  ctx.Err(),
				}
				return
			default:
			}

			// 获取信号量
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results[index] = &ToolResult{
					CallID: c.ID,
					Error:  ctx.Err(),
				}
				return
			}

			// 执行工具 - pass context to respect timeout
			result := e.executeWithRetry(ctx, c)
			results[index] = result
		}(i, call)
	}

	// 等待所有任务完成或context取消
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All tasks completed
	case <-ctx.Done():
		// Context cancelled, wait for goroutines to finish
		wg.Wait()
		return results, ctx.Err()
	}

	// 检查是否有执行失败的工具
	var hasError bool
	for _, result := range results {
		if result.Error != nil {
			hasError = true
			break
		}
	}

	// 如果有工具执行失败，返回错误
	if hasError {
		return results, agentErrors.New(agentErrors.CodeToolExecution, "one or more tools failed execution").
			WithComponent("tool_executor").
			WithOperation("execute_parallel")
	}

	return results, nil
}

// ExecuteSequential 顺序执行工具
func (e *ToolExecutor) ExecuteSequential(ctx context.Context, calls []*ToolCall) ([]*ToolResult, error) {
	results := make([]*ToolResult, 0, len(calls))

	for _, call := range calls {
		result := e.executeWithRetry(ctx, call)
		results = append(results, result)

		// 如果发生错误且不继续执行，则停止
		if result.Error != nil {
			return results, result.Error
		}
	}

	return results, nil
}

// ExecuteWithDependencies 根据依赖关系执行
func (e *ToolExecutor) ExecuteWithDependencies(ctx context.Context, graph *ToolGraph) ([]*ToolResult, error) {
	if graph == nil {
		return nil, agentErrors.New(agentErrors.CodeToolValidation, "tool graph is nil").
			WithComponent("tool_executor").
			WithOperation("execute_with_dependencies")
	}

	// 拓扑排序
	sorted, err := graph.TopologicalSort()
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to sort dependencies").
			WithComponent("tool_executor").
			WithOperation("execute_with_dependencies")
	}

	// 存储结果
	resultMap := make(map[string]*ToolResult)
	results := make([]*ToolResult, 0)

	// 按依赖顺序执行
	for _, nodeID := range sorted {
		node := graph.GetNode(nodeID)
		if node == nil {
			continue
		}

		// 检查依赖是否都成功
		allDepsSucceeded := true
		for _, depID := range node.Dependencies {
			if result, ok := resultMap[depID]; ok {
				if result.Error != nil {
					allDepsSucceeded = false
					break
				}
			}
		}

		if !allDepsSucceeded {
			// 如果依赖失败，跳过此工具
			result := &ToolResult{
				CallID: node.ID,
				Error: agentErrors.New(agentErrors.CodeToolExecution, "skipped due to failed dependencies").
					WithComponent("tool_executor").
					WithOperation("execute_with_dependencies").
					WithContext("node_id", node.ID),
				Duration: 0,
			}
			resultMap[node.ID] = result
			results = append(results, result)
			continue
		}

		// 执行工具
		call := &ToolCall{
			Tool:         node.Tool,
			Input:        node.Input,
			ID:           node.ID,
			Dependencies: node.Dependencies,
		}

		result := e.executeWithRetry(ctx, call)
		resultMap[node.ID] = result
		results = append(results, result)
	}

	return results, nil
}

// executeWithRetry 执行工具并支持重试
func (e *ToolExecutor) executeWithRetry(ctx context.Context, call *ToolCall) *ToolResult {
	var lastError error
	startTime := time.Now()

	// 首次尝试 + 重试次数
	maxAttempts := 1
	if e.retryPolicy != nil {
		maxAttempts += e.retryPolicy.MaxRetries
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// 创建带超时的上下文
		execCtx, cancel := context.WithTimeout(ctx, e.timeout)

		// 执行工具
		output, err := e.executeSingle(execCtx, call)
		cancel()

		if err == nil {
			// 成功执行
			return &ToolResult{
				CallID:   call.ID,
				Output:   output,
				Duration: time.Since(startTime),
				Error:    nil,
			}
		}

		lastError = err

		// 检查是否应该重试
		if attempt < maxAttempts-1 && e.shouldRetry(err) {
			// 计算重试延迟
			delay := e.calculateRetryDelay(attempt)
			time.Sleep(delay)
			continue
		}

		break
	}

	// 所有尝试都失败
	return &ToolResult{
		CallID:   call.ID,
		Output:   nil,
		Duration: time.Since(startTime),
		Error:    e.errorHandler(call, lastError),
	}
}

// executeSingle 执行单个工具
func (e *ToolExecutor) executeSingle(ctx context.Context, call *ToolCall) (*ToolOutput, error) {
	if call.Tool == nil {
		return nil, agentErrors.New(agentErrors.CodeToolValidation, "tool is nil").
			WithComponent("tool_executor").
			WithOperation("execute_single")
	}

	if call.Input == nil {
		call.Input = &ToolInput{
			Args:    make(map[string]interface{}),
			Context: ctx,
		}
	}

	// 确保输入有上下文
	call.Input.Context = ctx

	// 执行工具
	output, err := call.Tool.Invoke(ctx, call.Input)
	if err != nil {
		return nil, err
	}

	return output, nil
}

// shouldRetry 判断是否应该重试
func (e *ToolExecutor) shouldRetry(err error) bool {
	if e.retryPolicy == nil {
		return false
	}

	// TODO: 检查错误类型是否在可重试列表中
	// 当前简化版本：所有错误都可重试
	return true
}

// calculateRetryDelay 计算重试延迟
func (e *ToolExecutor) calculateRetryDelay(attempt int) time.Duration {
	if e.retryPolicy == nil {
		return time.Second
	}

	delay := float64(e.retryPolicy.InitialDelay)
	for i := 0; i < attempt; i++ {
		delay *= e.retryPolicy.Multiplier
	}

	finalDelay := time.Duration(delay)
	if finalDelay > e.retryPolicy.MaxDelay {
		finalDelay = e.retryPolicy.MaxDelay
	}

	return finalDelay
}

// defaultErrorHandler 默认错误处理器
func defaultErrorHandler(call *ToolCall, err error) error {
	if err == nil {
		return nil
	}
	return agentErrors.Wrap(err, agentErrors.CodeToolExecution, "tool execution failed").
		WithComponent("tool_executor").
		WithOperation("default_error_handler").
		WithContext("tool_name", call.Tool.Name()).
		WithContext("call_id", call.ID)
}

// ExecuteBatch 批量执行相同工具的不同输入
func (e *ToolExecutor) ExecuteBatch(ctx context.Context, tool Tool, inputs []*ToolInput) ([]*ToolResult, error) {
	calls := make([]*ToolCall, len(inputs))
	for i, input := range inputs {
		calls[i] = &ToolCall{
			Tool:  tool,
			Input: input,
			ID:    fmt.Sprintf("%s_%d", tool.Name(), i),
		}
	}

	return e.ExecuteParallel(ctx, calls)
}
