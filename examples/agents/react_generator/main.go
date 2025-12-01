package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/agents/react"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// MockLLMClient 模拟 LLM 客户端用于演示
type MockLLMClient struct{}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	// 模拟 LLM 响应
	time.Sleep(10 * time.Millisecond) // 模拟 LLM 延迟

	content := `Thought: I need to calculate 2 + 2 first
Action: calculator
Action Input: {"expression": "2 + 2"}
Observation:`

	return &llm.CompletionResponse{
		Content:    content,
		TokensUsed: 50,
		Usage: &interfaces.TokenUsage{
			PromptTokens:     30,
			CompletionTokens: 20,
			TotalTokens:      50,
		},
	}, nil
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// 委托给 Chat 方法
	return m.Chat(ctx, req.Messages)
}

func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// MockCalculatorTool 模拟计算器工具
type MockCalculatorTool struct{}

func (t *MockCalculatorTool) Name() string {
	return "calculator"
}

func (t *MockCalculatorTool) Description() string {
	return "Calculate mathematical expressions"
}

func (t *MockCalculatorTool) ArgsSchema() string {
	return `{"type": "object", "properties": {"expression": {"type": "string"}}}`
}

func (t *MockCalculatorTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	time.Sleep(5 * time.Millisecond) // 模拟工具执行延迟
	return &interfaces.ToolOutput{
		Success: true,
		Result:  4,
	}, nil
}

func main() {
	fmt.Println("=== ReactAgent RunGenerator 演示 ===")
	fmt.Println()

	// 创建模拟 LLM 和工具
	llmClient := &MockLLMClient{}
	calcTool := &MockCalculatorTool{}

	// 创建 ReAct Agent
	agent := react.NewReActAgent(react.ReActConfig{
		Name:        "math-agent",
		Description: "Agent for solving math problems",
		LLM:         llmClient,
		Tools:       []interfaces.Tool{calcTool},
		MaxSteps:    3,
	})

	// 创建输入
	input := &agentcore.AgentInput{
		Task:      "What is 2 + 2?",
		SessionID: "demo-session",
		Timestamp: time.Now(),
	}

	ctx := context.Background()

	fmt.Println("场景 1：使用 RunGenerator 流式输出每个推理步骤")
	fmt.Println("==============================================")
	fmt.Println()

	stepCount := 0
	for output, err := range agent.RunGenerator(ctx, input) {
		if err != nil {
			log.Printf("❌ 错误: %v\n", err)
			break
		}

		stepCount++
		fmt.Printf("\n[步骤 %d] 状态: %s\n", stepCount, output.Status)
		fmt.Printf("消息: %s\n", output.Message)

		if stepType, ok := output.Metadata["step_type"].(string); ok {
			fmt.Printf("类型: %s\n", stepType)
		}

		if output.Status == interfaces.StatusSuccess {
			fmt.Printf("\n✅ 最终答案: %v\n", output.Result)
			fmt.Printf("总推理步骤: %d\n", len(output.Steps))
			fmt.Printf("总工具调用: %d\n", len(output.ToolCalls))
			fmt.Printf("Token 使用: 提示=%d, 完成=%d, 总计=%d\n",
				output.TokenUsage.PromptTokens,
				output.TokenUsage.CompletionTokens,
				output.TokenUsage.TotalTokens)
			break
		}
	}

	fmt.Println()
	fmt.Println("场景 2：早期终止示例（仅处理前 2 步）")
	fmt.Println("=========================================")
	fmt.Println()

	stepCount = 0
	maxSteps := 2
	for output, err := range agent.RunGenerator(ctx, input) {
		if err != nil {
			log.Printf("❌ 错误: %v\n", err)
			break
		}

		stepCount++
		fmt.Printf("[步骤 %d/%d] %s\n", stepCount, maxSteps, output.Message)

		if stepCount >= maxSteps {
			fmt.Println("\n⏸️  达到最大步骤数，主动终止")
			break
		}
	}

	fmt.Println()
	fmt.Println("场景 3：统计分析")
	fmt.Println("================")
	fmt.Println()

	stats := struct {
		TotalSteps    int
		ThoughtSteps  int
		ActionSteps   int
		TotalDuration time.Duration
		TotalTokens   int
	}{}

	startTime := time.Now()
	for output, err := range agent.RunGenerator(ctx, input) {
		if err != nil {
			break
		}

		if stepType, ok := output.Metadata["step_type"].(string); ok {
			switch stepType {
			case "thought":
				stats.ThoughtSteps++
			case "action":
				stats.ActionSteps++
			}
		}

		if output.TokenUsage != nil {
			stats.TotalTokens = output.TokenUsage.TotalTokens
		}

		stats.TotalSteps++

		if output.Status == interfaces.StatusSuccess {
			break
		}
	}
	stats.TotalDuration = time.Since(startTime)

	fmt.Printf("总步骤数: %d\n", stats.TotalSteps)
	fmt.Printf("  - 思考步骤: %d\n", stats.ThoughtSteps)
	fmt.Printf("  - 行动步骤: %d\n", stats.ActionSteps)
	fmt.Printf("总耗时: %v\n", stats.TotalDuration)
	fmt.Printf("Token 使用: %d\n", stats.TotalTokens)

	fmt.Println("\n=== 演示完成 ===")
}
