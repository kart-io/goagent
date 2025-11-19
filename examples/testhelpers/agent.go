package testhelpers

import (
	"context"

	"github.com/kart-io/goagent/core"
)

// MockAgent is a simple function-based mock agent for testing examples
type MockAgent struct {
	name         string
	description  string
	capabilities []string
	invokeFn     func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error)
}

// NewMockAgent creates a new mock agent with the given name
func NewMockAgent(name string) *MockAgent {
	return &MockAgent{
		name:         name,
		description:  "Mock agent for testing",
		capabilities: []string{"test"},
	}
}

// SetInvokeFn sets a custom invoke function for this mock agent
func (m *MockAgent) SetInvokeFn(fn func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error)) {
	m.invokeFn = fn
}

// Name returns the agent name
func (m *MockAgent) Name() string {
	return m.name
}

// Description returns the agent description
func (m *MockAgent) Description() string {
	return m.description
}

// Capabilities returns the agent capabilities
func (m *MockAgent) Capabilities() []string {
	return m.capabilities
}

// Invoke executes the mock agent logic
func (m *MockAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, input)
	}
	return &core.AgentOutput{
		Result: "mock response",
		Status: "success",
		Metadata: map[string]interface{}{
			"mock": true,
		},
	}, nil
}

// Stream is not implemented for examples (not needed)
func (m *MockAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput])
	close(ch)
	return ch, nil
}

// Batch is not implemented for examples (not needed)
func (m *MockAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	outputs := make([]*core.AgentOutput, len(inputs))
	for i := range inputs {
		outputs[i] = &core.AgentOutput{Result: "success", Status: "success"}
	}
	return outputs, nil
}

// Pipe is not implemented for examples
func (m *MockAgent) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return nil
}

// WithCallbacks returns the agent (no-op for mock)
func (m *MockAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return m
}

// WithConfig returns the agent (no-op for mock)
func (m *MockAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return m
}
