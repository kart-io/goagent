// Package main 演示如何在 MultiAgent 系统中使用 LLM 调用工具
//
// 本示例展示：
// 1. 定义可供 LLM 调用的工具（计算器、天气查询、搜索等）
// 2. 创建具有工具调用能力的 Agent
// 3. LLM 自动决定调用哪个工具并处理返回结果
// 4. 多 Agent 协作，每个 Agent 拥有不同的工具集
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/multiagent"
	"github.com/kart-io/goagent/tools"
	loggercore "github.com/kart-io/logger/core"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          LLM Tool Calling 多智能体协作示例                     ║")
	fmt.Println("║   展示如何让 LLM Agent 调用工具完成任务                        ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 创建日志和系统
	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	// 创建 LLM 客户端
	llmClient := createLLMClient()
	fmt.Printf("LLM 提供商: %s\n\n", llmClient.Provider())

	// 场景 1：单 Agent 工具调用
	fmt.Println("【场景 1】单 Agent 工具调用")
	fmt.Println("════════════════════════════════════════════════════════════════")
	runSingleAgentToolCalling(ctx, system, llmClient)

	// 场景 2：多 Agent 专业工具协作
	fmt.Println("\n【场景 2】多 Agent 专业工具协作")
	fmt.Println("════════════════════════════════════════════════════════════════")
	runMultiAgentToolCollaboration(ctx, system, llmClient)

	// 场景 3：工具链式调用
	fmt.Println("\n【场景 3】工具链式调用（Pipeline）")
	fmt.Println("════════════════════════════════════════════════════════════════")
	runToolChainPipeline(ctx, system, llmClient)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// 场景 1：单 Agent 工具调用
// ============================================================================

func runSingleAgentToolCalling(ctx context.Context, system *multiagent.MultiAgentSystem, llmClient llm.Client) {
	fmt.Println("\n场景描述: 单个 Agent 配备多个工具，根据用户问题自动选择合适的工具")
	fmt.Println()

	// 创建工具集
	toolSet := createBasicToolSet()
	fmt.Println("可用工具:")
	for _, tool := range toolSet {
		fmt.Printf("  - %s: %s\n", tool.Name(), tool.Description())
	}
	fmt.Println()

	// 创建带工具的 Agent
	agent := NewToolAgent(
		"assistant",
		"通用助手",
		multiagent.RoleWorker,
		system,
		llmClient,
		toolSet,
		"你是一个智能助手。你必须使用提供的工具来回答问题。当用户问数学问题时使用 calculator 工具，问天气时使用 weather 工具，问搜索时使用 search 工具，问时间时使用 current_time 工具。请始终调用工具而不是直接回答。",
	)

	if err := system.RegisterAgent("assistant", agent); err != nil {
		fmt.Printf("注册 Agent 失败: %v\n", err)
		return
	}
	fmt.Println("✓ 注册工具 Agent: assistant")

	// 测试问题
	questions := []string{
		"计算 15 乘以 28 等于多少？",
		"今天北京的天气怎么样？",
		"搜索关于 Go 语言并发编程的资料",
		"现在是几点钟？",
	}

	for i, question := range questions {
		fmt.Printf("\n问题 %d: %s\n", i+1, question)
		fmt.Println("────────────────────────────────────────")

		task := &multiagent.CollaborativeTask{
			ID:          fmt.Sprintf("question-%d", i+1),
			Name:        "工具调用测试",
			Type:        multiagent.CollaborationTypeParallel,
			Input:       question,
			Assignments: make(map[string]multiagent.Assignment),
		}

		result, err := system.ExecuteTask(ctx, task)
		if err != nil {
			fmt.Printf("执行失败: %v\n", err)
			continue
		}

		if agentResult, ok := result.Results["assistant"]; ok {
			if resultMap, ok := agentResult.(map[string]interface{}); ok {
				fmt.Printf("回答: %v\n", resultMap["response"])
				if toolsUsed, ok := resultMap["tools_used"].([]string); ok && len(toolsUsed) > 0 {
					fmt.Printf("使用的工具: %v\n", toolsUsed)
				}
			}
		}
	}

	// 清理
	_ = system.UnregisterAgent("assistant")
}

// ============================================================================
// 场景 2：多 Agent 专业工具协作
// ============================================================================

func runMultiAgentToolCollaboration(ctx context.Context, system *multiagent.MultiAgentSystem, llmClient llm.Client) {
	fmt.Println("\n场景描述: 多个专业 Agent 各自拥有不同的工具集，协作完成复杂任务")
	fmt.Println()

	// 创建数学 Agent（拥有计算工具）
	mathTools := []interfaces.Tool{
		createCalculatorTool(),
		createAdvancedMathTool(),
	}
	mathAgent := NewToolAgent(
		"math-expert",
		"数学专家",
		multiagent.RoleSpecialist,
		system,
		llmClient,
		mathTools,
		"你是数学专家。你必须使用 calculator 或 advanced_math 工具来计算。不要直接回答，请调用工具。",
	)

	// 创建信息 Agent（拥有搜索和天气工具）
	infoTools := []interfaces.Tool{
		createSearchTool(),
		createWeatherTool(),
	}
	infoAgent := NewToolAgent(
		"info-expert",
		"信息专家",
		multiagent.RoleSpecialist,
		system,
		llmClient,
		infoTools,
		"你是信息专家。你必须使用 weather 或 search 工具来获取信息。不要直接回答，请调用工具。",
	)

	// 创建时间 Agent（拥有时间相关工具）
	timeTools := []interfaces.Tool{
		createTimeTool(),
		createTimerTool(),
	}
	timeAgent := NewToolAgent(
		"time-expert",
		"时间专家",
		multiagent.RoleSpecialist,
		system,
		llmClient,
		timeTools,
		"你是时间专家。你必须使用 current_time 或 timer 工具来处理时间问题。不要直接回答，请调用工具。",
	)

	// 注册 Agent
	_ = system.RegisterAgent("math-expert", mathAgent)
	_ = system.RegisterAgent("info-expert", infoAgent)
	_ = system.RegisterAgent("time-expert", timeAgent)

	fmt.Println("注册的专家 Agent:")
	fmt.Println("  ✓ math-expert: 数学专家 (calculator, advanced_math)")
	fmt.Println("  ✓ info-expert: 信息专家 (search, weather)")
	fmt.Println("  ✓ time-expert: 时间专家 (current_time, timer)")
	fmt.Println()

	// 复杂任务：需要多个专家协作
	complexTask := &multiagent.CollaborativeTask{
		ID:          "complex-task-001",
		Name:        "多专家协作任务",
		Description: "需要数学计算、信息查询和时间处理的综合任务",
		Type:        multiagent.CollaborationTypeParallel,
		Input: map[string]interface{}{
			"math_question": "计算圆周率的前5位小数乘以100",
			"info_question": "查询今天上海的天气",
			"time_question": "告诉我现在的时间",
		},
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Println("执行综合任务...")
	result, err := system.ExecuteTask(ctx, complexTask)
	if err != nil {
		fmt.Printf("任务执行失败: %v\n", err)
		return
	}

	fmt.Println("\n各专家执行结果:")
	fmt.Println("────────────────────────────────────────")
	for agentID, agentResult := range result.Results {
		fmt.Printf("\n【%s】\n", agentID)
		if resultMap, ok := agentResult.(map[string]interface{}); ok {
			fmt.Printf("  响应: %v\n", resultMap["response"])
			if toolsUsed, ok := resultMap["tools_used"].([]string); ok && len(toolsUsed) > 0 {
				fmt.Printf("  使用工具: %v\n", toolsUsed)
			}
		}
	}

	// 清理
	_ = system.UnregisterAgent("math-expert")
	_ = system.UnregisterAgent("info-expert")
	_ = system.UnregisterAgent("time-expert")
}

// ============================================================================
// 场景 3：工具链式调用
// ============================================================================

func runToolChainPipeline(ctx context.Context, system *multiagent.MultiAgentSystem, llmClient llm.Client) {
	fmt.Println("\n场景描述: 工具的输出作为下一个工具的输入，形成处理管道")
	fmt.Println()

	// 创建数据处理工具链
	stage1Tools := []interfaces.Tool{createDataFetchTool()}
	stage2Tools := []interfaces.Tool{createDataProcessTool()}
	stage3Tools := []interfaces.Tool{createDataFormatTool()}

	// 创建三个阶段的 Agent
	fetchAgent := NewToolAgent("data-fetcher", "数据获取", multiagent.RoleWorker, system, llmClient, stage1Tools,
		"你负责获取原始数据。使用 data_fetch 工具获取数据。")
	processAgent := NewToolAgent("data-processor", "数据处理", multiagent.RoleWorker, system, llmClient, stage2Tools,
		"你负责处理数据。使用 data_process 工具处理传入的数据。")
	formatAgent := NewToolAgent("data-formatter", "数据格式化", multiagent.RoleWorker, system, llmClient, stage3Tools,
		"你负责格式化输出。使用 data_format 工具将数据格式化为最终输出。")

	_ = system.RegisterAgent("data-fetcher", fetchAgent)
	_ = system.RegisterAgent("data-processor", processAgent)
	_ = system.RegisterAgent("data-formatter", formatAgent)

	fmt.Println("Pipeline 阶段:")
	fmt.Println("  Stage 1: data-fetcher  [data_fetch]")
	fmt.Println("  Stage 2: data-processor [data_process]")
	fmt.Println("  Stage 3: data-formatter [data_format]")
	fmt.Println()

	// 执行 Pipeline 任务
	pipelineTask := &multiagent.CollaborativeTask{
		ID:   "pipeline-001",
		Name: "数据处理管道",
		Type: multiagent.CollaborationTypePipeline,
		Input: []interface{}{
			map[string]interface{}{"name": "fetch", "source": "sales_2024"},
			map[string]interface{}{"name": "process", "operation": "aggregate"},
			map[string]interface{}{"name": "format", "output_type": "json"},
		},
		Assignments: make(map[string]multiagent.Assignment),
		Results:     make(map[string]interface{}),
	}

	fmt.Println("执行数据处理管道...")
	result, err := system.ExecuteTask(ctx, pipelineTask)
	if err != nil {
		fmt.Printf("Pipeline 执行失败: %v\n", err)
		return
	}

	fmt.Printf("✓ Pipeline 完成，状态: %s\n", result.Status)
	fmt.Println("\n各阶段结果:")
	for stageName, stageResult := range result.Results {
		fmt.Printf("  %s: %v\n", stageName, stageResult)
	}

	// 清理
	_ = system.UnregisterAgent("data-fetcher")
	_ = system.UnregisterAgent("data-processor")
	_ = system.UnregisterAgent("data-formatter")
}

// ============================================================================
// ToolAgent - 具有工具调用能力的 Agent
// ============================================================================

// ToolAgent 是具有工具调用能力的协作 Agent
type ToolAgent struct {
	*multiagent.BaseCollaborativeAgent
	llmClient    llm.Client
	tools        []interfaces.Tool
	toolMap      map[string]interfaces.Tool
	systemPrompt string
}

// NewToolAgent 创建工具 Agent
func NewToolAgent(
	id string,
	description string,
	role multiagent.Role,
	system *multiagent.MultiAgentSystem,
	llmClient llm.Client,
	toolSet []interfaces.Tool,
	systemPrompt string,
) *ToolAgent {
	agent := &ToolAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(id, description, role, system),
		llmClient:              llmClient,
		tools:                  toolSet,
		toolMap:                make(map[string]interfaces.Tool),
		systemPrompt:           systemPrompt,
	}

	// 建立工具映射
	for _, tool := range toolSet {
		agent.toolMap[tool.Name()] = tool
	}

	return agent
}

// Collaborate 实现协作方法
func (a *ToolAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	// 构建用户提示
	var userPrompt string
	switch input := task.Input.(type) {
	case string:
		userPrompt = input
	case map[string]interface{}:
		// 根据 Agent 角色选择对应的问题
		if strings.Contains(a.Name(), "math") {
			if q, ok := input["math_question"].(string); ok {
				userPrompt = q
			}
		} else if strings.Contains(a.Name(), "info") {
			if q, ok := input["info_question"].(string); ok {
				userPrompt = q
			}
		} else if strings.Contains(a.Name(), "time") {
			if q, ok := input["time_question"].(string); ok {
				userPrompt = q
			}
		} else if _, ok := input["stage"]; ok {
			// Pipeline 场景：从 config 中提取参数
			if config, ok := input["config"].(map[string]interface{}); ok {
				userPrompt = a.buildPipelinePrompt(config)
			} else {
				stageName, _ := input["stage_name"].(string)
				userPrompt = fmt.Sprintf("处理阶段 %s 的任务", stageName)
			}
		} else {
			// 检测 Pipeline 场景的工具参数输入
			userPrompt = a.buildPipelinePrompt(input)
		}
	default:
		data, _ := json.Marshal(input)
		userPrompt = string(data)
	}

	// 尝试使用工具调用
	toolCaller := llm.AsToolCaller(a.llmClient)
	var response string
	var toolsUsed []string

	if toolCaller != nil && len(a.tools) > 0 {
		// 使用带工具的生成
		result, err := toolCaller.GenerateWithTools(ctx, a.buildPrompt(userPrompt), a.tools)
		if err != nil {
			// 降级到普通生成
			resp, err := a.llmClient.Chat(ctx, []llm.Message{
				llm.SystemMessage(a.systemPrompt),
				llm.UserMessage(userPrompt),
			})
			if err != nil {
				return nil, err
			}
			response = resp.Content
		} else {
			// 处理工具调用
			if len(result.ToolCalls) > 0 {
				response, toolsUsed = a.executeToolCalls(ctx, result.ToolCalls, userPrompt)
			} else {
				// LLM 选择直接回答而不调用工具
				response = result.Content
			}
		}
	} else {
		// 不支持工具调用，使用普通生成
		resp, err := a.llmClient.Chat(ctx, []llm.Message{
			llm.SystemMessage(a.systemPrompt),
			llm.UserMessage(userPrompt),
		})
		if err != nil {
			return nil, err
		}
		response = resp.Content
	}

	return &multiagent.Assignment{
		AgentID: a.Name(),
		Role:    a.GetRole(),
		Status:  multiagent.TaskStatusCompleted,
		Result: map[string]interface{}{
			"response":   response,
			"tools_used": toolsUsed,
		},
	}, nil
}

// buildPrompt 构建完整提示
func (a *ToolAgent) buildPrompt(userPrompt string) string {
	// 构建工具描述
	var toolDescs []string
	for _, tool := range a.tools {
		toolDescs = append(toolDescs, fmt.Sprintf("- %s: %s", tool.Name(), tool.Description()))
	}

	return fmt.Sprintf(`%s

你可以使用以下工具:
%s

重要：你必须调用上述工具来回答问题，不要直接生成答案。

用户问题: %s`, a.systemPrompt, strings.Join(toolDescs, "\n"), userPrompt)
}

// buildPipelinePrompt 构建 Pipeline 场景的明确提示
// 根据输入参数匹配工具，生成明确的工具调用指令
func (a *ToolAgent) buildPipelinePrompt(input map[string]interface{}) string {
	// 获取第一个工具名称（Pipeline 场景每个 Agent 通常只有一个工具）
	if len(a.tools) == 0 {
		data, _ := json.Marshal(input)
		return string(data)
	}

	tool := a.tools[0]
	toolName := tool.Name()

	// 构建参数 JSON
	params := make(map[string]interface{})
	for key, value := range input {
		if key == "name" {
			continue // 跳过任务名称
		}
		params[key] = value
	}

	// 生成明确的工具调用指令，包含 JSON 格式的参数
	paramsJSON, _ := json.Marshal(params)
	return fmt.Sprintf(`【强制指令】你必须立即调用 %s 工具。

工具参数（JSON格式）: %s

重要提示：
1. 不要回复任何文字说明
2. 不要询问更多信息
3. 直接调用 %s 工具，使用上述参数
4. 这是一个自动化流程，无需人工确认`, toolName, string(paramsJSON), toolName)
}

// executeToolCalls 执行工具调用
func (a *ToolAgent) executeToolCalls(ctx context.Context, toolCalls []llm.ToolCall, originalPrompt string) (string, []string) {
	var results []string
	var toolsUsed []string

	for _, tc := range toolCalls {
		toolName := tc.Function.Name

		tool, exists := a.toolMap[toolName]
		if !exists {
			results = append(results, fmt.Sprintf("工具 %s 不存在", toolName))
			continue
		}

		// 解析参数（从 Function.Arguments JSON 字符串解析）
		var args map[string]interface{}
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}

		// 执行工具
		output, err := tool.Invoke(ctx, &interfaces.ToolInput{
			Args:    args,
			Context: ctx,
		})

		if err != nil {
			results = append(results, fmt.Sprintf("[%s] 执行失败: %v", toolName, err))
		} else if output.Success {
			results = append(results, fmt.Sprintf("[%s] %v", toolName, output.Result))
			toolsUsed = append(toolsUsed, toolName)
		} else {
			results = append(results, fmt.Sprintf("[%s] 失败: %s", toolName, output.Error))
		}
	}

	return strings.Join(results, "\n"), toolsUsed
}

// ============================================================================
// 工具定义
// ============================================================================

// createBasicToolSet 创建基础工具集
func createBasicToolSet() []interfaces.Tool {
	return []interfaces.Tool{
		createCalculatorTool(),
		createWeatherTool(),
		createSearchTool(),
		createTimeTool(),
	}
}

// createCalculatorTool 创建计算器工具
func createCalculatorTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"calculator",
		"执行基本数学计算（加减乘除）",
		`{
			"type": "object",
			"properties": {
				"operation": {"type": "string", "enum": ["add", "subtract", "multiply", "divide"], "description": "运算类型"},
				"a": {"type": "number", "description": "第一个数"},
				"b": {"type": "number", "description": "第二个数"}
			},
			"required": ["operation", "a", "b"]
		}`,
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
			default:
				return nil, fmt.Errorf("未知运算: %s", op)
			}
			return map[string]interface{}{
				"operation": op,
				"a":         a,
				"b":         b,
				"result":    result,
			}, nil
		},
	)
}

// createAdvancedMathTool 创建高级数学工具
func createAdvancedMathTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"advanced_math",
		"执行高级数学运算（幂、开方、三角函数等）",
		`{
			"type": "object",
			"properties": {
				"operation": {"type": "string", "enum": ["power", "sqrt", "sin", "cos", "tan", "log", "pi"], "description": "运算类型"},
				"value": {"type": "number", "description": "输入值（某些运算可选）"}
			},
			"required": ["operation"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			op := args["operation"].(string)
			value := 0.0
			if v, ok := args["value"].(float64); ok {
				value = v
			}

			var result float64
			switch op {
			case "power":
				exp := 2.0
				if e, ok := args["exponent"].(float64); ok {
					exp = e
				}
				result = math.Pow(value, exp)
			case "sqrt":
				result = math.Sqrt(value)
			case "sin":
				result = math.Sin(value)
			case "cos":
				result = math.Cos(value)
			case "tan":
				result = math.Tan(value)
			case "log":
				result = math.Log(value)
			case "pi":
				result = math.Pi
			default:
				return nil, fmt.Errorf("未知运算: %s", op)
			}
			return map[string]interface{}{
				"operation": op,
				"input":     value,
				"result":    result,
			}, nil
		},
	)
}

// createWeatherTool 创建天气查询工具
func createWeatherTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"weather",
		"查询指定城市的天气信息",
		`{
			"type": "object",
			"properties": {
				"city": {"type": "string", "description": "城市名称"}
			},
			"required": ["city"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			city := args["city"].(string)
			// 模拟天气数据
			weather := map[string]interface{}{
				"city":        city,
				"temperature": 22,
				"condition":   "晴朗",
				"humidity":    65,
				"wind":        "东南风 3级",
				"aqi":         45,
				"suggestion":  "适宜户外活动",
			}
			return weather, nil
		},
	)
}

// createSearchTool 创建搜索工具
func createSearchTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"search",
		"搜索相关信息和资料",
		`{
			"type": "object",
			"properties": {
				"query": {"type": "string", "description": "搜索关键词"},
				"limit": {"type": "integer", "description": "返回结果数量", "default": 3}
			},
			"required": ["query"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			query := args["query"].(string)
			limit := 3
			if l, ok := args["limit"].(float64); ok {
				limit = int(l)
			}

			// 模拟搜索结果
			results := []map[string]interface{}{}
			for i := 0; i < limit; i++ {
				results = append(results, map[string]interface{}{
					"title":   fmt.Sprintf("%s 相关文章 %d", query, i+1),
					"url":     fmt.Sprintf("https://example.com/%s/%d", strings.ReplaceAll(query, " ", "-"), i+1),
					"snippet": fmt.Sprintf("这是关于 %s 的详细介绍...", query),
				})
			}
			return map[string]interface{}{
				"query":   query,
				"count":   len(results),
				"results": results,
			}, nil
		},
	)
}

// createTimeTool 创建时间工具
func createTimeTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"current_time",
		"获取当前时间",
		`{
			"type": "object",
			"properties": {
				"timezone": {"type": "string", "description": "时区（可选）", "default": "Asia/Shanghai"}
			}
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			tz := "Asia/Shanghai"
			if t, ok := args["timezone"].(string); ok {
				tz = t
			}

			loc, err := time.LoadLocation(tz)
			if err != nil {
				loc = time.Local
			}

			now := time.Now().In(loc)
			return map[string]interface{}{
				"timezone": tz,
				"time":     now.Format("15:04:05"),
				"date":     now.Format("2006-01-02"),
				"datetime": now.Format("2006-01-02 15:04:05"),
				"weekday":  now.Weekday().String(),
				"unix":     now.Unix(),
			}, nil
		},
	)
}

// createTimerTool 创建计时器工具
func createTimerTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"timer",
		"设置定时器或倒计时",
		`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["countdown", "elapsed"], "description": "操作类型"},
				"duration_seconds": {"type": "integer", "description": "持续时间（秒）"}
			},
			"required": ["action"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			action := args["action"].(string)
			duration := 60
			if d, ok := args["duration_seconds"].(float64); ok {
				duration = int(d)
			}

			switch action {
			case "countdown":
				return map[string]interface{}{
					"action":   "countdown",
					"duration": duration,
					"message":  fmt.Sprintf("已设置 %d 秒倒计时", duration),
				}, nil
			case "elapsed":
				return map[string]interface{}{
					"action":  "elapsed",
					"elapsed": 0,
					"message": "计时器已启动",
				}, nil
			default:
				return nil, fmt.Errorf("未知操作: %s", action)
			}
		},
	)
}

// createDataFetchTool 创建数据获取工具
func createDataFetchTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"data_fetch",
		"从数据源获取原始数据",
		`{
			"type": "object",
			"properties": {
				"source": {"type": "string", "description": "数据源名称"}
			},
			"required": ["source"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			source := args["source"].(string)
			// 模拟数据获取
			return map[string]interface{}{
				"source": source,
				"data": []map[string]interface{}{
					{"id": 1, "value": 100, "category": "A"},
					{"id": 2, "value": 200, "category": "B"},
					{"id": 3, "value": 150, "category": "A"},
				},
				"fetched_at": time.Now().Format(time.RFC3339),
			}, nil
		},
	)
}

// createDataProcessTool 创建数据处理工具
func createDataProcessTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"data_process",
		"处理和转换数据",
		`{
			"type": "object",
			"properties": {
				"operation": {"type": "string", "enum": ["aggregate", "filter", "transform"], "description": "处理操作"}
			},
			"required": ["operation"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			operation := args["operation"].(string)
			// 模拟数据处理
			return map[string]interface{}{
				"operation": operation,
				"processed": map[string]interface{}{
					"total":   450,
					"average": 150,
					"count":   3,
				},
				"processed_at": time.Now().Format(time.RFC3339),
			}, nil
		},
	)
}

// createDataFormatTool 创建数据格式化工具
func createDataFormatTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"data_format",
		"格式化输出数据",
		`{
			"type": "object",
			"properties": {
				"output_type": {"type": "string", "enum": ["json", "csv", "table"], "description": "输出格式"}
			},
			"required": ["output_type"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			outputType := args["output_type"].(string)
			// 模拟格式化
			return map[string]interface{}{
				"format":       outputType,
				"formatted_at": time.Now().Format(time.RFC3339),
				"output":       fmt.Sprintf("数据已格式化为 %s 格式", outputType),
			}, nil
		},
	)
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
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.7),
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
			llm.WithTemperature(0.7),
		)
		if err == nil {
			return client
		}
	}

	// 返回 Mock 客户端
	return &MockToolClient{}
}

// MockToolClient 模拟的工具调用客户端
type MockToolClient struct{}

func (m *MockToolClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content:  "这是模拟响应",
		Model:    "mock-model",
		Provider: "mock",
	}, nil
}

func (m *MockToolClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content:  "这是模拟的聊天响应",
		Model:    "mock-model",
		Provider: "mock",
	}, nil
}

func (m *MockToolClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockToolClient) IsAvailable() bool {
	return true
}

// GenerateWithTools 实现工具调用接口
func (m *MockToolClient) GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*llm.ToolCallResponse, error) {
	// 模拟工具调用决策
	var toolCalls []llm.ToolCall

	// 提取用户问题部分（在 "用户问题:" 之后的内容）
	userQuestion := prompt
	if idx := strings.Index(prompt, "用户问题:"); idx != -1 {
		userQuestion = prompt[idx:]
	}

	// 根据用户问题判断要调用的工具
	if strings.Contains(userQuestion, "几点") || (strings.Contains(userQuestion, "现在") && strings.Contains(userQuestion, "时间")) {
		toolCalls = append(toolCalls, llm.ToolCall{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "current_time",
				Arguments: `{}`,
			},
		})
	} else if strings.Contains(userQuestion, "天气") {
		city := "北京"
		if strings.Contains(userQuestion, "上海") {
			city = "上海"
		}
		toolCalls = append(toolCalls, llm.ToolCall{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "weather",
				Arguments: fmt.Sprintf(`{"city": "%s"}`, city),
			},
		})
	} else if strings.Contains(userQuestion, "搜索") {
		toolCalls = append(toolCalls, llm.ToolCall{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "search",
				Arguments: `{"query": "Go 语言并发编程", "limit": 3}`,
			},
		})
	} else if strings.Contains(userQuestion, "乘") || strings.Contains(userQuestion, "加") || strings.Contains(userQuestion, "减") || strings.Contains(userQuestion, "除") || strings.Contains(userQuestion, "计算") {
		toolCalls = append(toolCalls, llm.ToolCall{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "calculator",
				Arguments: `{"operation": "multiply", "a": 15, "b": 28}`,
			},
		})
	} else if strings.Contains(userQuestion, "圆周率") || strings.Contains(strings.ToLower(userQuestion), "pi") {
		toolCalls = append(toolCalls, llm.ToolCall{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "advanced_math",
				Arguments: `{"operation": "pi"}`,
			},
		})
	} else if strings.Contains(prompt, "data_fetch") || strings.Contains(prompt, "fetch") || strings.Contains(prompt, "获取") {
		toolCalls = append(toolCalls, llm.ToolCall{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "data_fetch",
				Arguments: `{"source": "sales_2024"}`,
			},
		})
	} else if strings.Contains(prompt, "data_process") || strings.Contains(prompt, "process") || strings.Contains(prompt, "处理") {
		toolCalls = append(toolCalls, llm.ToolCall{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "data_process",
				Arguments: `{"operation": "aggregate"}`,
			},
		})
	} else if strings.Contains(prompt, "data_format") || strings.Contains(prompt, "format") || strings.Contains(prompt, "格式化") {
		toolCalls = append(toolCalls, llm.ToolCall{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "data_format",
				Arguments: `{"output_type": "json"}`,
			},
		})
	}

	if len(toolCalls) > 0 {
		return &llm.ToolCallResponse{
			Content:   "",
			ToolCalls: toolCalls,
		}, nil
	}

	return &llm.ToolCallResponse{
		Content: "我无法确定需要使用哪个工具来回答这个问题。",
	}, nil
}

// StreamWithTools 实现流式工具调用接口
func (m *MockToolClient) StreamWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (<-chan llm.ToolChunk, error) {
	ch := make(chan llm.ToolChunk)
	go func() {
		defer close(ch)
		ch <- llm.ToolChunk{
			Type:  "content",
			Value: "流式工具调用响应",
		}
	}()
	return ch, nil
}

// ============================================================================
// 简单日志实现
// ============================================================================

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
	fmt.Print("[DEBUG] ", msg, " ", formatKV(keysAndValues), "\n")
}

func (l *simpleLogger) Infow(msg string, keysAndValues ...interface{}) {
	fmt.Print("[INFO] ", msg, " ", formatKV(keysAndValues), "\n")
}

func (l *simpleLogger) Warnw(msg string, keysAndValues ...interface{}) {
	fmt.Print("[WARN] ", msg, " ", formatKV(keysAndValues), "\n")
}

func (l *simpleLogger) Errorw(msg string, keysAndValues ...interface{}) {
	fmt.Print("[ERROR] ", msg, " ", formatKV(keysAndValues), "\n")
}

func (l *simpleLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	fmt.Print("[FATAL] ", msg, " ", formatKV(keysAndValues), "\n")
	os.Exit(1)
}
func (l *simpleLogger) With(keyValues ...interface{}) loggercore.Logger { return l }
func (l *simpleLogger) WithCtx(ctx context.Context, keyValues ...interface{}) loggercore.Logger {
	return l
}
func (l *simpleLogger) WithCallerSkip(skip int) loggercore.Logger { return l }
func (l *simpleLogger) SetLevel(level loggercore.Level)           {}
func (l *simpleLogger) Sync() error                               { return nil }
func (l *simpleLogger) Flush() error                              { return nil }

func formatKV(keysAndValues []interface{}) string {
	if len(keysAndValues) == 0 {
		return ""
	}
	result := "["
	for i := 0; i < len(keysAndValues); i += 2 {
		if i > 0 {
			result += " "
		}
		if i+1 < len(keysAndValues) {
			result += fmt.Sprintf("%v %v", keysAndValues[i], keysAndValues[i+1])
		}
	}
	return result + "]"
}
