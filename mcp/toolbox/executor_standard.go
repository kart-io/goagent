package toolbox

import (
	"context"
	"math/rand"
	"time"

	coreTimeout "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/mcp/core"
)

// 重试策略常量
const (
	baseRetryDelay = 500 * time.Millisecond // 基础重试延迟
	maxRetryDelay  = 30 * time.Second       // 最大重试延迟
	jitterFactor   = 0.3                    // 抖动因子（30%）
)

// StandardExecutor 标准工具执行器
type StandardExecutor struct {
	defaultTimeout time.Duration
}

// NewStandardExecutor 创建标准执行器
func NewStandardExecutor() *StandardExecutor {
	return &StandardExecutor{
		defaultTimeout: coreTimeout.DefaultToolTimeout,
	}
}

// Execute 执行工具
func (e *StandardExecutor) Execute(ctx context.Context, tool core.MCPTool, call *core.ToolCall) (*core.ToolResult, error) {
	startTime := time.Now()

	// 验证输入
	if err := tool.Validate(call.Input); err != nil {
		return &core.ToolResult{
			Success:   false,
			Error:     err.Error(),
			ErrorCode: "VALIDATION_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, err
	}

	// 执行工具
	result, err := tool.Execute(ctx, call.Input)
	if err != nil {
		result = &core.ToolResult{
			Success:   false,
			Error:     err.Error(),
			ErrorCode: "EXECUTION_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}
		return result, err
	}

	// 确保设置了时间戳和耗时
	if result.Timestamp.IsZero() {
		result.Timestamp = time.Now()
	}
	result.Duration = time.Since(startTime)

	return result, nil
}

// ExecuteWithRetry 执行工具（带重试）
//
// 使用指数退避 + 抖动策略：
// - 基础延迟：500ms
// - 指数增长：delay = baseDelay * 2^attempt
// - 最大延迟：30s
// - 抖动：±30% 随机偏移，防止惊群效应
func (e *StandardExecutor) ExecuteWithRetry(ctx context.Context, tool core.MCPTool, call *core.ToolCall, maxRetries int) (*core.ToolResult, error) {
	var lastErr error
	var result *core.ToolResult

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// 计算指数退避延迟
			delay := e.calculateRetryDelay(attempt)

			// 等待或响应取消
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		result, lastErr = e.Execute(ctx, tool, call)
		if lastErr == nil && result.Success {
			return result, nil
		}

		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	return result, agentErrors.Wrap(lastErr, agentErrors.CodeAgentExecution, "execution failed after retries").
		WithComponent("standard_executor").
		WithOperation("execute_with_retry").
		WithContext("max_retries", maxRetries)
}

// calculateRetryDelay 计算重试延迟（指数退避 + 抖动）
func (e *StandardExecutor) calculateRetryDelay(attempt int) time.Duration {
	// 指数退避：baseDelay * 2^attempt
	delay := baseRetryDelay * time.Duration(1<<uint(attempt))

	// 限制最大延迟
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}

	// 添加抖动：±jitterFactor 的随机偏移
	jitter := time.Duration(float64(delay) * jitterFactor * (rand.Float64()*2 - 1))
	delay += jitter

	// 确保延迟不为负
	if delay < 0 {
		delay = baseRetryDelay
	}

	return delay
}

// ExecuteWithTimeout 执行工具（带超时）
//
// 修复说明：使用带缓冲的 channel 和 context 取消检测，
// 确保 goroutine 在超时后能正确退出，避免资源泄漏。
func (e *StandardExecutor) ExecuteWithTimeout(ctx context.Context, tool core.MCPTool, call *core.ToolCall) (*core.ToolResult, error) {
	// 创建带超时的上下文
	timeout := e.defaultTimeout
	if call.Context != nil {
		if t, ok := call.Context["timeout"].(time.Duration); ok {
			timeout = t
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 使用带缓冲的 channel，确保 goroutine 能正常退出
	type executeResult struct {
		result *core.ToolResult
		err    error
	}
	resultChan := make(chan executeResult, 1)

	go func() {
		result, err := e.Execute(ctx, tool, call)
		// 使用 select 检查 context 是否已取消
		// 如果已取消，直接退出，不阻塞在 channel 发送上
		select {
		case resultChan <- executeResult{result: result, err: err}:
		case <-ctx.Done():
			// context 已取消，直接退出
			return
		}
	}()

	// 等待结果或超时
	select {
	case <-ctx.Done():
		return &core.ToolResult{
				Success:   false,
				Error:     "execution timeout",
				ErrorCode: "TIMEOUT_ERROR",
				Timestamp: time.Now(),
			}, agentErrors.New(agentErrors.CodeContextTimeout, "execution timeout").
				WithComponent("standard_executor").
				WithOperation("execute_with_timeout").
				WithContext("timeout", timeout.String())
	case res := <-resultChan:
		return res.result, res.err
	}
}

// SetDefaultTimeout 设置默认超时时间
func (e *StandardExecutor) SetDefaultTimeout(timeout time.Duration) {
	e.defaultTimeout = timeout
}
