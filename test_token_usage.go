package main

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/utils/json"
	"os"

	"github.com/kart-io/goagent/agents/cot"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

func main() {
	ctx := context.Background()

	// 检查 API Key
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("请设置环境变量 DEEPSEEK_API_KEY")
		fmt.Println("例如: export DEEPSEEK_API_KEY='your-api-key'")
		os.Exit(1)
	}

	// 初始化 LLM 客户端
	llmClient, err := providers.NewDeepSeek(&llm.Config{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		MaxTokens:   500,
		Temperature: 0.7,
		Timeout:     30,
	})
	if err != nil {
		fmt.Printf("创建 LLM 客户端失败: %v\n", err)
		os.Exit(1)
	}

	// 创建 CoT Agent
	agent := cot.NewCoTAgent(cot.CoTConfig{
		Name:        "TestAgent",
		Description: "Test CoT Agent with Token Usage",
		LLM:         llmClient,
		MaxSteps:    5,
		ZeroShot:    true,
	})

	// 执行简单的数学问题
	input := &agentcore.AgentInput{
		Task: "计算 15 * 23 + 47 = ?",
	}

	fmt.Println("执行 CoT Agent...")
	output, err := agent.Invoke(ctx, input)
	if err != nil {
		fmt.Printf("执行失败: %v\n", err)
		os.Exit(1)
	}

	// 输出结果
	fmt.Println("\n=== 执行结果 ===")
	fmt.Printf("状态: %s\n", output.Status)
	fmt.Printf("消息: %s\n", output.Message)
	fmt.Printf("结果: %v\n", output.Result)
	fmt.Printf("推理步骤: %d 步\n", len(output.ReasoningSteps))
	fmt.Printf("执行延迟: %v\n", output.Latency)

	// 输出 Token 使用统计
	fmt.Println("\n=== Token 使用统计 ===")
	if output.TokenUsage != nil {
		fmt.Printf("Prompt Tokens: %d\n", output.TokenUsage.PromptTokens)
		fmt.Printf("Completion Tokens: %d\n", output.TokenUsage.CompletionTokens)
		fmt.Printf("Total Tokens: %d\n", output.TokenUsage.TotalTokens)
		if output.TokenUsage.CachedTokens > 0 {
			fmt.Printf("Cached Tokens: %d\n", output.TokenUsage.CachedTokens)
		}
	} else {
		fmt.Println("Token 使用统计: 未提供")
	}

	// 输出完整的 JSON 格式
	fmt.Println("\n=== 完整输出 (JSON) ===")
	jsonOutput, err := json.MarshalIndent(output, "", "  ")
	if err == nil {
		fmt.Println(string(jsonOutput))
	}
}
