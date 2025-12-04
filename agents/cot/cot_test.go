package cot

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/agents/base"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// MockLLM implements a simple mock LLM for testing
type MockLLM struct{}

func (m *MockLLM) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	// Return a simple CoT-style response
	return &llm.CompletionResponse{
		Content: `Let's think step by step:
Step 1: We have 2 apples
Step 2: We add 3 more apples
Step 3: 2 + 3 = 5
Therefore, the final answer is: 5 apples`,
		TokensUsed: 50,
	}, nil
}

func (m *MockLLM) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content:    "Generated response",
		TokensUsed: 10,
	}, nil
}

func (m *MockLLM) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLM) IsAvailable() bool {
	return true
}

func TestCoTAgent_BasicFunctionality(t *testing.T) {
	// Create a mock LLM
	mockLLM := &MockLLM{}

	// Create CoT agent
	config := CoTConfig{
		Name:            "test-cot",
		Description:     "Test CoT Agent",
		LLM:             mockLLM,
		MaxSteps:        5,
		ZeroShot:        true,
		ShowStepNumbers: true,
	}

	agent := NewCoTAgent(config)

	// Create test input
	input := &agentcore.AgentInput{
		Task: "If I have 2 apples and get 3 more, how many do I have?",
	}

	// Execute agent
	ctx := context.Background()
	output, err := agent.Invoke(ctx, input)

	// Verify results
	if err != nil {
		t.Fatalf("Agent execution failed: %v", err)
	}

	if output.Status != "success" {
		t.Errorf("Expected status 'success', got '%s'", output.Status)
	}

	if output.Result == nil {
		t.Error("Expected result, got nil")
	}

	// Check that we have reasoning steps
	if len(output.Steps) == 0 {
		t.Error("Expected reasoning steps, got none")
	}

	t.Logf("Agent completed successfully with result: %v", output.Result)
	t.Logf("Reasoning steps: %d", len(output.Steps))
}

func TestCoTAgent_WithConfiguration(t *testing.T) {
	mockLLM := &MockLLM{}

	// Test different configurations
	configs := []CoTConfig{
		{
			Name:     "zero-shot",
			LLM:      mockLLM,
			ZeroShot: true,
		},
		{
			Name:    "few-shot",
			LLM:     mockLLM,
			FewShot: true,
			FewShotExamples: []CoTExample{
				{
					Question: "What is 2+2?",
					Steps:    []string{"2+2=4"},
					Answer:   "4",
				},
			},
		},
		{
			Name:                 "with-justification",
			LLM:                  mockLLM,
			RequireJustification: true,
		},
	}

	for _, config := range configs {
		t.Run(config.Name, func(t *testing.T) {
			agent := NewCoTAgent(config)

			input := &agentcore.AgentInput{
				Task: "Test task",
			}

			_, err := agent.Invoke(context.Background(), input)
			if err != nil {
				t.Errorf("Config %s failed: %v", config.Name, err)
			}
		})
	}
}

// TestCoTAgent_RunGenerator tests the RunGenerator method
func TestCoTAgent_RunGenerator(t *testing.T) {
	mockLLM := &MockLLM{}

	agent := NewCoTAgent(CoTConfig{
		Name:            "test-cot-gen",
		Description:     "Test CoT Agent with Generator",
		LLM:             mockLLM,
		MaxSteps:        5,
		ZeroShot:        true,
		ShowStepNumbers: true,
	})

	input := &agentcore.AgentInput{
		Task: "If I have 2 apples and get 3 more, how many do I have?",
	}

	ctx := context.Background()

	// Collect all outputs from generator
	var outputs []*agentcore.AgentOutput
	var finalOutput *agentcore.AgentOutput

	for output, err := range agent.RunGenerator(ctx, input) {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
			break
		}

		if output == nil {
			t.Error("Output is nil")
			break
		}

		outputs = append(outputs, output)
		finalOutput = output

		// Check metadata
		if _, ok := output.Metadata["step_type"]; !ok {
			t.Error("Missing step_type in metadata")
		}

		// Break on final output
		if output.Metadata["step_type"] == "final" {
			break
		}
	}

	// Verify we got multiple outputs
	if len(outputs) == 0 {
		t.Fatal("RunGenerator did not produce any outputs")
	}

	t.Logf("Total outputs: %d", len(outputs))

	// Verify final output
	if finalOutput == nil {
		t.Fatal("Final output is nil")
	}

	if finalOutput.Metadata["step_type"] != "final" {
		t.Errorf("Expected final step_type, got: %v", finalOutput.Metadata["step_type"])
	}

	// Verify we have reasoning steps
	if len(finalOutput.Steps) == 0 {
		t.Error("No reasoning steps in final output")
	}

	t.Logf("Final result: %v", finalOutput.Result)
	t.Logf("Reasoning steps: %d", len(finalOutput.Steps))
}

// TestCoTAgent_RunGenerator_EarlyTermination tests early termination
func TestCoTAgent_RunGenerator_EarlyTermination(t *testing.T) {
	mockLLM := &MockLLM{}

	agent := NewCoTAgent(CoTConfig{
		Name:     "test-cot-early",
		LLM:      mockLLM,
		ZeroShot: true,
		MaxSteps: 5,
	})

	input := &agentcore.AgentInput{
		Task: "Test early termination",
	}

	ctx := context.Background()

	// Terminate after first output
	maxOutputs := 1
	outputCount := 0

	for _, err := range agent.RunGenerator(ctx, input) {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
			break
		}

		outputCount++

		if outputCount >= maxOutputs {
			t.Logf("Terminating early after %d outputs", outputCount)
			break
		}
	}

	// Verify we only got the expected number of outputs
	if outputCount != maxOutputs {
		t.Errorf("Expected %d outputs, got %d", maxOutputs, outputCount)
	}

	t.Logf("Successfully terminated early after %d outputs", outputCount)
}

// TestCoTStrategy_isStepHeader 测试步骤头部检测
func TestCoTStrategy_isStepHeader(t *testing.T) {
	strategy := &CoTStrategy{
		config: CoTConfig{},
		parser: base.GetDefaultParser(),
	}

	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{"英文步骤1", "Step 1: Calculate the sum", true},
		{"英文步骤2", "Step 2: Verify the result", true},
		{"数字前缀", "1. First step", true},
		{"数字前缀2", "2) Second step", true},
		{"中文步骤", "步骤1：计算总和", true},
		{"中文步骤2", "第一步：分析问题", true},
		{"普通文本", "This is just a normal sentence", false},
		{"空行", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strategy.isStepHeader(tt.line)
			if result != tt.expected {
				t.Errorf("isStepHeader(%q) = %v, want %v", tt.line, result, tt.expected)
			}
		})
	}
}

// TestCoTStrategy_isAnswerLine 测试答案行检测
func TestCoTStrategy_isAnswerLine(t *testing.T) {
	strategy := &CoTStrategy{
		config: CoTConfig{
			FinalAnswerFormat: "Therefore, the final answer is:",
		},
		parser: base.GetDefaultParser(),
	}

	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{"自定义格式", "Therefore, the final answer is: 42", true},
		{"Final Answer", "Final Answer: 42", true},
		{"Answer标记", "Answer: The result is 5", true},
		{"中文答案", "答案：42", true},
		{"中文最终答案", "最终答案：结果是5", true},
		{"普通文本", "This is just a calculation step", false},
		{"空行", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strategy.isAnswerLine(tt.line)
			if result != tt.expected {
				t.Errorf("isAnswerLine(%q) = %v, want %v", tt.line, result, tt.expected)
			}
		})
	}
}

// TestCoTStrategy_extractAnswer 测试答案提取
func TestCoTStrategy_extractAnswer(t *testing.T) {
	strategy := &CoTStrategy{
		config: CoTConfig{
			FinalAnswerFormat: "Therefore, the final answer is:",
		},
		parser: base.GetDefaultParser(),
	}

	tests := []struct {
		name     string
		line     string
		expected string
	}{
		{"自定义格式提取", "Therefore, the final answer is: 42", "42"},
		{"Final Answer提取", "Final Answer: 42", "42"},
		{"Answer标记提取", "Answer: The result is 5", "The result is 5"},
		{"中文答案提取", "答案：42", "42"},
		{"带空格", "Therefore, the final answer is:   spaced answer  ", "spaced answer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strategy.extractAnswer(tt.line)
			if result != tt.expected {
				t.Errorf("extractAnswer(%q) = %q, want %q", tt.line, result, tt.expected)
			}
		})
	}
}

// TestCoTStrategy_isSkippableParagraph 测试可跳过段落检测
func TestCoTStrategy_isSkippableParagraph(t *testing.T) {
	strategy := &CoTStrategy{
		config: CoTConfig{},
		parser: base.GetDefaultParser(),
	}

	tests := []struct {
		name     string
		para     string
		expected bool
	}{
		{"英文问题", "Question: What is 2+2?", true},
		{"英文Let's", "Let's think step by step", true},
		{"中文问题", "问题：这是什么？", true},
		{"中文让我们", "让我们逐步分析", true},
		{"正常内容", "The sum of 2 and 3 is 5", false},
		{"步骤内容", "First, we calculate the total", false},
		{"空段落", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strategy.isSkippableParagraph(tt.para)
			if result != tt.expected {
				t.Errorf("isSkippableParagraph(%q) = %v, want %v", tt.para, result, tt.expected)
			}
		})
	}
}

// TestCoTStrategy_isAnswerParagraph 测试答案段落检测
func TestCoTStrategy_isAnswerParagraph(t *testing.T) {
	strategy := &CoTStrategy{
		config: CoTConfig{},
		parser: base.GetDefaultParser(),
	}

	tests := []struct {
		name     string
		para     string
		expected bool
	}{
		{"英文answer", "The answer is 42", true},
		{"英文conclusion", "In conclusion, the result is correct", true},
		{"英文therefore", "Therefore, we can say that X = 5", true},
		{"英文thus", "Thus, the total is 100", true},
		{"中文答案", "答案是42", true},
		{"中文结论", "结论：结果正确", true},
		{"中文因此", "因此，我们可以说X=5", true},
		{"中文所以", "所以，总数是100", true},
		{"中文综上", "综上所述，答案是正确的", true},
		{"中文总结", "总结一下，结果如下", true},
		{"普通段落", "We need to calculate the sum first", false},
		{"空段落", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strategy.isAnswerParagraph(tt.para)
			if result != tt.expected {
				t.Errorf("isAnswerParagraph(%q) = %v, want %v", tt.para, result, tt.expected)
			}
		})
	}
}

// TestCoTStrategy_formatToolResults 测试工具结果格式化
func TestCoTStrategy_formatToolResults(t *testing.T) {
	strategy := &CoTStrategy{
		config: CoTConfig{},
		parser: base.GetDefaultParser(),
	}

	tests := []struct {
		name     string
		results  map[string]interface{}
		isEmpty  bool
		contains []string
	}{
		{
			name:    "空结果",
			results: map[string]interface{}{},
			isEmpty: true,
		},
		{
			name: "单个工具结果",
			results: map[string]interface{}{
				"calculator": 42,
			},
			isEmpty:  false,
			contains: []string{"Tool execution results:", "calculator: 42", "Please continue"},
		},
		{
			name: "多个工具结果",
			results: map[string]interface{}{
				"calculator": 100,
				"search":     "found 5 results",
			},
			isEmpty:  false,
			contains: []string{"Tool execution results:", "calculator:", "search:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strategy.formatToolResults(tt.results)

			if tt.isEmpty {
				if result != "" {
					t.Errorf("formatToolResults() = %q, want empty string", result)
				}
				return
			}

			for _, substr := range tt.contains {
				if !contains(result, substr) {
					t.Errorf("formatToolResults() result %q should contain %q", result, substr)
				}
			}
		})
	}
}

// contains 辅助函数检查字符串包含
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestCoTStrategy_parseCoTResponse 测试 CoT 响应解析
func TestCoTStrategy_parseCoTResponse(t *testing.T) {
	strategy := &CoTStrategy{
		config: CoTConfig{
			FinalAnswerFormat: "Therefore, the final answer is:",
		},
		parser: base.GetDefaultParser(),
	}

	tests := []struct {
		name           string
		response       string
		expectedSteps  int
		expectedAnswer string
	}{
		{
			name: "标准 CoT 响应",
			response: `Let's think step by step:
Step 1: We have 2 apples
Step 2: We add 3 more apples
Step 3: 2 + 3 = 5
Therefore, the final answer is: 5 apples`,
			expectedSteps:  3,
			expectedAnswer: "5 apples",
		},
		{
			name: "中文 CoT 响应",
			response: `让我们逐步思考：
步骤1：我们有2个苹果
步骤2：我们再加3个苹果
步骤3：2 + 3 = 5
答案：5个苹果`,
			expectedSteps:  3,
			expectedAnswer: "5个苹果",
		},
		{
			name:           "简单响应",
			response:       "The answer is 42",
			expectedSteps:  0,
			expectedAnswer: "The answer is 42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps, answer := strategy.parseCoTResponse(tt.response)

			if len(steps) < tt.expectedSteps {
				t.Errorf("parseCoTResponse() got %d steps, want at least %d", len(steps), tt.expectedSteps)
			}

			if answer == "" {
				t.Error("parseCoTResponse() returned empty answer")
			}

			t.Logf("Steps: %d, Answer: %s", len(steps), answer)
		})
	}
}

// MockTool 实现测试用的模拟工具
type MockTool struct {
	name   string
	result interface{}
	err    error
}

func (t *MockTool) Name() string        { return t.name }
func (t *MockTool) Description() string { return "Mock tool for testing" }
func (t *MockTool) ArgsSchema() string  { return `{"type": "object"}` }
func (t *MockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if t.err != nil {
		return nil, t.err
	}
	return &interfaces.ToolOutput{
		Result:  t.result,
		Success: true,
	}, nil
}

// TestCoTStrategy_executeToolsIfNeeded 测试工具执行
func TestCoTStrategy_executeToolsIfNeeded(t *testing.T) {
	strategy := &CoTStrategy{
		config: CoTConfig{},
		parser: base.GetDefaultParser(),
	}

	// 创建模拟工具
	calculatorTool := &MockTool{
		name:   "calculator",
		result: 42,
	}

	toolsByName := map[string]interfaces.Tool{
		"calculator": calculatorTool,
	}

	tests := []struct {
		name          string
		steps         []string
		expectResults bool
		expectCalls   int
	}{
		{
			name:          "无工具调用",
			steps:         []string{"Step 1: Calculate 2+2", "Step 2: The result is 4"},
			expectResults: false,
			expectCalls:   0,
		},
		{
			name:          "有工具调用",
			steps:         []string{"Step 1: USE_TOOL: calculator 2+2", "Step 2: Process result"},
			expectResults: true,
			expectCalls:   1,
		},
		{
			name:          "工具不存在",
			steps:         []string{"USE_TOOL: unknown_tool query"},
			expectResults: false,
			expectCalls:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := &agentcore.AgentOutput{
				ToolCalls: []agentcore.AgentToolCall{},
				Metadata:  make(map[string]interface{}),
			}

			results := strategy.executeToolsIfNeeded(context.Background(), tt.steps, toolsByName, output)

			if tt.expectResults && len(results) == 0 {
				t.Error("Expected tool results, got none")
			}

			if !tt.expectResults && len(results) > 0 {
				t.Errorf("Expected no tool results, got %d", len(results))
			}

			if len(output.ToolCalls) != tt.expectCalls {
				t.Errorf("Expected %d tool calls, got %d", tt.expectCalls, len(output.ToolCalls))
			}
		})
	}
}

// TestCoTAgent_WithTools 测试带工具的 CoT Agent
func TestCoTAgent_WithTools(t *testing.T) {
	// 创建返回带工具调用的 Mock LLM
	mockLLM := &MockLLMWithToolCall{}

	calculatorTool := &MockTool{
		name:   "calculator",
		result: 42,
	}

	agent := NewCoTAgent(CoTConfig{
		Name:     "test-cot-tools",
		LLM:      mockLLM,
		Tools:    []interfaces.Tool{calculatorTool},
		ZeroShot: true,
		MaxSteps: 5,
	})

	input := &agentcore.AgentInput{
		Task: "Calculate 2 + 2 using the calculator",
	}

	output, err := agent.Invoke(context.Background(), input)
	if err != nil {
		t.Fatalf("Agent execution failed: %v", err)
	}

	if output.Status != "success" {
		t.Errorf("Expected status 'success', got '%s'", output.Status)
	}

	t.Logf("Result: %v", output.Result)
	t.Logf("Tool calls: %d", len(output.ToolCalls))
}

// MockLLMWithToolCall 返回包含工具调用的响应
type MockLLMWithToolCall struct{}

func (m *MockLLMWithToolCall) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content: `Let's think step by step:
Step 1: I need to calculate 2 + 2
Step 2: USE_TOOL: calculator 2 + 2
Step 3: The calculator returned 4
Therefore, the final answer is: 4`,
		TokensUsed: 50,
	}, nil
}

func (m *MockLLMWithToolCall) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content:    "Generated response",
		TokensUsed: 10,
	}, nil
}

func (m *MockLLMWithToolCall) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLMWithToolCall) IsAvailable() bool {
	return true
}

// TestCreateStepOutput 测试步骤输出创建
func TestCreateStepOutput(t *testing.T) {
	accumulated := &agentcore.AgentOutput{
		Steps: []agentcore.AgentStep{
			{Step: 1, Action: "Test", Success: true},
		},
		ToolCalls: []agentcore.AgentToolCall{
			{ToolName: "test_tool", Success: true},
		},
		Metadata: map[string]interface{}{
			"key": "value",
		},
		TokenUsage: &interfaces.TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}

	startTime := time.Now().Add(-time.Second)
	stepOutput := createStepOutput(accumulated, "Test message", startTime)

	// 验证复制
	if len(stepOutput.Steps) != len(accumulated.Steps) {
		t.Errorf("Steps not copied correctly: got %d, want %d", len(stepOutput.Steps), len(accumulated.Steps))
	}

	if len(stepOutput.ToolCalls) != len(accumulated.ToolCalls) {
		t.Errorf("ToolCalls not copied correctly: got %d, want %d", len(stepOutput.ToolCalls), len(accumulated.ToolCalls))
	}

	if stepOutput.Metadata["key"] != "value" {
		t.Error("Metadata not copied correctly")
	}

	if stepOutput.TokenUsage.TotalTokens != 150 {
		t.Errorf("TokenUsage not copied correctly: got %d, want 150", stepOutput.TokenUsage.TotalTokens)
	}

	if stepOutput.Message != "Test message" {
		t.Errorf("Message not set correctly: got %q, want %q", stepOutput.Message, "Test message")
	}

	if stepOutput.Latency <= 0 {
		t.Error("Latency should be positive")
	}
}
