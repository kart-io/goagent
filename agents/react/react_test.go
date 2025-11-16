package react_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/kart-io/goagent/agents/executor"
	"github.com/kart-io/goagent/agents/react"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// MockLLMClient 模拟 LLM 客户端用于测试
type MockLLMClient struct {
	responses []string
	callCount int
}

func NewMockLLMClient(responses []string) *MockLLMClient {
	return &MockLLMClient{
		responses: responses,
		callCount: 0,
	}
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	if m.callCount >= len(m.responses) {
		return &llm.CompletionResponse{
			Content:    "Final Answer: I don't have enough information to answer that.",
			TokensUsed: 10,
		}, nil
	}

	response := m.responses[m.callCount]
	m.callCount++

	return &llm.CompletionResponse{
		Content:    response,
		TokensUsed: len(response) / 4, // 粗略估计
	}, nil
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return m.Chat(ctx, req.Messages)
}

func (m *MockLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// TestReActAgent 测试 ReAct Agent
func TestReActAgent(t *testing.T) {
	// 创建模拟工具
	calculatorTool := tools.NewBaseTool(
		"calculator",
		"Useful for mathematical calculations",
		`{"type": "object", "properties": {"expression": {"type": "string"}}}`,
		func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
			expr, ok := input.Args["expression"].(string)
			if !ok {
				return &tools.ToolOutput{
					Success: false,
					Error:   "expression must be a string",
				}, nil
			}

			// 简单计算 (实际应该使用表达式求值器)
			result := fmt.Sprintf("Result of %s is 42", expr)

			return &tools.ToolOutput{
				Result:  result,
				Success: true,
			}, nil
		},
	)

	searchTool := tools.NewBaseTool(
		"search",
		"Useful for searching information on the internet",
		`{"type": "object", "properties": {"query": {"type": "string"}}}`,
		func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
			query, ok := input.Args["query"].(string)
			if !ok {
				return &tools.ToolOutput{
					Success: false,
					Error:   "query must be a string",
				}, nil
			}

			result := fmt.Sprintf("Search results for '%s': Found 10 results", query)

			return &tools.ToolOutput{
				Result:  result,
				Success: true,
			}, nil
		},
	)

	// 创建模拟 LLM 响应
	mockLLM := NewMockLLMClient([]string{
		`Thought: I need to search for information about Go programming
Action: search
Action Input: {"query": "Go programming language"}`,

		`Thought: Now I have information about Go, I can provide a final answer
Final Answer: Go is a statically typed, compiled programming language designed at Google.`,
	})

	// 创建 ReAct Agent
	agent := react.NewReActAgent(react.ReActConfig{
		Name:        "TestAgent",
		Description: "A test ReAct agent",
		LLM:         mockLLM,
		Tools:       []tools.Tool{calculatorTool, searchTool},
		MaxSteps:    5,
	})

	// 测试执行
	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task: "What is Go programming language?",
	}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Agent execution failed: %v", err)
	}

	// 验证输出
	if output.Status != "success" {
		t.Errorf("Expected status 'success', got '%s'", output.Status)
	}

	if output.Result == nil {
		t.Error("Expected non-nil result")
	}

	t.Logf("Agent result: %v", output.Result)
	t.Logf("Reasoning steps: %d", len(output.ReasoningSteps))
	t.Logf("Tool calls: %d", len(output.ToolCalls))

	// 验证至少有一次工具调用
	if len(output.ToolCalls) == 0 {
		t.Error("Expected at least one tool call")
	}

	// 验证工具调用成功
	for i, toolCall := range output.ToolCalls {
		if !toolCall.Success {
			t.Errorf("Tool call %d failed: %s", i, toolCall.Error)
		}
	}
}

// TestAgentExecutor 测试 Agent 执行器
func TestAgentExecutor(t *testing.T) {
	// 创建简单工具
	echoTool := tools.NewBaseTool(
		"echo",
		"Echoes the input",
		`{"type": "object", "properties": {"message": {"type": "string"}}}`,
		func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
			msg, _ := input.Args["message"].(string)
			return &tools.ToolOutput{
				Result:  fmt.Sprintf("Echo: %s", msg),
				Success: true,
			}, nil
		},
	)

	// 创建模拟 LLM
	mockLLM := NewMockLLMClient([]string{
		`Thought: I should echo the message
Action: echo
Action Input: {"message": "Hello World"}`,

		`Final Answer: The echo tool returned: Echo: Hello World`,
	})

	// 创建 Agent
	agent := react.NewReActAgent(react.ReActConfig{
		Name:        "EchoAgent",
		Description: "An agent that echoes messages",
		LLM:         mockLLM,
		Tools:       []tools.Tool{echoTool},
		MaxSteps:    3,
	})

	// 创建执行器
	executor := executor.NewAgentExecutor(executor.ExecutorConfig{
		Agent:             agent,
		MaxIterations:     5,
		ReturnIntermSteps: true,
		Verbose:           true,
	})

	// 执行
	ctx := context.Background()
	result, err := executor.Run(ctx, "Echo 'Hello World'")
	if err != nil {
		t.Fatalf("Executor failed: %v", err)
	}

	t.Logf("Executor result: %s", result)

	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// BenchmarkReActAgent 性能基准测试
func BenchmarkReActAgent(b *testing.B) {
	// 创建工具
	simpleTool := tools.NewBaseTool(
		"simple",
		"A simple test tool",
		`{"type": "object"}`,
		func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
			return &tools.ToolOutput{
				Result:  "done",
				Success: true,
			}, nil
		},
	)

	mockLLM := NewMockLLMClient([]string{
		"Final Answer: Done",
	})

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "BenchAgent",
		LLM:   mockLLM,
		Tools: []tools.Tool{simpleTool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task: "Test task",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Invoke(ctx, input)
		mockLLM.callCount = 0 // 重置计数器
	}
}
