package tools

import (
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

/*
工具错误处理规范：

1. 验证错误（输入无效）：
   - 返回: &ToolOutput{Success: false, Error: message}, error
   - 示例: return resp.ValidationError("url is required")
   - 使用场景：参数缺失、类型错误、格式错误

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
*/

// ToolErrorResponse 统一的工具错误响应构建器
//
// 提供一致的错误处理接口，确保所有工具返回统一格式的错误
type ToolErrorResponse struct {
	toolName  string
	operation string
}

// NewToolErrorResponse 创建错误响应构建器
//
// Parameters:
//   - toolName: 工具名称，用于错误追踪和日志记录
func NewToolErrorResponse(toolName string) *ToolErrorResponse {
	return &ToolErrorResponse{toolName: toolName}
}

// WithOperation 设置操作名称
//
// 用于在错误上下文中标识具体的操作，便于问题定位
func (r *ToolErrorResponse) WithOperation(op string) *ToolErrorResponse {
	r.operation = op
	return r
}

// ValidationError 返回验证错误（输入无效）
//
// 规范：返回 &ToolOutput{Success: false, Error: message} + error
//
// 使用场景：
//   - 必需参数缺失
//   - 参数类型错误
//   - 参数格式不正确
//   - 参数值超出允许范围
//
// Parameters:
//   - message: 错误描述信息
//   - details: 可选的上下文键值对，格式为 key1, value1, key2, value2, ...
//
// Example:
//
//	return resp.ValidationError("url is required", "field", "url")
func (r *ToolErrorResponse) ValidationError(message string, details ...interface{}) (*interfaces.ToolOutput, error) {
	err := agentErrors.New(agentErrors.CodeInvalidInput, message).
		WithComponent(r.toolName).
		WithOperation(r.operation)

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

// ExecutionError 返回执行错误
//
// 规范：
//   - 始终返回 &ToolOutput{Success: false, Result: partialResult, Error: message} + error
//   - partialResult 可以为 nil（完全失败）或包含部分结果
//   - Error 字段包含 message 和 cause 的组合（如果 cause 存在）
//
// 使用场景：
//   - HTTP 请求返回非 2xx 状态码（有响应体）
//   - Shell 命令执行失败（有输出）
//   - 搜索引擎返回部分结果
//   - 网络连接失败（无结果）
//   - 上下文取消（无结果）
//
// Parameters:
//   - message: 错误描述信息
//   - partialResult: 部分结果（可为 nil）
//   - cause: 底层错误（可为 nil）
//
// Example:
//
//	// 有部分结果的情况
//	return resp.ExecutionError("HTTP request failed", responseData, err)
//
//	// 完全失败的情况
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

// Success 返回成功结果
//
// 规范：返回 &ToolOutput{Success: true, Result: result} + nil error
//
// Parameters:
//   - result: 执行结果
//   - metadata: 可选的元数据（可为 nil）
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
