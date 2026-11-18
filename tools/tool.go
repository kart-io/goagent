package tools

import (
	"context"

	"github.com/kart-io/goagent/interfaces"
)

// BaseTool provides a base implementation of Tool.
//
// It implements the common functionality of the interfaces.Tool interface;
// the concrete execution logic is supplied by the RunFunc function.
type BaseTool struct {
	name        string
	description string
	argsSchema  string
	runFunc     func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error)
}

// NewBaseTool creates a base tool.
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

// Name returns the tool name.
func (t *BaseTool) Name() string {
	return t.name
}

// Description returns the tool description.
func (t *BaseTool) Description() string {
	return t.description
}

// ArgsSchema returns the argument JSON Schema.
func (t *BaseTool) ArgsSchema() string {
	return t.argsSchema
}

// Invoke executes the tool.
func (t *BaseTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	// 执行工具函数
	return t.runFunc(ctx, input)
}

// ToolError is a tool execution error.
type ToolError struct {
	ToolName string
	Message  string
	Err      error
}

// Error implements the error interface.
func (e *ToolError) Error() string {
	if e.Err != nil {
		return e.ToolName + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.ToolName + ": " + e.Message
}

// Unwrap supports errors.Unwrap.
func (e *ToolError) Unwrap() error {
	return e.Err
}

// NewToolError creates a tool error.
func NewToolError(toolName, message string, err error) *ToolError {
	return &ToolError{
		ToolName: toolName,
		Message:  message,
		Err:      err,
	}
}
