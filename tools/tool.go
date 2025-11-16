package tools

import (
	"context"

	"github.com/kart-io/goagent/interfaces"
)

// Tool is a type alias for interfaces.Tool
//
// Deprecated: Use interfaces.Tool directly for new code.
// This alias provides backward compatibility and will be removed in v1.0.0.
//
// Migration: import "github.com/kart-io/goagent/interfaces"
type Tool = interfaces.Tool

// ToolInput 工具输入
//
// Deprecated: Use interfaces.ToolInput instead for new code.
// This type alias provides backward compatibility. It will be removed in v1.0.0.
//
// Note: This struct is retained for compatibility with existing tool implementations
// that reference tools.ToolInput. The canonical definition is now in interfaces.ToolInput.
type ToolInput = interfaces.ToolInput

// ToolOutput 工具输出
//
// Deprecated: Use interfaces.ToolOutput instead for new code.
// This type alias provides backward compatibility. It will be removed in v1.0.0.
//
// Note: This struct is retained for compatibility with existing tool implementations
// that reference tools.ToolOutput. The canonical definition is now in interfaces.ToolOutput.
type ToolOutput = interfaces.ToolOutput

// BaseTool 提供 Tool 的基础实现
//
// 实现了 interfaces.Tool 接口的通用功能
// 具体的执行逻辑通过 RunFunc 函数提供
type BaseTool struct {
	name        string
	description string
	argsSchema  string
	runFunc     func(context.Context, *ToolInput) (*ToolOutput, error)
}

// NewBaseTool 创建基础工具
func NewBaseTool(
	name string,
	description string,
	argsSchema string,
	runFunc func(context.Context, *ToolInput) (*ToolOutput, error),
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
func (t *BaseTool) Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
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
