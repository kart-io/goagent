package mocks

import (
	"context"
	"fmt"
	"sync"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// MockTool provides a mock implementation of the Tool interface
type MockTool struct {
	mu           sync.Mutex
	name         string
	description  string
	schema       string
	invokeFunc   func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error)
	invokeCount  int
	lastInput    *tools.ToolInput
	shouldError  bool
	errorMessage string
	delay        int // milliseconds to delay execution
}

// NewMockTool creates a new mock tool
func NewMockTool(name, description string) *MockTool {
	return &MockTool{
		name:        name,
		description: description,
		schema:      `{"type": "object"}`,
		invokeFunc: func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{
				Result:  fmt.Sprintf("Mock result from %s", name),
				Success: true,
			}, nil
		},
	}
}

// Name returns the tool name
func (m *MockTool) Name() string {
	return m.name
}

// Description returns the tool description
func (m *MockTool) Description() string {
	return m.description
}

// Schema returns the tool schema
func (m *MockTool) Schema() string {
	return m.schema
}

// Invoke executes the tool
func (m *MockTool) Invoke(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.invokeCount++
	m.lastInput = input

	if m.shouldError {
		return nil, fmt.Errorf("%s", m.errorMessage)
	}

	// Simulate delay if configured
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			// In real implementation, would use time.Sleep
		}
	}

	if m.invokeFunc != nil {
		return m.invokeFunc(ctx, input)
	}

	return &interfaces.ToolOutput{
		Result:  fmt.Sprintf("Mock result from %s", m.name),
		Success: true,
	}, nil
}

// SetInvokeFunc sets a custom invoke function
func (m *MockTool) SetInvokeFunc(fn func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invokeFunc = fn
}

// SetError configures the tool to return an error
func (m *MockTool) SetError(shouldError bool, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldError = shouldError
	m.errorMessage = message
}

// GetInvokeCount returns the number of times the tool was invoked
func (m *MockTool) GetInvokeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.invokeCount
}

// GetLastInput returns the last input passed to the tool
func (m *MockTool) GetLastInput() *tools.ToolInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastInput
}

// Reset resets the tool state
func (m *MockTool) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invokeCount = 0
	m.lastInput = nil
	m.shouldError = false
	m.errorMessage = ""
}

// MockToolRegistry provides a mock tool registry
type MockToolRegistry struct {
	mu    sync.Mutex
	tools map[string]interfaces.Tool
}

// NewMockToolRegistry creates a new mock tool registry
func NewMockToolRegistry() *MockToolRegistry {
	return &MockToolRegistry{
		tools: make(map[string]interfaces.Tool),
	}
}

// Register registers a tool
func (r *MockToolRegistry) Register(tool interfaces.Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[tool.Name()]; exists {
		return fmt.Errorf("tool %s already registered", tool.Name())
	}

	r.tools[tool.Name()] = tool
	return nil
}

// Get retrieves a tool by name
func (r *MockToolRegistry) Get(name string) (interfaces.Tool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tool, exists := r.tools[name]
	return tool, exists
}

// List lists all registered tools
func (r *MockToolRegistry) List() []interfaces.Tool {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]interfaces.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		result = append(result, tool)
	}
	return result
}

// Clear removes all tools
func (r *MockToolRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = make(map[string]interfaces.Tool)
}

// MockToolExecutor provides a mock tool executor
type MockToolExecutor struct {
	mu              sync.Mutex
	executeFunc     func(ctx context.Context, tools []interfaces.Tool, inputs []map[string]interface{}) ([]interface{}, error)
	executeCalls    int
	lastTools       []interfaces.Tool
	lastInputs      []map[string]interface{}
	shouldError     bool
	errorMessage    string
	parallelResults []interface{}
}

// NewMockToolExecutor creates a new mock tool executor
func NewMockToolExecutor() *MockToolExecutor {
	return &MockToolExecutor{
		parallelResults: []interface{}{},
	}
}

// ExecuteParallel executes tools in parallel
func (e *MockToolExecutor) ExecuteParallel(ctx context.Context, toolList []tools.Tool, inputs []map[string]interface{}) ([]interface{}, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.executeCalls++
	e.lastTools = toolList
	e.lastInputs = inputs

	if e.shouldError {
		return nil, fmt.Errorf("%s", e.errorMessage)
	}

	if e.executeFunc != nil {
		return e.executeFunc(ctx, toolList, inputs)
	}

	// Return mock results
	results := make([]interface{}, len(toolList))
	for i := range toolList {
		if i < len(e.parallelResults) {
			results[i] = e.parallelResults[i]
		} else {
			results[i] = fmt.Sprintf("Result from %s", toolList[i].Name())
		}
	}

	return results, nil
}

// SetExecuteFunc sets a custom execute function
func (e *MockToolExecutor) SetExecuteFunc(fn func(ctx context.Context, tools []tools.Tool, inputs []map[string]interface{}) ([]interface{}, error)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.executeFunc = fn
}

// SetParallelResults sets the results to return
func (e *MockToolExecutor) SetParallelResults(results ...interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.parallelResults = results
}

// SetError configures the executor to return an error
func (e *MockToolExecutor) SetError(shouldError bool, message string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.shouldError = shouldError
	e.errorMessage = message
}

// GetExecuteCalls returns the number of execute calls
func (e *MockToolExecutor) GetExecuteCalls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.executeCalls
}

// Reset resets the executor state
func (e *MockToolExecutor) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.executeCalls = 0
	e.lastTools = nil
	e.lastInputs = nil
	e.shouldError = false
	e.errorMessage = ""
	e.parallelResults = []interface{}{}
}
