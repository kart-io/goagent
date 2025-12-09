// Package main 演示不同的多智能体协作类型
// 本示例展示五种协作模式：并行、顺序、分层、共识、管道
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/multiagent"
	loggercore "github.com/kart-io/logger/core"
)

// simpleLogger 简单的日志实现，满足 loggercore.Logger 接口
type simpleLogger struct{}

func (l *simpleLogger) Debug(args ...interface{}) { fmt.Print("[DEBUG] "); fmt.Println(args...) }
func (l *simpleLogger) Info(args ...interface{})  { fmt.Print("[INFO] "); fmt.Println(args...) }
func (l *simpleLogger) Warn(args ...interface{})  { fmt.Print("[WARN] "); fmt.Println(args...) }
func (l *simpleLogger) Error(args ...interface{}) { fmt.Print("[ERROR] "); fmt.Println(args...) }
func (l *simpleLogger) Fatal(args ...interface{}) {
	fmt.Print("[FATAL] ")
	fmt.Println(args...)
	os.Exit(1)
}

func (l *simpleLogger) Debugf(template string, args ...interface{}) {
	fmt.Printf("[DEBUG] "+template+"\n", args...)
}

func (l *simpleLogger) Infof(template string, args ...interface{}) {
	fmt.Printf("[INFO] "+template+"\n", args...)
}

func (l *simpleLogger) Warnf(template string, args ...interface{}) {
	fmt.Printf("[WARN] "+template+"\n", args...)
}

func (l *simpleLogger) Errorf(template string, args ...interface{}) {
	fmt.Printf("[ERROR] "+template+"\n", args...)
}

func (l *simpleLogger) Fatalf(template string, args ...interface{}) {
	fmt.Printf("[FATAL] "+template+"\n", args...)
	os.Exit(1)
}

func (l *simpleLogger) Debugw(msg string, keysAndValues ...interface{}) {
	fmt.Printf("[DEBUG] %s %v\n", msg, keysAndValues)
}

func (l *simpleLogger) Infow(msg string, keysAndValues ...interface{}) {
	fmt.Printf("[INFO] %s %v\n", msg, keysAndValues)
}

func (l *simpleLogger) Warnw(msg string, keysAndValues ...interface{}) {
	fmt.Printf("[WARN] %s %v\n", msg, keysAndValues)
}

func (l *simpleLogger) Errorw(msg string, keysAndValues ...interface{}) {
	fmt.Printf("[ERROR] %s %v\n", msg, keysAndValues)
}

func (l *simpleLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	fmt.Printf("[FATAL] %s %v\n", msg, keysAndValues)
	os.Exit(1)
}
func (l *simpleLogger) With(keyValues ...interface{}) loggercore.Logger { return l }
func (l *simpleLogger) WithCtx(ctx context.Context, keyValues ...interface{}) loggercore.Logger {
	return l
}
func (l *simpleLogger) WithCallerSkip(skip int) loggercore.Logger { return l }
func (l *simpleLogger) SetLevel(level loggercore.Level)           {}
func (l *simpleLogger) Flush() error                              { return nil }

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          多智能体协作类型示例                                  ║")
	fmt.Println("║   展示五种协作模式：并行、顺序、分层、共识、管道               ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger, multiagent.WithMaxAgents(20))

	// 创建多个专业化 Agent
	setupAgents(system)

	// 依次演示各种协作类型
	fmt.Println()
	demonstrateParallelCollaboration(ctx, system)
	fmt.Println()
	demonstrateSequentialCollaboration(ctx, system)
	fmt.Println()
	demonstrateHierarchicalCollaboration(ctx, system)
	fmt.Println()
	demonstrateConsensusCollaboration(ctx, system)
	fmt.Println()
	demonstratePipelineCollaboration(ctx, system)

	// 总结
	printSummary()
}

// setupAgents 创建并注册所有需要的 Agent
func setupAgents(system *multiagent.MultiAgentSystem) {
	fmt.Println("【准备工作】注册协作 Agent")
	fmt.Println("────────────────────────────────────────")

	agents := []struct {
		id          string
		description string
		role        multiagent.Role
	}{
		{"data-collector", "数据采集 Agent", multiagent.RoleWorker},
		{"data-processor", "数据处理 Agent", multiagent.RoleWorker},
		{"data-analyzer", "数据分析 Agent", multiagent.RoleSpecialist},
		{"report-generator", "报告生成 Agent", multiagent.RoleWorker},
		{"quality-checker", "质量检查 Agent", multiagent.RoleValidator},
		{"coordinator", "协调者 Agent", multiagent.RoleCoordinator},
		{"leader", "领导者 Agent", multiagent.RoleLeader},
		{"observer", "观察者 Agent", multiagent.RoleObserver},
	}

	for _, a := range agents {
		agent := multiagent.NewBaseCollaborativeAgent(a.id, a.description, a.role, system)
		if err := system.RegisterAgent(a.id, agent); err != nil {
			fmt.Printf("✗ 注册 Agent %s 失败: %v\n", a.id, err)
			continue
		}
		fmt.Printf("✓ 注册 Agent: %-20s 角色: %s\n", a.id, a.role)
	}
}

// demonstrateParallelCollaboration 演示并行协作
func demonstrateParallelCollaboration(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("╭────────────────────────────────────────╮")
	fmt.Println("│  协作类型 1: Parallel（并行协作）      │")
	fmt.Println("╰────────────────────────────────────────╯")
	fmt.Println()
	fmt.Println("场景: 多个 Agent 同时处理独立的数据分片")
	fmt.Println("特点: 所有 Agent 并行执行，最后合并结果")
	fmt.Println()

	task := &multiagent.CollaborativeTask{
		ID:          "parallel-task-001",
		Name:        "并行数据处理",
		Description: "多个 Agent 并行处理数据分片",
		Type:        multiagent.CollaborationTypeParallel,
		Input: map[string]interface{}{
			"data_shards":     []string{"shard-A", "shard-B", "shard-C"},
			"operation":       "aggregate",
			"total_records":   10000,
			"shard_per_agent": 3334,
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Printf("任务: %s\n", task.Name)
	fmt.Printf("输入: %v\n", task.Input)
	fmt.Println()

	startTime := time.Now()
	result, err := system.ExecuteTask(ctx, task)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("✗ 执行失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 执行状态: %s\n", result.Status)
	fmt.Printf("✓ 总耗时: %v\n", duration)
	fmt.Printf("✓ 参与 Agent 数: %d\n", len(result.Assignments))

	fmt.Println("\n各 Agent 执行结果:")
	for agentID, assignment := range result.Assignments {
		fmt.Printf("  - %s: 状态=%s, 耗时=%v\n",
			agentID, assignment.Status, assignment.EndTime.Sub(assignment.StartTime))
	}
}

// demonstrateSequentialCollaboration 演示顺序协作
func demonstrateSequentialCollaboration(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("╭────────────────────────────────────────╮")
	fmt.Println("│  协作类型 2: Sequential（顺序协作）    │")
	fmt.Println("╰────────────────────────────────────────╯")
	fmt.Println()
	fmt.Println("场景: Agent 按顺序依次处理，前一个的输出作为后一个的输入")
	fmt.Println("特点: 链式处理，保证执行顺序")
	fmt.Println()

	task := &multiagent.CollaborativeTask{
		ID:          "sequential-task-001",
		Name:        "顺序数据流水线",
		Description: "数据依次经过采集、处理、分析、生成报告",
		Type:        multiagent.CollaborationTypeSequential,
		Input: map[string]interface{}{
			"pipeline_stages": []string{"collect", "process", "analyze", "report"},
			"data_source":     "sensor_network",
			"output_format":   "json",
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Printf("任务: %s\n", task.Name)
	fmt.Printf("流水线阶段: %v\n", task.Input.(map[string]interface{})["pipeline_stages"])
	fmt.Println()

	startTime := time.Now()
	result, err := system.ExecuteTask(ctx, task)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("✗ 执行失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 执行状态: %s\n", result.Status)
	fmt.Printf("✓ 总耗时: %v\n", duration)

	fmt.Println("\n执行顺序:")
	i := 1
	for agentID, assignment := range result.Assignments {
		fmt.Printf("  %d. %s -> 输出: %v\n", i, agentID, assignment.Result)
		i++
	}
}

// demonstrateHierarchicalCollaboration 演示分层协作
func demonstrateHierarchicalCollaboration(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("╭────────────────────────────────────────╮")
	fmt.Println("│  协作类型 3: Hierarchical（分层协作）  │")
	fmt.Println("╰────────────────────────────────────────╯")
	fmt.Println()
	fmt.Println("场景: 领导者分配任务，工作者执行，验证者检验结果")
	fmt.Println("特点: 层级分明，责任清晰")
	fmt.Println()

	task := &multiagent.CollaborativeTask{
		ID:          "hierarchical-task-001",
		Name:        "分层项目管理",
		Description: "领导者规划任务，工作者执行，验证者审核",
		Type:        multiagent.CollaborationTypeHierarchical,
		Input: map[string]interface{}{
			"project":   "数据分析项目",
			"phases":    []string{"planning", "execution", "validation"},
			"team_size": 5,
			"deadline":  "2024-12-31",
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Printf("任务: %s\n", task.Name)
	fmt.Printf("项目阶段: %v\n", task.Input.(map[string]interface{})["phases"])
	fmt.Println()

	startTime := time.Now()
	result, err := system.ExecuteTask(ctx, task)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("✗ 执行失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 执行状态: %s\n", result.Status)
	fmt.Printf("✓ 总耗时: %v\n", duration)

	fmt.Println("\n层级执行结果:")
	for agentID, assignment := range result.Assignments {
		fmt.Printf("  - [%s] %s: %v\n", assignment.Role, agentID, assignment.Result)
	}
}

// demonstrateConsensusCollaboration 演示共识协作
func demonstrateConsensusCollaboration(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("╭────────────────────────────────────────╮")
	fmt.Println("│  协作类型 4: Consensus（共识协作）     │")
	fmt.Println("╰────────────────────────────────────────╯")
	fmt.Println()
	fmt.Println("场景: 多个 Agent 对某个决策进行投票，达成共识")
	fmt.Println("特点: 民主决策，少数服从多数")
	fmt.Println()

	task := &multiagent.CollaborativeTask{
		ID:          "consensus-task-001",
		Name:        "共识决策",
		Description: "多个 Agent 投票决定最佳方案",
		Type:        multiagent.CollaborationTypeConsensus,
		Input: map[string]interface{}{
			"proposal": "采用新的数据处理架构",
			"options":  []string{"方案A: 微服务", "方案B: 单体", "方案C: 混合"},
			"quorum":   0.6, // 需要 60% 以上同意
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Printf("任务: %s\n", task.Name)
	fmt.Printf("提案: %v\n", task.Input.(map[string]interface{})["proposal"])
	fmt.Printf("选项: %v\n", task.Input.(map[string]interface{})["options"])
	fmt.Printf("通过阈值: %.0f%%\n", task.Input.(map[string]interface{})["quorum"].(float64)*100)
	fmt.Println()

	startTime := time.Now()
	result, err := system.ExecuteTask(ctx, task)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("✗ 执行失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 执行状态: %s\n", result.Status)
	fmt.Printf("✓ 总耗时: %v\n", duration)

	fmt.Println("\n投票结果:")
	yesCount := 0
	totalCount := len(result.Assignments)
	for agentID, assignment := range result.Assignments {
		if assignment.Result != nil {
			yesCount++
			fmt.Printf("  - %s: 同意\n", agentID)
		} else {
			fmt.Printf("  - %s: 反对\n", agentID)
		}
	}
	fmt.Printf("\n结论: %d/%d 同意 (%.0f%%)\n", yesCount, totalCount, float64(yesCount)/float64(totalCount)*100)
}

// demonstratePipelineCollaboration 演示管道协作
func demonstratePipelineCollaboration(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("╭────────────────────────────────────────╮")
	fmt.Println("│  协作类型 5: Pipeline（管道协作）      │")
	fmt.Println("╰────────────────────────────────────────╯")
	fmt.Println()
	fmt.Println("场景: 数据流经多个处理阶段，每个阶段由专门的 Agent 处理")
	fmt.Println("特点: 流式处理，支持并行管道")
	fmt.Println()

	// Pipeline 任务需要 []interface{} 类型的 stages 数组
	pipelineStages := []interface{}{
		map[string]interface{}{
			"stage":     "extract",
			"source":    "database_A",
			"operation": "SELECT * FROM users",
		},
		map[string]interface{}{
			"stage":      "transform",
			"operations": []string{"clean", "normalize", "enrich"},
		},
		map[string]interface{}{
			"stage":       "load",
			"destination": "data_warehouse",
			"batch_size":  1000,
		},
	}

	task := &multiagent.CollaborativeTask{
		ID:          "pipeline-task-001",
		Name:        "ETL 数据管道",
		Description: "Extract -> Transform -> Load 数据处理管道",
		Type:        multiagent.CollaborationTypePipeline,
		Input:       pipelineStages,
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Printf("任务: %s\n", task.Name)
	fmt.Printf("管道阶段数: %d\n", len(pipelineStages))
	fmt.Println("阶段详情:")
	for i, stage := range pipelineStages {
		if stageMap, ok := stage.(map[string]interface{}); ok {
			fmt.Printf("  %d. %v\n", i+1, stageMap["stage"])
		}
	}
	fmt.Println()

	startTime := time.Now()
	result, err := system.ExecuteTask(ctx, task)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("✗ 执行失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 执行状态: %s\n", result.Status)
	fmt.Printf("✓ 总耗时: %v\n", duration)

	fmt.Println("\n管道阶段执行结果:")
	stage := 1
	for agentID, assignment := range result.Assignments {
		fmt.Printf("  阶段 %d [%s]: %s -> %v\n", stage, assignment.Role, agentID, assignment.Result)
		stage++
	}
}

// printSummary 打印总结
func printSummary() {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("五种协作类型总结:")
	fmt.Println()
	fmt.Println("┌──────────────┬────────────────────────────────────────────────┐")
	fmt.Println("│ 协作类型     │ 适用场景                                       │")
	fmt.Println("├──────────────┼────────────────────────────────────────────────┤")
	fmt.Println("│ Parallel     │ 独立任务并行处理，如数据分片处理               │")
	fmt.Println("│ Sequential   │ 有依赖的任务链式处理，如数据流水线             │")
	fmt.Println("│ Hierarchical │ 层级分明的项目管理，如领导-执行-验证模式       │")
	fmt.Println("│ Consensus    │ 需要多方投票决策，如方案选择、审批流程         │")
	fmt.Println("│ Pipeline     │ 流式数据处理，如 ETL、日志处理                 │")
	fmt.Println("└──────────────┴────────────────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("更多示例请参考:")
	fmt.Println("  - 03-team-management: 团队管理示例")
	fmt.Println("  - 04-specialized-agents: 专业化 Agent 示例")
}
