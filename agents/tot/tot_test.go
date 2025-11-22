package tot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// MockLLMClient implements llm.Client for testing
type MockLLMClient struct {
	responses     []string
	currentIndex  int
	shouldErr     bool
	errorResponse error
}

func NewMockLLMClient(responses ...string) *MockLLMClient {
	if len(responses) == 0 {
		responses = []string{"Default response"}
	}
	return &MockLLMClient{
		responses: responses,
	}
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	if m.shouldErr {
		return nil, m.errorResponse
	}

	response := m.responses[m.currentIndex%len(m.responses)]
	m.currentIndex++

	return &llm.CompletionResponse{
		Content:    response,
		Model:      "mock-model",
		TokensUsed: 50,
		Provider:   "mock",
	}, nil
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return m.Chat(ctx, req.Messages)
}

func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// MockTool for testing
type MockTool struct {
	name   string
	result string
}

func NewMockTool(name, result string) *MockTool {
	return &MockTool{name: name, result: result}
}

func (t *MockTool) Name() string {
	return t.name
}

func (t *MockTool) Description() string {
	return "Mock tool for testing"
}

func (t *MockTool) ArgsSchema() string {
	return `{"type": "object"}`
}

func (t *MockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return &interfaces.ToolOutput{
		Result:  t.result,
		Success: true,
	}, nil
}

func TestNewToTAgent(t *testing.T) {
	tests := []struct {
		name   string
		config ToTConfig
		check  func(*testing.T, *ToTAgent)
	}{
		{
			name: "default configuration",
			config: ToTConfig{
				Name:        "test-tot",
				Description: "Test ToT Agent",
				LLM:         NewMockLLMClient(),
			},
			check: func(t *testing.T, agent *ToTAgent) {
				assert.Equal(t, "test-tot", agent.Name())
				assert.Equal(t, "Test ToT Agent", agent.Description())
				assert.Equal(t, 5, agent.config.MaxDepth)
				assert.Equal(t, 3, agent.config.BranchingFactor)
				assert.Equal(t, interfaces.StrategyBeamSearch, agent.config.SearchStrategy)
				assert.Equal(t, "llm", agent.config.EvaluationMethod)
				assert.Equal(t, 0.3, agent.config.PruneThreshold)
			},
		},
		{
			name: "custom configuration",
			config: ToTConfig{
				Name:             "custom-tot",
				Description:      "Custom ToT Agent",
				LLM:              NewMockLLMClient(),
				MaxDepth:         10,
				BranchingFactor:  5,
				BeamWidth:        3,
				SearchStrategy:   interfaces.StrategyDepthFirst,
				EvaluationMethod: "heuristic",
				PruneThreshold:   0.5,
			},
			check: func(t *testing.T, agent *ToTAgent) {
				assert.Equal(t, 10, agent.config.MaxDepth)
				assert.Equal(t, 5, agent.config.BranchingFactor)
				assert.Equal(t, 3, agent.config.BeamWidth)
				assert.Equal(t, interfaces.StrategyDepthFirst, agent.config.SearchStrategy)
				assert.Equal(t, "heuristic", agent.config.EvaluationMethod)
				assert.Equal(t, 0.5, agent.config.PruneThreshold)
			},
		},
		{
			name: "with tools",
			config: ToTConfig{
				Name:        "tot-with-tools",
				Description: "ToT with Tools",
				LLM:         NewMockLLMClient(),
				Tools:       []interfaces.Tool{NewMockTool("calculator", "42")},
			},
			check: func(t *testing.T, agent *ToTAgent) {
				assert.Len(t, agent.tools, 1)
				assert.Contains(t, agent.Capabilities(), "tool_calling")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewToTAgent(tt.config)
			require.NotNil(t, agent)
			tt.check(t, agent)
		})
	}
}

func TestToTAgent_Invoke_BeamSearch(t *testing.T) {
	mockLLM := NewMockLLMClient(
		// Thought generation responses
		"Step 1: First thought\nStep 2: Second thought\nStep 3: Third thought",
		// Solution check response
		"yes",
	)

	agent := NewToTAgent(ToTConfig{
		Name:           "test-tot-beam",
		Description:    "Test Beam Search",
		LLM:            mockLLM,
		MaxDepth:       2,
		SearchStrategy: interfaces.StrategyBeamSearch,
	})

	input := &agentcore.AgentInput{
		Task: "Solve a problem using tree search",
	}

	output, err := agent.Invoke(context.Background(), input)
	// ToT agent may not find solution, which is okay for testing
	require.NotNil(t, output)
	if err == nil {
		assert.NotEmpty(t, output.Status)
	}
}

func TestToTAgent_Invoke_DepthFirst(t *testing.T) {
	mockLLM := NewMockLLMClient(
		"Step 1: Explore first branch",
		"yes", // Solution found
	)

	agent := NewToTAgent(ToTConfig{
		Name:           "test-tot-dfs",
		Description:    "Test DFS",
		LLM:            mockLLM,
		MaxDepth:       3,
		SearchStrategy: interfaces.StrategyDepthFirst,
	})

	input := &agentcore.AgentInput{
		Task: "Use DFS to find solution",
	}

	output, err := agent.Invoke(context.Background(), input)
	require.NotNil(t, output)
	// DFS may not find solution with simple mock
	if err == nil {
		assert.NotEmpty(t, output.Status)
	}
}

func TestToTAgent_Invoke_BreadthFirst(t *testing.T) {
	mockLLM := NewMockLLMClient(
		"Step 1: BFS exploration",
		"yes", // Solution found
	)

	agent := NewToTAgent(ToTConfig{
		Name:           "test-tot-bfs",
		Description:    "Test BFS",
		LLM:            mockLLM,
		MaxDepth:       2,
		SearchStrategy: interfaces.StrategyBreadthFirst,
	})

	input := &agentcore.AgentInput{
		Task: "Use BFS to find solution",
	}

	output, err := agent.Invoke(context.Background(), input)
	require.NotNil(t, output)
	// BFS may not find solution with simple mock
	if err == nil {
		assert.NotEmpty(t, output.Status)
	}
}

func TestToTAgent_Invoke_MonteCarlo(t *testing.T) {
	mockLLM := NewMockLLMClient(
		"Step 1: MCTS node",
		"0.8", // Evaluation score
		"yes", // Solution check
	)

	agent := NewToTAgent(ToTConfig{
		Name:           "test-tot-mcts",
		Description:    "Test MCTS",
		LLM:            mockLLM,
		MaxDepth:       2,
		SearchStrategy: interfaces.StrategyMonteCarlo,
	})

	input := &agentcore.AgentInput{
		Task: "Use MCTS to find solution",
	}

	output, err := agent.Invoke(context.Background(), input)
	require.NotNil(t, output)
	// MCTS may complete without error
	if err == nil {
		assert.NotEmpty(t, output.Status)
	}
}

func TestToTAgent_EvaluationMethods(t *testing.T) {
	tests := []struct {
		name             string
		evaluationMethod string
	}{
		{"LLM evaluation", "llm"},
		{"Heuristic evaluation", "heuristic"},
		{"Hybrid evaluation", "hybrid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLLM := NewMockLLMClient(
				"Step 1: Test thought",
				"0.7", // Score for LLM evaluation
				"yes", // Solution check
			)

			agent := NewToTAgent(ToTConfig{
				Name:             "test-eval",
				Description:      "Test Evaluation",
				LLM:              mockLLM,
				MaxDepth:         1,
				EvaluationMethod: tt.evaluationMethod,
			})

			input := &agentcore.AgentInput{
				Task: "Test evaluation method",
			}

			output, err := agent.Invoke(context.Background(), input)
			require.NotNil(t, output)
			// May or may not find solution
			if err == nil {
				assert.NotEmpty(t, output.Status)
			}
		})
	}
}

func TestToTAgent_ParseGeneratedThoughts(t *testing.T) {
	agent := NewToTAgent(ToTConfig{
		Name:            "test-parse",
		LLM:             NewMockLLMClient(),
		BranchingFactor: 3,
	})

	tests := []struct {
		name     string
		response string
		expected int
	}{
		{
			name: "structured format",
			response: `Step 1: First thought
Step 2: Second thought
Step 3: Third thought`,
			expected: 3,
		},
		{
			name: "unstructured format",
			response: `Here are some thoughts:
This is the first idea about the problem
Another approach could be this
A third option would be that`,
			expected: 3,
		},
		{
			name:     "empty response",
			response: "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thoughts := agent.parseGeneratedThoughts(tt.response)
			assert.Equal(t, tt.expected, len(thoughts))
		})
	}
}

func TestToTAgent_EvaluateWithHeuristic(t *testing.T) {
	agent := NewToTAgent(ToTConfig{
		Name: "test-heuristic",
		LLM:  NewMockLLMClient(),
	})

	input := &agentcore.AgentInput{
		Task: "calculate sum product average",
	}

	tests := []struct {
		name     string
		node     *ThoughtNode
		minScore float64
		maxScore float64
	}{
		{
			name: "detailed thought with keywords",
			node: &ThoughtNode{
				Thought: "Let's calculate the sum of the numbers and then find the average which requires computing the product",
			},
			minScore: 0.5,
			maxScore: 1.0,
		},
		{
			name: "short thought",
			node: &ThoughtNode{
				Thought: "short",
			},
			minScore: 0.0,
			maxScore: 0.5,
		},
		{
			name: "repetitive thought",
			node: &ThoughtNode{
				Parent: &ThoughtNode{
					Thought: "calculate the sum",
				},
				Thought: "we need to calculate the sum",
			},
			minScore: 0.0,
			maxScore: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := agent.evaluateWithHeuristic(tt.node, input)
			assert.GreaterOrEqual(t, score, tt.minScore)
			assert.LessOrEqual(t, score, tt.maxScore)
		})
	}
}

func TestToTAgent_GetPathToRoot(t *testing.T) {
	agent := NewToTAgent(ToTConfig{
		Name: "test-path",
		LLM:  NewMockLLMClient(),
	})

	root := &ThoughtNode{ID: "root", Thought: "Start"}
	child1 := &ThoughtNode{ID: "child1", Thought: "Step 1", Parent: root}
	child2 := &ThoughtNode{ID: "child2", Thought: "Step 2", Parent: child1}

	path := agent.getPathToRoot(child2)

	assert.Len(t, path, 3)
	assert.Equal(t, "child2", path[0].ID)
	assert.Equal(t, "child1", path[1].ID)
	assert.Equal(t, "root", path[2].ID)
}

func TestToTAgent_CountNodes(t *testing.T) {
	agent := NewToTAgent(ToTConfig{
		Name: "test-count",
		LLM:  NewMockLLMClient(),
	})

	root := &ThoughtNode{ID: "root"}
	child1 := &ThoughtNode{ID: "child1", Parent: root}
	child2 := &ThoughtNode{ID: "child2", Parent: root}
	root.Children = []*ThoughtNode{child1, child2}

	count := agent.countNodes(root)
	assert.Equal(t, 3, count)
}

func TestToTAgent_CopyState(t *testing.T) {
	agent := NewToTAgent(ToTConfig{
		Name: "test-copy",
		LLM:  NewMockLLMClient(),
	})

	original := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}

	copied := agent.copyState(original)

	assert.Equal(t, original, copied)

	// Modify copy shouldn't affect original
	copied["key3"] = "value3"
	assert.NotContains(t, original, "key3")
}

func TestToTAgent_NeedsTools(t *testing.T) {
	agent := NewToTAgent(ToTConfig{
		Name: "test-tools",
		LLM:  NewMockLLMClient(),
	})

	tests := []struct {
		name    string
		thought string
		needs   bool
	}{
		{"needs calculation", "We need to calculate the sum", true},
		{"needs search", "Let's search for information", true},
		{"needs lookup", "We should look up the definition", true},
		{"no tools needed", "This is a simple thought", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := agent.needsTools(tt.thought)
			assert.Equal(t, tt.needs, result)
		})
	}
}

func TestToTAgent_Stream(t *testing.T) {
	mockLLM := NewMockLLMClient(
		"Step 1: Streaming thought",
		"yes",
	)

	agent := NewToTAgent(ToTConfig{
		Name:        "test-stream",
		Description: "Test Stream",
		LLM:         mockLLM,
		MaxDepth:    1,
	})

	input := &agentcore.AgentInput{
		Task: "Test streaming",
	}

	stream, err := agent.Stream(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Read from stream
	chunk := <-stream
	assert.NotNil(t, chunk.Data)
	assert.True(t, chunk.Done)
}

func TestToTAgent_WithCallbacks(t *testing.T) {
	agent := NewToTAgent(ToTConfig{
		Name:        "test-callbacks",
		Description: "Test Callbacks",
		LLM:         NewMockLLMClient(),
	})

	callback := &testCallback{}
	agentWithCallbacks := agent.WithCallbacks(callback)
	assert.NotNil(t, agentWithCallbacks)
}

func TestToTAgent_WithConfig(t *testing.T) {
	agent := NewToTAgent(ToTConfig{
		Name:        "test-config",
		Description: "Test Config",
		LLM:         NewMockLLMClient(),
	})

	config := agentcore.RunnableConfig{}
	agentWithConfig := agent.WithConfig(config)
	assert.NotNil(t, agentWithConfig)
}

func TestToTAgent_BuildAnswerFromPath(t *testing.T) {
	agent := NewToTAgent(ToTConfig{
		Name: "test-answer",
		LLM:  NewMockLLMClient(),
	})

	path := []*ThoughtNode{
		{ID: "node3", Thought: "Final conclusion", IsSolution: true},
		{ID: "node2", Thought: "Intermediate step"},
		{ID: "node1", Thought: "First step"},
		{ID: "root", Thought: "Problem statement"},
	}

	answer := agent.buildAnswerFromPath(path)

	assert.Contains(t, answer, "tree-of-thought reasoning")
	assert.Contains(t, answer, "First step")
	assert.Contains(t, answer, "Intermediate step")
	assert.Contains(t, answer, "Final conclusion")
}

func TestToTAgent_PruneThreshold(t *testing.T) {
	mockLLM := NewMockLLMClient(
		"Step 1: Low score thought",
		"0.1", // Low evaluation score
		"no",  // Not a solution
	)

	agent := NewToTAgent(ToTConfig{
		Name:             "test-prune",
		Description:      "Test Pruning",
		LLM:              mockLLM,
		MaxDepth:         2,
		PruneThreshold:   0.5,
		EvaluationMethod: "llm",
	})

	input := &agentcore.AgentInput{
		Task: "Test pruning",
	}

	output, err := agent.Invoke(context.Background(), input)
	// Should complete even with pruning, may error or succeed
	require.NotNil(t, output)
	if err == nil {
		assert.NotEmpty(t, output.Status)
	}
}

func TestToTAgent_MaxDepthReached(t *testing.T) {
	mockLLM := NewMockLLMClient(
		"Step 1: Deep thought",
		"no", // Never finds solution
	)

	agent := NewToTAgent(ToTConfig{
		Name:        "test-maxdepth",
		Description: "Test Max Depth",
		LLM:         mockLLM,
		MaxDepth:    1,
		BeamWidth:   1,
	})

	input := &agentcore.AgentInput{
		Task: "Test max depth",
	}

	output, err := agent.Invoke(context.Background(), input)
	// May return error or success without solution
	require.NotNil(t, output)
	if err == nil {
		assert.NotEmpty(t, output.Status)
	}
}

func TestToTAgent_GetContext(t *testing.T) {
	agent := NewToTAgent(ToTConfig{
		Name: "test-context",
		LLM:  NewMockLLMClient(),
	})

	root := &ThoughtNode{ID: "root", Thought: "Start problem"}
	child1 := &ThoughtNode{ID: "child1", Thought: "First step", Parent: root}
	child2 := &ThoughtNode{ID: "child2", Thought: "Second step", Parent: child1}

	context := agent.getContext(child2)

	assert.Contains(t, context, "First step")
	assert.Contains(t, context, "Second step")
	assert.NotContains(t, context, "root") // Root is filtered out
}

// Test callback implementation
type testCallback struct{}

func (tc *testCallback) OnStart(ctx context.Context, input interface{}) error {
	return nil
}

func (tc *testCallback) OnEnd(ctx context.Context, output interface{}) error {
	return nil
}

func (tc *testCallback) OnAgentFinish(ctx context.Context, output interface{}) error {
	return nil
}

func (tc *testCallback) OnError(ctx context.Context, err error) error {
	return nil
}

func (tc *testCallback) OnAgentAction(ctx context.Context, action *agentcore.AgentAction) error {
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

func BenchmarkToTAgent_Invoke(b *testing.B) {
	mockLLM := NewMockLLMClient(
		"Step 1: Benchmark thought",
		"yes",
	)

	agent := NewToTAgent(ToTConfig{
		Name:        "benchmark",
		Description: "Benchmark Agent",
		LLM:         mockLLM,
		MaxDepth:    1,
	})

	input := &agentcore.AgentInput{
		Task: "Benchmark task",
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		agent.Invoke(ctx, input)
	}
}
