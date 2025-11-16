package toolbox

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/mcp/core"
)

// StandardExecutor 标准工具执行器
type StandardExecutor struct {
	defaultTimeout time.Duration
}

// NewStandardExecutor 创建标准执行器
func NewStandardExecutor() *StandardExecutor {
	return &StandardExecutor{
		defaultTimeout: 30 * time.Second,
	}
}

// Execute 执行工具
func (e *StandardExecutor) Execute(ctx context.Context, tool core.Tool, call *core.ToolCall) (*core.ToolResult, error) {
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
func (e *StandardExecutor) ExecuteWithRetry(ctx context.Context, tool core.Tool, call *core.ToolCall, maxRetries int) (*core.ToolResult, error) {
	var lastErr error
	var result *core.ToolResult

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// 等待一段时间后重试
			time.Sleep(time.Duration(attempt) * time.Second)
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

	return result, fmt.Errorf("execution failed after %d retries: %w", maxRetries, lastErr)
}

// ExecuteWithTimeout 执行工具（带超时）
func (e *StandardExecutor) ExecuteWithTimeout(ctx context.Context, tool core.Tool, call *core.ToolCall) (*core.ToolResult, error) {
	// 创建带超时的上下文
	timeout := e.defaultTimeout
	if call.Context != nil {
		if t, ok := call.Context["timeout"].(time.Duration); ok {
			timeout = t
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 在 goroutine 中执行
	resultChan := make(chan *core.ToolResult, 1)
	errChan := make(chan error, 1)

	go func() {
		result, err := e.Execute(ctx, tool, call)
		resultChan <- result
		errChan <- err
	}()

	// 等待结果或超时
	select {
	case <-ctx.Done():
		return &core.ToolResult{
			Success:   false,
			Error:     "execution timeout",
			ErrorCode: "TIMEOUT_ERROR",
			Timestamp: time.Now(),
		}, fmt.Errorf("execution timeout after %v", timeout)
	case err := <-errChan:
		result := <-resultChan
		return result, err
	}
}

// SetDefaultTimeout 设置默认超时时间
func (e *StandardExecutor) SetDefaultTimeout(timeout time.Duration) {
	e.defaultTimeout = timeout
}
