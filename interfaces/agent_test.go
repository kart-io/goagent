package interfaces

import (
	"context"
	"testing"
)

// TestInputStructure verifies Input struct is properly defined
func TestInputStructure(t *testing.T) {
	input := &Input{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
		State: State{
			"key1": "value1",
			"key2": 42,
		},
		Config: map[string]interface{}{
			"temperature": 0.7,
			"max_tokens":  100,
		},
	}

	if len(input.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(input.Messages))
	}
	if input.Messages[0].Role != "user" {
		t.Errorf("Expected first message role 'user', got '%s'", input.Messages[0].Role)
	}
	if input.State["key1"] != "value1" {
		t.Errorf("Expected state key1='value1', got '%v'", input.State["key1"])
	}
	if input.Config["temperature"] != 0.7 {
		t.Errorf("Expected config temperature=0.7, got '%v'", input.Config["temperature"])
	}
}

// TestOutputStructure verifies Output struct is properly defined
func TestOutputStructure(t *testing.T) {
	output := &Output{
		Messages: []Message{
			{Role: "assistant", Content: "Response text", Name: "TestAgent"},
		},
		State: State{
			"counter": 1,
			"status":  "complete",
		},
		Metadata: map[string]interface{}{
			"execution_time_ms": 150,
			"model":             "gpt-4",
		},
	}

	if len(output.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(output.Messages))
	}
	if output.Messages[0].Role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", output.Messages[0].Role)
	}
	if output.State["counter"] != 1 {
		t.Errorf("Expected state counter=1, got '%v'", output.State["counter"])
	}
	if output.Metadata["model"] != "gpt-4" {
		t.Errorf("Expected metadata model='gpt-4', got '%v'", output.Metadata["model"])
	}
}

// TestMessageStructure verifies Message struct is properly defined
func TestMessageStructure(t *testing.T) {
	tests := []struct {
		name    string
		message Message
	}{
		{
			name: "user message",
			message: Message{
				Role:    "user",
				Content: "What is the weather?",
			},
		},
		{
			name: "assistant message",
			message: Message{
				Role:    "assistant",
				Content: "The weather is sunny.",
			},
		},
		{
			name: "function message with name",
			message: Message{
				Role:    "function",
				Content: `{"temperature": 72, "condition": "sunny"}`,
				Name:    "get_weather",
			},
		},
		{
			name: "system message",
			message: Message{
				Role:    "system",
				Content: "You are a helpful assistant.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.message.Role == "" {
				t.Error("Message role should not be empty")
			}
			if tt.message.Content == "" {
				t.Error("Message content should not be empty")
			}
			if tt.message.Role == "function" && tt.message.Name == "" {
				t.Error("Function message should have a name")
			}
		})
	}
}

// TestStreamChunkStructure verifies StreamChunk struct is properly defined
func TestStreamChunkStructure(t *testing.T) {
	chunk1 := &StreamChunk{
		Content:  "Partial response...",
		Metadata: map[string]interface{}{"chunk_index": 1},
		Done:     false,
	}

	chunk2 := &StreamChunk{
		Content:  "Final part.",
		Metadata: map[string]interface{}{"chunk_index": 2, "total_chunks": 2},
		Done:     true,
	}

	if chunk1.Done {
		t.Error("First chunk should not be marked as done")
	}
	if !chunk2.Done {
		t.Error("Final chunk should be marked as done")
	}
	if chunk1.Metadata["chunk_index"] != 1 {
		t.Errorf("Expected chunk_index=1, got %v", chunk1.Metadata["chunk_index"])
	}
}

// TestPlanStructure verifies Plan struct is properly defined
func TestPlanStructure(t *testing.T) {
	plan := &Plan{
		Steps: []Step{
			{
				Action:   "search",
				Input:    map[string]interface{}{"query": "weather in NYC"},
				ToolName: "search_tool",
			},
			{
				Action: "format",
				Input:  map[string]interface{}{"format": "json"},
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.9,
			"reasoning":  "Need to search first, then format",
		},
	}

	if len(plan.Steps) != 2 {
		t.Errorf("Expected 2 steps, got %d", len(plan.Steps))
	}
	if plan.Steps[0].Action != "search" {
		t.Errorf("Expected first step action 'search', got '%s'", plan.Steps[0].Action)
	}
	if plan.Steps[0].ToolName != "search_tool" {
		t.Errorf("Expected tool name 'search_tool', got '%s'", plan.Steps[0].ToolName)
	}
	if plan.Metadata["confidence"] != 0.9 {
		t.Errorf("Expected confidence=0.9, got %v", plan.Metadata["confidence"])
	}
}

// TestStepStructure verifies Step struct is properly defined
func TestStepStructure(t *testing.T) {
	step := &Step{
		Action: "analyze",
		Input: map[string]interface{}{
			"text":   "Sample text to analyze",
			"method": "sentiment",
		},
		ToolName: "analyzer_tool",
	}

	if step.Action != "analyze" {
		t.Errorf("Expected action 'analyze', got '%s'", step.Action)
	}
	if step.ToolName != "analyzer_tool" {
		t.Errorf("Expected tool name 'analyzer_tool', got '%s'", step.ToolName)
	}
	if step.Input["method"] != "sentiment" {
		t.Errorf("Expected input method='sentiment', got '%v'", step.Input["method"])
	}
}

// TestStateOperations verifies State type operations
func TestStateOperations(t *testing.T) {
	state := State{
		"counter":    0,
		"user_name":  "Alice",
		"items":      []string{"item1", "item2"},
		"config":     map[string]interface{}{"enabled": true},
		"nested_int": 42,
	}

	// Test get
	if state["counter"] != 0 {
		t.Errorf("Expected counter=0, got %v", state["counter"])
	}

	// Test set
	state["counter"] = 1
	if state["counter"] != 1 {
		t.Errorf("Expected counter=1 after update, got %v", state["counter"])
	}

	// Test delete
	delete(state, "nested_int")
	if _, exists := state["nested_int"]; exists {
		t.Error("Expected nested_int to be deleted")
	}

	// Test length
	expectedLen := 4 // counter, user_name, items, config
	if len(state) != expectedLen {
		t.Errorf("Expected state length %d, got %d", expectedLen, len(state))
	}
}

// mockRunnable is a minimal test implementation of Runnable
type mockRunnable struct {
	name   string
	output string
}

func (m *mockRunnable) Invoke(ctx context.Context, input *Input) (*Output, error) {
	return &Output{
		Messages: []Message{
			{Role: "assistant", Content: m.output},
		},
		State:    input.State,
		Metadata: map[string]interface{}{"invoked_by": m.name},
	}, nil
}

func (m *mockRunnable) Stream(ctx context.Context, input *Input) (<-chan *StreamChunk, error) {
	ch := make(chan *StreamChunk, 1)
	go func() {
		defer close(ch)
		ch <- &StreamChunk{Content: m.output, Done: true}
	}()
	return ch, nil
}

// Ensure mockRunnable implements Runnable interface
var _ Runnable = (*mockRunnable)(nil)

// TestRunnableInterface verifies the Runnable interface works correctly
func TestRunnableInterface(t *testing.T) {
	ctx := context.Background()
	runnable := &mockRunnable{
		name:   "test-runnable",
		output: "Test output",
	}

	input := &Input{
		Messages: []Message{{Role: "user", Content: "Test input"}},
		State:    State{"key": "value"},
	}

	// Test Invoke
	output, err := runnable.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if len(output.Messages) != 1 {
		t.Errorf("Expected 1 message in output, got %d", len(output.Messages))
	}
	if output.Messages[0].Content != "Test output" {
		t.Errorf("Expected content 'Test output', got '%s'", output.Messages[0].Content)
	}
	if output.Metadata["invoked_by"] != "test-runnable" {
		t.Errorf("Expected metadata invoked_by='test-runnable', got '%v'", output.Metadata["invoked_by"])
	}

	// Test Stream
	streamCh, err := runnable.Stream(ctx, input)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var chunks []*StreamChunk
	for chunk := range streamCh {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk, got %d", len(chunks))
	}
	if !chunks[0].Done {
		t.Error("Expected final chunk to be marked as done")
	}
	if chunks[0].Content != "Test output" {
		t.Errorf("Expected chunk content 'Test output', got '%s'", chunks[0].Content)
	}
}

// mockAgent is a minimal test implementation of Agent
type mockAgent struct {
	mockRunnable
	agentName        string
	agentDescription string
}

func (m *mockAgent) Name() string {
	return m.agentName
}

func (m *mockAgent) Description() string {
	return m.agentDescription
}

func (m *mockAgent) Plan(ctx context.Context, input *Input) (*Plan, error) {
	return &Plan{
		Steps: []Step{
			{
				Action: "process",
				Input:  map[string]interface{}{"data": "test"},
			},
		},
		Metadata: map[string]interface{}{"planner": m.agentName},
	}, nil
}

// Ensure mockAgent implements Agent interface
var _ Agent = (*mockAgent)(nil)

// TestAgentInterface verifies the Agent interface works correctly
func TestAgentInterface(t *testing.T) {
	ctx := context.Background()
	agent := &mockAgent{
		mockRunnable: mockRunnable{
			name:   "test-agent-runnable",
			output: "Agent response",
		},
		agentName:        "TestAgent",
		agentDescription: "A test agent for unit testing",
	}

	// Test Name
	if agent.Name() != "TestAgent" {
		t.Errorf("Expected name 'TestAgent', got '%s'", agent.Name())
	}

	// Test Description
	if agent.Description() != "A test agent for unit testing" {
		t.Errorf("Expected description 'A test agent for unit testing', got '%s'", agent.Description())
	}

	// Test Plan
	input := &Input{
		Messages: []Message{{Role: "user", Content: "Plan this task"}},
	}

	plan, err := agent.Plan(ctx, input)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.Steps) != 1 {
		t.Errorf("Expected 1 step in plan, got %d", len(plan.Steps))
	}
	if plan.Steps[0].Action != "process" {
		t.Errorf("Expected step action 'process', got '%s'", plan.Steps[0].Action)
	}
	if plan.Metadata["planner"] != "TestAgent" {
		t.Errorf("Expected planner='TestAgent', got '%v'", plan.Metadata["planner"])
	}

	// Test Invoke (inherited from Runnable)
	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if output.Messages[0].Content != "Agent response" {
		t.Errorf("Expected content 'Agent response', got '%s'", output.Messages[0].Content)
	}

	// Test Stream (inherited from Runnable)
	streamCh, err := agent.Stream(ctx, input)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var streamedContent string
	for chunk := range streamCh {
		streamedContent += chunk.Content
	}

	if streamedContent != "Agent response" {
		t.Errorf("Expected streamed content 'Agent response', got '%s'", streamedContent)
	}
}

// TestAgentEmbedding verifies Agent properly embeds Runnable
func TestAgentEmbedding(t *testing.T) {
	agent := &mockAgent{
		mockRunnable: mockRunnable{
			name:   "embedded-test",
			output: "Embedded output",
		},
		agentName:        "EmbeddedAgent",
		agentDescription: "Tests interface embedding",
	}

	// Agent should satisfy both Agent and Runnable interfaces
	var _ Agent = agent
	var _ Runnable = agent

	ctx := context.Background()
	input := &Input{Messages: []Message{{Role: "user", Content: "test"}}}

	// Should be able to call Runnable methods through Agent interface
	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if output == nil {
		t.Fatal("Output should not be nil")
	}
}
