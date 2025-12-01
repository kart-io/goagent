package react_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/kart-io/goagent/agents/executor"
	"github.com/kart-io/goagent/agents/react"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/tools"
)

// MockLLMClient 模拟 LLM 客户端用于测试
type MockLLMClient struct {
	responses []string
	callCount int
	mu        sync.Mutex // 保护并发访问
}

func NewMockLLMClient(responses []string) *MockLLMClient {
	return &MockLLMClient{
		responses: responses,
		callCount: 0,
	}
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

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

func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
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
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			expr, ok := input.Args["expression"].(string)
			if !ok {
				return &interfaces.ToolOutput{
					Success: false,
					Error:   "expression must be a string",
				}, nil
			}

			// 简单计算 (实际应该使用表达式求值器)
			result := fmt.Sprintf("Result of %s is 42", expr)

			return &interfaces.ToolOutput{
				Result:  result,
				Success: true,
			}, nil
		},
	)

	searchTool := tools.NewBaseTool(
		"search",
		"Useful for searching information on the internet",
		`{"type": "object", "properties": {"query": {"type": "string"}}}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			query, ok := input.Args["query"].(string)
			if !ok {
				return &interfaces.ToolOutput{
					Success: false,
					Error:   "query must be a string",
				}, nil
			}

			result := fmt.Sprintf("Search results for '%s': Found 10 results", query)

			return &interfaces.ToolOutput{
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
		Tools:       []interfaces.Tool{calculatorTool, searchTool},
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
	t.Logf("Reasoning steps: %d", len(output.Steps))
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
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			msg, _ := input.Args["message"].(string)
			return &interfaces.ToolOutput{
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
		Tools:       []interfaces.Tool{echoTool},
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
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{
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
		Tools: []interfaces.Tool{simpleTool},
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

// TestReActAgent_RunGenerator 测试 RunGenerator 方法
func TestReActAgent_RunGenerator(t *testing.T) {
	// 创建模拟 LLM，提供包含 Final Answer 的响应
	mockLLM := NewMockLLMClient([]string{
		`Thought: Let me calculate this
Action: calculator
Action Input: {"expression": "2 + 2"}
Observation:`,
		`Thought: I now know the final answer
Final Answer: The result is 4`,
	})

	// 创建简单工具
	calcTool := tools.NewBaseTool(
		"calculator",
		"Calculate expressions",
		`{"type": "object"}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{
				Success: true,
				Result:  4,
			}, nil
		},
	)

	// 创建 ReAct Agent
	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "TestAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{calcTool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task: "What is 2 + 2?",
	}

	// 测试 RunGenerator
	var stepCount int
	var finalOutput *agentcore.AgentOutput
	var lastError error

	for output, err := range agent.RunGenerator(ctx, input) {
		stepCount++
		finalOutput = output
		lastError = err

		if err != nil {
			t.Errorf("Unexpected error at step %d: %v", stepCount, err)
			break
		}

		// 验证输出不为 nil
		if output == nil {
			t.Errorf("Output is nil at step %d", stepCount)
			break
		}

		// 验证元数据包含必要信息
		if _, ok := output.Metadata["current_step"]; !ok {
			t.Errorf("Missing current_step in metadata at step %d", stepCount)
		}

		// 如果找到最终答案，验证并退出
		if output.Status == interfaces.StatusSuccess {
			if output.Result == nil {
				t.Error("Final answer result is nil")
			}
			break
		}
	}

	// 验证执行了多个步骤
	if stepCount == 0 {
		t.Fatal("RunGenerator did not produce any outputs")
	}

	t.Logf("Total steps: %d", stepCount)

	// 验证没有错误
	if lastError != nil {
		t.Errorf("Final error: %v", lastError)
	}

	// 验证最终输出
	if finalOutput == nil {
		t.Fatal("Final output is nil")
	}

	if finalOutput.Status != interfaces.StatusSuccess {
		t.Errorf("Expected success status, got: %s", finalOutput.Status)
	}

	// 验证有推理步骤
	if len(finalOutput.Steps) == 0 {
		t.Error("No reasoning steps in final output")
	}

	// 验证有工具调用
	if len(finalOutput.ToolCalls) == 0 {
		t.Error("No tool calls in final output")
	}

	t.Logf("Reasoning steps: %d", len(finalOutput.Steps))
	t.Logf("Tool calls: %d", len(finalOutput.ToolCalls))
}

// TestReActAgent_RunGenerator_EarlyTermination 测试早期终止
func TestReActAgent_RunGenerator_EarlyTermination(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: Step 1
Action: calculator
Action Input: {}`,
		`Thought: Step 2
Action: calculator
Action Input: {}`,
		`Thought: Step 3
Final Answer: Done`,
	})

	calcTool := tools.NewBaseTool(
		"calculator",
		"Calculate",
		`{"type": "object"}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{
				Success: true,
				Result:  "result",
			}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "TestAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{calcTool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "Test"}

	// 在第 3 步后主动终止
	stepCount := 0
	maxSteps := 3

	for _, err := range agent.RunGenerator(ctx, input) {
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		stepCount++

		if stepCount >= maxSteps {
			t.Logf("Terminating early at step %d", stepCount)
			break
		}
	}

	// 验证只执行了 maxSteps 步
	if stepCount != maxSteps {
		t.Errorf("Expected %d steps, got %d", maxSteps, stepCount)
	}

	// 验证 LLM 只被调用了 2 次（因为每个 LLM 调用会产生 thought + action 两个步骤）
	mockLLM.mu.Lock()
	callCount := mockLLM.callCount
	mockLLM.mu.Unlock()

	if callCount > maxSteps {
		t.Errorf("LLM was called %d times, expected <= %d", callCount, maxSteps)
	}

	t.Logf("LLM calls: %d, Steps: %d", callCount, stepCount)
}
