package providers

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/interfaces"
)

// MockTool provides a mock implementation of the Tool interface for testing.
type MockTool struct {
	name        string
	description string
	argsSchema  string
	invokeFunc  func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error)
}

// NewMockTool creates a new mock tool with default values.
func NewMockTool(name, description string) *MockTool {
	return &MockTool{
		name:        name,
		description: description,
		argsSchema:  `{"type": "object", "properties": {"location": {"type": "string"}}, "required": ["location"]}`,
	}
}

// Name returns the tool name.
func (m *MockTool) Name() string {
	if m.name == "" {
		return "mock_tool"
	}
	return m.name
}

// Description returns the tool description.
func (m *MockTool) Description() string {
	if m.description == "" {
		return "A mock tool for testing"
	}
	return m.description
}

// ArgsSchema returns the tool's input schema.
func (m *MockTool) ArgsSchema() string {
	if m.argsSchema == "" {
		return `{"type": "object", "properties": {"location": {"type": "string"}}, "required": ["location"]}`
	}
	return m.argsSchema
}

// Invoke executes the tool.
func (m *MockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if m.invokeFunc != nil {
		return m.invokeFunc(ctx, input)
	}
	return &interfaces.ToolOutput{
		Result:  fmt.Sprintf("Mock result from %s", m.Name()),
		Success: true,
	}, nil
}

// SetInvokeFunc sets a custom invoke function.
func (m *MockTool) SetInvokeFunc(fn func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error)) {
	m.invokeFunc = fn
}

// SetArgsSchema sets a custom args schema.
func (m *MockTool) SetArgsSchema(schema string) {
	m.argsSchema = schema
}
