// Package main 演示工具与 Agent 集成的使用方法
// 本示例展示如何将工具集成到 Agent 中进行自动调用
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/compute"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          工具与 Agent 集成示例                                 ║")
	fmt.Println("║   展示如何将工具集成到 Agent 中进行自动调用                    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 检查 API Key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("⚠️  警告: 未设置 OPENAI_API_KEY 环境变量")
		fmt.Println("   本示例将展示工具创建和注册流程，但不会执行实际的 LLM 调用")
		fmt.Println()
		demonstrateToolsOnly(ctx)
		return
	}

	// 1. 创建 LLM 客户端
	fmt.Println("【步骤 1】创建 LLM 客户端")
	fmt.Println("────────────────────────────────────────")

	client, err := providers.NewOpenAIWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("gpt-4"),
		llm.WithTemperature(0.7),
	)
	if err != nil {
		fmt.Printf("✗ 创建 LLM 客户端失败: %v\n", err)
		return
	}

	fmt.Println("✓ LLM 客户端创建成功")
	fmt.Printf("  提供商: %s\n", client.Provider())
	fmt.Println()

	// 2. 创建工具集合
	fmt.Println("【步骤 2】创建工具集合")
	fmt.Println("────────────────────────────────────────")

	// 计算器工具
	calculator := compute.NewCalculatorTool()
	fmt.Printf("✓ 创建工具: %s\n", calculator.Name())

	// 天气工具（自定义）
	weatherTool := createWeatherTool()
	fmt.Printf("✓ 创建工具: %s\n", weatherTool.Name())

	// 时间工具（自定义）
	timeTool := createTimeTool()
	fmt.Printf("✓ 创建工具: %s\n", timeTool.Name())

	// 翻译工具（自定义）
	translateTool := createTranslateTool()
	fmt.Printf("✓ 创建工具: %s\n", translateTool.Name())

	toolList := []interfaces.Tool{calculator, weatherTool, timeTool, translateTool}
	fmt.Printf("\n共创建 %d 个工具\n", len(toolList))
	fmt.Println()

	// 3. 创建带工具的 Agent
	fmt.Println("【步骤 3】创建带工具的 Agent")
	fmt.Println("────────────────────────────────────────")

	agent, err := builder.NewSimpleBuilder(client).
		WithSystemPrompt(`你是一个智能助手，可以使用以下工具来帮助用户：
- calculator: 数学计算
- weather: 查询天气
- current_time: 获取当前时间
- translate: 翻译文本

根据用户的问题，选择合适的工具来回答。如果不需要使用工具，直接回答即可。`).
		WithTools(toolList...).
		WithMaxIterations(5).
		WithTimeout(60 * time.Second).
		WithVerbose(true).
		Build()

	if err != nil {
		fmt.Printf("✗ 创建 Agent 失败: %v\n", err)
		return
	}

	fmt.Println("✓ Agent 创建成功")
	fmt.Println()

	// 4. 执行测试查询
	fmt.Println("【步骤 4】执行测试查询")
	fmt.Println("────────────────────────────────────────")

	testQueries := []string{
		"计算 (25 + 17) * 3 的结果",
		"北京今天的天气怎么样？",
		"现在几点了？",
		"把 'Hello World' 翻译成中文",
	}

	for i, query := range testQueries {
		fmt.Printf("\n查询 %d: %s\n", i+1, query)
		fmt.Println("─────────────────────────────")

		output, err := agent.ExecuteWithTools(ctx, query)

		if err != nil {
			fmt.Printf("✗ 执行失败: %v\n", err)
			continue
		}

		if output != nil && output.Result != nil {
			response := fmt.Sprintf("%v", output.Result)
			if len(response) > 500 {
				response = response[:500] + "..."
			}
			fmt.Printf("回答: %s\n", response)
		}

		// 显示工具调用信息
		if output != nil && output.Metadata != nil {
			if toolCalls, ok := output.Metadata["tool_calls"]; ok {
				fmt.Printf("工具调用: %v\n", toolCalls)
			}
		}
	}
	fmt.Println()

	// 总结
	printSummary()
}

// demonstrateToolsOnly 仅演示工具功能（无 LLM）
func demonstrateToolsOnly(ctx context.Context) {
	fmt.Println("【演示模式】仅展示工具功能")
	fmt.Println("────────────────────────────────────────")

	// 创建工具
	calculator := compute.NewCalculatorTool()
	weatherTool := createWeatherTool()
	timeTool := createTimeTool()
	translateTool := createTranslateTool()

	// 测试计算器
	fmt.Println("\n1. 计算器工具测试:")
	calcOutput, _ := calculator.Invoke(ctx, &interfaces.ToolInput{
		Args:    map[string]interface{}{"expression": "(25 + 17) * 3"},
		Context: ctx,
	})
	if calcOutput.Success {
		fmt.Printf("   (25 + 17) * 3 = %v\n", calcOutput.Result)
	}

	// 测试天气
	fmt.Println("\n2. 天气工具测试:")
	weatherOutput, _ := weatherTool.Invoke(ctx, &interfaces.ToolInput{
		Args:    map[string]interface{}{"city": "北京"},
		Context: ctx,
	})
	if weatherOutput.Success {
		fmt.Printf("   %v\n", weatherOutput.Result)
	}

	// 测试时间
	fmt.Println("\n3. 时间工具测试:")
	timeOutput, _ := timeTool.Invoke(ctx, &interfaces.ToolInput{
		Args:    map[string]interface{}{"format": "human"},
		Context: ctx,
	})
	if timeOutput.Success {
		fmt.Printf("   %v\n", timeOutput.Result)
	}

	// 测试翻译
	fmt.Println("\n4. 翻译工具测试:")
	translateOutput, _ := translateTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"text":   "Hello World",
			"source": "en",
			"target": "zh",
		},
		Context: ctx,
	})
	if translateOutput.Success {
		fmt.Printf("   %v\n", translateOutput.Result)
	}

	fmt.Println()
	printSummary()
}

// createWeatherTool 创建天气查询工具
func createWeatherTool() interfaces.Tool {
	return tools.NewFunctionToolBuilder("weather").
		WithDescription("查询指定城市的天气信息").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"city": {
					"type": "string",
					"description": "城市名称"
				}
			},
			"required": ["city"]
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			city, _ := args["city"].(string)

			// 模拟天气数据
			conditions := []string{"晴", "多云", "阴", "小雨", "大雨"}
			temp := 15 + rand.Intn(20)
			humidity := 30 + rand.Intn(50)

			return map[string]interface{}{
				"city":        city,
				"temperature": fmt.Sprintf("%d°C", temp),
				"condition":   conditions[rand.Intn(len(conditions))],
				"humidity":    fmt.Sprintf("%d%%", humidity),
				"wind":        fmt.Sprintf("%d级", 1+rand.Intn(5)),
			}, nil
		}).
		MustBuild()
}

// createTimeTool 创建时间查询工具
func createTimeTool() interfaces.Tool {
	return tools.NewFunctionToolBuilder("current_time").
		WithDescription("获取当前时间").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"format": {
					"type": "string",
					"enum": ["rfc3339", "human", "date", "time"],
					"default": "human",
					"description": "时间格式"
				}
			}
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			format := "human"
			if f, ok := args["format"].(string); ok {
				format = f
			}

			now := time.Now()
			var result string

			switch format {
			case "rfc3339":
				result = now.Format(time.RFC3339)
			case "date":
				result = now.Format("2006-01-02")
			case "time":
				result = now.Format("15:04:05")
			default:
				result = now.Format("2006年01月02日 15:04:05")
			}

			return map[string]interface{}{
				"time":     result,
				"timezone": now.Location().String(),
			}, nil
		}).
		MustBuild()
}

// createTranslateTool 创建翻译工具
func createTranslateTool() interfaces.Tool {
	return tools.NewFunctionToolBuilder("translate").
		WithDescription("翻译文本（模拟）").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"text": {
					"type": "string",
					"description": "要翻译的文本"
				},
				"source": {
					"type": "string",
					"description": "源语言（如 en, zh, ja）"
				},
				"target": {
					"type": "string",
					"description": "目标语言（如 en, zh, ja）"
				}
			},
			"required": ["text", "target"]
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			text, _ := args["text"].(string)
			target, _ := args["target"].(string)

			// 简单的模拟翻译
			translations := map[string]map[string]string{
				"Hello World": {
					"zh": "你好，世界",
					"ja": "こんにちは世界",
				},
				"Good morning": {
					"zh": "早上好",
					"ja": "おはようございます",
				},
			}

			result := text
			if trans, ok := translations[text]; ok {
				if t, ok := trans[target]; ok {
					result = t
				}
			}

			return map[string]interface{}{
				"original":   text,
				"translated": result,
				"target":     target,
			}, nil
		}).
		MustBuild()
}

func printSummary() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("本示例演示了工具与 Agent 的集成:")
	fmt.Println("  ✓ 创建多种自定义工具")
	fmt.Println("  ✓ 将工具注册到 Agent")
	fmt.Println("  ✓ Agent 根据用户问题自动选择工具")
	fmt.Println("  ✓ 工具执行结果整合到回答中")
	fmt.Println()
	fmt.Println("💡 集成最佳实践:")
	fmt.Println("  - 为每个工具提供清晰的名称和描述")
	fmt.Println("  - 在 System Prompt 中说明可用工具")
	fmt.Println("  - 设置合理的 MaxIterations 防止无限循环")
	fmt.Println("  - 处理工具执行失败的情况")
	fmt.Println()
	fmt.Println("更多示例请参考 examples/agents/ 目录")
}
