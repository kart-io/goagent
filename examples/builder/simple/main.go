// Simple API 示例
// 展示 Builder API 的 Simple 层级（5-8 个方法，覆盖 80% 使用场景）
//
// 本示例演示：
// 1. 最简单的 Agent 创建（3 行代码）
// 2. 带工具的 Agent
// 3. 调整常用配置（MaxIterations, Temperature, OutputFormat）
// 4. 使用快速构建函数
// 5. 输出格式控制（预定义格式和自定义格式）
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("=== Builder API - Simple 层级示例 ===")

	// 检查 API Key
	deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
	kimiKey := os.Getenv("KIMI_API_KEY")

	if deepseekKey == "" && kimiKey == "" {
		fmt.Println("⚠️  警告：未设置 DEEPSEEK_API_KEY 或 KIMI_API_KEY 环境变量")
		fmt.Println("\n配置步骤：")
		fmt.Println("  1. 获取 API Key：")
		fmt.Println("     - DeepSeek: https://platform.deepseek.com/")
		fmt.Println("     - Kimi: https://platform.moonshot.cn/")
		fmt.Println("  2. 设置环境变量：")
		fmt.Println("     export DEEPSEEK_API_KEY=your-deepseek-key")
		fmt.Println("     或")
		fmt.Println("     export KIMI_API_KEY=your-kimi-key")
		return
	}

	// 优先使用 DeepSeek，如果没有则使用 Kimi
	var apiKey string
	var providerName string
	if deepseekKey != "" {
		apiKey = deepseekKey
		providerName = "DeepSeek"
	} else {
		apiKey = kimiKey
		providerName = "Kimi"
	}

	fmt.Printf("使用 LLM 提供商: %s\n\n", providerName)

	// 示例 1: 最简单的 Agent（仅 3 行代码）
	example1SimpleAgent(apiKey, providerName)

	// 示例 2: 带工具的 Agent
	example2AgentWithTools(apiKey, providerName)

	// 示例 3: 调整常用配置
	example3ConfiguredAgent(apiKey, providerName)

	// 示例 4: 使用快速构建函数
	example4QuickAgent(apiKey, providerName)

	// 示例 5: 输出格式控制
	example5OutputFormat(apiKey, providerName)

	fmt.Println("\n✨ 所有示例完成！")
}

// createLLMClient 创建 LLM 客户端
func createLLMClient(apiKey, providerName string) (llm.Client, error) {
	if providerName == "DeepSeek" {
		return providers.NewDeepSeekWithOptions(
			llm.WithAPIKey(apiKey),
			llm.WithModel("deepseek-chat"),
			llm.WithTemperature(0.7),
			llm.WithMaxTokens(2000),
		)
	}
	// Kimi
	return providers.NewKimiWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("moonshot-v1-8k"),
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(2000),
	)
}

// 示例 1: 最简单的 Agent（仅 3 行代码）
//
// 使用的方法：
// - WithSystemPrompt (Simple)
// - Build (Simple)
func example1SimpleAgent(apiKey, providerName string) {
	fmt.Println("--- 示例 1: 最简单的 Agent ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 仅需 3 个方法调用！
	agent, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个翻译助手，专门将中文翻译成英文。").
		WithOutputFormat(builder.OutputFormatPlainText). // 使用纯文本格式
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
		fmt.Printf("输出: %s\n", result.Result)
	}

	fmt.Println()
}

// 示例 2: 带工具的 Agent
//
// 使用的方法：
// - WithSystemPrompt (Simple)
// - WithTools (Simple)
// - Build (Simple)
func example2AgentWithTools(apiKey, providerName string) {
	fmt.Println("--- 示例 2: 带工具的 Agent ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 创建计算器工具
	calculator := createCalculatorTool()

	// 添加工具到 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个数学助手，可以使用计算器工具来计算。").
		WithTools(calculator).
		WithOutputFormat(builder.OutputFormatPlainText). // 使用纯文本格式
		Build()
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 执行 Agent
	result, err := agent.Execute(context.Background(), "计算 123 加 456")
	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("输入: 计算 123 加 456\n")
		fmt.Printf("输出: %v\n", result.Result)
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
// - WithOutputFormat (Simple) - 控制输出格式
// - Build (Simple)
func example3ConfiguredAgent(apiKey, providerName string) {
	fmt.Println("--- 示例 3: 调整常用配置 ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 创建时间工具
	timeTool := createTimeTool()

	// 配置 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个时间助手，可以告诉用户当前时间。").
		WithTools(timeTool).
		WithMaxIterations(15).                           // 允许更多推理步骤（默认 10）
		WithTemperature(0.3).                            // 降低创造性，提高精确性（默认 0.7）
		WithOutputFormat(builder.OutputFormatPlainText). // 使用纯文本格式
		Build()
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 执行 Agent
	result, err := agent.Execute(context.Background(), "现在几点了？")
	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("输入: 现在几点了？\n")
		fmt.Printf("输出: %v\n", result.Result)
	}

	fmt.Println()
}

// 示例 4: 使用快速构建函数
//
// 快速构建函数提供了最简单的 API（1 行代码）
func example4QuickAgent(apiKey, providerName string) {
	fmt.Println("--- 示例 4: 使用快速构建函数 ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 方式 1: 使用 QuickAgent（最简单）
	agent1, err := builder.QuickAgent(llmClient, "你是一个问答助手。")
	if err != nil {
		log.Printf("QuickAgent 创建失败: %v", err)
	} else {
		result, _ := agent1.Execute(context.Background(), "什么是 Go 语言？")
		fmt.Printf("QuickAgent 输出: %v\n", result.Result)
	}

	// 方式 2: 使用场景预设 - ChatAgent
	agent2, err := builder.ChatAgent(llmClient, "小明")
	if err != nil {
		log.Printf("ChatAgent 创建失败: %v", err)
	} else {
		result, _ := agent2.Execute(context.Background(), "你好！")
		fmt.Printf("ChatAgent 输出: %v\n", result.Result)
	}

	fmt.Println()
}

// createCalculatorTool 创建计算器工具（使用 FunctionToolBuilder）
func createCalculatorTool() interfaces.Tool {
	tool, err := tools.NewFunctionToolBuilder("calculator").
		WithDescription("执行数学计算，支持基本的加减乘除运算。输入格式：'15 * 8'").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"expression": {
					"type": "string",
					"description": "要计算的数学表达式，如 '15 * 8' 或 '123 + 456'"
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
				case "+", "加":
					result = num1 + num2
				case "-", "减":
					result = num1 - num2
				case "*", "乘":
					result = num1 * num2
				case "/", "除":
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

// createTimeTool 创建时间工具
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

// 示例 5: 输出格式控制
//
// 使用的方法：
// - WithSystemPrompt (Simple)
// - WithOutputFormat (Simple) - 预定义格式
// - WithCustomOutputFormat (Simple) - 自定义格式
// - Build (Simple)
//
// 支持的预定义格式：
// - OutputFormatDefault: 不指定格式，由 LLM 自行决定
// - OutputFormatPlainText: 纯文本格式，不使用 Markdown
// - OutputFormatMarkdown: Markdown 格式
// - OutputFormatJSON: JSON 格式
func example5OutputFormat(apiKey, providerName string) {
	fmt.Println("--- 示例 5: 输出格式控制 ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	ctx := context.Background()

	// 5.1 使用纯文本格式（适合终端显示）
	fmt.Println("\n5.1 纯文本格式:")
	agent1, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个知识助手").
		WithOutputFormat(builder.OutputFormatPlainText).
		Build()
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
	} else {
		result, _ := agent1.Execute(ctx, "列出 Go 语言的三个主要特点")
		fmt.Printf("输出: %v\n", result.Result)
	}

	// 5.2 使用 JSON 格式（适合程序解析）
	fmt.Println("\n5.2 JSON 格式:")
	agent2, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个数据助手").
		WithOutputFormat(builder.OutputFormatJSON).
		Build()
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
	} else {
		result, _ := agent2.Execute(ctx, "返回一个包含 name 和 age 字段的用户信息示例")
		fmt.Printf("输出: %v\n", result.Result)
	}

	// 5.3 使用自定义格式
	fmt.Println("\n5.3 自定义格式:")
	agent3, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个技术文档助手").
		WithCustomOutputFormat("请按以下格式回复：\n【摘要】一句话概述\n【详情】详细说明\n【建议】给出建议").
		Build()
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
	} else {
		result, _ := agent3.Execute(ctx, "介绍一下 Go 的并发模型")
		fmt.Printf("输出:\n%v\n", result.Result)
	}

	// 5.4 使用 Markdown 格式（适合富文本显示）
	fmt.Println("\n5.4 Markdown 格式:")
	agent4, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个文档助手").
		WithOutputFormat(builder.OutputFormatMarkdown).
		Build()
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
	} else {
		result, _ := agent4.Execute(ctx, "写一个简单的 Hello World 代码示例")
		fmt.Printf("输出:\n%v\n", result.Result)
	}

	fmt.Println()
}
