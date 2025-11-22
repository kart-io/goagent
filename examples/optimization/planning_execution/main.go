package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/agents/cot"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/planning"
)

// PlanningExecutionExample 演示使用 Planning 模块优化复杂任务
//
// 注意：这是一个概念性示例，展示如何使用 Planning 进行前瞻性规划
// 使用模拟执行来演示完整流程
func main() {
	ctx := context.Background()

	// 检查 API Key
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		err := errors.New(errors.CodeInvalidConfig, "DEEPSEEK_API_KEY environment variable is not set").
			WithOperation("initialization").
			WithComponent("planning_execution_example").
			WithContext("env_var", "DEEPSEEK_API_KEY")
		fmt.Printf("错误: %v\n", err)
		fmt.Println("请设置环境变量 DEEPSEEK_API_KEY")
		os.Exit(1)
	}

	// 初始化 LLM 客户端
	llmClient, err := providers.NewDeepSeek(&llm.LLMOptions{
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		MaxTokens:   2000,
		Temperature: 0.7,
		Timeout:     60,
	})
	if err != nil {
		wrappedErr := errors.Wrap(err, errors.CodeLLMRequest, "failed to create LLM client").
			WithOperation("initialization").
			WithComponent("planning_execution_example").
			WithContext("provider", "deepseek").
			WithContext("model", "deepseek-chat")
		fmt.Printf("错误: %v\n", wrappedErr)
		os.Exit(1)
	}

	// 初始化内存管理器
	memoryManager := memory.NewInMemoryManager(memory.DefaultConfig())
	fmt.Println("✓ 内存管理器初始化完成")

	fmt.Println()
	fmt.Println("=== Planning + Execution 优化示例 ===")
	fmt.Println()

	// 示例任务：分析销售数据并生成报告
	task := `任务：分析 2024 年 Q4 的销售数据，并生成综合报告。

要求：
1. 加载并清洗销售数据
2. 分析销售趋势（按产品类别、地区、时间）
3. 识别表现最好和最差的产品
4. 分析客户行为模式
5. 生成可视化图表
6. 撰写执行摘要
7. 提供改进建议`

	// 步骤 1: 创建智能规划器
	fmt.Println("【步骤 1】创建智能规划器")
	planner := createSmartPlanner(llmClient, memoryManager)

	// 步骤 2: 创建初始计划
	fmt.Println()
	fmt.Println("【步骤 2】创建初始计划")
	plan := createInitialPlan(ctx, planner, task)
	printPlan("初始计划", plan)

	// 步骤 3: 验证计划
	fmt.Println()
	fmt.Println("【步骤 3】验证计划")
	validatedPlan := validateAndRefinePlan(ctx, planner, plan)

	// 步骤 4: 优化计划
	fmt.Println()
	fmt.Println("【步骤 4】优化计划")
	optimizedPlan := optimizePlan(ctx, planner, validatedPlan)
	printPlan("优化后计划", optimizedPlan)

	// 步骤 5: 执行计划（使用真实 Agent）
	fmt.Println()
	fmt.Println("【步骤 5】执行计划（使用真实 Agent）")
	executePlan(ctx, llmClient, optimizedPlan)

	// 总结
	fmt.Println()
	fmt.Println("=== 执行总结 ===")
	printExecutionSummary(optimizedPlan)
}

// createSmartPlanner 创建智能规划器
func createSmartPlanner(llmClient llm.Client, memoryMgr interfaces.MemoryManager) *planning.SmartPlanner {
	planner := planning.NewSmartPlanner(
		llmClient,
		memoryMgr,                           // 使用内存管理器
		planning.WithMaxDepth(3),            // 最大规划深度
		planning.WithTimeout(5*time.Minute), // 规划超时
		planning.WithOptimizer(&planning.DefaultOptimizer{}), // 使用默认优化器
	)

	fmt.Println("✓ 智能规划器创建成功")
	fmt.Println("  - 最大深度: 3")
	fmt.Println("  - 超时时间: 5 分钟")
	fmt.Println("  - 内存支持: 已启用")
	fmt.Println("  - 已注册策略: decomposition, backward_chaining, hierarchical")

	return planner
}

// createInitialPlan 创建初始计划
func createInitialPlan(ctx context.Context, planner *planning.SmartPlanner, task string) *planning.Plan {
	startTime := time.Now()

	plan, err := planner.CreatePlan(ctx, task, planning.PlanConstraints{
		MaxSteps:    20,               // 最大 20 个步骤
		MaxDuration: 30 * time.Minute, // 最大执行时间 30 分钟
	})
	if err != nil {
		wrappedErr := errors.Wrap(err, errors.CodeInternal, "failed to create plan").
			WithOperation("create_plan").
			WithComponent("smart_planner").
			WithContext("max_steps", 20).
			WithContext("max_duration", "30m")
		fmt.Printf("错误: %v\n", wrappedErr)
		os.Exit(1)
	}

	fmt.Printf("✓ 计划创建成功 (耗时: %v)\n", time.Since(startTime))
	fmt.Printf("  - 计划 ID: %s\n", plan.ID)
	fmt.Printf("  - 策略: %s\n", plan.Strategy)
	fmt.Printf("  - 步骤数: %d\n", len(plan.Steps))

	return plan
}

// validateAndRefinePlan 验证并优化计划
func validateAndRefinePlan(ctx context.Context, planner *planning.SmartPlanner, plan *planning.Plan) *planning.Plan {
	valid, issues, err := planner.ValidatePlan(ctx, plan)
	if err != nil {
		wrappedErr := errors.Wrap(err, errors.CodeInternal, "plan validation failed").
			WithOperation("validate_plan").
			WithComponent("smart_planner").
			WithContext("plan_id", plan.ID)
		fmt.Printf("错误: %v\n", wrappedErr)
		os.Exit(1)
	}

	if valid {
		fmt.Println("✓ 计划验证通过")
		return plan
	}

	// 如果验证失败，根据问题优化计划
	fmt.Printf("⚠ 发现 %d 个问题:\n", len(issues))
	for i, issue := range issues {
		fmt.Printf("  %d. %s\n", i+1, issue)
	}

	fmt.Println()
	fmt.Println("正在优化计划...")
	refinedPlan, err := planner.RefinePlan(ctx, plan, fmt.Sprintf("修复以下问题: %v", issues))
	if err != nil {
		wrappedErr := errors.Wrap(err, errors.CodeInternal, "plan refinement failed").
			WithOperation("refine_plan").
			WithComponent("smart_planner").
			WithContext("plan_id", plan.ID).
			WithContext("issues_count", len(issues))
		fmt.Printf("错误: %v\n", wrappedErr)
		os.Exit(1)
	}

	fmt.Println("✓ 计划已优化")
	return refinedPlan
}

// optimizePlan 优化计划
func optimizePlan(ctx context.Context, planner *planning.SmartPlanner, plan *planning.Plan) *planning.Plan {
	startTime := time.Now()

	optimizedPlan, err := planner.OptimizePlan(ctx, plan)
	if err != nil {
		wrappedErr := errors.Wrap(err, errors.CodeInternal, "plan optimization failed, using original plan").
			WithOperation("optimize_plan").
			WithComponent("smart_planner").
			WithContext("plan_id", plan.ID)
		fmt.Printf("警告: %v\n", wrappedErr)
		return plan
	}

	fmt.Printf("✓ 计划优化成功 (耗时: %v)\n", time.Since(startTime))

	// 计算优化效果
	originalSteps := len(plan.Steps)
	optimizedSteps := len(optimizedPlan.Steps)
	reduction := float64(originalSteps-optimizedSteps) / float64(originalSteps) * 100

	fmt.Printf("  - 原始步骤: %d\n", originalSteps)
	fmt.Printf("  - 优化后步骤: %d\n", optimizedSteps)
	if reduction > 0 {
		fmt.Printf("  - 步骤减少: %.1f%%\n", reduction)
	}

	// 统计可并行的步骤
	parallelSteps := 0
	for _, step := range optimizedPlan.Steps {
		if parallel, ok := step.Parameters["parallel"].(bool); ok && parallel {
			parallelSteps++
		}
	}
	if parallelSteps > 0 {
		fmt.Printf("  - 可并行步骤: %d\n", parallelSteps)
	}

	return optimizedPlan
}

// executePlan 执行计划（使用真实 Agent）
func executePlan(ctx context.Context, llmClient llm.Client, plan *planning.Plan) {
	fmt.Printf("开始执行计划 %s（真实 Agent 模式）\n", plan.ID)

	// 创建 CoT Agent 用于执行步骤
	agent := cot.NewCoTAgent(cot.CoTConfig{
		Name:        "ExecutionAgent",
		Description: "Execute planning steps with Chain-of-Thought reasoning",
		LLM:         llmClient,
		MaxSteps:    5,
		ZeroShot:    true,
	})

	totalSteps := len(plan.Steps)
	totalPromptTokens := 0
	totalCompletionTokens := 0
	totalTokens := 0

	for i, step := range plan.Steps {
		fmt.Println()
		fmt.Printf("[%d/%d] 执行步骤: %s\n", i+1, totalSteps, step.Name)
		fmt.Printf("      类型: %s\n", step.Type)
		fmt.Printf("      描述: %s\n", step.Description)

		// 使用真实 Agent 执行步骤
		startTime := time.Now()
		output, err := agent.Invoke(ctx, &agentcore.AgentInput{
			Task:        step.Description,
			Instruction: fmt.Sprintf("步骤 %d/%d: %s", i+1, totalSteps, step.Name),
			Context: map[string]interface{}{
				"step_type": step.Type,
				"priority":  step.Priority,
			},
			Timestamp: startTime,
		})
		duration := time.Since(startTime)

		if err != nil {
			fmt.Printf("      ❌ 执行失败: %v (耗时: %v)\n", err, duration)
			step.Status = planning.StepStatusFailed
			step.Result = &planning.StepResult{
				Success:   false,
				Output:    fmt.Sprintf("错误: %v", err),
				Duration:  duration,
				Timestamp: time.Now(),
				Metadata: map[string]interface{}{
					"error": err.Error(),
				},
			}
			continue
		}

		// 显示执行结果
		fmt.Printf("      ✓ 执行成功 (耗时: %v)\n", duration)
		fmt.Printf("      推理步骤: %d 步\n", len(output.ReasoningSteps))

		// 显示 Token 使用统计
		if output.TokenUsage != nil && !output.TokenUsage.IsEmpty() {
			fmt.Printf("      Token 使用: Prompt=%d, Completion=%d, Total=%d\n",
				output.TokenUsage.PromptTokens,
				output.TokenUsage.CompletionTokens,
				output.TokenUsage.TotalTokens)

			// 累加总 Token 使用
			totalPromptTokens += output.TokenUsage.PromptTokens
			totalCompletionTokens += output.TokenUsage.CompletionTokens
			totalTokens += output.TokenUsage.TotalTokens
		}

		// 显示结果摘要
		resultStr := ""
		if output.Result != nil && fmt.Sprintf("%v", output.Result) != "" {
			resultStr = fmt.Sprintf("%v", output.Result)
		} else if len(output.ReasoningSteps) > 0 {
			lastStep := output.ReasoningSteps[len(output.ReasoningSteps)-1]
			resultStr = lastStep.Result
		}

		if resultStr != "" {
			if len(resultStr) > 150 {
				resultStr = resultStr[:150] + "..."
			}
			fmt.Printf("      结果: %s\n", resultStr)
		}

		// 检查是否可以并行执行
		if parallel, ok := step.Parameters["parallel"].(bool); ok && parallel {
			fmt.Println("      ⚡ 此步骤可与其他步骤并行执行")
		}

		// 更新步骤状态
		step.Status = planning.StepStatusCompleted
		step.Result = &planning.StepResult{
			Success:   true,
			Output:    fmt.Sprintf("%v", output.Result),
			Duration:  duration,
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"reasoning_steps": len(output.ReasoningSteps),
				"tool_calls":      len(output.ToolCalls),
				"token_usage":     output.TokenUsage,
			},
		}
	}

	plan.Status = planning.PlanStatusCompleted
	fmt.Println()
	fmt.Println("✓ 计划执行完成")

	// 显示总 Token 使用统计
	if totalTokens > 0 {
		fmt.Println()
		fmt.Println("=== 总 Token 使用统计 ===")
		fmt.Printf("Prompt Tokens: %d\n", totalPromptTokens)
		fmt.Printf("Completion Tokens: %d\n", totalCompletionTokens)
		fmt.Printf("Total Tokens: %d\n", totalTokens)
		if totalSteps > 0 {
			fmt.Printf("平均每步: %.1f tokens\n", float64(totalTokens)/float64(totalSteps))
		}
	}
}

// printPlan 打印计划详情
func printPlan(title string, plan *planning.Plan) {
	fmt.Println()
	fmt.Printf("--- %s ---\n", title)
	fmt.Printf("ID: %s\n", plan.ID)
	fmt.Printf("目标: %s\n", plan.Goal)
	fmt.Printf("策略: %s\n", plan.Strategy)
	fmt.Printf("状态: %s\n", plan.Status)
	fmt.Printf("步骤数: %d\n", len(plan.Steps))

	fmt.Println()
	fmt.Println("步骤列表:")
	for i, step := range plan.Steps {
		fmt.Printf("  %d. [%s] %s\n", i+1, step.Type, step.Name)
		fmt.Printf("     描述: %s\n", step.Description)
		fmt.Printf("     优先级: %d\n", step.Priority)
		if step.EstimatedDuration > 0 {
			fmt.Printf("     预计时长: %v\n", step.EstimatedDuration)
		}

		// 显示依赖关系
		if deps, ok := plan.Dependencies[step.ID]; ok && len(deps) > 0 {
			fmt.Printf("     依赖: %v\n", deps)
		}
	}
}

// printExecutionSummary 打印执行总结
func printExecutionSummary(plan *planning.Plan) {
	completedSteps := 0
	failedSteps := 0
	totalDuration := time.Duration(0)
	totalPromptTokens := 0
	totalCompletionTokens := 0
	totalTokens := 0

	for _, step := range plan.Steps {
		switch step.Status {
		case planning.StepStatusCompleted:
			completedSteps++
			if step.Result != nil {
				totalDuration += step.Result.Duration

				// 收集 Token 使用统计
				if tokenUsage, ok := step.Result.Metadata["token_usage"].(*interfaces.TokenUsage); ok && tokenUsage != nil {
					totalPromptTokens += tokenUsage.PromptTokens
					totalCompletionTokens += tokenUsage.CompletionTokens
					totalTokens += tokenUsage.TotalTokens
				}
			}
		case planning.StepStatusFailed:
			failedSteps++
		}
	}

	successRate := float64(completedSteps) / float64(len(plan.Steps)) * 100

	fmt.Printf("计划 ID: %s\n", plan.ID)
	fmt.Printf("总步骤: %d\n", len(plan.Steps))
	fmt.Printf("已完成: %d\n", completedSteps)
	fmt.Printf("失败: %d\n", failedSteps)
	fmt.Printf("成功率: %.1f%%\n", successRate)
	fmt.Printf("总耗时: %v\n", totalDuration)

	// 显示 Token 使用统计
	if totalTokens > 0 {
		fmt.Println()
		fmt.Println("=== Token 使用总结 ===")
		fmt.Printf("Prompt Tokens: %d\n", totalPromptTokens)
		fmt.Printf("Completion Tokens: %d\n", totalCompletionTokens)
		fmt.Printf("Total Tokens: %d\n", totalTokens)
		if completedSteps > 0 {
			fmt.Printf("平均每步: %.1f tokens\n", float64(totalTokens)/float64(completedSteps))
		}
	}

	fmt.Println()
	fmt.Println("=== Planning 模式优势 ===")
	fmt.Println("1. ✓ 前瞻性规划：提前识别所有必需步骤")
	fmt.Println("2. ✓ 智能优化：自动减少冗余步骤")
	fmt.Println("3. ✓ 并行执行：识别可并行步骤，节省时间")
	fmt.Println("4. ✓ 可验证性：执行前验证计划可行性")
	fmt.Println("5. ✓ 可追踪性：完整的执行历史和指标")
	fmt.Println("6. ✓ Token 追踪：精确的 Token 使用统计")
}
