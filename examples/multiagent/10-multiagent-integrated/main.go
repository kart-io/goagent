// Package main 演示多智能体系统综合使用 LLM、工具注册表、中间件和记忆
//
// 本示例展示：
// 1. 多 Agent 使用 LLM 进行智能决策
// 2. 共享工具注册表提供统一工具访问
// 3. 中间件增强工具调用的可观测性和控制
// 4. 记忆管理器存储对话历史和知识
// 5. 完整的多智能体协作流程
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/multiagent"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/middleware"

	loggercore "github.com/kart-io/logger/core"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          多智能体综合示例                                       ║")
	fmt.Println("║   展示多 Agent 同时使用 LLM、工具、中间件和记忆                   ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 创建日志和系统
	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	// 场景：智能数据分析流水线
	// 多个 Agent 协作完成数据分析任务：
	// 1. 协调者 Agent 使用 LLM 理解任务并分配工作
	// 2. 数据 Agent 使用工具获取和处理数据
	// 3. 分析 Agent 使用 LLM 进行智能分析
	// 4. 所有工具调用都经过中间件进行日志和指标收集
	// 5. 记忆管理器存储对话历史和分析案例
	fmt.Println("【综合场景】智能数据分析流水线")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateIntegratedPipeline(ctx, system)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// 综合场景：智能数据分析流水线
// ============================================================================

func demonstrateIntegratedPipeline(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述:")
	fmt.Println("  多个 Agent 协作完成智能数据分析任务")
	fmt.Println("  - 协调者 Agent：使用 LLM 理解任务并分配工作")
	fmt.Println("  - 数据 Agent：使用带中间件的工具获取和处理数据")
	fmt.Println("  - 分析 Agent：使用 LLM 进行智能分析")
	fmt.Println("  - 记忆管理器：存储对话历史和分析案例")
	fmt.Println("  - 所有工具调用经过日志和指标中间件")
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

	// 创建工具注册表
	registry := tools.NewRegistry()
	fmt.Println("  ✓ 工具注册表已创建")

	// 创建记忆管理器
	memoryConfig := &memory.Config{
		EnableConversation:    true,
		MaxConversationLength: 20,
	}
	memoryManager := memory.NewInMemoryManager(memoryConfig)
	fmt.Println("  ✓ 记忆管理器已创建 (对话历史 + 案例存储)")

	// 创建中间件组件
	logCollector := &LogCollector{}
	metricsCollector := &MetricsCollector{}

	loggingMW := createLoggingMiddleware(logCollector)
	metricsMW := createMetricsMiddleware(metricsCollector)

	fmt.Println("  ✓ 日志中间件已创建")
	fmt.Println("  ✓ 指标中间件已创建")

	// ========== 2. 注册带中间件的工具 ==========
	fmt.Println("\n【步骤 2】注册带中间件的工具")
	fmt.Println("────────────────────────────────────────")

	// 创建基础工具
	dataFetchTool := createDataFetchTool()
	dataProcessTool := createDataProcessTool()
	calculatorTool := createCalculatorTool()
	formatterTool := createFormatterTool()

	// 应用中间件并注册
	toolsWithMW := []struct {
		name string
		tool interfaces.Tool
	}{
		{"data_fetch", tools.WithMiddleware(dataFetchTool, loggingMW, metricsMW)},
		{"data_process", tools.WithMiddleware(dataProcessTool, loggingMW, metricsMW)},
		{"calculator", tools.WithMiddleware(calculatorTool, loggingMW, metricsMW)},
		{"formatter", tools.WithMiddleware(formatterTool, loggingMW, metricsMW)},
	}

	for _, t := range toolsWithMW {
		if err := registry.Register(t.tool); err != nil {
			fmt.Printf("  ✗ %s: %v\n", t.name, err)
		} else {
			fmt.Printf("  ✓ %s: 已注册（带日志和指标中间件）\n", t.name)
		}
	}

	fmt.Printf("  注册表状态: %d 个工具可用\n", registry.Size())

	// ========== 3. 创建 Agent ==========
	fmt.Println("\n【步骤 3】创建智能 Agent")
	fmt.Println("────────────────────────────────────────")

	// 创建协调者 Agent（使用 LLM 进行任务理解和分配）
	coordinator := NewIntegratedAgent(
		"coordinator",
		"协调者",
		multiagent.RoleCoordinator,
		system,
		llmClient,
		registry,
		memoryManager,
		[]string{}, // 协调者不直接使用工具
		"你是一个任务协调者，负责理解用户需求并分配任务给其他 Agent。请简洁回答。",
	)

	// 创建数据 Agent（使用工具获取和处理数据）
	dataAgent := NewIntegratedAgent(
		"data-agent",
		"数据Agent",
		multiagent.RoleWorker,
		system,
		llmClient,
		registry,
		memoryManager,
		[]string{"data_fetch", "data_process"},
		"你是一个数据处理专家，负责获取和预处理数据。",
	)

	// 创建分析 Agent（使用 LLM 和工具进行分析）
	analysisAgent := NewIntegratedAgent(
		"analysis-agent",
		"分析Agent",
		multiagent.RoleSpecialist,
		system,
		llmClient,
		registry,
		memoryManager,
		[]string{"calculator", "formatter"},
		"你是一个数据分析专家，负责对数据进行深度分析并生成报告。请简洁回答。",
	)

	_ = system.RegisterAgent("coordinator", coordinator)
	_ = system.RegisterAgent("data-agent", dataAgent)
	_ = system.RegisterAgent("analysis-agent", analysisAgent)

	fmt.Println("  ✓ coordinator: 协调者 (LLM 决策 + 记忆)")
	fmt.Println("  ✓ data-agent: 数据Agent (data_fetch, data_process + 记忆)")
	fmt.Println("  ✓ analysis-agent: 分析Agent (calculator, formatter + LLM + 记忆)")

	// ========== 4. 执行分析流水线 ==========
	fmt.Println("\n【步骤 4】执行智能分析流水线")
	fmt.Println("────────────────────────────────────────")

	// 会话 ID
	sessionID := "analysis-session-001"

	// 用户任务
	userTask := "分析销售数据，计算总额和平均值，并生成报告"
	fmt.Printf("\n用户任务: %s\n", userTask)
	fmt.Println()

	// 4.1 记录用户请求到记忆
	_ = memoryManager.AddConversation(ctx, &interfaces.Conversation{
		SessionID: sessionID,
		Role:      "user",
		Content:   userTask,
		Timestamp: time.Now(),
	})

	// 4.2 协调者理解任务
	fmt.Println("[4.1] 协调者理解任务...")
	taskUnderstanding := coordinator.AnalyzeWithLLM(ctx, userTask)
	fmt.Printf("  协调者理解: %s\n", truncateString(taskUnderstanding, 100))

	// 记录协调者响应
	_ = memoryManager.AddConversation(ctx, &interfaces.Conversation{
		SessionID: sessionID,
		Role:      "assistant",
		Content:   taskUnderstanding,
		Timestamp: time.Now(),
		Metadata:  map[string]interface{}{"agent": "coordinator"},
	})

	// 4.3 数据 Agent 获取数据
	fmt.Println("\n[4.2] 数据Agent获取数据...")
	fetchResult, err := dataAgent.ExecuteTool(ctx, "data_fetch", map[string]interface{}{
		"source": "sales_2024",
		"type":   "quarterly",
	})
	if err != nil {
		fmt.Printf("  ✗ 获取数据失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 获取数据成功: %v\n", fetchResult)
	}

	// 4.4 数据 Agent 处理数据
	fmt.Println("\n[4.3] 数据Agent处理数据...")
	processResult, err := dataAgent.ExecuteTool(ctx, "data_process", map[string]interface{}{
		"data":      []float64{1200, 1500, 1800, 2100},
		"operation": "normalize",
	})
	if err != nil {
		fmt.Printf("  ✗ 处理数据失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 处理数据成功: %v\n", processResult)
	}

	// 4.5 分析 Agent 计算统计
	fmt.Println("\n[4.4] 分析Agent计算统计...")

	// 计算总和
	sumResult, err := analysisAgent.ExecuteTool(ctx, "calculator", map[string]interface{}{
		"operation": "sum",
		"values":    []float64{1200, 1500, 1800, 2100},
	})
	if err != nil {
		fmt.Printf("  ✗ 计算总和失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 总和: %v\n", sumResult)
	}

	// 计算平均
	avgResult, err := analysisAgent.ExecuteTool(ctx, "calculator", map[string]interface{}{
		"operation": "average",
		"values":    []float64{1200, 1500, 1800, 2100},
	})
	if err != nil {
		fmt.Printf("  ✗ 计算平均失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 平均: %v\n", avgResult)
	}

	// 4.6 分析 Agent 使用 LLM 生成分析
	fmt.Println("\n[4.5] 分析Agent生成智能分析...")
	analysisPrompt := fmt.Sprintf(
		"基于以下销售数据分析结果，给出简短的业务洞察（2-3句话）:\n"+
			"- 季度数据: Q1=1200, Q2=1500, Q3=1800, Q4=2100\n"+
			"- 总销售额: %v\n"+
			"- 平均销售额: %v",
		sumResult, avgResult,
	)
	llmAnalysis := analysisAgent.AnalyzeWithLLM(ctx, analysisPrompt)
	fmt.Printf("  智能分析: %s\n", llmAnalysis)

	// 记录分析结果到记忆
	_ = memoryManager.AddConversation(ctx, &interfaces.Conversation{
		SessionID: sessionID,
		Role:      "assistant",
		Content:   llmAnalysis,
		Timestamp: time.Now(),
		Metadata:  map[string]interface{}{"agent": "analysis-agent", "type": "analysis"},
	})

	// 4.7 格式化输出
	fmt.Println("\n[4.6] 格式化最终报告...")
	formatResult, err := analysisAgent.ExecuteTool(ctx, "formatter", map[string]interface{}{
		"template": "report",
		"data": map[string]interface{}{
			"title":    "2024年销售分析报告",
			"total":    sumResult,
			"average":  avgResult,
			"analysis": llmAnalysis,
		},
	})
	if err != nil {
		fmt.Printf("  ✗ 格式化失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 报告已生成\n")
	}

	// 4.8 存储分析案例到记忆
	fmt.Println("\n[4.7] 存储分析案例到记忆...")
	analysisCase := &interfaces.Case{
		Title:       "2024年销售数据分析",
		Description: "基于季度销售数据的综合分析",
		Problem:     userTask,
		Solution:    llmAnalysis,
		Category:    "sales-analysis",
		Tags:        []string{"sales", "quarterly", "2024"},
		Metadata: map[string]interface{}{
			"total":   sumResult,
			"average": avgResult,
		},
	}
	if err := memoryManager.AddCase(ctx, analysisCase); err != nil {
		fmt.Printf("  ✗ 存储案例失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 分析案例已存储 (ID: %s)\n", analysisCase.ID)
	}

	// 4.9 存储关键数据到键值存储
	_ = memoryManager.Store(ctx, "last_analysis_session", sessionID)
	_ = memoryManager.Store(ctx, "last_analysis_time", time.Now().Format(time.RFC3339))
	_ = memoryManager.Store(ctx, "total_sales_2024", sumResult)
	fmt.Println("  ✓ 关键数据已存储到键值存储")

	// ========== 5. 显示记忆数据 ==========
	fmt.Println("\n【步骤 5】记忆数据")
	fmt.Println("────────────────────────────────────────")

	// 获取对话历史
	fmt.Println("\n对话历史:")
	conversations, _ := memoryManager.GetConversationHistory(ctx, sessionID, 10)
	for i, conv := range conversations {
		agentInfo := ""
		if agent, ok := conv.Metadata["agent"].(string); ok {
			agentInfo = fmt.Sprintf(" [%s]", agent)
		}
		fmt.Printf("  %d. [%s]%s %s\n", i+1, conv.Role, agentInfo, truncateString(conv.Content, 60))
	}

	// 搜索相似案例
	fmt.Println("\n相似案例搜索 (查询: '销售分析'):")
	cases, _ := memoryManager.SearchSimilarCases(ctx, "销售分析", 3)
	for i, c := range cases {
		fmt.Printf("  %d. %s (相似度: %.2f)\n", i+1, c.Title, c.Similarity)
		fmt.Printf("     问题: %s\n", truncateString(c.Problem, 50))
		fmt.Printf("     方案: %s\n", truncateString(c.Solution, 50))
	}

	// 检索存储的值
	fmt.Println("\n键值存储:")
	if lastSession, err := memoryManager.Retrieve(ctx, "last_analysis_session"); err == nil {
		fmt.Printf("  last_analysis_session: %v\n", lastSession)
	}
	if lastTime, err := memoryManager.Retrieve(ctx, "last_analysis_time"); err == nil {
		fmt.Printf("  last_analysis_time: %v\n", lastTime)
	}
	if totalSales, err := memoryManager.Retrieve(ctx, "total_sales_2024"); err == nil {
		fmt.Printf("  total_sales_2024: %v\n", totalSales)
	}

	// ========== 6. 显示可观测性数据 ==========
	fmt.Println("\n【步骤 6】可观测性数据")
	fmt.Println("────────────────────────────────────────")

	// 显示日志
	fmt.Println("\n工具调用日志:")
	logs := logCollector.GetLogs()
	for i, log := range logs {
		if i < 10 { // 只显示前10条
			fmt.Printf("  %s\n", log)
		}
	}
	if len(logs) > 10 {
		fmt.Printf("  ... 共 %d 条日志\n", len(logs))
	}

	// 显示指标
	fmt.Println("\n调用指标统计:")
	stats := metricsCollector.GetStats()
	fmt.Printf("  总调用次数: %d\n", stats.TotalCalls)
	fmt.Printf("  成功次数: %d\n", stats.SuccessCalls)
	fmt.Printf("  失败次数: %d\n", stats.FailedCalls)
	fmt.Printf("  总耗时: %v\n", stats.TotalDuration)
	if stats.TotalCalls > 0 {
		fmt.Printf("  平均耗时: %v\n", stats.TotalDuration/time.Duration(stats.TotalCalls))
	}

	// ========== 7. 显示最终报告 ==========
	fmt.Println("\n【最终报告】")
	fmt.Println("════════════════════════════════════════════════════════════════")
	if formatResult != nil {
		if reportMap, ok := formatResult.(map[string]interface{}); ok {
			if report, ok := reportMap["report"].(string); ok {
				fmt.Println(report)
			}
		}
	}

	// 清理
	_ = system.UnregisterAgent("coordinator")
	_ = system.UnregisterAgent("data-agent")
	_ = system.UnregisterAgent("analysis-agent")
}

// ============================================================================
// 综合 Agent 实现
// ============================================================================

// IntegratedAgent 综合 Agent，同时支持 LLM、工具注册表、中间件和记忆
type IntegratedAgent struct {
	*multiagent.BaseCollaborativeAgent
	llmClient     llm.Client
	registry      *tools.Registry
	memoryManager *memory.InMemoryManager
	allowedTools  []string
	systemPrompt  string
}

// NewIntegratedAgent 创建综合 Agent
func NewIntegratedAgent(
	id, description string,
	role multiagent.Role,
	system *multiagent.MultiAgentSystem,
	llmClient llm.Client,
	registry *tools.Registry,
	memoryManager *memory.InMemoryManager,
	allowedTools []string,
	systemPrompt string,
) *IntegratedAgent {
	return &IntegratedAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(id, description, role, system),
		llmClient:              llmClient,
		registry:               registry,
		memoryManager:          memoryManager,
		allowedTools:           allowedTools,
		systemPrompt:           systemPrompt,
	}
}

// ExecuteTool 执行工具
func (a *IntegratedAgent) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
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

	// 执行工具（工具已经包含中间件）
	output, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: args})
	if err != nil {
		return nil, err
	}

	return output.Result, nil
}

// AnalyzeWithLLM 使用 LLM 进行分析
func (a *IntegratedAgent) AnalyzeWithLLM(ctx context.Context, prompt string) string {
	if a.llmClient == nil {
		// 模拟 LLM 响应
		return simulateLLMResponse(a.Name(), prompt)
	}

	// 使用真实 LLM
	messages := []llm.Message{
		llm.SystemMessage(a.systemPrompt),
		llm.UserMessage(prompt),
	}

	resp, err := a.llmClient.Chat(ctx, messages)
	if err != nil {
		return fmt.Sprintf("LLM 调用失败: %v", err)
	}

	return resp.Content
}

// Collaborate 实现协作接口
func (a *IntegratedAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	assignment := &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Status:    multiagent.TaskStatusExecuting,
		StartTime: time.Now(),
	}

	// 根据输入类型决定处理方式
	switch input := task.Input.(type) {
	case string:
		// 使用 LLM 分析
		result := a.AnalyzeWithLLM(ctx, input)
		assignment.Result = result

	case map[string]interface{}:
		// 执行工具调用
		toolName, _ := input["tool"].(string)
		toolArgs, _ := input["args"].(map[string]interface{})
		result, err := a.ExecuteTool(ctx, toolName, toolArgs)
		if err != nil {
			assignment.Status = multiagent.TaskStatusFailed
			return assignment, err
		}
		assignment.Result = result

	default:
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, fmt.Errorf("不支持的输入类型")
	}

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
			collector.Log(fmt.Sprintf("[%s] 开始: %s", start.Format("15:04:05"), tool.Name()))

			output, err := next(ctx, input)

			duration := time.Since(start)
			if err != nil {
				collector.Log(fmt.Sprintf("[%s] 失败: %s (耗时: %v, 错误: %v)",
					time.Now().Format("15:04:05"), tool.Name(), duration, err))
			} else {
				collector.Log(fmt.Sprintf("[%s] 完成: %s (耗时: %v)",
					time.Now().Format("15:04:05"), tool.Name(), duration))
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

// ============================================================================
// 工具定义
// ============================================================================

func createDataFetchTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"data_fetch",
		"从数据源获取数据",
		`{"type": "object", "properties": {"source": {"type": "string"}, "type": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			source := args["source"].(string)
			dataType := args["type"].(string)

			// 模拟获取数据
			time.Sleep(50 * time.Millisecond)

			return map[string]interface{}{
				"source":   source,
				"type":     dataType,
				"data":     []float64{1200, 1500, 1800, 2100},
				"metadata": map[string]string{"period": "Q1-Q4 2024"},
			}, nil
		},
	)
}

func createDataProcessTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"data_process",
		"处理和转换数据",
		`{"type": "object", "properties": {"data": {"type": "array"}, "operation": {"type": "string"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			operation := args["operation"].(string)

			// 模拟处理数据
			time.Sleep(30 * time.Millisecond)

			switch operation {
			case "normalize":
				return map[string]interface{}{
					"operation": "normalize",
					"result":    "数据已标准化",
					"status":    "success",
				}, nil
			case "clean":
				return map[string]interface{}{
					"operation": "clean",
					"result":    "数据已清洗",
					"status":    "success",
				}, nil
			default:
				return map[string]interface{}{
					"operation": operation,
					"result":    "处理完成",
					"status":    "success",
				}, nil
			}
		},
	)
}

func createCalculatorTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"calculator",
		"执行数学计算",
		`{"type": "object", "properties": {"operation": {"type": "string"}, "values": {"type": "array"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			operation := args["operation"].(string)
			valuesRaw := args["values"].([]float64)

			var result float64
			switch operation {
			case "sum":
				for _, v := range valuesRaw {
					result += v
				}
			case "average":
				for _, v := range valuesRaw {
					result += v
				}
				if len(valuesRaw) > 0 {
					result /= float64(len(valuesRaw))
				}
			case "max":
				result = valuesRaw[0]
				for _, v := range valuesRaw {
					if v > result {
						result = v
					}
				}
			case "min":
				result = valuesRaw[0]
				for _, v := range valuesRaw {
					if v < result {
						result = v
					}
				}
			}

			return map[string]interface{}{
				"operation": operation,
				"result":    result,
			}, nil
		},
	)
}

func createFormatterTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"formatter",
		"格式化输出报告",
		`{"type": "object", "properties": {"template": {"type": "string"}, "data": {"type": "object"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			template := args["template"].(string)
			data := args["data"].(map[string]interface{})

			var report strings.Builder

			switch template {
			case "report":
				title := data["title"].(string)
				report.WriteString("┌────────────────────────────────────────┐\n")
				report.WriteString(fmt.Sprintf("│ %s\n", title))
				report.WriteString("├────────────────────────────────────────┤\n")

				if total, ok := data["total"].(map[string]interface{}); ok {
					if result, ok := total["result"].(float64); ok {
						report.WriteString(fmt.Sprintf("│ 总销售额: %.2f\n", result))
					}
				}

				if avg, ok := data["average"].(map[string]interface{}); ok {
					if result, ok := avg["result"].(float64); ok {
						report.WriteString(fmt.Sprintf("│ 平均销售额: %.2f\n", result))
					}
				}

				report.WriteString("├────────────────────────────────────────┤\n")
				report.WriteString(fmt.Sprintf("│ 分析: %s\n", data["analysis"]))
				report.WriteString("└────────────────────────────────────────┘\n")

			default:
				report.WriteString(fmt.Sprintf("数据: %v", data))
			}

			return map[string]interface{}{
				"template": template,
				"report":   report.String(),
			}, nil
		},
	)
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
			llm.WithMaxTokens(500),
		)
		if err == nil {
			return client
		}
	}

	// 其次使用 Kimi (Moonshot)
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

func simulateLLMResponse(agentName, prompt string) string {
	// 模拟 LLM 响应
	switch {
	case strings.Contains(prompt, "分析") && strings.Contains(prompt, "销售"):
		return "销售数据呈现持续增长趋势，Q4表现最佳。建议继续加大市场投入。"
	case strings.Contains(prompt, "理解") || strings.Contains(prompt, "任务"):
		return "任务已理解：需要获取销售数据、进行统计分析、生成报告。"
	default:
		return fmt.Sprintf("[%s 模拟响应] 已处理请求", agentName)
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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
