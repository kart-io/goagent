// Package main 演示 LLM 包的高级用法
//
// 本示例展示：
// 1. LLM 客户端的创建和配置（多种 Provider）
// 2. 流式响应处理（Streaming）
// 3. 能力检查（Capability Checking）
// 4. 多 Provider 协作（Provider Fallback）
// 5. Token 统计和成本控制
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/multiagent"
	loggercore "github.com/kart-io/logger/core"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          LLM 高级用法示例                                       ║")
	fmt.Println("║   展示 LLM 包的高级功能：流式、能力检查、多 Provider 等          ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 创建日志和系统
	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	// 场景 1：多种 LLM 客户端配置
	fmt.Println("【场景 1】LLM 客户端配置")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateLLMConfiguration()

	// 场景 2：流式响应处理
	fmt.Println("\n【场景 2】流式响应处理")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateStreamingResponse(ctx)

	// 场景 3：能力检查
	fmt.Println("\n【场景 3】能力检查")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateCapabilityChecking()

	// 场景 4：多 Provider 协作
	fmt.Println("\n【场景 4】多 Provider 协作")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateMultiProviderCollaboration(ctx, system)

	// 场景 5：Token 统计和成本控制
	fmt.Println("\n【场景 5】Token 统计和成本控制")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateTokenTracking(ctx)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// 场景 1：LLM 客户端配置
// ============================================================================

func demonstrateLLMConfiguration() {
	fmt.Println("\n场景描述: 展示多种 LLM 客户端的配置方式")
	fmt.Println()

	// 1. 显式配置
	fmt.Println("1. 显式配置 (Explicit Configuration)")
	fmt.Println("────────────────────────────────────────")

	configs := []struct {
		name string
		opts []llm.ClientOption
	}{
		{"Development", []llm.ClientOption{
			llm.WithModel("gpt-3.5-turbo"),
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.5),
		}},
		{"Production", []llm.ClientOption{
			llm.WithModel("gpt-4"),
			llm.WithMaxTokens(2000),
			llm.WithTemperature(0.7),
			llm.WithCache(true, 5*time.Minute),
		}},
	}

	for _, c := range configs {
		fmt.Printf("  - %s: 显式设置模型和参数\n", c.name)
	}

	// 2. 针对场景优化
	fmt.Println("\n2. 针对场景优化 (Scenario Optimization)")
	fmt.Println("────────────────────────────────────────")

	scenarios := []struct {
		name string
		opts []llm.ClientOption
	}{
		{"Chat", []llm.ClientOption{
			llm.WithTemperature(0.7),
			llm.WithMaxTokens(1500),
			llm.WithTopP(0.9),
		}},
		{"CodeGeneration", []llm.ClientOption{
			llm.WithTemperature(0.2),
			llm.WithMaxTokens(2500),
			llm.WithTopP(0.95),
			llm.WithModel("gpt-4"),
		}},
	}

	for _, s := range scenarios {
		fmt.Printf("  - %s: 针对特定任务调整参数\n", s.name)
	}

	// 3. 使用 Provider 预设
	fmt.Println("\n3. 支持的 Provider")
	fmt.Println("────────────────────────────────────────")

	providerList := []struct {
		name   string
		envKey string
		hasKey bool
	}{
		{"DeepSeek", "DEEPSEEK_API_KEY", os.Getenv("DEEPSEEK_API_KEY") != ""},
		{"OpenAI", "OPENAI_API_KEY", os.Getenv("OPENAI_API_KEY") != ""},
		{"Anthropic", "ANTHROPIC_API_KEY", os.Getenv("ANTHROPIC_API_KEY") != ""},
		{"Gemini", "GEMINI_API_KEY", os.Getenv("GEMINI_API_KEY") != ""},
		{"Kimi", "KIMI_API_KEY", os.Getenv("KIMI_API_KEY") != ""},
		{"SiliconFlow", "SILICONFLOW_API_KEY", os.Getenv("SILICONFLOW_API_KEY") != ""},
	}

	for _, p := range providerList {
		status := "✗ 未配置"
		if p.hasKey {
			status = "✓ 已配置"
		}
		fmt.Printf("  - %-12s [%s] 环境变量: %s\n", p.name, status, p.envKey)
	}

	// 4. 自定义配置示例
	fmt.Println("\n4. 自定义配置选项")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  支持的配置选项:")
	fmt.Println("  - WithModel(model)          设置模型名称")
	fmt.Println("  - WithMaxTokens(n)          设置最大 Token 数")
	fmt.Println("  - WithTemperature(t)        设置温度参数 (0.0-2.0)")
	fmt.Println("  - WithTopP(p)               设置 Top-P 采样参数")
	fmt.Println("  - WithTimeout(d)            设置请求超时")
	fmt.Println("  - WithRetryCount(n)         设置重试次数")
	fmt.Println("  - WithRateLimiting(qps, b)  设置限流参数")
	fmt.Println("  - WithCache(c)              设置缓存实现")
}

// ============================================================================
// 场景 2：流式响应处理
// ============================================================================

func demonstrateStreamingResponse(ctx context.Context) {
	fmt.Println("\n场景描述: 展示如何处理 LLM 的流式响应")
	fmt.Println()

	client := createLLMClient()
	if client == nil {
		fmt.Println("✗ 无法创建 LLM 客户端")
		return
	}

	// 检查是否支持流式
	streamClient := llm.AsStreamClient(client)
	if streamClient == nil {
		fmt.Println("当前客户端不支持流式响应，使用模拟演示")
		demonstrateMockStreaming()
		return
	}

	fmt.Println("✓ 客户端支持流式响应")
	fmt.Println()

	// 发送流式请求
	prompt := "用三句话介绍 Go 语言的优势。"
	fmt.Printf("提示: %s\n", prompt)
	fmt.Println("────────────────────────────────────────")
	fmt.Print("响应: ")

	messages := []llm.Message{
		llm.SystemMessage("你是一个简洁的技术顾问，回答要精炼。"),
		llm.UserMessage(prompt),
	}

	stream, err := streamClient.ChatStream(ctx, messages)
	if err != nil {
		fmt.Printf("✗ 创建流失败: %v\n", err)
		return
	}

	var fullContent strings.Builder
	tokenCount := 0

	for chunk := range stream {
		if chunk.Error != nil {
			fmt.Printf("\n✗ 流式错误: %v\n", chunk.Error)
			break
		}
		fmt.Print(chunk.Content)
		fullContent.WriteString(chunk.Content)
		tokenCount++
	}

	fmt.Println()
	fmt.Println("────────────────────────────────────────")
	fmt.Printf("✓ 流式完成，共 %d 个 chunk\n", tokenCount)
}

func demonstrateMockStreaming() {
	fmt.Println()
	fmt.Print("模拟流式输出: ")

	mockContent := "Go 语言具有简洁的语法设计，学习曲线平缓。它拥有强大的并发支持，goroutine 让并发编程变得简单。Go 还具有出色的编译速度和运行效率，非常适合构建高性能服务。"

	for _, char := range mockContent {
		fmt.Print(string(char))
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Println()
	fmt.Println("────────────────────────────────────────")
	fmt.Println("✓ 模拟流式完成")
}

// ============================================================================
// 场景 3：能力检查
// ============================================================================

func demonstrateCapabilityChecking() {
	fmt.Println("\n场景描述: 展示如何检查 LLM 客户端的能力")
	fmt.Println()

	client := createLLMClient()
	if client == nil {
		fmt.Println("✗ 无法创建 LLM 客户端，使用模拟演示")
		demonstrateMockCapabilities()
		return
	}

	fmt.Printf("Provider: %s\n", client.Provider())
	fmt.Println("────────────────────────────────────────")

	// 检查各种能力
	capabilities := []struct {
		name    string
		checker func(llm.Client) bool
	}{
		{"基础补全 (Complete)", func(c llm.Client) bool { return c != nil }},
		{"聊天对话 (Chat)", func(c llm.Client) bool { return c != nil }},
		{"流式响应 (Stream)", func(c llm.Client) bool { return llm.AsStreamClient(c) != nil }},
		{"工具调用 (ToolCalling)", func(c llm.Client) bool { return llm.AsToolCaller(c) != nil }},
		{"文本嵌入 (Embedding)", func(c llm.Client) bool { return llm.AsEmbedder(c) != nil }},
	}

	for _, cap := range capabilities {
		status := "✗"
		if cap.checker(client) {
			status = "✓"
		}
		fmt.Printf("  [%s] %s\n", status, cap.name)
	}

	// 检查可用性
	fmt.Println()
	if client.IsAvailable() {
		fmt.Println("✓ 客户端当前可用")
	} else {
		fmt.Println("✗ 客户端当前不可用")
	}
}

func demonstrateMockCapabilities() {
	fmt.Println("Provider: Mock (模拟)")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  [✓] 基础补全 (Complete)")
	fmt.Println("  [✓] 聊天对话 (Chat)")
	fmt.Println("  [✓] 流式响应 (Stream)")
	fmt.Println("  [✓] 工具调用 (ToolCalling)")
	fmt.Println("  [✗] 文本嵌入 (Embedding)")
	fmt.Println()
	fmt.Println("✓ 客户端当前可用")
}

// ============================================================================
// 场景 4：多 Provider 协作
// ============================================================================

func demonstrateMultiProviderCollaboration(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: 多个 LLM Provider 协作完成任务，包含 Fallback 机制")
	fmt.Println()

	// 创建多个 LLM Agent，每个使用不同的 Provider（或模拟）
	agents := []struct {
		id       string
		provider string
		role     string
	}{
		{"primary-agent", "DeepSeek", "主要处理"},
		{"fallback-agent", "OpenAI", "备用处理"},
		{"validator-agent", "Mock", "结果验证"},
	}

	fmt.Println("多 Provider 架构:")
	fmt.Println("────────────────────────────────────────")
	for _, a := range agents {
		fmt.Printf("  - %s: %s (%s)\n", a.id, a.provider, a.role)

		// 创建模拟 Agent
		agent := multiagent.NewBaseCollaborativeAgent(
			a.id,
			fmt.Sprintf("%s - %s", a.provider, a.role),
			multiagent.RoleWorker,
			system,
		)
		_ = system.RegisterAgent(a.id, agent)
	}

	fmt.Println()
	fmt.Println("Fallback 策略:")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  1. 首先尝试 primary-agent (DeepSeek)")
	fmt.Println("  2. 如果失败，切换到 fallback-agent (OpenAI)")
	fmt.Println("  3. 最后由 validator-agent 验证结果")
	fmt.Println()

	// 执行任务
	task := &multiagent.CollaborativeTask{
		ID:          "multi-provider-task",
		Name:        "多 Provider 协作任务",
		Type:        multiagent.CollaborationTypeSequential,
		Input:       "分析 Go 语言在微服务架构中的应用",
		Assignments: make(map[string]multiagent.Assignment),
	}

	result, err := system.ExecuteTask(ctx, task)
	if err != nil {
		fmt.Printf("✗ 任务执行失败: %v\n", err)
	} else {
		fmt.Printf("✓ 任务完成，状态: %s\n", result.Status)
	}

	// 清理
	for _, a := range agents {
		_ = system.UnregisterAgent(a.id)
	}
}

// ============================================================================
// 场景 5：Token 统计和成本控制
// ============================================================================

func demonstrateTokenTracking(ctx context.Context) {
	fmt.Println("\n场景描述: 展示 Token 统计和成本控制")
	fmt.Println()

	client := createLLMClient()
	if client == nil {
		fmt.Println("使用模拟数据演示 Token 统计")
		demonstrateMockTokenTracking()
		return
	}

	// 执行一次调用并统计 Token
	prompt := "Hello, how are you?"
	response, err := client.Chat(ctx, []llm.Message{
		llm.UserMessage(prompt),
	})
	if err != nil {
		fmt.Printf("✗ 调用失败: %v\n", err)
		demonstrateMockTokenTracking()
		return
	}

	fmt.Println("Token 使用统计:")
	fmt.Println("────────────────────────────────────────")
	fmt.Printf("  模型: %s\n", response.Model)
	fmt.Printf("  提供商: %s\n", response.Provider)
	fmt.Printf("  总 Token 数: %d\n", response.TokensUsed)
	fmt.Printf("  完成原因: %s\n", response.FinishReason)

	// 成本估算（示例价格）
	estimateCost(response.Model, response.TokensUsed)
}

func demonstrateMockTokenTracking() {
	fmt.Println()
	fmt.Println("Token 使用统计 (模拟数据):")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  模型: deepseek-chat")
	fmt.Println("  提供商: DeepSeek")
	fmt.Println("  输入 Token: 25")
	fmt.Println("  输出 Token: 150")
	fmt.Println("  总 Token 数: 175")
	fmt.Println("  完成原因: stop")

	// 成本估算
	estimateCost("deepseek-chat", 175)
}

func estimateCost(model string, tokens int) {
	fmt.Println()
	fmt.Println("成本估算:")
	fmt.Println("────────────────────────────────────────")

	// 各模型的价格（每 1M Token，美元）
	prices := map[string]struct {
		input  float64
		output float64
	}{
		"deepseek-chat":   {0.14, 0.28},
		"gpt-4o-mini":     {0.15, 0.60},
		"gpt-4o":          {2.50, 10.00},
		"claude-3-sonnet": {3.00, 15.00},
	}

	price, ok := prices[model]
	if !ok {
		price = prices["deepseek-chat"] // 默认
	}

	// 简化计算（假设输入输出各占一半）
	inputTokens := tokens / 2
	outputTokens := tokens - inputTokens

	inputCost := float64(inputTokens) / 1000000 * price.input
	outputCost := float64(outputTokens) / 1000000 * price.output
	totalCost := inputCost + outputCost

	fmt.Printf("  输入成本: $%.6f (%d tokens × $%.2f/M)\n", inputCost, inputTokens, price.input)
	fmt.Printf("  输出成本: $%.6f (%d tokens × $%.2f/M)\n", outputCost, outputTokens, price.output)
	fmt.Printf("  总成本:   $%.6f\n", totalCost)

	// 成本控制建议
	fmt.Println()
	fmt.Println("成本控制建议:")
	fmt.Println("  - 使用 WithMaxTokens() 限制输出长度")
	fmt.Println("  - 使用 WithCache() 缓存重复请求")
	fmt.Println("  - 显式配置经济型模型参数 (如 gpt-3.5-turbo)")
	fmt.Println("  - 批量处理请求以提高效率")
}

// ============================================================================
// 辅助函数
// ============================================================================

func createLLMClient() llm.Client {
	// 优先使用 DeepSeek
	if apiKey := os.Getenv("DEEPSEEK_API_KEY"); apiKey != "" {
		client, err := providers.NewDeepSeekWithOptions(
			llm.WithAPIKey(apiKey),
			llm.WithModel("deepseek-chat"),
			llm.WithMaxTokens(500),
			llm.WithTemperature(0.7),
		)
		if err == nil {
			return client
		}
	}

	// 其次使用 OpenAI
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		client, err := providers.NewOpenAIWithOptions(
			llm.WithAPIKey(apiKey),
			llm.WithModel("gpt-4o-mini"),
			llm.WithMaxTokens(500),
			llm.WithTemperature(0.7),
		)
		if err == nil {
			return client
		}
	}

	return nil
}

// ============================================================================
// 日志实现
// ============================================================================

type simpleLogger struct{}

func (l *simpleLogger) Debug(args ...interface{})                       {}
func (l *simpleLogger) Info(args ...interface{})                        {}
func (l *simpleLogger) Warn(args ...interface{})                        {}
func (l *simpleLogger) Error(args ...interface{})                       {}
func (l *simpleLogger) Fatal(args ...interface{})                       {}
func (l *simpleLogger) Debugf(template string, args ...interface{})     {}
func (l *simpleLogger) Infof(template string, args ...interface{})      {}
func (l *simpleLogger) Warnf(template string, args ...interface{})      {}
func (l *simpleLogger) Errorf(template string, args ...interface{})     {}
func (l *simpleLogger) Fatalf(template string, args ...interface{})     {}
func (l *simpleLogger) Debugw(msg string, keysAndValues ...interface{}) {}
func (l *simpleLogger) Infow(msg string, keysAndValues ...interface{})  {}
func (l *simpleLogger) Warnw(msg string, keysAndValues ...interface{})  {}
func (l *simpleLogger) Errorw(msg string, keysAndValues ...interface{}) {}
func (l *simpleLogger) Fatalw(msg string, keysAndValues ...interface{}) {}
func (l *simpleLogger) With(keyValues ...interface{}) loggercore.Logger { return l }
func (l *simpleLogger) WithCtx(_ context.Context, keyValues ...interface{}) loggercore.Logger {
	return l
}
func (l *simpleLogger) WithCallerSkip(skip int) loggercore.Logger { return l }
func (l *simpleLogger) SetLevel(level loggercore.Level)           {}
func (l *simpleLogger) Sync() error                               { return nil }
func (l *simpleLogger) Flush() error                              { return nil }

var _ loggercore.Logger = (*simpleLogger)(nil)
