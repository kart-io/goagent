// Package main 演示多智能体系统中使用工具中间件
//
// 本示例展示：
// 1. 多 Agent 共享带中间件的工具
// 2. 中间件增强 Agent 工具调用能力
// 3. 跨 Agent 调用的可观测性
// 4. 自定义中间件实现 Agent 级别控制
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/multiagent"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/middleware"
	loggercore "github.com/kart-io/logger/core"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          多智能体中间件示例                                     ║")
	fmt.Println("║   展示多 Agent 使用带中间件的工具进行协作                        ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 创建日志和系统
	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	// 场景 1：带日志中间件的 Agent 协作
	fmt.Println("【场景 1】带日志中间件的 Agent 协作")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateLoggingMiddleware(ctx, system)

	// 场景 2：带指标中间件的分布式执行
	fmt.Println("\n【场景 2】带指标中间件的分布式执行")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateMetricsMiddleware(ctx, system)

	// 场景 3：Agent 级别的自定义中间件
	fmt.Println("\n【场景 3】Agent 级别的自定义中间件")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateAgentMiddleware(ctx, system)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// 场景 1：带日志中间件的 Agent 协作
// ============================================================================

func demonstrateLoggingMiddleware(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: 多个 Agent 使用带日志中间件的工具，追踪调用链")
	fmt.Println()

	// 创建日志收集器
	logCollector := &LogCollector{}

	// 创建日志中间件
	loggingMW := createLoggingMiddleware(logCollector)

	// 创建带中间件的工具
	calcTool := tools.WithMiddleware(createCalculatorTool(), loggingMW)
	textTool := tools.WithMiddleware(createTextProcessorTool(), loggingMW)

	// 创建共享工具的 Agent
	agent1 := NewMiddlewareAgent("agent-1", "计算Agent", multiagent.RoleWorker, system, calcTool)
	agent2 := NewMiddlewareAgent("agent-2", "文本Agent", multiagent.RoleWorker, system, textTool)

	_ = system.RegisterAgent("agent-1", agent1)
	_ = system.RegisterAgent("agent-2", agent2)

	fmt.Println("Agent 配置:")
	fmt.Println("  agent-1: 使用带日志的 calculator 工具")
	fmt.Println("  agent-2: 使用带日志的 text_processor 工具")

	// 执行工具调用
	fmt.Println("\n执行工具调用:")
	fmt.Println("────────────────────────────────────────")

	// agent-1 执行计算
	result1, _ := agent1.ExecuteTool(ctx, map[string]interface{}{
		"operation": "multiply",
		"a":         12.0,
		"b":         5.0,
	})
	fmt.Printf("  agent-1: 12 × 5 = %v\n", result1)

	// agent-2 执行文本处理
	result2, _ := agent2.ExecuteTool(ctx, map[string]interface{}{
		"text":   "hello middleware",
		"action": "uppercase",
	})
	fmt.Printf("  agent-2: uppercase('hello middleware') = %v\n", result2)

	// 显示日志
	fmt.Println("\n日志记录:")
	fmt.Println("────────────────────────────────────────")
	for _, log := range logCollector.GetLogs() {
		fmt.Printf("  %s\n", log)
	}

	// 清理
	_ = system.UnregisterAgent("agent-1")
	_ = system.UnregisterAgent("agent-2")
}

// ============================================================================
// 场景 2：带指标中间件的分布式执行
// ============================================================================

func demonstrateMetricsMiddleware(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: 多个 Agent 并行执行，收集调用指标")
	fmt.Println()

	// 创建指标收集器
	metrics := &MetricsCollector{}

	// 创建指标中间件
	metricsMW := createMetricsMiddleware(metrics)

	// 创建带中间件的工具
	calcTool := tools.WithMiddleware(createCalculatorTool(), metricsMW)

	// 创建多个 Worker Agent
	workers := make([]*MiddlewareAgent, 4)
	workerTasks := []struct {
		id   string
		args map[string]interface{}
	}{
		{"worker-1", map[string]interface{}{"operation": "add", "a": 10.0, "b": 20.0}},
		{"worker-2", map[string]interface{}{"operation": "multiply", "a": 5.0, "b": 8.0}},
		{"worker-3", map[string]interface{}{"operation": "subtract", "a": 100.0, "b": 35.0}},
		{"worker-4", map[string]interface{}{"operation": "divide", "a": 144.0, "b": 12.0}},
	}

	for i, wt := range workerTasks {
		workers[i] = NewMiddlewareAgent(wt.id, fmt.Sprintf("工作者%d", i+1), multiagent.RoleWorker, system, calcTool)
		_ = system.RegisterAgent(wt.id, workers[i])
	}

	fmt.Println("创建 Worker Agent:")
	fmt.Println("  worker-1: add (10 + 20)")
	fmt.Println("  worker-2: multiply (5 × 8)")
	fmt.Println("  worker-3: subtract (100 - 35)")
	fmt.Println("  worker-4: divide (144 ÷ 12)")

	// 并行执行
	fmt.Println("\n并行执行工具:")
	fmt.Println("────────────────────────────────────────")

	var wg sync.WaitGroup
	results := make([]string, 4)
	var mu sync.Mutex

	for i, wt := range workerTasks {
		wg.Add(1)
		go func(idx int, task struct {
			id   string
			args map[string]interface{}
		},
		) {
			defer wg.Done()
			result, err := workers[idx].ExecuteTool(ctx, task.args)
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

	// 显示指标
	fmt.Println("\n指标统计:")
	fmt.Println("────────────────────────────────────────")
	stats := metrics.GetStats()
	fmt.Printf("  总调用次数: %d\n", stats.TotalCalls)
	fmt.Printf("  成功次数: %d\n", stats.SuccessCalls)
	fmt.Printf("  失败次数: %d\n", stats.FailedCalls)
	fmt.Printf("  总耗时: %v\n", stats.TotalDuration)
	if stats.TotalCalls > 0 {
		fmt.Printf("  平均耗时: %v\n", stats.TotalDuration/time.Duration(stats.TotalCalls))
	}

	// 清理
	for _, wt := range workerTasks {
		_ = system.UnregisterAgent(wt.id)
	}
}

// ============================================================================
// 场景 3：Agent 级别的自定义中间件
// ============================================================================

func demonstrateAgentMiddleware(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: 使用自定义中间件实现 Agent 级别的访问控制和增强")
	fmt.Println()

	// 创建 Agent 追踪器
	tracker := &AgentTracker{}

	// 创建基础工具
	baseTool := createCalculatorTool()

	// 为不同 Agent 创建带不同中间件的工具
	// admin-agent: 完整权限，带追踪
	adminMW := createAgentTrackingMiddleware("admin-agent", tracker)
	adminTool := tools.WithMiddleware(baseTool, adminMW)

	// worker-agent: 限制某些操作，带追踪
	workerMW := createAgentTrackingMiddleware("worker-agent", tracker)
	restrictedMW := createOperationRestrictionMiddleware([]string{"divide"})
	workerTool := tools.WithMiddleware(baseTool, workerMW, restrictedMW)

	// 创建 Agent
	adminAgent := NewMiddlewareAgent("admin-agent", "管理员", multiagent.RoleCoordinator, system, adminTool)
	workerAgent := NewMiddlewareAgent("worker-agent", "工作者", multiagent.RoleWorker, system, workerTool)

	_ = system.RegisterAgent("admin-agent", adminAgent)
	_ = system.RegisterAgent("worker-agent", workerAgent)

	fmt.Println("Agent 配置:")
	fmt.Println("  admin-agent: 完整权限")
	fmt.Println("  worker-agent: 禁止 divide 操作")

	// 测试调用
	fmt.Println("\n执行测试:")
	fmt.Println("────────────────────────────────────────")

	// admin 执行 divide
	result1, err1 := adminAgent.ExecuteTool(ctx, map[string]interface{}{
		"operation": "divide",
		"a":         100.0,
		"b":         4.0,
	})
	if err1 != nil {
		fmt.Printf("  admin-agent divide: 失败 - %v\n", err1)
	} else {
		fmt.Printf("  admin-agent divide(100, 4) = %v\n", result1)
	}

	// worker 执行 multiply（允许）
	result2, err2 := workerAgent.ExecuteTool(ctx, map[string]interface{}{
		"operation": "multiply",
		"a":         7.0,
		"b":         8.0,
	})
	if err2 != nil {
		fmt.Printf("  worker-agent multiply: 失败 - %v\n", err2)
	} else {
		fmt.Printf("  worker-agent multiply(7, 8) = %v\n", result2)
	}

	// worker 执行 divide（禁止）
	result3, err3 := workerAgent.ExecuteTool(ctx, map[string]interface{}{
		"operation": "divide",
		"a":         100.0,
		"b":         4.0,
	})
	if err3 != nil {
		fmt.Printf("  worker-agent divide: 被拒绝 - %v\n", err3)
	} else {
		fmt.Printf("  worker-agent divide(100, 4) = %v\n", result3)
	}

	// 显示追踪信息
	fmt.Println("\nAgent 调用追踪:")
	fmt.Println("────────────────────────────────────────")
	for _, trace := range tracker.GetTraces() {
		fmt.Printf("  %s\n", trace)
	}

	// 清理
	_ = system.UnregisterAgent("admin-agent")
	_ = system.UnregisterAgent("worker-agent")
}

// ============================================================================
// Agent 实现
// ============================================================================

// MiddlewareAgent 使用带中间件工具的 Agent
type MiddlewareAgent struct {
	*multiagent.BaseCollaborativeAgent
	tool interfaces.Tool
}

// NewMiddlewareAgent 创建中间件 Agent
func NewMiddlewareAgent(
	id, description string,
	role multiagent.Role,
	system *multiagent.MultiAgentSystem,
	tool interfaces.Tool,
) *MiddlewareAgent {
	return &MiddlewareAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(id, description, role, system),
		tool:                   tool,
	}
}

// ExecuteTool 执行工具
func (a *MiddlewareAgent) ExecuteTool(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	output, err := a.tool.Invoke(ctx, &interfaces.ToolInput{Args: args})
	if err != nil {
		return nil, err
	}
	return output.Result, nil
}

// Collaborate 实现协作接口
func (a *MiddlewareAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	assignment := &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Status:    multiagent.TaskStatusExecuting,
		StartTime: time.Now(),
	}

	// 从任务中提取工具调用参数
	args, ok := task.Input.(map[string]interface{})
	if !ok {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, fmt.Errorf("无效的任务输入格式")
	}

	result, err := a.ExecuteTool(ctx, args)
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
// 中间件实现
// ============================================================================

// LogCollector 日志收集器
type LogCollector struct {
	logs []string
	mu   sync.Mutex
}

func (c *LogCollector) Log(message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, message)
}

func (c *LogCollector) GetLogs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]string, len(c.logs))
	copy(result, c.logs)
	return result
}

// createLoggingMiddleware 创建日志中间件
func createLoggingMiddleware(collector *LogCollector) middleware.ToolMiddlewareFunc {
	return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
		return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			start := time.Now()
			collector.Log(fmt.Sprintf("[开始] %s - 参数: %v", tool.Name(), input.Args))

			output, err := next(ctx, input)

			duration := time.Since(start)
			if err != nil {
				collector.Log(fmt.Sprintf("[失败] %s - 耗时: %v, 错误: %v", tool.Name(), duration, err))
			} else {
				collector.Log(fmt.Sprintf("[完成] %s - 耗时: %v, 结果: %v", tool.Name(), duration, output.Result))
			}

			return output, err
		}
	}
}

// MetricsCollector 指标收集器
type MetricsCollector struct {
	totalCalls    int64
	successCalls  int64
	failedCalls   int64
	totalDuration int64 // 纳秒
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

// createMetricsMiddleware 创建指标中间件
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

// AgentTracker Agent 追踪器
type AgentTracker struct {
	traces []string
	mu     sync.Mutex
}

func (t *AgentTracker) Track(agentID, toolName string, args map[string]interface{}, success bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	status := "成功"
	if !success {
		status = "失败"
	}
	t.traces = append(t.traces, fmt.Sprintf("[%s] Agent:%s 工具:%s 状态:%s",
		time.Now().Format("15:04:05"), agentID, toolName, status))
}

func (t *AgentTracker) GetTraces() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]string, len(t.traces))
	copy(result, t.traces)
	return result
}

// createAgentTrackingMiddleware 创建 Agent 追踪中间件
func createAgentTrackingMiddleware(agentID string, tracker *AgentTracker) middleware.ToolMiddlewareFunc {
	return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
		return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			output, err := next(ctx, input)
			tracker.Track(agentID, tool.Name(), input.Args, err == nil)
			return output, err
		}
	}
}

// createOperationRestrictionMiddleware 创建操作限制中间件
func createOperationRestrictionMiddleware(forbiddenOps []string) middleware.ToolMiddlewareFunc {
	forbidden := make(map[string]bool)
	for _, op := range forbiddenOps {
		forbidden[op] = true
	}

	return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
		return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			// 检查操作是否被禁止
			if op, ok := input.Args["operation"].(string); ok {
				if forbidden[op] {
					return nil, fmt.Errorf("操作 '%s' 被禁止", op)
				}
			}
			return next(ctx, input)
		}
	}
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
				for _, r := range text {
					if r >= 'a' && r <= 'z' {
						result += string(r - 32)
					} else {
						result += string(r)
					}
				}
			case "lowercase":
				for _, r := range text {
					if r >= 'A' && r <= 'Z' {
						result += string(r + 32)
					} else {
						result += string(r)
					}
				}
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
