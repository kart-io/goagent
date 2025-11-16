package builder

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/k8s-agent/pkg/agent/core"
	"github.com/kart-io/k8s-agent/pkg/agent/llm"
	"github.com/kart-io/k8s-agent/pkg/agent/store/memory"
	"github.com/kart-io/k8s-agent/pkg/agent/tools"
)

// MockLLMClient implements llm.Client for testing
type MockLLMClient struct {
	responses []string
	index     int
}

func NewMockLLMClient(responses ...string) *MockLLMClient {
	if len(responses) == 0 {
		responses = []string{"Default response"}
	}
	return &MockLLMClient{
		responses: responses,
	}
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	response := m.responses[m.index%len(m.responses)]
	m.index++
	return &llm.CompletionResponse{
		Content:    response,
		Model:      "mock-model",
		TokensUsed: 30,
		Provider:   "mock",
	}, nil
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return m.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}

func (m *MockLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// MockTool implements tools.Tool for testing
type MockTool struct {
	*core.BaseRunnable[*tools.ToolInput, *tools.ToolOutput]
	name   string
	result string
}

func NewMockTool(name, result string) *MockTool {
	return &MockTool{
		BaseRunnable: core.NewBaseRunnable[*tools.ToolInput, *tools.ToolOutput](),
		name:         name,
		result:       result,
	}
}

func (t *MockTool) Name() string {
	return t.name
}

func (t *MockTool) Description() string {
	return "Mock tool for testing"
}

func (t *MockTool) ArgsSchema() string {
	return `{"type": "object", "properties": {}}`
}

// Invoke implements the Runnable interface for Tool
func (t *MockTool) Invoke(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
	return &tools.ToolOutput{
		Result:  t.result,
		Success: true,
	}, nil
}

// Stream implements the Runnable interface for Tool
func (t *MockTool) Stream(ctx context.Context, input *tools.ToolInput) (<-chan core.StreamChunk[*tools.ToolOutput], error) {
	outChan := make(chan core.StreamChunk[*tools.ToolOutput], 1)
	go func() {
		defer close(outChan)
		output, err := t.Invoke(ctx, input)
		outChan <- core.StreamChunk[*tools.ToolOutput]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()
	return outChan, nil
}

// Batch implements the Runnable interface for Tool
func (t *MockTool) Batch(ctx context.Context, inputs []*tools.ToolInput) ([]*tools.ToolOutput, error) {
	outputs := make([]*tools.ToolOutput, len(inputs))
	for i, input := range inputs {
		output, err := t.Invoke(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

// Pipe implements the Runnable interface for Tool
func (t *MockTool) Pipe(next core.Runnable[*tools.ToolOutput, any]) core.Runnable[*tools.ToolInput, any] {
	return core.NewRunnablePipe[*tools.ToolInput, *tools.ToolOutput, any](t, next)
}

// WithCallbacks adds callbacks to the tool
func (t *MockTool) WithCallbacks(callbacks ...core.Callback) core.Runnable[*tools.ToolInput, *tools.ToolOutput] {
	newTool := *t
	newTool.BaseRunnable = t.BaseRunnable.WithCallbacks(callbacks...)
	return &newTool
}

// WithConfig sets the config for the tool
func (t *MockTool) WithConfig(config core.RunnableConfig) core.Runnable[*tools.ToolInput, *tools.ToolOutput] {
	newTool := *t
	newTool.BaseRunnable = t.BaseRunnable.WithConfig(config)
	return &newTool
}

// TestContext for testing
type TestContext struct {
	UserID   string
	UserName string
}

func TestDefaultAgentConfig(t *testing.T) {
	config := DefaultAgentConfig()

	require.NotNil(t, config)
	assert.Equal(t, 10, config.MaxIterations)
	assert.Equal(t, 5*time.Minute, config.Timeout)
	assert.False(t, config.EnableStreaming)
	assert.True(t, config.EnableAutoSave)
	assert.Equal(t, 30*time.Second, config.SaveInterval)
	assert.Equal(t, 2000, config.MaxTokens)
	assert.Equal(t, 0.7, config.Temperature)
	assert.NotEmpty(t, config.SessionID)
	assert.False(t, config.Verbose)
}

func TestNewAgentBuilder(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	require.NotNil(t, builder)
	assert.Equal(t, llmClient, builder.llmClient)
	assert.Empty(t, builder.tools)
	assert.Empty(t, builder.middlewares)
	assert.NotNil(t, builder.config)
}

func TestAgentBuilder_WithTools(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	tool1 := NewMockTool("tool1", "result1")
	tool2 := NewMockTool("tool2", "result2")

	builder.WithTools(tool1, tool2)

	assert.Equal(t, 2, len(builder.tools))
}

func TestAgentBuilder_WithSystemPrompt(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	prompt := "You are a helpful assistant"
	builder.WithSystemPrompt(prompt)

	assert.Equal(t, prompt, builder.systemPrompt)
}

func TestAgentBuilder_WithState(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	state := core.NewAgentState()
	state.Set("key", "value")
	builder.WithState(state)

	assert.NotNil(t, builder.state)
	val, ok := builder.state.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
}

func TestAgentBuilder_WithContext(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	ctx := TestContext{
		UserID:   "user123",
		UserName: "Alice",
	}
	builder.WithContext(ctx)

	assert.Equal(t, "user123", builder.context.UserID)
	assert.Equal(t, "Alice", builder.context.UserName)
}

func TestAgentBuilder_WithStore(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	store := memory.New()
	builder.WithStore(store)

	assert.NotNil(t, builder.store)
}

func TestAgentBuilder_WithCheckpointer(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	checkpointer := core.NewInMemorySaver()
	builder.WithCheckpointer(checkpointer)

	assert.NotNil(t, builder.checkpointer)
}

func TestAgentBuilder_WithMiddleware(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	mw1 := core.NewLoggingMiddleware(nil)
	mw2 := core.NewTimingMiddleware()

	builder.WithMiddleware(mw1, mw2)

	assert.Equal(t, 2, len(builder.middlewares))
}

func TestAgentBuilder_WithConfig(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	config := &AgentConfig{
		MaxIterations: 20,
		Timeout:       10 * time.Minute,
		Temperature:   0.5,
	}
	builder.WithConfig(config)

	assert.Equal(t, 20, builder.config.MaxIterations)
	assert.Equal(t, 10*time.Minute, builder.config.Timeout)
	assert.Equal(t, 0.5, builder.config.Temperature)
}

func TestAgentBuilder_ConfigureForRAG(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	builder.ConfigureForRAG()

	assert.Equal(t, 3000, builder.config.MaxTokens)
	assert.Equal(t, 0.3, builder.config.Temperature)
	assert.Greater(t, len(builder.middlewares), 0)
}

func TestAgentBuilder_ConfigureForChatbot(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	builder.ConfigureForChatbot()

	assert.True(t, builder.config.EnableStreaming)
	assert.Equal(t, 0.8, builder.config.Temperature)
	assert.Greater(t, len(builder.middlewares), 0)
}

func TestAgentBuilder_ConfigureForAnalysis(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[TestContext, *core.AgentState](llmClient)

	builder.ConfigureForAnalysis()

	assert.Equal(t, 0.1, builder.config.Temperature)
	assert.Equal(t, 20, builder.config.MaxIterations)
	assert.Greater(t, len(builder.middlewares), 0)
}

func TestAgentBuilder_Build(t *testing.T) {
	llmClient := NewMockLLMClient("Test response")

	state := core.NewAgentState()
	state.Set("user", "Alice")

	ctx := TestContext{
		UserID:   "user123",
		UserName: "Alice",
	}

	agent, err := NewAgentBuilder[TestContext, *core.AgentState](llmClient).
		WithSystemPrompt("You are a test assistant").
		WithState(state).
		WithContext(ctx).
		WithStore(memory.New()).
		WithCheckpointer(core.NewInMemorySaver()).
		Build()

	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Equal(t, llmClient, agent.llmClient)
	assert.Equal(t, "You are a test assistant", agent.systemPrompt)
	assert.NotNil(t, agent.runtime)
	assert.NotNil(t, agent.chain)
}

func TestAgentBuilder_BuildWithDefaults(t *testing.T) {
	llmClient := NewMockLLMClient("Test response")

	// Build with minimal configuration - should use defaults
	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test prompt").
		Build()

	require.NoError(t, err)
	require.NotNil(t, agent)

	// Check defaults were set
	assert.NotNil(t, agent.runtime.State)
	assert.NotNil(t, agent.runtime.Store)
	assert.NotNil(t, agent.runtime.Checkpointer)
}

func TestAgentBuilder_BuildWithoutLLMClient(t *testing.T) {
	builder := NewAgentBuilder[any, *core.AgentState](nil)

	agent, err := builder.Build()

	assert.Error(t, err)
	assert.Nil(t, agent)
	assert.Contains(t, err.Error(), "LLM client is required")
}

func TestConfigurableAgent_Execute(t *testing.T) {
	llmClient := NewMockLLMClient("Agent response")

	state := core.NewAgentState()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("You are helpful").
		WithState(state).
		Build()
	require.NoError(t, err)

	output, err := agent.Execute(context.Background(), "Test input")
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "Agent response", output.Result)
	assert.NotNil(t, output.State)
	assert.NotZero(t, output.Timestamp)
}

func TestConfigurableAgent_GetState(t *testing.T) {
	llmClient := NewMockLLMClient()

	state := core.NewAgentState()
	state.Set("initial", "value")

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithState(state).
		Build()
	require.NoError(t, err)

	retrievedState := agent.GetState()
	val, ok := retrievedState.Get("initial")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
}

func TestConfigurableAgent_GetMetrics(t *testing.T) {
	llmClient := NewMockLLMClient()
	tool := NewMockTool("test-tool", "result")

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithTools(tool).
		WithMiddleware(core.NewLoggingMiddleware(nil)).
		Build()
	require.NoError(t, err)

	metrics := agent.GetMetrics()

	assert.NotEmpty(t, metrics["session_id"])
	assert.Equal(t, 1, metrics["tools_count"])
	// Note: middleware_count not available as field is unexported
}

func TestConfigurableAgent_Shutdown(t *testing.T) {
	llmClient := NewMockLLMClient()
	checkpointer := core.NewInMemorySaver()
	state := core.NewAgentState()
	state.Set("key", "value")

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithState(state).
		WithCheckpointer(checkpointer).
		Build()
	require.NoError(t, err)

	// Execute to create some state
	agent.Execute(context.Background(), "test")

	// Shutdown should save state
	err = agent.Shutdown(context.Background())
	assert.NoError(t, err)

	// Verify state was saved
	exists, _ := checkpointer.Exists(context.Background(), agent.config.SessionID)
	assert.True(t, exists)
}

func TestQuickAgent(t *testing.T) {
	llmClient := NewMockLLMClient("Quick response")

	agent, err := QuickAgent(llmClient, "Quick prompt")
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Equal(t, "Quick prompt", agent.systemPrompt)
	assert.NotNil(t, agent.GetState())
}

func TestRAGAgent(t *testing.T) {
	llmClient := NewMockLLMClient("RAG response")

	agent, err := RAGAgent(llmClient, nil)
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Contains(t, agent.systemPrompt, "helpful assistant")
	assert.Equal(t, "rag", agent.metadata["type"])
	assert.Equal(t, 3000, agent.config.MaxTokens)
	assert.Equal(t, 0.3, agent.config.Temperature)
}

func TestChatAgent(t *testing.T) {
	llmClient := NewMockLLMClient("Chat response")

	agent, err := ChatAgent(llmClient, "Bob")
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Contains(t, agent.systemPrompt, "Bob")
	assert.Equal(t, "chatbot", agent.metadata["type"])
	assert.True(t, agent.config.EnableStreaming)

	// Check user name was set in state
	userName, ok := agent.GetState().Get("user_name")
	assert.True(t, ok)
	assert.Equal(t, "Bob", userName)
}

func TestAnalysisAgent(t *testing.T) {
	llmClient := NewMockLLMClient("Analysis response")

	dataSource := map[string]interface{}{
		"type": "csv",
		"path": "/data/sales.csv",
	}

	agent, err := AnalysisAgent(llmClient, dataSource)
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Contains(t, agent.systemPrompt, "data analysis")
	assert.Equal(t, "analysis", agent.metadata["type"])
	assert.Equal(t, 0.1, agent.config.Temperature)
	assert.Equal(t, 20, agent.config.MaxIterations)

	// Check data source was set in state
	ds, ok := agent.GetState().Get("data_source")
	assert.True(t, ok)
	assert.Equal(t, dataSource, ds)
}

func TestWorkflowAgent(t *testing.T) {
	llmClient := NewMockLLMClient("Workflow response")

	workflows := map[string]interface{}{
		"deploy":   []string{"build", "test", "deploy"},
		"rollback": []string{"stop", "restore", "restart"},
	}

	agent, err := WorkflowAgent(llmClient, workflows)
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Contains(t, agent.systemPrompt, "workflow orchestrator")
	assert.Equal(t, "workflow", agent.metadata["type"])
	assert.Equal(t, 15, agent.config.MaxIterations)
	assert.True(t, agent.config.EnableAutoSave)

	// Check workflows were set in state
	wf, ok := agent.GetState().Get("workflows")
	assert.True(t, ok)
	assert.Equal(t, workflows, wf)

	status, ok := agent.GetState().Get("workflow_status")
	assert.True(t, ok)
	assert.Equal(t, "initialized", status)
}

func TestMonitoringAgent(t *testing.T) {
	llmClient := NewMockLLMClient("Monitoring response")

	checkInterval := 30 * time.Second

	agent, err := MonitoringAgent(llmClient, checkInterval)
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Contains(t, agent.systemPrompt, "monitoring")
	assert.Equal(t, "monitoring", agent.metadata["type"])
	assert.Equal(t, 100, agent.config.MaxIterations) // Long-running
	assert.Equal(t, 0.3, agent.config.Temperature)

	// Check check_interval was set in state
	interval, ok := agent.GetState().Get("check_interval")
	assert.True(t, ok)
	assert.Equal(t, checkInterval, interval)

	status, ok := agent.GetState().Get("monitoring_status")
	assert.True(t, ok)
	assert.Equal(t, "active", status)
}

func TestResearchAgent(t *testing.T) {
	llmClient := NewMockLLMClient("Research response")

	sources := []string{
		"https://arxiv.org",
		"https://scholar.google.com",
		"https://pubmed.ncbi.nlm.nih.gov",
	}

	agent, err := ResearchAgent(llmClient, sources)
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Contains(t, agent.systemPrompt, "research")
	assert.Equal(t, "research", agent.metadata["type"])
	assert.Equal(t, 4000, agent.config.MaxTokens)
	assert.Equal(t, 0.5, agent.config.Temperature)
	assert.Equal(t, 15, agent.config.MaxIterations)

	// Check sources were set in state
	stateSources, ok := agent.GetState().Get("sources")
	assert.True(t, ok)
	assert.Equal(t, sources, stateSources)

	count, ok := agent.GetState().Get("sources_count")
	assert.True(t, ok)
	assert.Equal(t, 3, count)

	status, ok := agent.GetState().Get("research_status")
	assert.True(t, ok)
	assert.Equal(t, "initialized", status)
}

func TestAnalysisAgent_NilDataSource(t *testing.T) {
	llmClient := NewMockLLMClient()

	agent, err := AnalysisAgent(llmClient, nil)
	require.NoError(t, err)
	require.NotNil(t, agent)

	// Should still work with nil data source
	assert.Equal(t, "analysis", agent.metadata["type"])
}

func TestWorkflowAgent_NilWorkflows(t *testing.T) {
	llmClient := NewMockLLMClient()

	agent, err := WorkflowAgent(llmClient, nil)
	require.NoError(t, err)
	require.NotNil(t, agent)

	// Should still work with nil workflows
	assert.Equal(t, "workflow", agent.metadata["type"])
}

func TestResearchAgent_EmptySources(t *testing.T) {
	llmClient := NewMockLLMClient()

	agent, err := ResearchAgent(llmClient, []string{})
	require.NoError(t, err)
	require.NotNil(t, agent)

	// Should still work with empty sources
	assert.Equal(t, "research", agent.metadata["type"])
	assert.Equal(t, 0, agent.metadata["sources_count"])
}

func TestAgentBuilder_ErrorHandling(t *testing.T) {
	llmClient := NewMockLLMClient()

	errorHandler := func(err error) error {
		return fmt.Errorf("handled: %w", err)
	}

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithErrorHandler(errorHandler).
		Build()
	require.NoError(t, err)

	assert.NotNil(t, agent.errorHandler)
}

func TestAgentBuilder_CompleteFlow(t *testing.T) {
	// This test demonstrates a complete agent building flow
	llmClient := NewMockLLMClient("Complete flow response")

	// Create components
	state := core.NewAgentState()
	state.Set("session_start", time.Now())

	ctx := TestContext{
		UserID:   "user456",
		UserName: "Charlie",
	}

	store := memory.New()
	checkpointer := core.NewInMemorySaver()

	// Create tools
	calcTool := NewMockTool("calculator", "42")
	searchTool := NewMockTool("search", "search results")

	// Create middleware
	loggingMW := core.NewLoggingMiddleware(func(msg string) {
		// Silent logging for test
	})
	cacheMW := core.NewCacheMiddleware(1 * time.Minute)

	// Build agent with everything
	agent, err := NewAgentBuilder[TestContext, *core.AgentState](llmClient).
		WithSystemPrompt("You are a comprehensive assistant").
		WithState(state).
		WithContext(ctx).
		WithStore(store).
		WithCheckpointer(checkpointer).
		WithTools(calcTool, searchTool).
		WithMiddleware(loggingMW, cacheMW).
		WithMetadata("test", "complete").
		WithConfig(&AgentConfig{
			MaxIterations:   5,
			Timeout:         1 * time.Minute,
			EnableStreaming: false,
			EnableAutoSave:  true,
			Temperature:     0.5,
			Verbose:         true,
		}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, agent)

	// Execute
	output, err := agent.Execute(context.Background(), "Test complete flow")
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "Complete flow response", output.Result)
	assert.NotNil(t, output.State)

	// Verify components were integrated
	assert.Equal(t, 2, len(agent.tools))
	// Context is not an interface, so we directly access the field
	assert.Equal(t, "user456", agent.runtime.Context.UserID)

	// Check metadata
	assert.Equal(t, "complete", agent.metadata["test"])
}

func BenchmarkAgentBuilder_Build(b *testing.B) {
	llmClient := NewMockLLMClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agent, _ := NewAgentBuilder[any, *core.AgentState](llmClient).
			WithSystemPrompt("Benchmark prompt").
			Build()
		_ = agent
	}
}

func BenchmarkConfigurableAgent_Execute(b *testing.B) {
	llmClient := NewMockLLMClient("Benchmark response")
	agent, _ := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Benchmark").
		Build()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output, _ := agent.Execute(ctx, "Benchmark input")
		_ = output
	}
}
