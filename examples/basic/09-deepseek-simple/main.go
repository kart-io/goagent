// Package main demonstrates a simple DeepSeek agent implementation
//
// This example shows:
// - Creating a DeepSeek client
// - Simple chat interaction
// - Basic error handling
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/kart-io/goagent/builder"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

func main() {
	fmt.Println("GoAgent + DeepSeek 简单示例")
	fmt.Println(string([]rune{61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61, 61}))
	fmt.Println()

	// 从环境变量获取 API Key
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("错误: 请设置 DEEPSEEK_API_KEY 环境变量\n提示: export DEEPSEEK_API_KEY=your-api-key")
	}

	// 示例 1: 使用 Provider 直接对话
	runProviderExample(apiKey)

	fmt.Println()

	// 示例 2: 使用 Builder 创建 Agent
	runBuilderExample(apiKey)

	fmt.Println()

	// 示例 3: 输出结构化数据（JSON 格式）
	runStructuredOutputExample(apiKey)
}

// runProviderExample 演示使用 DeepSeek Provider 直接对话
func runProviderExample(apiKey string) {
	fmt.Println("示例 1: DeepSeek Provider 基础对话")
	fmt.Println("-----------------------------------")

	// 创建 DeepSeek 配置
	config := &llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		Temperature: 0.7,
		MaxTokens:   1000,
		Timeout:     30,
	}

	// 创建 DeepSeek provider
	client, err := providers.NewDeepSeek(config)
	if err != nil {
		log.Fatalf("创建 DeepSeek provider 失败: %v", err)
	}

	// 准备对话消息
	messages := []llm.Message{
		llm.SystemMessage("你是一个友好的 AI 助手，擅长用简洁的语言回答问题。"),
		llm.UserMessage("请用一句话解释什么是 AI Agent。"),
	}

	// 发送请求
	ctx := context.Background()
	fmt.Println("发送请求到 DeepSeek...")

	response, err := client.Chat(ctx, messages)
	if err != nil {
		log.Fatalf("对话失败: %v", err)
	}

	// 显示响应
	fmt.Printf("\n回复: %s\n", response.Content)
	fmt.Printf("\nToken 使用统计:\n")
	fmt.Printf("  - 输入 Tokens: %d\n", response.Usage.PromptTokens)
	fmt.Printf("  - 输出 Tokens: %d\n", response.Usage.CompletionTokens)
	fmt.Printf("  - 总计 Tokens: %d\n", response.Usage.TotalTokens)
	fmt.Printf("  - 模型: %s\n", response.Model)
}

// runBuilderExample 演示使用 Builder 创建 DeepSeek Agent
func runBuilderExample(apiKey string) {
	fmt.Println("示例 2: 使用 Builder 创建 DeepSeek Agent")
	fmt.Println("--------------------------------------")

	// 创建 DeepSeek 配置
	config := &llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		Temperature: 0.7,
		MaxTokens:   1000,
		Timeout:     30,
	}

	// 创建 DeepSeek provider
	client, err := providers.NewDeepSeek(config)
	if err != nil {
		log.Fatalf("创建 DeepSeek provider 失败: %v", err)
	}

	// 使用 Builder 构建 Agent（指定泛型参数）
	agent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
		WithSystemPrompt("你是一个专业的 Go 语言顾问，擅长解答 Go 编程相关的问题。").
		WithMetadata("name", "Go-Advisor").
		WithMetadata("description", "Go 语言专业顾问").
		Build()
	if err != nil {
		log.Fatalf("构建 Agent 失败: %v", err)
	}

	// 准备输入任务
	task := "请列举 Go 语言的三个主要特点。"

	// 执行任务
	ctx := context.Background()
	fmt.Printf("任务: %s\n", task)
	fmt.Println("\nAgent 正在思考...")
	fmt.Println()

	output, err := agent.Execute(ctx, task)
	if err != nil {
		log.Fatalf("Agent 执行失败: %v", err)
	}

	// 显示结果
	fmt.Printf("结果:\n%v\n", output.Result)
	fmt.Printf("\n执行时间: %v\n", output.Duration)
}

// runStructuredOutputExample 演示输出结构化数据
func runStructuredOutputExample(apiKey string) {
	fmt.Println("示例 3: 输出结构化数据（JSON 格式）")
	fmt.Println("-------------------------------------")

	// 创建 DeepSeek 配置
	config := &llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		Temperature: 0.3, // 使用较低的 temperature 以获得更稳定的输出
		MaxTokens:   1000,
		Timeout:     30,
	}

	// 创建 DeepSeek provider
	client, err := providers.NewDeepSeek(config)
	if err != nil {
		log.Fatalf("创建 DeepSeek provider 失败: %v", err)
	}

	// 使用 Builder 构建 Agent，要求输出 JSON 格式
	agent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
		WithSystemPrompt(`你是一个数据分析助手。你的任务是根据用户的要求，生成结构化的 JSON 数据。
请确保输出的是有效的 JSON 格式，不要包含任何其他文字说明。`).
		WithMetadata("name", "DataGenerator").
		WithMetadata("description", "结构化数据生成器").
		Build()
	if err != nil {
		log.Fatalf("构建 Agent 失败: %v", err)
	}

	// 准备输入任务
	task := `请生成一个包含 3 个用户信息的 JSON 数组，每个用户包含以下字段：
- id: 用户ID（整数）
- name: 用户名（字符串）
- email: 邮箱地址（字符串）
- age: 年龄（整数）
- skills: 技能列表（字符串数组）
- shool: 学校（字符串）

请直接输出 JSON 数据，不要有任何其他说明文字。`

	// 执行任务
	ctx := context.Background()
	fmt.Printf("任务: 生成结构化用户数据（JSON 格式）\n")
	fmt.Println("\nAgent 正在生成数据...")
	fmt.Println()

	output, err := agent.Execute(ctx, task)
	if err != nil {
		log.Fatalf("Agent 执行失败: %v", err)
	}

	// 显示结果
	fmt.Println("生成的 JSON 数据:")
	fmt.Println("------------------")
	fmt.Printf("%v\n", output.Result)
	fmt.Printf("\n执行时间: %v\n", output.Duration)

	// 额外示例：生成产品信息
	fmt.Println()
	fmt.Println("示例 3.2: 生成产品信息")
	fmt.Println("------------------------")

	productTask := `请生成一个产品信息的 JSON 对象，包含以下字段：
- product_id: 产品ID（字符串）
- name: 产品名称（字符串）
- description: 产品描述（字符串）
- price: 价格（浮点数）
- category: 类别（字符串）
- tags: 标签列表（字符串数组）
- in_stock: 是否有货（布尔值）
- specifications: 规格（对象，包含 weight, dimensions, color 等）

请直接输出 JSON 数据，不要有任何其他说明文字。`

	fmt.Println("Agent 正在生成产品数据...")
	fmt.Println()

	productOutput, err := agent.Execute(ctx, productTask)
	if err != nil {
		log.Fatalf("Agent 执行失败: %v", err)
	}

	fmt.Println("生成的产品 JSON 数据:")
	fmt.Println("---------------------")
	fmt.Printf("%v\n", productOutput.Result)
	fmt.Printf("\n执行时间: %v\n", productOutput.Duration)
}
