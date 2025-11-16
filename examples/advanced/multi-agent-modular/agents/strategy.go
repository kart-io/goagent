package agents

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/llm"
)

// StrategyAgent specializes in strategy formulation and planning
type StrategyAgent struct {
	name      string
	llmClient llm.Client
}

// NewStrategyAgent creates a new strategy agent
func NewStrategyAgent(llmClient llm.Client) *StrategyAgent {
	return &StrategyAgent{
		name:      "StrategyAgent",
		llmClient: llmClient,
	}
}

// Name returns the agent's name
func (s *StrategyAgent) Name() string {
	return s.name
}

// FormulateStrategy creates a strategy based on analysis
func (s *StrategyAgent) FormulateStrategy(ctx context.Context, analysis string) (string, error) {
	prompt := fmt.Sprintf(`You are a Strategy Agent specialized in formulating optimal approaches and plans.

Based on this analysis, create a comprehensive strategy:
Analysis: %s

Please provide:
1. Strategic approach (phased implementation, iterative, parallel, etc.)
2. Detailed phases with timelines
3. Priority order of tasks
4. Resource requirements
5. Success metrics and KPIs
6. Risk mitigation plan
7. Contingency plans

Format your response with clear sections and actionable items.`, analysis)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := s.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("strategy formulation failed: %w", err)
	}

	return response.Content, nil
}

// PrioritizeTasks determines task priorities
func (s *StrategyAgent) PrioritizeTasks(ctx context.Context, tasks []string) ([]map[string]interface{}, error) {
	taskList := ""
	for i, task := range tasks {
		taskList += fmt.Sprintf("%d. %s\n", i+1, task)
	}

	prompt := fmt.Sprintf(`You are a Strategy Agent. Prioritize these tasks based on impact and effort:

Tasks:
%s

For each task, provide:
- Priority Level (Critical/High/Medium/Low)
- Impact Score (1-10)
- Effort Score (1-10)
- Dependencies
- Recommended Order

Consider:
- Quick wins (high impact, low effort)
- Critical path items
- Resource constraints
- Dependencies between tasks`, taskList)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := s.llmClient.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("task prioritization failed: %w", err)
	}

	// In production, parse the structured response
	// For now, return a basic structure
	prioritized := []map[string]interface{}{
		{
			"task":       "Core Implementation",
			"priority":   "High",
			"impact":     8,
			"effort":     5,
			"order":      1,
			"raw_output": response.Content,
		},
	}

	return prioritized, nil
}

// CreateActionPlan creates a detailed action plan
func (s *StrategyAgent) CreateActionPlan(ctx context.Context, strategy string, constraints string) (string, error) {
	prompt := fmt.Sprintf(`You are a Strategy Agent creating a detailed action plan.

Strategy: %s

Constraints: %s

Create an action plan with:
1. Specific, measurable actions
2. Clear ownership and responsibilities
3. Deadlines and milestones
4. Required resources for each action
5. Success criteria
6. Monitoring and feedback loops
7. Escalation paths

Make it practical and immediately actionable.`, strategy, constraints)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := s.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("action plan creation failed: %w", err)
	}

	return response.Content, nil
}

// OptimizeStrategy refines an existing strategy
func (s *StrategyAgent) OptimizeStrategy(ctx context.Context, currentStrategy string, feedback string) (string, error) {
	prompt := fmt.Sprintf(`You are a Strategy Agent optimizing an existing strategy based on feedback.

Current Strategy: %s

Feedback/Issues: %s

Provide an optimized strategy that:
1. Addresses the feedback points
2. Maintains successful elements
3. Improves weak areas
4. Adds new insights
5. Adjusts timelines if needed
6. Updates success metrics

Explain what changed and why.`, currentStrategy, feedback)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := s.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("strategy optimization failed: %w", err)
	}

	return response.Content, nil
}

// EvaluateOptions compares multiple strategic options
func (s *StrategyAgent) EvaluateOptions(ctx context.Context, options []string) (string, error) {
	optionsList := ""
	for i, option := range options {
		optionsList += fmt.Sprintf("Option %d: %s\n\n", i+1, option)
	}

	prompt := fmt.Sprintf(`You are a Strategy Agent evaluating multiple strategic options.

Options to evaluate:
%s

For each option, assess:
1. Pros and cons
2. Risk level
3. Resource requirements
4. Timeline
5. Success probability
6. Alignment with goals

Provide a recommendation with justification.`, optionsList)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := s.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("option evaluation failed: %w", err)
	}

	return response.Content, nil
}
