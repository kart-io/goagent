package agents

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/llm"
)

// ExecutionAgent specializes in task execution and tool usage
type ExecutionAgent struct {
	name      string
	llmClient llm.Client
	tools     map[string]Tool
}

// Tool interface for execution tools
type Tool interface {
	Name() string
	Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

// NewExecutionAgent creates a new execution agent
func NewExecutionAgent(llmClient llm.Client) *ExecutionAgent {
	agent := &ExecutionAgent{
		name:      "ExecutionAgent",
		llmClient: llmClient,
		tools:     make(map[string]Tool),
	}

	// Register default tools
	agent.RegisterTool(&HTTPTool{})
	agent.RegisterTool(&FileTool{})
	agent.RegisterTool(&CommandTool{})
	agent.RegisterTool(&DataProcessorTool{})

	return agent
}

// Name returns the agent's name
func (e *ExecutionAgent) Name() string {
	return e.name
}

// RegisterTool adds a tool to the agent
func (e *ExecutionAgent) RegisterTool(tool Tool) {
	e.tools[tool.Name()] = tool
}

// Execute carries out the strategy with available tools
func (e *ExecutionAgent) Execute(ctx context.Context, strategy string) (string, error) {
	prompt := fmt.Sprintf(`You are an Execution Agent specialized in implementing strategies and using tools.

Strategy to execute: %s

Available tools:
- http_request: Make HTTP API calls
- file_ops: Perform file operations
- command: Execute system commands (simulated)
- data_processor: Process and transform data

Create an execution plan that:
1. Lists specific actions to take
2. Identifies which tools to use
3. Defines execution order
4. Specifies expected outcomes
5. Includes verification steps
6. Handles potential failures

Be specific and actionable.`, strategy)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := e.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("execution planning failed: %w", err)
	}

	return response.Content, nil
}

// ExecuteWithTools executes a plan using specific tools
func (e *ExecutionAgent) ExecuteWithTools(ctx context.Context, plan string, toolNames []string) (map[string]interface{}, error) {
	results := make(map[string]interface{})

	// Simulate tool execution based on the plan
	for _, toolName := range toolNames {
		if tool, exists := e.tools[toolName]; exists {
			// Simulate parameters based on the plan
			params := map[string]interface{}{
				"plan": plan,
				"tool": toolName,
			}

			result, err := tool.Execute(ctx, params)
			if err != nil {
				results[toolName] = map[string]interface{}{
					"status": "failed",
					"error":  err.Error(),
				}
			} else {
				results[toolName] = map[string]interface{}{
					"status": "success",
					"result": result,
				}
			}
		}
	}

	// Get LLM interpretation of results
	resultsPrompt := fmt.Sprintf(`You are an Execution Agent. Summarize these execution results:

Plan: %s

Tool Execution Results: %v

Provide:
1. Overall status
2. Key achievements
3. Any issues encountered
4. Next steps if needed`, plan, results)

	messages := []llm.Message{
		llm.UserMessage(resultsPrompt),
	}

	response, err := e.llmClient.Chat(ctx, messages)
	if err != nil {
		results["summary"] = "Failed to generate summary"
	} else {
		results["summary"] = response.Content
	}

	return results, nil
}

// MonitorExecution tracks ongoing execution
func (e *ExecutionAgent) MonitorExecution(ctx context.Context, executionID string) (string, error) {
	prompt := fmt.Sprintf(`You are an Execution Agent monitoring an ongoing execution.

Execution ID: %s

Provide a status update including:
1. Current progress
2. Completed steps
3. Running operations
4. Pending tasks
5. Any issues detected
6. Estimated completion time

Be concise but informative.`, executionID)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := e.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("execution monitoring failed: %w", err)
	}

	return response.Content, nil
}

// HandleFailure manages execution failures
func (e *ExecutionAgent) HandleFailure(ctx context.Context, failure string, context string) (string, error) {
	prompt := fmt.Sprintf(`You are an Execution Agent handling an execution failure.

Failure: %s

Context: %s

Provide:
1. Root cause analysis
2. Immediate mitigation steps
3. Recovery plan
4. Prevention measures
5. Alternative approaches
6. Escalation if needed

Focus on practical solutions.`, failure, context)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := e.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("failure handling failed: %w", err)
	}

	return response.Content, nil
}

// ValidateResults verifies execution results
func (e *ExecutionAgent) ValidateResults(ctx context.Context, expectedOutcome string, actualResult string) (bool, string, error) {
	prompt := fmt.Sprintf(`You are an Execution Agent validating execution results.

Expected Outcome: %s

Actual Result: %s

Evaluate:
1. Does the result meet expectations? (Yes/No)
2. What matched expectations?
3. What didn't match?
4. Is the result acceptable?
5. Any corrective actions needed?

Be objective and thorough.`, expectedOutcome, actualResult)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := e.llmClient.Chat(ctx, messages)
	if err != nil {
		return false, "", fmt.Errorf("result validation failed: %w", err)
	}

	// Simple validation logic - in production this would parse the response
	isValid := true // Would be determined from the response

	return isValid, response.Content, nil
}
