package cot

import (
	"context"
	"testing"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
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

func (m *MockLLM) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLM) IsAvailable() bool {
	return true
}

func TestCoTAgent_BasicFunctionality(t *testing.T) {
	// Create a mock LLM
	mockLLM := &MockLLM{}

	// Create CoT agent
	config := CoTConfig{
		Name:            "test-cot",
		Description:     "Test CoT Agent",
		LLM:             mockLLM,
		MaxSteps:        5,
		ZeroShot:        true,
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

// TestCoTAgent_RunGenerator tests the RunGenerator method
func TestCoTAgent_RunGenerator(t *testing.T) {
	mockLLM := &MockLLM{}

	agent := NewCoTAgent(CoTConfig{
		Name:            "test-cot-gen",
		Description:     "Test CoT Agent with Generator",
		LLM:             mockLLM,
		MaxSteps:        5,
		ZeroShot:        true,
		ShowStepNumbers: true,
	})

	input := &agentcore.AgentInput{
		Task: "If I have 2 apples and get 3 more, how many do I have?",
	}

	ctx := context.Background()

	// Collect all outputs from generator
	var outputs []*agentcore.AgentOutput
	var finalOutput *agentcore.AgentOutput

	for output, err := range agent.RunGenerator(ctx, input) {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
			break
		}

		if output == nil {
			t.Error("Output is nil")
			break
		}

		outputs = append(outputs, output)
		finalOutput = output

		// Check metadata
		if _, ok := output.Metadata["step_type"]; !ok {
			t.Error("Missing step_type in metadata")
		}

		// Break on final output
		if output.Metadata["step_type"] == "final" {
			break
		}
	}

	// Verify we got multiple outputs
	if len(outputs) == 0 {
		t.Fatal("RunGenerator did not produce any outputs")
	}

	t.Logf("Total outputs: %d", len(outputs))

	// Verify final output
	if finalOutput == nil {
		t.Fatal("Final output is nil")
	}

	if finalOutput.Metadata["step_type"] != "final" {
		t.Errorf("Expected final step_type, got: %v", finalOutput.Metadata["step_type"])
	}

	// Verify we have reasoning steps
	if len(finalOutput.ReasoningSteps) == 0 {
		t.Error("No reasoning steps in final output")
	}

	t.Logf("Final result: %v", finalOutput.Result)
	t.Logf("Reasoning steps: %d", len(finalOutput.ReasoningSteps))
}

// TestCoTAgent_RunGenerator_EarlyTermination tests early termination
func TestCoTAgent_RunGenerator_EarlyTermination(t *testing.T) {
	mockLLM := &MockLLM{}

	agent := NewCoTAgent(CoTConfig{
		Name:     "test-cot-early",
		LLM:      mockLLM,
		ZeroShot: true,
		MaxSteps: 5,
	})

	input := &agentcore.AgentInput{
		Task: "Test early termination",
	}

	ctx := context.Background()

	// Terminate after first output
	maxOutputs := 1
	outputCount := 0

	for _, err := range agent.RunGenerator(ctx, input) {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
			break
		}

		outputCount++

		if outputCount >= maxOutputs {
			t.Logf("Terminating early after %d outputs", outputCount)
			break
		}
	}

	// Verify we only got the expected number of outputs
	if outputCount != maxOutputs {
		t.Errorf("Expected %d outputs, got %d", maxOutputs, outputCount)
	}

	t.Logf("Successfully terminated early after %d outputs", outputCount)
}
