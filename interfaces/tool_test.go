package interfaces

import (
	"context"
	"testing"
)

// TestToolInputStructure verifies ToolInput struct is properly defined
func TestToolInputStructure(t *testing.T) {
	ctx := context.Background()

	toolInput := &ToolInput{
		Args: map[string]interface{}{
			"query":  "search term",
			"limit":  10,
			"filter": map[string]interface{}{"category": "tech"},
		},
		Context:  ctx,
		CallerID: "agent-123",
		TraceID:  "trace-abc-xyz",
	}

	if toolInput.Args["query"] != "search term" {
		t.Errorf("Expected query 'search term', got '%v'", toolInput.Args["query"])
	}
	if toolInput.Args["limit"] != 10 {
		t.Errorf("Expected limit 10, got '%v'", toolInput.Args["limit"])
	}
	if toolInput.CallerID != "agent-123" {
		t.Errorf("Expected CallerID 'agent-123', got '%s'", toolInput.CallerID)
	}
	if toolInput.TraceID != "trace-abc-xyz" {
		t.Errorf("Expected TraceID 'trace-abc-xyz', got '%s'", toolInput.TraceID)
	}
	if toolInput.Context != ctx {
		t.Error("Context mismatch")
	}
}

// TestToolOutputStructure verifies ToolOutput struct is properly defined
func TestToolOutputStructure(t *testing.T) {
	tests := []struct {
		name   string
		output *ToolOutput
	}{
		{
			name: "successful execution",
			output: &ToolOutput{
				Result:  "Operation completed successfully",
				Success: true,
				Error:   "",
				Metadata: map[string]interface{}{
					"execution_time": 123,
					"retries":        0,
				},
			},
		},
		{
			name: "failed execution",
			output: &ToolOutput{
				Result:  nil,
				Success: false,
				Error:   "Connection timeout",
				Metadata: map[string]interface{}{
					"retries": 3,
					"timeout": 5000,
				},
			},
		},
		{
			name: "structured result",
			output: &ToolOutput{
				Result: map[string]interface{}{
					"status": "ok",
					"data":   []int{1, 2, 3, 4, 5},
				},
				Success: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.output.Success && tt.output.Error != "" {
				t.Error("Success=true should have empty Error")
			}
			if !tt.output.Success && tt.output.Error == "" {
				t.Error("Success=false should have non-empty Error")
			}
		})
	}
}

// TestToolCallStructure verifies ToolCall struct is properly defined
func TestToolCallStructure(t *testing.T) {
	startTime := int64(1000000)
	endTime := int64(1000250)

	toolCall := &ToolCall{
		ID:       "call-123",
		ToolName: "calculator",
		Args: map[string]interface{}{
			"operation": "add",
			"a":         5,
			"b":         3,
		},
		Result: &ToolOutput{
			Result:  8,
			Success: true,
		},
		Error:     "",
		StartTime: startTime,
		EndTime:   endTime,
		Metadata: map[string]interface{}{
			"caller": "agent-456",
		},
	}

	if toolCall.ID != "call-123" {
		t.Errorf("Expected ID 'call-123', got '%s'", toolCall.ID)
	}
	if toolCall.ToolName != "calculator" {
		t.Errorf("Expected ToolName 'calculator', got '%s'", toolCall.ToolName)
	}
	if toolCall.StartTime != startTime {
		t.Errorf("Expected StartTime %d, got %d", startTime, toolCall.StartTime)
	}
	if toolCall.EndTime != endTime {
		t.Errorf("Expected EndTime %d, got %d", endTime, toolCall.EndTime)
	}

	duration := toolCall.EndTime - toolCall.StartTime
	if duration != 250 {
		t.Errorf("Expected duration 250ms, got %d", duration)
	}
}

// mockTool is a minimal test implementation of Tool
type mockTool struct {
	name        string
	description string
	schema      string
	result      interface{}
	shouldError bool
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return m.description
}

func (m *mockTool) Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
	if m.shouldError {
		return &ToolOutput{
			Result:  nil,
			Success: false,
			Error:   "Tool execution failed",
		}, nil
	}

	return &ToolOutput{
		Result:  m.result,
		Success: true,
		Metadata: map[string]interface{}{
			"tool_name": m.name,
		},
	}, nil
}

func (m *mockTool) ArgsSchema() string {
	return m.schema
}

// Ensure mockTool implements Tool interface
var _ Tool = (*mockTool)(nil)

// TestToolInterface verifies the Tool interface works correctly
func TestToolInterface(t *testing.T) {
	ctx := context.Background()

	tool := &mockTool{
		name:        "test_tool",
		description: "A tool for testing",
		schema: `{
			"type": "object",
			"properties": {
				"input": {"type": "string"}
			},
			"required": ["input"]
		}`,
		result: "Tool executed successfully",
	}

	// Test Name
	if tool.Name() != "test_tool" {
		t.Errorf("Expected name 'test_tool', got '%s'", tool.Name())
	}

	// Test Description
	if tool.Description() != "A tool for testing" {
		t.Errorf("Expected description 'A tool for testing', got '%s'", tool.Description())
	}

	// Test ArgsSchema
	schema := tool.ArgsSchema()
	if schema == "" {
		t.Error("Schema should not be empty")
	}

	// Test Invoke (success)
	input := &ToolInput{
		Args:    map[string]interface{}{"input": "test"},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if !output.Success {
		t.Error("Tool execution should succeed")
	}
	if output.Result != "Tool executed successfully" {
		t.Errorf("Expected result 'Tool executed successfully', got '%v'", output.Result)
	}

	// Test Invoke (error)
	errorTool := &mockTool{
		name:        "error_tool",
		description: "Tool that errors",
		shouldError: true,
	}

	errorOutput, err := errorTool.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke should not return error: %v", err)
	}
	if errorOutput.Success {
		t.Error("Tool execution should fail")
	}
	if errorOutput.Error == "" {
		t.Error("Error message should not be empty")
	}
}

// TestToolInputOptionalFields verifies ToolInput works with optional fields
func TestToolInputOptionalFields(t *testing.T) {
	ctx := context.Background()

	// Minimal ToolInput (only required fields)
	minimalInput := &ToolInput{
		Args:    map[string]interface{}{"key": "value"},
		Context: ctx,
	}

	if minimalInput.CallerID != "" {
		t.Error("CallerID should be empty when not set")
	}
	if minimalInput.TraceID != "" {
		t.Error("TraceID should be empty when not set")
	}

	// Full ToolInput (with optional fields)
	fullInput := &ToolInput{
		Args:     map[string]interface{}{"key": "value"},
		Context:  ctx,
		CallerID: "agent-123",
		TraceID:  "trace-456",
	}

	if fullInput.CallerID != "agent-123" {
		t.Error("CallerID should be set")
	}
	if fullInput.TraceID != "trace-456" {
		t.Error("TraceID should be set")
	}
}

// TestToolOutputVariousResultTypes verifies ToolOutput can hold different result types
func TestToolOutputVariousResultTypes(t *testing.T) {
	testCases := []struct {
		name   string
		result interface{}
	}{
		{"string result", "text result"},
		{"int result", 123},
		{"float result", 45.67},
		{"bool result", true},
		{"map result", map[string]interface{}{"key": "value"}},
		{"array result", []string{"a", "b", "c"}},
		{"nil result", nil},
		{"struct result", struct{ Name string }{"TestName"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output := &ToolOutput{
				Result:  tc.result,
				Success: true,
			}

			if output.Result == nil && tc.result != nil {
				t.Error("Result should be preserved")
			}
		})
	}
}

// TestToolCallInProgress verifies ToolCall for in-progress calls
func TestToolCallInProgress(t *testing.T) {
	inProgressCall := &ToolCall{
		ID:        "call-in-progress",
		ToolName:  "long_running_tool",
		Args:      map[string]interface{}{"task": "process"},
		Result:    nil,
		Error:     "",
		StartTime: 1000000,
		EndTime:   0, // Not finished yet
		Metadata:  map[string]interface{}{"status": "running"},
	}

	if inProgressCall.Result != nil {
		t.Error("In-progress call should have nil Result")
	}
	if inProgressCall.EndTime != 0 {
		t.Error("In-progress call should have EndTime=0")
	}
	if inProgressCall.Metadata["status"] != "running" {
		t.Errorf("Expected status 'running', got '%v'", inProgressCall.Metadata["status"])
	}
}
