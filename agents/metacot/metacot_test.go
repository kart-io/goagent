package metacot

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
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

func (m *MockLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
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

func TestNewMetaCoTAgent(t *testing.T) {
	tests := []struct {
		name   string
		config MetaCoTConfig
		check  func(t *testing.T, agent *MetaCoTAgent)
	}{
		{
			name: "default configuration",
			config: MetaCoTConfig{
				Name:        "test-metacot",
				Description: "Test MetaCoT Agent",
				LLM:         &MockLLMClient{},
			},
			check: func(t *testing.T, agent *MetaCoTAgent) {
				assert.Equal(t, "test-metacot", agent.Name())
				assert.Equal(t, "Test MetaCoT Agent", agent.Description())
				assert.Equal(t, 5, agent.config.MaxQuestions)
				assert.Equal(t, 3, agent.config.MaxDepth)
				assert.Equal(t, "focused", agent.config.QuestionStrategy)
				assert.Equal(t, 0.7, agent.config.ConfidenceThreshold)
			},
		},
		{
			name: "custom configuration",
			config: MetaCoTConfig{
				Name:                "custom-metacot",
				Description:         "Custom MetaCoT Agent",
				LLM:                 &MockLLMClient{},
				MaxQuestions:        10,
				MaxDepth:            5,
				AutoDecompose:       false,
				RequireEvidence:     false,
				SelfCritique:        false,
				QuestionStrategy:    "broad",
				VerifyAnswers:       false,
				ConfidenceThreshold: 0.8,
			},
			check: func(t *testing.T, agent *MetaCoTAgent) {
				assert.Equal(t, 10, agent.config.MaxQuestions)
				assert.Equal(t, 5, agent.config.MaxDepth)
				assert.False(t, agent.config.AutoDecompose)
				assert.False(t, agent.config.RequireEvidence)
				assert.False(t, agent.config.SelfCritique)
				assert.Equal(t, "broad", agent.config.QuestionStrategy)
				assert.False(t, agent.config.VerifyAnswers)
				assert.Equal(t, 0.8, agent.config.ConfidenceThreshold)
			},
		},
		{
			name: "with tools",
			config: MetaCoTConfig{
				Name:        "metacot-with-tools",
				Description: "MetaCoT with Tools",
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
			check: func(t *testing.T, agent *MetaCoTAgent) {
				assert.Len(t, agent.tools, 1)
				assert.Contains(t, agent.Capabilities(), "tool_calling")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewMetaCoTAgent(tt.config)
			assert.NotNil(t, agent)
			tt.check(t, agent)
		})
	}
}

func TestMetaCoTAgent_ShouldDecompose(t *testing.T) {
	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name:          "test-decompose",
		LLM:           &MockLLMClient{},
		AutoDecompose: true,
	})

	tests := []struct {
		question string
		expected bool
	}{
		{
			"Compare and contrast the benefits of renewable energy with traditional fossil fuels",
			true,
		},
		{
			"Analyze multiple factors affecting climate change and evaluate various solutions",
			true,
		},
		{
			"What is the capital of France?",
			false,
		},
		{
			"This is a very long question with more than twenty words that should trigger decomposition based on length alone even without complexity indicators",
			true,
		},
	}

	for _, tt := range tests {
		result := agent.shouldDecompose(tt.question)
		assert.Equal(t, tt.expected, result, "Question: %s", tt.question)
	}
}

func TestMetaCoTAgent_NeedsExternalInfo(t *testing.T) {
	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name: "test-external",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		question *Question
		expected bool
	}{
		{&Question{Text: "What is machine learning?"}, true},
		{&Question{Text: "Who is the president of the United States?"}, true},
		{&Question{Text: "When did World War II end?"}, true},
		{&Question{Text: "Where is the Eiffel Tower located?"}, true},
		{&Question{Text: "How many planets are in our solar system?"}, true},
		{&Question{Text: "Which programming language is best?"}, true},
		{&Question{Text: "Define quantum computing"}, true},
		{&Question{Text: "Explain the theory of relativity"}, true},
		{&Question{Text: "Think about this problem"}, false},
		{&Question{Text: "Consider the implications"}, false},
	}

	for _, tt := range tests {
		result := agent.needsExternalInfo(tt.question)
		assert.Equal(t, tt.expected, result, "Question: %s", tt.question.Text)
	}
}

func TestMetaCoTAgent_ParseFollowupQuestions(t *testing.T) {
	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name: "test-parse",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		name     string
		response string
		parentID string
		expected int
	}{
		{
			name: "questions with Q prefix",
			response: `Q: What are the main components?
Q: How do they interact?
Q: What are the dependencies?`,
			parentID: "main",
			expected: 3,
		},
		{
			name: "questions with Question prefix",
			response: `Question: First follow-up question
Question: Second follow-up question`,
			parentID: "test",
			expected: 2,
		},
		{
			name:     "direct answer signal",
			response: "DIRECT_ANSWER: The answer is 42",
			parentID: "direct",
			expected: 0,
		},
		{
			name:     "mixed format",
			response: "Some text\nQ: Valid question\nInvalid line\nQuestion: Another valid",
			parentID: "mixed",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			questions := agent.parseFollowupQuestions(tt.response, tt.parentID)
			assert.Len(t, questions, tt.expected)
			for _, q := range questions {
				assert.NotEmpty(t, q.ID)
				assert.NotEmpty(t, q.Text)
				assert.Equal(t, "followup", q.Type)
				assert.Equal(t, tt.parentID, q.ParentID)
				assert.Equal(t, "pending", q.Status)
			}
		})
	}
}

func TestMetaCoTAgent_EstimateConfidence(t *testing.T) {
	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name: "test-confidence",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		answer  string
		minConf float64
		maxConf float64
	}{
		{
			"This is a detailed answer with more than one hundred characters that provides comprehensive information about the topic at hand.",
			0.7, 0.8,
		},
		{
			"Maybe this is correct, but I'm not sure.",
			0.3, 0.5,
		},
		{
			"This is definitely and certainly the correct answer, obviously.",
			0.8, 1.0,
		},
		{
			"Short answer",
			0.4, 0.6,
		},
		{
			"This might possibly be the answer, but it could be wrong.",
			0.2, 0.4,
		},
	}

	for _, tt := range tests {
		confidence := agent.estimateConfidence(tt.answer)
		assert.GreaterOrEqual(t, confidence, tt.minConf)
		assert.LessOrEqual(t, confidence, tt.maxConf)
	}
}

func TestMetaCoTAgent_NeedsRefinement(t *testing.T) {
	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name: "test-refinement",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		critique string
		expected bool
	}{
		{"The answer is incorrect and needs revision", true},
		{"This is wrong", true},
		{"Missing important information", true},
		{"The response is incomplete", true},
		{"You should include more details", true},
		{"This needs improvement", true},
		{"The answer must be corrected", true},
		{"We need to improve this", true},
		{"The answer looks good and complete", false},
		{"Well done, accurate response", false},
		{"Excellent analysis", false},
	}

	for _, tt := range tests {
		result := agent.needsRefinement(tt.critique)
		assert.Equal(t, tt.expected, result, "Critique: %s", tt.critique)
	}
}

func TestMetaCoTAgent_BuildFollowupPrompt(t *testing.T) {
	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name:             "test-prompt",
		LLM:              &MockLLMClient{},
		QuestionStrategy: "focused",
	})

	question := &Question{
		Text: "How does machine learning work?",
	}

	prompt := agent.buildFollowupPrompt(question)

	assert.Contains(t, prompt, question.Text)
	assert.Contains(t, prompt, "focused questions")
	assert.Contains(t, prompt, "DIRECT_ANSWER")
	assert.Contains(t, prompt, "Q: ")

	// Test different strategies
	strategies := []struct {
		strategy string
		expected string
	}{
		{"broad", "broad questions"},
		{"critical", "critical questions"},
		{"unknown", "helpful follow-up"},
	}

	for _, s := range strategies {
		agent.config.QuestionStrategy = s.strategy
		prompt := agent.buildFollowupPrompt(question)
		assert.Contains(t, prompt, s.expected)
	}
}

func TestMetaCoTAgent_FormatQuestions(t *testing.T) {
	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name: "test-format",
		LLM:  &MockLLMClient{},
	})

	questions := []*Question{
		{Text: "First question"},
		{Text: "Second question"},
		{Text: "Third question"},
	}

	formatted := agent.formatQuestions(questions)

	assert.Contains(t, formatted, "1. First question")
	assert.Contains(t, formatted, "2. Second question")
	assert.Contains(t, formatted, "3. Third question")
}

func TestMetaCoTAgent_CountQuestions(t *testing.T) {
	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name: "test-count",
		LLM:  &MockLLMClient{},
	})

	root := &Question{
		Text: "Main question",
		SubQuestions: []*Question{
			{
				Text: "Sub 1",
				SubQuestions: []*Question{
					{Text: "Sub 1.1"},
					{Text: "Sub 1.2"},
				},
			},
			{Text: "Sub 2"},
		},
	}

	count := agent.countQuestions(root)
	assert.Equal(t, 5, count) // root + 2 subs + 2 sub-subs
}

func TestMetaCoTAgent_GetMaxDepth(t *testing.T) {
	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name: "test-depth",
		LLM:  &MockLLMClient{},
	})

	tests := []struct {
		name     string
		root     *Question
		expected int
	}{
		{
			name:     "no sub-questions",
			root:     &Question{Text: "Single question"},
			expected: 0,
		},
		{
			name: "one level deep",
			root: &Question{
				Text: "Main",
				SubQuestions: []*Question{
					{Text: "Sub 1"},
					{Text: "Sub 2"},
				},
			},
			expected: 1,
		},
		{
			name: "two levels deep",
			root: &Question{
				Text: "Main",
				SubQuestions: []*Question{
					{
						Text: "Sub 1",
						SubQuestions: []*Question{
							{Text: "Sub 1.1"},
						},
					},
				},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			depth := agent.getMaxDepth(tt.root)
			assert.Equal(t, tt.expected, depth)
		})
	}
}

func TestMetaCoTAgent_Invoke(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// Mock for decomposition check
	mockLLM.On("Chat", ctx, mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "Q: What are the components?\nQ: How do they work?",
		}, nil,
	).Once()

	// Mock for follow-up questions
	mockLLM.On("Chat", ctx, mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "DIRECT_ANSWER",
		}, nil,
	).Times(2)

	// Mock for direct answers
	mockLLM.On("Chat", ctx, mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "This is the answer",
		}, nil,
	)

	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name:          "test-invoke",
		Description:   "Test Invoke",
		LLM:           mockLLM,
		AutoDecompose: true,
		SelfCritique:  false,
		MaxDepth:      2,
	})

	input := &core.AgentInput{
		Task:    "Explain and analyze machine learning algorithms",
		Context: make(map[string]interface{}),
	}

	output, err := agent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Status)
	assert.NotEmpty(t, output.Result)
	assert.NotEmpty(t, output.ReasoningSteps)
}

func TestMetaCoTAgent_SelfCritique(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	// Mock for initial answer
	mockLLM.On("Chat", ctx, mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "Initial answer",
		}, nil,
	).Times(3)

	// Mock for critique
	mockLLM.On("Chat", ctx, mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "The answer needs improvement in clarity",
		}, nil,
	).Once()

	// Mock for refined answer
	mockLLM.On("Chat", ctx, mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "Improved answer with better clarity",
		}, nil,
	).Once()

	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name:         "test-critique",
		LLM:          mockLLM,
		SelfCritique: true,
		MaxDepth:     1,
	})

	input := &core.AgentInput{
		Task: "Simple task",
	}

	output, err := agent.Invoke(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, output)
	assert.Equal(t, "success", output.Status)
	assert.Contains(t, output.Result, "Improved answer")
}

func TestMetaCoTAgent_Stream(t *testing.T) {
	ctx := context.Background()
	mockLLM := new(MockLLMClient)

	mockLLM.On("Chat", ctx, mock.Anything, mock.Anything).Return(
		&llm.CompletionResponse{
			Content: "Streaming answer",
		}, nil,
	)

	agent := NewMetaCoTAgent(MetaCoTConfig{
		Name:        "test-stream",
		Description: "Test Stream",
		LLM:         mockLLM,
		MaxDepth:    1,
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

func TestMetaCoTAgent_WithCallbacks(t *testing.T) {

	callback := &testCallback{
		onStart: func(ctx context.Context, input interface{}) error {
			return nil
		},
	}

	agent := NewMetaCoTAgent(MetaCoTConfig{
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

func TestMetaCoTAgent_SearchForAnswer(t *testing.T) {
	ctx := context.Background()

	t.Run("with search tool", func(t *testing.T) {
		mockTool := new(MockTool)
		mockTool.On("Name").Return("search")
		mockTool.On("Invoke", ctx, mock.Anything).Return(
			&interfaces.ToolOutput{
				Success: true,
				Result:  "Search result for the question",
			}, nil,
		)

		agent := NewMetaCoTAgent(MetaCoTConfig{
			Name:  "test-search",
			LLM:   &MockLLMClient{},
			Tools: []interfaces.Tool{mockTool},
		})

		question := &Question{
			ID:     "q1",
			Text:   "What is AI?",
			Status: "pending",
		}
		output := &core.AgentOutput{
			ToolCalls: make([]core.ToolCall, 0),
		}

		agent.searchForAnswer(ctx, question, output)

		assert.Equal(t, "answered", question.Status)
		assert.Equal(t, "Search result for the question", question.Answer)
		assert.Len(t, question.Evidence, 1)
		assert.Len(t, output.ToolCalls, 1)
	})

	t.Run("without search tool", func(t *testing.T) {
		mockLLM := new(MockLLMClient)
		mockLLM.On("Chat", ctx, mock.Anything, mock.Anything).Return(
			&llm.CompletionResponse{
				Content: "Direct answer without tool",
			}, nil,
		)

		agent := NewMetaCoTAgent(MetaCoTConfig{
			Name: "test-no-search",
			LLM:  mockLLM,
		})

		question := &Question{
			ID:     "q2",
			Text:   "What is AI?",
			Status: "pending",
		}
		output := &core.AgentOutput{}

		agent.searchForAnswer(ctx, question, output)

		assert.Equal(t, "answered", question.Status)
		assert.Equal(t, "Direct answer without tool", question.Answer)
	})
}
