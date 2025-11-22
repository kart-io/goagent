package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	loggercore "github.com/kart-io/logger/core"
)

// 命令行参数
var (
	pattern  = flag.String("pattern", "all", "要运行的模式: aggregator, router, loop, all")
	provider = flag.String("provider", "deepseek", "LLM提供者: deepseek, openai")
)

func main() {
	flag.Parse()

	// 打印欢迎信息
	printWelcome()

	// 创建 Logger
	logger, err := createLogger()
	if err != nil {
		fmt.Printf("❌ 创建Logger失败: %v\n", err)
		os.Exit(1)
	}

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(*provider)
	if err != nil {
		fmt.Printf("❌ 创建LLM客户端失败: %v\n", err)
		printLLMSetupInstructions()
		os.Exit(1)
	}

	// 根据选择运行对应的模式
	switch *pattern {
	case "aggregator":
		if err := RunAggregatorPattern(llmClient, logger); err != nil {
			fmt.Printf("❌ 运行Aggregator模式失败: %v\n", err)
			os.Exit(1)
		}

	case "router":
		if err := RunRouterPattern(llmClient, logger); err != nil {
			fmt.Printf("❌ 运行Router模式失败: %v\n", err)
			os.Exit(1)
		}

	case "loop":
		if err := RunLoopPattern(llmClient, logger); err != nil {
			fmt.Printf("❌ 运行Loop模式失败: %v\n", err)
			os.Exit(1)
		}

	case "all":
		// 运行所有模式
		patterns := []struct {
			name string
			fn   func(llm.Client, loggercore.Logger) error
		}{
			{"Aggregator", RunAggregatorPattern},
			{"Router", RunRouterPattern},
			{"Loop", RunLoopPattern},
		}

		for i, p := range patterns {
			if i > 0 {
				fmt.Println("\n" + repeatString("=", 80))
			}

			if err := p.fn(llmClient, logger); err != nil {
				fmt.Printf("❌ 运行%s模式失败: %v\n", p.name, err)
				os.Exit(1)
			}
		}

	default:
		fmt.Printf("❌ 未知的模式: %s\n", *pattern)
		fmt.Println("\n可用模式:")
		fmt.Println("  aggregator - 聚合模式（社交媒体情感分析）")
		fmt.Println("  router     - 路由模式（客服工单分配）")
		fmt.Println("  loop       - 循环模式（代码迭代优化）")
		fmt.Println("  all        - 运行所有模式")
		os.Exit(1)
	}

	fmt.Println("\n" + repeatString("=", 80))
	fmt.Println("✨ 示例运行完成！")
	printSummary()
}

// printWelcome 打印欢迎信息
func printWelcome() {
	fmt.Println(repeatString("=", 80))
	fmt.Println("🤖 多智能体协作模式示例")
	fmt.Println("   基于文章: AI Agents & Multi-Agent Architectures (Part 7)")
	fmt.Println(repeatString("=", 80))
	fmt.Println()
	fmt.Println("本示例演示了以下多智能体协作模式:")
	fmt.Println()
	fmt.Println("1️⃣  Aggregator（聚合模式）")
	fmt.Println("   - 场景: 社交媒体情感分析")
	fmt.Println("   - 特点: 多个Agent并行处理，聚合者综合所有结果")
	fmt.Println()
	fmt.Println("2️⃣  Router（路由模式）")
	fmt.Println("   - 场景: 智能客服工单分配")
	fmt.Println("   - 特点: 中央路由器根据任务类型自动分配给专业Agent")
	fmt.Println()
	fmt.Println("3️⃣  Loop（循环模式）")
	fmt.Println("   - 场景: 代码编写与测试迭代")
	fmt.Println("   - 特点: Agent基于反馈迭代改进输出")
	fmt.Println()
	fmt.Println(repeatString("=", 80))
}

// printSummary 打印总结
func printSummary() {
	fmt.Println()
	fmt.Println("📚 关键要点:")
	fmt.Println()
	fmt.Println("✓ Aggregator模式: 适用于需要综合多个来源信息的场景")
	fmt.Println("✓ Router模式: 适用于任务分类和专家分配场景")
	fmt.Println("✓ Loop模式: 适用于需要迭代优化的场景")
	fmt.Println()
	fmt.Println("💡 更多信息:")
	fmt.Println("   - 项目文档: DOCUMENTATION_INDEX.md")
	fmt.Println("   - 架构文档: docs/architecture/ARCHITECTURE.md")
	fmt.Println("   - 其他示例: examples/advanced/")
	fmt.Println()
}

// printLLMSetupInstructions 打印LLM设置说明
func printLLMSetupInstructions() {
	fmt.Println()
	fmt.Println("📝 LLM API密钥设置说明:")
	fmt.Println()
	fmt.Println("DeepSeek:")
	fmt.Println("  export DEEPSEEK_API_KEY=\"your-api-key\"")
	fmt.Println()
	fmt.Println("OpenAI:")
	fmt.Println("  export OPENAI_API_KEY=\"your-api-key\"")
	fmt.Println()
	fmt.Println("使用示例:")
	fmt.Println("  go run . -pattern=aggregator -provider=deepseek")
	fmt.Println("  go run . -pattern=all -provider=openai")
	fmt.Println()
}

// createLogger 创建Logger
func createLogger() (loggercore.Logger, error) {
	return newSimpleLogger(), nil
}

// createLLMClient 创建LLM客户端
func createLLMClient(providerName string) (llm.Client, error) {
	switch providerName {
	case "deepseek":
		apiKey := os.Getenv("DEEPSEEK_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("DEEPSEEK_API_KEY environment variable not set")
		}
		return providers.NewDeepSeek(&llm.LLMOptions{
			APIKey: apiKey,
			Model:  "deepseek-chat",
		})

	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
		return providers.NewOpenAI(&llm.LLMOptions{
			APIKey: apiKey,
			Model:  "gpt-3.5-turbo",
		})

	default:
		return nil, fmt.Errorf("unknown provider: %s (supported: deepseek, openai)", providerName)
	}
}

// repeatString 重复字符串
func repeatString(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}
