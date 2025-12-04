package got

import (
	"context"
	"strings"
	"testing"

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

func TestNewGoTAgent(t *testing.T) {
	tests := []struct {
		name   string
		config GoTConfig
		check  func(t *testing.T, agent *GoTAgent)
	}{
		{
			name: "default configuration",
			config: GoTConfig{
				Name:        "test-got",
				Description: "Test GoT Agent",
				LLM:         &MockLLMClient{},
			},
			check: func(t *testing.T, agent *GoTAgent) {
				assert.Equal(t, "test-got", agent.Name())
				assert.Equal(t, "Test GoT Agent", agent.Description())
				assert.Equal(t, 10, agent.config.MaxNodes)       // 优化后的默认值
				assert.Equal(t, 3, agent.config.MaxEdgesPerNode) // 优化后的默认值
				assert.Equal(t, "weighted", agent.config.MergeStrategy)
				assert.Equal(t, 0.5, agent.config.PruneThreshold) // 优化后的默认值
			},
		},
		{
			name: "custom configuration",
			config: GoTConfig{
				Name:              "custom-got",
				Description:       "Custom GoT Agent",
				LLM:               &MockLLMClient{},
				MaxNodes:          100,
				MaxEdgesPerNode:   10,
				ParallelExecution: true,
				MergeStrategy:     "llm",
				CycleDetection:    true,
				PruneThreshold:    0.5,
			},
			check: func(t *testing.T, agent *GoTAgent) {
				assert.Equal(t, 100, agent.config.MaxNodes)
				assert.Equal(t, 10, agent.config.MaxEdgesPerNode)
				assert.True(t, agent.config.ParallelExecution)
				assert.Equal(t, "llm", agent.config.MergeStrategy)
				assert.True(t, agent.config.CycleDetection)
				assert.Equal(t, 0.5, agent.config.PruneThreshold)
			},
		},
		{
			name: "with tools",
			config: GoTConfig{
				Name:        "got-with-tools",
				Description: "GoT with Tools",
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
			check: func(t *testing.T, agent *GoTAgent) {
				assert.Len(t, agent.tools, 1)
				assert.Contains(t, agent.Capabilities(), "tool_calling")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewGoTAgent(tt.config)
			assert.NotNil(t, agent)
			tt.check(t, agent)
		})
	}
}

func TestGoTAgent_Invoke(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// Setup mock for processNode calls (node analysis/answer)
	// 使用 mock.Anything 匹配带 timeout 的 context
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(messages []llm.Message) bool {
		if len(messages) > 0 {
			return strings.Contains(messages[0].Content, "Provide your analysis or answer")
		}
		return false
	})).Return(
		&llm.CompletionResponse{
			Content: "Solar energy offers clean, renewable power with minimal environmental impact. Wind power provides consistent energy generation in many regions.",
		}, nil,
	).Maybe()

	// Setup mock for thought generation requests
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(messages []llm.Message) bool {
		if len(messages) > 0 {
			return strings.Contains(messages[0].Content, "Generate") &&
				strings.Contains(messages[0].Content, "follow-up thoughts")
		}
		return false
	})).Return(
		&llm.CompletionResponse{
			Content: "- Analysis of solar energy benefits\n- Consideration of wind power advantages",
		}, nil,
	).Maybe()

	// Setup mock for evaluation requests (only used when FastEvaluation=false)
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(messages []llm.Message) bool {
		if len(messages) > 0 {
			return strings.Contains(messages[0].Content, "Rate the following thought")
		}
		return false
	})).Return(
		&llm.CompletionResponse{
			Content: "0.8",
		}, nil,
	).Maybe()

	agent := NewGoTAgent(GoTConfig{
		Name:              "test-got",
		Description:       "Test GoT",
		LLM:               mockLLM,
		MaxNodes:          5, // Reduced from 10 to minimize calls
		ParallelExecution: false,
		MergeStrategy:     "weighted",
		FastEvaluation:    true, // 启用快速评估，减少 LLM 调用
	})

	input := &core.AgentInput{
		Task:    "Analyze the benefits of renewable energy",
		Context: make(map[string]interface{}),
	}

	output, err := agent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Status)
	assert.NotEmpty(t, output.Result)
	// mockLLM.AssertExpectations(t) // Removed due to Maybe()
}

func TestGoTAgent_ParallelExecution(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// Setup mock for processNode calls
	// 使用 mock.Anything 匹配带 timeout 的 context
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(messages []llm.Message) bool {
		if len(messages) > 0 {
			return strings.Contains(messages[0].Content, "Provide your analysis or answer")
		}
		return false
	})).Return(
		&llm.CompletionResponse{
			Content: "Parallel thought analysis result",
		}, nil,
	).Maybe()

	// Setup mock for thought generation
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(messages []llm.Message) bool {
		if len(messages) > 0 {
			return strings.Contains(messages[0].Content, "Generate") &&
				strings.Contains(messages[0].Content, "follow-up thoughts")
		}
		return false
	})).Return(
		&llm.CompletionResponse{
			Content: "- Parallel thought 1\n- Parallel thought 2",
		}, nil,
	).Maybe()

	// Setup mock for evaluation (only used when FastEvaluation=false)
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(messages []llm.Message) bool {
		if len(messages) > 0 {
			return strings.Contains(messages[0].Content, "Rate the following thought")
		}
		return false
	})).Return(
		&llm.CompletionResponse{
			Content: "0.8",
		}, nil,
	).Maybe()

	agent := NewGoTAgent(GoTConfig{
		Name:              "test-parallel-got",
		Description:       "Test Parallel GoT",
		LLM:               mockLLM,
		MaxNodes:          5,
		ParallelExecution: true,
		MergeStrategy:     "vote",
		FastEvaluation:    true, // 启用快速评估
	})

	input := &core.AgentInput{
		Task: "Test parallel execution",
	}

	output, err := agent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.True(t, output.Metadata["parallel_execution"].(bool))
}

func TestGoTAgent_CycleDetection(t *testing.T) {
	agent := NewGoTAgent(GoTConfig{
		Name:           "test-cycle",
		Description:    "Test Cycle Detection",
		LLM:            &MockLLMClient{},
		CycleDetection: true,
	})

	// Create a graph with a cycle
	node1 := &GraphNode{ID: "1", Status: "pending"}
	node2 := &GraphNode{ID: "2", Status: "pending"}
	node3 := &GraphNode{ID: "3", Status: "pending"}

	// Create cycle: 1 -> 2 -> 3 -> 1
	node1.Dependents = []*GraphNode{node2}
	node2.Dependents = []*GraphNode{node3}
	node3.Dependents = []*GraphNode{node1}

	graph := []*GraphNode{node1, node2, node3}

	hasCycle := agent.hasCycles(graph)
	assert.True(t, hasCycle)
}

func TestGoTAgent_TopologicalSort(t *testing.T) {
	agent := NewGoTAgent(GoTConfig{
		Name:        "test-topo",
		Description: "Test Topological Sort",
		LLM:         &MockLLMClient{},
	})

	// Create a DAG
	node1 := &GraphNode{ID: "1", Status: "pending"}
	node2 := &GraphNode{ID: "2", Status: "pending"}
	node3 := &GraphNode{ID: "3", Status: "pending"}

	// Dependencies: 1 -> 2 -> 3
	node1.Dependents = []*GraphNode{node2}
	node2.Dependents = []*GraphNode{node3}

	graph := []*GraphNode{node1, node2, node3}

	sorted, err := agent.topologicalSort(graph)

	assert.NoError(t, err)
	assert.Len(t, sorted, 3)
	// Verify topological order
	assert.Equal(t, "1", sorted[0].ID)
	assert.Equal(t, "2", sorted[1].ID)
	assert.Equal(t, "3", sorted[2].ID)
}

func TestGoTAgent_MergeStrategies(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		strategy string
		nodes    []*GraphNode
		check    func(t *testing.T, result interface{})
	}{
		{
			name:     "voting strategy",
			strategy: "vote",
			nodes: []*GraphNode{
				{ID: "1", Score: 0.8, Result: "Answer A"},
				{ID: "2", Score: 0.7, Result: "Answer A"},
				{ID: "3", Score: 0.6, Result: "Answer B"},
			},
			check: func(t *testing.T, result interface{}) {
				assert.Equal(t, "Answer A", result) // A has 2 votes
			},
		},
		{
			name:     "weighted strategy",
			strategy: "weighted",
			nodes: []*GraphNode{
				{ID: "1", Score: 0.9, Result: "High score result"},
				{ID: "2", Score: 0.5, Result: "Low score result"},
			},
			check: func(t *testing.T, result interface{}) {
				assert.Contains(t, result.(string), "Combined insights")
				assert.Contains(t, result.(string), "Weight:")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewGoTAgent(GoTConfig{
				Name:          "test-merge",
				Description:   "Test Merge",
				LLM:           &MockLLMClient{},
				MergeStrategy: tt.strategy,
			})

			result, err := agent.mergeResults(ctx, tt.nodes)
			assert.NoError(t, err)
			tt.check(t, result)
		})
	}
}

func TestGoTAgent_Stream(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// Setup mock for processNode calls
	// 使用 mock.Anything 匹配带 timeout 的 context
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(messages []llm.Message) bool {
		if len(messages) > 0 {
			return strings.Contains(messages[0].Content, "Provide your analysis or answer")
		}
		return false
	})).Return(
		&llm.CompletionResponse{
			Content: "Streaming response with analysis",
		}, nil,
	).Maybe()

	// Setup mock for thought generation
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(messages []llm.Message) bool {
		if len(messages) > 0 {
			return strings.Contains(messages[0].Content, "Generate") &&
				strings.Contains(messages[0].Content, "follow-up thoughts")
		}
		return false
	})).Return(
		&llm.CompletionResponse{
			Content: "- First thought\n- Second thought",
		}, nil,
	).Maybe()

	// Setup mock for evaluation (only used when FastEvaluation=false)
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(messages []llm.Message) bool {
		if len(messages) > 0 {
			return strings.Contains(messages[0].Content, "Rate the following thought")
		}
		return false
	})).Return(
		&llm.CompletionResponse{
			Content: "0.8",
		}, nil,
	).Maybe()

	agent := NewGoTAgent(GoTConfig{
		Name:           "test-stream",
		Description:    "Test Stream",
		LLM:            mockLLM,
		MaxNodes:       3, // Limit to speed up test
		FastEvaluation: true,
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

func TestGoTAgent_WithCallbacks(t *testing.T) {
	callback := &testCallback{
		onStart: func(ctx context.Context, input interface{}) error {
			return nil
		},
	}

	agent := NewGoTAgent(GoTConfig{
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

func TestGoTAgent_EvaluateThought(t *testing.T) {
	ctx := context.Background()

	// Test various score responses
	tests := []struct {
		name          string
		llmResponse   string
		expectedScore float64
	}{
		{"valid score", "0.75", 0.75},
		{"invalid format", "not a number", 0.5},
		{"out of range high", "2.0", 1.0},
		{"out of range low", "-0.5", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLLM := new(MockLLMClient)
			// 使用 mock.Anything 匹配带 timeout 的 context
			mockLLM.On("Chat", mock.Anything, mock.Anything).Return(
				&llm.CompletionResponse{Content: tt.llmResponse}, nil,
			).Once()

			agent := NewGoTAgent(GoTConfig{
				Name:           "test-evaluate",
				LLM:            mockLLM,
				FastEvaluation: false, // 禁用快速评估以测试 LLM 评估
			})

			score := agent.evaluateThought(ctx, "test thought", &core.AgentInput{Task: "test"})
			assert.Equal(t, tt.expectedScore, score)
		})
	}
}

func TestGoTAgent_GenerateThoughtsFromNode(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	mockLLM.On("Chat", ctx, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "- First thought\n- Second thought\n- Third thought",
		}, nil,
	)

	agent := NewGoTAgent(GoTConfig{
		Name: "test-generate",
		LLM:  mockLLM,
	})

	node := &GraphNode{
		ID:      "test",
		Thought: "Initial thought",
	}

	thoughts := agent.generateThoughtsFromNode(ctx, node, &core.AgentInput{Task: "test"})

	assert.Len(t, thoughts, 3)
	assert.Equal(t, "First thought", thoughts[0])
	assert.Equal(t, "Second thought", thoughts[1])
	assert.Equal(t, "Third thought", thoughts[2])
}

func TestGoTAgent_GroupByDepth(t *testing.T) {
	agent := NewGoTAgent(GoTConfig{
		Name: "test-depth",
		LLM:  &MockLLMClient{},
	})

	// Create nodes with different depths
	node1 := &GraphNode{ID: "1"}
	node2 := &GraphNode{ID: "2", Dependencies: []*GraphNode{node1}}
	node3 := &GraphNode{ID: "3", Dependencies: []*GraphNode{node1}}
	node4 := &GraphNode{ID: "4", Dependencies: []*GraphNode{node2, node3}}

	sorted := []*GraphNode{node1, node2, node3, node4}
	waves := agent.groupByDepth(sorted)

	assert.Len(t, waves, 3)
	assert.Contains(t, waves[0], node1)
	assert.Contains(t, waves[1], node2)
	assert.Contains(t, waves[1], node3)
	assert.Contains(t, waves[2], node4)
}

func TestGoTAgent_AreThoughtsRelated(t *testing.T) {
	// 使用全局解析器测试，因为 agent.parser 是私有字段
	parser := base.GetDefaultParser()

	tests := []struct {
		thought1 string
		thought2 string
		expected bool
	}{
		{
			"Therefore, we conclude",
			"Therefore, the result is",
			true,
		},
		{
			"Analysis shows",
			"Analysis indicates",
			true,
		},
		{
			"Random thought",
			"Another unrelated idea",
			false,
		},
	}

	for _, tt := range tests {
		related := parser.AreThoughtsRelated(tt.thought1, tt.thought2)
		assert.Equal(t, tt.expected, related)
	}
}

// TestGoTAgent_FastEvaluation 测试快速评估模式
func TestGoTAgent_FastEvaluation(t *testing.T) {
	agent := NewGoTAgent(GoTConfig{
		Name:           "test-fast-eval",
		LLM:            &MockLLMClient{},
		FastEvaluation: true,
	})

	tests := []struct {
		name         string
		thought      string
		task         string
		expectHigher float64 // 期望分数高于此值
		expectLower  float64 // 期望分数低于此值
	}{
		{
			name:         "relevant long thought",
			thought:      "分析 Go 语言的并发特性，因为它是解决这个问题的关键",
			task:         "分析 Go 语言的优势",
			expectHigher: 0.5,
			expectLower:  1.0,
		},
		{
			name:         "short thought",
			thought:      "ok",
			task:         "test task",
			expectHigher: 0.0,
			expectLower:  0.5,
		},
		{
			name:         "medium thought with reasoning",
			thought:      "Therefore, we should consider the performance implications of this approach",
			task:         "optimize performance",
			expectHigher: 0.5,
			expectLower:  1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := agent.evaluateThoughtFast(tt.thought, &core.AgentInput{Task: tt.task})
			assert.GreaterOrEqual(t, score, tt.expectHigher, "score should be >= %f", tt.expectHigher)
			assert.LessOrEqual(t, score, tt.expectLower, "score should be <= %f", tt.expectLower)
		})
	}
}

// TestGoTAgent_MinimalMode 测试极简模式
func TestGoTAgent_MinimalMode(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// 第一次调用：生成多个思考路径
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(msgs []llm.Message) bool {
		return strings.Contains(msgs[0].Content, "多个不同角度")
	})).Return(
		&llm.CompletionResponse{
			Content: `思考路径 1:
从用户体验角度分析，API 应该简洁易用。

---

思考路径 2:
从性能角度分析，API 应该高效响应。

---

思考路径 3:
从安全角度分析，API 应该防止攻击。`,
		}, nil,
	).Once()

	// 第二次调用：合成最终答案
	mockLLM.On("Chat", mock.Anything, mock.MatchedBy(func(msgs []llm.Message) bool {
		return strings.Contains(msgs[0].Content, "综合以上所有分析")
	})).Return(
		&llm.CompletionResponse{
			Content: "API 设计需要兼顾易用性、性能和安全性三个方面。",
		}, nil,
	).Once()

	agent := NewGoTAgent(GoTConfig{
		Name:        "test-minimal",
		Description: "Test Minimal Mode",
		LLM:         mockLLM,
		MinimalMode: true, // 启用极简模式
	})

	input := &core.AgentInput{
		Task: "简述 API 设计的要点",
	}

	output, err := agent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, interfaces.StatusSuccess, output.Status)
	assert.Contains(t, output.Message, "minimal mode")

	// 验证元数据
	assert.Equal(t, "minimal", output.Metadata["mode"])
	assert.Equal(t, 2, output.Metadata["llm_calls"])
	assert.GreaterOrEqual(t, output.Metadata["thought_count"], 1)

	// 验证只有 2 个步骤
	assert.Equal(t, 2, len(output.Steps))
	assert.Equal(t, "Generate Thoughts", output.Steps[0].Action)
	assert.Equal(t, "Synthesize Answer", output.Steps[1].Action)

	mockLLM.AssertExpectations(t)
}

// TestGoTAgent_ParseThoughts 测试思考解析
func TestGoTAgent_ParseThoughts(t *testing.T) {
	agent := NewGoTAgent(GoTConfig{
		Name: "test-parse",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		name     string
		content  string
		expected int // 期望解析出的思考数量
	}{
		{
			name: "正常分隔",
			content: `思考1：这是第一个思考。

---

思考2：这是第二个思考。

---

思考3：这是第三个思考。`,
			expected: 3,
		},
		{
			name:     "无分隔符",
			content:  "这是一个完整的思考，没有分隔符。",
			expected: 1,
		},
		{
			name:     "空内容",
			content:  "",
			expected: 0,
		},
		{
			name: "混合短内容",
			content: `长思考1：这是一个足够长的思考内容，超过20字符。

---

短

---

长思考2：这也是一个足够长的思考内容，超过20字符。`,
			expected: 2, // 短内容被过滤
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thoughts := agent.parseThoughts(tt.content)
			assert.Equal(t, tt.expected, len(thoughts))
		})
	}
}

// TestGoTAgent_MinimalModeWithError 测试极简模式错误处理
func TestGoTAgent_MinimalModeWithError(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// 模拟 LLM 调用失败
	mockLLM.On("Chat", mock.Anything, mock.Anything).Return(
		nil, assert.AnError,
	).Once()

	agent := NewGoTAgent(GoTConfig{
		Name:        "test-minimal-error",
		LLM:         mockLLM,
		MinimalMode: true,
	})

	input := &core.AgentInput{
		Task: "测试任务",
	}

	output, err := agent.Invoke(ctx, input)

	assert.Error(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, interfaces.StatusFailed, output.Status)
}
