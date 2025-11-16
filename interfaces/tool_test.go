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

// TestToolResultStructure verifies ToolResult struct is properly defined
func TestToolResultStructure(t *testing.T) {
	result := &ToolResult{
		ToolName: "search_tool",
		Output: &ToolOutput{
			Result:  "Search completed",
			Success: true,
		},
		ExecutionTime: 250,
	}

	if result.ToolName != "search_tool" {
		t.Errorf("Expected ToolName 'search_tool', got '%s'", result.ToolName)
	}
	if result.ExecutionTime != 250 {
		t.Errorf("Expected ExecutionTime 250, got %d", result.ExecutionTime)
	}
	if result.Output == nil {
		t.Fatal("Output should not be nil")
	}
	if !result.Output.Success {
		t.Error("Output should be marked as success")
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

// mockToolExecutor is a minimal test implementation of ToolExecutor
type mockToolExecutor struct {
	tools map[string]Tool
}

func newMockToolExecutor() *mockToolExecutor {
	return &mockToolExecutor{
		tools: make(map[string]Tool),
	}
}

func (m *mockToolExecutor) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (*ToolResult, error) {
	tool, exists := m.tools[toolName]
	if !exists {
		return &ToolResult{
			ToolName: toolName,
			Output: &ToolOutput{
				Result:  nil,
				Success: false,
				Error:   "Tool not found",
			},
		}, nil
	}

	input := &ToolInput{
		Args:    args,
		Context: ctx,
	}

	startTime := int64(1000000)
	output, err := tool.Invoke(ctx, input)
	endTime := int64(1000050)

	if err != nil {
		return &ToolResult{
			ToolName: toolName,
			Output: &ToolOutput{
				Result:  nil,
				Success: false,
				Error:   err.Error(),
			},
			ExecutionTime: endTime - startTime,
		}, err
	}

	return &ToolResult{
		ToolName:      toolName,
		Output:        output,
		ExecutionTime: endTime - startTime,
	}, nil
}

func (m *mockToolExecutor) ListTools() []Tool {
	tools := make([]Tool, 0, len(m.tools))
	for _, tool := range m.tools {
		tools = append(tools, tool)
	}
	return tools
}

func (m *mockToolExecutor) registerTool(tool Tool) {
	m.tools[tool.Name()] = tool
}

// Ensure mockToolExecutor implements ToolExecutor interface
var _ ToolExecutor = (*mockToolExecutor)(nil)

// TestToolExecutorInterface verifies the ToolExecutor interface works correctly
func TestToolExecutorInterface(t *testing.T) {
	ctx := context.Background()
	executor := newMockToolExecutor()

	// Register some tools
	tool1 := &mockTool{
		name:        "calculator",
		description: "Performs calculations",
		result:      42,
	}

	tool2 := &mockTool{
		name:        "search",
		description: "Searches the web",
		result:      "Search results",
	}

	executor.registerTool(tool1)
	executor.registerTool(tool2)

	// Test ListTools
	tools := executor.ListTools()
	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	// Test ExecuteTool (success)
	result, err := executor.ExecuteTool(ctx, "calculator", map[string]interface{}{
		"operation": "add",
		"a":         5,
		"b":         3,
	})
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if result.ToolName != "calculator" {
		t.Errorf("Expected ToolName 'calculator', got '%s'", result.ToolName)
	}
	if !result.Output.Success {
		t.Error("Tool execution should succeed")
	}
	if result.Output.Result != 42 {
		t.Errorf("Expected result 42, got '%v'", result.Output.Result)
	}
	if result.ExecutionTime <= 0 {
		t.Error("ExecutionTime should be positive")
	}

	// Test ExecuteTool (tool not found)
	notFoundResult, err := executor.ExecuteTool(ctx, "non_existent_tool", map[string]interface{}{})
	if err != nil {
		t.Fatalf("ExecuteTool should not error for missing tool: %v", err)
	}
	if notFoundResult.Output.Success {
		t.Error("Execution should fail for non-existent tool")
	}
	if notFoundResult.Output.Error != "Tool not found" {
		t.Errorf("Expected error 'Tool not found', got '%s'", notFoundResult.Output.Error)
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

// TestMultipleToolsInExecutor verifies ToolExecutor can manage multiple tools
func TestMultipleToolsInExecutor(t *testing.T) {
	ctx := context.Background()
	executor := newMockToolExecutor()

	// Register 5 different tools
	for i := 0; i < 5; i++ {
		tool := &mockTool{
			name:        string(rune('a'+i)) + "_tool",
			description: "Tool " + string(rune('A'+i)),
			result:      i * 10,
		}
		executor.registerTool(tool)
	}

	// List should return all 5 tools
	tools := executor.ListTools()
	if len(tools) != 5 {
		t.Errorf("Expected 5 tools, got %d", len(tools))
	}

	// Execute each tool
	for i := 0; i < 5; i++ {
		toolName := string(rune('a'+i)) + "_tool"
		result, err := executor.ExecuteTool(ctx, toolName, map[string]interface{}{})
		if err != nil {
			t.Fatalf("ExecuteTool(%s) failed: %v", toolName, err)
		}
		if !result.Output.Success {
			t.Errorf("Tool %s should succeed", toolName)
		}
		expectedResult := i * 10
		if result.Output.Result != expectedResult {
			t.Errorf("Tool %s: expected result %d, got %v", toolName, expectedResult, result.Output.Result)
		}
	}
}
