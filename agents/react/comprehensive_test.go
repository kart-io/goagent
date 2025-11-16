package react_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/agents/react"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// ============================================================================
// Mock Implementations for Testing
// ============================================================================

// ErrorLLMClient simulates an LLM that fails
type ErrorLLMClient struct {
	callCount int
	failAfter int
	failError error
}

func NewErrorLLMClient(failAfter int, err error) *ErrorLLMClient {
	return &ErrorLLMClient{
		callCount: 0,
		failAfter: failAfter,
		failError: err,
	}
}

func (m *ErrorLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	if m.callCount >= m.failAfter {
		return nil, m.failError
	}
	m.callCount++
	return &llm.CompletionResponse{
		Content:    "Final Answer: Test",
		TokensUsed: 10,
	}, nil
}

func (m *ErrorLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return m.Chat(ctx, req.Messages)
}

func (m *ErrorLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (m *ErrorLLMClient) IsAvailable() bool {
	return true
}

// TrackingCallback tracks callback invocations
type TrackingCallback struct {
	mu               sync.Mutex
	onStartCalls     int
	onFinishCalls    int
	onErrorCalls     int
	onLLMStartCalls  int
	onLLMEndCalls    int
	onLLMErrorCalls  int
	onToolStartCalls int
	onToolEndCalls   int
	onToolErrorCalls int
	lastError        error
	lastToolName     string
	shouldFailAt     map[string]int
}

func NewTrackingCallback() *TrackingCallback {
	return &TrackingCallback{
		shouldFailAt: make(map[string]int),
	}
}

func (tc *TrackingCallback) OnStart(ctx context.Context, input interface{}) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.onStartCalls++
	if count, ok := tc.shouldFailAt["OnStart"]; ok && tc.onStartCalls >= count {
		return errors.New("callback OnStart failed")
	}
	return nil
}

func (tc *TrackingCallback) OnAgentFinish(ctx context.Context, output interface{}) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.onFinishCalls++
	if count, ok := tc.shouldFailAt["OnFinish"]; ok && tc.onFinishCalls >= count {
		return errors.New("callback OnFinish failed")
	}
	return nil
}

func (tc *TrackingCallback) OnError(ctx context.Context, err error) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.onErrorCalls++
	tc.lastError = err
	return nil
}

func (tc *TrackingCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.onLLMStartCalls++
	return nil
}

func (tc *TrackingCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.onLLMEndCalls++
	return nil
}

func (tc *TrackingCallback) OnLLMError(ctx context.Context, err error) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.onLLMErrorCalls++
	tc.lastError = err
	return nil
}

func (tc *TrackingCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.onToolStartCalls++
	tc.lastToolName = toolName
	return nil
}

func (tc *TrackingCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.onToolEndCalls++
	return nil
}

func (tc *TrackingCallback) OnToolError(ctx context.Context, toolName string, err error) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.onToolErrorCalls++
	tc.lastError = err
	return nil
}

func (tc *TrackingCallback) OnEnd(ctx context.Context, output interface{}) error {
	return nil
}

func (tc *TrackingCallback) OnChainStart(ctx context.Context, chainName string, input interface{}) error {
	return nil
}

func (tc *TrackingCallback) OnChainEnd(ctx context.Context, chainName string, output interface{}) error {
	return nil
}

func (tc *TrackingCallback) OnChainError(ctx context.Context, chainName string, err error) error {
	return nil
}

func (tc *TrackingCallback) OnAgentAction(ctx context.Context, action *agentcore.AgentAction) error {
	return nil
}

// ============================================================================
// Configuration Tests
// ============================================================================

// TestReActAgent_DefaultConfiguration tests default config values
func TestReActAgent_DefaultConfiguration(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		"Final Answer: test",
	})

	tool := tools.NewBaseTool(
		"test",
		"test tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
		},
	)

	// Create with minimal config - should use defaults
	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "DefaultAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
		// Not setting MaxSteps, StopPattern, PromptPrefix, etc.
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed with defaults: %v", err)
	}

	if output == nil {
		t.Error("Expected non-nil output")
	}
}

// TestReActAgent_CustomPrompts tests custom prompt configuration
func TestReActAgent_CustomPrompts(t *testing.T) {
	customPrefix := "CUSTOM PREFIX: {tools}"
	customSuffix := "CUSTOM SUFFIX: {input}"
	customFormat := "CUSTOM FORMAT: {tool_names}"

	mockLLM := NewMockLLMClient([]string{
		"Final Answer: done",
	})

	tool := tools.NewBaseTool(
		"custom_tool",
		"custom tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "custom", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:         "CustomPromptAgent",
		LLM:          mockLLM,
		Tools:        []interfaces.Tool{tool},
		PromptPrefix: customPrefix,
		PromptSuffix: customSuffix,
		FormatInstr:  customFormat,
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task:        "solve this",
		Instruction: "carefully",
	}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed with custom prompts: %v", err)
	}

	if output != nil && output.Status != "success" {
		t.Errorf("Expected success status, got %s", output.Status)
	}
}

// ============================================================================
// Stream Tests
// ============================================================================

// TestReActAgent_Stream tests streaming functionality
func TestReActAgent_Stream(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		"Final Answer: streaming test",
	})

	tool := tools.NewBaseTool(
		"stream_tool",
		"stream tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "stream_ok", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "StreamAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "stream test"}

	streamChan, err := agent.Stream(ctx, input)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var chunks []agentcore.StreamChunk[*agentcore.AgentOutput]
	for chunk := range streamChan {
		chunks = append(chunks, chunk)
	}

	if len(chunks) == 0 {
		t.Error("Expected at least one chunk")
	}

	// Should have exactly one chunk with Done=true
	foundDone := false
	for _, chunk := range chunks {
		if chunk.Done {
			foundDone = true
			if chunk.Data == nil {
				t.Error("Expected data in final chunk")
			}
			if chunk.Error != nil {
				t.Errorf("Unexpected error in chunk: %v", chunk.Error)
			}
		}
	}

	if !foundDone {
		t.Error("Expected a chunk with Done=true")
	}
}

// ============================================================================
// Error Handling Tests
// ============================================================================

// TestReActAgent_LLMError tests handling of LLM errors
func TestReActAgent_LLMError(t *testing.T) {
	llmErr := errors.New("LLM connection failed")
	mockLLM := NewErrorLLMClient(0, llmErr)

	tool := tools.NewBaseTool(
		"error_tool",
		"error tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "ErrorAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agent.Invoke(ctx, input)
	if err == nil {
		t.Error("Expected error from LLM")
	}

	if output == nil {
		t.Error("Expected output even with error")
	} else {
		if output.Status != "failed" {
			t.Errorf("Expected failed status, got %s", output.Status)
		}
	}
}

// TestReActAgent_InvalidAction tests handling of invalid actions
func TestReActAgent_InvalidAction(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: should do something
Action: non_existent_tool
Action Input: {}`,
	})

	tool := tools.NewBaseTool(
		"existing_tool",
		"existing tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "InvalidActionAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, _ := agent.Invoke(ctx, input)
	// The agent should handle this - either with an error or by trying the tool
	// and getting a tool-not-found error
	if output != nil {
		if len(output.ToolCalls) > 0 {
			// If it tried to call the tool, verify the failure was recorded
			if output.ToolCalls[0].Success {
				t.Error("Tool call should have failed for non-existent tool")
			}
		}
	}
}

// TestReActAgent_EmptyAction tests handling of empty actions
func TestReActAgent_EmptyAction(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: thinking
Action:
Action Input: {}`,
	})

	tool := tools.NewBaseTool(
		"valid_tool",
		"valid tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "EmptyActionAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, _ := agent.Invoke(ctx, input)
	// For empty action, the parser might handle it gracefully
	// The important thing is that the agent doesn't crash and handles it appropriately
	if output == nil {
		t.Error("Expected output even with malformed input")
	}
}

// TestReActAgent_ToolExecutionError tests handling of tool execution errors
func TestReActAgent_ToolExecutionError(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: need to use failing tool
Action: failing_tool
Action Input: {}`,

		`Thought: tool failed, giving final answer
Final Answer: Unable to complete due to tool error`,
	})

	failingTool := tools.NewBaseTool(
		"failing_tool",
		"tool that fails",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return nil, errors.New("tool execution failed")
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "ToolErrorAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{failingTool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(output.ToolCalls) == 0 {
		t.Error("Expected tool call")
	} else {
		if output.ToolCalls[0].Success {
			t.Error("Tool call should have failed")
		}
		if output.ToolCalls[0].Error == "" {
			t.Error("Expected error message in tool call")
		}
	}
}

// TestReActAgent_MaxStepsExceeded tests behavior when max steps is exceeded
func TestReActAgent_MaxStepsExceeded(t *testing.T) {
	// Create LLM that never outputs Final Answer
	responses := make([]string, 10)
	for i := 0; i < 10; i++ {
		responses[i] = fmt.Sprintf(`Thought: step %d
Action: test_tool
Action Input: {}`, i)
	}

	mockLLM := NewMockLLMClient(responses)

	tool := tools.NewBaseTool(
		"test_tool",
		"test tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:     "MaxStepsAgent",
		LLM:      mockLLM,
		Tools:    []interfaces.Tool{tool},
		MaxSteps: 3,
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if output.Status != "partial" {
		t.Errorf("Expected partial status for max steps, got %s", output.Status)
	}

	if len(output.ReasoningSteps) > 3*3 { // Each step creates 3 reasoning steps (Thought, Action, Observation)
		t.Errorf("Exceeded max steps: %d reasoning steps", len(output.ReasoningSteps))
	}
}

// ============================================================================
// Callback Tests
// ============================================================================

// TestReActAgent_WithCallbacks tests callback integration
func TestReActAgent_WithCallbacks(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: callback test
Action: cb_tool
Action Input: {}`,

		`Final Answer: callbacks working`,
	})

	tool := tools.NewBaseTool(
		"cb_tool",
		"callback tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "cb_ok", Success: true}, nil
		},
	)

	baseAgent := react.NewReActAgent(react.ReActConfig{
		Name:  "CallbackAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	// Add callbacks via WithCallbacks
	callback := NewTrackingCallback()
	agentWithCallbacks := baseAgent.WithCallbacks(callback)

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agentWithCallbacks.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed with callbacks: %v", err)
	}

	if output.Status != "success" {
		t.Errorf("Expected success status, got %s", output.Status)
	}

	// Verify callbacks were called
	if callback.onStartCalls == 0 {
		t.Error("OnStart callback not called")
	}
	if callback.onFinishCalls == 0 {
		t.Error("OnFinish callback not called")
	}
	if callback.onLLMStartCalls == 0 {
		t.Error("OnLLMStart callback not called")
	}
	if callback.onLLMEndCalls == 0 {
		t.Error("OnLLMEnd callback not called")
	}
	if callback.onToolStartCalls == 0 {
		t.Error("OnToolStart callback not called")
	}
	if callback.onToolEndCalls == 0 {
		t.Error("OnToolEnd callback not called")
	}
}

// TestReActAgent_CallbackFailure tests that callback failures propagate
func TestReActAgent_CallbackFailure(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		"Final Answer: test",
	})

	tool := tools.NewBaseTool(
		"test",
		"test",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
		},
	)

	baseAgent := react.NewReActAgent(react.ReActConfig{
		Name:  "FailingCallbackAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	// Create callback that fails on OnStart
	failingCallback := NewTrackingCallback()
	failingCallback.shouldFailAt["OnStart"] = 1

	agentWithCallback := baseAgent.WithCallbacks(failingCallback)

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	_, err := agentWithCallback.Invoke(ctx, input)
	if err == nil {
		t.Error("Expected error from failing callback")
	}
}

// TestReActAgent_MultipleCallbacks tests multiple callbacks
func TestReActAgent_MultipleCallbacks(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: multi callback test
Action: multi_tool
Action Input: {}`,

		`Final Answer: multi callbacks work`,
	})

	tool := tools.NewBaseTool(
		"multi_tool",
		"multi tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "multi_ok", Success: true}, nil
		},
	)

	baseAgent := react.NewReActAgent(react.ReActConfig{
		Name:  "MultiCallbackAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	callback1 := NewTrackingCallback()
	callback2 := NewTrackingCallback()

	agentWithCallbacks := baseAgent.WithCallbacks(callback1, callback2)

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agentWithCallbacks.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed with multiple callbacks: %v", err)
	}

	if output.Status != "success" {
		t.Errorf("Expected success, got %s", output.Status)
	}

	// Both callbacks should have been called
	if callback1.onStartCalls == 0 {
		t.Error("Callback1 OnStart not called")
	}
	if callback2.onStartCalls == 0 {
		t.Error("Callback2 OnStart not called")
	}
}

// ============================================================================
// Reasoning Chain Tests
// ============================================================================

// TestReActAgent_MultiStepReasoning tests multi-step reasoning chain
func TestReActAgent_MultiStepReasoning(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: First, I need to gather data
Action: search
Action Input: {"query": "initial data"}`,

		`Thought: Now I have initial data, need more details
Action: analyze
Action Input: {"data": "initial data"}`,

		`Thought: I now have enough information
Final Answer: Complete analysis provided`,
	})

	searchTool := tools.NewBaseTool(
		"search",
		"search tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "search_result", Success: true}, nil
		},
	)

	analyzeTool := tools.NewBaseTool(
		"analyze",
		"analyze tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "analysis_result", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:     "MultiStepAgent",
		LLM:      mockLLM,
		Tools:    []interfaces.Tool{searchTool, analyzeTool},
		MaxSteps: 5,
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "complex analysis"}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	if output.Status != "success" {
		t.Errorf("Expected success, got %s", output.Status)
	}

	// Should have at least 2 tool calls
	if len(output.ToolCalls) < 2 {
		t.Errorf("Expected at least 2 tool calls, got %d", len(output.ToolCalls))
	}

	// Tool calls should be in the correct order
	if len(output.ToolCalls) >= 2 {
		if output.ToolCalls[0].ToolName != "search" {
			t.Errorf("First tool should be search, got %s", output.ToolCalls[0].ToolName)
		}
		if output.ToolCalls[1].ToolName != "analyze" {
			t.Errorf("Second tool should be analyze, got %s", output.ToolCalls[1].ToolName)
		}
	}
}

// TestReActAgent_ReasoningStepsGeneration tests that reasoning steps are properly recorded
func TestReActAgent_ReasoningStepsGeneration(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: Think about this carefully
Action: action_tool
Action Input: {}`,

		`Final Answer: The answer is found`,
	})

	tool := tools.NewBaseTool(
		"action_tool",
		"action tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "action_result", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "ReasoningAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	// Should have reasoning steps: Thought, Action, Final Answer
	if len(output.ReasoningSteps) < 3 {
		t.Errorf("Expected at least 3 reasoning steps, got %d", len(output.ReasoningSteps))
	}

	// Verify step sequence
	stepTypes := make(map[string]int)
	for _, step := range output.ReasoningSteps {
		stepTypes[step.Action]++
	}

	if stepTypes["Thought"] == 0 {
		t.Error("Missing Thought step")
	}
	if stepTypes["Action"] == 0 {
		t.Error("Missing Action step")
	}
	if stepTypes["Final Answer"] == 0 {
		t.Error("Missing Final Answer step")
	}
}

// ============================================================================
// Action Selection and Execution Tests
// ============================================================================

// TestReActAgent_ActionSelection tests that actions are selected correctly
func TestReActAgent_ActionSelection(t *testing.T) {
	selectedTools := []string{}
	mu := sync.Mutex{}

	mockLLM := NewMockLLMClient([]string{
		`Thought: Select first tool
Action: tool_a
Action Input: {}`,

		`Thought: Select second tool
Action: tool_b
Action Input: {}`,

		`Final Answer: Selected both tools`,
	})

	toolA := tools.NewBaseTool(
		"tool_a",
		"tool a",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			mu.Lock()
			selectedTools = append(selectedTools, "tool_a")
			mu.Unlock()
			return &interfaces.ToolOutput{Result: "a_result", Success: true}, nil
		},
	)

	toolB := tools.NewBaseTool(
		"tool_b",
		"tool b",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			mu.Lock()
			selectedTools = append(selectedTools, "tool_b")
			mu.Unlock()
			return &interfaces.ToolOutput{Result: "b_result", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:     "ActionSelectionAgent",
		LLM:      mockLLM,
		Tools:    []interfaces.Tool{toolA, toolB},
		MaxSteps: 5,
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	_, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	// Verify tools were called in order
	if len(selectedTools) != 2 {
		t.Errorf("Expected 2 tools selected, got %d", len(selectedTools))
	}
	if len(selectedTools) >= 1 && selectedTools[0] != "tool_a" {
		t.Errorf("First tool should be a, got %s", selectedTools[0])
	}
	if len(selectedTools) >= 2 && selectedTools[1] != "tool_b" {
		t.Errorf("Second tool should be b, got %s", selectedTools[1])
	}
}

// ============================================================================
// Observation Processing Tests
// ============================================================================

// TestReActAgent_ObservationIntegration tests observation handling and integration
func TestReActAgent_ObservationIntegration(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: Get some data
Action: data_tool
Action Input: {}`,

		`Thought: I got data and will process it
Final Answer: Processed: success`,
	})

	tool := tools.NewBaseTool(
		"data_tool",
		"data tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "data_result", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "ObservationAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	// Verify tool call result is captured
	if len(output.ToolCalls) == 0 {
		t.Error("Expected tool call")
	} else {
		if output.ToolCalls[0].Output != "data_result" {
			t.Errorf("Expected output 'data_result', got %v", output.ToolCalls[0].Output)
		}
	}
}

// ============================================================================
// Concurrent Operation Tests
// ============================================================================

// TestReActAgent_ConcurrentInvocations tests thread safety
func TestReActAgent_ConcurrentInvocations(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		"Final Answer: concurrent test 1",
		"Final Answer: concurrent test 2",
		"Final Answer: concurrent test 3",
		"Final Answer: concurrent test 4",
		"Final Answer: concurrent test 5",
	})

	tool := tools.NewBaseTool(
		"concurrent_tool",
		"concurrent tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "ConcurrentAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	var wg sync.WaitGroup
	errorCount := atomic.Int32{}
	successCount := atomic.Int32{}

	ctx := context.Background()

	// Run 5 concurrent invocations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			input := &agentcore.AgentInput{Task: fmt.Sprintf("test %d", idx)}
			_, err := agent.Invoke(ctx, input)
			if err != nil {
				errorCount.Add(1)
			} else {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if errorCount.Load() > 0 {
		t.Errorf("Expected no errors, got %d", errorCount.Load())
	}

	if successCount.Load() != 5 {
		t.Errorf("Expected 5 successes, got %d", successCount.Load())
	}
}

// ============================================================================
// Configuration and Metadata Tests
// ============================================================================

// TestReActAgent_WithConfig tests configuration via WithConfig
func TestReActAgent_WithConfig(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		"Final Answer: config test",
	})

	tool := tools.NewBaseTool(
		"config_tool",
		"config tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
		},
	)

	baseAgent := react.NewReActAgent(react.ReActConfig{
		Name:  "ConfigAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	config := agentcore.RunnableConfig{
		Tags: []string{"test", "config"},
	}

	agentWithConfig := baseAgent.WithConfig(config)

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agentWithConfig.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed with config: %v", err)
	}

	if output == nil {
		t.Error("Expected output")
	}
}

// TestReActAgent_OutputMetadata tests metadata generation
func TestReActAgent_OutputMetadata(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: test metadata
Action: metadata_tool
Action Input: {}`,

		`Final Answer: metadata test complete`,
	})

	tool := tools.NewBaseTool(
		"metadata_tool",
		"metadata tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "meta_ok", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "MetadataAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	// Verify metadata
	if output.Metadata == nil {
		t.Error("Expected metadata")
	}

	stepsCount, ok := output.Metadata["steps"]
	if !ok {
		t.Error("Missing 'steps' in metadata")
	}
	if steps, ok := stepsCount.(int); ok {
		if steps == 0 {
			t.Error("Expected non-zero steps count")
		}
	}

	toolCallsCount, ok := output.Metadata["tool_calls"]
	if !ok {
		t.Error("Missing 'tool_calls' in metadata")
	}
	if calls, ok := toolCallsCount.(int); ok {
		if calls == 0 {
			t.Error("Expected non-zero tool calls count")
		}
	}
}

// TestReActAgent_StopPattern tests custom stop patterns
func TestReActAgent_StopPattern(t *testing.T) {
	customStopPattern := []string{"STOP"}

	mockLLM := NewMockLLMClient([]string{
		`Thought: something
Action: stop_tool
Action Input: {}

STOP`,
	})

	tool := tools.NewBaseTool(
		"stop_tool",
		"stop tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "stopped", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:        "StopPatternAgent",
		LLM:         mockLLM,
		Tools:       []interfaces.Tool{tool},
		StopPattern: customStopPattern,
		MaxSteps:    10, // Large number to ensure we stop via pattern
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	// Should have stopped due to pattern, not max steps
	if len(output.ReasoningSteps) > 10 {
		t.Error("Should have stopped due to pattern")
	}
}

// TestReActAgent_TimingMetrics tests that timing metrics are collected
func TestReActAgent_TimingMetrics(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: timing test
Action: timing_tool
Action Input: {}`,

		`Final Answer: timing complete`,
	})

	tool := tools.NewBaseTool(
		"timing_tool",
		"timing tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			time.Sleep(10 * time.Millisecond)
			return &interfaces.ToolOutput{Result: "timed", Success: true}, nil
		},
	)

	agent := react.NewReActAgent(react.ReActConfig{
		Name:  "TimingAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	output, err := agent.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	// Verify latency is recorded
	if output.Latency <= 0 {
		t.Error("Expected non-zero latency")
	}

	// Verify reasoning steps have timing
	for _, step := range output.ReasoningSteps {
		if step.Duration < 0 {
			t.Error("Expected non-negative duration")
		}
	}

	// Verify tool calls have timing
	for _, toolCall := range output.ToolCalls {
		if toolCall.Duration < 0 {
			t.Error("Expected non-negative duration")
		}
	}
}

// TestReActAgent_LLMErrorCallback tests that LLM errors trigger callbacks
func TestReActAgent_LLMErrorCallback(t *testing.T) {
	llmErr := errors.New("LLM failure")
	mockLLM := NewErrorLLMClient(0, llmErr)

	tool := tools.NewBaseTool(
		"test_tool",
		"test tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
		},
	)

	baseAgent := react.NewReActAgent(react.ReActConfig{
		Name:  "LLMErrorCallbackAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{tool},
	})

	callback := NewTrackingCallback()
	agentWithCallback := baseAgent.WithCallbacks(callback)

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	_, err := agentWithCallback.Invoke(ctx, input)
	if err == nil {
		t.Error("Expected error")
	}

	// Verify error callback was triggered
	if callback.onLLMErrorCalls == 0 {
		t.Error("Expected OnLLMError callback to be called")
	}
}

// TestReActAgent_ToolErrorCallback tests that tool errors trigger callbacks
func TestReActAgent_ToolErrorCallback(t *testing.T) {
	mockLLM := NewMockLLMClient([]string{
		`Thought: use failing tool
Action: failing_tool
Action Input: {}`,

		`Final Answer: tool failed`,
	})

	failingTool := tools.NewBaseTool(
		"failing_tool",
		"failing tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return nil, errors.New("tool failed")
		},
	)

	baseAgent := react.NewReActAgent(react.ReActConfig{
		Name:  "ToolErrorCallbackAgent",
		LLM:   mockLLM,
		Tools: []interfaces.Tool{failingTool},
	})

	callback := NewTrackingCallback()
	agentWithCallback := baseAgent.WithCallbacks(callback)

	ctx := context.Background()
	input := &agentcore.AgentInput{Task: "test"}

	_, err := agentWithCallback.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify tool error callback was triggered
	if callback.onToolErrorCalls == 0 {
		t.Error("Expected OnToolError callback to be called")
	}
}
