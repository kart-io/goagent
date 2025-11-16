package agents

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/llm"
)

// AnalysisAgent specializes in data analysis and pattern recognition
type AnalysisAgent struct {
	name      string
	llmClient llm.Client
}

// NewAnalysisAgent creates a new analysis agent
func NewAnalysisAgent(llmClient llm.Client) *AnalysisAgent {
	return &AnalysisAgent{
		name:      "AnalysisAgent",
		llmClient: llmClient,
	}
}

// Name returns the agent's name
func (a *AnalysisAgent) Name() string {
	return a.name
}

// Analyze performs analysis on the given task
func (a *AnalysisAgent) Analyze(ctx context.Context, task string) (string, error) {
	prompt := fmt.Sprintf(`You are an Analysis Agent specialized in data analysis and pattern recognition.

Your task is to analyze the following and provide insights:
Task: %s

Please provide:
1. Key data points and patterns identified
2. Complexity assessment (Low/Medium/High)
3. Potential risks and challenges
4. Opportunities for optimization
5. Recommended approach

Format your response in a structured way with clear sections.`, task)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := a.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("analysis failed: %w", err)
	}

	return response.Content, nil
}

// AnalyzeWithContext performs analysis with additional context
func (a *AnalysisAgent) AnalyzeWithContext(ctx context.Context, task string, context string) (string, error) {
	prompt := fmt.Sprintf(`You are an Analysis Agent specialized in data analysis and pattern recognition.

Context: %s

Your task is to analyze the following and provide insights:
Task: %s

Please provide:
1. Key data points and patterns identified
2. Complexity assessment (Low/Medium/High)
3. Potential risks and challenges
4. Opportunities for optimization
5. Recommended approach
6. How the context affects your analysis

Format your response in a structured way with clear sections.`, context, task)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := a.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("contextual analysis failed: %w", err)
	}

	return response.Content, nil
}

// ExtractInsights extracts key insights from raw data
func (a *AnalysisAgent) ExtractInsights(ctx context.Context, data string) (map[string]interface{}, error) {
	prompt := fmt.Sprintf(`You are an Analysis Agent. Extract key insights from this data:

Data: %s

Provide insights in the following categories:
- patterns: List of identified patterns
- metrics: Key metrics found
- anomalies: Any unusual findings
- recommendations: Suggested actions

Respond in JSON format.`, data)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := a.llmClient.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("insight extraction failed: %w", err)
	}

	// In production, you would parse the JSON response
	// For now, return a structured map
	insights := map[string]interface{}{
		"raw_analysis": response.Content,
		"status":       "completed",
	}

	return insights, nil
}

// PerformRiskAssessment evaluates risks in the task
func (a *AnalysisAgent) PerformRiskAssessment(ctx context.Context, task string) (string, error) {
	prompt := fmt.Sprintf(`You are an Analysis Agent performing risk assessment.

Task: %s

Evaluate and provide:
1. Risk Level (Low/Medium/High/Critical)
2. Specific risks identified
3. Impact assessment
4. Mitigation strategies
5. Dependencies that could affect success

Be thorough but concise.`, task)

	messages := []llm.Message{
		llm.UserMessage(prompt),
	}

	response, err := a.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("risk assessment failed: %w", err)
	}

	return response.Content, nil
}
