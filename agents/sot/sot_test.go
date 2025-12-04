package sot

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/agents/base"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockLLMClient for testing
type MockLLMClient struct {
	mock.Mock
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*llm.CompletionResponse), args.Error(1)
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	args := m.Called(ctx, messages)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*llm.CompletionResponse), args.Error(1)
}

func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// MockTool for testing
type MockTool struct {
	mock.Mock
}

func (m *MockTool) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockTool) Description() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.ToolOutput), args.Error(1)
}

func (m *MockTool) ArgsSchema() string {
	args := m.Called()
	return args.String(0)
}

func TestNewSoTAgent(t *testing.T) {
	tests := []struct {
		name   string
		config SoTConfig
		check  func(t *testing.T, agent *SoTAgent)
	}{
		{
			name: "default configuration",
			config: SoTConfig{
				Name:        "test-sot",
				Description: "Test SoT Agent",
				LLM:         &MockLLMClient{},
			},
			check: func(t *testing.T, agent *SoTAgent) {
				assert.Equal(t, "test-sot", agent.Name())
				assert.Equal(t, "Test SoT Agent", agent.Description())
				assert.Equal(t, 10, agent.config.MaxSkeletonPoints)
				assert.Equal(t, 3, agent.config.MinSkeletonPoints)
				assert.Equal(t, 5, agent.config.MaxConcurrency)
				assert.Equal(t, 30*time.Second, agent.config.ElaborationTimeout)
				assert.Equal(t, 3, agent.config.BatchSize)
				assert.Equal(t, "sequential", agent.config.AggregationStrategy)
			},
		},
		{
			name: "custom configuration",
			config: SoTConfig{
				Name:                "custom-sot",
				Description:         "Custom SoT Agent",
				LLM:                 &MockLLMClient{},
				MaxSkeletonPoints:   15,
				MinSkeletonPoints:   5,
				AutoDecompose:       true,
				MaxConcurrency:      10,
				ElaborationTimeout:  60 * time.Second,
				BatchSize:           5,
				AggregationStrategy: "hierarchical",
				DependencyAware:     true,
			},
			check: func(t *testing.T, agent *SoTAgent) {
				assert.Equal(t, 15, agent.config.MaxSkeletonPoints)
				assert.Equal(t, 5, agent.config.MinSkeletonPoints)
				assert.True(t, agent.config.AutoDecompose)
				assert.Equal(t, 10, agent.config.MaxConcurrency)
				assert.Equal(t, 60*time.Second, agent.config.ElaborationTimeout)
				assert.Equal(t, 5, agent.config.BatchSize)
				assert.Equal(t, "hierarchical", agent.config.AggregationStrategy)
				assert.True(t, agent.config.DependencyAware)
			},
		},
		{
			name: "with tools",
			config: SoTConfig{
				Name:        "sot-with-tools",
				Description: "SoT with Tools",
				LLM:         &MockLLMClient{},
				Tools: []interfaces.Tool{
					func() interfaces.Tool {
						m := &MockTool{}
						m.On("Name").Return("test-tool")
						m.On("Description").Return("Test tool")
						m.On("ArgsSchema").Return("{}")
						return m
					}(),
				},
			},
			check: func(t *testing.T, agent *SoTAgent) {
				assert.Len(t, agent.tools, 1)
				assert.Contains(t, agent.Capabilities(), "tool_calling")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewSoTAgent(tt.config)
			assert.NotNil(t, agent)
			tt.check(t, agent)
		})
	}
}

func TestSoTAgent_ParseSkeleton(t *testing.T) {
	agent := NewSoTAgent(SoTConfig{
		Name: "test-parse",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		name     string
		response string
		expected int
	}{
		{
			name: "numbered points with dependencies",
			response: `1. [Analysis]: Analyze the problem.
2. [Solution]: Develop the solution. Depends on: 1
3. [Testing]: Test the solution. Depends on: 2
4. [Conclusion]: Summarize findings. Depends on: 2, 3`,
			expected: 4,
		},
		{
			name: "simple numbered list",
			response: `1. First point: Do this
2. Second point: Do that
3. Third point: Finish up`,
			expected: 3,
		},
		{
			name:     "invalid format falls back to default",
			response: "This is just plain text without proper formatting",
			expected: 3, // Should create default skeleton
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skeleton := agent.parseSkeleton(tt.response)
			assert.Len(t, skeleton, tt.expected)
		})
	}
}

func TestSoTAgent_GroupByDependencyLevel(t *testing.T) {
	agent := NewSoTAgent(SoTConfig{
		Name: "test-dependency",
		LLM:  &MockLLMClient{},
	})

	// Create skeleton with dependencies
	point1 := &SkeletonPoint{ID: "point_1", Status: "pending"}
	point2 := &SkeletonPoint{ID: "point_2", Status: "pending", Dependencies: []string{"point_1"}}
	point3 := &SkeletonPoint{ID: "point_3", Status: "pending", Dependencies: []string{"point_1"}}
	point4 := &SkeletonPoint{ID: "point_4", Status: "pending", Dependencies: []string{"point_2", "point_3"}}

	skeleton := []*SkeletonPoint{point1, point2, point3, point4}
	levels := agent.groupByDependencyLevel(skeleton)

	assert.Len(t, levels, 3)
	assert.Contains(t, levels[0], point1)
	assert.Contains(t, levels[1], point2)
	assert.Contains(t, levels[1], point3)
	assert.Contains(t, levels[2], point4)
}

func TestSoTAgent_ElaboratePoint(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	mockLLM.On("Chat", mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "This is the detailed elaboration of the point.",
		}, nil,
	)

	agent := NewSoTAgent(SoTConfig{
		Name: "test-elaborate",
		LLM:  mockLLM,
	})

	point := &SkeletonPoint{
		ID:          "test_point",
		Title:       "Test Point",
		Description: "Test description",
		Status:      "pending",
		Metadata:    make(map[string]interface{}),
	}

	skeleton := []*SkeletonPoint{point}
	input := &core.AgentInput{Task: "Test task"}

	err := agent.elaboratePoint(ctx, point, skeleton, input)

	assert.NoError(t, err)
	assert.Equal(t, "completed", point.Status)
	assert.Equal(t, "This is the detailed elaboration of the point.", point.Elaboration)
	mockLLM.AssertExpectations(t)
}

func TestSoTAgent_BuildDependencyContext(t *testing.T) {
	agent := NewSoTAgent(SoTConfig{
		Name: "test-context",
		LLM:  &MockLLMClient{},
	})

	// Create skeleton with dependencies
	dep1 := &SkeletonPoint{
		ID:          "dep_1",
		Title:       "Dependency 1",
		Status:      "completed",
		Elaboration: "Elaboration of dependency 1",
	}
	dep2 := &SkeletonPoint{
		ID:          "dep_2",
		Title:       "Dependency 2",
		Status:      "completed",
		Elaboration: "Elaboration of dependency 2",
	}
	point := &SkeletonPoint{
		ID:           "main_point",
		Title:        "Main Point",
		Dependencies: []string{"dep_1", "dep_2"},
	}

	skeleton := []*SkeletonPoint{dep1, dep2, point}
	context := agent.buildDependencyContext(point, skeleton)

	assert.Contains(t, context, "Dependency 1")
	assert.Contains(t, context, "Elaboration of dependency 1")
	assert.Contains(t, context, "Dependency 2")
	assert.Contains(t, context, "Elaboration of dependency 2")
}

func TestSoTAgent_AggregationStrategies(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		strategy string
		skeleton []*SkeletonPoint
		check    func(t *testing.T, result string)
	}{
		{
			name:     "sequential aggregation",
			strategy: "sequential",
			skeleton: []*SkeletonPoint{
				{Title: "Point 1", Elaboration: "Elaboration 1"},
				{Title: "Point 2", Elaboration: "Elaboration 2"},
			},
			check: func(t *testing.T, result string) {
				assert.Contains(t, result, "Point 1")
				assert.Contains(t, result, "Elaboration 1")
				assert.Contains(t, result, "Point 2")
				assert.Contains(t, result, "Elaboration 2")
			},
		},
		{
			name:     "hierarchical aggregation",
			strategy: "hierarchical",
			skeleton: []*SkeletonPoint{
				{
					Title:       "Parent",
					Elaboration: "Parent elaboration",
					SubPoints: []*SkeletonPoint{
						{Title: "Child", Elaboration: "Child elaboration"},
					},
				},
			},
			check: func(t *testing.T, result string) {
				assert.Contains(t, result, "Hierarchical analysis")
				assert.Contains(t, result, "Parent")
				assert.Contains(t, result, "Child")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewSoTAgent(SoTConfig{
				Name:                "test-aggregate",
				LLM:                 &MockLLMClient{},
				AggregationStrategy: tt.strategy,
			})

			input := &core.AgentInput{Task: "Test task"}
			result := agent.aggregateResults(ctx, tt.skeleton, input)
			tt.check(t, result)
		})
	}
}

func TestSoTAgent_FormatSkeleton(t *testing.T) {
	agent := NewSoTAgent(SoTConfig{
		Name: "test-format",
		LLM:  &MockLLMClient{},
	})

	skeleton := []*SkeletonPoint{
		{Title: "First Point"},
		{Title: "Second Point", Dependencies: []string{"point_1"}},
		{Title: "Third Point", Dependencies: []string{"point_1", "point_2"}},
	}

	formatted := agent.formatSkeleton(skeleton)

	assert.Contains(t, formatted, "1. First Point")
	assert.Contains(t, formatted, "2. Second Point (depends on: point_1)")
	assert.Contains(t, formatted, "3. Third Point (depends on: point_1, point_2)")
}

func TestSoTAgent_TruncateText(t *testing.T) {
	agent := NewSoTAgent(SoTConfig{
		Name: "test-truncate",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		text     string
		maxLen   int
		expected string
	}{
		{"Short text", 20, "Short text"},
		{"This is a very long text that should be truncated", 10, "This is a ..."},
		{"", 10, ""},
	}

	for _, tt := range tests {
		result := agent.truncateText(tt.text, tt.maxLen)
		assert.Equal(t, tt.expected, result)
	}
}

func TestSoTAgent_ParallelElaboration(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// Setup mock to return elaborations
	// Use mock.Anything for context since parallel elaboration creates timeout contexts
	mockLLM.On("Chat", mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "Elaborated content",
		}, nil,
	)

	agent := NewSoTAgent(SoTConfig{
		Name:           "test-parallel",
		LLM:            mockLLM,
		MaxConcurrency: 3,
	})

	// Create skeleton without dependencies (all can run in parallel)
	skeleton := []*SkeletonPoint{
		{ID: "1", Title: "Point 1", Status: "pending", Metadata: make(map[string]interface{})},
		{ID: "2", Title: "Point 2", Status: "pending", Metadata: make(map[string]interface{})},
		{ID: "3", Title: "Point 3", Status: "pending", Metadata: make(map[string]interface{})},
	}

	input := &core.AgentInput{Task: "Test parallel"}
	output := &core.AgentOutput{
		Steps:     make([]core.AgentStep, 0),
		ToolCalls: make([]core.AgentToolCall, 0),
		Metadata:  make(map[string]interface{}),
	}

	err := agent.elaborateSkeletonParallel(ctx, skeleton, input, output)

	assert.NoError(t, err)
	for _, point := range skeleton {
		assert.Equal(t, "completed", point.Status)
		assert.Equal(t, "Elaborated content", point.Elaboration)
	}
}

func TestSoTAgent_Stream(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	mockLLM.On("Chat", mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "1. Point: Description",
		}, nil,
	).Once()

	mockLLM.On("Chat", mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "Elaboration",
		}, nil,
	)

	agent := NewSoTAgent(SoTConfig{
		Name:        "test-stream",
		Description: "Test Stream",
		LLM:         mockLLM,
	})

	input := &core.AgentInput{
		Task: "Test streaming",
	}

	stream, err := agent.Stream(ctx, input)
	assert.NoError(t, err)
	assert.NotNil(t, stream)

	// Read from stream
	chunk := <-stream
	assert.NotNil(t, chunk.Data)
	assert.True(t, chunk.Done)
}

func TestSoTAgent_WithCallbacks(t *testing.T) {
	callback := &testCallback{
		onStart: func(ctx context.Context, input interface{}) error {
			return nil
		},
	}

	agent := NewSoTAgent(SoTConfig{
		Name:        "test-callbacks",
		Description: "Test Callbacks",
		LLM:         &MockLLMClient{},
	})

	agentWithCallbacks := agent.WithCallbacks(callback)
	assert.NotNil(t, agentWithCallbacks)
}

// Test callback implementation
type testCallback struct {
	onStart  func(context.Context, interface{}) error
	onFinish func(context.Context, interface{}) error
	onError  func(context.Context, error) error
}

func (tc *testCallback) OnStart(ctx context.Context, input interface{}) error {
	if tc.onStart != nil {
		return tc.onStart(ctx, input)
	}
	return nil
}

func (tc *testCallback) OnEnd(ctx context.Context, output interface{}) error {
	return nil
}

func (tc *testCallback) OnAgentFinish(ctx context.Context, output interface{}) error {
	if tc.onFinish != nil {
		return tc.onFinish(ctx, output)
	}
	return nil
}

func (tc *testCallback) OnError(ctx context.Context, err error) error {
	if tc.onError != nil {
		return tc.onError(ctx, err)
	}
	return nil
}

func (tc *testCallback) OnAgentAction(ctx context.Context, action *core.AgentAction) error {
	return nil
}

func (tc *testCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	return nil
}

func (tc *testCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	return nil
}

func (tc *testCallback) OnLLMError(ctx context.Context, err error) error {
	return nil
}

func (tc *testCallback) OnChainStart(ctx context.Context, chainName string, input interface{}) error {
	return nil
}

func (tc *testCallback) OnChainEnd(ctx context.Context, chainName string, output interface{}) error {
	return nil
}

func (tc *testCallback) OnChainError(ctx context.Context, chainName string, err error) error {
	return nil
}

func (tc *testCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	return nil
}

func (tc *testCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	return nil
}

func (tc *testCallback) OnToolError(ctx context.Context, toolName string, err error) error {
	return nil
}

func TestParseNumberedLine(t *testing.T) {
	// 使用 agent 的 parseSkeletonContent 方法测试
	agent := NewSoTAgent(SoTConfig{
		Name: "test-parse-line",
		LLM:  &MockLLMClient{},
	})

	// 使用解析器的 IsStepLine 和 ExtractStepContent 方法
	parser := base.GetDefaultParser()

	tests := []struct {
		line          string
		shouldBeStep  bool
		expectedTitle string
	}{
		{
			line:          "1. [Title]: Description",
			shouldBeStep:  true,
			expectedTitle: "Title",
		},
		{
			line:          "2. Simple title: With description. Depends on: 1",
			shouldBeStep:  true,
			expectedTitle: "Simple title",
		},
		{
			line:          "3. Title only",
			shouldBeStep:  true,
			expectedTitle: "Title only",
		},
		{
			line:         "Not a numbered line",
			shouldBeStep: false,
		},
	}

	for _, tt := range tests {
		isStep, _ := parser.IsStepLine(tt.line)
		assert.Equal(t, tt.shouldBeStep, isStep, "IsStepLine mismatch for: %s", tt.line)

		if tt.shouldBeStep {
			// 使用 agent 的 parseSkeletonContent 方法解析内容
			content := parser.ExtractStepContent(tt.line)
			if content != "" {
				result := agent.parseSkeletonContent(content)
				assert.NotNil(t, result, "parseSkeletonContent should not return nil for: %s", tt.line)
				if result != nil {
					assert.Equal(t, tt.expectedTitle, result["title"], "Title mismatch for: %s", tt.line)
				}
			}
		}
	}
}

func TestParseDependencies(t *testing.T) {
	tests := []struct {
		deps     string
		expected []string
	}{
		{"1, 2, 3", []string{"point_1", "point_2", "point_3"}},
		{"point_1, point_2", []string{"point_1", "point_2"}},
		{"1", []string{"point_1"}},
		{"", []string{}},
	}

	for _, tt := range tests {
		result := parseDependencies(tt.deps)
		assert.Equal(t, tt.expected, result)
	}
}

func TestSoTAgent_ConcurrentElaborationWithDependencies(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// Track elaboration order
	var mu sync.Mutex
	elaborationOrder := []string{}

	// Use mock.Anything for context since parallel elaboration creates timeout contexts
	mockLLM.On("Chat", mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{Content: "Elaborated"},
		nil,
	).Run(func(args mock.Arguments) {
		messages := args.Get(1).([]llm.Message)
		// Extract point ID from the prompt
		for _, msg := range messages {
			if strings.Contains(msg.Content, "Point 1") {
				mu.Lock()
				elaborationOrder = append(elaborationOrder, "1")
				mu.Unlock()
			} else if strings.Contains(msg.Content, "Point 2") {
				mu.Lock()
				elaborationOrder = append(elaborationOrder, "2")
				mu.Unlock()
			} else if strings.Contains(msg.Content, "Point 3") {
				mu.Lock()
				elaborationOrder = append(elaborationOrder, "3")
				mu.Unlock()
			}
		}
	})

	agent := NewSoTAgent(SoTConfig{
		Name:            "test-deps",
		LLM:             mockLLM,
		MaxConcurrency:  3,
		DependencyAware: true,
	})

	// Create skeleton with dependencies: 1 -> 2 -> 3
	skeleton := []*SkeletonPoint{
		{ID: "point_1", Title: "Point 1", Status: "pending", Metadata: make(map[string]interface{})},
		{ID: "point_2", Title: "Point 2", Status: "pending", Dependencies: []string{"point_1"}, Metadata: make(map[string]interface{})},
		{ID: "point_3", Title: "Point 3", Status: "pending", Dependencies: []string{"point_2"}, Metadata: make(map[string]interface{})},
	}

	input := &core.AgentInput{Task: "Test dependencies"}
	output := &core.AgentOutput{
		Steps:     make([]core.AgentStep, 0),
		ToolCalls: make([]core.AgentToolCall, 0),
		Metadata:  make(map[string]interface{}),
	}

	err := agent.elaborateSkeletonParallel(ctx, skeleton, input, output)

	assert.NoError(t, err)
	// Verify all points were elaborated
	for _, point := range skeleton {
		assert.Equal(t, "completed", point.Status)
	}
}

// TestSoTAgent_RunGenerator tests the RunGenerator method
func TestSoTAgent_RunGenerator(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// Mock skeleton generation
	skeletonResponse := `Point 1: Understand the problem
Point 2: Break down into steps
Point 3: Implement solution`
	mockLLM.On("Chat", mock.Anything, mock.Anything).Return(&llm.CompletionResponse{
		Content:    skeletonResponse,
		TokensUsed: 50,
	}, nil).Once()

	// Mock elaborations for each skeleton point
	mockLLM.On("Chat", mock.Anything, mock.Anything).Return(&llm.CompletionResponse{
		Content:    "Detailed elaboration for this point",
		TokensUsed: 30,
	}, nil).Times(3)

	agent := NewSoTAgent(SoTConfig{
		Name:                "test-sot-gen",
		Description:         "Test SoT Agent with Generator",
		LLM:                 mockLLM,
		MaxConcurrency:      2,
		MaxSkeletonPoints:   5,
		MinSkeletonPoints:   2,
		AggregationStrategy: "sequential",
	})

	input := &core.AgentInput{
		Task: "Test task for generator",
	}

	// Collect all outputs from generator
	var outputs []*core.AgentOutput
	var finalOutput *core.AgentOutput
	var foundSkeleton bool
	var foundElaboration bool

	for output, err := range agent.RunGenerator(ctx, input) {
		if err != nil {
			t.Logf("Error at step %d: %v", len(outputs)+1, err)
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

		// Check if skeleton was generated
		if stepType, ok := output.Metadata["step_type"].(string); ok {
			if stepType == "skeleton_generated" {
				foundSkeleton = true
				t.Log("Skeleton generated!")
			}
			if stepType == "elaboration_completed" {
				foundElaboration = true
				t.Log("Elaboration completed!")
			}
		}

		// Log step type
		t.Logf("Step %d: %s - %s", len(outputs), output.Metadata["step_type"], output.Message)

		// Break on final output
		if output.Metadata["step_type"] == "final" {
			break
		}
	}

	// Verify we got multiple outputs
	assert.NotEmpty(t, outputs, "RunGenerator should produce outputs")
	assert.GreaterOrEqual(t, len(outputs), 3, "Should have at least 3 outputs (skeleton, elaboration, final)")

	t.Logf("Total outputs: %d", len(outputs))
	t.Logf("Found skeleton: %v", foundSkeleton)
	t.Logf("Found elaboration: %v", foundElaboration)

	// Verify final output exists
	assert.NotNil(t, finalOutput, "Final output should not be nil")

	// Verify we found skeleton and elaboration stages
	assert.True(t, foundSkeleton, "Should have generated skeleton")
	assert.True(t, foundElaboration, "Should have completed elaboration")

	// Verify final output status
	if finalOutput != nil {
		assert.Equal(t, interfaces.StatusSuccess, finalOutput.Status, "Final status should be success")
		assert.Equal(t, "final", finalOutput.Metadata["step_type"], "Last output should be final")
	}

	// Log final result
	t.Logf("Final result: %v", finalOutput.Result)
	t.Logf("Total reasoning steps: %d", len(finalOutput.Steps))

	mockLLM.AssertExpectations(t)
}

// TestSoTAgent_RunGenerator_EarlyTermination tests early termination
func TestSoTAgent_RunGenerator_EarlyTermination(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// Mock skeleton generation
	skeletonResponse := `Point 1: First point
Point 2: Second point`
	mockLLM.On("Chat", mock.Anything, mock.Anything).Return(&llm.CompletionResponse{
		Content:    skeletonResponse,
		TokensUsed: 50,
	}, nil).Once()

	agent := NewSoTAgent(SoTConfig{
		Name:           "test-sot-early",
		LLM:            mockLLM,
		MaxConcurrency: 2,
	})

	input := &core.AgentInput{
		Task: "Test early termination",
	}

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
	assert.Equal(t, maxOutputs, outputCount, "Should terminate after exactly %d outputs", maxOutputs)

	t.Logf("Successfully terminated early after %d outputs", outputCount)

	mockLLM.AssertExpectations(t)
}
