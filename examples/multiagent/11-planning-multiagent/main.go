// Package main 演示多智能体系统与规划模块的集成使用
//
// 本示例展示：
// 1. Planning + MultiAgent 集成架构
// 2. 规划协调 Agent 使用 SmartPlanner 生成和管理计划
// 3. 专业化 Agent 执行计划中的特定步骤
// 4. 使用 planning.AgentExecutor 执行计划
// 5. 完整的规划-执行-验证流程
// 6. 执行结果的记忆存储和复用
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
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/middleware"

	loggercore "github.com/kart-io/logger/core"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          Planning + MultiAgent 集成示例                        ║")
	fmt.Println("║   展示规划模块与多智能体系统的协同工作                          ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// 创建日志和系统
	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	// 场景：基于规划的智能任务执行
	fmt.Println("【综合场景】基于规划的智能任务执行系统")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstratePlanningMultiAgent(ctx, system, logger)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// 综合场景：基于规划的智能任务执行系统
// ============================================================================

func demonstratePlanningMultiAgent(ctx context.Context, system *multiagent.MultiAgentSystem, logger loggercore.Logger) {
	fmt.Println("\n场景描述:")
	fmt.Println("  构建一个基于规划的智能任务执行系统")
	fmt.Println("  - 规划协调 Agent：使用 SmartPlanner 生成和管理计划")
	fmt.Println("  - 分析 Agent：执行分析类型步骤")
	fmt.Println("  - 执行 Agent：执行行动类型步骤")
	fmt.Println("  - 验证 Agent：执行验证类型步骤")
	fmt.Println("  - 使用 planning.AgentExecutor 执行计划")
	fmt.Println()

	// ========== 1. 创建共享组件 ==========
	fmt.Println("【步骤 1】创建共享组件")
	fmt.Println("────────────────────────────────────────")

	// 创建 LLM 客户端
	llmClient := createLLMClient()
	if llmClient == nil {
		fmt.Println("  ⚠ 未配置 LLM API Key，将使用模拟模式")
	} else {
		fmt.Printf("  ✓ LLM 客户端: %s\n", llmClient.Provider())
	}

	// 创建记忆管理器
	memoryConfig := &memory.Config{
		EnableConversation:    true,
		MaxConversationLength: 50,
	}
	memoryManager := memory.NewInMemoryManager(memoryConfig)
	fmt.Println("  ✓ 记忆管理器已创建 (对话历史 + 案例存储 + 计划存储)")

	// 创建工具注册表
	registry := tools.NewRegistry()
	fmt.Println("  ✓ 工具注册表已创建")

	// 创建中间件
	metricsCollector := &MetricsCollector{}
	metricsMW := createMetricsMiddleware(metricsCollector)

	// 注册工具
	registerPlanningTools(registry, metricsMW)
	fmt.Printf("  ✓ 已注册 %d 个工具\n", registry.Size())

	// 创建智能规划器
	planner := planning.NewSmartPlanner(
		llmClient,
		memoryManager,
		planning.WithMaxDepth(3),
		planning.WithTimeout(2*time.Minute),
	)
	fmt.Println("  ✓ SmartPlanner 已创建")

	// 创建计划执行器（使用 planning 模块提供的执行器）
	executor := planning.NewAgentExecutor(logger)
	fmt.Println("  ✓ AgentExecutor 已创建")

	// ========== 2. 创建专业化 Agent ==========
	fmt.Println("\n【步骤 2】创建专业化 Agent")
	fmt.Println("────────────────────────────────────────")

	// 创建分析 Agent
	analysisAgent := NewSpecialistAgent(
		"analysis_agent", // 统一使用下划线命名
		"分析专家",
		multiagent.RoleSpecialist,
		system,
		llmClient,
		registry,
		[]string{"data_analyzer", "requirement_analyzer"},
		"你是一个分析专家，负责分析数据和需求。请简洁准确地完成分析任务。",
	)

	// 创建执行 Agent
	actionAgent := NewSpecialistAgent(
		"action_agent",
		"执行专家",
		multiagent.RoleWorker,
		system,
		llmClient,
		registry,
		[]string{"code_generator", "api_builder"},
		"你是一个执行专家，负责执行具体的开发任务。请高效完成任务。",
	)

	// 创建验证 Agent
	validationAgent := NewSpecialistAgent(
		"validation_agent",
		"验证专家",
		multiagent.RoleValidator,
		system,
		llmClient,
		registry,
		[]string{"test_runner", "quality_checker"},
		"你是一个验证专家，负责验证任务结果的正确性和质量。",
	)

	// 注册 Agent 到 MultiAgentSystem（用于消息通信）
	if err := system.RegisterAgent("analysis_agent", analysisAgent); err != nil {
		fmt.Printf("  ✗ 注册 analysis_agent 失败: %v\n", err)
		return
	}
	if err := system.RegisterAgent("action_agent", actionAgent); err != nil {
		fmt.Printf("  ✗ 注册 action_agent 失败: %v\n", err)
		return
	}
	if err := system.RegisterAgent("validation_agent", validationAgent); err != nil {
		fmt.Printf("  ✗ 注册 validation_agent 失败: %v\n", err)
		return
	}

	// 注册 Agent 到 AgentExecutor（用于计划执行）
	executor.RegisterAgent("analysis_agent", analysisAgent)
	executor.RegisterAgent("action_agent", actionAgent)
	executor.RegisterAgent("validation_agent", validationAgent)
	executor.RegisterAgent("default_agent", actionAgent)

	// 创建规划协调 Agent
	coordinatorAgent := NewPlanningCoordinatorAgent(
		"planning_coordinator",
		"规划协调者",
		system,
		llmClient,
		planner,
		executor,
		memoryManager,
	)

	if err := system.RegisterAgent("planning_coordinator", coordinatorAgent); err != nil {
		fmt.Printf("  ✗ 注册 planning_coordinator 失败: %v\n", err)
		return
	}

	fmt.Println("  ✓ planning_coordinator: 规划协调者 (SmartPlanner + AgentExecutor)")
	fmt.Println("  ✓ analysis_agent: 分析专家 (data_analyzer, requirement_analyzer)")
	fmt.Println("  ✓ action_agent: 执行专家 (code_generator, api_builder)")
	fmt.Println("  ✓ validation_agent: 验证专家 (test_runner, quality_checker)")

	// ========== 3. 执行规划任务 ==========
	fmt.Println("\n【步骤 3】执行规划任务")
	fmt.Println("────────────────────────────────────────")

	// 用户目标
	userGoal := "开发一个用户认证 API，包括登录、注册和密码重置功能"
	fmt.Printf("\n用户目标: %s\n\n", userGoal)

	// 记录用户请求
	sessionID := "planning-session-001"
	if err := memoryManager.AddConversation(ctx, &interfaces.Conversation{
		SessionID: sessionID,
		Role:      "user",
		Content:   userGoal,
		Timestamp: time.Now(),
	}); err != nil {
		logger.Warnw("记录对话失败", "error", err)
	}

	// 3.1 创建计划
	fmt.Println("[3.1] 创建计划...")
	startTime := time.Now()

	plan, err := coordinatorAgent.CreatePlan(ctx, userGoal, planning.PlanConstraints{
		MaxSteps:    8,
		MaxDuration: 30 * time.Minute,
	})

	if err != nil {
		fmt.Printf("  ✗ 创建计划失败: %v\n", err)
		return
	}

	fmt.Printf("  ✓ 计划创建成功 (耗时: %v)\n", time.Since(startTime))
	printPlanSummary(plan)

	// 3.2 验证计划
	fmt.Println("\n[3.2] 验证计划...")
	valid, issues, err := planner.ValidatePlan(ctx, plan)
	if err != nil {
		fmt.Printf("  ✗ 验证失败: %v\n", err)
	} else if valid {
		fmt.Println("  ✓ 计划验证通过")
	} else {
		fmt.Printf("  ⚠ 发现 %d 个问题:\n", len(issues))
		for _, issue := range issues {
			fmt.Printf("    - %s\n", issue)
		}
	}

	// 3.3 执行计划
	fmt.Println("\n[3.3] 执行计划...")
	fmt.Println("  步骤执行进度:")

	result, err := coordinatorAgent.ExecutePlan(ctx, plan)
	if err != nil {
		fmt.Printf("\n  ✗ 计划执行失败: %v\n", err)
	} else {
		fmt.Printf("\n  ✓ 计划执行完成\n")
		printExecutionResult(result)
	}

	// 3.4 存储执行经验
	fmt.Println("\n[3.4] 存储执行经验...")

	// 存储计划作为案例
	var successRate float64
	if plan.Metrics != nil {
		successRate = plan.Metrics.SuccessRate
	}

	planCase := &interfaces.Case{
		Title:       "用户认证 API 开发计划",
		Description: "完整的用户认证系统开发计划和执行经验",
		Problem:     userGoal,
		Solution:    fmt.Sprintf("执行了 %d 个步骤，成功率: %.1f%%", result.CompletedSteps+result.FailedSteps, successRate*100),
		Category:    "api-development",
		Tags:        []string{"authentication", "api", "user-management"},
		Metadata: map[string]any{
			"plan_id":         plan.ID,
			"total_steps":     len(plan.Steps),
			"completed_steps": result.CompletedSteps,
			"failed_steps":    result.FailedSteps,
			"success_rate":    successRate,
			"duration":        result.TotalDuration.String(),
		},
	}
	if caseErr := memoryManager.AddCase(ctx, planCase); caseErr != nil {
		fmt.Printf("  ✗ 存储案例失败: %v\n", caseErr)
	} else {
		fmt.Printf("  ✓ 执行经验已存储 (案例 ID: %s)\n", planCase.ID)
	}

	// 存储关键数据
	if storeErr := memoryManager.Store(ctx, "last_plan_id", plan.ID); storeErr != nil {
		logger.Warnw("存储 last_plan_id 失败", "error", storeErr)
	}
	if storeErr := memoryManager.Store(ctx, "last_plan_goal", plan.Goal); storeErr != nil {
		logger.Warnw("存储 last_plan_goal 失败", "error", storeErr)
	}
	if storeErr := memoryManager.Store(ctx, "last_execution_time", time.Now().Format(time.RFC3339)); storeErr != nil {
		logger.Warnw("存储 last_execution_time 失败", "error", storeErr)
	}
	fmt.Println("  ✓ 关键数据已存储到键值存储")

	// ========== 4. 复用经验 ==========
	fmt.Println("\n【步骤 4】复用执行经验")
	fmt.Println("────────────────────────────────────────")

	// 搜索相似案例
	fmt.Println("\n相似案例搜索 (查询: 'API 开发'):")
	cases, err := memoryManager.SearchSimilarCases(ctx, "API 开发", 3)
	if err != nil {
		logger.Warnw("搜索相似案例失败", "error", err)
	}
	for i, c := range cases {
		fmt.Printf("  %d. %s (相似度: %.2f)\n", i+1, c.Title, c.Similarity)
		fmt.Printf("     问题: %s\n", truncateString(c.Problem, 50))
		fmt.Printf("     方案: %s\n", truncateString(c.Solution, 50))
	}

	// ========== 5. 显示统计信息 ==========
	fmt.Println("\n【步骤 5】执行统计")
	fmt.Println("────────────────────────────────────────")

	stats := metricsCollector.GetStats()
	fmt.Printf("  工具调用次数: %d\n", stats.TotalCalls)
	fmt.Printf("  成功次数: %d\n", stats.SuccessCalls)
	fmt.Printf("  失败次数: %d\n", stats.FailedCalls)
	if stats.TotalCalls > 0 {
		fmt.Printf("  平均耗时: %v\n", stats.TotalDuration/time.Duration(stats.TotalCalls))
	}

	// 清理
	_ = system.UnregisterAgent("planning_coordinator")
	_ = system.UnregisterAgent("analysis_agent")
	_ = system.UnregisterAgent("action_agent")
	_ = system.UnregisterAgent("validation_agent")
}

// ============================================================================
// 规划协调 Agent
// ============================================================================

// PlanningCoordinatorAgent 规划协调 Agent，负责创建计划并协调执行
type PlanningCoordinatorAgent struct {
	*multiagent.BaseCollaborativeAgent
	llmClient     llm.Client
	planner       *planning.SmartPlanner
	executor      *planning.AgentExecutor
	memoryManager *memory.InMemoryManager
}

// NewPlanningCoordinatorAgent 创建规划协调 Agent
func NewPlanningCoordinatorAgent(
	id, description string,
	system *multiagent.MultiAgentSystem,
	llmClient llm.Client,
	planner *planning.SmartPlanner,
	executor *planning.AgentExecutor,
	memoryManager *memory.InMemoryManager,
) *PlanningCoordinatorAgent {
	return &PlanningCoordinatorAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(
			id, description, multiagent.RoleCoordinator, system,
		),
		llmClient:     llmClient,
		planner:       planner,
		executor:      executor,
		memoryManager: memoryManager,
	}
}

// CreatePlan 创建计划
func (a *PlanningCoordinatorAgent) CreatePlan(ctx context.Context, goal string, constraints planning.PlanConstraints) (*planning.Plan, error) {
	// 首先搜索相似的历史案例
	cases, _ := a.memoryManager.SearchSimilarCases(ctx, goal, 3)
	if len(cases) > 0 {
		fmt.Printf("  发现 %d 个相似历史案例可供参考\n", len(cases))
	}

	// 创建计划
	plan, err := a.planner.CreatePlan(ctx, goal, constraints)
	if err != nil {
		return nil, err
	}

	// 为每个步骤分配 Agent
	for _, step := range plan.Steps {
		step.Agent = selectAgentForStep(step)
	}

	return plan, nil
}

// ExecutePlan 执行计划（使用 planning.AgentExecutor）
func (a *PlanningCoordinatorAgent) ExecutePlan(ctx context.Context, plan *planning.Plan) (*planning.PlanResult, error) {
	return a.executor.Execute(ctx, plan)
}

// Invoke 实现 Agent 接口
func (a *PlanningCoordinatorAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	goal := input.Task
	if goal == "" {
		if g, ok := input.Context["goal"].(string); ok {
			goal = g
		}
	}

	// 获取约束
	var constraints planning.PlanConstraints
	if c, ok := input.Context["constraints"].(planning.PlanConstraints); ok {
		constraints = c
	} else {
		constraints = planning.PlanConstraints{
			MaxSteps:    10,
			MaxDuration: 30 * time.Minute,
		}
	}

	// 创建计划
	plan, err := a.CreatePlan(ctx, goal, constraints)
	if err != nil {
		return nil, err
	}

	// 执行计划
	result, err := a.ExecutePlan(ctx, plan)
	if err != nil {
		return &core.AgentOutput{
			Result: result,
			Metadata: map[string]any{
				"plan_id": plan.ID,
				"error":   err.Error(),
			},
		}, err
	}

	return &core.AgentOutput{
		Result: result,
		Metadata: map[string]any{
			"plan_id":         plan.ID,
			"total_steps":     len(plan.Steps),
			"completed_steps": result.CompletedSteps,
			"success":         result.Success,
		},
	}, nil
}

// Collaborate 实现协作接口
func (a *PlanningCoordinatorAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	assignment := &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Status:    multiagent.TaskStatusExecuting,
		StartTime: time.Now(),
	}

	// 从任务中提取目标
	var goal string
	switch input := task.Input.(type) {
	case string:
		goal = input
	case map[string]any:
		if g, ok := input["goal"].(string); ok {
			goal = g
		}
	}

	if goal == "" {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, fmt.Errorf("未提供目标")
	}

	// 创建并执行计划
	plan, err := a.CreatePlan(ctx, goal, planning.PlanConstraints{})
	if err != nil {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, err
	}

	result, err := a.ExecutePlan(ctx, plan)
	if err != nil {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, err
	}

	assignment.Result = result
	assignment.Status = multiagent.TaskStatusCompleted
	assignment.EndTime = time.Now()

	return assignment, nil
}

// ============================================================================
// 专业化 Agent
// ============================================================================

// SpecialistAgent 专业化 Agent，执行特定类型的任务
type SpecialistAgent struct {
	*multiagent.BaseCollaborativeAgent
	llmClient    llm.Client
	registry     *tools.Registry
	allowedTools []string
	systemPrompt string
}

// NewSpecialistAgent 创建专业化 Agent
func NewSpecialistAgent(
	id, description string,
	role multiagent.Role,
	system *multiagent.MultiAgentSystem,
	llmClient llm.Client,
	registry *tools.Registry,
	allowedTools []string,
	systemPrompt string,
) *SpecialistAgent {
	return &SpecialistAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(id, description, role, system),
		llmClient:              llmClient,
		registry:               registry,
		allowedTools:           allowedTools,
		systemPrompt:           systemPrompt,
	}
}

// Invoke 实现 Agent 接口
func (a *SpecialistAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// 从输入中获取步骤信息
	var step *planning.Step
	if s, ok := input.Context["step"].(*planning.Step); ok {
		step = s
	}

	// 获取任务描述
	task := input.Task
	if task == "" && step != nil {
		task = step.Description
	}

	fmt.Printf("    → [%s] 执行: %s\n", a.Name(), truncateString(task, 40))

	// 根据步骤类型选择工具
	var result any
	var err error

	if step != nil && len(a.allowedTools) > 0 {
		// 选择合适的工具
		toolName := a.selectTool(step)
		if toolName != "" {
			tool := a.registry.Get(toolName)
			if tool != nil {
				output, toolErr := tool.Invoke(ctx, &interfaces.ToolInput{
					Args: map[string]any{
						"task":        task,
						"step_type":   string(step.Type),
						"description": step.Description,
					},
				})
				if toolErr == nil && output != nil {
					result = output.Result
				} else {
					err = toolErr
				}
			}
		}
	}

	// 如果没有工具结果，使用 LLM
	if result == nil && err == nil {
		result = a.analyzeWithLLM(ctx, task)
	}

	stepType := ""
	if step != nil {
		stepType = string(step.Type)
	}

	return &core.AgentOutput{
		Result: result,
		Metadata: map[string]any{
			"agent":     a.Name(),
			"task":      task,
			"step_type": stepType,
		},
	}, err
}

// selectTool 选择合适的工具
func (a *SpecialistAgent) selectTool(step *planning.Step) string {
	if len(a.allowedTools) == 0 {
		return ""
	}

	// 根据步骤类型选择工具
	switch step.Type {
	case planning.StepTypeAnalysis:
		for _, tool := range a.allowedTools {
			if strings.Contains(tool, "analyzer") {
				return tool
			}
		}
	case planning.StepTypeAction:
		for _, tool := range a.allowedTools {
			if strings.Contains(tool, "generator") || strings.Contains(tool, "builder") {
				return tool
			}
		}
	case planning.StepTypeValidation:
		for _, tool := range a.allowedTools {
			if strings.Contains(tool, "test") || strings.Contains(tool, "checker") {
				return tool
			}
		}
	}

	// 返回第一个可用工具
	return a.allowedTools[0]
}

// analyzeWithLLM 使用 LLM 分析
func (a *SpecialistAgent) analyzeWithLLM(ctx context.Context, task string) string {
	if a.llmClient == nil {
		return simulateLLMResponse(a.Name(), task)
	}

	messages := []llm.Message{
		llm.SystemMessage(a.systemPrompt),
		llm.UserMessage(task),
	}

	resp, err := a.llmClient.Chat(ctx, messages)
	if err != nil {
		return fmt.Sprintf("LLM 调用失败: %v", err)
	}

	return resp.Content
}

// Collaborate 实现协作接口
func (a *SpecialistAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	assignment := &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Status:    multiagent.TaskStatusExecuting,
		StartTime: time.Now(),
	}

	// 构建输入
	input := &core.AgentInput{
		Context: make(map[string]any),
	}

	switch v := task.Input.(type) {
	case string:
		input.Task = v
	case map[string]any:
		if t, ok := v["task"].(string); ok {
			input.Task = t
		}
		if s, ok := v["step"].(*planning.Step); ok {
			input.Context["step"] = s
		}
	}

	// 执行任务
	output, err := a.Invoke(ctx, input)
	if err != nil {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, err
	}

	assignment.Result = output.Result
	assignment.Status = multiagent.TaskStatusCompleted
	assignment.EndTime = time.Now()

	return assignment, nil
}

// ============================================================================
// 辅助函数
// ============================================================================

// selectAgentForStep 根据步骤类型选择合适的 Agent（统一的映射逻辑）
func selectAgentForStep(step *planning.Step) string {
	switch step.Type {
	case planning.StepTypeAnalysis:
		return "analysis_agent"
	case planning.StepTypeAction:
		return "action_agent"
	case planning.StepTypeValidation:
		return "validation_agent"
	case planning.StepTypeDecision:
		return "analysis_agent"
	case planning.StepTypeOptimization:
		return "action_agent"
	default:
		return "default_agent"
	}
}

// ============================================================================
// 工具定义
// ============================================================================

func registerPlanningTools(registry *tools.Registry, metricsMW middleware.ToolMiddlewareFunc) {
	// 需求分析器
	requirementAnalyzer := tools.NewFunctionTool(
		"requirement_analyzer",
		"分析用户需求并提取关键要素",
		`{"type": "object", "properties": {"task": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]any) (any, error) {
			task, _ := args["task"].(string)
			time.Sleep(100 * time.Millisecond)
			return map[string]any{
				"status":       "success",
				"requirements": []string{"用户认证", "API 接口", "数据验证"},
				"complexity":   "medium",
				"summary":      fmt.Sprintf("已分析需求: %s", truncateString(task, 30)),
			}, nil
		},
	)

	// 数据分析器
	dataAnalyzer := tools.NewFunctionTool(
		"data_analyzer",
		"分析数据结构和模式",
		`{"type": "object", "properties": {"task": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]any) (any, error) {
			time.Sleep(80 * time.Millisecond)
			return map[string]any{
				"status":   "success",
				"patterns": []string{"用户模型", "认证令牌", "会话管理"},
				"insights": "建议使用 JWT 进行身份验证",
			}, nil
		},
	)

	// 代码生成器
	codeGenerator := tools.NewFunctionTool(
		"code_generator",
		"生成代码框架",
		`{"type": "object", "properties": {"task": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]any) (any, error) {
			time.Sleep(150 * time.Millisecond)
			return map[string]any{
				"status":      "success",
				"files":       []string{"auth_handler.go", "user_model.go", "middleware.go"},
				"lines_added": 250,
				"message":     "代码框架已生成",
			}, nil
		},
	)

	// API 构建器
	apiBuilder := tools.NewFunctionTool(
		"api_builder",
		"构建 API 端点",
		`{"type": "object", "properties": {"task": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]any) (any, error) {
			time.Sleep(120 * time.Millisecond)
			return map[string]any{
				"status":    "success",
				"endpoints": []string{"POST /login", "POST /register", "POST /reset-password"},
				"message":   "API 端点已创建",
			}, nil
		},
	)

	// 测试运行器
	testRunner := tools.NewFunctionTool(
		"test_runner",
		"运行测试用例",
		`{"type": "object", "properties": {"task": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]any) (any, error) {
			time.Sleep(200 * time.Millisecond)
			return map[string]any{
				"status":     "success",
				"tests_run":  12,
				"tests_pass": 12,
				"tests_fail": 0,
				"coverage":   "85%",
				"message":    "所有测试通过",
			}, nil
		},
	)

	// 质量检查器
	qualityChecker := tools.NewFunctionTool(
		"quality_checker",
		"检查代码质量",
		`{"type": "object", "properties": {"task": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]any) (any, error) {
			time.Sleep(100 * time.Millisecond)
			return map[string]any{
				"status":  "success",
				"score":   92,
				"issues":  []string{},
				"message": "代码质量良好",
			}, nil
		},
	)

	// 注册工具（带中间件）
	toolsList := []interfaces.Tool{
		tools.WithMiddleware(requirementAnalyzer, metricsMW),
		tools.WithMiddleware(dataAnalyzer, metricsMW),
		tools.WithMiddleware(codeGenerator, metricsMW),
		tools.WithMiddleware(apiBuilder, metricsMW),
		tools.WithMiddleware(testRunner, metricsMW),
		tools.WithMiddleware(qualityChecker, metricsMW),
	}

	for _, tool := range toolsList {
		_ = registry.Register(tool)
	}
}

// ============================================================================
// 中间件
// ============================================================================

// MetricsCollector 指标收集器
type MetricsCollector struct {
	totalCalls    int64
	successCalls  int64
	failedCalls   int64
	totalDuration int64
}

// MetricsStats 指标统计
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
			duration := time.Since(start)
			collector.RecordCall(err == nil, duration)
			return output, err
		}
	}
}

// ============================================================================
// LLM 客户端创建
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

	// 其次使用 Kimi
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

func simulateLLMResponse(agentName, task string) string {
	switch {
	case strings.Contains(task, "分析"):
		return "分析完成，已识别关键需求和依赖关系。"
	case strings.Contains(task, "执行") || strings.Contains(task, "实现"):
		return "任务已执行，相关组件已创建。"
	case strings.Contains(task, "验证") || strings.Contains(task, "测试"):
		return "验证通过，结果符合预期。"
	default:
		return fmt.Sprintf("[%s] 任务已处理", agentName)
	}
}

// ============================================================================
// 输出辅助函数
// ============================================================================

func printPlanSummary(plan *planning.Plan) {
	fmt.Println()
	fmt.Printf("  计划 ID: %s\n", plan.ID)
	fmt.Printf("  目标: %s\n", truncateString(plan.Goal, 50))
	fmt.Printf("  策略: %s\n", plan.Strategy)
	fmt.Printf("  状态: %s\n", plan.Status)
	fmt.Printf("  步骤数: %d\n", len(plan.Steps))

	if len(plan.Steps) > 0 {
		fmt.Println("\n  步骤列表:")
		for i, step := range plan.Steps {
			agentInfo := ""
			if step.Agent != "" {
				agentInfo = fmt.Sprintf(" → %s", step.Agent)
			}
			fmt.Printf("    %d. [%s] %s%s\n", i+1, step.Type, step.Name, agentInfo)
		}
	}
}

func printExecutionResult(result *planning.PlanResult) {
	fmt.Printf("  计划 ID: %s\n", result.PlanID)
	fmt.Printf("  执行成功: %v\n", result.Success)
	fmt.Printf("  完成步骤: %d\n", result.CompletedSteps)
	fmt.Printf("  失败步骤: %d\n", result.FailedSteps)
	fmt.Printf("  跳过步骤: %d\n", result.SkippedSteps)
	fmt.Printf("  总耗时: %v\n", result.TotalDuration)
}

func truncateString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ============================================================================
// 日志实现
// ============================================================================

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
