// Package main 演示使用简化的 Builder API 构建 Kimi Agent
//
// 本示例展示：
// - 使用 NewSimpleBuilder 简化 API（无需泛型参数）
// - 使用 QuickAgent 等快速构建函数
// - SimpleAgent 和 SimpleAgentBuilder 类型别名
// - 工具集成和中间件配置
// - Kimi (Moonshot AI) 的超长上下文能力
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("简化 Builder API + Kimi (Moonshot) 示例")
	fmt.Println("========================================")
	fmt.Println()

	// 从环境变量获取 API Key
	apiKey := os.Getenv("KIMI_API_KEY")
	if apiKey == "" {
		// 也支持 MOONSHOT_API_KEY
		apiKey = os.Getenv("MOONSHOT_API_KEY")
	}
	if apiKey == "" {
		fmt.Println("警告：未设置 KIMI_API_KEY 或 MOONSHOT_API_KEY 环境变量")
		fmt.Println("提示：export KIMI_API_KEY=your-api-key")
		fmt.Println("\n使用模拟模式运行示例...")
		runMockExample()
		return
	}

	// 示例 1: NewSimpleBuilder 基础用法
	fmt.Println("示例 1: NewSimpleBuilder 基础用法")
	fmt.Println("-----------------------------------")
	if err := runSimpleBuilderBasic(apiKey); err != nil {
		fmt.Printf("示例 1 失败: %v\n", err)
	}

	// 示例 2: QuickAgent 快速创建
	fmt.Println("\n示例 2: QuickAgent 快速创建")
	fmt.Println("-----------------------------------")
	if err := runQuickAgent(apiKey); err != nil {
		fmt.Printf("示例 2 失败: %v\n", err)
	}

	// 示例 3: SimpleBuilder + 工具
	fmt.Println("\n示例 3: SimpleBuilder + 工具")
	fmt.Println("-----------------------------------")
	if err := runSimpleBuilderWithTools(apiKey); err != nil {
		fmt.Printf("示例 3 失败: %v\n", err)
	}

	// 示例 4: 预设配置快速构建
	fmt.Println("\n示例 4: 预设配置快速构建")
	fmt.Println("-----------------------------------")
	if err := runPresetAgents(apiKey); err != nil {
		fmt.Printf("示例 4 失败: %v\n", err)
	}

	// 示例 5: JSON 格式化输出（ResponseFormat）
	fmt.Println("\n示例 5: JSON 格式化输出（ResponseFormat）")
	fmt.Println("-----------------------------------")
	if err := runJSONFormatExample(apiKey); err != nil {
		fmt.Printf("示例 5 失败: %v\n", err)
	}

	// 示例 6: 超长上下文能力演示
	fmt.Println("\n示例 6: 超长上下文能力演示")
	fmt.Println("-----------------------------------")
	if err := runLongContextExample(apiKey); err != nil {
		fmt.Printf("示例 6 失败: %v\n", err)
	}

	fmt.Println("\n所有示例完成!")
}

// runSimpleBuilderBasic 演示 NewSimpleBuilder 基础用法
func runSimpleBuilderBasic(apiKey string) error {
	// 创建 Kimi LLM 客户端
	fmt.Println("创建 Kimi LLM 客户端...")
	llmClient, err := providers.NewKimiWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("moonshot-v1-8k"), // 8K 上下文模型
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(2000),
	)
	if err != nil {
		return fmt.Errorf("创建 Kimi 客户端失败: %w", err)
	}

	// 使用 NewSimpleBuilder - 无需泛型参数
	fmt.Println("使用 NewSimpleBuilder 构建 Agent...")
	agent, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是 Kimi，由月之暗面科技开发的智能助手。你擅长用简洁明了的语言回答问题。").
		WithState(core.NewAgentState()).
		Build()
	if err != nil {
		return fmt.Errorf("构建 Agent 失败: %w", err)
	}

	// 运行 Agent
	fmt.Println("运行 Agent...")
	ctx := context.Background()
	input := "请用一句话介绍 Go 语言的主要特点"

	output, err := agent.Execute(ctx, input)
	if err != nil {
		return fmt.Errorf("agent 执行失败: %w", err)
	}

	// 显示结果
	fmt.Println("\n结果:")
	fmt.Printf("回复: %v\n", output.Result)
	if output.Duration > 0 {
		fmt.Printf("耗时: %v\n", output.Duration)
	}
	// 显示 Token 使用统计
	if output.TokenUsage != nil {
		fmt.Println("\nToken 使用统计:")
		fmt.Printf("  输入 Tokens: %d\n", output.TokenUsage.PromptTokens)
		fmt.Printf("  输出 Tokens: %d\n", output.TokenUsage.CompletionTokens)
		fmt.Printf("  总计 Tokens: %d\n", output.TokenUsage.TotalTokens)
	}

	return nil
}

// runQuickAgent 演示 QuickAgent 快速创建
func runQuickAgent(apiKey string) error {
	// 创建 Kimi LLM 客户端
	llmClient, err := providers.NewKimiWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("moonshot-v1-8k"),
		llm.WithTemperature(0.7),
	)
	if err != nil {
		return fmt.Errorf("创建 Kimi 客户端失败: %w", err)
	}

	// 使用 QuickAgent - 一行代码创建 Agent
	fmt.Println("使用 QuickAgent 快速创建...")
	agent, err := builder.QuickAgent(llmClient, "你是一个专业的技术顾问。")
	if err != nil {
		return fmt.Errorf("创建 QuickAgent 失败: %w", err)
	}

	// 运行 Agent
	ctx := context.Background()
	output, err := agent.Execute(ctx, "什么是微服务架构？请简要说明。")
	if err != nil {
		return fmt.Errorf("agent 执行失败: %w", err)
	}

	fmt.Println("\n结果:")
	fmt.Printf("回复: %v\n", output.Result)
	// 显示 Token 使用统计
	if output.TokenUsage != nil {
		fmt.Println("\nToken 使用统计:")
		fmt.Printf("  输入 Tokens: %d\n", output.TokenUsage.PromptTokens)
		fmt.Printf("  输出 Tokens: %d\n", output.TokenUsage.CompletionTokens)
		fmt.Printf("  总计 Tokens: %d\n", output.TokenUsage.TotalTokens)
	}
	return nil
}

// runSimpleBuilderWithTools 演示 SimpleBuilder + 工具
func runSimpleBuilderWithTools(apiKey string) error {
	// 创建 Kimi LLM 客户端
	llmClient, err := providers.NewKimiWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("moonshot-v1-8k"),
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(2000),
	)
	if err != nil {
		return fmt.Errorf("创建 Kimi 客户端失败: %w", err)
	}

	// 创建工具
	fmt.Println("创建工具...")
	calculatorTool := createCalculatorTool()
	timeTool := createTimeTool()

	// 使用 NewSimpleBuilder 构建带工具的 Agent
	fmt.Println("构建带工具的 Agent...")
	agent, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个智能助手，可以使用工具来帮助用户完成任务。").
		WithTools(calculatorTool, timeTool).
		WithState(core.NewAgentState()).
		WithMaxIterations(10).
		WithTimeout(30 * time.Second).
		Build()
	if err != nil {
		return fmt.Errorf("构建 Agent 失败: %w", err)
	}

	// 运行任务
	fmt.Println("运行任务...")
	ctx := context.Background()
	input := "请帮我计算 25 * 4，然后告诉我现在的时间"

	output, err := agent.ExecuteWithTools(ctx, input)
	if err != nil {
		return fmt.Errorf("agent 执行失败: %w", err)
	}

	fmt.Println("\n结果:")
	fmt.Printf("回复: %v\n", output.Result)
	fmt.Printf("耗时: %v\n", output.Duration)
	// 显示 Token 使用统计
	if output.TokenUsage != nil {
		fmt.Println("\nToken 使用统计:")
		fmt.Printf("  输入 Tokens: %d\n", output.TokenUsage.PromptTokens)
		fmt.Printf("  输出 Tokens: %d\n", output.TokenUsage.CompletionTokens)
		fmt.Printf("  总计 Tokens: %d\n", output.TokenUsage.TotalTokens)
	}

	return nil
}

// runPresetAgents 演示预设配置快速构建
func runPresetAgents(apiKey string) error {
	// 创建 Kimi LLM 客户端
	llmClient, err := providers.NewKimiWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("moonshot-v1-8k"),
		llm.WithTemperature(0.7),
	)
	if err != nil {
		return fmt.Errorf("创建 Kimi 客户端失败: %w", err)
	}

	// 累计 Token 使用统计
	var totalPromptTokens, totalCompletionTokens, totalTokens int

	// 1. ChatAgent - 聊天机器人
	fmt.Println("1. 创建 ChatAgent...")
	chatAgent, err := builder.ChatAgent(llmClient, "用户")
	if err != nil {
		return fmt.Errorf("创建 ChatAgent 失败: %w", err)
	}

	ctx := context.Background()
	output, err := chatAgent.Execute(ctx, "你好！")
	if err != nil {
		return fmt.Errorf("ChatAgent 执行失败: %w", err)
	}
	fmt.Printf("ChatAgent 回复: %v\n", output.Result)
	if output.TokenUsage != nil {
		totalPromptTokens += output.TokenUsage.PromptTokens
		totalCompletionTokens += output.TokenUsage.CompletionTokens
		totalTokens += output.TokenUsage.TotalTokens
	}

	// 2. AnalysisAgent - 数据分析
	fmt.Println("\n2. 创建 AnalysisAgent...")
	analysisAgent, err := builder.AnalysisAgent(llmClient, nil)
	if err != nil {
		return fmt.Errorf("创建 AnalysisAgent 失败: %w", err)
	}

	output, err = analysisAgent.Execute(ctx, "分析一下：数字 1,2,3,4,5 的平均值和总和是多少？")
	if err != nil {
		return fmt.Errorf("AnalysisAgent 执行失败: %w", err)
	}
	fmt.Printf("AnalysisAgent 回复: %v\n", output.Result)
	if output.TokenUsage != nil {
		totalPromptTokens += output.TokenUsage.PromptTokens
		totalCompletionTokens += output.TokenUsage.CompletionTokens
		totalTokens += output.TokenUsage.TotalTokens
	}

	// 3. RAGAgent - RAG 系统
	fmt.Println("\n3. 创建 RAGAgent...")
	ragAgent, err := builder.RAGAgent(llmClient, nil)
	if err != nil {
		return fmt.Errorf("创建 RAGAgent 失败: %w", err)
	}

	output, err = ragAgent.Execute(ctx, "请总结一下 Go 语言的并发特性。")
	if err != nil {
		return fmt.Errorf("RAGAgent 执行失败: %w", err)
	}
	fmt.Printf("RAGAgent 回复: %v\n", output.Result)
	if output.TokenUsage != nil {
		totalPromptTokens += output.TokenUsage.PromptTokens
		totalCompletionTokens += output.TokenUsage.CompletionTokens
		totalTokens += output.TokenUsage.TotalTokens
	}

	// 显示累计 Token 使用统计
	fmt.Println("\n累计 Token 使用统计:")
	fmt.Printf("  输入 Tokens: %d\n", totalPromptTokens)
	fmt.Printf("  输出 Tokens: %d\n", totalCompletionTokens)
	fmt.Printf("  总计 Tokens: %d\n", totalTokens)

	return nil
}

// runJSONFormatExample 演示 JSON 格式化输出（ResponseFormat）
func runJSONFormatExample(apiKey string) error {
	// 创建支持 JSON 格式输出的 Kimi LLM 客户端
	fmt.Println("创建支持 JSON 格式输出的 Kimi 客户端...")
	llmClient, err := providers.NewKimiWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("moonshot-v1-8k"),
		llm.WithTemperature(0.3), // 较低温度确保 JSON 格式稳定
		llm.WithMaxTokens(1000),
		llm.WithJSONResponse(), // 启用 JSON 格式输出
	)
	if err != nil {
		return fmt.Errorf("创建 Kimi 客户端失败: %w", err)
	}

	// 使用 NewSimpleBuilder 构建 Agent
	fmt.Println("构建支持 JSON 输出的 Agent...")
	agent, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt(`你是一个数据分析助手，必须以 JSON 格式回复。
请严格按照以下格式输出：
{
  "analysis": "分析内容",
  "result": "结果",
  "confidence": 0.0-1.0之间的数字
}`).
		WithState(core.NewAgentState()).
		Build()
	if err != nil {
		return fmt.Errorf("构建 Agent 失败: %w", err)
	}

	// 运行 Agent
	fmt.Println("请求 JSON 格式响应...")
	ctx := context.Background()
	input := "分析数字序列 2, 4, 6, 8, 10 的规律"

	output, err := agent.Execute(ctx, input)
	if err != nil {
		return fmt.Errorf("agent 执行失败: %w", err)
	}

	// 显示 JSON 格式结果
	fmt.Println("\n结果（JSON 格式）:")
	fmt.Printf("%v\n", output.Result)

	// 尝试解析 JSON 验证格式
	var jsonResult map[string]interface{}
	resultStr, ok := output.Result.(string)
	if ok {
		if err := json.Unmarshal([]byte(resultStr), &jsonResult); err == nil {
			fmt.Println("\n解析后的 JSON 字段:")
			for key, value := range jsonResult {
				fmt.Printf("  %s: %v\n", key, value)
			}
		}
	}

	// 显示 Token 使用统计
	if output.TokenUsage != nil {
		fmt.Println("\nToken 使用统计:")
		fmt.Printf("  输入 Tokens: %d\n", output.TokenUsage.PromptTokens)
		fmt.Printf("  输出 Tokens: %d\n", output.TokenUsage.CompletionTokens)
		fmt.Printf("  总计 Tokens: %d\n", output.TokenUsage.TotalTokens)
	}

	return nil
}

// runLongContextExample 演示 Kimi 的超长上下文能力
func runLongContextExample(apiKey string) error {
	// 创建使用 128K 上下文的 Kimi LLM 客户端
	fmt.Println("创建 128K 上下文的 Kimi 客户端...")
	llmClient, err := providers.NewKimiWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("moonshot-v1-128k"), // 128K 上下文模型
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(2000),
	)
	if err != nil {
		return fmt.Errorf("创建 Kimi 客户端失败: %w", err)
	}

	// 构建 Agent
	fmt.Println("构建支持超长上下文的 Agent...")
	agent, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是 Kimi，一个擅长处理长文本的智能助手。你可以分析和总结大量文本内容。").
		WithState(core.NewAgentState()).
		Build()
	if err != nil {
		return fmt.Errorf("构建 Agent 失败: %w", err)
	}

	// 模拟长文本输入
	longText := generateSampleLongText()
	fmt.Printf("输入文本长度: %d 字符\n", len(longText))

	// 运行 Agent
	fmt.Println("请求分析长文本...")
	ctx := context.Background()

	output, err := agent.Execute(ctx, longText)
	if err != nil {
		return fmt.Errorf("agent 执行失败: %w", err)
	}

	// 显示结果
	fmt.Println("\n分析结果:")
	fmt.Printf("%v\n", output.Result)

	// 显示 Token 使用统计
	if output.TokenUsage != nil {
		fmt.Println("\nToken 使用统计:")
		fmt.Printf("  输入 Tokens: %d\n", output.TokenUsage.PromptTokens)
		fmt.Printf("  输出 Tokens: %d\n", output.TokenUsage.CompletionTokens)
		fmt.Printf("  总计 Tokens: %d\n", output.TokenUsage.TotalTokens)
	}

	return nil
}

// generateSampleLongText 生成示例长文本
func generateSampleLongText() string {
	return `请帮我总结以下关于 Go 语言的技术文档：

# Go 语言概述

Go（又称 Golang）是 Google 开发的一种静态强类型、编译型、并发型编程语言。
它由 Robert Griesemer、Rob Pike 和 Ken Thompson 于 2007 年开始设计，2009 年正式发布。

## 主要特点

1. 简洁的语法：Go 语言设计简洁，关键字只有 25 个
2. 高效的并发：goroutine 和 channel 提供了优雅的并发编程模型
3. 快速编译：Go 编译器可以在几秒内编译大型项目
4. 内置垃圾回收：自动内存管理，减轻开发者负担
5. 丰富的标准库：涵盖网络、加密、文件系统等常用功能

## 应用场景

Go 语言广泛应用于：
- 云计算和容器技术（Docker、Kubernetes）
- 微服务架构
- 网络编程和 API 开发
- 命令行工具
- 分布式系统

请用 3 个要点总结 Go 语言的核心优势。`
}

// createCalculatorTool 创建计算器工具
func createCalculatorTool() interfaces.Tool {
	tool, err := tools.NewFunctionToolBuilder("calculator").
		WithDescription("执行数学计算，支持基本的加减乘除运算。输入格式：'15 * 8'").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"expression": {
					"type": "string",
					"description": "要计算的数学表达式，如 '15 * 8'"
				}
			},
			"required": ["expression"]
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			expression, ok := args["expression"].(string)
			if !ok {
				return nil, fmt.Errorf("需要提供 expression 参数")
			}

			// 简化的计算器实现
			parts := strings.Fields(expression)
			if len(parts) == 3 {
				var num1, num2 float64
				var op string
				if _, err := fmt.Sscanf(parts[0], "%f", &num1); err != nil {
					return nil, fmt.Errorf("无效的第一个数字: %w", err)
				}
				op = parts[1]
				if _, err := fmt.Sscanf(parts[2], "%f", &num2); err != nil {
					return nil, fmt.Errorf("无效的第二个数字: %w", err)
				}

				var result float64
				switch op {
				case "+":
					result = num1 + num2
				case "-":
					result = num1 - num2
				case "*":
					result = num1 * num2
				case "/":
					if num2 == 0 {
						return nil, fmt.Errorf("除数不能为零")
					}
					result = num1 / num2
				default:
					return nil, fmt.Errorf("不支持的运算符: %s", op)
				}

				return map[string]interface{}{
					"expression": expression,
					"result":     result,
				}, nil
			}

			return nil, fmt.Errorf("无效的表达式格式")
		}).
		Build()

	if err != nil {
		panic(fmt.Sprintf("创建计算器工具失败: %v", err))
	}
	return tool
}

// createTimeTool 创建时间查询工具
func createTimeTool() interfaces.Tool {
	tool, err := tools.NewFunctionToolBuilder("get_current_time").
		WithDescription("获取当前时间").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"timezone": {
					"type": "string",
					"description": "时区，如 'Asia/Shanghai', 'UTC'",
					"default": "Asia/Shanghai"
				}
			}
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			timezone := "Asia/Shanghai"
			if tz, ok := args["timezone"].(string); ok && tz != "" {
				timezone = tz
			}

			loc, err := time.LoadLocation(timezone)
			if err != nil {
				loc = time.UTC
				timezone = "UTC"
			}

			now := time.Now().In(loc)

			return map[string]interface{}{
				"time":      now.Format("2006-01-02 15:04:05"),
				"timezone":  timezone,
				"timestamp": now.Unix(),
				"weekday":   now.Weekday().String(),
			}, nil
		}).
		Build()

	if err != nil {
		panic(fmt.Sprintf("创建时间工具失败: %v", err))
	}
	return tool
}

// runMockExample 在没有 API Key 时运行的模拟示例
func runMockExample() {
	fmt.Println("模拟模式示例")
	fmt.Println("-----------------------------------")
	fmt.Println("\n简化 Builder API 使用方法（Kimi 版）：")
	fmt.Println()
	fmt.Println("1. NewSimpleBuilder - 基础用法（推荐）:")
	fmt.Print(`
   // 无需泛型参数，使用更简洁
   llmClient, _ := providers.NewKimiWithOptions(
       llm.WithAPIKey(apiKey),
       llm.WithModel("moonshot-v1-8k"),
   )
   agent, err := builder.NewSimpleBuilder(llmClient).
       WithSystemPrompt("你是 Kimi，一个友好的 AI 助手").
       WithState(core.NewAgentState()).
       Build()
`)
	fmt.Println("2. QuickAgent - 一行创建:")
	fmt.Print(`
   // 最简单的方式，快速创建 Agent
   agent, err := builder.QuickAgent(llmClient, "你是一个技术顾问")
`)
	fmt.Println("3. Kimi 模型选择:")
	fmt.Print(`
   // moonshot-v1-8k   - 8K 上下文，适合一般对话
   // moonshot-v1-32k  - 32K 上下文，适合长文档
   // moonshot-v1-128k - 128K 上下文，适合超长文档

   llmClient, _ := providers.NewKimiWithOptions(
       llm.WithAPIKey(apiKey),
       llm.WithModel("moonshot-v1-128k"), // 超长上下文
   )
`)
	fmt.Println("4. 预设 Agent 类型:")
	fmt.Print(`
   // ChatAgent - 聊天机器人
   chatAgent, _ := builder.ChatAgent(llmClient, "用户名")

   // AnalysisAgent - 数据分析
   analysisAgent, _ := builder.AnalysisAgent(llmClient, dataSource)

   // RAGAgent - RAG 系统
   ragAgent, _ := builder.RAGAgent(llmClient, retriever)
`)
	fmt.Println()
	fmt.Println("配置步骤:")
	fmt.Println("   1. 访问 https://platform.moonshot.cn/ 获取 API Key")
	fmt.Println("   2. 设置环境变量: export KIMI_API_KEY=your-key")
	fmt.Println("      或者: export MOONSHOT_API_KEY=your-key")
	fmt.Println("   3. 运行此程序: go run main.go")
}
