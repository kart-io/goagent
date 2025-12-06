// Package main 演示 Planning 包的使用
//
// 本示例展示：
// 1. SmartPlanner - 智能规划器的创建和配置
// 2. Plan - 计划的创建、验证、优化和执行
// 3. PlanningAgent - 规划 Agent 的使用
// 4. TaskDecompositionAgent - 任务分解 Agent
// 5. 多种规划策略的应用
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/planning"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          Planning 智能规划示例                                  ║")
	fmt.Println("║   展示任务分解、计划生成、验证和执行的完整流程                    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// 创建 LLM 客户端
	llmClient := createLLMClient()
	if llmClient == nil {
		fmt.Println("未配置 LLM API Key，使用模拟模式运行...")
		runMockDemo()
		return
	}

	fmt.Printf("✓ LLM 提供商: %s\n\n", llmClient.Provider())

	// 创建内存管理器
	memoryManager := memory.NewInMemoryManager(memory.DefaultConfig())

	// 场景 1：基础规划流程
	fmt.Println("【场景 1】基础规划流程")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateBasicPlanning(ctx, llmClient, memoryManager)

	// 场景 2：任务分解
	fmt.Println("\n【场景 2】任务分解")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateTaskDecomposition(ctx, llmClient, memoryManager)

	// 场景 3：计划优化与执行
	fmt.Println("\n【场景 3】计划优化与执行")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstratePlanOptimization(ctx, llmClient, memoryManager)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// 场景 1：基础规划流程
// ============================================================================

func demonstrateBasicPlanning(ctx context.Context, llmClient llm.Client, memMgr interfaces.MemoryManager) {
	fmt.Println("\n场景描述: 使用 SmartPlanner 创建和管理计划")
	fmt.Println()

	// 1. 创建智能规划器
	fmt.Println("1. 创建智能规划器")
	fmt.Println("────────────────────────────────────────")

	planner := planning.NewSmartPlanner(
		llmClient,
		memMgr,
		planning.WithMaxDepth(3),
		planning.WithTimeout(2*time.Minute),
	)

	fmt.Println("  ✓ SmartPlanner 创建成功")
	fmt.Println("  - 最大深度: 3")
	fmt.Println("  - 超时时间: 2 分钟")
	fmt.Println("  - 内置策略: decomposition, backward_chaining, hierarchical")
	fmt.Println("  - 内置验证器: dependency, resource, time")

	// 2. 创建计划
	fmt.Println("\n2. 创建计划")
	fmt.Println("────────────────────────────────────────")

	goal := "开发一个用户注册功能，包括表单验证、密码加密和邮件通知"

	fmt.Printf("  目标: %s\n\n", goal)

	startTime := time.Now()
	plan, err := planner.CreatePlan(ctx, goal, planning.PlanConstraints{
		MaxSteps:    10,
		MaxDuration: 30 * time.Minute,
	})

	if err != nil {
		fmt.Printf("  ✗ 创建计划失败: %v\n", err)
		return
	}

	fmt.Printf("  ✓ 计划创建成功 (耗时: %v)\n", time.Since(startTime))
	printPlanSummary(plan)

	// 3. 验证计划
	fmt.Println("\n3. 验证计划")
	fmt.Println("────────────────────────────────────────")

	valid, issues, err := planner.ValidatePlan(ctx, plan)
	if err != nil {
		fmt.Printf("  ✗ 验证失败: %v\n", err)
		return
	}

	if valid {
		fmt.Println("  ✓ 计划验证通过")
	} else {
		fmt.Printf("  ⚠ 发现 %d 个问题:\n", len(issues))
		for i, issue := range issues {
			fmt.Printf("    %d. %s\n", i+1, issue)
		}
	}
}

// ============================================================================
// 场景 2：任务分解
// ============================================================================

func demonstrateTaskDecomposition(ctx context.Context, llmClient llm.Client, memMgr interfaces.MemoryManager) {
	fmt.Println("\n场景描述: 使用 TaskDecompositionAgent 将复杂任务分解为子任务")
	fmt.Println()

	// 创建规划器
	planner := planning.NewSmartPlanner(llmClient, memMgr)

	// 创建任务分解 Agent
	decomposer := planning.NewTaskDecompositionAgent(planner)

	fmt.Println("1. 创建任务分解 Agent")
	fmt.Println("────────────────────────────────────────")
	fmt.Printf("  Agent 名称: %s\n", decomposer.Name())
	fmt.Printf("  Agent 描述: %s\n", decomposer.Description())

	// 执行任务分解
	fmt.Println("\n2. 分解复杂任务")
	fmt.Println("────────────────────────────────────────")

	task := "设计并实现一个电商购物车系统"
	fmt.Printf("  原始任务: %s\n\n", task)

	startTime := time.Now()
	output, err := decomposer.Execute(ctx, &core.AgentInput{
		Task: task,
		Context: map[string]interface{}{
			"task": task,
		},
	})

	if err != nil {
		fmt.Printf("  ✗ 任务分解失败: %v\n", err)
		return
	}

	fmt.Printf("  ✓ 任务分解成功 (耗时: %v)\n\n", time.Since(startTime))

	// 显示分解结果
	if subtasks, ok := output.Result.([]map[string]interface{}); ok {
		fmt.Printf("  分解为 %d 个子任务:\n", len(subtasks))
		for i, subtask := range subtasks {
			name := subtask["name"].(string)
			desc := subtask["description"].(string)
			stepType := subtask["type"].(planning.StepType)
			fmt.Printf("\n  [%d] %s\n", i+1, name)
			fmt.Printf("      类型: %s\n", stepType)
			fmt.Printf("      描述: %s\n", truncateString(desc, 60))
		}
	}
}

// ============================================================================
// 场景 3：计划优化与执行
// ============================================================================

func demonstratePlanOptimization(ctx context.Context, llmClient llm.Client, memMgr interfaces.MemoryManager) {
	fmt.Println("\n场景描述: 使用 PlanningAgent 创建、优化并执行计划")
	fmt.Println()

	// 创建规划器
	planner := planning.NewSmartPlanner(
		llmClient,
		memMgr,
		planning.WithOptimizer(&planning.DefaultOptimizer{}),
	)

	// 创建规划 Agent（executor 为 nil，仅演示计划创建）
	planningAgent := planning.NewPlanningAgent(planner, nil)

	fmt.Println("1. 创建 PlanningAgent")
	fmt.Println("────────────────────────────────────────")
	fmt.Printf("  Agent 名称: %s\n", planningAgent.Name())

	// 创建并执行计划
	fmt.Println("\n2. 创建并优化计划")
	fmt.Println("────────────────────────────────────────")

	goal := "构建一个 RESTful API 服务"

	startTime := time.Now()
	output, err := planningAgent.Execute(ctx, &core.AgentInput{
		Task: goal,
		Context: map[string]interface{}{
			"goal": goal,
			"constraints": planning.PlanConstraints{
				MaxSteps:    8,
				MaxDuration: 20 * time.Minute,
			},
		},
	})

	if err != nil {
		fmt.Printf("  ✗ 执行失败: %v\n", err)
		return
	}

	fmt.Printf("  ✓ 计划创建成功 (耗时: %v)\n", time.Since(startTime))

	// 显示计划
	if plan, ok := output.Result.(*planning.Plan); ok {
		printPlanSummary(plan)

		// 显示执行元数据
		fmt.Println("\n3. 计划元数据")
		fmt.Println("────────────────────────────────────────")
		if planID, ok := output.Metadata["plan_id"].(string); ok {
			fmt.Printf("  计划 ID: %s\n", planID)
		}
		if totalSteps, ok := output.Metadata["total_steps"].(int); ok {
			fmt.Printf("  总步骤数: %d\n", totalSteps)
		}
	}

	// 使用策略 Agent
	fmt.Println("\n4. 应用规划策略")
	fmt.Println("────────────────────────────────────────")

	strategyAgent := planning.NewStrategyAgent()
	fmt.Printf("  策略 Agent: %s\n", strategyAgent.Name())
	fmt.Println("  可用策略:")
	fmt.Println("  - decomposition: 任务分解策略")
	fmt.Println("  - backward_chaining: 逆向推理策略")
	fmt.Println("  - hierarchical: 层次化规划策略")

	// 使用验证 Agent
	fmt.Println("\n5. 计划验证")
	fmt.Println("────────────────────────────────────────")

	validationAgent := planning.NewValidationAgent()
	fmt.Printf("  验证 Agent: %s\n", validationAgent.Name())
	fmt.Println("  内置验证器:")
	fmt.Println("  - DependencyValidator: 检查步骤依赖关系")
	fmt.Println("  - ResourceValidator: 检查资源约束")
	fmt.Println("  - TimeValidator: 检查时间约束")
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
			llm.WithMaxTokens(1000),
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
			llm.WithMaxTokens(1000),
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
			llm.WithMaxTokens(1000),
		)
		if err == nil {
			return client
		}
	}

	return nil
}

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
			fmt.Printf("    %d. [%s] %s\n", i+1, step.Type, step.Name)
			if step.Description != "" {
				fmt.Printf("       %s\n", truncateString(step.Description, 50))
			}
		}
	}
}

func truncateString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// runMockDemo 运行模拟演示
func runMockDemo() {
	fmt.Println()
	fmt.Println("【模拟演示】Planning 包的核心功能")
	fmt.Println("════════════════════════════════════════════════════════════════")

	fmt.Println("\n1. SmartPlanner - 智能规划器")
	fmt.Println("────────────────────────────────────────")
	fmt.Print(`
   // 创建智能规划器
   planner := planning.NewSmartPlanner(
       llmClient,
       memoryManager,
       planning.WithMaxDepth(3),
       planning.WithTimeout(5*time.Minute),
       planning.WithOptimizer(&planning.DefaultOptimizer{}),
   )

   // 创建计划
   plan, err := planner.CreatePlan(ctx, goal, planning.PlanConstraints{
       MaxSteps:    10,
       MaxDuration: 30 * time.Minute,
   })

   // 验证计划
   valid, issues, err := planner.ValidatePlan(ctx, plan)

   // 优化计划
   optimizedPlan, err := planner.OptimizePlan(ctx, plan)
`)

	fmt.Println("\n2. Plan - 计划结构")
	fmt.Println("────────────────────────────────────────")
	fmt.Print(`
   type Plan struct {
       ID           string              // 计划 ID
       Goal         string              // 目标
       Strategy     string              // 策略
       Steps        []*Step             // 步骤列表
       Dependencies map[string][]string // 依赖关系
       Status       PlanStatus          // 状态
       Metrics      *PlanMetrics        // 执行指标
   }

   type Step struct {
       ID                string           // 步骤 ID
       Name              string           // 名称
       Description       string           // 描述
       Type              StepType         // 类型
       Agent             string           // 执行 Agent
       Parameters        map[string]any   // 参数
       Priority          int              // 优先级
       EstimatedDuration time.Duration    // 预计时长
       Status            StepStatus       // 状态
       Result            *StepResult      // 结果
   }
`)

	fmt.Println("\n3. Planning Agents")
	fmt.Println("────────────────────────────────────────")
	fmt.Print(`
   // PlanningAgent - 创建和执行计划
   planningAgent := planning.NewPlanningAgent(planner, executor)

   // TaskDecompositionAgent - 任务分解
   decomposer := planning.NewTaskDecompositionAgent(planner)

   // StrategyAgent - 策略选择和应用
   strategyAgent := planning.NewStrategyAgent()

   // OptimizationAgent - 计划优化
   optimizer := planning.NewOptimizationAgent(nil) // 使用默认优化器

   // ValidationAgent - 计划验证
   validator := planning.NewValidationAgent()
`)

	fmt.Println("\n4. 规划策略")
	fmt.Println("────────────────────────────────────────")
	fmt.Print(`
   内置策略:
   - decomposition:     任务分解策略，将复杂任务拆分为简单子任务
   - backward_chaining: 逆向推理策略，从目标倒推所需步骤
   - hierarchical:      层次化策略，按层级组织任务

   自定义策略:
   type PlanStrategy interface {
       Apply(ctx context.Context, plan *Plan, constraints PlanConstraints) (*Plan, error)
   }
`)

	fmt.Println("\n5. 计划验证器")
	fmt.Println("────────────────────────────────────────")
	fmt.Print(`
   内置验证器:
   - DependencyValidator: 验证步骤依赖关系是否合理
   - ResourceValidator:   验证资源约束是否满足
   - TimeValidator:       验证时间约束是否可行

   自定义验证器:
   type PlanValidator interface {
       Validate(ctx context.Context, plan *Plan) (bool, []string, error)
   }
`)

	fmt.Println("\n6. 计划执行器")
	fmt.Println("────────────────────────────────────────")
	fmt.Print(`
   // 顺序执行器
   executor := planning.NewSequentialExecutor(agentRegistry)

   // 执行计划
   result, err := executor.Execute(ctx, plan)

   // 执行结果
   type ExecutionResult struct {
       PlanID        string
       Success       bool
       StepResults   map[string]*StepResult
       TotalDuration time.Duration
       Metrics       *PlanMetrics
   }
`)

	fmt.Println("\n配置步骤:")
	fmt.Println("   1. 设置环境变量: export DEEPSEEK_API_KEY=your-key")
	fmt.Println("   2. 运行示例: go run main.go")
}
