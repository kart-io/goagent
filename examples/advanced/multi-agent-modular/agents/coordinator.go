package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/llm"
)

// Coordinator manages the multi-agent workflow
type Coordinator struct {
	analysisAgent  *AnalysisAgent
	strategyAgent  *StrategyAgent
	executionAgent *ExecutionAgent
	llmClient      llm.Client
}

// NewCoordinator creates a new coordinator
func NewCoordinator(llmClient llm.Client) *Coordinator {
	return &Coordinator{
		analysisAgent:  NewAnalysisAgent(llmClient),
		strategyAgent:  NewStrategyAgent(llmClient),
		executionAgent: NewExecutionAgent(llmClient),
		llmClient:      llmClient,
	}
}

// WorkflowResult contains the results from all agents
type WorkflowResult struct {
	Task        string                 `json:"task"`
	Analysis    string                 `json:"analysis"`
	Strategy    string                 `json:"strategy"`
	Execution   string                 `json:"execution"`
	ToolResults map[string]interface{} `json:"tool_results"`
	Summary     string                 `json:"summary"`
	Duration    time.Duration          `json:"duration"`
	Status      string                 `json:"status"`
}

// ExecuteWorkflow runs the complete multi-agent workflow
func (c *Coordinator) ExecuteWorkflow(ctx context.Context, task string) (*WorkflowResult, error) {
	startTime := time.Now()
	result := &WorkflowResult{
		Task:        task,
		ToolResults: make(map[string]interface{}),
		Status:      "in_progress",
	}

	// Phase 1: Analysis
	fmt.Printf("\n🔍 %s: Analyzing task...\n", c.analysisAgent.Name())
	analysis, err := c.analysisAgent.Analyze(ctx, task)
	if err != nil {
		result.Status = "failed_analysis"
		return result, fmt.Errorf("analysis phase failed: %w", err)
	}
	result.Analysis = analysis
	fmt.Printf("   ✓ Analysis complete: %s\n", truncate(analysis, 100))

	// Phase 2: Strategy
	fmt.Printf("\n📋 %s: Formulating strategy...\n", c.strategyAgent.Name())
	strategy, err := c.strategyAgent.FormulateStrategy(ctx, analysis)
	if err != nil {
		result.Status = "failed_strategy"
		return result, fmt.Errorf("strategy phase failed: %w", err)
	}
	result.Strategy = strategy
	fmt.Printf("   ✓ Strategy formulated: %s\n", truncate(strategy, 100))

	// Phase 3: Execution
	fmt.Printf("\n⚡ %s: Executing strategy...\n", c.executionAgent.Name())
	execution, err := c.executionAgent.Execute(ctx, strategy)
	if err != nil {
		result.Status = "failed_execution"
		return result, fmt.Errorf("execution phase failed: %w", err)
	}
	result.Execution = execution
	fmt.Printf("   ✓ Execution plan created: %s\n", truncate(execution, 100))

	// Execute with tools
	fmt.Println("\n🛠️  Executing with tools...")
	toolsToUse := []string{"http_request", "data_processor", "monitor"}
	toolResults, err := c.executionAgent.ExecuteWithTools(ctx, execution, toolsToUse)
	if err != nil {
		fmt.Printf("   ⚠️  Tool execution had issues: %v\n", err)
	} else {
		result.ToolResults = toolResults
		fmt.Printf("   ✓ Tools executed successfully\n")
	}

	// Generate summary
	result.Summary = c.generateSummary(result)
	result.Duration = time.Since(startTime)
	result.Status = "completed"

	return result, nil
}

// ExecuteParallel runs analysis and initial strategy in parallel
func (c *Coordinator) ExecuteParallel(ctx context.Context, tasks []string) ([]*WorkflowResult, error) {
	results := make([]*WorkflowResult, len(tasks))
	errChan := make(chan error, len(tasks))

	for i, task := range tasks {
		go func(index int, t string) {
			result, err := c.ExecuteWorkflow(ctx, t)
			results[index] = result
			errChan <- err
		}(i, task)
	}

	// Collect errors
	var firstErr error
	for i := 0; i < len(tasks); i++ {
		if err := <-errChan; err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return results, firstErr
}

// ExecuteWithFeedback runs the workflow with iterative refinement
func (c *Coordinator) ExecuteWithFeedback(ctx context.Context, task string, maxIterations int) (*WorkflowResult, error) {
	var result *WorkflowResult
	var err error

	for i := 0; i < maxIterations; i++ {
		fmt.Printf("\n=== Iteration %d ===\n", i+1)

		result, err = c.ExecuteWorkflow(ctx, task)
		if err != nil {
			return result, err
		}

		// Validate results
		isValid, feedback, valErr := c.executionAgent.ValidateResults(ctx, task, result.Execution)
		if valErr != nil {
			fmt.Printf("Validation error: %v\n", valErr)
			break
		}

		if isValid {
			fmt.Println("✓ Results validated successfully")
			break
		}

		// Refine strategy based on feedback
		fmt.Printf("⚠️  Validation issues found. Refining strategy...\n")
		refinedStrategy, refineErr := c.strategyAgent.OptimizeStrategy(ctx, result.Strategy, feedback)
		if refineErr != nil {
			return result, fmt.Errorf("strategy refinement failed: %w", refineErr)
		}

		// Update task for next iteration
		task = fmt.Sprintf("Refined task based on feedback: %s\nOriginal: %s", feedback, task)
		result.Strategy = refinedStrategy
	}

	return result, nil
}

// generateSummary creates a summary of the workflow results
func (c *Coordinator) generateSummary(result *WorkflowResult) string {
	summary := fmt.Sprintf(`Multi-Agent Workflow Summary
============================
Task: %s
Duration: %s
Status: %s

Key Points:
- Analysis identified key patterns and risks
- Strategy formulated with phased approach
- Execution plan created with tool integration
- %d tools executed successfully

Next Steps:
- Monitor execution progress
- Validate outcomes against expectations
- Iterate if needed based on results`,
		truncate(result.Task, 50),
		result.Duration,
		result.Status,
		len(result.ToolResults),
	)

	return summary
}

// ExecuteCustomWorkflow allows custom agent sequencing
func (c *Coordinator) ExecuteCustomWorkflow(ctx context.Context, task string, sequence []string) (*WorkflowResult, error) {
	result := &WorkflowResult{
		Task:        task,
		ToolResults: make(map[string]interface{}),
		Status:      "in_progress",
	}

	var previousOutput string = task

	for _, agentName := range sequence {
		switch agentName {
		case "analysis":
			output, err := c.analysisAgent.Analyze(ctx, previousOutput)
			if err != nil {
				return result, fmt.Errorf("analysis failed: %w", err)
			}
			result.Analysis = output
			previousOutput = output

		case "strategy":
			output, err := c.strategyAgent.FormulateStrategy(ctx, previousOutput)
			if err != nil {
				return result, fmt.Errorf("strategy failed: %w", err)
			}
			result.Strategy = output
			previousOutput = output

		case "execution":
			output, err := c.executionAgent.Execute(ctx, previousOutput)
			if err != nil {
				return result, fmt.Errorf("execution failed: %w", err)
			}
			result.Execution = output
			previousOutput = output

		default:
			return result, fmt.Errorf("unknown agent: %s", agentName)
		}

		fmt.Printf("✓ %s agent completed\n", agentName)
	}

	result.Status = "completed"
	result.Summary = c.generateSummary(result)

	return result, nil
}

// Helper function to truncate strings
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
