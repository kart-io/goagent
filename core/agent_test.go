package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockAgent 模拟 Agent 实现
type MockAgent struct {
	*BaseAgent
	executeFunc func(ctx context.Context, input *AgentInput) (*AgentOutput, error)
}

// NewMockAgent 创建模拟 Agent
func NewMockAgent(name, description string, capabilities []string, executeFunc func(context.Context, *AgentInput) (*AgentOutput, error)) *MockAgent {
	return &MockAgent{
		BaseAgent:   NewBaseAgent(name, description, capabilities),
		executeFunc: executeFunc,
	}
}

// Invoke 实现 Agent 接口（Runnable 接口的核心方法）
func (m *MockAgent) Invoke(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, input)
	}
	return &AgentOutput{
		Status:    "success",
		Message:   "mock execution completed",
		Result:    "mock result",
		Timestamp: time.Now(),
	}, nil
}

func TestBaseAgent_Name(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
	}{
		{
			name:      "simple name",
			agentName: "TestAgent",
		},
		{
			name:      "name with spaces",
			agentName: "Test Agent Name",
		},
		{
			name:      "empty name",
			agentName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewBaseAgent(tt.agentName, "description", nil)
			assert.Equal(t, tt.agentName, agent.Name())
		})
	}
}

func TestBaseAgent_Description(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "simple description",
			description: "A test agent",
		},
		{
			name:        "long description",
			description: "This is a very long description that contains multiple words and sentences.",
		},
		{
			name:        "empty description",
			description: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewBaseAgent("TestAgent", tt.description, nil)
			assert.Equal(t, tt.description, agent.Description())
		})
	}
}

func TestBaseAgent_Capabilities(t *testing.T) {
	tests := []struct {
		name         string
		capabilities []string
	}{
		{
			name:         "single capability",
			capabilities: []string{"diagnosis"},
		},
		{
			name:         "multiple capabilities",
			capabilities: []string{"diagnosis", "remediation", "analysis"},
		},
		{
			name:         "empty capabilities",
			capabilities: []string{},
		},
		{
			name:         "nil capabilities",
			capabilities: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewBaseAgent("TestAgent", "description", tt.capabilities)
			assert.Equal(t, tt.capabilities, agent.Capabilities())
		})
	}
}

func TestBaseAgent_Invoke_ReturnsError(t *testing.T) {
	agent := NewBaseAgent("TestAgent", "description", nil)
	input := &AgentInput{
		Task:        "test task",
		Instruction: "test instruction",
	}

	output, err := agent.Invoke(context.Background(), input)

	require.Error(t, err)
	assert.Equal(t, ErrNotImplemented, err)
	assert.NotNil(t, output)
	assert.Equal(t, "failed", output.Status)
	assert.Equal(t, "Invoke method must be implemented by concrete agent", output.Message)
}

func TestMockAgent_Execute_Success(t *testing.T) {
	expectedResult := map[string]interface{}{
		"status": "completed",
		"data":   "test data",
	}

	agent := NewMockAgent("TestAgent", "description", []string{"testing"}, func(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
		return &AgentOutput{
			Status:    "success",
			Message:   "test completed",
			Result:    expectedResult,
			Timestamp: time.Now(),
			Latency:   100 * time.Millisecond,
		}, nil
	})

	input := &AgentInput{
		Task:        "test task",
		Instruction: "execute test",
		Options:     DefaultAgentOptions(),
	}

	output, err := agent.Invoke(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	assert.Equal(t, "test completed", output.Message)
	assert.Equal(t, expectedResult, output.Result)
	assert.Equal(t, 100*time.Millisecond, output.Latency)
}

func TestMockAgent_Execute_WithContext(t *testing.T) {
	agent := NewMockAgent("TestAgent", "description", nil, func(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
		// Simulate work
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return &AgentOutput{
				Status:    "success",
				Message:   "completed",
				Timestamp: time.Now(),
			}, nil
		}
	})

	t.Run("normal execution", func(t *testing.T) {
		ctx := context.Background()
		input := &AgentInput{Task: "test"}

		output, err := agent.Invoke(ctx, input)
		require.NoError(t, err)
		assert.Equal(t, "success", output.Status)
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		input := &AgentInput{Task: "test"}

		output, err := agent.Invoke(ctx, input)
		assert.Error(t, err)
		assert.Nil(t, output)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("context timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		// Use agent that takes longer than timeout
		slowAgent := NewMockAgent("SlowAgent", "description", nil, func(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
				return &AgentOutput{Status: "success"}, nil
			}
		})

		input := &AgentInput{Task: "test"}

		output, err := slowAgent.Invoke(ctx, input)
		assert.Error(t, err)
		assert.Nil(t, output)
		assert.Equal(t, context.DeadlineExceeded, err)
	})
}

func TestAgentInput_Structure(t *testing.T) {
	input := &AgentInput{
		Task:        "diagnose pod crash",
		Instruction: "analyze pod logs and events",
		Context: map[string]interface{}{
			"pod":       "my-pod",
			"namespace": "default",
		},
		Options: AgentOptions{
			Temperature:  0.7,
			MaxTokens:    1000,
			EnableTools:  true,
			EnableMemory: true,
		},
		SessionID: "session-123",
		Timestamp: time.Now(),
	}

	assert.Equal(t, "diagnose pod crash", input.Task)
	assert.Equal(t, "analyze pod logs and events", input.Instruction)
	assert.Equal(t, "my-pod", input.Context["pod"])
	assert.Equal(t, "default", input.Context["namespace"])
	assert.Equal(t, 0.7, input.Options.Temperature)
	assert.Equal(t, 1000, input.Options.MaxTokens)
	assert.True(t, input.Options.EnableTools)
	assert.True(t, input.Options.EnableMemory)
	assert.Equal(t, "session-123", input.SessionID)
}

func TestAgentOutput_Structure(t *testing.T) {
	reasoningSteps := []ReasoningStep{
		{
			Step:        1,
			Action:      "analyze logs",
			Description: "analyzing pod logs",
			Result:      "found error pattern",
			Duration:    50 * time.Millisecond,
			Success:     true,
		},
		{
			Step:        2,
			Action:      "check events",
			Description: "checking k8s events",
			Result:      "found OOMKilled event",
			Duration:    30 * time.Millisecond,
			Success:     true,
		},
	}

	toolCalls := []ToolCall{
		{
			ToolName: "kubectl",
			Input: map[string]interface{}{
				"command": "logs",
				"pod":     "my-pod",
			},
			Output:   "log output",
			Duration: 100 * time.Millisecond,
			Success:  true,
		},
	}

	output := &AgentOutput{
		Result:         map[string]interface{}{"root_cause": "OOMKilled"},
		Status:         "success",
		Message:        "diagnosis completed",
		ReasoningSteps: reasoningSteps,
		ToolCalls:      toolCalls,
		Latency:        200 * time.Millisecond,
		Timestamp:      time.Now(),
		Metadata: map[string]interface{}{
			"confidence": 0.95,
		},
	}

	assert.Equal(t, "success", output.Status)
	assert.Equal(t, "diagnosis completed", output.Message)
	assert.Len(t, output.ReasoningSteps, 2)
	assert.Len(t, output.ToolCalls, 1)
	assert.Equal(t, 200*time.Millisecond, output.Latency)
	assert.Equal(t, 0.95, output.Metadata["confidence"])
}

func TestDefaultAgentOptions(t *testing.T) {
	opts := DefaultAgentOptions()

	assert.Equal(t, 0.7, opts.Temperature)
	assert.Equal(t, 2000, opts.MaxTokens)
	assert.True(t, opts.EnableTools)
	assert.Equal(t, 5, opts.MaxToolCalls)
	assert.False(t, opts.EnableMemory)
	assert.False(t, opts.LoadHistory)
	assert.False(t, opts.SaveToMemory)
	assert.Equal(t, 10, opts.MaxHistoryLength)
	assert.Equal(t, 60*time.Second, opts.Timeout)
}

func TestAgentOptions_CustomValues(t *testing.T) {
	opts := AgentOptions{
		Temperature:      0.9,
		MaxTokens:        4000,
		Model:            "gpt-4",
		EnableTools:      false,
		AllowedTools:     []string{"kubectl", "curl"},
		MaxToolCalls:     10,
		EnableMemory:     true,
		LoadHistory:      true,
		SaveToMemory:     true,
		MaxHistoryLength: 20,
		Timeout:          120 * time.Second,
	}

	assert.Equal(t, 0.9, opts.Temperature)
	assert.Equal(t, 4000, opts.MaxTokens)
	assert.Equal(t, "gpt-4", opts.Model)
	assert.False(t, opts.EnableTools)
	assert.Equal(t, []string{"kubectl", "curl"}, opts.AllowedTools)
	assert.Equal(t, 10, opts.MaxToolCalls)
	assert.True(t, opts.EnableMemory)
	assert.True(t, opts.LoadHistory)
	assert.True(t, opts.SaveToMemory)
	assert.Equal(t, 20, opts.MaxHistoryLength)
	assert.Equal(t, 120*time.Second, opts.Timeout)
}

func TestReasoningStep_Structure(t *testing.T) {
	step := ReasoningStep{
		Step:        1,
		Action:      "analyze",
		Description: "analyzing data",
		Result:      "found pattern",
		Duration:    100 * time.Millisecond,
		Success:     true,
	}

	assert.Equal(t, 1, step.Step)
	assert.Equal(t, "analyze", step.Action)
	assert.Equal(t, "analyzing data", step.Description)
	assert.Equal(t, "found pattern", step.Result)
	assert.Equal(t, 100*time.Millisecond, step.Duration)
	assert.True(t, step.Success)
	assert.Empty(t, step.Error)
}

func TestReasoningStep_WithError(t *testing.T) {
	step := ReasoningStep{
		Step:        1,
		Action:      "analyze",
		Description: "analyzing data",
		Duration:    50 * time.Millisecond,
		Success:     false,
		Error:       "analysis failed: invalid data",
	}

	assert.Equal(t, 1, step.Step)
	assert.False(t, step.Success)
	assert.Equal(t, "analysis failed: invalid data", step.Error)
}

func TestToolCall_Structure(t *testing.T) {
	toolCall := ToolCall{
		ToolName: "kubectl",
		Input: map[string]interface{}{
			"command":   "get",
			"resource":  "pods",
			"namespace": "default",
		},
		Output:   "pod list",
		Duration: 200 * time.Millisecond,
		Success:  true,
	}

	assert.Equal(t, "kubectl", toolCall.ToolName)
	assert.Equal(t, "get", toolCall.Input["command"])
	assert.Equal(t, "pods", toolCall.Input["resource"])
	assert.Equal(t, "default", toolCall.Input["namespace"])
	assert.Equal(t, "pod list", toolCall.Output)
	assert.Equal(t, 200*time.Millisecond, toolCall.Duration)
	assert.True(t, toolCall.Success)
	assert.Empty(t, toolCall.Error)
}

func TestToolCall_WithError(t *testing.T) {
	toolCall := ToolCall{
		ToolName: "kubectl",
		Input: map[string]interface{}{
			"command": "get",
		},
		Duration: 50 * time.Millisecond,
		Success:  false,
		Error:    "command execution failed: connection timeout",
	}

	assert.Equal(t, "kubectl", toolCall.ToolName)
	assert.False(t, toolCall.Success)
	assert.Equal(t, "command execution failed: connection timeout", toolCall.Error)
}

// Benchmark tests
func BenchmarkBaseAgent_Name(b *testing.B) {
	agent := NewBaseAgent("TestAgent", "description", []string{"cap1", "cap2"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = agent.Name()
	}
}

func BenchmarkBaseAgent_Capabilities(b *testing.B) {
	agent := NewBaseAgent("TestAgent", "description", []string{"cap1", "cap2", "cap3", "cap4"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = agent.Capabilities()
	}
}

func BenchmarkMockAgent_Execute(b *testing.B) {
	agent := NewMockAgent("TestAgent", "description", nil, func(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
		return &AgentOutput{
			Status:    "success",
			Result:    "result",
			Timestamp: time.Now(),
		}, nil
	})

	input := &AgentInput{
		Task:    "test",
		Options: DefaultAgentOptions(),
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Invoke(ctx, input)
	}
}
