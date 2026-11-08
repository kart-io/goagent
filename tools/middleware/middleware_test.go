package middleware

import (
	"context"

	"github.com/kart-io/goagent/interfaces"
)

// mockTool 是用于测试的模拟工具
type mockTool struct {
	name        string
	description string
	argsSchema  string
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return m.description
}

func (m *mockTool) ArgsSchema() string {
	return m.argsSchema
}

func (m *mockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return &interfaces.ToolOutput{
		Result:  "success",
		Success: true,
	}, nil
}

// newMockTool 创建测试用的模拟工具
func newMockTool() *mockTool {
	return &mockTool{
		name:        "test_tool",
		description: "A test tool",
		argsSchema:  `{"type": "object"}`,
	}
}
