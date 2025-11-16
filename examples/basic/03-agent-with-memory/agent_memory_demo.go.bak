package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/k8s-agent/pkg/agent/core"
	"github.com/kart-io/k8s-agent/pkg/agent/memory"
	"github.com/kart-io/k8s-agent/pkg/agent/utils"
)

// AnalysisAgent 示例分析 Agent
type AnalysisAgent struct {
	*core.BaseAgent
	memory memory.Manager
}

// NewAnalysisAgent 创建分析 Agent
func NewAnalysisAgent(memMgr memory.Manager) *AnalysisAgent {
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
		ReasoningSteps: make([]core.ReasoningStep, 0),
		Timestamp:      start,
	}

	fmt.Printf("\n=== %s Executing ===\n", a.Name())
	fmt.Printf("Task: %s\n", input.Task)

	// 步骤 1: 加载历史上下文
	if input.Options.EnableMemory && input.SessionID != "" {
		stepStart := time.Now()
		history, err := a.memory.GetConversationHistory(ctx, input.SessionID, 5)

		step := core.ReasoningStep{
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

		output.ReasoningSteps = append(output.ReasoningSteps, step)
	}

	// 步骤 2: 执行分析
	stepStart := time.Now()
	analysisResult := fmt.Sprintf("Analysis completed for: %s", input.Task)

	step := core.ReasoningStep{
		Step:        2,
		Action:      "analyze",
		Description: "Perform data analysis",
		Result:      analysisResult,
		Duration:    time.Since(stepStart),
		Success:     true,
	}
	output.ReasoningSteps = append(output.ReasoningSteps, step)
	fmt.Printf("  [Step 2] %s: %s\n", step.Description, step.Result)

	// 步骤 3: 生成建议
	stepStart = time.Now()
	recommendations := []string{
		"Recommendation 1: Implement regular monitoring",
		"Recommendation 2: Set up automated alerts",
		"Recommendation 3: Review system logs daily",
	}

	step = core.ReasoningStep{
		Step:        3,
		Action:      "generate_recommendations",
		Description: "Generate actionable recommendations",
		Result:      fmt.Sprintf("Generated %d recommendations", len(recommendations)),
		Duration:    time.Since(stepStart),
		Success:     true,
	}
	output.ReasoningSteps = append(output.ReasoningSteps, step)
	fmt.Printf("  [Step 3] %s: %s\n", step.Description, step.Result)

	// 保存到记忆
	if input.Options.SaveToMemory && input.SessionID != "" {
		conv := &memory.Conversation{
			SessionID: input.SessionID,
			Role:      "assistant",
			Content:   analysisResult,
			Timestamp: time.Now(),
		}
		a.memory.AddConversation(ctx, conv)
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

// DataProcessingChain 示例数据处理 Chain
type DataProcessingChain struct {
	*core.BaseChain
}

// NewDataProcessingChain 创建数据处理 Chain
func NewDataProcessingChain() *DataProcessingChain {
	steps := []core.Step{
		&ValidationStep{},
		&TransformStep{},
		&EnrichmentStep{},
	}

	return &DataProcessingChain{
		BaseChain: core.NewBaseChain("data-processing-chain", steps),
	}
}

// ValidationStep 验证步骤
type ValidationStep struct{}

func (s *ValidationStep) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	fmt.Println("  [Chain Step 1] Validating input data...")
	data, ok := input.(map[string]interface{})
	if !ok {
		data = map[string]interface{}{"raw": input}
	}
	data["validated"] = true
	return data, nil
}

func (s *ValidationStep) Name() string        { return "validation" }
func (s *ValidationStep) Description() string { return "Validate input data" }

// TransformStep 转换步骤
type TransformStep struct{}

func (s *TransformStep) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	fmt.Println("  [Chain Step 2] Transforming data...")
	data := input.(map[string]interface{})
	data["transformed"] = true
	data["timestamp"] = time.Now()
	return data, nil
}

func (s *TransformStep) Name() string        { return "transformation" }
func (s *TransformStep) Description() string { return "Transform data format" }

// EnrichmentStep 增强步骤
type EnrichmentStep struct{}

func (s *EnrichmentStep) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	fmt.Println("  [Chain Step 3] Enriching data...")
	data := input.(map[string]interface{})
	data["enriched"] = true
	data["metadata"] = map[string]interface{}{
		"processed_by": "data-processing-chain",
		"version":      "1.0",
	}
	return data, nil
}

func (s *EnrichmentStep) Name() string        { return "enrichment" }
func (s *EnrichmentStep) Description() string { return "Enrich data with metadata" }

// SimpleOrchestrator 简单编排器示例
type SimpleOrchestrator struct {
	*core.BaseOrchestrator
}

// NewSimpleOrchestrator 创建简单编排器
func NewSimpleOrchestrator() *SimpleOrchestrator {
	return &SimpleOrchestrator{
		BaseOrchestrator: core.NewBaseOrchestrator("simple-orchestrator"),
	}
}

// Execute 执行编排任务
//
//nolint:unparam // error return required by Orchestrator interface
func (o *SimpleOrchestrator) Execute(ctx context.Context, request *core.OrchestratorRequest) (*core.OrchestratorResponse, error) {
	start := time.Now()
	response := &core.OrchestratorResponse{
		ExecutionSteps: make([]core.ExecutionStep, 0),
		StartTime:      start,
		Status:         "success",
		Metadata:       make(map[string]interface{}),
	}

	fmt.Printf("\n=== Orchestrator: %s ===\n", request.Description)

	// 步骤 1: 执行 Chain
	if chain, exists := o.GetChain("data-processing"); exists {
		stepStart := time.Now()
		chainInput := &core.ChainInput{
			Data:    request.Parameters,
			Options: core.DefaultChainOptions(),
		}

		chainOutput, err := chain.Invoke(ctx, chainInput)

		step := core.ExecutionStep{
			Step:          1,
			Name:          "data_processing",
			Type:          "chain",
			ComponentName: chain.Name(),
			Input:         chainInput,
			Output:        chainOutput,
			StartTime:     stepStart,
			EndTime:       time.Now(),
			Duration:      time.Since(stepStart),
			Status:        "success",
		}

		if err != nil {
			step.Status = "failed"
			step.Error = err.Error()
			response.Status = "failed"
		}

		response.ExecutionSteps = append(response.ExecutionSteps, step)
	}

	// 步骤 2: 执行 Agent
	if agent, exists := o.GetAgent("analysis"); exists {
		stepStart := time.Now()
		agentInput := &core.AgentInput{
			Task:      request.Description,
			Context:   request.Parameters,
			Options:   core.DefaultAgentOptions(),
			SessionID: request.SessionID,
		}

		agentOutput, err := agent.Invoke(ctx, agentInput)

		step := core.ExecutionStep{
			Step:          2,
			Name:          "analysis",
			Type:          "agent",
			ComponentName: agent.Name(),
			Input:         agentInput,
			Output:        agentOutput,
			StartTime:     stepStart,
			EndTime:       time.Now(),
			Duration:      time.Since(stepStart),
			Status:        "success",
		}

		if err != nil {
			step.Status = "failed"
			step.Error = err.Error()
			response.Status = "partial"
		} else {
			response.Result = agentOutput.Result
		}

		response.ExecutionSteps = append(response.ExecutionSteps, step)
	}

	response.EndTime = time.Now()
	response.TotalLatency = time.Since(start)
	response.Message = "Orchestration completed"

	return response, nil
}

func main() {
	fmt.Println("========================================")
	fmt.Println("  Agent Framework Example")
	fmt.Println("========================================")

	// 1. 创建记忆管理器
	fmt.Println("\n1. Creating Memory Manager...")
	memMgr := memory.NewInMemoryManager(memory.DefaultConfig())

	// 添加一些历史对话
	memMgr.AddConversation(context.Background(), &memory.Conversation{
		SessionID: "session-1",
		Role:      "user",
		Content:   "What's the system status?",
		Timestamp: time.Now().Add(-5 * time.Minute),
	})

	// 2. 创建 Agent
	fmt.Println("2. Creating Analysis Agent...")
	agent := NewAnalysisAgent(memMgr)

	// 3. 创建 Chain
	fmt.Println("3. Creating Data Processing Chain...")
	chain := NewDataProcessingChain()

	// 4. 创建 Orchestrator
	fmt.Println("4. Creating Orchestrator...")
	orchestrator := NewSimpleOrchestrator()
	orchestrator.RegisterAgent("analysis", agent)
	orchestrator.RegisterChain("data-processing", chain)

	// 5. 执行编排任务
	fmt.Println("\n5. Executing Orchestration Task...")
	request := &core.OrchestratorRequest{
		TaskID:      "task-001",
		TaskType:    "system_analysis",
		Description: "Analyze system performance and provide recommendations",
		Parameters: map[string]interface{}{
			"system": "production",
			"metrics": map[string]float64{
				"cpu_usage":    75.5,
				"memory_usage": 82.3,
				"disk_usage":   68.9,
			},
		},
		Strategy:  core.DefaultOrchestratorStrategy(),
		Options:   core.DefaultOrchestratorOptions(),
		SessionID: "session-1",
		Timestamp: time.Now(),
	}

	response, err := orchestrator.Execute(context.Background(), request)
	if err != nil {
		log.Fatal(err)
	}

	// 6. 显示结果
	fmt.Println("\n========================================")
	fmt.Println("  Execution Results")
	fmt.Println("========================================")
	fmt.Printf("Status: %s\n", response.Status)
	fmt.Printf("Total Latency: %v\n", response.TotalLatency)
	fmt.Printf("Steps Executed: %d\n", len(response.ExecutionSteps))

	fmt.Println("\nExecution Steps:")
	for _, step := range response.ExecutionSteps {
		fmt.Printf("  - Step %d: %s (%s) - %s (took %v)\n",
			step.Step, step.Name, step.Type, step.Status, step.Duration)
	}

	fmt.Println("\nFinal Result:")
	if result, ok := response.Result.(map[string]interface{}); ok {
		fmt.Printf("  Analysis: %v\n", result["analysis"])
		if recs, ok := result["recommendations"].([]string); ok {
			fmt.Println("  Recommendations:")
			for i, rec := range recs {
				fmt.Printf("    %d. %s\n", i+1, rec)
			}
		}
	}

	// 7. 演示 Prompt Builder
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

	// 8. 演示 Response Parser
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
