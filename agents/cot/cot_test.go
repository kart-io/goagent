package cot

import (
	"context"
	"testing"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
)

// MockLLM implements a simple mock LLM for testing
type MockLLM struct{}

func (m *MockLLM) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	// Return a simple CoT-style response
	return &llm.CompletionResponse{
		Content: `Let's think step by step:
Step 1: We have 2 apples
Step 2: We add 3 more apples
Step 3: 2 + 3 = 5
Therefore, the final answer is: 5 apples`,
		TokensUsed: 50,
	}, nil
}

func (m *MockLLM) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content:    "Generated response",
		TokensUsed: 10,
	}, nil
}

func (m *MockLLM) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (m *MockLLM) IsAvailable() bool {
	return true
}

func TestCoTAgent_BasicFunctionality(t *testing.T) {
	// Create a mock LLM
	mockLLM := &MockLLM{}

	// Create CoT agent
	config := CoTConfig{
		Name:        "test-cot",
		Description: "Test CoT Agent",
		LLM:         mockLLM,
		MaxSteps:    5,
		ZeroShot:    true,
		ShowStepNumbers: true,
	}

	agent := NewCoTAgent(config)

	// Create test input
	input := &agentcore.AgentInput{
		Task: "If I have 2 apples and get 3 more, how many do I have?",
	}

	// Execute agent
	ctx := context.Background()
	output, err := agent.Invoke(ctx, input)

	// Verify results
	if err != nil {
		t.Fatalf("Agent execution failed: %v", err)
	}

	if output.Status != "success" {
		t.Errorf("Expected status 'success', got '%s'", output.Status)
	}

	if output.Result == nil {
		t.Error("Expected result, got nil")
	}

	// Check that we have reasoning steps
	if len(output.ReasoningSteps) == 0 {
		t.Error("Expected reasoning steps, got none")
	}

	t.Logf("Agent completed successfully with result: %v", output.Result)
	t.Logf("Reasoning steps: %d", len(output.ReasoningSteps))
}

func TestCoTAgent_WithConfiguration(t *testing.T) {
	mockLLM := &MockLLM{}

	// Test different configurations
	configs := []CoTConfig{
		{
			Name:     "zero-shot",
			LLM:      mockLLM,
			ZeroShot: true,
		},
		{
			Name:    "few-shot",
			LLM:     mockLLM,
			FewShot: true,
			FewShotExamples: []CoTExample{
				{
					Question: "What is 2+2?",
					Steps:    []string{"2+2=4"},
					Answer:   "4",
				},
			},
		},
		{
			Name:                 "with-justification",
			LLM:                  mockLLM,
			RequireJustification: true,
		},
	}

	for _, config := range configs {
		t.Run(config.Name, func(t *testing.T) {
			agent := NewCoTAgent(config)

			input := &agentcore.AgentInput{
				Task: "Test task",
			}

			_, err := agent.Invoke(context.Background(), input)
			if err != nil {
				t.Errorf("Config %s failed: %v", config.Name, err)
			}
		})
	}
}