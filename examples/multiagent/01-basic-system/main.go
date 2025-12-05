// Package main 演示 MultiAgentSystem 的基本用法
// 本示例展示如何创建多智能体系统、注册 Agent、发送消息和执行协作任务
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
	fmt.Println("║          MultiAgentSystem 基础示例                             ║")
	fmt.Println("║   展示如何创建多智能体系统、注册 Agent 并执行协作任务          ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 创建 MultiAgentSystem
	fmt.Println("【步骤 1】创建 MultiAgentSystem")
	fmt.Println("────────────────────────────────────────")

	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(
		logger,
		multiagent.WithMaxAgents(10),
		multiagent.WithTimeout(30*time.Second),
	)

	fmt.Println("✓ MultiAgentSystem 创建成功")
	fmt.Println()

	// 2. 创建并注册协作 Agent
	fmt.Println("【步骤 2】创建并注册协作 Agent")
	fmt.Println("────────────────────────────────────────")

	// 创建不同角色的 Agent
	leaderAgent := multiagent.NewBaseCollaborativeAgent(
		"leader-1",
		"团队领导者，负责任务规划和分配",
		multiagent.RoleLeader,
		system,
	)

	workerAgent1 := multiagent.NewBaseCollaborativeAgent(
		"worker-1",
		"工作者 1，负责数据处理",
		multiagent.RoleWorker,
		system,
	)

	workerAgent2 := multiagent.NewBaseCollaborativeAgent(
		"worker-2",
		"工作者 2，负责计算任务",
		multiagent.RoleWorker,
		system,
	)

	validatorAgent := multiagent.NewBaseCollaborativeAgent(
		"validator-1",
		"验证者，负责结果校验",
		multiagent.RoleValidator,
		system,
	)

	// 注册 Agent 到系统
	agents := []struct {
		id    string
		agent multiagent.CollaborativeAgent
	}{
		{"leader-1", leaderAgent},
		{"worker-1", workerAgent1},
		{"worker-2", workerAgent2},
		{"validator-1", validatorAgent},
	}

	for _, a := range agents {
		if err := system.RegisterAgent(a.id, a.agent); err != nil {
			fmt.Printf("✗ 注册 Agent %s 失败: %v\n", a.id, err)
			return
		}
		fmt.Printf("✓ 注册 Agent: %s (角色: %s)\n", a.id, a.agent.GetRole())
	}
	fmt.Println()

	// 3. 执行并行协作任务
	fmt.Println("【步骤 3】执行并行协作任务")
	fmt.Println("────────────────────────────────────────")

	parallelTask := &multiagent.CollaborativeTask{
		ID:          "task-parallel-001",
		Name:        "并行数据处理任务",
		Description: "多个 Agent 并行处理数据",
		Type:        multiagent.CollaborationTypeParallel,
		Input: map[string]interface{}{
			"data_source": "sensor_data",
			"records":     1000,
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Printf("任务 ID: %s\n", parallelTask.ID)
	fmt.Printf("任务类型: %s\n", parallelTask.Type)
	fmt.Printf("任务输入: %v\n", parallelTask.Input)
	fmt.Println()

	fmt.Println("执行中...")
	result, err := system.ExecuteTask(ctx, parallelTask)
	if err != nil {
		fmt.Printf("✗ 任务执行失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 任务状态: %s\n", result.Status)
	fmt.Printf("✓ 执行时长: %v\n", result.EndTime.Sub(result.StartTime))
	fmt.Println()

	fmt.Println("任务结果:")
	for agentID, agentResult := range result.Results {
		fmt.Printf("  - Agent %s: %v\n", agentID, agentResult)
	}
	fmt.Println()

	// 4. 执行顺序协作任务
	fmt.Println("【步骤 4】执行顺序协作任务")
	fmt.Println("────────────────────────────────────────")

	sequentialTask := &multiagent.CollaborativeTask{
		ID:          "task-sequential-001",
		Name:        "顺序处理流水线",
		Description: "Agent 按顺序依次处理任务",
		Type:        multiagent.CollaborationTypeSequential,
		Input: map[string]interface{}{
			"pipeline": "data_transformation",
			"stages":   []string{"extract", "transform", "load"},
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Printf("任务 ID: %s\n", sequentialTask.ID)
	fmt.Printf("任务类型: %s\n", sequentialTask.Type)
	fmt.Println()

	fmt.Println("执行中...")
	seqResult, err := system.ExecuteTask(ctx, sequentialTask)
	if err != nil {
		fmt.Printf("✗ 任务执行失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 任务状态: %s\n", seqResult.Status)
	fmt.Printf("✓ 执行时长: %v\n", seqResult.EndTime.Sub(seqResult.StartTime))
	fmt.Println()

	// 5. Agent 消息通信示例
	fmt.Println("【步骤 5】Agent 消息通信")
	fmt.Println("────────────────────────────────────────")

	// 演示 Agent 间发送消息
	message := multiagent.Message{
		ID:        "msg-001",
		From:      "leader-1",
		To:        "worker-1",
		Type:      multiagent.MessageTypeCommand,
		Content:   "开始处理数据批次 A",
		Priority:  1,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"batch_id": "A",
			"urgent":   true,
		},
	}

	fmt.Printf("发送消息: %s -> %s\n", message.From, message.To)
	fmt.Printf("消息类型: %s\n", message.Type)
	fmt.Printf("消息内容: %v\n", message.Content)

	// 发送消息到 Agent
	if err := workerAgent1.ReceiveMessage(ctx, message); err != nil {
		fmt.Printf("✗ 消息发送失败: %v\n", err)
	} else {
		fmt.Println("✓ 消息发送成功")
	}
	fmt.Println()

	// 6. 注销 Agent
	fmt.Println("【步骤 6】注销 Agent")
	fmt.Println("────────────────────────────────────────")

	if err := system.UnregisterAgent("worker-2"); err != nil {
		fmt.Printf("✗ 注销 Agent 失败: %v\n", err)
	} else {
		fmt.Println("✓ Agent worker-2 已注销")
	}
	fmt.Println()

	// 总结
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("本示例演示了 MultiAgentSystem 的核心功能:")
	fmt.Println("  ✓ 创建多智能体系统")
	fmt.Println("  ✓ 注册不同角色的协作 Agent")
	fmt.Println("  ✓ 执行并行协作任务")
	fmt.Println("  ✓ 执行顺序协作任务")
	fmt.Println("  ✓ Agent 间消息通信")
	fmt.Println("  ✓ 注销 Agent")
	fmt.Println()
	fmt.Println("更多协作模式请参考 02-collaboration-types 示例")
}
