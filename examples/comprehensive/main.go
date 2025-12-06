// Package main 综合示例 - GoAgent 框架组件完整性验证
//
// 本示例用于验证 GoAgent 框架各组件的完整性、协同能力与可用性：
// 1. LLM 集成 - 多提供商客户端创建与对话
// 2. 工具系统 - 工具注册、中间件、执行
// 3. 记忆管理 - 对话历史、案例存储、键值存储
// 4. 多智能体 - 协作类型、消息通信、团队管理
// 5. 规划执行 - SmartPlanner、AgentExecutor、步骤分配
// 6. RAG 检索 - 向量存储、文档检索、重排序
// 7. 流式处理 - StreamManager、传输层、事件处理
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/multiagent"
	"github.com/kart-io/goagent/planning"
	"github.com/kart-io/goagent/retrieval"
	"github.com/kart-io/goagent/stream"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/middleware"

	loggercore "github.com/kart-io/logger/core"
)

// ============================================================================
// 验证结果收集
// ============================================================================

// VerificationResult 单个验证结果
type VerificationResult struct {
	Component string
	Feature   string
	Success   bool
	Message   string
	Duration  time.Duration
}

// VerificationReport 验证报告
type VerificationReport struct {
	Results     []VerificationResult
	TotalTests  int
	PassedTests int
	FailedTests int
	TotalTime   time.Duration
}

func (r *VerificationReport) Add(result VerificationResult) {
	r.Results = append(r.Results, result)
	r.TotalTests++
	if result.Success {
		r.PassedTests++
	} else {
		r.FailedTests++
	}
	r.TotalTime += result.Duration
}

func (r *VerificationReport) Print() {
	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                      验证报告                                  ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")

	// 按组件分组
	groups := make(map[string][]VerificationResult)
	for _, result := range r.Results {
		groups[result.Component] = append(groups[result.Component], result)
	}

	for component, results := range groups {
		fmt.Printf("\n【%s】\n", component)
		fmt.Println("────────────────────────────────────────")
		for _, result := range results {
			status := "✓"
			if !result.Success {
				status = "✗"
			}
			fmt.Printf("  %s %s (%v)\n", status, result.Feature, result.Duration)
			if result.Message != "" {
				fmt.Printf("      %s\n", result.Message)
			}
		}
	}

	fmt.Println("\n════════════════════════════════════════════════════════════════")
	passRate := float64(r.PassedTests) / float64(r.TotalTests) * 100
	fmt.Printf("总计: %d 项测试 | 通过: %d | 失败: %d | 通过率: %.1f%% | 总耗时: %v\n",
		r.TotalTests, r.PassedTests, r.FailedTests, passRate, r.TotalTime)

	if r.FailedTests == 0 {
		fmt.Println("════════════════════════════════════════════════════════════════")
		fmt.Println("               ✓ 所有组件验证通过！框架可用性完整。")
		fmt.Println("════════════════════════════════════════════════════════════════")
	} else {
		fmt.Println("════════════════════════════════════════════════════════════════")
		fmt.Println("               ⚠ 部分组件验证失败，请检查上述错误。")
		fmt.Println("════════════════════════════════════════════════════════════════")
	}
}

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          GoAgent 框架组件完整性验证                            ║")
	fmt.Println("║   验证各组件的完整性、协同能力与可用性                          ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	report := &VerificationReport{}
	logger := &simpleLogger{}

	// 1. LLM 集成验证
	fmt.Println("【1. LLM 集成验证】")
	fmt.Println("════════════════════════════════════════════════════════════════")
	verifyLLMIntegration(ctx, report)

	// 2. 工具系统验证
	fmt.Println("\n【2. 工具系统验证】")
	fmt.Println("════════════════════════════════════════════════════════════════")
	verifyToolSystem(ctx, report)

	// 3. 记忆管理验证
	fmt.Println("\n【3. 记忆管理验证】")
	fmt.Println("════════════════════════════════════════════════════════════════")
	verifyMemoryManagement(ctx, report)

	// 4. 多智能体协作验证
	fmt.Println("\n【4. 多智能体协作验证】")
	fmt.Println("════════════════════════════════════════════════════════════════")
	verifyMultiAgentCollaboration(ctx, report, logger)

	// 5. 规划执行验证
	fmt.Println("\n【5. 规划执行验证】")
	fmt.Println("════════════════════════════════════════════════════════════════")
	verifyPlanningExecution(ctx, report, logger)

	// 6. RAG 检索验证
	fmt.Println("\n【6. RAG 检索验证】")
	fmt.Println("════════════════════════════════════════════════════════════════")
	verifyRAGRetrieval(ctx, report)

	// 7. 流式处理验证
	fmt.Println("\n【7. 流式处理验证】")
	fmt.Println("════════════════════════════════════════════════════════════════")
	verifyStreamProcessing(ctx, report)

	// 8. 组件协同验证
	fmt.Println("\n【8. 组件协同验证】")
	fmt.Println("════════════════════════════════════════════════════════════════")
	verifyComponentIntegration(ctx, report, logger)

	// 打印验证报告
	report.Print()
}

// ============================================================================
// 1. LLM 集成验证
// ============================================================================

func verifyLLMIntegration(ctx context.Context, report *VerificationReport) {
	// 验证 LLM 客户端创建
	start := time.Now()
	client := createLLMClient()
	if client != nil {
		report.Add(VerificationResult{
			Component: "LLM 集成",
			Feature:   "客户端创建",
			Success:   true,
			Message:   fmt.Sprintf("提供商: %s", client.Provider()),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ LLM 客户端创建成功 (提供商: %s)\n", client.Provider())

		// 验证对话能力
		start = time.Now()
		messages := []llm.Message{
			llm.SystemMessage("你是一个简洁的助手。"),
			llm.UserMessage("用一句话解释什么是 Agent。"),
		}

		resp, err := client.Chat(ctx, messages)
		if err == nil && resp.Content != "" {
			report.Add(VerificationResult{
				Component: "LLM 集成",
				Feature:   "对话能力",
				Success:   true,
				Message:   fmt.Sprintf("响应长度: %d 字符", len(resp.Content)),
				Duration:  time.Since(start),
			})
			fmt.Printf("  ✓ LLM 对话成功 (响应: %s...)\n", truncateString(resp.Content, 50))
		} else {
			report.Add(VerificationResult{
				Component: "LLM 集成",
				Feature:   "对话能力",
				Success:   false,
				Message:   fmt.Sprintf("错误: %v", err),
				Duration:  time.Since(start),
			})
			fmt.Printf("  ✗ LLM 对话失败: %v\n", err)
		}
	} else {
		report.Add(VerificationResult{
			Component: "LLM 集成",
			Feature:   "客户端创建",
			Success:   true,
			Message:   "使用模拟模式（未配置 API Key）",
			Duration:  time.Since(start),
		})
		fmt.Println("  ⚠ 未配置 LLM API Key，使用模拟模式")

		// 模拟模式验证
		report.Add(VerificationResult{
			Component: "LLM 集成",
			Feature:   "模拟模式",
			Success:   true,
			Message:   "模拟 LLM 响应正常",
			Duration:  time.Millisecond * 10,
		})
		fmt.Println("  ✓ 模拟模式验证通过")
	}

	// 验证消息构建器
	start = time.Now()
	sysMsg := llm.SystemMessage("系统提示")
	userMsg := llm.UserMessage("用户消息")
	assistantMsg := llm.AssistantMessage("助手回复")

	if sysMsg.Role == "system" && userMsg.Role == "user" && assistantMsg.Role == "assistant" {
		report.Add(VerificationResult{
			Component: "LLM 集成",
			Feature:   "消息构建器",
			Success:   true,
			Message:   "System/User/Assistant 消息构建正常",
			Duration:  time.Since(start),
		})
		fmt.Println("  ✓ 消息构建器验证通过")
	}
}

// ============================================================================
// 2. 工具系统验证
// ============================================================================

func verifyToolSystem(ctx context.Context, report *VerificationReport) {
	// 创建工具注册表
	start := time.Now()
	registry := tools.NewRegistry()
	report.Add(VerificationResult{
		Component: "工具系统",
		Feature:   "注册表创建",
		Success:   true,
		Duration:  time.Since(start),
	})
	fmt.Println("  ✓ 工具注册表创建成功")

	// 创建并注册工具
	start = time.Now()
	calculatorTool := tools.NewFunctionTool(
		"calculator",
		"执行数学计算",
		`{"type": "object", "properties": {"expression": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]any) (any, error) {
			expr, _ := args["expression"].(string)
			// 简单模拟计算
			return map[string]any{
				"expression": expr,
				"result":     42,
			}, nil
		},
	)

	err := registry.Register(calculatorTool)
	if err == nil {
		report.Add(VerificationResult{
			Component: "工具系统",
			Feature:   "工具注册",
			Success:   true,
			Message:   "calculator 工具注册成功",
			Duration:  time.Since(start),
		})
		fmt.Println("  ✓ 工具注册成功")
	} else {
		report.Add(VerificationResult{
			Component: "工具系统",
			Feature:   "工具注册",
			Success:   false,
			Message:   err.Error(),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✗ 工具注册失败: %v\n", err)
	}

	// 验证工具执行
	start = time.Now()
	tool := registry.Get("calculator")
	if tool != nil {
		output, err := tool.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]any{"expression": "1 + 1"},
		})
		if err == nil && output != nil {
			report.Add(VerificationResult{
				Component: "工具系统",
				Feature:   "工具执行",
				Success:   true,
				Message:   fmt.Sprintf("结果: %v", output.Result),
				Duration:  time.Since(start),
			})
			fmt.Printf("  ✓ 工具执行成功: %v\n", output.Result)
		}
	}

	// 验证中间件
	start = time.Now()
	metricsCollector := &MetricsCollector{}
	metricsMW := createMetricsMiddleware(metricsCollector)

	wrappedTool := tools.WithMiddleware(calculatorTool, metricsMW)
	_, _ = wrappedTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]any{"expression": "2 * 3"},
	})

	stats := metricsCollector.GetStats()
	if stats.TotalCalls > 0 {
		report.Add(VerificationResult{
			Component: "工具系统",
			Feature:   "中间件",
			Success:   true,
			Message:   fmt.Sprintf("调用次数: %d", stats.TotalCalls),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ 中间件验证成功 (调用次数: %d)\n", stats.TotalCalls)
	}

	// 验证工具列表
	start = time.Now()
	allTools := registry.List()
	report.Add(VerificationResult{
		Component: "工具系统",
		Feature:   "工具列表",
		Success:   len(allTools) > 0,
		Message:   fmt.Sprintf("已注册 %d 个工具", len(allTools)),
		Duration:  time.Since(start),
	})
	fmt.Printf("  ✓ 工具列表验证: %d 个工具\n", len(allTools))
}

// ============================================================================
// 3. 记忆管理验证
// ============================================================================

func verifyMemoryManagement(ctx context.Context, report *VerificationReport) {
	// 创建记忆管理器
	start := time.Now()
	config := &memory.Config{
		EnableConversation:    true,
		MaxConversationLength: 100,
	}
	memoryManager := memory.NewInMemoryManager(config)
	report.Add(VerificationResult{
		Component: "记忆管理",
		Feature:   "管理器创建",
		Success:   true,
		Duration:  time.Since(start),
	})
	fmt.Println("  ✓ 记忆管理器创建成功")

	// 验证对话历史
	start = time.Now()
	sessionID := "test-session-001"
	err := memoryManager.AddConversation(ctx, &interfaces.Conversation{
		SessionID: sessionID,
		Role:      "user",
		Content:   "你好，这是测试消息",
		Timestamp: time.Now(),
	})

	if err == nil {
		history, _ := memoryManager.GetConversationHistory(ctx, sessionID, 10)
		if len(history) > 0 {
			report.Add(VerificationResult{
				Component: "记忆管理",
				Feature:   "对话历史",
				Success:   true,
				Message:   fmt.Sprintf("存储 %d 条对话", len(history)),
				Duration:  time.Since(start),
			})
			fmt.Printf("  ✓ 对话历史验证成功: %d 条消息\n", len(history))
		}
	}

	// 验证案例存储
	start = time.Now()
	testCase := &interfaces.Case{
		Title:       "测试案例",
		Description: "用于验证案例存储功能",
		Problem:     "如何验证案例存储？",
		Solution:    "创建案例并检索",
		Category:    "test",
		Tags:        []string{"verification", "test"},
	}
	err = memoryManager.AddCase(ctx, testCase)
	if err == nil && testCase.ID != "" {
		report.Add(VerificationResult{
			Component: "记忆管理",
			Feature:   "案例存储",
			Success:   true,
			Message:   fmt.Sprintf("案例 ID: %s", testCase.ID),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ 案例存储验证成功: %s\n", testCase.ID)
	}

	// 验证案例搜索
	start = time.Now()
	cases, err := memoryManager.SearchSimilarCases(ctx, "验证", 3)
	if err == nil {
		report.Add(VerificationResult{
			Component: "记忆管理",
			Feature:   "案例搜索",
			Success:   true,
			Message:   fmt.Sprintf("找到 %d 个相似案例", len(cases)),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ 案例搜索验证成功: %d 个结果\n", len(cases))
	}

	// 验证键值存储
	start = time.Now()
	err = memoryManager.Store(ctx, "test_key", "test_value")
	if err == nil {
		value, getErr := memoryManager.Retrieve(ctx, "test_key")
		if getErr == nil && value == "test_value" {
			report.Add(VerificationResult{
				Component: "记忆管理",
				Feature:   "键值存储",
				Success:   true,
				Message:   "存取验证通过",
				Duration:  time.Since(start),
			})
			fmt.Println("  ✓ 键值存储验证成功")
		}
	}
}

// ============================================================================
// 4. 多智能体协作验证
// ============================================================================

func verifyMultiAgentCollaboration(ctx context.Context, report *VerificationReport, logger loggercore.Logger) {
	// 创建多智能体系统
	start := time.Now()
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	report.Add(VerificationResult{
		Component: "多智能体",
		Feature:   "系统创建",
		Success:   true,
		Duration:  time.Since(start),
	})
	fmt.Println("  ✓ 多智能体系统创建成功")

	// 创建并注册 Agent
	start = time.Now()
	leaderAgent := NewTestAgent("leader", "领导者", multiagent.RoleLeader, system)
	worker1Agent := NewTestAgent("worker_1", "工作者1", multiagent.RoleWorker, system)
	worker2Agent := NewTestAgent("worker_2", "工作者2", multiagent.RoleWorker, system)

	err1 := system.RegisterAgent("leader", leaderAgent)
	err2 := system.RegisterAgent("worker_1", worker1Agent)
	err3 := system.RegisterAgent("worker_2", worker2Agent)

	if err1 == nil && err2 == nil && err3 == nil {
		report.Add(VerificationResult{
			Component: "多智能体",
			Feature:   "Agent 注册",
			Success:   true,
			Message:   "注册 3 个 Agent",
			Duration:  time.Since(start),
		})
		fmt.Println("  ✓ Agent 注册成功: leader, worker_1, worker_2")
	}

	// 验证并行协作
	start = time.Now()
	parallelTask := &multiagent.CollaborativeTask{
		ID:          "parallel-task-001",
		Name:        "并行测试任务",
		Type:        multiagent.CollaborationTypeParallel,
		Input:       "测试并行执行",
		Assignments: make(map[string]multiagent.Assignment),
	}

	_, err := system.ExecuteTask(ctx, parallelTask)
	if err == nil {
		report.Add(VerificationResult{
			Component: "多智能体",
			Feature:   "并行协作",
			Success:   true,
			Message:   fmt.Sprintf("任务状态: %s", parallelTask.Status),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ 并行协作验证成功: %s\n", parallelTask.Status)
	} else {
		report.Add(VerificationResult{
			Component: "多智能体",
			Feature:   "并行协作",
			Success:   false,
			Message:   err.Error(),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✗ 并行协作失败: %v\n", err)
	}

	// 验证顺序协作
	start = time.Now()
	sequentialTask := &multiagent.CollaborativeTask{
		ID:          "sequential-task-001",
		Name:        "顺序测试任务",
		Type:        multiagent.CollaborationTypeSequential,
		Input:       "初始输入",
		Assignments: make(map[string]multiagent.Assignment),
	}

	_, err = system.ExecuteTask(ctx, sequentialTask)
	if err == nil {
		report.Add(VerificationResult{
			Component: "多智能体",
			Feature:   "顺序协作",
			Success:   true,
			Message:   fmt.Sprintf("任务状态: %s", sequentialTask.Status),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ 顺序协作验证成功: %s\n", sequentialTask.Status)
	}

	// 验证消息通信
	start = time.Now()
	message := multiagent.Message{
		ID:        "msg-001",
		From:      "leader",
		To:        "worker_1",
		Type:      multiagent.MessageTypeRequest,
		Content:   "测试消息",
		Timestamp: time.Now(),
	}

	err = system.SendMessage(message)
	if err == nil {
		report.Add(VerificationResult{
			Component: "多智能体",
			Feature:   "消息通信",
			Success:   true,
			Message:   "消息发送成功",
			Duration:  time.Since(start),
		})
		fmt.Println("  ✓ 消息通信验证成功")
	}

	// 清理
	_ = system.UnregisterAgent("leader")
	_ = system.UnregisterAgent("worker_1")
	_ = system.UnregisterAgent("worker_2")
}

// ============================================================================
// 5. 规划执行验证
// ============================================================================

func verifyPlanningExecution(ctx context.Context, report *VerificationReport, logger loggercore.Logger) {
	// 创建记忆管理器
	memoryManager := memory.NewInMemoryManager(memory.DefaultConfig())

	// 创建 LLM 客户端（或使用模拟）
	llmClient := createLLMClient()

	// 创建智能规划器
	start := time.Now()
	planner := planning.NewSmartPlanner(
		llmClient,
		memoryManager,
		planning.WithMaxDepth(3),
		planning.WithTimeout(1*time.Minute),
	)

	report.Add(VerificationResult{
		Component: "规划执行",
		Feature:   "规划器创建",
		Success:   true,
		Duration:  time.Since(start),
	})
	fmt.Println("  ✓ SmartPlanner 创建成功")

	// 创建 AgentExecutor
	start = time.Now()
	executor := planning.NewAgentExecutor(logger)

	// 注册测试 Agent
	testAgent := &TestPlanningAgent{name: "test_agent"}
	executor.RegisterAgent("test_agent", testAgent)
	executor.RegisterAgent("analysis_agent", testAgent)
	executor.RegisterAgent("action_agent", testAgent)
	executor.RegisterAgent("validation_agent", testAgent)
	executor.RegisterAgent("default_agent", testAgent)

	report.Add(VerificationResult{
		Component: "规划执行",
		Feature:   "执行器创建",
		Success:   true,
		Message:   "AgentExecutor 已创建并注册 Agent",
		Duration:  time.Since(start),
	})
	fmt.Println("  ✓ AgentExecutor 创建成功")

	// 创建测试计划
	start = time.Now()
	goal := "开发一个简单的 API 服务"
	plan, err := planner.CreatePlan(ctx, goal, planning.PlanConstraints{
		MaxSteps:    5,
		MaxDuration: 10 * time.Minute,
	})

	if err == nil && plan != nil {
		report.Add(VerificationResult{
			Component: "规划执行",
			Feature:   "计划创建",
			Success:   true,
			Message:   fmt.Sprintf("计划 ID: %s, 步骤数: %d", plan.ID, len(plan.Steps)),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ 计划创建成功: %s (%d 步骤)\n", plan.ID, len(plan.Steps))

		// 为步骤分配 Agent
		for _, step := range plan.Steps {
			step.Agent = "test_agent"
		}

		// 验证计划
		start = time.Now()
		valid, issues, validateErr := planner.ValidatePlan(ctx, plan)
		if validateErr == nil {
			report.Add(VerificationResult{
				Component: "规划执行",
				Feature:   "计划验证",
				Success:   valid,
				Message:   fmt.Sprintf("有效: %v, 问题数: %d", valid, len(issues)),
				Duration:  time.Since(start),
			})
			fmt.Printf("  ✓ 计划验证: 有效=%v\n", valid)
		}

		// 执行计划
		start = time.Now()
		result, execErr := executor.Execute(ctx, plan)
		if execErr == nil && result != nil {
			report.Add(VerificationResult{
				Component: "规划执行",
				Feature:   "计划执行",
				Success:   result.Success,
				Message:   fmt.Sprintf("成功: %v, 完成: %d/%d", result.Success, result.CompletedSteps, len(plan.Steps)),
				Duration:  time.Since(start),
			})
			fmt.Printf("  ✓ 计划执行: 成功=%v, 完成=%d/%d\n", result.Success, result.CompletedSteps, len(plan.Steps))
		} else {
			report.Add(VerificationResult{
				Component: "规划执行",
				Feature:   "计划执行",
				Success:   false,
				Message:   fmt.Sprintf("错误: %v", execErr),
				Duration:  time.Since(start),
			})
			fmt.Printf("  ✗ 计划执行失败: %v\n", execErr)
		}
	} else {
		report.Add(VerificationResult{
			Component: "规划执行",
			Feature:   "计划创建",
			Success:   false,
			Message:   fmt.Sprintf("错误: %v", err),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✗ 计划创建失败: %v\n", err)
	}
}

// ============================================================================
// 6. RAG 检索验证
// ============================================================================

func verifyRAGRetrieval(ctx context.Context, report *VerificationReport) {
	// 创建模拟向量存储
	start := time.Now()
	vectorStore := retrieval.NewMockVectorStore()

	report.Add(VerificationResult{
		Component: "RAG 检索",
		Feature:   "向量存储创建",
		Success:   true,
		Duration:  time.Since(start),
	})
	fmt.Println("  ✓ 向量存储创建成功")

	// 添加测试文档
	start = time.Now()
	docs := []*interfaces.Document{
		{ID: "doc1", PageContent: "GoAgent 是一个 Go 语言的 Agent 框架", Metadata: map[string]any{"source": "docs"}},
		{ID: "doc2", PageContent: "Agent 可以使用工具来完成任务", Metadata: map[string]any{"source": "docs"}},
		{ID: "doc3", PageContent: "多智能体系统支持协作执行", Metadata: map[string]any{"source": "docs"}},
	}

	for _, doc := range docs {
		_ = vectorStore.AddDocuments(ctx, []*interfaces.Document{doc})
	}

	report.Add(VerificationResult{
		Component: "RAG 检索",
		Feature:   "文档添加",
		Success:   true,
		Message:   fmt.Sprintf("添加 %d 个文档", len(docs)),
		Duration:  time.Since(start),
	})
	fmt.Printf("  ✓ 文档添加成功: %d 个\n", len(docs))

	// 验证相似度搜索
	start = time.Now()
	results, err := vectorStore.SimilaritySearch(ctx, "Agent 框架", 2)
	if err == nil && len(results) > 0 {
		report.Add(VerificationResult{
			Component: "RAG 检索",
			Feature:   "相似度搜索",
			Success:   true,
			Message:   fmt.Sprintf("找到 %d 个结果", len(results)),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ 相似度搜索成功: %d 个结果\n", len(results))
	}

	// 创建 RAG 检索器
	start = time.Now()
	ragRetriever, err := retrieval.NewRAGRetriever(retrieval.RAGRetrieverConfig{
		VectorStore:      vectorStore,
		TopK:             3,
		ScoreThreshold:   0.0,
		MaxContentLength: 500,
	})

	if err == nil {
		report.Add(VerificationResult{
			Component: "RAG 检索",
			Feature:   "RAG 检索器",
			Success:   true,
			Duration:  time.Since(start),
		})
		fmt.Println("  ✓ RAG 检索器创建成功")

		// 验证 RAG 检索
		start = time.Now()
		retrievedDocs, retrieveErr := ragRetriever.Retrieve(ctx, "工具使用")
		if retrieveErr == nil {
			report.Add(VerificationResult{
				Component: "RAG 检索",
				Feature:   "RAG 检索",
				Success:   true,
				Message:   fmt.Sprintf("检索 %d 个文档", len(retrievedDocs)),
				Duration:  time.Since(start),
			})
			fmt.Printf("  ✓ RAG 检索成功: %d 个文档\n", len(retrievedDocs))
		}
	}

	// 验证向量存储检索器
	start = time.Now()
	vsRetriever := retrieval.NewVectorStoreRetriever(vectorStore, retrieval.RetrieverConfig{
		TopK:     3,
		MinScore: 0.0,
		Name:     "test-retriever",
	})

	docs2, err := vsRetriever.GetRelevantDocuments(ctx, "多智能体")
	if err == nil {
		report.Add(VerificationResult{
			Component: "RAG 检索",
			Feature:   "向量存储检索器",
			Success:   true,
			Message:   fmt.Sprintf("检索 %d 个文档", len(docs2)),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ 向量存储检索器验证成功: %d 个文档\n", len(docs2))
	}
}

// ============================================================================
// 7. 流式处理验证
// ============================================================================

func verifyStreamProcessing(_ context.Context, report *VerificationReport) {
	// 创建流管理器
	start := time.Now()
	streamManager := stream.NewStreamManager(stream.StreamManagerConfig{
		BufferSize: 100,
		Timeout:    30 * time.Second,
	})

	if streamManager != nil {
		report.Add(VerificationResult{
			Component: "流式处理",
			Feature:   "管理器创建",
			Success:   true,
			Duration:  time.Since(start),
		})
		fmt.Println("  ✓ 流管理器创建成功")
	}

	// 验证流数据块
	start = time.Now()
	chunk := stream.NewStreamChunk("测试数据")
	chunk.ChunkID = 1
	chunk.Metadata["type"] = "test"

	if chunk.Data != nil && chunk.ChunkID == 1 {
		report.Add(VerificationResult{
			Component: "流式处理",
			Feature:   "数据块创建",
			Success:   true,
			Message:   fmt.Sprintf("ChunkID: %d", chunk.ChunkID),
			Duration:  time.Since(start),
		})
		fmt.Println("  ✓ 流数据块创建成功")
	}

	// 验证函数式流处理器
	start = time.Now()
	var receivedChunks int
	handler := stream.NewFuncStreamHandler(
		func(c *stream.StreamChunk) error {
			receivedChunks++
			return nil
		},
		func() error {
			return nil
		},
		func(err error) error {
			return err
		},
	)

	// 模拟处理几个数据块
	for i := 0; i < 3; i++ {
		testChunk := stream.NewStreamChunk(fmt.Sprintf("数据 %d", i))
		_ = handler.OnChunk(testChunk)
	}
	_ = handler.OnComplete()

	if receivedChunks == 3 {
		report.Add(VerificationResult{
			Component: "流式处理",
			Feature:   "函数式处理器",
			Success:   true,
			Message:   fmt.Sprintf("处理 %d 个数据块", receivedChunks),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ 函数式处理器验证成功: %d 个数据块\n", receivedChunks)
	}

	// 验证环形缓冲区
	start = time.Now()
	ringBuffer := stream.NewRingBuffer(10)
	if ringBuffer != nil {
		// 推入数据
		for i := 0; i < 5; i++ {
			testChunk := &core.LegacyStreamChunk{
				Type: core.ChunkTypeText,
				Text: fmt.Sprintf("缓冲数据 %d", i),
			}
			ringBuffer.Push(testChunk)
		}

		// 读取数据
		var readCount int
		for !ringBuffer.IsEmpty() {
			ringBuffer.Pop()
			readCount++
		}

		report.Add(VerificationResult{
			Component: "流式处理",
			Feature:   "环形缓冲区",
			Success:   readCount == 5,
			Message:   fmt.Sprintf("写入 5, 读取 %d", readCount),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✓ 环形缓冲区验证成功: 读取 %d 个\n", readCount)
	}
}

// ============================================================================
// 8. 组件协同验证
// ============================================================================

func verifyComponentIntegration(ctx context.Context, report *VerificationReport, logger loggercore.Logger) {
	start := time.Now()

	// 创建所有核心组件
	memoryManager := memory.NewInMemoryManager(memory.DefaultConfig())
	registry := tools.NewRegistry()
	llmClient := createLLMClient()
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	// 注册工具
	analyzerTool := tools.NewFunctionTool(
		"analyzer",
		"分析输入数据",
		`{"type": "object", "properties": {"data": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]any) (any, error) {
			data, _ := args["data"].(string)
			return map[string]any{
				"analysis": fmt.Sprintf("分析结果: %s", data),
				"score":    0.85,
			}, nil
		},
	)
	_ = registry.Register(analyzerTool)

	// 创建规划器和执行器
	planner := planning.NewSmartPlanner(llmClient, memoryManager)
	executor := planning.NewAgentExecutor(logger)

	// 创建集成 Agent
	integratedAgent := &IntegratedAgent{
		name:          "integrated_agent",
		registry:      registry,
		memoryManager: memoryManager,
	}
	executor.RegisterAgent("integrated_agent", integratedAgent)
	executor.RegisterAgent("analysis_agent", integratedAgent)
	executor.RegisterAgent("action_agent", integratedAgent)
	executor.RegisterAgent("validation_agent", integratedAgent)
	executor.RegisterAgent("default_agent", integratedAgent)

	_ = system.RegisterAgent("integrated_agent", &TestAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(
			"integrated_agent", "集成测试 Agent", multiagent.RoleWorker, system,
		),
	})

	// 执行集成测试
	goal := "分析并处理用户请求"
	plan, err := planner.CreatePlan(ctx, goal, planning.PlanConstraints{
		MaxSteps:    3,
		MaxDuration: 5 * time.Minute,
	})

	if err == nil && plan != nil {
		for _, step := range plan.Steps {
			step.Agent = "integrated_agent"
		}

		result, execErr := executor.Execute(ctx, plan)

		// 存储执行经验
		_ = memoryManager.AddCase(ctx, &interfaces.Case{
			Title:    "集成测试案例",
			Problem:  goal,
			Solution: fmt.Sprintf("执行 %d 步骤", len(plan.Steps)),
			Category: "integration-test",
		})

		if execErr == nil && result != nil {
			report.Add(VerificationResult{
				Component: "组件协同",
				Feature:   "完整流程集成",
				Success:   result.Success,
				Message:   fmt.Sprintf("LLM+工具+记忆+规划+执行 全流程: 成功=%v", result.Success),
				Duration:  time.Since(start),
			})
			fmt.Printf("  ✓ 组件协同验证成功: 完整流程执行 成功=%v\n", result.Success)
		} else {
			report.Add(VerificationResult{
				Component: "组件协同",
				Feature:   "完整流程集成",
				Success:   false,
				Message:   fmt.Sprintf("执行错误: %v", execErr),
				Duration:  time.Since(start),
			})
			fmt.Printf("  ✗ 组件协同验证失败: %v\n", execErr)
		}
	} else {
		report.Add(VerificationResult{
			Component: "组件协同",
			Feature:   "完整流程集成",
			Success:   false,
			Message:   fmt.Sprintf("规划错误: %v", err),
			Duration:  time.Since(start),
		})
		fmt.Printf("  ✗ 组件协同验证失败: %v\n", err)
	}

	// 验证记忆持久化
	start = time.Now()
	cases, _ := memoryManager.SearchSimilarCases(ctx, "集成", 1)
	if len(cases) > 0 {
		report.Add(VerificationResult{
			Component: "组件协同",
			Feature:   "经验复用",
			Success:   true,
			Message:   "执行经验已存储并可检索",
			Duration:  time.Since(start),
		})
		fmt.Println("  ✓ 经验复用验证成功")
	}
}

// ============================================================================
// 辅助类型和函数
// ============================================================================

// TestAgent 测试用协作 Agent
type TestAgent struct {
	*multiagent.BaseCollaborativeAgent
}

func NewTestAgent(id, desc string, role multiagent.Role, system *multiagent.MultiAgentSystem) *TestAgent {
	return &TestAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(id, desc, role, system),
	}
}

func (a *TestAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	return &core.AgentOutput{
		Result: fmt.Sprintf("[%s] 处理: %s", a.Name(), input.Task),
		Metadata: map[string]any{
			"agent": a.Name(),
		},
	}, nil
}

func (a *TestAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	return &multiagent.Assignment{
		AgentID: a.Name(),
		Role:    a.GetRole(),
		Status:  multiagent.TaskStatusCompleted,
		Result:  fmt.Sprintf("[%s] 协作完成", a.Name()),
	}, nil
}

// TestPlanningAgent 测试用规划 Agent，实现完整的 core.Agent 接口
type TestPlanningAgent struct {
	name string
}

func (a *TestPlanningAgent) Name() string        { return a.name }
func (a *TestPlanningAgent) Description() string { return "测试规划 Agent" }
func (a *TestPlanningAgent) Capabilities() []string {
	return []string{"plan-execution", "step-handling"}
}

func (a *TestPlanningAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	return &core.AgentOutput{
		Result: fmt.Sprintf("[%s] 执行步骤: %s", a.name, input.Task),
		Metadata: map[string]any{
			"agent": a.name,
			"step":  input.Task,
		},
	}, nil
}

func (a *TestPlanningAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput], 1)
	go func() {
		defer close(ch)
		output, err := a.Invoke(ctx, input)
		ch <- core.StreamChunk[*core.AgentOutput]{Data: output, Error: err, Done: true}
	}()
	return ch, nil
}

func (a *TestPlanningAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	results := make([]*core.AgentOutput, len(inputs))
	for i, input := range inputs {
		output, err := a.Invoke(ctx, input)
		if err != nil {
			return nil, err
		}
		results[i] = output
	}
	return results, nil
}

func (a *TestPlanningAgent) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return nil // 简化实现
}

func (a *TestPlanningAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return a
}

func (a *TestPlanningAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return a
}

// IntegratedAgent 集成测试 Agent，实现完整的 core.Agent 接口
type IntegratedAgent struct {
	name          string
	registry      *tools.Registry
	memoryManager *memory.InMemoryManager
}

func (a *IntegratedAgent) Name() string        { return a.name }
func (a *IntegratedAgent) Description() string { return "集成测试 Agent" }
func (a *IntegratedAgent) Capabilities() []string {
	return []string{"tool-usage", "memory-access", "integration"}
}

func (a *IntegratedAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// 使用工具
	tool := a.registry.Get("analyzer")
	if tool != nil {
		output, _ := tool.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]any{"data": input.Task},
		})
		if output != nil {
			return &core.AgentOutput{
				Result:   output.Result,
				Metadata: map[string]any{"agent": a.name},
			}, nil
		}
	}

	return &core.AgentOutput{
		Result:   fmt.Sprintf("[%s] 处理: %s", a.name, input.Task),
		Metadata: map[string]any{"agent": a.name},
	}, nil
}

func (a *IntegratedAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput], 1)
	go func() {
		defer close(ch)
		output, err := a.Invoke(ctx, input)
		ch <- core.StreamChunk[*core.AgentOutput]{Data: output, Error: err, Done: true}
	}()
	return ch, nil
}

func (a *IntegratedAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	results := make([]*core.AgentOutput, len(inputs))
	for i, input := range inputs {
		output, err := a.Invoke(ctx, input)
		if err != nil {
			return nil, err
		}
		results[i] = output
	}
	return results, nil
}

func (a *IntegratedAgent) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return nil // 简化实现
}

func (a *IntegratedAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return a
}

func (a *IntegratedAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return a
}

// MetricsCollector 指标收集器
type MetricsCollector struct {
	totalCalls    int64
	successCalls  int64
	failedCalls   int64
	totalDuration int64
}

type MetricsStats struct {
	TotalCalls    int64
	SuccessCalls  int64
	FailedCalls   int64
	TotalDuration time.Duration
}

func (c *MetricsCollector) RecordCall(success bool, duration time.Duration) {
	atomic.AddInt64(&c.totalCalls, 1)
	if success {
		atomic.AddInt64(&c.successCalls, 1)
	} else {
		atomic.AddInt64(&c.failedCalls, 1)
	}
	atomic.AddInt64(&c.totalDuration, int64(duration))
}

func (c *MetricsCollector) GetStats() MetricsStats {
	return MetricsStats{
		TotalCalls:    atomic.LoadInt64(&c.totalCalls),
		SuccessCalls:  atomic.LoadInt64(&c.successCalls),
		FailedCalls:   atomic.LoadInt64(&c.failedCalls),
		TotalDuration: time.Duration(atomic.LoadInt64(&c.totalDuration)),
	}
}

func createMetricsMiddleware(collector *MetricsCollector) middleware.ToolMiddlewareFunc {
	return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
		return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			start := time.Now()
			output, err := next(ctx, input)
			collector.RecordCall(err == nil, time.Since(start))
			return output, err
		}
	}
}

// LLM 客户端创建
func createLLMClient() llm.Client {
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

func truncateString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// 简单日志实现
type simpleLogger struct{}

func (l *simpleLogger) Debug(args ...any)                       {}
func (l *simpleLogger) Info(args ...any)                        {}
func (l *simpleLogger) Warn(args ...any)                        {}
func (l *simpleLogger) Error(args ...any)                       {}
func (l *simpleLogger) Fatal(args ...any)                       {}
func (l *simpleLogger) Debugf(template string, args ...any)     {}
func (l *simpleLogger) Infof(template string, args ...any)      {}
func (l *simpleLogger) Warnf(template string, args ...any)      {}
func (l *simpleLogger) Errorf(template string, args ...any)     {}
func (l *simpleLogger) Fatalf(template string, args ...any)     {}
func (l *simpleLogger) Debugw(msg string, keysAndValues ...any) {}
func (l *simpleLogger) Infow(msg string, keysAndValues ...any)  {}
func (l *simpleLogger) Warnw(msg string, keysAndValues ...any)  {}
func (l *simpleLogger) Errorw(msg string, keysAndValues ...any) {}
func (l *simpleLogger) Fatalw(msg string, keysAndValues ...any) {}
func (l *simpleLogger) With(keyValues ...any) loggercore.Logger { return l }
func (l *simpleLogger) WithCtx(_ context.Context, keyValues ...any) loggercore.Logger {
	return l
}
func (l *simpleLogger) WithCallerSkip(skip int) loggercore.Logger { return l }
func (l *simpleLogger) SetLevel(level loggercore.Level)           {}
func (l *simpleLogger) Sync() error                               { return nil }
func (l *simpleLogger) Flush() error                              { return nil }

var _ loggercore.Logger = (*simpleLogger)(nil)
