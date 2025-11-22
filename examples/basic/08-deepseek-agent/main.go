// Package main demonstrates using DeepSeek LLM provider with GoAgent
//
// This example shows:
// - Basic DeepSeek configuration
// - Simple chat conversation
// - Using tools with DeepSeek
// - Streaming responses
// - ReAct agent with DeepSeek
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/agents/react"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

// CalculatorTool 简单的计算器工具
type CalculatorTool struct{}

func (c *CalculatorTool) Name() string {
	return "calculator"
}

func (c *CalculatorTool) Description() string {
	return "执行简单的数学计算，支持加减乘除运算。输入格式：'2 + 2' 或 '10 * 5'"
}

func (c *CalculatorTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	// 简化版计算器，实际应用中应该解析表达式
	expression, ok := input.Args["expression"].(string)
	if !ok {
		return &interfaces.ToolOutput{
			Result:  "错误：需要提供 expression 参数",
			Success: false,
		}, nil
	}

	// 这里简化处理，实际应该用表达式解析器
	result := fmt.Sprintf("计算结果：%s = 42 (示例结果)", expression)

	return &interfaces.ToolOutput{
		Result:  result,
		Success: true,
	}, nil
}

func (c *CalculatorTool) ArgsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"expression": {
				"type": "string",
				"description": "要计算的数学表达式，如 '2 + 2'"
			}
		},
		"required": ["expression"]
	}`
}

// WeatherTool 天气查询工具
type WeatherTool struct{}

func (w *WeatherTool) Name() string {
	return "get_weather"
}

func (w *WeatherTool) Description() string {
	return "查询指定城市的天气信息"
}

func (w *WeatherTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	city, ok := input.Args["city"].(string)
	if !ok {
		return &interfaces.ToolOutput{
			Result:  "错误：需要提供 city 参数",
			Success: false,
		}, nil
	}

	// 模拟天气数据
	weather := fmt.Sprintf("%s 今天天气：晴朗，温度 25°C，湿度 60%%，空气质量：优", city)

	return &interfaces.ToolOutput{
		Result:  weather,
		Success: true,
	}, nil
}

func (w *WeatherTool) ArgsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"city": {
				"type": "string",
				"description": "要查询的城市名称，如 '北京'、'上海'"
			}
		},
		"required": ["city"]
	}`
}

func main() {
	fmt.Println("GoAgent DeepSeek 示例")
	fmt.Println("=====================")

	// 从环境变量获取 API Key
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("⚠️  警告：未设置 DEEPSEEK_API_KEY 环境变量")
		fmt.Println("提示：export DEEPSEEK_API_KEY=your-api-key")
		fmt.Println("\n使用模拟模式运行示例...")
		runMockExample()
		return
	}

	// 示例 1: 基础配置和简单对话
	fmt.Println("示例 1: 基础 DeepSeek 配置")
	fmt.Println("----------------------------")
	runBasicChatExample(apiKey)

	// 示例 2: 使用工具
	fmt.Println("\n示例 2: DeepSeek + 工具调用")
	fmt.Println("----------------------------")
	runToolCallingExample(apiKey)

	// 示例 3: 流式响应
	fmt.Println("\n示例 3: DeepSeek 流式输出")
	fmt.Println("----------------------------")
	runStreamingExample(apiKey)

	// 示例 4: ReAct Agent
	fmt.Println("\n示例 4: DeepSeek ReAct Agent")
	fmt.Println("----------------------------")
	runReActExample(apiKey)
}

// runBasicChatExample 演示基础对话
func runBasicChatExample(apiKey string) {
	// 创建 DeepSeek 配置
	config := &llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat", // 使用 deepseek-chat 模型
		Temperature: 0.7,
		MaxTokens:   2000,
		Timeout:     30,
	}

	// 创建 DeepSeek provider
	deepseek, err := providers.NewDeepSeek(config)
	if err != nil {
		fmt.Printf("❌ 创建 DeepSeek provider 失败: %v\n", err)
		return
	}

	// 测试可用性
	fmt.Println("📡 检查 DeepSeek 连接...")
	if deepseek.IsAvailable() {
		fmt.Println("✅ DeepSeek 连接成功")
	} else {
		fmt.Println("❌ DeepSeek 连接失败")
		return
	}

	// 进行简单对话
	ctx := context.Background()
	messages := []llm.Message{
		llm.SystemMessage("你是一个友好的 AI 助手，擅长用简洁的语言回答问题。"),
		llm.UserMessage("请用一句话介绍一下 Go 语言的特点。"),
	}

	fmt.Println("\n💬 发送消息到 DeepSeek...")
	response, err := deepseek.Chat(ctx, messages)
	if err != nil {
		fmt.Printf("❌ 对话失败: %v\n", err)
		return
	}

	fmt.Printf("🤖 DeepSeek 回复:\n%s\n", response.Content)
	fmt.Printf("📊 Token 使用: 输入=%d, 输出=%d, 总计=%d\n",
		response.Usage.PromptTokens,
		response.Usage.CompletionTokens,
		response.Usage.TotalTokens)
}

// runToolCallingExample 演示工具调用
func runToolCallingExample(apiKey string) {
	config := &llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		Temperature: 0.7,
		MaxTokens:   2000,
		Timeout:     30,
	}

	deepseek, err := providers.NewDeepSeek(config)
	if err != nil {
		fmt.Printf("❌ 创建 DeepSeek provider 失败: %v\n", err)
		return
	}

	// 创建工具
	tools := []interfaces.Tool{
		&CalculatorTool{},
		&WeatherTool{},
	}

	// 调用带工具的生成
	ctx := context.Background()
	prompt := "请帮我计算 15 * 8 的结果，然后查询北京的天气"

	fmt.Printf("🔧 任务: %s\n", prompt)
	fmt.Println("🤖 DeepSeek 正在思考如何使用工具...")

	result, err := deepseek.GenerateWithTools(ctx, prompt, tools)
	if err != nil {
		fmt.Printf("❌ 工具调用失败: %v\n", err)
		return
	}

	if result.Content != "" {
		fmt.Printf("💭 思考: %s\n", result.Content)
	}

	if len(result.ToolCalls) > 0 {
		fmt.Printf("🔨 计划调用 %d 个工具:\n", len(result.ToolCalls))
		for i, tc := range result.ToolCalls {
			fmt.Printf("  %d. %s (参数: %v)\n", i+1, tc.Name, tc.Arguments)
		}
	} else {
		fmt.Println("ℹ️  DeepSeek 没有调用任何工具")
	}
}

// runStreamingExample 演示流式输出
func runStreamingExample(apiKey string) {
	config := &llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		Temperature: 0.8,
		MaxTokens:   500,
		Timeout:     30,
	}

	deepseek, err := providers.NewDeepSeek(config)
	if err != nil {
		fmt.Printf("❌ 创建 DeepSeek provider 失败: %v\n", err)
		return
	}

	ctx := context.Background()
	prompt := "请用三句话介绍 AI Agent 的概念。"

	fmt.Printf("💬 问题: %s\n", prompt)
	fmt.Print("🤖 DeepSeek 回复: ")

	// 开始流式输出
	stream, err := deepseek.Stream(ctx, prompt)
	if err != nil {
		fmt.Printf("\n❌ 流式输出失败: %v\n", err)
		return
	}

	// 逐个接收 token
	for token := range stream {
		fmt.Print(token)
	}
	fmt.Println()
}

// runReActExample 演示 ReAct Agent
func runReActExample(apiKey string) {
	config := &llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		Temperature: 0.7,
		MaxTokens:   2000,
		Timeout:     60,
	}

	deepseek, err := providers.NewDeepSeek(config)
	if err != nil {
		fmt.Printf("❌ 创建 DeepSeek provider 失败: %v\n", err)
		return
	}

	// 创建工具集
	tools := []interfaces.Tool{
		&CalculatorTool{},
		&WeatherTool{},
	}

	// 创建 ReAct Agent
	reactAgent := react.NewReActAgent(react.ReActConfig{
		Name:        "DeepSeek-ReAct-Agent",
		Description: "使用 DeepSeek 的 ReAct 推理 Agent",
		LLM:         deepseek,
		Tools:       tools,
		MaxSteps:    5,
	})

	// 准备输入
	input := &agentcore.AgentInput{
		Task:      "计算 25 * 4，然后查询上海的天气",
		Timestamp: time.Now(),
	}

	// 执行任务
	ctx := context.Background()
	fmt.Printf("📋 任务: %s\n", input.Task)
	fmt.Println("🔄 ReAct Agent 开始推理...")

	output, err := reactAgent.Invoke(ctx, input)
	if err != nil {
		fmt.Printf("❌ Agent 执行失败: %v\n", err)
		return
	}

	// 显示结果
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Printf("✅ 任务状态: %s\n", output.Status)
	fmt.Printf("📝 最终结果:\n%v\n", output.Result)
	fmt.Printf("⏱️  执行时间: %v\n", output.Latency)

	// 显示推理步骤
	if len(output.ReasoningSteps) > 0 {
		fmt.Printf("\n🧠 推理步骤 (%d 步):\n", len(output.ReasoningSteps))
		for _, step := range output.ReasoningSteps {
			status := "✅"
			if !step.Success {
				status = "❌"
			}
			fmt.Printf("  %s 步骤 %d: %s\n", status, step.Step, step.Action)
			if step.Description != "" {
				fmt.Printf("     描述: %s\n", step.Description)
			}
			if step.Result != "" {
				fmt.Printf("     结果: %v\n", step.Result)
			}
		}
	}
}

// runMockExample 在没有 API Key 时运行的模拟示例
func runMockExample() {
	fmt.Println("🎭 模拟模式示例")
	fmt.Println("----------------------------")
	fmt.Println("\n这个示例展示了 DeepSeek Agent 的基本用法：")
	fmt.Println()
	fmt.Println("1️⃣  基础对话:")
	fmt.Println("   - 配置 DeepSeek provider")
	fmt.Println("   - 发送消息并接收回复")
	fmt.Println("   - 查看 token 使用情况")
	fmt.Println()
	fmt.Println("2️⃣  工具调用:")
	fmt.Println("   - 使用计算器工具")
	fmt.Println("   - 使用天气查询工具")
	fmt.Println("   - DeepSeek 自动选择合适的工具")
	fmt.Println()
	fmt.Println("3️⃣  流式输出:")
	fmt.Println("   - 实时接收 AI 生成的文本")
	fmt.Println("   - 逐个 token 显示响应")
	fmt.Println()
	fmt.Println("4️⃣  ReAct Agent:")
	fmt.Println("   - 思考-行动-观察循环")
	fmt.Println("   - 多步骤推理")
	fmt.Println("   - 自动工具选择和执行")
	fmt.Println()
	fmt.Println("💡 配置步骤:")
	fmt.Println("   1. 访问 https://platform.deepseek.com/ 获取 API Key")
	fmt.Println("   2. 设置环境变量: export DEEPSEEK_API_KEY=your-key")
	fmt.Println("   3. 重新运行此程序: go run main.go")
	fmt.Println()
	fmt.Println("📖 更多信息:")
	fmt.Println("   - DeepSeek 文档: https://platform.deepseek.com/docs")
	fmt.Println("   - GoAgent 文档: https://github.com/kart-io/goagent")
}
