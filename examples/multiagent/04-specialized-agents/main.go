// Package main 演示专业化 Agent 的使用
// 本示例展示 SpecializedAgent 和 NegotiatingAgent 的高级用法
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
	fmt.Println("║          专业化 Agent 示例                                     ║")
	fmt.Println("║   展示 SpecializedAgent 和 NegotiatingAgent 的高级用法         ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger, multiagent.WithMaxAgents(20))

	// 1. 演示专业化 Agent
	fmt.Println("【演示 1】专业化 Agent (SpecializedAgent)")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateSpecializedAgents(ctx, system)
	fmt.Println()

	// 2. 演示谈判 Agent
	fmt.Println("【演示 2】谈判 Agent (NegotiatingAgent)")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateNegotiatingAgents(ctx, system)
	fmt.Println()

	// 3. 演示混合协作
	fmt.Println("【演示 3】专业化 Agent 混合协作")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateMixedCollaboration(ctx, system)
	fmt.Println()

	// 4. 演示 Agent 投票机制
	fmt.Println("【演示 4】Agent 投票决策")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateVoting(ctx, system)

	// 总结
	printSummary()
}

// demonstrateSpecializedAgents 演示专业化 Agent
func demonstrateSpecializedAgents(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println()
	fmt.Println("创建多个领域专家 Agent...")
	fmt.Println()

	// 创建不同专业领域的 Agent
	specializations := []struct {
		id             string
		specialization string
	}{
		{"security-expert", "安全分析"},
		{"performance-expert", "性能优化"},
		{"architecture-expert", "架构设计"},
		{"ml-expert", "机器学习"},
		{"database-expert", "数据库优化"},
	}

	specialists := make(map[string]*multiagent.SpecializedAgent)

	for _, spec := range specializations {
		agent := multiagent.NewSpecializedAgent(spec.id, spec.specialization, system)
		if err := system.RegisterAgent(spec.id, agent); err != nil {
			fmt.Printf("✗ 注册专家 %s 失败: %v\n", spec.id, err)
			continue
		}
		specialists[spec.id] = agent
		fmt.Printf("✓ 创建专家: %-20s 专业: %s\n", spec.id, spec.specialization)
	}

	fmt.Println()
	fmt.Println("执行专家协作任务...")
	fmt.Println()

	// 创建需要多个专家协作的任务
	task := &multiagent.CollaborativeTask{
		ID:          "expert-review-001",
		Name:        "系统架构评审",
		Description: "多位专家对新系统架构进行全面评审",
		Type:        multiagent.CollaborationTypeParallel,
		Input: map[string]interface{}{
			"system_name": "智能推荐引擎",
			"components":  []string{"API Gateway", "推荐服务", "用户画像", "特征存储"},
			"review_aspects": []string{
				"安全性评估",
				"性能瓶颈分析",
				"架构合理性",
				"可扩展性",
				"数据存储方案",
			},
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Printf("任务: %s\n", task.Name)
	fmt.Printf("描述: %s\n", task.Description)
	fmt.Println()

	result, err := system.ExecuteTask(ctx, task)
	if err != nil {
		fmt.Printf("✗ 任务执行失败: %v\n", err)
		return
	}

	fmt.Println("专家评审结果:")
	fmt.Println("────────────────────────────────────────")
	for agentID, assignment := range result.Assignments {
		fmt.Printf("\n【%s】\n", agentID)
		if resultMap, ok := assignment.Result.(map[string]interface{}); ok {
			if spec, exists := resultMap["specialization"]; exists {
				fmt.Printf("  专业领域: %v\n", spec)
			}
			if analysis, exists := resultMap["analysis"]; exists {
				fmt.Printf("  分析结果: %v\n", analysis)
			}
			if confidence, exists := resultMap["confidence"]; exists {
				fmt.Printf("  置信度: %.0f%%\n", confidence.(float64)*100)
			}
		}
	}

	fmt.Println()
	fmt.Printf("✓ 评审完成，共 %d 位专家参与\n", len(result.Assignments))
}

// demonstrateNegotiatingAgents 演示谈判 Agent
func demonstrateNegotiatingAgents(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println()
	fmt.Println("场景: 资源分配谈判")
	fmt.Println("多个 Agent 需要协商服务器资源分配方案")
	fmt.Println()

	// 创建谈判 Agent
	negotiators := []string{"negotiator-A", "negotiator-B", "negotiator-C"}

	for _, id := range negotiators {
		agent := multiagent.NewNegotiatingAgent(id, system)
		if err := system.RegisterAgent(id, agent); err != nil {
			fmt.Printf("✗ 注册谈判 Agent %s 失败: %v\n", id, err)
			continue
		}
		fmt.Printf("✓ 创建谈判 Agent: %s\n", id)
	}

	fmt.Println()
	fmt.Println("谈判主题: 服务器资源分配")
	fmt.Println("────────────────────────────────────────")

	// 模拟谈判场景
	proposals := []map[string]interface{}{
		{
			"proposal_id": "P1",
			"allocation": map[string]int{
				"negotiator-A": 40,
				"negotiator-B": 35,
				"negotiator-C": 25,
			},
			"total_cpu_cores": 100,
		},
		{
			"proposal_id": "P2",
			"allocation": map[string]int{
				"negotiator-A": 33,
				"negotiator-B": 33,
				"negotiator-C": 34,
			},
			"total_cpu_cores": 100,
		},
		{
			"proposal_id": "P3",
			"allocation": map[string]int{
				"negotiator-A": 50,
				"negotiator-B": 30,
				"negotiator-C": 20,
			},
			"total_cpu_cores": 100,
		},
	}

	fmt.Println("\n提出的分配方案:")
	for _, proposal := range proposals {
		fmt.Printf("\n方案 %s:\n", proposal["proposal_id"])
		allocation := proposal["allocation"].(map[string]int)
		for agent, cores := range allocation {
			fmt.Printf("  %s: %d 核 (%.0f%%)\n", agent, cores, float64(cores))
		}
	}

	// 使用共识协作进行投票
	fmt.Println("\n开始投票决策...")

	task := &multiagent.CollaborativeTask{
		ID:          "negotiation-001",
		Name:        "资源分配谈判",
		Description: "通过投票确定最终资源分配方案",
		Type:        multiagent.CollaborationTypeConsensus,
		Input: map[string]interface{}{
			"proposals": proposals,
			"quorum":    0.6,
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	result, err := system.ExecuteTask(ctx, task)
	if err != nil {
		fmt.Printf("✗ 谈判失败: %v\n", err)
		return
	}

	fmt.Println("\n投票结果:")
	for agentID, assignment := range result.Assignments {
		fmt.Printf("  %s: 状态=%s\n", agentID, assignment.Status)
	}

	fmt.Println()
	fmt.Printf("✓ 谈判完成，状态: %s\n", result.Status)
}

// demonstrateMixedCollaboration 演示混合协作
func demonstrateMixedCollaboration(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println()
	fmt.Println("场景: 复杂项目需要多种专业 Agent 协作")
	fmt.Println()

	// 创建基础协作 Agent
	baseAgents := []struct {
		id   string
		role multiagent.Role
	}{
		{"project-leader", multiagent.RoleLeader},
		{"coordinator", multiagent.RoleCoordinator},
		{"validator", multiagent.RoleValidator},
		{"observer", multiagent.RoleObserver},
	}

	for _, cfg := range baseAgents {
		agent := multiagent.NewBaseCollaborativeAgent(cfg.id, "", cfg.role, system)
		if err := system.RegisterAgent(cfg.id, agent); err != nil {
			// Agent 可能已存在
			continue
		}
	}

	fmt.Println("执行分层协作任务...")
	fmt.Println()

	task := &multiagent.CollaborativeTask{
		ID:          "mixed-collab-001",
		Name:        "智能客服系统开发",
		Description: "多种角色 Agent 分层协作开发系统",
		Type:        multiagent.CollaborationTypeHierarchical,
		Input: map[string]interface{}{
			"project":    "智能客服系统",
			"milestones": []string{"需求分析", "架构设计", "核心开发", "测试验收"},
			"budget":     1000000,
			"deadline":   "2024-06-30",
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Printf("项目: %s\n", task.Input.(map[string]interface{})["project"])
	fmt.Printf("里程碑: %v\n", task.Input.(map[string]interface{})["milestones"])
	fmt.Println()

	startTime := time.Now()
	result, err := system.ExecuteTask(ctx, task)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("✗ 执行失败: %v\n", err)
		return
	}

	fmt.Println("执行结果:")
	fmt.Println("────────────────────────────────────────")
	fmt.Printf("状态: %s\n", result.Status)
	fmt.Printf("耗时: %v\n", duration)
	fmt.Printf("参与 Agent: %d\n", len(result.Assignments))

	fmt.Println("\n各角色执行情况:")
	for agentID, assignment := range result.Assignments {
		fmt.Printf("  [%s] %s: %s\n", assignment.Role, agentID, assignment.Status)
	}

	fmt.Println()
	fmt.Println("✓ 混合协作任务完成")
}

// demonstrateVoting 演示投票机制
func demonstrateVoting(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println()
	fmt.Println("场景: 技术方案投票决策")
	fmt.Println()

	// 创建投票 Agent（使用已注册的 Agent）
	voters := []string{"security-expert", "performance-expert", "architecture-expert"}

	proposal := map[string]interface{}{
		"title":       "采用微服务架构",
		"description": "将单体应用拆分为微服务架构",
		"pros":        []string{"可扩展性强", "独立部署", "技术栈灵活"},
		"cons":        []string{"运维复杂度高", "网络开销大", "数据一致性挑战"},
	}

	fmt.Printf("提案: %s\n", proposal["title"])
	fmt.Printf("描述: %s\n", proposal["description"])
	fmt.Println()
	fmt.Println("优点:")
	for _, pro := range proposal["pros"].([]string) {
		fmt.Printf("  + %s\n", pro)
	}
	fmt.Println("缺点:")
	for _, con := range proposal["cons"].([]string) {
		fmt.Printf("  - %s\n", con)
	}
	fmt.Println()

	fmt.Println("开始投票...")
	fmt.Println("────────────────────────────────────────")

	// 模拟各 Agent 投票
	yesCount := 0
	totalCount := len(voters)

	for _, voterID := range voters {
		// 创建一个简单的 Agent 用于投票演示
		agent := multiagent.NewBaseCollaborativeAgent(voterID+"-voter", "", multiagent.RoleWorker, nil)

		vote, err := agent.Vote(ctx, proposal)
		if err != nil {
			fmt.Printf("  %s: 投票失败 - %v\n", voterID, err)
			continue
		}

		voteStr := "反对"
		if vote {
			voteStr = "同意"
			yesCount++
		}
		fmt.Printf("  %s: %s\n", voterID, voteStr)
	}

	fmt.Println()
	percentage := float64(yesCount) / float64(totalCount) * 100
	fmt.Printf("投票结果: %d/%d 同意 (%.1f%%)\n", yesCount, totalCount, percentage)

	if percentage >= 60 {
		fmt.Println("✓ 提案通过！")
	} else {
		fmt.Println("✗ 提案未通过")
	}
}

// printSummary 打印总结
func printSummary() {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("专业化 Agent 功能总结:")
	fmt.Println()
	fmt.Println("┌────────────────────┬──────────────────────────────────────────┐")
	fmt.Println("│ Agent 类型         │ 特点与用途                               │")
	fmt.Println("├────────────────────┼──────────────────────────────────────────┤")
	fmt.Println("│ BaseCollaborative  │ 基础协作 Agent，支持消息通信、任务执行   │")
	fmt.Println("│ SpecializedAgent   │ 领域专家 Agent，提供专业化分析能力       │")
	fmt.Println("│ NegotiatingAgent   │ 谈判 Agent，支持多轮谈判和协商           │")
	fmt.Println("└────────────────────┴──────────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("核心能力:")
	fmt.Println("  ✓ 专业领域分析 - 基于专业领域提供专家意见")
	fmt.Println("  ✓ 多轮谈判 - 支持提案、反馈、修改、达成共识")
	fmt.Println("  ✓ 投票机制 - 民主决策，少数服从多数")
	fmt.Println("  ✓ 角色适配 - 根据角色自动调整行为策略")
	fmt.Println()
	fmt.Println("更多信息请参考项目文档: docs/architecture/ARCHITECTURE.md")
}
