// Simple API 示例
// 展示 Builder API 的 Simple 层级（5-8 个方法，覆盖 80% 使用场景）
//
// 本示例演示：
// 1. 最简单的 Agent 创建（3 行代码）
// 2. 带工具的 Agent
// 3. 调整常用配置（MaxIterations, Temperature）
// 4. 使用快速构建函数
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm/providers/mockllm"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("=== Builder API - Simple 层级示例 ===\n")

	// 示例 1: 最简单的 Agent（仅 3 行代码）
	example1SimpleAgent()

	// 示例 2: 带工具的 Agent
	example2AgentWithTools()

	// 示例 3: 调整常用配置
	example3ConfiguredAgent()

	// 示例 4: 使用快速构建函数
	example4QuickAgent()
}

// 示例 1: 最简单的 Agent（仅 3 行代码）
//
// 使用的方法：
// - WithSystemPrompt (Simple)
// - Build (Simple)
func example1SimpleAgent() {
	fmt.Println("--- 示例 1: 最简单的 Agent ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("你是一个友好的助手，总是礼貌地回答问题。")

	// 仅需 2 个方法调用！
	agent, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个翻译助手，专门将中文翻译成英文").
		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 执行 Agent
	result, err := agent.Execute(context.Background(), "你好世界")
	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("输入: 你好世界\n")
		fmt.Printf("输出: %s\n", result)
	}

	fmt.Println()
}

// 示例 2: 带工具的 Agent
//
// 使用的方法：
// - WithSystemPrompt (Simple)
// - WithTools (Simple)
// - Build (Simple)
func example2AgentWithTools() {
	fmt.Println("--- 示例 2: 带工具的 Agent ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我会使用工具来帮助回答问题。")

	// 创建简单的计算器工具
	calculator := tools.NewCalculatorTool()

	// 添加工具到 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个数学助手，可以使用计算器工具来计算").
		WithTools(calculator).
		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 执行 Agent
	result, err := agent.Execute(context.Background(), "计算 123 + 456")
	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("输入: 计算 123 + 456\n")
		fmt.Printf("输出: %s\n", result)
	}

	fmt.Println()
}

// 示例 3: 调整常用配置
//
// 使用的方法：
// - WithSystemPrompt (Simple)
// - WithTools (Simple)
// - WithMaxIterations (Simple) - 控制推理步骤数
// - WithTemperature (Simple) - 控制创造性
// - Build (Simple)
func example3ConfiguredAgent() {
	fmt.Println("--- 示例 3: 调整常用配置 ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我是一个精确的助手。")

	// 创建工具
	calculator := tools.NewCalculatorTool()

	// 配置 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个数据分析助手").
		WithTools(calculator).
		WithMaxIterations(15).      // 允许更多推理步骤（默认 10）
		WithTemperature(0.3).        // 降低创造性，提高精确性（默认 0.7）
		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 执行 Agent
	result, err := agent.Execute(context.Background(), "分析数据: [10, 20, 30, 40, 50]")
	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("输入: 分析数据: [10, 20, 30, 40, 50]\n")
		fmt.Printf("输出: %s\n", result)
	}

	fmt.Println()
}

// 示例 4: 使用快速构建函数
//
// 快速构建函数提供了最简单的 API（1 行代码）
func example4QuickAgent() {
	fmt.Println("--- 示例 4: 使用快速构建函数 ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我是一个快速助手。")

	// 方式 1: 使用 QuickAgent（最简单）
	agent1, err := builder.QuickAgent(llmClient, "你是一个问答助手")
	if err != nil {
		log.Printf("QuickAgent 创建失败: %v", err)
	} else {
		result, _ := agent1.Execute(context.Background(), "什么是 Go 语言？")
		fmt.Printf("QuickAgent 输出: %s\n", result)
	}

	// 方式 2: 使用场景预设 - ChatAgent
	agent2, err := builder.ChatAgent(llmClient, "小明")
	if err != nil {
		log.Printf("ChatAgent 创建失败: %v", err)
	} else {
		result, _ := agent2.Execute(context.Background(), "你好！")
		fmt.Printf("ChatAgent 输出: %s\n", result)
	}

	fmt.Println()
}

// 工具演示：简单的天气查询工具
type weatherTool struct{}

func (t *weatherTool) Name() string {
	return "weather"
}

func (t *weatherTool) Description() string {
	return "查询指定城市的天气信息"
}

func (t *weatherTool) Parameters() []interfaces.ToolParameter {
	return []interfaces.ToolParameter{
		{
			Name:        "city",
			Type:        "string",
			Description: "城市名称",
			Required:    true,
		},
	}
}

func (t *weatherTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	city, ok := args["city"].(string)
	if !ok {
		return nil, fmt.Errorf("city 参数无效")
	}

	// 模拟天气查询
	return fmt.Sprintf("%s 的天气: 晴天，温度 25°C", city), nil
}

// NewWeatherTool 创建天气查询工具
func NewWeatherTool() interfaces.Tool {
	return &weatherTool{}
}
