package tools

import (
	"context"
	"testing"
)

// TestBaseTool 测试基础工具
func TestBaseTool(t *testing.T) {
	tool := NewBaseTool(
		"test_tool",
		"A test tool",
		`{"type": "object"}`,
		func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			return &ToolOutput{
				Result:  "test result",
				Success: true,
			}, nil
		},
	)

	if tool.Name() != "test_tool" {
		t.Errorf("Expected name 'test_tool', got '%s'", tool.Name())
	}

	if tool.Description() != "A test tool" {
		t.Errorf("Expected description 'A test tool', got '%s'", tool.Description())
	}

	ctx := context.Background()
	input := &ToolInput{
		Args:    map[string]interface{}{},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if !output.Success {
		t.Error("Expected success=true")
	}

	if output.Result != "test result" {
		t.Errorf("Expected result 'test result', got '%v'", output.Result)
	}
}

// TestFunctionTool 测试函数工具
func TestFunctionTool(t *testing.T) {
	tool := NewFunctionTool(
		"adder",
		"Adds two numbers",
		`{"type": "object", "properties": {"a": {"type": "number"}, "b": {"type": "number"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			a := args["a"].(float64)
			b := args["b"].(float64)
			return a + b, nil
		},
	)

	ctx := context.Background()
	input := &ToolInput{
		Args: map[string]interface{}{
			"a": 5.0,
			"b": 3.0,
		},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if !output.Success {
		t.Error("Expected success=true")
	}

	if result, ok := output.Result.(float64); !ok || result != 8.0 {
		t.Errorf("Expected result 8.0, got %v", output.Result)
	}
}

// TestToolWithCallbacks 测试工具回调
// NOTE: This test is disabled because WithCallbacks is no longer part of the simplified Tool interface
/*
func TestToolWithCallbacks(t *testing.T) {
	var callbackExecuted bool

	callback := &testCallback{
		onToolStart: func(ctx context.Context, toolName string, input interface{}) error {
			callbackExecuted = true
			if toolName != "test_tool" {
				t.Errorf("Expected toolName 'test_tool', got '%s'", toolName)
			}
			return nil
		},
	}

	tool := NewBaseTool(
		"test_tool",
		"A test tool",
		`{}`,
		func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			return &ToolOutput{Success: true}, nil
		},
	)

	toolWithCallback := tool.WithCallbacks(callback).(Tool)

	ctx := context.Background()
	input := &ToolInput{
		Args:    map[string]interface{}{},
		Context: ctx,
	}

	_, err := toolWithCallback.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if !callbackExecuted {
		t.Error("Callback was not executed")
	}
}
*/

// TestBasicToolInvocation tests basic tool invocation without callbacks
func TestBasicToolInvocation(t *testing.T) {
	tool := NewBaseTool(
		"test_tool",
		"A test tool",
		`{}`,
		func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			return &ToolOutput{
				Success: true,
				Result:  "tool executed",
			}, nil
		},
	)

	ctx := context.Background()
	input := &ToolInput{
		Args:    map[string]interface{}{},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if !output.Success {
		t.Error("Expected success=true")
	}

	if output.Result != "tool executed" {
		t.Errorf("Expected result 'tool executed', got '%v'", output.Result)
	}
}

// testCallback 测试回调实现
// NOTE: Commented out - no longer used after interface simplification
/*
type testCallback struct {
	agentcore.BaseCallback
	onToolStart func(ctx context.Context, toolName string, input interface{}) error
	onToolEnd   func(ctx context.Context, toolName string, output interface{}) error
	onToolError func(ctx context.Context, toolName string, err error) error
}

func (t *testCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	if t.onToolStart != nil {
		return t.onToolStart(ctx, toolName, input)
	}
	return nil
}

func (t *testCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	if t.onToolEnd != nil {
		return t.onToolEnd(ctx, toolName, output)
	}
	return nil
}

func (t *testCallback) OnToolError(ctx context.Context, toolName string, err error) error {
	if t.onToolError != nil {
		return t.onToolError(ctx, toolName, err)
	}
	return nil
}
*/

// BenchmarkFunctionTool 性能测试
func BenchmarkFunctionTool(b *testing.B) {
	tool := NewFunctionTool(
		"adder",
		"Adds numbers",
		`{}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			a := args["a"].(float64)
			b := args["b"].(float64)
			return a + b, nil
		},
	)

	ctx := context.Background()
	input := &ToolInput{
		Args: map[string]interface{}{
			"a": 5.0,
			"b": 3.0,
		},
		Context: ctx,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tool.Invoke(ctx, input)
	}
}
