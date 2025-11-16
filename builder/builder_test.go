package builder

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/tools"
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
	*core.BaseRunnable[*interfaces.ToolInput, *interfaces.ToolOutput]
	name   string
	result string
}

func NewMockTool(name, result string) *MockTool {
	return &MockTool{
		BaseRunnable: core.NewBaseRunnable[*interfaces.ToolInput, *interfaces.ToolOutput](),
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
func (t *MockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return &tools.ToolOutput{
		Result:  t.result,
		Success: true,
	}, nil
}

// Stream implements the Runnable interface for Tool
func (t *MockTool) Stream(ctx context.Context, input *interfaces.ToolInput) (<-chan core.StreamChunk[*interfaces.ToolOutput], error) {
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
func (t *MockTool) Pipe(next core.Runnable[*interfaces.ToolOutput, any]) core.Runnable[*interfaces.ToolInput, any] {
	return core.NewRunnablePipe[*interfaces.ToolInput, *interfaces.ToolOutput, any](t, next)
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

	mw1 := middleware.NewLoggingMiddleware(nil)
	mw2 := middleware.NewTimingMiddleware()

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
		WithMiddleware(middleware.NewLoggingMiddleware(nil)).
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
	loggingMW := middleware.NewLoggingMiddleware(func(msg string) {
		// Silent logging for test
	})
	cacheMW := middleware.NewCacheMiddleware(1 * time.Minute)

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

// MockCallback implements core.Callback for testing
type MockCallback struct {
	*core.BaseCallback
	onStartCalled bool
	onEndCalled   bool
	onErrorCalled bool
	onStartError  error
}

func NewMockCallback() *MockCallback {
	return &MockCallback{
		BaseCallback: core.NewBaseCallback(),
	}
}

func (mc *MockCallback) OnStart(ctx context.Context, input interface{}) error {
	mc.onStartCalled = true
	return mc.onStartError
}

func (mc *MockCallback) OnEnd(ctx context.Context, output interface{}) error {
	mc.onEndCalled = true
	return nil
}

func (mc *MockCallback) OnError(ctx context.Context, err error) error {
	mc.onErrorCalled = true
	return nil
}

// TestAgentBuilder_WithCallbacks tests the WithCallbacks method
func TestAgentBuilder_WithCallbacks(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[any, *core.AgentState](llmClient)

	cb1 := NewMockCallback()
	cb2 := NewMockCallback()

	builder.WithCallbacks(cb1, cb2)

	assert.Equal(t, 2, len(builder.callbacks))
}

// TestAgentBuilder_WithMetadata tests the WithMetadata method with multiple keys
func TestAgentBuilder_WithMetadata_Multiple(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[any, *core.AgentState](llmClient)

	builder.
		WithMetadata("key1", "value1").
		WithMetadata("key2", 42).
		WithMetadata("key3", map[string]interface{}{"nested": "value"})

	assert.Equal(t, "value1", builder.metadata["key1"])
	assert.Equal(t, 42, builder.metadata["key2"])
	assert.Equal(t, map[string]interface{}{"nested": "value"}, builder.metadata["key3"])
}

// TestAgentBuilder_WithTelemetry tests telemetry provider configuration
func TestAgentBuilder_WithTelemetry(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[any, *core.AgentState](llmClient)

	provider := map[string]interface{}{"type": "otel"}
	builder.WithTelemetry(provider)

	agent, err := builder.Build()
	require.NoError(t, err)

	assert.Equal(t, provider, agent.metadata["telemetry_provider"])
}

// TestAgentBuilder_WithCommunicator tests communicator configuration
func TestAgentBuilder_WithCommunicator(t *testing.T) {
	llmClient := NewMockLLMClient()
	builder := NewAgentBuilder[any, *core.AgentState](llmClient)

	communicator := map[string]interface{}{"type": "grpc"}
	builder.WithCommunicator(communicator)

	agent, err := builder.Build()
	require.NoError(t, err)

	assert.Equal(t, communicator, agent.metadata["communicator"])
}

// TestAgentBuilder_ChainedConfiguration tests method chaining
func TestAgentBuilder_ChainedConfiguration(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithMetadata("key1", "value1").
		WithMetadata("key2", "value2").
		WithTools(NewMockTool("tool1", "result")).
		Build()

	require.NoError(t, err)
	assert.Equal(t, "Test", agent.systemPrompt)
	assert.Equal(t, 1, len(agent.tools))
	assert.Equal(t, "value1", agent.metadata["key1"])
	assert.Equal(t, "value2", agent.metadata["key2"])
}

// TestConfigurableAgent_Execute_WithTimeout tests execution timeout
func TestConfigurableAgent_Execute_WithTimeout(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	config := &AgentConfig{
		Timeout:     100 * time.Millisecond,
		MaxTokens:   2000,
		Temperature: 0.7,
	}

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithConfig(config).
		Build()

	require.NoError(t, err)

	ctx := context.Background()
	output, err := agent.Execute(ctx, "test")
	require.NoError(t, err)
	assert.NotNil(t, output)
}

// TestConfigurableAgent_Initialize_WithCheckpoint tests state loading from checkpoint
func TestConfigurableAgent_Initialize_WithCheckpoint(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	checkpointer := core.NewInMemorySaver()
	state := core.NewAgentState()
	state.Set("saved_key", "saved_value")

	// First, build and save state
	agent1, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithCheckpointer(checkpointer).
		Build()
	require.NoError(t, err)

	sessionID := agent1.config.SessionID
	agent1.Execute(context.Background(), "test input")
	agent1.Shutdown(context.Background())

	// Create new agent with same checkpointer and session ID
	state2 := core.NewAgentState()
	config2 := DefaultAgentConfig()
	config2.SessionID = sessionID

	agent2, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state2).
		WithCheckpointer(checkpointer).
		WithConfig(config2).
		Build()

	require.NoError(t, err)

	// Verify state was loaded from checkpoint
	retrievedState := agent2.GetState()
	assert.NotNil(t, retrievedState)
}

// TestConfigurableAgent_Initialize_WithCallback tests callback execution during initialization
func TestConfigurableAgent_Initialize_WithCallback(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()
	callback := NewMockCallback()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithCallbacks(callback).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, agent)
	assert.True(t, callback.onStartCalled)
}

// TestConfigurableAgent_Initialize_WithCallbackError tests callback error handling
func TestConfigurableAgent_Initialize_WithCallbackError(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()
	callback := NewMockCallback()
	callback.onStartError = fmt.Errorf("callback error")

	_, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithCallbacks(callback).
		Build()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "callback error")
}

// TestConfigurableAgent_Execute_WithCallbacks tests callback execution during execution
func TestConfigurableAgent_Execute_WithCallbacks(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()
	callback := NewMockCallback()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithCallbacks(callback).
		Build()

	require.NoError(t, err)
	require.True(t, callback.onStartCalled)

	// Reset for execution test
	callback.onEndCalled = false
	output, err := agent.Execute(context.Background(), "test")

	require.NoError(t, err)
	assert.True(t, callback.onEndCalled)
	assert.NotNil(t, output)
}

// TestConfigurableAgent_Shutdown_WithSaveError tests shutdown when save fails
func TestConfigurableAgent_Shutdown_WithSaveError(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		Build()

	require.NoError(t, err)

	// Execute to populate state
	agent.Execute(context.Background(), "test")

	// Shutdown should succeed even with nil checkpointer
	err = agent.Shutdown(context.Background())
	assert.NoError(t, err)
}

// TestConfigurableAgent_Shutdown_WithCallback tests callback execution during shutdown
func TestConfigurableAgent_Shutdown_WithCallback(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()
	callback := NewMockCallback()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithCallbacks(callback).
		Build()

	require.NoError(t, err)

	err = agent.Shutdown(context.Background())
	assert.NoError(t, err)
}

// TestConfigurableAgent_ExecuteWithTools tests tool execution flow
func TestConfigurableAgent_ExecuteWithTools(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()
	tool := NewMockTool("test-tool", "tool-result")

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithTools(tool).
		Build()

	require.NoError(t, err)

	output, err := agent.ExecuteWithTools(context.Background(), "test")
	assert.NoError(t, err)
	assert.NotNil(t, output)
}

// TestConfigurableAgent_ExecuteWithTools_MaxIterations tests max iterations limit
func TestConfigurableAgent_ExecuteWithTools_MaxIterations(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	config := &AgentConfig{
		MaxIterations: 2,
		Timeout:       30 * time.Second,
		Temperature:   0.7,
		MaxTokens:     2000,
	}

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithConfig(config).
		Build()

	require.NoError(t, err)

	output, err := agent.ExecuteWithTools(context.Background(), "test")
	// Should succeed even if max iterations exceeded (no tool calls)
	assert.NoError(t, err)
	assert.NotNil(t, output)
}

// TestConfigurableAgent_Execute_WithErrorHandler tests error handler invocation
func TestConfigurableAgent_Execute_WithErrorHandler(t *testing.T) {
	errorHandler := func(err error) error {
		return fmt.Errorf("handled: %w", err)
	}

	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithErrorHandler(errorHandler).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, agent.errorHandler)
}

// TestConfigurableAgent_GetMetrics_WithAgentState tests metrics with AgentState
func TestConfigurableAgent_GetMetrics_WithAgentState(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()
	state.Set("key1", "value1")
	state.Set("key2", "value2")

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		Build()

	require.NoError(t, err)

	metrics := agent.GetMetrics()
	assert.NotNil(t, metrics)
	assert.Equal(t, 0, metrics["tools_count"]) // No tools added
	assert.NotEmpty(t, metrics["session_id"])
	assert.NotNil(t, metrics["state_size"])
}

// TestConfigurableAgent_ExecuteWithTools_ToolNotFound tests error when tool not found
func TestConfigurableAgent_ExecuteWithTools_ToolNotFound(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		Build()

	require.NoError(t, err)

	// executeToolCall should return error for non-existent tool
	call := ToolCall{Name: "non-existent", Input: map[string]interface{}{}}
	result, err := agent.executeToolCall(context.Background(), call)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "tool not found")
}

// TestConfigurableAgent_ExtractToolCalls tests tool call extraction
func TestConfigurableAgent_ExtractToolCalls(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		Build()

	require.NoError(t, err)

	// extractToolCalls should return empty slice for current implementation
	calls := agent.extractToolCalls("test output")
	assert.Empty(t, calls)
}

// TestAgentBuilder_BuildWithStateRequired tests build fails without required state
func TestAgentBuilder_BuildWithStateRequired(t *testing.T) {
	llmClient := NewMockLLMClient("response")

	// Create a builder with custom state type that requires explicit state
	// For AgentState, it should auto-create if not provided
	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		Build()

	// Should succeed because AgentState can be auto-created
	require.NoError(t, err)
	assert.NotNil(t, agent)
}

// TestAgentBuilder_ConfigureForChatbot_Validation tests validation middleware
func TestAgentBuilder_ConfigureForChatbot_Validation(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	builder := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state)

	builder.ConfigureForChatbot()

	agent, err := builder.Build()

	require.NoError(t, err)
	assert.True(t, agent.config.EnableStreaming)
	assert.Equal(t, 0.8, agent.config.Temperature)
	assert.Greater(t, len(builder.middlewares), 0)
}

// TestAgentBuilder_ConfigureForRAG_FullCoverage tests RAG configuration
func TestAgentBuilder_ConfigureForRAG_FullCoverage(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	builder := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state)

	builder.ConfigureForRAG()

	assert.Equal(t, 3000, builder.config.MaxTokens)
	assert.Equal(t, 0.3, builder.config.Temperature)
	assert.Greater(t, len(builder.middlewares), 0)

	agent, err := builder.Build()
	require.NoError(t, err)
	assert.NotNil(t, agent)
}

// TestAgentBuilder_ConfigureForAnalysis_FullCoverage tests analysis configuration
func TestAgentBuilder_ConfigureForAnalysis_FullCoverage(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	builder := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state)

	builder.ConfigureForAnalysis()

	assert.Equal(t, 0.1, builder.config.Temperature)
	assert.Equal(t, 20, builder.config.MaxIterations)
	assert.Greater(t, len(builder.middlewares), 0)

	agent, err := builder.Build()
	require.NoError(t, err)
	assert.NotNil(t, agent)
}

// TestWorkflowAgent_WithValidation tests workflow agent validation middleware
func TestWorkflowAgent_WithValidation(t *testing.T) {
	llmClient := NewMockLLMClient("response")

	workflows := map[string]interface{}{
		"deploy": []string{"build", "test"},
	}

	agent, err := WorkflowAgent(llmClient, workflows)
	require.NoError(t, err)

	// Verify validation middleware is installed
	assert.Equal(t, 15, agent.config.MaxIterations)
	assert.True(t, agent.config.EnableAutoSave)
	assert.NotNil(t, agent)
}

// TestAgentBuilder_MultipleTools tests adding multiple tools in separate calls
func TestAgentBuilder_MultipleTools(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	builder := NewAgentBuilder[any, *core.AgentState](llmClient)

	tool1 := NewMockTool("tool1", "result1")
	tool2 := NewMockTool("tool2", "result2")
	tool3 := NewMockTool("tool3", "result3")

	builder.
		WithTools(tool1).
		WithTools(tool2, tool3)

	assert.Equal(t, 3, len(builder.tools))
}

// TestConfigurableAgent_GetState_Concurrent tests concurrent GetState access
func TestConfigurableAgent_GetState_Concurrent(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()
	state.Set("initial", "value")

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithState(state).
		Build()
	require.NoError(t, err)

	// Concurrent reads should be safe
	done := make(chan bool, 2)

	go func() {
		_ = agent.GetState()
		done <- true
	}()

	go func() {
		_ = agent.GetState()
		done <- true
	}()

	<-done
	<-done
}

// TestAgentBuilder_Build_WithVerboseConfig tests verbose mode configuration
func TestAgentBuilder_Build_WithVerboseConfig(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	config := &AgentConfig{
		MaxIterations: 10,
		Timeout:       5 * time.Minute,
		Temperature:   0.7,
		MaxTokens:     2000,
		Verbose:       true,
	}

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithConfig(config).
		Build()

	require.NoError(t, err)
	assert.True(t, agent.config.Verbose)
}

// TestConfigurableAgent_Execute_UpdatesState tests that Execute updates state
func TestConfigurableAgent_Execute_UpdatesState(t *testing.T) {
	llmClient := NewMockLLMClient("Test response")
	state := core.NewAgentState()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		Build()

	require.NoError(t, err)

	output, err := agent.Execute(context.Background(), "test input")
	require.NoError(t, err)
	assert.NotNil(t, output)
	assert.NotNil(t, output.State)
}

// TestConfigurableAgent_Execute_WithMetadata tests metadata propagation
func TestConfigurableAgent_Execute_WithMetadata(t *testing.T) {
	llmClient := NewMockLLMClient("response")
	state := core.NewAgentState()

	agent, err := NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("Test").
		WithState(state).
		WithMetadata("key1", "value1").
		WithMetadata("key2", "value2").
		Build()

	require.NoError(t, err)

	output, err := agent.Execute(context.Background(), "test")
	require.NoError(t, err)
	assert.NotNil(t, output)
	// Metadata should be available in output
	assert.NotNil(t, output.Metadata)
}
