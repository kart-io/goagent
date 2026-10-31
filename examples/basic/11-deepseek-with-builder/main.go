// Package main demonstrates using DeepSeek LLM with AgentBuilder
//
// This example shows how to use the fluent AgentBuilder API with DeepSeek:
// - Creating DeepSeek LLM client with options
// - Building agents with builder pattern
// - Using tools with agents
// - Configuring middleware and callbacks
// - Streaming responses
// - Custom configuration
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
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
	fmt.Println("========================================")
	fmt.Println("DeepSeek Agent Builder 示例")
	fmt.Println("========================================")
	fmt.Println()

	// 从环境变量获取 API Key
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("⚠️  警告：未设置 DEEPSEEK_API_KEY 环境变量")
		fmt.Println("提示：export DEEPSEEK_API_KEY=your-api-key")
		fmt.Println("\n使用模拟模式运行示例...")
		runMockExample()
		return
	}

	// 示例 1: 基础 Agent Builder
	fmt.Println("示例 1: 基础 Agent Builder")
	fmt.Println("----------------------------")
	if err := runBasicAgentBuilder(apiKey); err != nil {
		fmt.Printf("❌ 示例 1 失败: %v\n", err)
	}

	// 示例 2: Agent Builder + 工具
	fmt.Println("\n示例 2: Agent Builder + 工具")
	fmt.Println("----------------------------")
	if err := runAgentBuilderWithTools(apiKey); err != nil {
		fmt.Printf("❌ 示例 2 失败: %v\n", err)
	}

	// 示例 3: Agent Builder + 中间件
	fmt.Println("\n示例 3: Agent Builder + 中间件")
	fmt.Println("----------------------------")
	if err := runAgentBuilderWithMiddleware(apiKey); err != nil {
		fmt.Printf("❌ 示例 3 失败: %v\n", err)
	}

	// 示例 4: 自定义配置
	fmt.Println("\n示例 4: 自定义配置")
	fmt.Println("----------------------------")
	if err := runAgentBuilderWithConfig(apiKey); err != nil {
		fmt.Printf("❌ 示例 4 失败: %v\n", err)
	}

	// 示例 5: 聊天机器人配置
	fmt.Println("\n示例 5: 聊天机器人配置")
	fmt.Println("----------------------------")
	if err := runChatbotAgent(apiKey); err != nil {
		fmt.Printf("❌ 示例 5 失败: %v\n", err)
	}

	fmt.Println("\n✨ 所有示例完成!")
}

// runBasicAgentBuilder 演示基础 Agent Builder 使用
func runBasicAgentBuilder(apiKey string) error {
	// 步骤 1: 创建 DeepSeek LLM 客户端
	fmt.Println("📡 创建 DeepSeek LLM 客户端...")
	llmClient, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(2000),
	)
	if err != nil {
		return fmt.Errorf("创建 DeepSeek 客户端失败: %w", err)
	}

	// 步骤 2: 使用 AgentBuilder 创建 Agent
	fmt.Println("🔨 使用 AgentBuilder 构建 Agent...")
	//nolint:staticcheck // Example demonstrates old API for backward compatibility
	agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
		WithSystemPrompt("你是一个友好的 AI 助手，擅长用简洁明了的语言回答问题。").
		WithState(agentstate.NewAgentState()).
		Build()
	if err != nil {
		return fmt.Errorf("构建 Agent 失败: %w", err)
	}

	// 步骤 3: 运行 Agent
	fmt.Println("🚀 运行 Agent...")
	ctx := context.Background()
	input := "请用一句话介绍 Go 语言的主要特点"

	output, err := agent.Execute(ctx, input)
	if err != nil {
		return fmt.Errorf("agent 执行失败: %w", err)
	}

	// 步骤 4: 显示结果
	fmt.Println("\n📝 结果:")
	fmt.Printf("回复: %v\n", output.Result)
	if output.Duration > 0 {
		fmt.Printf("耗时: %v\n", output.Duration)
	}

	return nil
}

// runAgentBuilderWithTools 演示 Agent Builder + 工具
func runAgentBuilderWithTools(apiKey string) error {
	// 创建 DeepSeek 客户端
	llmClient, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(2000),
	)
	if err != nil {
		return fmt.Errorf("创建 DeepSeek 客户端失败: %w", err)
	}

	// 创建工具
	fmt.Println("🔧 创建工具...")
	calculatorTool := createCalculatorTool()
	weatherTool := createWeatherTool()
	timeTool := createTimeTool()

	// 使用 AgentBuilder 创建带工具的 Agent
	fmt.Println("🔨 构建带工具的 Agent...")
	//nolint:staticcheck // Example demonstrates old API for backward compatibility
	agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
		WithSystemPrompt("你是一个智能助手，可以使用工具来帮助用户完成任务。").
		WithTools(calculatorTool, weatherTool, timeTool).
		WithState(agentstate.NewAgentState()).
		WithMaxIterations(10).
		WithTimeout(30 * time.Second).
		WithVerbose(true).
		Build()
	if err != nil {
		return fmt.Errorf("构建 Agent 失败: %w", err)
	}

	// 运行任务
	fmt.Println("🚀 运行任务...")
	ctx := context.Background()
	input := "请帮我计算 15 * 8，然后告诉我现在的时间"

	output, err := agent.ExecuteWithTools(ctx, input)
	if err != nil {
		return fmt.Errorf("agent 执行失败: %w", err)
	}

	// 显示结果
	fmt.Println("\n📝 结果:")
	fmt.Printf("回复: %v\n", output.Result)
	fmt.Printf("耗时: %v\n", output.Duration)

	return nil
}

// runAgentBuilderWithMiddleware 演示 Agent Builder + 中间件
func runAgentBuilderWithMiddleware(apiKey string) error {
	// 创建 DeepSeek 客户端
	llmClient, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0.7),
	)
	if err != nil {
		return fmt.Errorf("创建 DeepSeek 客户端失败: %w", err)
	}

	// 创建成本追踪回调
	fmt.Println("📊 配置成本追踪...")
	pricing := map[string]float64{
		"deepseek-chat": 0.21 / 1_000_000, // DeepSeek 平均定价：$0.21/M tokens
	}
	costTracker := core.NewCostTrackingCallback(pricing)

	// 使用 AgentBuilder 创建带中间件和回调的 Agent
	fmt.Println("🔨 构建 Agent...")
	//nolint:staticcheck // Example demonstrates old API for backward compatibility
	agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
		WithSystemPrompt("你是一个专业的 AI 助手。").
		WithState(agentstate.NewAgentState()).
		WithCallbacks(costTracker).
		WithVerbose(true). // 自动添加日志和计时中间件
		Build()
	if err != nil {
		return fmt.Errorf("构建 Agent 失败: %w", err)
	}

	// 运行任务
	fmt.Println("🚀 运行任务...")
	ctx := context.Background()
	input := "请简要解释什么是机器学习"

	output, err := agent.Execute(ctx, input)
	if err != nil {
		return fmt.Errorf("agent 执行失败: %w", err)
	}

	// 显示结果
	fmt.Println("\n📝 结果:")
	fmt.Printf("回复: %v\n", output.Result)
	fmt.Printf("耗时: %v\n", output.Duration)

	// 显示成本追踪信息
	fmt.Printf("\n💰 成本信息:\n")
	fmt.Printf("总 Tokens: %d\n", costTracker.GetTotalTokens())
	fmt.Printf("总成本: $%.6f\n", costTracker.GetTotalCost())

	return nil
}

// runAgentBuilderWithConfig 演示自定义配置
func runAgentBuilderWithConfig(apiKey string) error {
	// 创建 DeepSeek 客户端
	llmClient, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0.3), // 低温度，更加准确
		llm.WithMaxTokens(1000),
	)
	if err != nil {
		return fmt.Errorf("创建 DeepSeek 客户端失败: %w", err)
	}

	// 自定义配置
	fmt.Println("⚙️  配置自定义选项...")
	customConfig := &builder.AgentConfig{
		MaxIterations:  5,
		Timeout:        60 * time.Second,
		EnableAutoSave: false,
		MaxTokens:      1000,
		Temperature:    0.3,
		Verbose:        false,
	}

	// 使用 AgentBuilder 创建 Agent
	fmt.Println("🔨 构建 Agent...")
	//nolint:staticcheck // Example demonstrates old API for backward compatibility
	agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
		WithSystemPrompt("你是一个专业的技术文档写作助手，专注于准确性和清晰度。").
		WithState(agentstate.NewAgentState()).
		WithMaxIterations(customConfig.MaxIterations).
		WithTimeout(customConfig.Timeout).
		WithAutoSaveEnabled(customConfig.EnableAutoSave).
		WithMaxTokens(customConfig.MaxTokens).
		WithTemperature(customConfig.Temperature).
		WithVerbose(customConfig.Verbose).
		WithMetadata("version", "1.0").
		WithMetadata("purpose", "documentation").
		Build()
	if err != nil {
		return fmt.Errorf("构建 Agent 失败: %w", err)
	}

	// 运行任务
	fmt.Println("🚀 运行任务...")
	ctx := context.Background()
	input := "请用一段话解释什么是 RESTful API"

	output, err := agent.Execute(ctx, input)
	if err != nil {
		return fmt.Errorf("agent 执行失败: %w", err)
	}

	// 显示结果
	fmt.Println("\n📝 结果:")
	fmt.Printf("回复: %v\n", output.Result)

	return nil
}

// runChatbotAgent 演示聊天机器人配置
func runChatbotAgent(apiKey string) error {
	// 创建 DeepSeek 客户端
	llmClient, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0.8), // 更高的温度，更有创造性
	)
	if err != nil {
		return fmt.Errorf("创建 DeepSeek 客户端失败: %w", err)
	}

	// 使用聊天机器人预设配置
	fmt.Println("🤖 创建聊天机器人 Agent...")
	//nolint:staticcheck // Example demonstrates old API for backward compatibility
	agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
		WithSystemPrompt("你是一个友好、幽默的聊天机器人，喜欢用轻松的语气与用户交流。").
		WithState(agentstate.NewAgentState()).
		ConfigureForChatbot(). // 使用聊天机器人预设配置
		Build()
	if err != nil {
		return fmt.Errorf("构建 Agent 失败: %w", err)
	}

	// 运行多轮对话
	fmt.Println("💬 开始对话...")
	ctx := context.Background()

	conversations := []string{
		"你好！你能做什么？",
		"那你能给我讲个笑话吗？",
		"哈哈，很有趣！再见！",
	}

	for i, userMsg := range conversations {
		fmt.Printf("\n👤 用户: %s\n", userMsg)

		output, err := agent.Execute(ctx, userMsg)
		if err != nil {
			return fmt.Errorf("对话 %d 失败: %w", i+1, err)
		}

		fmt.Printf("🤖 助手: %v\n", output.Result)
	}

	return nil
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
			// 实际应用中应该使用表达式解析库
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

// createWeatherTool 创建天气查询工具
func createWeatherTool() interfaces.Tool {
	tool, err := tools.NewFunctionToolBuilder("get_weather").
		WithDescription("查询指定城市的天气信息").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"city": {
					"type": "string",
					"description": "要查询的城市名称，如 '北京'、'上海'"
				}
			},
			"required": ["city"]
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			city, ok := args["city"].(string)
			if !ok {
				return nil, fmt.Errorf("需要提供 city 参数")
			}

			// 模拟天气数据
			weatherData := map[string]interface{}{
				"city":        city,
				"weather":     "晴朗",
				"temperature": 22,
				"humidity":    60,
				"wind_speed":  "3-4级",
				"air_quality": "优",
			}

			return weatherData, nil
		}).
		Build()
	if err != nil {
		panic(fmt.Sprintf("创建天气工具失败: %v", err))
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
	fmt.Println("🎭 模拟模式示例")
	fmt.Println("----------------------------")
	fmt.Println("\n这个示例展示了 DeepSeek + AgentBuilder 的使用方法：")
	fmt.Println()
	fmt.Println("1️⃣  基础用法:")
	fmt.Println("   - 创建 DeepSeek LLM 客户端")
	fmt.Println("   - 使用 NewAgentBuilder 构建 Agent")
	fmt.Println("   - 设置系统提示词")
	fmt.Println("   - 运行 Agent 并获取结果")
	fmt.Println()
	fmt.Println("2️⃣  工具集成:")
	fmt.Println("   - 使用 WithTools 添加工具")
	fmt.Println("   - Agent 自动选择和调用工具")
	fmt.Println("   - 查看工具使用情况")
	fmt.Println()
	fmt.Println("3️⃣  中间件和回调:")
	fmt.Println("   - 添加成本追踪回调")
	fmt.Println("   - 启用日志和计时中间件")
	fmt.Println("   - 监控 Agent 执行")
	fmt.Println()
	fmt.Println("4️⃣  自定义配置:")
	fmt.Println("   - 配置最大迭代次数")
	fmt.Println("   - 设置超时时间")
	fmt.Println("   - 调整温度和 tokens")
	fmt.Println("   - 添加元数据")
	fmt.Println()
	fmt.Println("5️⃣  预设配置:")
	fmt.Println("   - ConfigureForChatbot() - 聊天机器人配置")
	fmt.Println("   - ConfigureForRAG() - RAG 系统配置")
	fmt.Println("   - ConfigureForAnalysis() - 数据分析配置")
	fmt.Println()
	fmt.Println("💡 完整代码示例:")
	fmt.Println()
	exampleCode := `
// 创建 DeepSeek 客户端
llmClient, err := providers.NewDeepSeekWithOptions(
    llm.WithAPIKey(apiKey),
    llm.WithModel("deepseek-chat"),
    llm.WithTemperature(0.7),
)

// 使用 AgentBuilder 构建 Agent（agentstate = "github.com/kart-io/goagent/core/state"）
agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
    WithSystemPrompt("你是一个友好的 AI 助手").
    WithTools(calculatorTool, weatherTool).
    WithState(agentstate.NewAgentState()).
    WithMaxIterations(10).
    WithTimeout(30 * time.Second).
    WithVerbose(true).
    Build()

// 运行 Agent
ctx := context.Background()
output, err := agent.Execute(ctx, "请帮我计算 15 * 8")
fmt.Println(output.Result)
`
	fmt.Println(exampleCode)
	fmt.Println()
	fmt.Println("📖 配置步骤:")
	fmt.Println("   1. 访问 https://platform.deepseek.com/ 获取 API Key")
	fmt.Println("   2. 设置环境变量: export DEEPSEEK_API_KEY=your-key")
	fmt.Println("   3. 运行此程序: go run main.go")
	fmt.Println()
	fmt.Println("📚 更多信息:")
	fmt.Println("   - DeepSeek 文档: https://platform.deepseek.com/docs")
	fmt.Println("   - GoAgent 文档: https://github.com/kart-io/goagent")
	fmt.Println("   - AgentBuilder 文档: https://github.com/kart-io/goagent/tree/master/builder")
}
