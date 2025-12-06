// Package main 演示多智能体系统中使用 LLM 流式响应
//
// 本示例展示：
// 1. 多 Agent 使用 LLM 流式响应协作处理任务
// 2. Agent 间流式数据传递
// 3. 实时响应聚合与展示
// 4. 流式对话的多轮交互
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/multiagent"

	loggercore "github.com/kart-io/logger/core"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          多智能体 LLM 流式响应示例                              ║")
	fmt.Println("║   展示多 Agent 使用 LLM 流式响应协作处理任务                    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 创建日志和系统
	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	// 场景 1：多 Agent 流式响应协作
	fmt.Println("【场景 1】多 Agent 流式响应协作")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateStreamCollaboration(ctx, system)

	// 场景 2：流式响应聚合
	fmt.Println("\n【场景 2】流式响应聚合")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateStreamAggregation(ctx, system)

	// 场景 3：多轮流式对话
	fmt.Println("\n【场景 3】多轮流式对话")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateMultiTurnStream(ctx, system)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// 场景 1：多 Agent 流式响应协作
// ============================================================================

func demonstrateStreamCollaboration(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: 多个专家 Agent 并行使用 LLM 流式响应分析问题")
	fmt.Println()

	// 创建 LLM 客户端
	llmClient := createLLMClient()
	if llmClient == nil {
		fmt.Println("无法创建 LLM 客户端，使用模拟演示")
		demonstrateMockStreamCollaboration()
		return
	}

	// 检查流式能力
	streamClient := llm.AsStreamClient(llmClient)
	if streamClient == nil {
		fmt.Println("LLM 客户端不支持流式响应，使用模拟演示")
		demonstrateMockStreamCollaboration()
		return
	}

	fmt.Printf("✓ LLM 提供商: %s (支持流式)\n\n", llmClient.Provider())

	// 创建多个流式 Agent
	type agentInfo struct {
		id     string
		name   string
		role   string
		prompt string
		agent  *StreamAgent
	}

	agentList := []agentInfo{
		{
			id:     "tech-analyst",
			name:   "技术分析师",
			role:   "技术评估",
			prompt: "你是技术分析师。简短分析技术可行性（3句话内）。",
		},
		{
			id:     "market-analyst",
			name:   "市场分析师",
			role:   "市场评估",
			prompt: "你是市场分析师。简短分析市场前景（3句话内）。",
		},
		{
			id:     "risk-analyst",
			name:   "风险分析师",
			role:   "风险评估",
			prompt: "你是风险分析师。简短分析潜在风险（3句话内）。",
		},
	}

	// 注册 Agent
	for i := range agentList {
		agentList[i].agent = NewStreamAgent(agentList[i].id, agentList[i].name, multiagent.RoleSpecialist, system, streamClient, agentList[i].prompt)
		if err := system.RegisterAgent(agentList[i].id, agentList[i].agent); err != nil {
			fmt.Printf("  ✗ 注册失败: %s - %v\n", agentList[i].id, err)
		} else {
			fmt.Printf("  ✓ 注册: %s (%s)\n", agentList[i].name, agentList[i].role)
		}
	}

	// 并行执行流式分析
	topic := "在企业中部署大语言模型应用"
	fmt.Printf("\n分析主题: %s\n", topic)
	fmt.Println("────────────────────────────────────────")

	var wg sync.WaitGroup
	results := make(map[string]string)
	var mu sync.Mutex

	for _, a := range agentList {
		wg.Add(1)
		go func(info agentInfo) {
			defer wg.Done()

			if info.agent == nil {
				return
			}

			// 收集流式响应
			var content strings.Builder
			err := info.agent.StreamAnalyze(ctx, topic, func(chunk string) {
				content.WriteString(chunk)
			})

			mu.Lock()
			if err != nil {
				results[info.role] = fmt.Sprintf("分析失败: %v", err)
			} else {
				results[info.role] = content.String()
			}
			mu.Unlock()
		}(a)
	}

	wg.Wait()

	// 输出结果
	fmt.Println("\n分析结果:")
	for _, a := range agentList {
		fmt.Printf("\n【%s】\n", a.role)
		if result, ok := results[a.role]; ok {
			fmt.Printf("  %s\n", result)
		}
	}

	// 清理
	for _, a := range agentList {
		_ = system.UnregisterAgent(a.id)
	}
}

func demonstrateMockStreamCollaboration() {
	fmt.Println("\n模拟流式协作:")
	fmt.Println("────────────────────────────────────────")

	analyses := map[string]string{
		"技术评估": "大语言模型部署需要高性能GPU集群支持。容器化部署可提升可维护性。API网关设计需考虑高并发场景。",
		"市场评估": "企业AI应用市场快速增长。大模型能显著提升知识工作效率。早期采用者可获得竞争优势。",
		"风险评估": "数据隐私和合规是主要风险点。模型幻觉可能导致决策失误。运维成本需要持续投入。",
	}

	for role, content := range analyses {
		fmt.Printf("\n【%s】\n", role)
		// 模拟流式输出
		for _, char := range content {
			fmt.Print(string(char))
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Println()
	}
}

// ============================================================================
// 场景 2：流式响应聚合
// ============================================================================

func demonstrateStreamAggregation(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: 多个 Agent 流式输出实时聚合到协调者")
	fmt.Println()

	llmClient := createLLMClient()
	if llmClient == nil {
		demonstrateMockStreamAggregation()
		return
	}

	streamClient := llm.AsStreamClient(llmClient)
	if streamClient == nil {
		demonstrateMockStreamAggregation()
		return
	}

	// 创建协调者和工作者
	coordinator := NewAggregatorAgent("coordinator", "协调者", system, streamClient)
	worker1 := NewStreamAgent("worker-1", "工作者1", multiagent.RoleWorker, system, streamClient,
		"你是助手1，用一句话回答问题。")
	worker2 := NewStreamAgent("worker-2", "工作者2", multiagent.RoleWorker, system, streamClient,
		"你是助手2，用一句话从另一个角度回答问题。")

	_ = system.RegisterAgent("coordinator", coordinator)
	_ = system.RegisterAgent("worker-1", worker1)
	_ = system.RegisterAgent("worker-2", worker2)

	fmt.Println("Agent 架构:")
	fmt.Println("  coordinator (协调者)")
	fmt.Println("    ├── worker-1 (工作者1)")
	fmt.Println("    └── worker-2 (工作者2)")

	// 执行聚合查询
	question := "Go 语言的主要优势是什么？"
	fmt.Printf("\n问题: %s\n", question)
	fmt.Println("────────────────────────────────────────")

	// 并行收集工作者响应
	responses := make(chan struct {
		id      string
		content string
	}, 2)

	go func() {
		var content strings.Builder
		_ = worker1.StreamAnalyze(ctx, question, func(chunk string) {
			content.WriteString(chunk)
		})
		responses <- struct {
			id      string
			content string
		}{"worker-1", content.String()}
	}()

	go func() {
		var content strings.Builder
		_ = worker2.StreamAnalyze(ctx, question, func(chunk string) {
			content.WriteString(chunk)
		})
		responses <- struct {
			id      string
			content string
		}{"worker-2", content.String()}
	}()

	// 收集并聚合
	aggregated := make(map[string]string)
	for i := 0; i < 2; i++ {
		resp := <-responses
		aggregated[resp.id] = resp.content
		fmt.Printf("\n[%s 完成] %s\n", resp.id, resp.content)
	}

	// 协调者总结
	fmt.Println("\n[协调者总结]")
	summary := coordinator.Aggregate(ctx, aggregated)
	fmt.Printf("  %s\n", summary)

	// 清理
	_ = system.UnregisterAgent("coordinator")
	_ = system.UnregisterAgent("worker-1")
	_ = system.UnregisterAgent("worker-2")
}

func demonstrateMockStreamAggregation() {
	fmt.Println("\n模拟流式聚合:")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("\n[worker-1 完成] Go 语言语法简洁，学习曲线平缓。")
	fmt.Println("[worker-2 完成] Go 的并发模型使编写高性能服务变得简单。")
	fmt.Println("\n[协调者总结]")
	fmt.Println("  综合分析：Go 语言兼具易用性和高性能，特别适合构建后端服务。")
}

// ============================================================================
// 场景 3：多轮流式对话
// ============================================================================

func demonstrateMultiTurnStream(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: Agent 维护对话历史，进行多轮流式交互")
	fmt.Println()

	llmClient := createLLMClient()
	if llmClient == nil {
		demonstrateMockMultiTurn()
		return
	}

	streamClient := llm.AsStreamClient(llmClient)
	if streamClient == nil {
		demonstrateMockMultiTurn()
		return
	}

	// 创建对话 Agent
	agent := NewConversationAgent("chat-agent", "对话助手", system, streamClient)
	_ = system.RegisterAgent("chat-agent", agent)

	fmt.Println("✓ 创建对话 Agent (支持多轮对话)")

	// 多轮对话
	conversations := []string{
		"什么是微服务架构？",
		"它有什么优缺点？",
		"给我一个实际应用场景。",
	}

	for i, msg := range conversations {
		fmt.Printf("\n[轮次 %d] 用户: %s\n", i+1, msg)
		fmt.Println("────────────────────────────────────────")
		fmt.Print("助手: ")

		err := agent.Chat(ctx, msg, func(chunk string) {
			fmt.Print(chunk)
		})

		if err != nil {
			fmt.Printf("\n对话失败: %v\n", err)
		}
		fmt.Println()
	}

	// 显示对话统计
	fmt.Printf("\n对话统计: 共 %d 轮对话\n", len(conversations))

	_ = system.UnregisterAgent("chat-agent")
}

func demonstrateMockMultiTurn() {
	fmt.Println("\n模拟多轮对话:")

	conversations := []struct {
		user string
		bot  string
	}{
		{
			"什么是微服务架构？",
			"微服务架构是将应用程序拆分为小型独立服务的设计模式，每个服务运行独立进程并通过API通信。",
		},
		{
			"它有什么优缺点？",
			"优点：独立部署、技术栈灵活、易于扩展。缺点：分布式复杂性、运维成本高、数据一致性挑战。",
		},
		{
			"给我一个实际应用场景。",
			"电商平台是典型场景：用户服务、商品服务、订单服务、支付服务各自独立，可独立扩容应对流量高峰。",
		},
	}

	for i, conv := range conversations {
		fmt.Printf("\n[轮次 %d] 用户: %s\n", i+1, conv.user)
		fmt.Println("────────────────────────────────────────")
		fmt.Print("助手: ")
		for _, char := range conv.bot {
			fmt.Print(string(char))
			time.Sleep(15 * time.Millisecond)
		}
		fmt.Println()
	}
}

// ============================================================================
// Agent 实现
// ============================================================================

// StreamAgent 支持流式响应的 Agent
type StreamAgent struct {
	*multiagent.BaseCollaborativeAgent
	streamClient llm.StreamClient
	systemPrompt string
}

// NewStreamAgent 创建流式 Agent
func NewStreamAgent(
	id, description string,
	role multiagent.Role,
	system *multiagent.MultiAgentSystem,
	streamClient llm.StreamClient,
	systemPrompt string,
) *StreamAgent {
	return &StreamAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(id, description, role, system),
		streamClient:           streamClient,
		systemPrompt:           systemPrompt,
	}
}

// StreamAnalyze 流式分析
func (a *StreamAgent) StreamAnalyze(ctx context.Context, topic string, onChunk func(string)) error {
	messages := []llm.Message{
		llm.SystemMessage(a.systemPrompt),
		llm.UserMessage(topic),
	}

	stream, err := a.streamClient.ChatStream(ctx, messages)
	if err != nil {
		return err
	}

	for chunk := range stream {
		if chunk.Error != nil {
			return chunk.Error
		}
		onChunk(chunk.Content)
	}

	return nil
}

// Collaborate 实现协作接口
func (a *StreamAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	assignment := &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Status:    multiagent.TaskStatusExecuting,
		StartTime: time.Now(),
	}

	var content strings.Builder
	err := a.StreamAnalyze(ctx, fmt.Sprintf("%v", task.Input), func(chunk string) {
		content.WriteString(chunk)
	})

	if err != nil {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, err
	}

	assignment.Result = content.String()
	assignment.Status = multiagent.TaskStatusCompleted
	assignment.EndTime = time.Now()

	return assignment, nil
}

// AggregatorAgent 聚合器 Agent
type AggregatorAgent struct {
	*multiagent.BaseCollaborativeAgent
	streamClient llm.StreamClient
}

// NewAggregatorAgent 创建聚合器
func NewAggregatorAgent(
	id, description string,
	system *multiagent.MultiAgentSystem,
	streamClient llm.StreamClient,
) *AggregatorAgent {
	return &AggregatorAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(id, description, multiagent.RoleCoordinator, system),
		streamClient:           streamClient,
	}
}

// Aggregate 聚合多个响应
func (a *AggregatorAgent) Aggregate(ctx context.Context, responses map[string]string) string {
	var parts []string
	for id, content := range responses {
		parts = append(parts, fmt.Sprintf("%s: %s", id, content))
	}

	prompt := fmt.Sprintf("请综合以下观点给出总结（一句话）:\n%s", strings.Join(parts, "\n"))

	messages := []llm.Message{
		llm.SystemMessage("你是总结专家，擅长综合多方观点。"),
		llm.UserMessage(prompt),
	}

	stream, err := a.streamClient.ChatStream(ctx, messages)
	if err != nil {
		return "聚合失败: " + err.Error()
	}

	var result strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			return "聚合失败: " + chunk.Error.Error()
		}
		result.WriteString(chunk.Content)
	}

	return result.String()
}

// ConversationAgent 多轮对话 Agent
type ConversationAgent struct {
	*multiagent.BaseCollaborativeAgent
	streamClient llm.StreamClient
	history      []llm.Message
	mu           sync.Mutex
}

// NewConversationAgent 创建对话 Agent
func NewConversationAgent(
	id, description string,
	system *multiagent.MultiAgentSystem,
	streamClient llm.StreamClient,
) *ConversationAgent {
	return &ConversationAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(id, description, multiagent.RoleWorker, system),
		streamClient:           streamClient,
		history: []llm.Message{
			llm.SystemMessage("你是一个有帮助的助手，请简洁回答问题。"),
		},
	}
}

// Chat 进行对话
func (a *ConversationAgent) Chat(ctx context.Context, message string, onChunk func(string)) error {
	// 使用锁确保对话的原子性：用户消息和助手响应成对出现
	a.mu.Lock()
	defer a.mu.Unlock()

	// 添加用户消息
	a.history = append(a.history, llm.UserMessage(message))

	// 复制消息用于 API 调用
	messages := make([]llm.Message, len(a.history))
	copy(messages, a.history)

	// 调用 LLM（在锁内进行，确保对话原子性）
	stream, err := a.streamClient.ChatStream(ctx, messages)
	if err != nil {
		// 回滚：移除用户消息
		a.history = a.history[:len(a.history)-1]
		return err
	}

	var response strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			// 回滚：移除用户消息
			a.history = a.history[:len(a.history)-1]
			return chunk.Error
		}
		response.WriteString(chunk.Content)
		onChunk(chunk.Content)
	}

	// 保存助手回复到历史
	a.history = append(a.history, llm.AssistantMessage(response.String()))

	return nil
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
		)
		if err == nil {
			return client
		}
	}

	// 其次使用 Kimi (Moonshot)
	if apiKey := os.Getenv("KIMI_API_KEY"); apiKey != "" {
		client, err := providers.NewKimiWithOptions(
			llm.WithAPIKey(apiKey),
			llm.WithModel("moonshot-v1-8k"),
			llm.WithMaxTokens(500),
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
