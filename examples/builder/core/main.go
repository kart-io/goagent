// Core API 示例
// 展示 Builder API 的 Core 层级（15-20 个方法，覆盖 95% 使用场景）
//
// 本示例演示：
// 1. 带监控和日志的 Agent（Callbacks）
// 2. 带超时和性能控制的 Agent
// 3. 带存储和持久化的 Agent
// 4. 带推理模式的 Agent（Chain-of-Thought）
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
)

func main() {
	fmt.Println("=== Builder API - Core 层级示例 ===\n")

	// 示例 1: 带监控和日志的 Agent
	example1AgentWithMonitoring()

	// 示例 2: 带超时和性能控制的 Agent
	example2AgentWithPerformanceControl()

	// 示例 3: 带存储和持久化的 Agent
	example3AgentWithPersistence()

	// 示例 4: 带错误处理的 Agent
	example4AgentWithErrorHandling()
}

// 示例 1: 带监控和日志的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools, Build
// - Core API: WithCallbacks, WithVerbose
func example1AgentWithMonitoring() {
	fmt.Println("--- 示例 1: 带监控和日志的 Agent ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我会详细记录执行过程。")

	// 创建工具
	calculator := tools.NewCalculatorTool()

	// 创建回调函数用于监控
	stdoutCallback := core.NewStdoutCallback(true) // 打印详细日志

	// 配置带监控的 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个数学助手").
		WithTools(calculator).
		WithMaxIterations(10).

		// Core API - 监控和调试
		WithCallbacks(stdoutCallback).   // 添加回调函数
		WithVerbose(true).                // 启用详细日志

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
		fmt.Printf("\n最终结果: %s\n", result)
	}

	fmt.Println()
}

// 示例 2: 带超时和性能控制的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Core API: WithTimeout, WithMaxTokens
func example2AgentWithPerformanceControl() {
	fmt.Println("--- 示例 2: 带超时和性能控制的 Agent ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我会在时间和 token 限制内工作。")

	// 创建工具
	calculator := tools.NewCalculatorTool()

	// 配置带性能控制的 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个高效的助手").
		WithTools(calculator).

		// Core API - 性能控制
		WithTimeout(3 * time.Minute).    // 3 分钟超时
		WithMaxTokens(3000).              // 最多 3000 tokens

		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 执行 Agent
	ctx := context.Background()
	result, err := agent.Execute(ctx, "快速计算 100 个数字的总和")
	if err != nil {
		log.Printf("执行失败（可能超时）: %v", err)
	} else {
		fmt.Printf("结果: %s\n", result)
	}

	fmt.Println()
}

// 示例 3: 带存储和持久化的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Core API: WithStore, WithCallbacks
func example3AgentWithPersistence() {
	fmt.Println("--- 示例 3: 带存储和持久化的 Agent ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我会记住之前的对话。")

	// 创建内存存储（生产环境建议使用 Redis 等持久化存储）
	memoryStore := store.NewMemoryStore()

	// 创建工具
	calculator := tools.NewCalculatorTool()

	// 创建简单的回调用于观察状态保存
	metricsCallback := &metricsCallbackImpl{}

	// 配置带存储的 Agent
	agent, err := builder.NewSimpleBuilder(llmClient).
		// Simple API
		WithSystemPrompt("你是一个有记忆的助手").
		WithTools(calculator).

		// Core API - 存储和持久化
		WithStore(memoryStore).           // 添加存储
		WithCallbacks(metricsCallback).   // 监控指标

		Build()

	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 第一次对话
	fmt.Println("第一次对话：")
	result1, _ := agent.Execute(context.Background(), "我的名字是小明")
	fmt.Printf("Agent: %s\n", result1)

	// 第二次对话（测试记忆）
	fmt.Println("\n第二次对话（测试记忆）：")
	result2, _ := agent.Execute(context.Background(), "你还记得我的名字吗？")
	fmt.Printf("Agent: %s\n", result2)

	fmt.Printf("\n存储的键数量: %d\n", metricsCallback.storeAccessCount)
	fmt.Println()
}

// 示例 4: 带错误处理的 Agent
//
// 使用的方法：
// - Simple API: WithSystemPrompt, WithTools
// - Core API: WithErrorHandler
func example4AgentWithErrorHandling() {
	fmt.Println("--- 示例 4: 带错误处理的 Agent ---")

	// 创建模拟 LLM 客户端
	llmClient := mockllm.NewMockLLMClient("我可能会遇到错误。")

	// 创建工具
	calculator := tools.NewCalculatorTool()

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
	result, err := agent.Execute(context.Background(), "执行一个复杂任务")
	if err != nil {
		fmt.Printf("最终错误: %v\n", err)
	} else {
		fmt.Printf("结果: %s\n", result)
	}

	fmt.Println()
}

// metricsCallbackImpl 实现指标回调
type metricsCallbackImpl struct {
	storeAccessCount int
}

func (m *metricsCallbackImpl) OnLLMStart(ctx context.Context, prompt string) error {
	return nil
}

func (m *metricsCallbackImpl) OnLLMEnd(ctx context.Context, response string) error {
	return nil
}

func (m *metricsCallbackImpl) OnToolStart(ctx context.Context, toolName string, args map[string]interface{}) error {
	return nil
}

func (m *metricsCallbackImpl) OnToolEnd(ctx context.Context, toolName string, result interface{}) error {
	m.storeAccessCount++
	return nil
}

func (m *metricsCallbackImpl) OnError(ctx context.Context, err error) error {
	return nil
}

func (m *metricsCallbackImpl) OnAgentAction(ctx context.Context, action string) error {
	return nil
}

func (m *metricsCallbackImpl) OnAgentFinish(ctx context.Context, result string) error {
	return nil
}
