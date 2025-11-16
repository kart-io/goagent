package pot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockLLMClient for testing
type MockLLMClient struct {
	mock.Mock
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*llm.CompletionResponse), args.Error(1)
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	args := m.Called(ctx, messages)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*llm.CompletionResponse), args.Error(1)
}

func (m *MockLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// MockTool for testing
type MockTool struct {
	mock.Mock
}

func (m *MockTool) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockTool) Description() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.ToolOutput), args.Error(1)
}

func (m *MockTool) ArgsSchema() string {
	args := m.Called()
	return args.String(0)
}

func TestNewPoTAgent(t *testing.T) {
	tests := []struct {
		name   string
		config PoTConfig
		check  func(t *testing.T, agent *PoTAgent)
	}{
		{
			name: "default configuration",
			config: PoTConfig{
				Name:        "test-pot",
				Description: "Test PoT Agent",
				LLM:         &MockLLMClient{},
			},
			check: func(t *testing.T, agent *PoTAgent) {
				assert.Equal(t, "test-pot", agent.Name())
				assert.Equal(t, "Test PoT Agent", agent.Description())
				assert.Equal(t, "python", agent.config.Language)
				assert.Equal(t, 2000, agent.config.MaxCodeLength)
				assert.Equal(t, 10*time.Second, agent.config.ExecutionTimeout)
				assert.Equal(t, 3, agent.config.MaxIterations)
				assert.Equal(t, "python3", agent.config.PythonPath)
				assert.Equal(t, "node", agent.config.NodePath)
			},
		},
		{
			name: "custom configuration",
			config: PoTConfig{
				Name:             "custom-pot",
				Description:      "Custom PoT Agent",
				LLM:              &MockLLMClient{},
				Language:         "javascript",
				AllowedLanguages: []string{"javascript", "python"},
				MaxCodeLength:    5000,
				ExecutionTimeout: 30 * time.Second,
				SafeMode:         true,
				MaxIterations:    5,
				PythonPath:       "/usr/bin/python",
				NodePath:         "/usr/bin/node",
			},
			check: func(t *testing.T, agent *PoTAgent) {
				assert.Equal(t, "javascript", agent.config.Language)
				assert.Equal(t, []string{"javascript", "python"}, agent.config.AllowedLanguages)
				assert.Equal(t, 5000, agent.config.MaxCodeLength)
				assert.Equal(t, 30*time.Second, agent.config.ExecutionTimeout)
				assert.True(t, agent.config.SafeMode)
				assert.Equal(t, 5, agent.config.MaxIterations)
				assert.Equal(t, "/usr/bin/python", agent.config.PythonPath)
				assert.Equal(t, "/usr/bin/node", agent.config.NodePath)
			},
		},
		{
			name: "with tools",
			config: PoTConfig{
				Name:        "pot-with-tools",
				Description: "PoT with Tools",
				LLM:         &MockLLMClient{},
				Tools: []interfaces.Tool{
					func() interfaces.Tool {
						m := &MockTool{}
						m.On("Name").Return("test-tool")
						m.On("Description").Return("Test tool")
						m.On("ArgsSchema").Return("{}")
						return m
					}(),
				},
			},
			check: func(t *testing.T, agent *PoTAgent) {
				assert.Len(t, agent.tools, 1)
				assert.Contains(t, agent.Capabilities(), "tool_calling")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewPoTAgent(tt.config)
			assert.NotNil(t, agent)
			tt.check(t, agent)
		})
	}
}

func TestPoTAgent_SelectLanguage(t *testing.T) {
	agent := NewPoTAgent(PoTConfig{
		Name:     "test-select",
		LLM:      &MockLLMClient{},
		Language: "go",
	})

	tests := []struct {
		task     string
		expected string
	}{
		{"Calculate the factorial of 10", "python"},
		{"Perform statistical analysis on data", "python"},
		{"Use numpy to process arrays", "python"},
		{"Create a web API endpoint", "javascript"},
		{"Parse JSON data", "javascript"},
		{"Run concurrent operations", "go"},
		{"Use goroutines for parallel processing", "go"},
		{"Generic task", "go"}, // Default to configured language
	}

	for _, tt := range tests {
		language := agent.selectLanguage(tt.task)
		assert.Equal(t, tt.expected, language)
	}
}

func TestPoTAgent_ExtractCode(t *testing.T) {
	agent := NewPoTAgent(PoTConfig{
		Name: "test-extract",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		name     string
		response string
		language string
		expected string
	}{
		{
			name:     "python code block",
			response: "Here's the solution:\n```python\ndef factorial(n):\n    return 1 if n <= 1 else n * factorial(n-1)\n```",
			language: "python",
			expected: "def factorial(n):\n    return 1 if n <= 1 else n * factorial(n-1)",
		},
		{
			name:     "generic code block",
			response: "```\nconsole.log('Hello');\n```",
			language: "javascript",
			expected: "console.log('Hello');",
		},
		{
			name:     "no code block",
			response: "print('Hello, World!')",
			language: "python",
			expected: "print('Hello, World!')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := agent.extractCode(tt.response, tt.language)
			assert.Equal(t, tt.expected, code)
		})
	}
}

func TestPoTAgent_ValidatePythonCode(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		safeMode  bool
		imports   []string
		shouldErr bool
	}{
		{
			name:      "valid python code",
			code:      "def add(a, b):\n    return a + b",
			safeMode:  false,
			shouldErr: false,
		},
		{
			name:      "dangerous import in safe mode",
			code:      "import os\nos.system('rm -rf /')",
			safeMode:  true,
			shouldErr: true,
		},
		{
			name:      "allowed import in safe mode",
			code:      "import math\nprint(math.pi)",
			safeMode:  true,
			imports:   []string{"math"},
			shouldErr: false,
		},
		{
			name:      "unbalanced parentheses",
			code:      "print('hello'",
			safeMode:  false,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewPoTAgent(PoTConfig{
				Name:         "test-validate",
				LLM:          &MockLLMClient{},
				SafeMode:     tt.safeMode,
				AllowImports: tt.imports,
			})

			err := agent.validatePythonCode(tt.code)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPoTAgent_ValidateJavaScriptCode(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		safeMode  bool
		shouldErr bool
	}{
		{
			name:      "valid javascript code",
			code:      "function add(a, b) { return a + b; }",
			safeMode:  false,
			shouldErr: false,
		},
		{
			name:      "eval in safe mode",
			code:      "eval('alert(1)')",
			safeMode:  true,
			shouldErr: true,
		},
		{
			name:      "child_process in safe mode",
			code:      "const exec = require('child_process').exec;",
			safeMode:  true,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewPoTAgent(PoTConfig{
				Name:     "test-validate-js",
				LLM:      &MockLLMClient{},
				SafeMode: tt.safeMode,
			})

			err := agent.validateJavaScriptCode(tt.code)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPoTAgent_ValidateGoCode(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		shouldErr bool
	}{
		{
			name: "valid go code",
			code: `package main
func main() {
    println("Hello")
}`,
			shouldErr: false,
		},
		{
			name:      "missing main function",
			code:      "func add(a, b int) int { return a + b }",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewPoTAgent(PoTConfig{
				Name: "test-validate-go",
				LLM:  &MockLLMClient{},
			})

			err := agent.validateGoCode(tt.code)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPoTAgent_ParseResult(t *testing.T) {
	agent := NewPoTAgent(PoTConfig{
		Name: "test-parse",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		name     string
		result   *CodeResult
		expected interface{}
	}{
		{
			name:     "json output",
			result:   &CodeResult{Output: `{"result": 42, "status": "success"}`},
			expected: map[string]interface{}{"result": float64(42), "status": "success"},
		},
		{
			name:     "simple string output",
			result:   &CodeResult{Output: "Hello, World!"},
			expected: "Hello, World!",
		},
		{
			name:     "multiline output",
			result:   &CodeResult{Output: "Line 1\nLine 2\nLine 3"},
			expected: "Line 1\nLine 2\nLine 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, _ := agent.parseResult(tt.result)
			assert.Equal(t, tt.expected, parsed)
		})
	}
}

func TestPoTAgent_BuildPrompts(t *testing.T) {
	agent := NewPoTAgent(PoTConfig{
		Name: "test-prompts",
		LLM:  &MockLLMClient{},
	})

	t.Run("initial code prompt", func(t *testing.T) {
		prompt := agent.buildInitialCodePrompt("Calculate factorial of 5", "python")
		assert.Contains(t, prompt, "python")
		assert.Contains(t, prompt, "Calculate factorial of 5")
		assert.Contains(t, prompt, "complete and executable")
	})

	t.Run("refinement prompt", func(t *testing.T) {
		prompt := agent.buildRefinementPrompt("Calculate sum", "Error: undefined variable", "javascript")
		assert.Contains(t, prompt, "javascript")
		assert.Contains(t, prompt, "Calculate sum")
		assert.Contains(t, prompt, "Error: undefined variable")
		assert.Contains(t, prompt, "Fix any errors")
	})
}

func TestPoTAgent_GetSystemPrompt(t *testing.T) {
	agent := NewPoTAgent(PoTConfig{
		Name: "test-system",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		language string
		expected string
	}{
		{"python", "PEP 8"},
		{"javascript", "ES6+"},
		{"go", "idiomatic Go"},
		{"unknown", "expert programmer"},
	}

	for _, tt := range tests {
		prompt := agent.getSystemPrompt(tt.language)
		assert.Contains(t, prompt, tt.expected)
	}
}

func TestPoTAgent_FormatCodeForDisplay(t *testing.T) {
	agent := NewPoTAgent(PoTConfig{
		Name: "test-format",
		LLM:  &MockLLMClient{},
	})

	t.Run("short code", func(t *testing.T) {
		code := "def add(a, b):\n    return a + b"
		formatted := agent.formatCodeForDisplay(code, "python")
		assert.Contains(t, formatted, "```python")
		assert.Contains(t, formatted, code)
	})

	t.Run("long code", func(t *testing.T) {
		lines := make([]string, 20)
		for i := range lines {
			lines[i] = "line"
		}
		code := strings.Join(lines, "\n")
		formatted := agent.formatCodeForDisplay(code, "javascript")
		assert.Contains(t, formatted, "```javascript")
		assert.Contains(t, formatted, "10 more lines")
	})
}

func TestPoTAgent_DebugError(t *testing.T) {
	agent := NewPoTAgent(PoTConfig{
		Name: "test-debug",
		LLM:  &MockLLMClient{},
	})

	ctx := context.Background()
	code := "print(undefined_var)"
	err := assert.AnError
	result := &CodeResult{
		Output: "",
		Error:  "NameError: name 'undefined_var' is not defined",
	}

	debugInfo := agent.debugError(ctx, code, err, result)
	assert.Contains(t, debugInfo, "Code execution failed")
	assert.Contains(t, debugInfo, "undefined_var")
	assert.Contains(t, debugInfo, "fix the code")
}

func TestPoTAgent_BuildFinalAnswer(t *testing.T) {
	agent := NewPoTAgent(PoTConfig{
		Name: "test-final",
		LLM:  &MockLLMClient{},
	})

	result := 42
	code := "def answer(): return 42"

	answer := agent.buildFinalAnswer(result, code)
	assert.Contains(t, answer, "42")
	assert.Contains(t, answer, code)
	assert.Contains(t, answer, "Solution found")
}

func TestPoTAgent_Stream(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	mockLLM.On("Chat", ctx, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "```python\nprint('Hello')\n```",
		}, nil,
	)

	agent := NewPoTAgent(PoTConfig{
		Name:          "test-stream",
		Description:   "Test Stream",
		LLM:           mockLLM,
		MaxIterations: 1,
	})

	input := &core.AgentInput{
		Task: "Print hello",
	}

	stream, err := agent.Stream(ctx, input)
	assert.NoError(t, err)
	assert.NotNil(t, stream)

	// Read from stream
	chunk := <-stream
	assert.NotNil(t, chunk.Data)
	assert.True(t, chunk.Done)
}

func TestPoTAgent_WithCallbacks(t *testing.T) {
	callback := &testCallback{
		onStart: func(ctx context.Context, input interface{}) error {
			return nil
		},
	}

	agent := NewPoTAgent(PoTConfig{
		Name:        "test-callbacks",
		Description: "Test Callbacks",
		LLM:         &MockLLMClient{},
	})

	agentWithCallbacks := agent.WithCallbacks(callback)
	assert.NotNil(t, agentWithCallbacks)
}

// Test callback implementation
type testCallback struct {
	onStart  func(context.Context, interface{}) error
	onFinish func(context.Context, interface{}) error
	onError  func(context.Context, error) error
}

func (tc *testCallback) OnStart(ctx context.Context, input interface{}) error {
	if tc.onStart != nil {
		return tc.onStart(ctx, input)
	}
	return nil
}

func (tc *testCallback) OnEnd(ctx context.Context, output interface{}) error {
	return nil
}

func (tc *testCallback) OnAgentFinish(ctx context.Context, output interface{}) error {
	if tc.onFinish != nil {
		return tc.onFinish(ctx, output)
	}
	return nil
}

func (tc *testCallback) OnError(ctx context.Context, err error) error {
	if tc.onError != nil {
		return tc.onError(ctx, err)
	}
	return nil
}

func (tc *testCallback) OnAgentAction(ctx context.Context, action *core.AgentAction) error {
	return nil
}

func (tc *testCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	return nil
}

func (tc *testCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	return nil
}

func (tc *testCallback) OnLLMError(ctx context.Context, err error) error {
	return nil
}

func (tc *testCallback) OnChainStart(ctx context.Context, chainName string, input interface{}) error {
	return nil
}

func (tc *testCallback) OnChainEnd(ctx context.Context, chainName string, output interface{}) error {
	return nil
}

func (tc *testCallback) OnChainError(ctx context.Context, chainName string, err error) error {
	return nil
}

func (tc *testCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	return nil
}

func (tc *testCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	return nil
}

func (tc *testCallback) OnToolError(ctx context.Context, toolName string, err error) error {
	return nil
}
