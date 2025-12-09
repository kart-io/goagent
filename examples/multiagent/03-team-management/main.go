// Package main 演示多智能体团队管理功能
// 本示例展示如何创建团队、分配角色、管理团队成员
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
	fmt.Println("║          多智能体团队管理示例                                  ║")
	fmt.Println("║   展示如何创建团队、分配角色、管理团队成员                     ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger, multiagent.WithMaxAgents(30))

	// 1. 创建所有 Agent
	fmt.Println("【步骤 1】创建 Agent 池")
	fmt.Println("────────────────────────────────────────")
	agents := createAgentPool(system)
	fmt.Printf("✓ 共创建 %d 个 Agent\n", len(agents))
	fmt.Println()

	// 2. 创建研发团队
	fmt.Println("【步骤 2】创建研发团队")
	fmt.Println("────────────────────────────────────────")
	devTeam := createDevelopmentTeam(system)
	fmt.Println()

	// 3. 创建数据团队
	fmt.Println("【步骤 3】创建数据分析团队")
	fmt.Println("────────────────────────────────────────")
	dataTeam := createDataTeam(system)
	fmt.Println()

	// 4. 创建运维团队
	fmt.Println("【步骤 4】创建运维团队")
	fmt.Println("────────────────────────────────────────")
	opsTeam := createOpsTeam(system)
	fmt.Println()

	// 5. 团队协作示例：跨团队项目
	fmt.Println("【步骤 5】跨团队协作项目")
	fmt.Println("────────────────────────────────────────")
	executeProjectWithTeams(ctx, system, devTeam, dataTeam, opsTeam)
	fmt.Println()

	// 6. 角色动态调整
	fmt.Println("【步骤 6】角色动态调整")
	fmt.Println("────────────────────────────────────────")
	demonstrateRoleChange(system, agents)
	fmt.Println()

	// 总结
	printTeamSummary(devTeam, dataTeam, opsTeam)
}

// createAgentPool 创建 Agent 池
func createAgentPool(system *multiagent.MultiAgentSystem) map[string]multiagent.CollaborativeAgent {
	agents := make(map[string]multiagent.CollaborativeAgent)

	agentConfigs := []struct {
		id          string
		description string
		role        multiagent.Role
	}{
		// 研发团队
		{"dev-lead", "研发负责人", multiagent.RoleLeader},
		{"frontend-dev-1", "前端开发工程师 1", multiagent.RoleWorker},
		{"frontend-dev-2", "前端开发工程师 2", multiagent.RoleWorker},
		{"backend-dev-1", "后端开发工程师 1", multiagent.RoleWorker},
		{"backend-dev-2", "后端开发工程师 2", multiagent.RoleWorker},
		{"qa-engineer", "测试工程师", multiagent.RoleValidator},

		// 数据团队
		{"data-lead", "数据负责人", multiagent.RoleLeader},
		{"data-engineer-1", "数据工程师 1", multiagent.RoleWorker},
		{"data-engineer-2", "数据工程师 2", multiagent.RoleWorker},
		{"data-analyst", "数据分析师", multiagent.RoleSpecialist},
		{"ml-engineer", "机器学习工程师", multiagent.RoleSpecialist},

		// 运维团队
		{"ops-lead", "运维负责人", multiagent.RoleLeader},
		{"devops-1", "DevOps 工程师 1", multiagent.RoleWorker},
		{"devops-2", "DevOps 工程师 2", multiagent.RoleWorker},
		{"sre-1", "SRE 工程师", multiagent.RoleSpecialist},
		{"monitor", "监控专员", multiagent.RoleObserver},

		// 协调者
		{"project-coordinator", "项目协调者", multiagent.RoleCoordinator},
	}

	for _, cfg := range agentConfigs {
		agent := multiagent.NewBaseCollaborativeAgent(cfg.id, cfg.description, cfg.role, system)
		if err := system.RegisterAgent(cfg.id, agent); err != nil {
			fmt.Printf("✗ 注册 Agent %s 失败: %v\n", cfg.id, err)
			continue
		}
		agents[cfg.id] = agent
		fmt.Printf("  ✓ %s (%s)\n", cfg.id, cfg.role)
	}

	return agents
}

// createDevelopmentTeam 创建研发团队
func createDevelopmentTeam(system *multiagent.MultiAgentSystem) *multiagent.Team {
	team := &multiagent.Team{
		ID:      "team-dev",
		Name:    "研发团队",
		Leader:  "dev-lead",
		Members: []string{"dev-lead", "frontend-dev-1", "frontend-dev-2", "backend-dev-1", "backend-dev-2", "qa-engineer"},
		Purpose: "负责产品功能开发和质量保证",
		Capabilities: []string{
			"前端开发",
			"后端开发",
			"API 设计",
			"单元测试",
			"集成测试",
		},
		Metadata: map[string]interface{}{
			"tech_stack":    []string{"React", "Go", "PostgreSQL"},
			"sprint_length": 2,
		},
	}

	if err := system.CreateTeam(team); err != nil {
		fmt.Printf("✗ 创建团队失败: %v\n", err)
		return nil
	}

	printTeamInfo(team)
	return team
}

// createDataTeam 创建数据团队
func createDataTeam(system *multiagent.MultiAgentSystem) *multiagent.Team {
	team := &multiagent.Team{
		ID:      "team-data",
		Name:    "数据分析团队",
		Leader:  "data-lead",
		Members: []string{"data-lead", "data-engineer-1", "data-engineer-2", "data-analyst", "ml-engineer"},
		Purpose: "负责数据处理、分析和机器学习",
		Capabilities: []string{
			"数据采集",
			"数据清洗",
			"数据分析",
			"机器学习",
			"报表生成",
		},
		Metadata: map[string]interface{}{
			"tech_stack":   []string{"Python", "Spark", "TensorFlow"},
			"data_sources": 15,
		},
	}

	if err := system.CreateTeam(team); err != nil {
		fmt.Printf("✗ 创建团队失败: %v\n", err)
		return nil
	}

	printTeamInfo(team)
	return team
}

// createOpsTeam 创建运维团队
func createOpsTeam(system *multiagent.MultiAgentSystem) *multiagent.Team {
	team := &multiagent.Team{
		ID:      "team-ops",
		Name:    "运维团队",
		Leader:  "ops-lead",
		Members: []string{"ops-lead", "devops-1", "devops-2", "sre-1", "monitor"},
		Purpose: "负责系统运维、部署和监控",
		Capabilities: []string{
			"CI/CD",
			"容器化部署",
			"系统监控",
			"故障排查",
			"性能优化",
		},
		Metadata: map[string]interface{}{
			"tech_stack": []string{"Kubernetes", "Docker", "Prometheus", "Grafana"},
			"uptime_sla": 99.9,
		},
	}

	if err := system.CreateTeam(team); err != nil {
		fmt.Printf("✗ 创建团队失败: %v\n", err)
		return nil
	}

	printTeamInfo(team)
	return team
}

// printTeamInfo 打印团队信息
func printTeamInfo(team *multiagent.Team) {
	fmt.Printf("┌─────────────────────────────────────────┐\n")
	fmt.Printf("│ 团队: %-33s │\n", team.Name)
	fmt.Printf("├─────────────────────────────────────────┤\n")
	fmt.Printf("│ ID: %-35s │\n", team.ID)
	fmt.Printf("│ 负责人: %-31s │\n", team.Leader)
	fmt.Printf("│ 成员数: %-31d │\n", len(team.Members))
	fmt.Printf("│ 目标: %-33s │\n", truncate(team.Purpose, 33))
	fmt.Printf("└─────────────────────────────────────────┘\n")

	fmt.Println("团队成员:")
	for _, member := range team.Members {
		if member == team.Leader {
			fmt.Printf("  ★ %s (负责人)\n", member)
		} else {
			fmt.Printf("  - %s\n", member)
		}
	}

	fmt.Println("团队能力:")
	for _, cap := range team.Capabilities {
		fmt.Printf("  ✓ %s\n", cap)
	}
}

// executeProjectWithTeams 执行跨团队项目
func executeProjectWithTeams(ctx context.Context, system *multiagent.MultiAgentSystem,
	devTeam, dataTeam, opsTeam *multiagent.Team,
) {
	fmt.Println("项目名称: 智能推荐系统 v2.0")
	fmt.Println("项目目标: 基于用户行为数据，构建个性化推荐引擎")
	fmt.Println()

	// 模拟项目阶段
	phases := []struct {
		name        string
		team        *multiagent.Team
		taskType    multiagent.CollaborationType
		description string
	}{
		{
			name:        "数据准备阶段",
			team:        dataTeam,
			taskType:    multiagent.CollaborationTypeSequential,
			description: "数据采集、清洗、特征工程",
		},
		{
			name:        "模型开发阶段",
			team:        dataTeam,
			taskType:    multiagent.CollaborationTypeParallel,
			description: "多个模型并行训练和评估",
		},
		{
			name:        "后端集成阶段",
			team:        devTeam,
			taskType:    multiagent.CollaborationTypeSequential,
			description: "API 开发、模型集成、测试",
		},
		{
			name:        "部署上线阶段",
			team:        opsTeam,
			taskType:    multiagent.CollaborationTypePipeline,
			description: "容器化、部署、监控配置",
		},
	}

	for i, phase := range phases {
		fmt.Printf("\n【阶段 %d】%s\n", i+1, phase.name)
		fmt.Printf("负责团队: %s\n", phase.team.Name)
		fmt.Printf("协作类型: %s\n", phase.taskType)
		fmt.Printf("描述: %s\n", phase.description)

		// 根据协作类型构建不同的 Input 格式
		var taskInput interface{}
		if phase.taskType == multiagent.CollaborationTypePipeline {
			// Pipeline 类型需要 []interface{} 格式的 stages 数组
			taskInput = []interface{}{
				map[string]interface{}{
					"stage":   "containerize",
					"team_id": phase.team.ID,
					"action":  "构建 Docker 镜像",
				},
				map[string]interface{}{
					"stage":   "deploy",
					"team_id": phase.team.ID,
					"action":  "部署到 Kubernetes 集群",
				},
				map[string]interface{}{
					"stage":   "monitor",
					"team_id": phase.team.ID,
					"action":  "配置监控和告警",
				},
			}
		} else {
			// 其他协作类型使用 map[string]interface{} 格式
			taskInput = map[string]interface{}{
				"team_id": phase.team.ID,
				"phase":   i + 1,
				"members": phase.team.Members,
			}
		}

		task := &multiagent.CollaborativeTask{
			ID:          fmt.Sprintf("project-phase-%d", i+1),
			Name:        phase.name,
			Description: phase.description,
			Type:        phase.taskType,
			Input:       taskInput,
			Assignments: make(map[string]multiagent.Assignment),
		}

		startTime := time.Now()
		result, err := system.ExecuteTask(ctx, task)
		duration := time.Since(startTime)

		if err != nil {
			fmt.Printf("✗ 阶段执行失败: %v\n", err)
			continue
		}

		fmt.Printf("✓ 阶段完成 - 状态: %s, 耗时: %v, 参与: %d 个 Agent\n",
			result.Status, duration, len(result.Assignments))
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════")
	fmt.Println("✓ 项目所有阶段执行完成!")
}

// demonstrateRoleChange 演示角色动态调整
func demonstrateRoleChange(system *multiagent.MultiAgentSystem, agents map[string]multiagent.CollaborativeAgent) {
	fmt.Println("场景: 项目紧急，需要临时调整人员角色")
	fmt.Println()

	// 选择一个 Worker 提升为临时负责人
	agent := agents["frontend-dev-1"]
	if agent == nil {
		fmt.Println("✗ Agent 不存在")
		return
	}

	fmt.Printf("调整前: %s 角色为 %s\n", "frontend-dev-1", agent.GetRole())

	// 临时提升角色
	agent.SetRole(multiagent.RoleCoordinator)
	fmt.Printf("调整后: %s 角色为 %s (临时协调者)\n", "frontend-dev-1", agent.GetRole())

	// 恢复角色
	agent.SetRole(multiagent.RoleWorker)
	fmt.Printf("恢复后: %s 角色为 %s\n", "frontend-dev-1", agent.GetRole())

	fmt.Println()
	fmt.Println("✓ 角色动态调整演示完成")
}

// printTeamSummary 打印团队总结
func printTeamSummary(teams ...*multiagent.Team) {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("团队管理核心功能总结:")
	fmt.Println()
	fmt.Println("┌──────────────────┬───────────────────────────────────────────┐")
	fmt.Println("│ 功能             │ 说明                                      │")
	fmt.Println("├──────────────────┼───────────────────────────────────────────┤")
	fmt.Println("│ 团队创建         │ CreateTeam() 创建具有特定能力的团队       │")
	fmt.Println("│ 成员管理         │ 添加/移除团队成员，设置团队负责人         │")
	fmt.Println("│ 角色分配         │ 为 Agent 分配 Leader/Worker/Specialist 等 │")
	fmt.Println("│ 能力定义         │ 定义团队的核心能力和技术栈                │")
	fmt.Println("│ 跨团队协作       │ 多团队协同完成复杂项目                    │")
	fmt.Println("│ 角色动态调整     │ 根据需要动态调整 Agent 角色               │")
	fmt.Println("└──────────────────┴───────────────────────────────────────────┘")
	fmt.Println()

	fmt.Println("本示例创建的团队:")
	totalMembers := 0
	for _, team := range teams {
		if team != nil {
			fmt.Printf("  ✓ %s: %d 成员\n", team.Name, len(team.Members))
			totalMembers += len(team.Members)
		}
	}
	fmt.Printf("\n总计: %d 个团队, %d 个 Agent\n", len(teams), totalMembers)
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
