package cot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// CoTAgent implements Chain-of-Thought reasoning pattern.
//
// Chain-of-Thought (CoT) prompts the model to break down complex problems
// into intermediate reasoning steps, making the problem-solving process
// more transparent and accurate. This agent:
// - Encourages step-by-step reasoning
// - Shows intermediate calculations and logic
// - Improves accuracy on complex tasks
// - Provides interpretable reasoning traces
type CoTAgent struct {
	*agentcore.BaseAgent
	llm         llm.Client
	tools       []interfaces.Tool
	toolsByName map[string]interfaces.Tool
	maxSteps    int
	config      CoTConfig
}

// CoTConfig configuration for Chain-of-Thought agent
type CoTConfig struct {
	Name        string            // Agent name
	Description string            // Agent description
	LLM         llm.Client        // LLM client
	Tools       []interfaces.Tool // Available tools (optional)
	MaxSteps    int               // Maximum reasoning steps

	// CoT-specific settings
	ShowStepNumbers      bool   // Show step numbers in reasoning
	RequireJustification bool   // Require justification for each step
	FinalAnswerFormat    string // Format for final answer
	ExampleFormat        string // Example CoT format to show model

	// Prompting strategy
	ZeroShot        bool         // Use zero-shot CoT ("Let's think step by step")
	FewShot         bool         // Use few-shot CoT with examples
	FewShotExamples []CoTExample // Examples for few-shot learning
}

// CoTExample represents an example for few-shot Chain-of-Thought
type CoTExample struct {
	Question string
	Steps    []string
	Answer   string
}

// NewCoTAgent creates a new Chain-of-Thought agent
func NewCoTAgent(config CoTConfig) *CoTAgent {
	if config.MaxSteps <= 0 {
		config.MaxSteps = 10
	}

	if config.FinalAnswerFormat == "" {
		config.FinalAnswerFormat = "Therefore, the final answer is:"
	}

	// Build tools map
	toolsByName := make(map[string]interfaces.Tool)
	for _, tool := range config.Tools {
		toolsByName[tool.Name()] = tool
	}

	capabilities := []string{"chain_of_thought", "step_by_step", "reasoning"}
	if len(config.Tools) > 0 {
		capabilities = append(capabilities, "tool_calling")
	}

	return &CoTAgent{
		BaseAgent:   agentcore.NewBaseAgent(config.Name, config.Description, capabilities),
		llm:         config.LLM,
		tools:       config.Tools,
		toolsByName: toolsByName,
		maxSteps:    config.MaxSteps,
		config:      config,
	}
}

// Invoke executes the Chain-of-Thought reasoning
func (c *CoTAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// Trigger start callback
	if err := c.triggerOnStart(ctx, input); err != nil {
		return nil, err
	}

	// Build CoT prompt
	prompt := c.buildCoTPrompt(input)

	// Initialize output
	output := &agentcore.AgentOutput{
		ReasoningSteps: make([]agentcore.ReasoningStep, 0),
		ToolCalls:      make([]agentcore.ToolCall, 0),
		Metadata:       make(map[string]interface{}),
	}

	// Execute Chain-of-Thought reasoning
	reasoningSteps := make([]string, 0)

	// Call LLM with CoT prompt
	messages := []llm.Message{
		llm.SystemMessage(c.getSystemPrompt()),
		llm.UserMessage(prompt),
	}

	llmResp, err := c.llm.Chat(ctx, messages)
	if err != nil {
		return c.handleError(ctx, output, "LLM call failed", err, startTime)
	}

	// Parse CoT response
	response := llmResp.Content
	steps, finalAnswer := c.parseCoTResponse(response)

	// Record reasoning steps
	for i, step := range steps {
		output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
			Step:        i + 1,
			Action:      "Reasoning",
			Description: fmt.Sprintf("Step %d", i+1),
			Result:      step,
			Duration:    time.Since(startTime) / time.Duration(len(steps)),
			Success:     true,
		})
		reasoningSteps = append(reasoningSteps, step)
	}

	// If tools are available and needed, execute them
	if len(c.tools) > 0 {
		toolResults := c.executeToolsIfNeeded(ctx, steps, output)
		if len(toolResults) > 0 {
			// Re-run reasoning with tool results
			toolContext := c.formatToolResults(toolResults)
			messages = append(messages, llm.AssistantMessage(response))
			messages = append(messages, llm.UserMessage(toolContext))

			llmResp2, err := c.llm.Chat(ctx, messages)
			if err == nil {
				response = llmResp2.Content
				additionalSteps, newAnswer := c.parseCoTResponse(response)
				if newAnswer != "" {
					finalAnswer = newAnswer
				}
				for i, step := range additionalSteps {
					output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
						Step:        len(steps) + i + 1,
						Action:      "Reasoning with Tools",
						Description: fmt.Sprintf("Step %d (with tools)", len(steps)+i+1),
						Result:      step,
						Duration:    time.Since(startTime) / time.Duration(len(steps)+len(additionalSteps)),
						Success:     true,
					})
				}
			}
		}
	}

	// Set final output
	output.Status = "success"
	output.Result = finalAnswer
	output.Message = "Chain-of-Thought reasoning completed"
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)
	output.Metadata["total_steps"] = len(output.ReasoningSteps)
	output.Metadata["reasoning_trace"] = reasoningSteps

	// Trigger finish callback
	if err := c.triggerOnFinish(ctx, output); err != nil {
		return nil, err
	}

	return output, nil
}

// Stream executes Chain-of-Thought with streaming
func (c *CoTAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])

	go func() {
		defer close(outChan)

		// For now, wrap Invoke in streaming
		output, err := c.Invoke(ctx, input)
		outChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// buildCoTPrompt builds the Chain-of-Thought prompt
func (c *CoTAgent) buildCoTPrompt(input *agentcore.AgentInput) string {
	var prompt strings.Builder

	// Add few-shot examples if configured
	if c.config.FewShot && len(c.config.FewShotExamples) > 0 {
		prompt.WriteString("Here are some examples of step-by-step reasoning:\n\n")
		for _, example := range c.config.FewShotExamples {
			prompt.WriteString(fmt.Sprintf("Question: %s\n", example.Question))
			prompt.WriteString("Let's think step by step:\n")
			for i, step := range example.Steps {
				if c.config.ShowStepNumbers {
					prompt.WriteString(fmt.Sprintf("Step %d: %s\n", i+1, step))
				} else {
					prompt.WriteString(fmt.Sprintf("- %s\n", step))
				}
			}
			prompt.WriteString(fmt.Sprintf("%s %s\n\n", c.config.FinalAnswerFormat, example.Answer))
		}
		prompt.WriteString("Now, let's solve this problem:\n\n")
	}

	// Add the actual question
	prompt.WriteString(fmt.Sprintf("Question: %s\n\n", input.Task))

	// Add CoT trigger
	if c.config.ZeroShot || !c.config.FewShot {
		prompt.WriteString("Let's think step by step:\n")
	}

	// Add format instructions if provided
	if c.config.ExampleFormat != "" {
		prompt.WriteString(fmt.Sprintf("\nPlease follow this format:\n%s\n", c.config.ExampleFormat))
	}

	// Add instruction to show work
	if c.config.RequireJustification {
		prompt.WriteString("\nFor each step, provide clear justification and show all work.\n")
	}

	// Add final answer format reminder
	prompt.WriteString(fmt.Sprintf("\nEnd with: %s [your final answer]\n", c.config.FinalAnswerFormat))

	return prompt.String()
}

// getSystemPrompt returns the system prompt for CoT
func (c *CoTAgent) getSystemPrompt() string {
	prompt := `You are an expert problem solver that uses Chain-of-Thought reasoning.
Break down complex problems into clear, logical steps.
Show your work and reasoning at each step.
Be systematic and thorough in your analysis.`

	if c.config.ShowStepNumbers {
		prompt += "\nNumber each step clearly."
	}

	if c.config.RequireJustification {
		prompt += "\nProvide justification for each reasoning step."
	}

	if len(c.tools) > 0 {
		prompt += "\n\nYou have access to tools that can help verify or calculate results."
		prompt += "\nIndicate when a tool would be helpful by saying 'USE_TOOL: [tool_name]'."
	}

	return prompt
}

// parseCoTResponse parses the Chain-of-Thought response
func (c *CoTAgent) parseCoTResponse(response string) ([]string, string) {
	lines := strings.Split(response, "\n")
	steps := make([]string, 0)
	finalAnswer := ""

	currentStep := strings.Builder{}
	inStep := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check for final answer
		if strings.Contains(line, c.config.FinalAnswerFormat) {
			// Save current step if any
			if currentStep.Len() > 0 {
				steps = append(steps, strings.TrimSpace(currentStep.String()))
			}
			parts := strings.SplitN(line, c.config.FinalAnswerFormat, 2)
			if len(parts) > 1 {
				finalAnswer = strings.TrimSpace(parts[1])
			}
			break
		}

		// Detect step header (e.g., "**Step 1:**", "Step 2:", "- Step 3")
		// More flexible: just look for "step" with a number
		lowerLine := strings.ToLower(line)
		isStepHeader := strings.Contains(lowerLine, "step") &&
			(strings.Contains(line, ":") || strings.HasPrefix(line, "**") || strings.HasPrefix(line, "-"))

		if isStepHeader {
			// Save previous step if any
			if currentStep.Len() > 0 {
				steps = append(steps, strings.TrimSpace(currentStep.String()))
				currentStep.Reset()
			}
			inStep = true

			// Extract step title and content
			// Remove markdown formatting and step number
			cleanLine := strings.TrimPrefix(line, "**")
			cleanLine = strings.TrimSuffix(cleanLine, "**")
			cleanLine = strings.TrimPrefix(cleanLine, "- ")
			currentStep.WriteString(cleanLine)
			currentStep.WriteString(" ")
		} else if inStep {
			// Skip empty lines, LaTeX delimiters, and pure formula lines
			if line == "\\[" || line == "\\]" {
				continue
			}
			// Skip lines that are only numbers (likely part of LaTeX)
			if matched, _ := regexp.MatchString(`^\d+$`, line); matched {
				continue
			}
			// Skip lines that look like pure LaTeX formulas
			if strings.HasPrefix(line, "\\frac") || strings.HasPrefix(line, "\\quad") ||
				strings.HasPrefix(line, "\\text") {
				continue
			}
			// Skip "Question:" line and "Let's" line
			if strings.HasPrefix(lowerLine, "question:") ||
				strings.HasPrefix(lowerLine, "let's") {
				continue
			}

			// Collect content for current step
			currentStep.WriteString(line)
			currentStep.WriteString(" ")
		}
	}

	// Save last step
	if currentStep.Len() > 0 {
		steps = append(steps, strings.TrimSpace(currentStep.String()))
	}

	// If no structured steps found, try alternative parsing
	if len(steps) == 0 && finalAnswer == "" {
		// Split by double newlines (paragraphs)
		paragraphs := strings.Split(response, "\n\n")
		for _, para := range paragraphs {
			para = strings.TrimSpace(para)
			if para != "" && !strings.HasPrefix(strings.ToLower(para), "question") &&
				!strings.HasPrefix(strings.ToLower(para), "let's") {
				steps = append(steps, para)
			}
		}

		// Last paragraph might be the answer
		if len(steps) > 0 {
			lastStep := steps[len(steps)-1]
			if strings.Contains(strings.ToLower(lastStep), "answer") ||
				strings.Contains(strings.ToLower(lastStep), "conclusion") {
				finalAnswer = lastStep
				steps = steps[:len(steps)-1]
			}
		}
	}

	return steps, finalAnswer
}

// executeToolsIfNeeded checks if tools are needed and executes them
func (c *CoTAgent) executeToolsIfNeeded(ctx context.Context, steps []string, output *agentcore.AgentOutput) map[string]interface{} {
	toolResults := make(map[string]interface{})

	for _, step := range steps {
		// Check if step mentions needing a tool
		if strings.Contains(step, "USE_TOOL:") {
			parts := strings.SplitN(step, "USE_TOOL:", 2)
			if len(parts) > 1 {
				toolRequest := strings.TrimSpace(parts[1])
				// Parse tool name and input
				toolParts := strings.SplitN(toolRequest, " ", 2)
				toolName := toolParts[0]

				var toolInput map[string]interface{}
				if len(toolParts) > 1 {
					toolInput = map[string]interface{}{
						"query": toolParts[1],
					}
				}

				// Execute tool if available
				if tool, exists := c.toolsByName[toolName]; exists {
					toolIn := &interfaces.ToolInput{
						Args:    toolInput,
						Context: ctx,
					}

					startTime := time.Now()
					result, err := tool.Invoke(ctx, toolIn)

					toolCall := agentcore.ToolCall{
						ToolName: toolName,
						Input:    toolInput,
						Duration: time.Since(startTime),
						Success:  err == nil,
					}

					if err != nil {
						toolCall.Error = err.Error()
					} else {
						toolCall.Output = result.Result
						toolResults[toolName] = result.Result
					}

					output.ToolCalls = append(output.ToolCalls, toolCall)
				}
			}
		}
	}

	return toolResults
}

// formatToolResults formats tool results for the LLM
func (c *CoTAgent) formatToolResults(results map[string]interface{}) string {
	if len(results) == 0 {
		return ""
	}

	var formatted strings.Builder
	formatted.WriteString("Tool execution results:\n")
	for toolName, result := range results {
		formatted.WriteString(fmt.Sprintf("- %s: %v\n", toolName, result))
	}
	formatted.WriteString("\nPlease continue your reasoning with these results.")

	return formatted.String()
}

// handleError handles errors during execution
func (c *CoTAgent) handleError(ctx context.Context, output *agentcore.AgentOutput, message string, err error, startTime time.Time) (*agentcore.AgentOutput, error) {
	output.Status = "failed"
	output.Message = message
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	_ = c.triggerOnError(ctx, err)
	return output, err
}

// Callback trigger methods
func (c *CoTAgent) triggerOnStart(ctx context.Context, input *agentcore.AgentInput) error {
	config := c.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (c *CoTAgent) triggerOnFinish(ctx context.Context, output *agentcore.AgentOutput) error {
	config := c.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnAgentFinish(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (c *CoTAgent) triggerOnError(ctx context.Context, err error) error {
	config := c.GetConfig()
	for _, cb := range config.Callbacks {
		if cbErr := cb.OnError(ctx, err); cbErr != nil {
			return cbErr
		}
	}
	return nil
}

// WithCallbacks adds callback handlers
func (c *CoTAgent) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *c
	newAgent.BaseAgent = c.BaseAgent.WithCallbacks(callbacks...).(*agentcore.BaseAgent)
	return &newAgent
}

// WithConfig configures the agent
func (c *CoTAgent) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := *c
	newAgent.BaseAgent = c.BaseAgent.WithConfig(config).(*agentcore.BaseAgent)
	return &newAgent
}
