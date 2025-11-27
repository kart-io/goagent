package tools

import (
	"context"

	"github.com/kart-io/goagent/interfaces"
)

// BaseTool 提供 Tool 的基础实现
//
// 实现了 interfaces.Tool 接口的通用功能
// 具体的执行逻辑通过 RunFunc 函数提供
type BaseTool struct {
	name        string
	description string
	argsSchema  string
	runFunc     func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error)
}

// NewBaseTool 创建基础工具
func NewBaseTool(
	name string,
	description string,
	argsSchema string,
	runFunc func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error),
) *BaseTool {
	return &BaseTool{
		name:        name,
		description: description,
		argsSchema:  argsSchema,
		runFunc:     runFunc,
	}
}

// Name 返回工具名称
func (t *BaseTool) Name() string {
	return t.name
}

// Description 返回工具描述
func (t *BaseTool) Description() string {
	return t.description
}

// ArgsSchema 返回参数 JSON Schema
func (t *BaseTool) ArgsSchema() string {
	return t.argsSchema
}

// Invoke 执行工具
func (t *BaseTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	// 执行工具函数
	return t.runFunc(ctx, input)
}

// ToolError 工具执行错误
type ToolError struct {
	ToolName string
	Message  string
	Err      error
}

// Error 实现 error 接口
func (e *ToolError) Error() string {
	if e.Err != nil {
		return e.ToolName + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.ToolName + ": " + e.Message
}

// Unwrap 支持 errors.Unwrap
func (e *ToolError) Unwrap() error {
	return e.Err
}

// NewToolError 创建工具错误
func NewToolError(toolName, message string, err error) *ToolError {
	return &ToolError{
		ToolName: toolName,
		Message:  message,
		Err:      err,
	}
}
