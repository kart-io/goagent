package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/utils"
)

// AnalysisAgent 示例分析 Agent
type AnalysisAgent struct {
	*core.BaseAgent
	memory interfaces.MemoryManager
}

// NewAnalysisAgent 创建分析 Agent
func NewAnalysisAgent(memMgr interfaces.MemoryManager) *AnalysisAgent {
	return &AnalysisAgent{
		BaseAgent: core.NewBaseAgent(
			"analysis-agent",
			"An agent that performs data analysis",
			[]string{"analysis", "reasoning", "recommendation"},
		),
		memory: memMgr,
	}
}

// Execute 执行分析任务（实现 Runnable 接口的 Invoke 方法）
func (a *AnalysisAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	start := time.Now()
	output := &core.AgentOutput{
		Steps:     make([]core.AgentStep, 0),
		Timestamp: start,
	}

	fmt.Printf("\n=== %s Executing ===\n", a.Name())
	fmt.Printf("Task: %s\n", input.Task)

	// 步骤 1: 加载历史上下文
	if input.Options.EnableMemory && input.SessionID != "" {
		stepStart := time.Now()
		history, err := a.memory.GetConversationHistory(ctx, input.SessionID, 5)

		step := core.AgentStep{
			Step:        1,
			Action:      "load_history",
			Description: "Load conversation history",
			Duration:    time.Since(stepStart),
			Success:     err == nil,
		}

		if err == nil {
			step.Result = fmt.Sprintf("Loaded %d conversations", len(history))
			fmt.Printf("  [Step 1] %s: %s\n", step.Description, step.Result)
		} else {
			step.Error = err.Error()
		}

		output.Steps = append(output.Steps, step)
	}

	// 步骤 2: 执行分析
	stepStart := time.Now()
	analysisResult := fmt.Sprintf("Analysis completed for: %s", input.Task)

	step := core.AgentStep{
		Step:        2,
		Action:      "analyze",
		Description: "Perform data analysis",
		Result:      analysisResult,
		Duration:    time.Since(stepStart),
		Success:     true,
	}
	output.Steps = append(output.Steps, step)
	fmt.Printf("  [Step 2] %s: %s\n", step.Description, step.Result)

	// 步骤 3: 生成建议
	stepStart = time.Now()
	recommendations := []string{
		"Recommendation 1: Implement regular monitoring",
		"Recommendation 2: Set up automated alerts",
		"Recommendation 3: Review system logs daily",
	}

	step = core.AgentStep{
		Step:        3,
		Action:      "generate_recommendations",
		Description: "Generate actionable recommendations",
		Result:      fmt.Sprintf("Generated %d recommendations", len(recommendations)),
		Duration:    time.Since(stepStart),
		Success:     true,
	}
	output.Steps = append(output.Steps, step)
	fmt.Printf("  [Step 3] %s: %s\n", step.Description, step.Result)

	// 保存到记忆
	if input.Options.SaveToMemory && input.SessionID != "" {
		conv := &interfaces.Conversation{
			SessionID: input.SessionID,
			Role:      "assistant",
			Content:   analysisResult,
			Timestamp: time.Now(),
		}
		_ = a.memory.AddConversation(ctx, conv)
	}

	output.Result = map[string]interface{}{
		"analysis":        analysisResult,
		"recommendations": recommendations,
	}
	output.Status = "success"
	output.Message = "Analysis completed successfully"
	output.Latency = time.Since(start)

	return output, nil
}

func main() {
	fmt.Println("========================================")
	fmt.Println("  Agent Framework Example")
	fmt.Println("========================================")

	// 1. 创建记忆管理器
	fmt.Println("\n1. Creating Memory Manager...")
	memMgr := memory.NewInMemoryManager(memory.DefaultConfig())

	// 添加一些历史对话
	if err := memMgr.AddConversation(context.Background(), &interfaces.Conversation{
		SessionID: "session-1",
		Role:      "user",
		Content:   "What's the system status?",
		Timestamp: time.Now().Add(-5 * time.Minute),
	}); err != nil {
		log.Fatalf("Failed to add conversation: %v", err)
	}

	// 2. 创建 Agent
	fmt.Println("2. Creating Analysis Agent...")
	agent := NewAnalysisAgent(memMgr)

	// 3. 执行分析任务：Agent 先从记忆读取会话历史，完成后写回
	fmt.Println("\n3. Executing Analysis Task...")
	input := &core.AgentInput{
		Task: "Analyze system performance and provide recommendations",
		Context: map[string]interface{}{
			"system": "production",
			"metrics": map[string]float64{
				"cpu_usage":    75.5,
				"memory_usage": 82.3,
				"disk_usage":   68.9,
			},
		},
		Options:   core.AgentOptions{EnableMemory: true, SaveToMemory: true},
		SessionID: "session-1",
		Timestamp: time.Now(),
	}
	output, err := agent.Invoke(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}

	// 4. 显示结果
	fmt.Println("\n========================================")
	fmt.Println("  Execution Results")
	fmt.Println("========================================")
	fmt.Printf("Status: %s\n", output.Status)
	fmt.Printf("Latency: %v\n", output.Latency)
	fmt.Printf("Steps Executed: %d\n", len(output.Steps))
	if result, ok := output.Result.(map[string]interface{}); ok {
		fmt.Printf("  Analysis: %v\n", result["analysis"])
		if recs, ok := result["recommendations"].([]string); ok {
			fmt.Println("  Recommendations:")
			for i, rec := range recs {
				fmt.Printf("    %d. %s\n", i+1, rec)
			}
		}
	}

	// 5. 演示 Prompt Builder
	fmt.Println("\n========================================")
	fmt.Println("  Prompt Builder Example")
	fmt.Println("========================================")

	prompt := utils.NewPromptBuilder().
		WithSystemPrompt("You are a system performance analyst.").
		WithContext("System CPU usage is at 75.5%").
		WithContext("Memory usage is at 82.3%").
		WithTask("Analyze the system metrics and provide optimization suggestions").
		WithConstraint("Keep recommendations actionable and specific").
		WithOutputFormat("Provide a JSON response with 'analysis' and 'suggestions' fields").
		Build()

	fmt.Println(prompt)

	// 6. 演示 Response Parser
	fmt.Println("\n========================================")
	fmt.Println("  Response Parser Example")
	fmt.Println("========================================")

	sampleResponse := `Based on the analysis, here are my findings:

## Analysis
The system is experiencing high memory usage, which could lead to performance degradation.

## Recommendations
1. Increase memory allocation by 20%
2. Implement memory caching strategy
3. Review and optimize memory-intensive processes

` + "```json\n" + `{
  "severity": "medium",
  "confidence": 0.85,
  "priority": "high"
}
` + "```"

	parser := utils.NewResponseParser(sampleResponse)

	// 提取列表
	items := parser.ExtractList()
	fmt.Println("Extracted recommendations:")
	for i, item := range items {
		fmt.Printf("  %d. %s\n", i+1, item)
	}

	// 提取 JSON
	jsonData, err := parser.ExtractJSON()
	if err == nil {
		fmt.Printf("\nExtracted JSON:\n%s\n", jsonData)
	}

	// 提取章节
	if section, err := parser.ExtractSection("Analysis"); err == nil {
		fmt.Printf("\nExtracted Analysis Section:\n%s\n", section)
	}

	fmt.Println("\n========================================")
	fmt.Println("  Example Completed Successfully!")
	fmt.Println("========================================")
}
