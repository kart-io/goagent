// Core API 示例
// 展示 Builder API 的 Core 层级（15-20 个方法，覆盖 95% 使用场景）
//
// 本示例演示：
// 1. 带监控和日志的 Agent（Callbacks）
// 2. 带超时和性能控制的 Agent
// 3. 带对话记忆的 Agent（多轮对话）
// 4. 带错误处理的 Agent
// 5. 输出格式控制（结合性能配置）
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("=== Builder API - Core 层级示例 ===")

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

	// 优先使用 DeepSeek
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

	// 示例 1: 带监控和日志的 Agent
	example1AgentWithMonitoring(apiKey, providerName)

	// 示例 2: 带超时和性能控制的 Agent
	example2AgentWithPerformanceControl(apiKey, providerName)

	// 示例 3: 带对话记忆的 Agent（多轮对话）
	example3AgentWithMemory(apiKey, providerName)

	// 示例 4: 带错误处理的 Agent
	example4AgentWithErrorHandling(apiKey, providerName)

	// 示例 5: 输出格式控制
	example5OutputFormatWithPerformance(apiKey, providerName)

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

// 示例 1: 带监控和日志的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools, Build
// - Core API: WithCallbacks, WithVerbose
func example1AgentWithMonitoring(apiKey, providerName string) {
	fmt.Println("--- 示例 1: 带监控和日志的 Agent ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 创建工具
	calculator := createCalculatorTool()

	// 创建回调函数用于监控
	stdoutCallback := core.NewStdoutCallback(true) // 打印详细日志

	// 配置带监控的 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个数学助手").
		WithTools(calculator).
		WithMaxIterations(10).

		// Core API - 监控和调试
		WithCallbacks(stdoutCallback). // 添加回调函数
		WithVerbose(true).             // 启用详细日志

		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 执行 Agent（会打印详细的执行日志）
	fmt.Println("\n开始执行 Agent（观察详细日志）：")
	result, err := agent.Execute(context.Background(), "计算 (10 + 20) * 3")
	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("\n最终结果: %v\n", result.Result)
	}

	fmt.Println()
}

// 示例 2: 带超时和性能控制的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Core API: WithTimeout, WithMaxTokens
func example2AgentWithPerformanceControl(apiKey, providerName string) {
	fmt.Println("--- 示例 2: 带超时和性能控制的 Agent ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 创建工具
	calculator := createCalculatorTool()

	// 配置带性能控制的 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个高效的助手").
		WithTools(calculator).

		// Core API - 性能控制
		WithTimeout(3 * time.Minute). // 3 分钟超时
		WithMaxTokens(3000).          // 最多 3000 tokens

		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 执行 Agent
	ctx := context.Background()
	result, err := agent.Execute(ctx, "快速计算 100 + 200 + 300")
	if err != nil {
		log.Printf("执行失败（可能超时）: %v", err)
	} else {
		fmt.Printf("结果: %v\n", result.Result)
	}

	fmt.Println()
}

// 示例 3: 带对话记忆的 Agent（多轮对话）
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools, Build
// - Core API: WithMemory, WithSessionID, WithMaxConversationHistory
//
// 本示例演示如何使用 MemoryManager 实现多轮对话。
// Agent 会记住之前的对话内容，在后续对话中可以引用之前的信息。
func example3AgentWithMemory(apiKey, providerName string) {
	fmt.Println("--- 示例 3: 带对话记忆的 Agent（多轮对话）---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 创建对话记忆管理器
	// 使用内存存储，生产环境可以使用 Redis 等持久化存储
	memMgr := memory.NewInMemoryManager(memory.DefaultConfig())

	// 创建工具
	calculator := createCalculatorTool()

	// 定义会话 ID（同一用户的多轮对话应使用相同的 SessionID）
	sessionID := "user-session-001"

	// 配置带对话记忆的 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个友好的助手，可以进行数学计算。请记住用户之前告诉你的信息。").
		WithTools(calculator).

		// Core API - 对话记忆
		WithMemory(memMgr).             // 设置对话记忆管理器
		WithSessionID(sessionID).       // 设置会话 ID
		WithMaxConversationHistory(10). // 最多保留 10 轮历史对话

		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	ctx := context.Background()

	// 第一轮对话：告诉 Agent 一些信息
	fmt.Println("\n=== 第一轮对话 ===")
	fmt.Println("用户: 我的名字叫小明，我今年 25 岁")
	result1, err := agent.Execute(ctx, "我的名字叫小明，我今年 25 岁")
	if err != nil {
		log.Printf("第一轮对话失败: %v", err)
	} else {
		fmt.Printf("Agent: %v\n", result1.Result)
	}

	// 第二轮对话：测试 Agent 是否记住了用户信息
	fmt.Println("\n=== 第二轮对话 ===")
	fmt.Println("用户: 你还记得我叫什么名字吗？我多大了？")
	result2, err := agent.Execute(ctx, "你还记得我叫什么名字吗？我多大了？")
	if err != nil {
		log.Printf("第二轮对话失败: %v", err)
	} else {
		fmt.Printf("Agent: %v\n", result2.Result)
	}

	// 第三轮对话：进行计算并关联之前的信息
	fmt.Println("\n=== 第三轮对话 ===")
	fmt.Println("用户: 请帮我计算 5 年后我多大")
	result3, err := agent.Execute(ctx, "请帮我计算 5 年后我多大")
	if err != nil {
		log.Printf("第三轮对话失败: %v", err)
	} else {
		fmt.Printf("Agent: %v\n", result3.Result)
	}

	// 查看保存的对话历史
	fmt.Println("\n=== 对话历史记录 ===")
	history, _ := memMgr.GetConversationHistory(ctx, sessionID, 0)
	for i, conv := range history {
		role := "用户"
		if conv.Role == "assistant" {
			role = "助手"
		}
		// 只显示前 50 个字符
		content := conv.Content
		if len(content) > 50 {
			content = content[:50] + "..."
		}
		fmt.Printf("%d. [%s] %s\n", i+1, role, content)
	}

	fmt.Println()
}

// 示例 4: 带错误处理的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Core API: WithErrorHandler
func example4AgentWithErrorHandling(apiKey, providerName string) {
	fmt.Println("--- 示例 4: 带错误处理的 Agent ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 创建工具
	calculator := createCalculatorTool()

	// 自定义错误处理函数
	errorHandler := func(err error) error {
		// 在这里可以实现：
		// - 错误重试逻辑
		// - 降级策略
		// - 错误告警
		// - 错误日志记录

		fmt.Printf("⚠️  捕获到错误: %v\n", err)
		fmt.Println("✅ 应用降级策略...")

		// 返回处理后的错误（或 nil 表示已恢复）
		return err
	}

	// 配置带错误处理的 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个可靠的助手").
		WithTools(calculator).

		// Core API - 错误处理
		WithErrorHandler(errorHandler).
		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 执行 Agent（模拟可能出错的场景）
	result, err := agent.Execute(context.Background(), "执行一个计算任务：计算 50 乘以 2")
	if err != nil {
		fmt.Printf("最终错误: %v\n", err)
	} else {
		fmt.Printf("结果: %v\n", result.Result)
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
				case "*", "乘", "×":
					result = num1 * num2
				case "/", "除", "÷":
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

// 示例 5: 输出格式控制（结合性能配置）
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithOutputFormat, WithCustomOutputFormat
// - Core API: WithTimeout, WithMaxTokens, WithVerbose
//
// 本示例展示如何将输出格式控制与 Core 层级的性能配置结合使用
func example5OutputFormatWithPerformance(apiKey, providerName string) {
	fmt.Println("--- 示例 5: 输出格式控制（结合性能配置）---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	ctx := context.Background()

	// 5.1 JSON 格式 + 性能优化（适合 API 响应）
	fmt.Println("\n5.1 JSON 格式 + 性能优化:")
	agent1, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个 API 数据助手").
		WithOutputFormat(builder.OutputFormatJSON).
		// Core API - 性能配置
		WithTimeout(30 * time.Second).
		WithMaxTokens(1000). // 限制 token 节省成本
		Build()
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
	} else {
		result, _ := agent1.Execute(ctx, "返回一个包含 id, name, status 字段的订单信息")
		fmt.Printf("输出: %v\n", result.Result)
	}

	// 5.2 自定义格式 + 详细日志（适合调试）
	fmt.Println("\n5.2 自定义格式 + 详细日志:")
	agent2, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个代码审查助手").
		WithCustomOutputFormat("请按以下格式回复：\n[问题] 发现的问题\n[建议] 改进建议\n[示例] 代码示例").
		// Core API - 调试配置
		WithVerbose(true). // 启用详细日志
		WithTimeout(60 * time.Second).
		Build()
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
	} else {
		result, _ := agent2.Execute(ctx, "审查这段代码：for i := 0; i < len(arr); i++ { fmt.Println(arr[i]) }")
		fmt.Printf("输出:\n%v\n", result.Result)
	}

	// 5.3 纯文本格式 + 对话记忆（适合聊天场景）
	fmt.Println("\n5.3 纯文本格式 + 对话记忆:")
	memMgr := memory.NewInMemoryManager(memory.DefaultConfig())
	sessionID := fmt.Sprintf("format-demo-%d", time.Now().Unix())

	agent3, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个友好的聊天助手").
		WithOutputFormat(builder.OutputFormatPlainText).
		// Core API - 对话记忆
		WithMemory(memMgr).
		WithSessionID(sessionID).
		WithMaxConversationHistory(10).
		Build()
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
	} else {
		// 第一轮对话
		result1, _ := agent3.Execute(ctx, "我叫小明，今年 25 岁")
		fmt.Printf("第一轮: %v\n", result1.Result)

		// 第二轮对话（测试记忆）
		result2, _ := agent3.Execute(ctx, "你还记得我的名字和年龄吗？")
		fmt.Printf("第二轮: %v\n", result2.Result)
	}

	fmt.Println()
}
