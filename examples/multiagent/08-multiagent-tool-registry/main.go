// Package main 演示多智能体系统中使用工具注册表
//
// 本示例展示：
// 1. 多 Agent 共享工具注册表
// 2. Agent 按需动态获取工具
// 3. 工具执行结果在 Agent 间传递
// 4. 分布式工具协作模式
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/multiagent"
	"github.com/kart-io/goagent/tools"

	loggercore "github.com/kart-io/logger/core"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          多智能体工具注册表示例                                 ║")
	fmt.Println("║   展示多 Agent 共享和协作使用工具注册表                         ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 创建日志和系统
	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	// 场景 1：共享工具注册表
	fmt.Println("【场景 1】共享工具注册表")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateSharedRegistry(ctx, system)

	// 场景 2：分布式工具执行
	fmt.Println("\n【场景 2】分布式工具执行")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateDistributedExecution(ctx, system)

	// 场景 3：工具结果传递
	fmt.Println("\n【场景 3】工具结果传递")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateResultPassing(ctx, system)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// 场景 1：共享工具注册表
// ============================================================================

func demonstrateSharedRegistry(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: 多个 Agent 共享同一个工具注册表，按需获取工具")
	fmt.Println()

	// 创建共享工具注册表
	registry := tools.NewRegistry()

	// 注册各类工具
	toolsToRegister := []interfaces.Tool{
		createCalculatorTool(),
		createTextProcessorTool(),
		createDateTimeTool(),
		createDataTransformTool(),
	}

	fmt.Println("注册工具到共享注册表:")
	for _, tool := range toolsToRegister {
		if err := registry.Register(tool); err != nil {
			fmt.Printf("  ✗ %s: %v\n", tool.Name(), err)
		} else {
			fmt.Printf("  ✓ %s: %s\n", tool.Name(), tool.Description())
		}
	}

	fmt.Printf("\n注册表状态: %d 个工具可用\n", registry.Size())

	// 创建使用共享注册表的 Agent
	mathAgent := NewRegistryAgent("math-agent", "数学Agent", multiagent.RoleWorker, system, registry, []string{"calculator"})
	textAgent := NewRegistryAgent("text-agent", "文本Agent", multiagent.RoleWorker, system, registry, []string{"text_processor"})
	utilAgent := NewRegistryAgent("util-agent", "工具Agent", multiagent.RoleWorker, system, registry, []string{"datetime", "data_transform"})

	_ = system.RegisterAgent("math-agent", mathAgent)
	_ = system.RegisterAgent("text-agent", textAgent)
	_ = system.RegisterAgent("util-agent", utilAgent)

	fmt.Println("\n注册 Agent 及其可用工具:")
	fmt.Println("  ✓ math-agent: [calculator]")
	fmt.Println("  ✓ text-agent: [text_processor]")
	fmt.Println("  ✓ util-agent: [datetime, data_transform]")

	// 各 Agent 执行任务
	fmt.Println("\n执行工具调用:")
	fmt.Println("────────────────────────────────────────")

	// math-agent 执行计算
	result1, _ := mathAgent.ExecuteTool(ctx, "calculator", map[string]interface{}{
		"operation": "multiply",
		"a":         15.0,
		"b":         8.0,
	})
	fmt.Printf("  math-agent: 15 × 8 = %v\n", result1)

	// text-agent 执行文本处理
	result2, _ := textAgent.ExecuteTool(ctx, "text_processor", map[string]interface{}{
		"text":   "hello world",
		"action": "uppercase",
	})
	fmt.Printf("  text-agent: uppercase('hello world') = %v\n", result2)

	// util-agent 获取时间
	result3, _ := utilAgent.ExecuteTool(ctx, "datetime", map[string]interface{}{
		"format": "full",
	})
	fmt.Printf("  util-agent: 当前时间 = %v\n", result3)

	// 清理
	_ = system.UnregisterAgent("math-agent")
	_ = system.UnregisterAgent("text-agent")
	_ = system.UnregisterAgent("util-agent")
}

// ============================================================================
// 场景 2：分布式工具执行
// ============================================================================

func demonstrateDistributedExecution(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: 多个 Agent 并行执行不同工具，汇总结果")
	fmt.Println()

	// 创建共享注册表
	registry := tools.NewRegistry()
	_ = registry.Register(createCalculatorTool())
	_ = registry.Register(createTextProcessorTool())
	_ = registry.Register(createDateTimeTool())
	_ = registry.Register(createDataTransformTool())

	// 创建多个 Worker Agent
	workers := make([]*RegistryAgent, 4)
	workerTasks := []struct {
		id       string
		toolName string
		args     map[string]interface{}
	}{
		{"worker-1", "calculator", map[string]interface{}{"operation": "add", "a": 100.0, "b": 200.0}},
		{"worker-2", "text_processor", map[string]interface{}{"text": "distributed", "action": "uppercase"}},
		{"worker-3", "datetime", map[string]interface{}{"format": "date"}},
		{"worker-4", "data_transform", map[string]interface{}{"data": []int{1, 2, 3, 4, 5}, "operation": "sum"}},
	}

	for i, wt := range workerTasks {
		workers[i] = NewRegistryAgent(wt.id, fmt.Sprintf("工作者%d", i+1), multiagent.RoleWorker, system, registry, []string{wt.toolName})
		_ = system.RegisterAgent(wt.id, workers[i])
	}

	fmt.Println("创建分布式工作者:")
	fmt.Println("  worker-1: calculator (加法)")
	fmt.Println("  worker-2: text_processor (大写)")
	fmt.Println("  worker-3: datetime (日期)")
	fmt.Println("  worker-4: data_transform (求和)")

	// 并行执行
	fmt.Println("\n并行执行工具:")
	fmt.Println("────────────────────────────────────────")

	var wg sync.WaitGroup
	results := make([]string, 4)
	var mu sync.Mutex

	for i, wt := range workerTasks {
		wg.Add(1)
		go func(idx int, task struct {
			id       string
			toolName string
			args     map[string]interface{}
		}) {
			defer wg.Done()
			result, err := workers[idx].ExecuteTool(ctx, task.toolName, task.args)
			mu.Lock()
			if err != nil {
				results[idx] = fmt.Sprintf("%s: 失败 - %v", task.id, err)
			} else {
				results[idx] = fmt.Sprintf("%s: %v", task.id, result)
			}
			mu.Unlock()
		}(i, wt)
	}

	wg.Wait()

	for _, r := range results {
		fmt.Printf("  %s\n", r)
	}

	// 清理
	for _, wt := range workerTasks {
		_ = system.UnregisterAgent(wt.id)
	}
}

// ============================================================================
// 场景 3：工具结果传递
// ============================================================================

func demonstrateResultPassing(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: Agent 链式处理，前一个工具的输出作为后一个的输入")
	fmt.Println()

	// 创建共享注册表
	registry := tools.NewRegistry()
	_ = registry.Register(createCalculatorTool())
	_ = registry.Register(createTextProcessorTool())
	_ = registry.Register(createDataTransformTool())

	// 创建流水线 Agent
	agent1 := NewRegistryAgent("stage-1", "阶段1", multiagent.RoleWorker, system, registry, []string{"calculator"})
	agent2 := NewRegistryAgent("stage-2", "阶段2", multiagent.RoleWorker, system, registry, []string{"data_transform"})
	agent3 := NewRegistryAgent("stage-3", "阶段3", multiagent.RoleWorker, system, registry, []string{"text_processor"})

	_ = system.RegisterAgent("stage-1", agent1)
	_ = system.RegisterAgent("stage-2", agent2)
	_ = system.RegisterAgent("stage-3", agent3)

	fmt.Println("工具链配置:")
	fmt.Println("  stage-1 (calculator) → stage-2 (data_transform) → stage-3 (text_processor)")

	// 执行流水线
	fmt.Println("\n执行工具链:")
	fmt.Println("────────────────────────────────────────")

	// 阶段 1: 计算
	fmt.Println("\n[阶段 1] 计算 10 × 5")
	result1, _ := agent1.ExecuteTool(ctx, "calculator", map[string]interface{}{
		"operation": "multiply",
		"a":         10.0,
		"b":         5.0,
	})
	fmt.Printf("  结果: %v\n", result1)

	// 提取计算结果
	var calcResult float64
	if resultMap, ok := result1.(map[string]interface{}); ok {
		if r, ok := resultMap["result"].(float64); ok {
			calcResult = r
		}
	}

	// 阶段 2: 数据转换
	fmt.Println("\n[阶段 2] 将结果作为数组元素处理")
	result2, _ := agent2.ExecuteTool(ctx, "data_transform", map[string]interface{}{
		"data":      []float64{calcResult, calcResult * 2, calcResult * 3},
		"operation": "format",
	})
	fmt.Printf("  结果: %v\n", result2)

	// 提取格式化结果
	var formattedResult string
	if resultMap, ok := result2.(map[string]interface{}); ok {
		if r, ok := resultMap["result"].(string); ok {
			formattedResult = r
		}
	}

	// 阶段 3: 文本处理
	fmt.Println("\n[阶段 3] 将结果转为大写")
	result3, _ := agent3.ExecuteTool(ctx, "text_processor", map[string]interface{}{
		"text":   formattedResult,
		"action": "uppercase",
	})
	fmt.Printf("  最终结果: %v\n", result3)

	// 清理
	_ = system.UnregisterAgent("stage-1")
	_ = system.UnregisterAgent("stage-2")
	_ = system.UnregisterAgent("stage-3")
}

// ============================================================================
// Agent 实现
// ============================================================================

// RegistryAgent 使用工具注册表的 Agent
type RegistryAgent struct {
	*multiagent.BaseCollaborativeAgent
	registry     *tools.Registry
	allowedTools []string
}

// NewRegistryAgent 创建注册表 Agent
func NewRegistryAgent(
	id, description string,
	role multiagent.Role,
	system *multiagent.MultiAgentSystem,
	registry *tools.Registry,
	allowedTools []string,
) *RegistryAgent {
	return &RegistryAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(id, description, role, system),
		registry:               registry,
		allowedTools:           allowedTools,
	}
}

// ExecuteTool 执行工具
func (a *RegistryAgent) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	// 检查是否允许使用该工具
	allowed := false
	for _, t := range a.allowedTools {
		if t == toolName {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("agent %s 不允许使用工具 %s", a.Name(), toolName)
	}

	// 从注册表获取工具
	tool := a.registry.Get(toolName)
	if tool == nil {
		return nil, fmt.Errorf("工具 %s 未找到", toolName)
	}

	// 执行工具
	output, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: args})
	if err != nil {
		return nil, err
	}

	return output.Result, nil
}

// Collaborate 实现协作接口
func (a *RegistryAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	assignment := &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Status:    multiagent.TaskStatusExecuting,
		StartTime: time.Now(),
	}

	// 从任务中提取工具调用信息
	input, ok := task.Input.(map[string]interface{})
	if !ok {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, fmt.Errorf("无效的任务输入格式")
	}

	toolName, _ := input["tool"].(string)
	toolArgs, _ := input["args"].(map[string]interface{})

	result, err := a.ExecuteTool(ctx, toolName, toolArgs)
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
// 工具定义
// ============================================================================

func createCalculatorTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"calculator",
		"执行基本数学计算",
		`{"type": "object", "properties": {"operation": {"type": "string"}, "a": {"type": "number"}, "b": {"type": "number"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			op := args["operation"].(string)
			a := args["a"].(float64)
			b := args["b"].(float64)

			var result float64
			switch op {
			case "add":
				result = a + b
			case "subtract":
				result = a - b
			case "multiply":
				result = a * b
			case "divide":
				if b == 0 {
					return nil, fmt.Errorf("除数不能为零")
				}
				result = a / b
			}
			return map[string]interface{}{"result": result}, nil
		},
	)
}

func createTextProcessorTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"text_processor",
		"处理文本",
		`{"type": "object", "properties": {"text": {"type": "string"}, "action": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			text := args["text"].(string)
			action := args["action"].(string)

			var result string
			switch action {
			case "uppercase":
				result = strings.ToUpper(text)
			case "lowercase":
				result = strings.ToLower(text)
			case "reverse":
				runes := []rune(text)
				for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
					runes[i], runes[j] = runes[j], runes[i]
				}
				result = string(runes)
			}
			return map[string]interface{}{"result": result}, nil
		},
	)
}

func createDateTimeTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"datetime",
		"获取当前日期时间",
		`{"type": "object", "properties": {"format": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			format := "full"
			if f, ok := args["format"].(string); ok {
				format = f
			}

			now := time.Now()
			var result string
			switch format {
			case "date":
				result = now.Format("2006-01-02")
			case "time":
				result = now.Format("15:04:05")
			case "full":
				result = now.Format("2006-01-02 15:04:05")
			}
			return map[string]interface{}{"result": result}, nil
		},
	)
}

func createDataTransformTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"data_transform",
		"数据转换工具",
		`{"type": "object", "properties": {"data": {"type": "array"}, "operation": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			operation := args["operation"].(string)

			switch operation {
			case "sum":
				// 计算数组总和
				data, _ := args["data"].([]int)
				sum := 0
				for _, v := range data {
					sum += v
				}
				return map[string]interface{}{"result": sum}, nil
			case "format":
				// 格式化数组为字符串
				data, _ := args["data"].([]float64)
				parts := make([]string, len(data))
				for i, v := range data {
					parts[i] = fmt.Sprintf("%.0f", v)
				}
				return map[string]interface{}{"result": strings.Join(parts, ", ")}, nil
			default:
				return map[string]interface{}{"result": args["data"]}, nil
			}
		},
	)
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
