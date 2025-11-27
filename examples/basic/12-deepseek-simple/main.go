// Package main demonstrates simplified DeepSeek Agent usage
//
// 借鉴 go-kratos/blades 的设计理念：
// - 简洁的 API
// - Option 模式
// - 链式调用
// - 专注核心功能
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	agentstate "github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
)

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("⚠️  请设置 DEEPSEEK_API_KEY 环境变量")
		fmt.Println("提示：export DEEPSEEK_API_KEY=your-api-key")
		return
	}

	fmt.Println("=== DeepSeek Agent 简化示例 ===")
	fmt.Println()

	// 示例 1: 最简单的用法
	example1SimpleChat(apiKey)

	// 示例 2: 添加工具
	example2WithTools(apiKey)

	// 示例 3: 聊天机器人
	example3Chatbot(apiKey)

	fmt.Println("\n✨ 所有示例完成!")
}

// example1SimpleChat 最简单的对话示例
func example1SimpleChat(apiKey string) {
	fmt.Println("【示例 1】最简单的对话")
	fmt.Println("-------------------")

	// 一行创建 Agent
	agent := quickAgent(apiKey, "你是一个友好的 AI 助手")

	// 运行
	output := run(agent, "用一句话介绍 Go 语言")

	fmt.Printf("🤖 回复: %v\n\n", output)
}

// example2WithTools 带工具的示例
func example2WithTools(apiKey string) {
	fmt.Println("【示例 2】使用工具")
	fmt.Println("-------------------")

	// 创建工具
	calculator := simpleTool(
		"calculator",
		"计算数学表达式，如 '15 * 8'",
		func(ctx context.Context, input string) (string, error) {
			// 简化：直接返回结果
			return "120", nil
		},
	)

	weather := simpleTool(
		"get_weather",
		"查询城市天气",
		func(ctx context.Context, city string) (string, error) {
			return fmt.Sprintf("%s 天气晴朗，22°C", city), nil
		},
	)

	// 创建带工具的 Agent
	agent := quickAgentWithTools(apiKey,
		"你是智能助手，可以使用工具帮助用户",
		calculator, weather,
	)

	// 运行
	output := runWithTools(agent, "计算 15 * 8 的结果")

	fmt.Printf("🤖 回复: %v\n\n", output)
}

// example3Chatbot 聊天机器人示例
func example3Chatbot(apiKey string) {
	fmt.Println("【示例 3】聊天机器人")
	fmt.Println("-------------------")

	// 创建聊天机器人
	agent := chatbot(apiKey)

	// 多轮对话
	conversations := []string{
		"你好！",
		"告诉我一个有趣的事实",
		"再见！",
	}

	for _, msg := range conversations {
		fmt.Printf("👤 用户: %s\n", msg)
		output := run(agent, msg)
		fmt.Printf("🤖 助手: %v\n\n", output)
	}
}

// ========== 辅助函数：简化 API ==========

// quickAgent 快速创建 Agent（最简化）
func quickAgent(apiKey, prompt string) *builder.ConfigurableAgent[any, core.State] {
	llm := mustCreateLLM(apiKey)

	//nolint:staticcheck // Example demonstrates old API for backward compatibility
	agent, err := builder.NewAgentBuilder[any, core.State](llm).
		WithSystemPrompt(prompt).
		WithState(agentstate.NewAgentState()).
		Build()
	if err != nil {
		panic(fmt.Sprintf("创建 Agent 失败: %v", err))
	}

	return agent
}

// quickAgentWithTools 快速创建带工具的 Agent
func quickAgentWithTools(apiKey, prompt string, tools ...interfaces.Tool) *builder.ConfigurableAgent[any, core.State] {
	llm := mustCreateLLM(apiKey)

	//nolint:staticcheck // Example demonstrates old API for backward compatibility
	agent, err := builder.NewAgentBuilder[any, core.State](llm).
		WithSystemPrompt(prompt).
		WithTools(tools...).
		WithState(agentstate.NewAgentState()).
		WithConfig(&builder.AgentConfig{
			Verbose: true,
		}).
		Build()
	if err != nil {
		panic(fmt.Sprintf("创建 Agent 失败: %v", err))
	}

	return agent
}

// chatbot 创建聊天机器人
func chatbot(apiKey string) *builder.ConfigurableAgent[any, core.State] {
	llm := mustCreateLLM(apiKey)

	//nolint:staticcheck // Example demonstrates old API for backward compatibility
	agent, err := builder.NewAgentBuilder[any, core.State](llm).
		WithSystemPrompt("你是友好的聊天机器人").
		WithState(agentstate.NewAgentState()).
		ConfigureForChatbot().
		Build()
	if err != nil {
		panic(fmt.Sprintf("创建 Agent 失败: %v", err))
	}

	return agent
}

// run 运行 Agent（简化）
func run(agent *builder.ConfigurableAgent[any, core.State], input string) interface{} {
	ctx := context.Background()
	output, err := agent.Execute(ctx, input)
	if err != nil {
		panic(fmt.Sprintf("执行失败: %v", err))
	}
	return output.Result
}

// runWithTools 运行带工具的 Agent（简化）
func runWithTools(agent *builder.ConfigurableAgent[any, core.State], input string) interface{} {
	ctx := context.Background()
	output, err := agent.ExecuteWithTools(ctx, input)
	if err != nil {
		panic(fmt.Sprintf("执行失败: %v", err))
	}
	return output.Result
}

// simpleTool 创建简单工具（简化）
func simpleTool(name, description string, handler func(context.Context, string) (string, error)) interfaces.Tool {
	tool, err := tools.NewFunctionToolBuilder(name).
		WithDescription(description).
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"input": {
					"type": "string",
					"description": "工具输入"
				}
			},
			"required": ["input"]
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			input, _ := args["input"].(string)
			return handler(ctx, input)
		}).
		Build()

	if err != nil {
		panic(fmt.Sprintf("创建工具失败: %v", err))
	}

	return tool
}

// mustCreateLLM 创建 LLM 客户端（简化）
func mustCreateLLM(apiKey string) llm.Client {
	client, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(2000),
		llm.WithTimeout(30*time.Second),
	)
	if err != nil {
		panic(fmt.Sprintf("创建 LLM 失败: %v", err))
	}
	return client
}
