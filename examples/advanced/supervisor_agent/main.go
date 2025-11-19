package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/agents"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/examples/testhelpers"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

var (
	scenario = flag.String("scenario", "basic", "Scenario to run: basic, travel, review, all")
	provider = flag.String("provider", "deepseek", "LLM provider: deepseek, openai")
)

func main() {
	flag.Parse()

	fmt.Println("=== SupervisorAgent 功能示例 ===\n")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(*provider)
	if err != nil {
		fmt.Printf("❌ 创建 LLM 客户端失败: %v\n", err)
		fmt.Println("\n提示：请设置环境变量：")
		fmt.Println("  export DEEPSEEK_API_KEY=\"your-api-key\"")
		fmt.Println("  或")
		fmt.Println("  export OPENAI_API_KEY=\"your-api-key\"")
		os.Exit(1)
	}

	// 根据场景运行示例
	switch *scenario {
	case "basic":
		runBasicExample(llmClient)
	case "travel":
		runTravelPlannerExample(llmClient)
	case "review":
		runCodeReviewExample(llmClient)
	case "all":
		runBasicExample(llmClient)
		fmt.Println("\n" + strings.Repeat("=", 80) + "\n")
		runTravelPlannerExample(llmClient)
		fmt.Println("\n" + strings.Repeat("=", 80) + "\n")
		runCodeReviewExample(llmClient)
	default:
		fmt.Printf("❌ 未知场景: %s\n", *scenario)
		fmt.Println("可用场景: basic, travel, review, all")
		os.Exit(1)
	}
}

// createLLMClient 创建 LLM 客户端
func createLLMClient(providerName string) (llm.Client, error) {
	switch providerName {
	case "deepseek":
		apiKey := os.Getenv("DEEPSEEK_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("DEEPSEEK_API_KEY environment variable not set")
		}
		return providers.NewDeepSeek(&llm.Config{
			APIKey: apiKey,
			Model:  "deepseek-chat",
		})

	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
		return providers.NewOpenAI(&llm.Config{
			APIKey: apiKey,
			Model:  "gpt-3.5-turbo",
		})

	default:
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}
}

// runBasicExample 基础示例：展示基本的多 Agent 协作
func runBasicExample(llmClient llm.Client) {
	fmt.Println("📋 场景 1: 基础示例 - 多 Agent 协作")
	fmt.Println(strings.Repeat("-", 80))

	// 创建子 Agent
	searchAgent := createSimpleAgent(llmClient, "search", "负责搜索信息")
	weatherAgent := createSimpleAgent(llmClient, "weather", "负责查询天气")
	summaryAgent := createSimpleAgent(llmClient, "summary", "负责生成总结")

	// 创建 SupervisorAgent
	config := agents.DefaultSupervisorConfig()
	config.AggregationStrategy = agents.StrategyHierarchy

	supervisor := agents.NewSupervisorAgent(llmClient, config)
	supervisor.AddSubAgent("search", searchAgent)
	supervisor.AddSubAgent("weather", weatherAgent)
	supervisor.AddSubAgent("summary", summaryAgent)

	// 执行任务
	task := "研究法国的首都，查询当地天气，并生成一份简短的旅行建议"
	fmt.Printf("\n🎯 任务: %s\n\n", task)

	startTime := time.Now()

	result, err := supervisor.Invoke(context.Background(), &core.AgentInput{
		Task: task,
	})

	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ 执行失败: %v\n", err)
		return
	}

	// 输出结果
	fmt.Println("✅ 执行成功！\n")
	fmt.Println("📊 最终结果:")
	fmt.Println(strings.Repeat("-", 80))
	printResult(result.Result)
	fmt.Println(strings.Repeat("-", 80))

	// 输出统计信息
	fmt.Printf("\n⏱️  执行时间: %v\n", duration)
	if result.TokenUsage != nil {
		fmt.Printf("🎫 Token 使用: %d (提示: %d, 完成: %d)\n",
			result.TokenUsage.TotalTokens,
			result.TokenUsage.PromptTokens,
			result.TokenUsage.CompletionTokens,
		)
	}
}

// runTravelPlannerExample 旅行规划示例：展示层次聚合策略
func runTravelPlannerExample(llmClient llm.Client) {
	fmt.Println("🗺️  场景 2: 旅行规划助手 - 层次聚合策略")
	fmt.Println(strings.Repeat("-", 80))

	// 创建专业的旅行规划 Agent
	cityInfoAgent := createCityInfoAgent(llmClient)
	weatherInfoAgent := createWeatherInfoAgent(llmClient)
	attractionAgent := createAttractionAgent(llmClient)
	itineraryAgent := createItineraryAgent(llmClient)

	// 创建 SupervisorAgent（使用层次聚合）
	config := agents.DefaultSupervisorConfig()
	config.AggregationStrategy = agents.StrategyHierarchy
	config.SubAgentTimeout = 60 * time.Second // 增加超时时间以处理复杂任务

	supervisor := agents.NewSupervisorAgent(llmClient, config)
	supervisor.AddSubAgent("city_info", cityInfoAgent)
	supervisor.AddSubAgent("weather", weatherInfoAgent)
	supervisor.AddSubAgent("attractions", attractionAgent)
	supervisor.AddSubAgent("itinerary", itineraryAgent)

	// 执行任务
	task := "我计划去东京旅行3天，帮我了解城市信息、天气情况、推荐景点，并生成一份3天的行程安排"
	fmt.Printf("\n🎯 任务: %s\n\n", task)

	startTime := time.Now()

	result, err := supervisor.Invoke(context.Background(), &core.AgentInput{
		Task: task,
	})

	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ 执行失败: %v\n", err)
		return
	}

	// 输出结果
	fmt.Println("✅ 执行成功！\n")
	fmt.Println("📊 旅行规划:")
	fmt.Println(strings.Repeat("-", 80))
	printResult(result.Result)
	fmt.Println(strings.Repeat("-", 80))

	// 输出统计信息
	fmt.Printf("\n⏱️  执行时间: %v\n", duration)
	if result.TokenUsage != nil {
		fmt.Printf("🎫 Token 使用: %d\n", result.TokenUsage.TotalTokens)
	}
}

// runCodeReviewExample 代码审查示例：展示协商聚合策略
func runCodeReviewExample(llmClient llm.Client) {
	fmt.Println("🔍 场景 3: 代码审查 - 协商聚合策略")
	fmt.Println(strings.Repeat("-", 80))

	// 创建专业审查 Agent
	securityAgent := createSecurityReviewAgent(llmClient)
	performanceAgent := createPerformanceReviewAgent(llmClient)
	readabilityAgent := createReadabilityReviewAgent(llmClient)

	// 创建 SupervisorAgent（使用合并聚合来测试）
	config := agents.DefaultSupervisorConfig()
	config.AggregationStrategy = agents.StrategyMerge // 改为合并，可以看到每个 Agent 的独立结果
	config.SubAgentTimeout = 60 * time.Second         // 增加超时时间以处理复杂的代码分析任务

	supervisor := agents.NewSupervisorAgent(llmClient, config)
	supervisor.AddSubAgent("security", securityAgent)
	supervisor.AddSubAgent("performance", performanceAgent)
	supervisor.AddSubAgent("readability", readabilityAgent)

	// 待审查的代码
	codeToReview := `
func ProcessUserData(data string) error {
    // 直接使用用户输入构建 SQL
    query := "SELECT * FROM users WHERE name = '" + data + "'"

    // 执行查询
    for i := 0; i < 1000000; i++ {
        result := db.Query(query)
        // 处理结果...
    }

    return nil
}
`

	task := fmt.Sprintf(`请仔细审查以下 Go 代码的安全性、性能和可读性。

**待审查代码：**
%s

**要求：**
请从安全、性能、可读性三个维度进行专业分析，给出评分和改进建议。`, codeToReview)

	fmt.Printf("\n🎯 任务: 代码审查\n")
	fmt.Printf("\n📝 待审查代码:\n%s\n", codeToReview)

	startTime := time.Now()

	result, err := supervisor.Invoke(context.Background(), &core.AgentInput{
		Task: task,
	})

	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ 执行失败: %v\n", err)
		return
	}

	// 输出结果
	fmt.Println("✅ 审查完成！\n")
	fmt.Println("📊 审查结果:")
	fmt.Println(strings.Repeat("-", 80))
	printResult(result.Result)
	fmt.Println(strings.Repeat("-", 80))

	// 输出统计信息
	fmt.Printf("\n⏱️  执行时间: %v\n", duration)
	if result.TokenUsage != nil {
		fmt.Printf("🎫 Token 使用: %d\n", result.TokenUsage.TotalTokens)
	}
}

// ========== Agent 创建辅助函数 ==========

// createSimpleAgent 创建简单的 Agent
func createSimpleAgent(llmClient llm.Client, name, description string) core.Agent {
	agent := testhelpers.NewMockAgent(name)
	agent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		// 使用 LLM 处理任务
		response, err := llmClient.Complete(ctx, &llm.CompletionRequest{
			Messages: []llm.Message{
				{Role: "system", Content: fmt.Sprintf("你是一个%s", description)},
				{Role: "user", Content: input.Task},
			},
		})

		if err != nil {
			return nil, err
		}

		return &core.AgentOutput{
			Result:     response.Content,
			Status:     "success",
			TokenUsage: response.Usage,
		}, nil
	})

	return agent
}

// createCityInfoAgent 创建城市信息 Agent
func createCityInfoAgent(llmClient llm.Client) core.Agent {
	return createSimpleAgent(llmClient, "city_info", "城市信息专家，负责提供城市的基本信息、文化特色等")
}

// createWeatherInfoAgent 创建天气信息 Agent
func createWeatherInfoAgent(llmClient llm.Client) core.Agent {
	return createSimpleAgent(llmClient, "weather", "天气预报专家，负责提供天气信息和穿衣建议")
}

// createAttractionAgent 创建景点推荐 Agent
func createAttractionAgent(llmClient llm.Client) core.Agent {
	return createSimpleAgent(llmClient, "attractions", "旅游景点专家，负责推荐热门景点和美食")
}

// createItineraryAgent 创建行程安排 Agent
func createItineraryAgent(llmClient llm.Client) core.Agent {
	return createSimpleAgent(llmClient, "itinerary", "行程规划专家，负责根据信息生成详细的旅行行程")
}

// createSecurityReviewAgent 创建安全审查 Agent
func createSecurityReviewAgent(llmClient llm.Client) core.Agent {
	agent := testhelpers.NewMockAgent("security")
	agent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		prompt := fmt.Sprintf(`你是一个代码安全审查专家。

%s

请从**安全角度**审查上述代码，重点关注：
1. SQL 注入漏洞
2. XSS 攻击风险
3. 数据验证缺失
4. 敏感信息泄露

**请按以下格式输出：**
- 安全评分：X/10分
- 发现的安全问题（列出具体问题）
- 改进建议（给出具体的修复方案）`, input.Task)

		response, err := llmClient.Complete(ctx, &llm.CompletionRequest{
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
		})

		if err != nil {
			return nil, err
		}

		return &core.AgentOutput{
			Result:     response.Content,
			Status:     "success",
			TokenUsage: response.Usage,
		}, nil
	})

	return agent
}

// createPerformanceReviewAgent 创建性能审查 Agent
func createPerformanceReviewAgent(llmClient llm.Client) core.Agent {
	agent := testhelpers.NewMockAgent("performance")
	agent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		prompt := fmt.Sprintf(`你是一个代码性能审查专家。

%s

请从**性能角度**审查上述代码，重点关注：
1. 算法复杂度（时间复杂度和空间复杂度）
2. 内存使用（是否有内存泄漏）
3. 数据库查询优化（查询次数、索引使用）
4. 循环效率（是否有不必要的重复计算）

**请按以下格式输出：**
- 性能评分：X/10分
- 发现的性能问题（列出具体问题）
- 改进建议（给出具体的优化方案）`, input.Task)

		response, err := llmClient.Complete(ctx, &llm.CompletionRequest{
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
		})

		if err != nil {
			return nil, err
		}

		return &core.AgentOutput{
			Result:     response.Content,
			Status:     "success",
			TokenUsage: response.Usage,
		}, nil
	})

	return agent
}

// createReadabilityReviewAgent 创建可读性审查 Agent
func createReadabilityReviewAgent(llmClient llm.Client) core.Agent {
	agent := testhelpers.NewMockAgent("readability")
	agent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		prompt := fmt.Sprintf(`你是一个代码可读性审查专家。

%s

请从**可读性角度**审查上述代码，重点关注：
1. 命名规范（变量名、函数名是否清晰）
2. 代码结构（是否易于理解）
3. 注释质量（是否有必要的注释）
4. 代码风格（是否符合 Go 语言规范）

**请按以下格式输出：**
- 可读性评分：X/10分
- 发现的可读性问题（列出具体问题）
- 改进建议（给出具体的改进方案）`, input.Task)

		response, err := llmClient.Complete(ctx, &llm.CompletionRequest{
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
		})

		if err != nil {
			return nil, err
		}

		return &core.AgentOutput{
			Result:     response.Content,
			Status:     "success",
			TokenUsage: response.Usage,
		}, nil
	})

	return agent
}

// ========== 辅助函数 ==========

// printResult 格式化输出结果
func printResult(result interface{}) {
	switch v := result.(type) {
	case string:
		fmt.Println(v)
	case map[string]interface{}:
		for key, value := range v {
			fmt.Printf("\n【%s】\n", key)
			if subMap, ok := value.(map[string]interface{}); ok {
				for subKey, subValue := range subMap {
					fmt.Printf("  %s: %v\n", subKey, subValue)
				}
			} else {
				fmt.Printf("  %v\n", value)
			}
		}
	default:
		fmt.Printf("%+v\n", result)
	}
}
