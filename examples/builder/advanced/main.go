// Advanced API 示例
// 展示 Builder API 的 Advanced 层级（30+ 个方法，覆盖 100% 使用场景）
//
// 本示例演示：
// 1. 带自定义状态的 Agent（泛型）
// 2. 带中间件的 Agent（缓存、限流、日志）
// 3. 带对话记忆的会话管理 Agent（多轮对话 + SessionID）
// 4. 带元数据和遥测的 Agent（企业级监控）
// 5. 输出格式控制（结合企业级配置）
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
	storeMemory "github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("=== Builder API - Advanced 层级示例 ===")

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

	// 示例 1: 带自定义状态的 Agent
	example1AgentWithCustomState(apiKey, providerName)

	// 示例 2: 带中间件的 Agent
	example2AgentWithMiddleware(apiKey, providerName)

	// 示例 3: 带会话管理的 Agent
	example3AgentWithSessionManagement(apiKey, providerName)

	// 示例 4: 完整的企业级配置
	example4EnterpriseAgent(apiKey, providerName)

	// 示例 5: 输出格式控制（结合企业级配置）
	example5OutputFormatEnterprise(apiKey, providerName)

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

// CustomState 自定义状态类型
// 在标准 AgentState 基础上扩展业务字段
type CustomState struct {
	*core.AgentState
	UserProfile     map[string]interface{} // 用户画像
	BusinessContext map[string]string      // 业务上下文
	RequestCount    int                    // 请求计数
}

// 示例 1: 带自定义状态的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Advanced API: WithState (泛型)
func example1AgentWithCustomState(apiKey, providerName string) {
	fmt.Println("--- 示例 1: 带自定义状态的 Agent ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 创建自定义状态实例
	customState := &CustomState{
		AgentState: core.NewAgentState(),
		UserProfile: map[string]interface{}{
			"user_id":   "user-123",
			"user_name": "张三",
			"vip_level": 3,
		},
		BusinessContext: map[string]string{
			"tenant_id": "tenant-001",
			"region":    "cn-beijing",
		},
		RequestCount: 0,
	}

	// 创建工具
	calculator := createCalculatorTool()

	// 使用泛型 AgentBuilder 配置自定义状态
	agent, err := builder.NewAgentBuilder[any, *CustomState](llmClient).
		// Simple API
		WithSystemPrompt("你是一个企业级助手").
		WithTools(calculator).

		// Advanced API - 自定义状态
		WithState(customState).
		Build()
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 执行 Agent
	result, err := agent.Execute(context.Background(), "帮我处理 VIP 用户请求：计算 100 + 200")
	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("结果: %v\n", result.Result)
		fmt.Printf("用户信息: %v\n", customState.UserProfile)
		fmt.Printf("请求次数: %d\n", customState.RequestCount+1)
	}

	fmt.Println()
}

// 示例 2: 带中间件的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Advanced API: WithMiddleware (概念演示)
func example2AgentWithMiddleware(apiKey, providerName string) {
	fmt.Println("--- 示例 2: 带中间件的 Agent ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 创建工具
	calculator := createCalculatorTool()

	// 注意：中间件通常在工具级别应用，而非 Agent 级别
	// 这里演示基本的 Agent 配置，实际的中间件应用需要包装工具
	fmt.Println("中间件功能包括: 缓存、限流、日志记录等")
	fmt.Println("中间件通常在工具级别应用，可提供横切关注点支持")

	// 配置 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个高性能助手").
		WithTools(calculator).
		Build()
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 使用 Agent 执行任务
	result, err := agent.Execute(context.Background(), "计算 10 + 20")

	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("结果: %v\n", result.Result)
	}

	fmt.Println()
}

// 示例 3: 带对话记忆的会话管理 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Core API: WithMemory, WithMaxConversationHistory
// - Advanced API: WithSessionID, WithAutoSaveEnabled, WithSaveInterval
//
// 本示例演示如何结合对话记忆和会话管理实现有状态的多轮对话。
// Agent 会记住之前的对话内容，同时支持会话持久化和恢复。
func example3AgentWithSessionManagement(apiKey, providerName string) {
	fmt.Println("--- 示例 3: 带对话记忆的会话管理 Agent ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 创建对话记忆管理器（用于多轮对话）
	memMgr := memory.NewInMemoryManager(memory.DefaultConfig())

	// 创建工具
	calculator := createCalculatorTool()

	// 生成唯一会话 ID
	sessionID := fmt.Sprintf("session-%d", time.Now().Unix())

	// 配置带对话记忆和会话管理的 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个有记忆的助手，请记住用户告诉你的所有信息。").
		WithTools(calculator).

		// Core API - 对话记忆
		WithMemory(memMgr).             // 设置对话记忆管理器
		WithMaxConversationHistory(20). // 最多保留 20 轮历史对话

		// Advanced API - 会话管理
		WithSessionID(sessionID).           // 设置会话 ID
		WithAutoSaveEnabled(true).          // 启用自动保存
		WithSaveInterval(30 * time.Second). // 每 30 秒保存一次状态

		Build()
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 模拟多次会话交互
	fmt.Printf("会话 ID: %s\n", sessionID)
	ctx := context.Background()

	// 第一次交互：告诉 Agent 用户信息
	fmt.Println("\n第一次交互:")
	fmt.Println("用户: 我的用户 ID 是 12345，我是一名软件工程师")
	result1, _ := agent.Execute(ctx, "我的用户 ID 是 12345，我是一名软件工程师")
	fmt.Printf("Agent: %v\n", result1.Result)

	// 第二次交互：测试 Agent 是否记住了用户信息
	fmt.Println("\n第二次交互:")
	fmt.Println("用户: 你还记得我的用户 ID 吗？我的职业是什么？")
	result2, _ := agent.Execute(ctx, "你还记得我的用户 ID 吗？我的职业是什么？")
	fmt.Printf("Agent: %v\n", result2.Result)

	// 第三次交互：进行计算任务
	fmt.Println("\n第三次交互:")
	fmt.Println("用户: 帮我计算 100 + 200")
	result3, _ := agent.Execute(ctx, "帮我计算 100 + 200")
	fmt.Printf("Agent: %v\n", result3.Result)

	// 查看保存的对话历史
	fmt.Println("\n=== 对话历史记录 ===")
	history, _ := memMgr.GetConversationHistory(ctx, sessionID, 0)
	for i, conv := range history {
		role := "用户"
		if conv.Role == "assistant" {
			role = "助手"
		}
		content := conv.Content
		if len(content) > 60 {
			content = content[:60] + "..."
		}
		fmt.Printf("%d. [%s] %s\n", i+1, role, content)
	}

	fmt.Println()
}

// 示例 4: 完整的企业级配置
//
// 综合使用 Simple, Core, Advanced 所有层级的方法
func example4EnterpriseAgent(apiKey, providerName string) {
	fmt.Println("--- 示例 4: 完整的企业级配置 ---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	// 创建自定义状态
	customState := &CustomState{
		AgentState: core.NewAgentState(),
		UserProfile: map[string]interface{}{
			"org_id":    "org-001",
			"user_role": "admin",
		},
		BusinessContext: map[string]string{
			"environment": "production",
			"data_center": "us-west-2",
		},
	}

	// 创建存储（用于状态持久化，不是对话记忆）
	memoryStore := storeMemory.New()

	// 创建工具
	calculator := createCalculatorTool()

	// 创建回调
	metricsCallback := &enterpriseCallbackImpl{
		BaseCallback: core.NewBaseCallback(),
	}

	// 完整的企业级配置
	agent, err := builder.NewAgentBuilder[any, *CustomState](llmClient).
		// ========== Simple API ==========
		WithSystemPrompt("你是一个企业级智能助手，为组织提供专业服务").
		WithTools(calculator).
		WithMaxIterations(30). // 允许更多推理步骤
		WithTemperature(0.5).  // 平衡创造性和精确性

		// ========== Core API ==========
		WithTimeout(10*time.Minute).    // 更长的超时时间
		WithMaxTokens(5000).            // 更多 token 预算
		WithCallbacks(metricsCallback). // 监控指标
		WithStore(memoryStore).         // 持久化存储
		WithVerbose(false).             // 生产环境关闭详细日志

		// ========== Advanced API ==========
		WithState(customState).                    // 自定义状态
		WithSessionID("enterprise-session-001").   // 会话管理
		WithAutoSaveEnabled(true).                 // 自动保存
		WithSaveInterval(1*time.Minute).           // 每分钟保存
		WithMetadata("tenant_id", "tenant-001").   // 租户信息
		WithMetadata("region", "us-west-2").       // 区域信息
		WithMetadata("environment", "production"). // 环境标识

		Build()
	if err != nil {
		log.Fatalf("创建企业级 Agent 失败: %v", err)
	}

	// 执行企业级任务
	ctx := context.Background()
	fmt.Println("执行企业级任务...")

	result, err := agent.Execute(ctx, "分析销售数据：计算 1000 乘以 1.2")
	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("任务结果: %v\n", result.Result)
	}

	// 打印企业级指标
	fmt.Println("\n企业级指标:")
	fmt.Printf("- 总请求数: %d\n", metricsCallback.requestCount)
	fmt.Printf("- 平均延迟: %v\n", metricsCallback.avgLatency)
	fmt.Printf("- 错误率: %.2f%%\n", metricsCallback.errorRate*100)
	fmt.Printf("- 组织 ID: %v\n", customState.UserProfile["org_id"])

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

// enterpriseCallbackImpl 企业级回调实现
type enterpriseCallbackImpl struct {
	*core.BaseCallback
	requestCount int
	avgLatency   time.Duration
	errorRate    float64
	startTime    time.Time
}

func (e *enterpriseCallbackImpl) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	e.requestCount++
	e.startTime = time.Now()
	return nil
}

func (e *enterpriseCallbackImpl) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	latency := time.Since(e.startTime)
	e.avgLatency = (e.avgLatency + latency) / 2
	return nil
}

func (e *enterpriseCallbackImpl) OnError(ctx context.Context, err error) error {
	if e.requestCount > 0 {
		e.errorRate = float64(1) / float64(e.requestCount)
	}
	return nil
}

// 示例 5: 输出格式控制（结合企业级配置）
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithOutputFormat, WithCustomOutputFormat
// - Core API: WithTimeout, WithMaxTokens, WithCallbacks
// - Advanced API: WithState (泛型), WithMetadata, WithSessionID
//
// 本示例展示如何将输出格式控制与 Advanced 层级的企业级功能结合使用
func example5OutputFormatEnterprise(apiKey, providerName string) {
	fmt.Println("--- 示例 5: 输出格式控制（结合企业级配置）---")

	// 创建 LLM 客户端
	llmClient, err := createLLMClient(apiKey, providerName)
	if err != nil {
		log.Fatalf("创建 %s 客户端失败: %v", providerName, err)
	}

	ctx := context.Background()

	// 5.1 JSON 格式 + 自定义状态（适合数据处理管道）
	fmt.Println("\n5.1 JSON 格式 + 自定义状态:")

	// 创建自定义状态用于跟踪处理信息
	customState := &CustomState{
		AgentState: core.NewAgentState(),
		UserProfile: map[string]interface{}{
			"data_type": "order",
			"version":   "v1",
		},
		BusinessContext: map[string]string{
			"pipeline": "data-extraction",
		},
	}

	agent1, err := builder.NewAgentBuilder[any, *CustomState](llmClient).
		WithSystemPrompt("你是一个数据提取助手，专门从文本中提取结构化数据").
		WithOutputFormat(builder.OutputFormatJSON).
		// Advanced API
		WithState(customState).
		WithMetadata("output_schema", "order").
		WithSessionID("data-pipeline-001").
		Build()
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
	} else {
		result, _ := agent1.Execute(ctx, "从以下文本提取订单信息：客户张三在2024年1月15日下单购买了iPhone 15，数量2台，总价15999元")
		fmt.Printf("输出: %v\n", result.Result)
		fmt.Printf("状态元数据: %v\n", customState.BusinessContext)
	}

	// 5.2 自定义格式 + 监控回调（适合生产环境）
	fmt.Println("\n5.2 自定义格式 + 监控回调:")

	// 创建监控回调
	monitorCallback := &enterpriseCallbackImpl{
		BaseCallback: core.NewBaseCallback(),
	}

	agent2, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个报告生成助手").
		WithCustomOutputFormat("请按以下企业报告格式回复：\n=== 报告标题 ===\n【摘要】\n【详细分析】\n【结论与建议】\n【风险提示】").
		// Core API
		WithCallbacks(monitorCallback).
		WithTimeout(2*time.Minute).
		WithMaxTokens(3000).
		// Advanced API
		WithMetadata("report_type", "analysis").
		WithMetadata("department", "engineering").
		Build()
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
	} else {
		result, _ := agent2.Execute(ctx, "分析 Go 语言在微服务架构中的应用现状")
		fmt.Printf("输出:\n%v\n", result.Result)
		fmt.Printf("监控指标 - 请求数: %d, 平均延迟: %v\n", monitorCallback.requestCount, monitorCallback.avgLatency)
	}

	// 5.3 多格式支持 + 完整企业配置
	fmt.Println("\n5.3 多格式支持演示（Markdown 格式 + 企业配置）:")

	// 创建存储
	memoryStore := storeMemory.New()

	agent3, err := builder.NewSimpleBuilder(llmClient).
		WithSystemPrompt("你是一个技术文档助手").
		WithOutputFormat(builder.OutputFormatMarkdown).
		// Core API
		WithTimeout(90*time.Second).
		WithMaxTokens(2000).
		WithStore(memoryStore).
		// Advanced API
		WithSessionID("doc-gen-001").
		WithAutoSaveEnabled(true).
		WithMetadata("doc_type", "technical").
		WithMetadata("format", "markdown").
		Build()
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
	} else {
		result, _ := agent3.Execute(ctx, "写一个简短的 Go HTTP 服务器示例，包含路由和错误处理")
		fmt.Printf("输出:\n%v\n", result.Result)
	}

	fmt.Println()
}
