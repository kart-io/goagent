package tools

import (
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

/*
工具错误处理规范：

1. 验证错误（输入无效）：
   - 返回: &ToolOutput{Success: false, Error: message}, error（*AgentError，已标记 MarkModelArgument）
   - 示例: return resp.ValidationError("url is required")
   - 使用场景：参数缺失、类型错误、格式错误
   - Dispatcher.Tools() 开启 ReturnArgumentErrors 时会交还模型改正

2. 执行错误（操作失败但有部分结果）：
   - 返回: &ToolOutput{Success: false, Result: partialResult, Error: message}, error
   - 示例: return resp.ExecutionError("request failed", response, err)
   - 使用场景：HTTP 非 2xx 响应、命令执行失败但有输出

3. 执行错误（完全失败）：
   - 返回: &ToolOutput{Success: false, Error: message}, error
   - 示例: return resp.ExecutionError("connection failed", nil, err)
   - 使用场景：网络连接失败、上下文取消、致命错误

4. 成功：
   - 返回: &ToolOutput{Success: true, Result: result}, nil
   - 示例: return resp.Success(result, metadata)
   - 使用场景：操作完全成功

重要：
- 始终返回 ToolOutput 对象（非 nil）以保持向后兼容性
- Success 字段明确标识成功/失败状态
- 错误时同时设置 Error 字段和返回 error
- 唯一例外：Dispatcher.Tools() 开启 ReturnArgumentErrors 时，参数错误以
  &ToolOutput{Success: false, Result: {"error":"invalid_arguments","message":...}}、Error 为空、err 为 nil 返回
*/

// ToolErrorResponse is a unified tool error response builder.
//
// It provides a consistent error handling interface so that all tools return
// errors in a uniform format.
type ToolErrorResponse struct {
	toolName  string
	operation string
}

// NewToolErrorResponse creates an error response builder.
//
// Parameters:
//   - toolName: tool name, used for error tracing and logging
func NewToolErrorResponse(toolName string) *ToolErrorResponse {
	return &ToolErrorResponse{toolName: toolName}
}

// WithOperation sets the operation name.
//
// It identifies the specific operation in the error context to help locate
// problems.
func (r *ToolErrorResponse) WithOperation(op string) *ToolErrorResponse {
	r.operation = op
	return r
}

// ValidationError returns a validation error (invalid input).
//
// Contract: returns &ToolOutput{Success: false, Error: message} plus an
// *AgentError (CodeInvalidInput). The error is marked with MarkModelArgument:
// when Dispatcher.Tools() has ReturnArgumentErrors enabled, only message is
// returned to the model; component, operation and details are not shown to
// the model.
//
// Use cases:
//   - a required argument is missing
//   - an argument has the wrong type
//   - an argument is malformed
//   - an argument value is out of the allowed range
//
// Parameters:
//   - message: error description
//   - details: optional context key-value pairs, as key1, value1, key2, value2, ...
//
// Example:
//
//	return resp.ValidationError("url is required", "field", "url")
func (r *ToolErrorResponse) ValidationError(message string, details ...interface{}) (*interfaces.ToolOutput, error) {
	err := agentErrors.New(agentErrors.CodeInvalidInput, message).
		WithComponent(r.toolName).
		WithOperation(r.operation).
		MarkModelArgument()

	// 添加上下文详情
	for i := 0; i < len(details)-1; i += 2 {
		if key, ok := details[i].(string); ok {
			err = err.WithContext(key, details[i+1])
		}
	}

	output := &interfaces.ToolOutput{
		Success: false,
		Error:   message,
	}

	return output, err
}

// ExecutionError returns an execution error.
//
// Contract:
//   - always returns &ToolOutput{Success: false, Result: partialResult, Error: message} plus an error
//   - partialResult may be nil (complete failure) or contain partial results
//   - the Error field combines message and cause (if cause is present)
//
// Use cases:
//   - an HTTP request returns a non-2xx status code (with a response body)
//   - a shell command fails (with output)
//   - a search engine returns partial results
//   - a network connection fails (no result)
//   - the context is canceled (no result)
//
// Parameters:
//   - message: error description
//   - partialResult: partial result (may be nil)
//   - cause: underlying error (may be nil)
//
// Example:
//
//	// with a partial result
//	return resp.ExecutionError("HTTP request failed", responseData, err)
//
//	// complete failure
//	return resp.ExecutionError("connection failed", nil, err)
func (r *ToolErrorResponse) ExecutionError(message string, partialResult interface{}, cause error) (*interfaces.ToolOutput, error) {
	var err error
	if cause != nil {
		err = agentErrors.Wrap(cause, agentErrors.CodeToolExecution, message).
			WithComponent(r.toolName).
			WithOperation(r.operation)
	} else {
		err = agentErrors.New(agentErrors.CodeToolExecution, message).
			WithComponent(r.toolName).
			WithOperation(r.operation)
	}

	// 组合错误消息：包含 message 和 cause（如果存在）
	errorMsg := message
	if cause != nil {
		errorMsg = message + ": " + cause.Error()
	}

	// 始终返回 ToolOutput 对象，保持向后兼容性
	output := &interfaces.ToolOutput{
		Result:  partialResult,
		Success: false,
		Error:   errorMsg,
	}
	return output, err
}

// Success returns a successful result.
//
// Contract: returns &ToolOutput{Success: true, Result: result} plus a nil error.
//
// Parameters:
//   - result: execution result
//   - metadata: optional metadata (may be nil)
//
// Example:
//
//	return resp.Success(result, map[string]interface{}{
//	    "duration": duration.String(),
//	    "count": len(results),
//	})
func (r *ToolErrorResponse) Success(result interface{}, metadata map[string]interface{}) (*interfaces.ToolOutput, error) {
	return &interfaces.ToolOutput{
		Result:   result,
		Success:  true,
		Metadata: metadata,
	}, nil
}
