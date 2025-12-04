// Advanced API 示例
// 展示 Builder API 的 Advanced 层级（30+ 个方法，覆盖 100% 使用场景）
//
// 本示例演示：
// 1. 带自定义状态的 Agent（泛型）
// 2. 带中间件的 Agent（缓存、限流、日志）
// 3. 带会话管理的 Agent（SessionID、自动保存）
// 4. 带元数据和遥测的 Agent（企业级监控）
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm/providers/mockllm"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/middleware"
)

func main() {
	fmt.Println("=== Builder API - Advanced 层级示例 ===\n")

	// 示例 1: 带自定义状态的 Agent
	example1AgentWithCustomState()

	// 示例 2: 带中间件的 Agent
	example2AgentWithMiddleware()

	// 示例 3: 带会话管理的 Agent
	example3AgentWithSessionManagement()

	// 示例 4: 完整的企业级配置
	example4EnterpriseAgent()
}

// CustomState 自定义状态类型
// 在标准 AgentState 基础上扩展业务字段
type CustomState struct {
	*core.AgentState
	UserProfile    map[string]interface{} // 用户画像
	BusinessContext map[string]string     // 业务上下文
	RequestCount   int                    // 请求计数
}

// 示例 1: 带自定义状态的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Advanced API: WithState (泛型)
func example1AgentWithCustomState() {
	fmt.Println("--- 示例 1: 带自定义状态的 Agent ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我会使用自定义状态。")

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
	calculator := tools.NewCalculatorTool()

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
	result, err := agent.Execute(context.Background(), "帮我处理 VIP 用户请求")
	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("结果: %s\n", result)
		fmt.Printf("用户信息: %v\n", customState.UserProfile)
		fmt.Printf("请求次数: %d\n", customState.RequestCount+1)
	}

	fmt.Println()
}

// 示例 2: 带中间件的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Advanced API: WithMiddleware
func example2AgentWithMiddleware() {
	fmt.Println("--- 示例 2: 带中间件的 Agent ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我会通过中间件处理。")

	// 创建工具
	calculator := tools.NewCalculatorTool()

	// 创建中间件实例
	cachingMW := middleware.Caching(
		middleware.WithCache(cache.NewSimpleCache(5*time.Minute)),
		middleware.WithTTL(5*time.Minute),
	)

	rateLimitMW := middleware.RateLimit(
		middleware.WithQPS(10),  // 每秒 10 个请求
		middleware.WithBurst(20), // 突发 20 个请求
	)

	loggingMW := middleware.Logging(
		middleware.WithLogger(nil), // 使用默认日志
	)

	// 配置带中间件的 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个高性能助手").
		WithTools(calculator).

		// Advanced API - 中间件
		// 注意: middleware.Apply 需要在工具上应用，这里展示概念
		// 实际使用时需要对工具应用中间件

		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 应用中间件到工具
	cachedCalc := cachingMW.Apply(calculator)
	rateLimitedCalc := rateLimitMW.Apply(cachedCalc)
	loggedCalc := loggingMW.Apply(rateLimitedCalc)

	// 使用带中间件的工具
	fmt.Println("中间件链: Logging → RateLimit → Caching → Tool")
	result, err := loggedCalc.Execute(context.Background(), map[string]interface{}{
		"operation": "add",
		"a":         10,
		"b":         20,
	})

	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("结果: %v\n", result)
	}

	fmt.Println()
}

// 示例 3: 带会话管理的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Core API: WithStore
// - Advanced API: WithSessionID, WithAutoSaveEnabled, WithSaveInterval
func example3AgentWithSessionManagement() {
	fmt.Println("--- 示例 3: 带会话管理的 Agent ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我会管理会话状态。")

	// 创建存储（用于会话持久化）
	memoryStore := store.NewMemoryStore()

	// 创建工具
	calculator := tools.NewCalculatorTool()

	// 生成唯一会话 ID
	sessionID := fmt.Sprintf("session-%d", time.Now().Unix())

	// 配置带会话管理的 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个有状态的助手").
		WithTools(calculator).

		// Core API
		WithStore(memoryStore).

		// Advanced API - 会话管理
		WithSessionID(sessionID).                // 设置会话 ID
		WithAutoSaveEnabled(true).               // 启用自动保存
		WithSaveInterval(30 * time.Second).      // 每 30 秒保存一次

		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 模拟多次会话交互
	fmt.Printf("会话 ID: %s\n", sessionID)

	// 第一次交互
	fmt.Println("\n第一次交互:")
	result1, _ := agent.Execute(context.Background(), "我的用户 ID 是 12345")
	fmt.Printf("Agent: %s\n", result1)

	// 第二次交互
	fmt.Println("\n第二次交互:")
	result2, _ := agent.Execute(context.Background(), "记住我的 ID 了吗？")
	fmt.Printf("Agent: %s\n", result2)

	// 模拟会话恢复（30 秒后）
	fmt.Println("\n模拟 30 秒后的会话恢复...")
	time.Sleep(100 * time.Millisecond) // 实际应该等待 30 秒

	fmt.Println()
}

// 示例 4: 完整的企业级配置
//
// 综合使用 Simple, Core, Advanced 所有层级的方法
func example4EnterpriseAgent() {
	fmt.Println("--- 示例 4: 完整的企业级配置 ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我是一个企业级 Agent。")

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

	// 创建存储
	memoryStore := store.NewMemoryStore()

	// 创建工具
	calculator := tools.NewCalculatorTool()

	// 创建回调
	metricsCallback := &enterpriseCallbackImpl{}

	// 完整的企业级配置
	agent, err := builder.NewAgentBuilder[any, *CustomState](llmClient).
		// ========== Simple API ==========
		WithSystemPrompt("你是一个企业级智能助手，为组织提供专业服务").
		WithTools(calculator).
		WithMaxIterations(30).    // 允许更多推理步骤
		WithTemperature(0.5).     // 平衡创造性和精确性

		// ========== Core API ==========
		WithTimeout(10 * time.Minute).       // 更长的超时时间
		WithMaxTokens(5000).                 // 更多 token 预算
		WithCallbacks(metricsCallback).      // 监控指标
		WithStore(memoryStore).              // 持久化存储
		WithVerbose(false).                  // 生产环境关闭详细日志

		// ========== Advanced API ==========
		WithState(customState).                        // 自定义状态
		WithSessionID("enterprise-session-001").       // 会话管理
		WithAutoSaveEnabled(true).                     // 自动保存
		WithSaveInterval(1 * time.Minute).             // 每分钟保存
		WithMetadata("tenant_id", "tenant-001").       // 租户信息
		WithMetadata("region", "us-west-2").           // 区域信息
		WithMetadata("environment", "production").     // 环境标识

		Build()

	if err != nil {
		log.Fatalf("创建企业级 Agent 失败: %v", err)
	}

	// 执行企业级任务
	ctx := context.Background()
	fmt.Println("执行企业级任务...")

	result, err := agent.Execute(ctx, "分析本月销售数据并生成报告")
	if err != nil {
		log.Printf("执行失败: %v", err)
	} else {
		fmt.Printf("任务结果: %s\n", result)
	}

	// 打印企业级指标
	fmt.Println("\n企业级指标:")
	fmt.Printf("- 总请求数: %d\n", metricsCallback.requestCount)
	fmt.Printf("- 平均延迟: %v\n", metricsCallback.avgLatency)
	fmt.Printf("- 错误率: %.2f%%\n", metricsCallback.errorRate*100)
	fmt.Printf("- 组织 ID: %v\n", customState.UserProfile["org_id"])

	fmt.Println()
}

// enterpriseCallbackImpl 企业级回调实现
type enterpriseCallbackImpl struct {
	requestCount int
	avgLatency   time.Duration
	errorRate    float64
	startTime    time.Time
}

func (e *enterpriseCallbackImpl) OnLLMStart(ctx context.Context, prompt string) error {
	e.requestCount++
	e.startTime = time.Now()
	return nil
}

func (e *enterpriseCallbackImpl) OnLLMEnd(ctx context.Context, response string) error {
	latency := time.Since(e.startTime)
	e.avgLatency = (e.avgLatency + latency) / 2
	return nil
}

func (e *enterpriseCallbackImpl) OnToolStart(ctx context.Context, toolName string, args map[string]interface{}) error {
	return nil
}

func (e *enterpriseCallbackImpl) OnToolEnd(ctx context.Context, toolName string, result interface{}) error {
	return nil
}

func (e *enterpriseCallbackImpl) OnError(ctx context.Context, err error) error {
	e.errorRate = float64(1) / float64(e.requestCount)
	return nil
}

func (e *enterpriseCallbackImpl) OnAgentAction(ctx context.Context, action string) error {
	return nil
}

func (e *enterpriseCallbackImpl) OnAgentFinish(ctx context.Context, result string) error {
	return nil
}
