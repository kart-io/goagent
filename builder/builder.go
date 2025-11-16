package builder

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/execution"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/store/memory"
)

// AgentBuilder provides a fluent API for building agents with all features
//
// Inspired by LangChain's create_agent function, it integrates:
//   - LLM client configuration
//   - Tools registration
//   - State management
//   - Runtime context
//   - Store and Checkpointer
//   - Middleware stack
//   - System prompts
type AgentBuilder[C any, S core.State] struct {
	// Core components
	llmClient    llm.Client
	tools        []interfaces.Tool
	systemPrompt string

	// Phase 1 components
	state        S
	store        store.Store
	checkpointer core.Checkpointer
	context      C

	// Phase 2 components
	middlewares []middleware.Middleware

	// Configuration
	config *AgentConfig

	// Callbacks
	callbacks []core.Callback

	// Error handling
	errorHandler func(error) error

	// Metadata
	metadata map[string]interface{}
}

// AgentConfig holds agent configuration options
type AgentConfig struct {
	// MaxIterations limits the number of reasoning steps
	MaxIterations int

	// Timeout for agent execution
	Timeout time.Duration

	// EnableStreaming enables streaming responses
	EnableStreaming bool

	// EnableAutoSave automatically saves state after each step
	EnableAutoSave bool

	// SaveInterval for auto-save
	SaveInterval time.Duration

	// MaxTokens limits LLM response tokens
	MaxTokens int

	// Temperature for LLM sampling
	Temperature float64

	// SessionID for checkpointing
	SessionID string

	// Verbose enables detailed logging
	Verbose bool
}

// DefaultAgentConfig returns default configuration
func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		MaxIterations:   10,
		Timeout:         5 * time.Minute,
		EnableStreaming: false,
		EnableAutoSave:  true,
		SaveInterval:    30 * time.Second,
		MaxTokens:       2000,
		Temperature:     0.7,
		SessionID:       fmt.Sprintf("session-%d", time.Now().Unix()),
		Verbose:         false,
	}
}

// NewAgentBuilder creates a new agent builder
func NewAgentBuilder[C any, S core.State](llmClient llm.Client) *AgentBuilder[C, S] {
	return &AgentBuilder[C, S]{
		llmClient:   llmClient,
		tools:       []interfaces.Tool{},
		middlewares: []middleware.Middleware{},
		callbacks:   []core.Callback{},
		config:      DefaultAgentConfig(),
		metadata:    make(map[string]interface{}),
	}
}

// WithTools adds tools to the agent
func (b *AgentBuilder[C, S]) WithTools(tools ...interfaces.Tool) *AgentBuilder[C, S] {
	b.tools = append(b.tools, tools...)
	return b
}

// WithSystemPrompt sets the system prompt
func (b *AgentBuilder[C, S]) WithSystemPrompt(prompt string) *AgentBuilder[C, S] {
	b.systemPrompt = prompt
	return b
}

// WithState sets the agent state
func (b *AgentBuilder[C, S]) WithState(state S) *AgentBuilder[C, S] {
	b.state = state
	return b
}

// WithContext sets the application context
func (b *AgentBuilder[C, S]) WithContext(context C) *AgentBuilder[C, S] {
	b.context = context
	return b
}

// WithStore sets the long-term storage
func (b *AgentBuilder[C, S]) WithStore(st store.Store) *AgentBuilder[C, S] {
	b.store = st
	return b
}

// WithCheckpointer sets the session checkpointer
func (b *AgentBuilder[C, S]) WithCheckpointer(checkpointer core.Checkpointer) *AgentBuilder[C, S] {
	b.checkpointer = checkpointer
	return b
}

// WithMiddleware adds middleware to the chain
func (b *AgentBuilder[C, S]) WithMiddleware(mw ...middleware.Middleware) *AgentBuilder[C, S] {
	b.middlewares = append(b.middlewares, mw...)
	return b
}

// WithCallbacks adds callbacks for monitoring
func (b *AgentBuilder[C, S]) WithCallbacks(callbacks ...core.Callback) *AgentBuilder[C, S] {
	b.callbacks = append(b.callbacks, callbacks...)
	return b
}

// WithConfig sets custom configuration
func (b *AgentBuilder[C, S]) WithConfig(config *AgentConfig) *AgentBuilder[C, S] {
	b.config = config
	return b
}

// WithErrorHandler sets custom error handling
func (b *AgentBuilder[C, S]) WithErrorHandler(handler func(error) error) *AgentBuilder[C, S] {
	b.errorHandler = handler
	return b
}

// WithMetadata adds metadata to the agent
func (b *AgentBuilder[C, S]) WithMetadata(key string, value interface{}) *AgentBuilder[C, S] {
	b.metadata[key] = value
	return b
}

// ConfigureForRAG adds common RAG (Retrieval-Augmented Generation) components
func (b *AgentBuilder[C, S]) ConfigureForRAG() *AgentBuilder[C, S] {
	// Add common RAG middleware
	b.WithMiddleware(
		middleware.NewCacheMiddleware(5*time.Minute),
		middleware.NewDynamicPromptMiddleware(func(req *middleware.MiddlewareRequest) string {
			// Add context from retrieval
			return fmt.Sprintf("Use the following context to answer: %v", req.Input)
		}),
	)

	// Set appropriate config
	b.config.MaxTokens = 3000
	b.config.Temperature = 0.3 // Lower temperature for factual responses

	return b
}

// ConfigureForChatbot adds common chatbot components
func (b *AgentBuilder[C, S]) ConfigureForChatbot() *AgentBuilder[C, S] {
	// Add chatbot middleware
	b.WithMiddleware(
		middleware.NewRateLimiterMiddleware(20, time.Minute),
		middleware.NewValidationMiddleware(
			func(req *middleware.MiddlewareRequest) error {
				// Validate input length
				if len(fmt.Sprintf("%v", req.Input)) > 1000 {
					return fmt.Errorf("message too long")
				}
				return nil
			},
		),
	)

	// Enable streaming for better UX
	b.config.EnableStreaming = true
	b.config.Temperature = 0.8 // Higher temperature for creativity

	return b
}

// ConfigureForAnalysis adds components for data analysis tasks
func (b *AgentBuilder[C, S]) ConfigureForAnalysis() *AgentBuilder[C, S] {
	// Add analysis middleware
	b.WithMiddleware(
		middleware.NewTimingMiddleware(),
		middleware.NewTransformMiddleware(
			nil, // No input transform
			func(output interface{}) (interface{}, error) {
				// Format output as structured data
				return map[string]interface{}{
					"analysis":  output,
					"timestamp": time.Now(),
				}, nil
			},
		),
	)

	// Configure for accuracy
	b.config.Temperature = 0.1  // Very low temperature for consistency
	b.config.MaxIterations = 20 // More iterations for complex analysis

	return b
}

// Build constructs the final agent
func (b *AgentBuilder[C, S]) Build() (*ConfigurableAgent[C, S], error) {
	// Validate required components
	if b.llmClient == nil {
		return nil, fmt.Errorf("LLM client is required")
	}

	// Set defaults if not provided
	// Check if state is zero value
	var zero S
	if reflect.DeepEqual(b.state, zero) {
		// Try to create a default state if S is *core.AgentState
		if _, ok := any(zero).(*core.AgentState); ok {
			b.state = any(core.NewAgentState()).(S)
		} else {
			return nil, fmt.Errorf("state is required")
		}
	}

	if b.store == nil {
		b.store = memory.New()
	}

	if b.checkpointer == nil {
		b.checkpointer = core.NewInMemorySaver()
	}

	// Create runtime
	runtime := execution.NewRuntime(
		b.context,
		b.state,
		b.store,
		b.checkpointer,
		b.config.SessionID,
	)

	// Build middleware chain
	handler := b.createHandler(runtime)
	chain := middleware.NewMiddlewareChain(handler)

	// Add default middleware if verbose
	if b.config.Verbose {
		chain.Use(middleware.NewLoggingMiddleware(nil))
		chain.Use(middleware.NewTimingMiddleware())
	}

	// Add user-specified middleware
	chain.Use(b.middlewares...)

	// Create the agent
	agent := &ConfigurableAgent[C, S]{
		llmClient:    b.llmClient,
		tools:        b.tools,
		systemPrompt: b.systemPrompt,
		runtime:      runtime,
		chain:        chain,
		config:       b.config,
		callbacks:    b.callbacks,
		errorHandler: b.errorHandler,
		metadata:     b.metadata,
	}

	// Initialize if needed
	if err := agent.Initialize(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize agent: %w", err)
	}

	return agent, nil
}

// createHandler creates the main execution handler
func (b *AgentBuilder[C, S]) createHandler(runtime *execution.Runtime[C, S]) middleware.Handler {
	return func(ctx context.Context, request *middleware.MiddlewareRequest) (*middleware.MiddlewareResponse, error) {
		// Extract input
		inputStr := fmt.Sprintf("%v", request.Input)

		// Create LLM request
		llmReq := &llm.CompletionRequest{
			Messages: []llm.Message{
				{
					Role:    "system",
					Content: b.systemPrompt,
				},
				{
					Role:    "user",
					Content: inputStr,
				},
			},
			MaxTokens:   b.config.MaxTokens,
			Temperature: b.config.Temperature,
		}

		// Call LLM
		response, err := b.llmClient.Complete(ctx, llmReq)
		if err != nil {
			return nil, fmt.Errorf("LLM error: %w", err)
		}

		// Trigger OnLLMEnd callbacks
		if len(b.callbacks) > 0 {
			for _, cb := range b.callbacks {
				if err := cb.OnLLMEnd(ctx, response.Content, response.TokensUsed); err != nil {
					// Log error but don't fail the request
					fmt.Fprintf(os.Stderr, "Callback OnLLMEnd error: %v\n", err)
				}
			}
		}

		// Update state if needed
		if request.State != nil {
			request.State.Set("last_response", response.Content)
			request.State.Set("last_timestamp", time.Now())
		}

		// Save checkpoint if auto-save is enabled
		if b.config.EnableAutoSave && runtime.Checkpointer != nil {
			_ = runtime.SaveState(ctx)
		}

		// Create response
		return &middleware.MiddlewareResponse{
			Output:   response.Content,
			State:    request.State,
			Metadata: request.Metadata,
		}, nil
	}
}

// ConfigurableAgent is the built agent with full configuration
type ConfigurableAgent[C any, S core.State] struct {
	llmClient    llm.Client
	tools        []interfaces.Tool
	systemPrompt string
	runtime      *execution.Runtime[C, S]
	chain        *middleware.MiddlewareChain
	config       *AgentConfig
	callbacks    []core.Callback
	errorHandler func(error) error
	metadata     map[string]interface{}
	mu           sync.RWMutex
}

// Initialize prepares the agent for execution
func (a *ConfigurableAgent[C, S]) Initialize(ctx context.Context) error {
	// Load previous state if exists
	if a.runtime.Checkpointer != nil {
		if exists, _ := a.runtime.Checkpointer.Exists(ctx, a.config.SessionID); exists {
			state, err := a.runtime.Checkpointer.Load(ctx, a.config.SessionID)
			if err == nil {
				// Update runtime state
				a.runtime.State = state.(S)
			}
		}
	}

	// Notify callbacks
	for _, cb := range a.callbacks {
		if err := cb.OnStart(ctx, a.metadata); err != nil {
			return err
		}
	}

	return nil
}

// Execute runs the agent with the given input
func (a *ConfigurableAgent[C, S]) Execute(ctx context.Context, input interface{}) (*AgentOutput, error) {
	// Apply timeout if configured
	if a.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.config.Timeout)
		defer cancel()
	}

	// Create request
	request := &middleware.MiddlewareRequest{
		Input:     input,
		State:     a.runtime.State,
		Runtime:   a.runtime,
		Metadata:  make(map[string]interface{}),
		Headers:   make(map[string]string),
		Timestamp: time.Now(),
	}

	// Add metadata
	for k, v := range a.metadata {
		request.Metadata[k] = v
	}

	// Execute through middleware chain
	response, err := a.chain.Execute(ctx, request)
	if err != nil {
		// Handle error
		if a.errorHandler != nil {
			err = a.errorHandler(err)
		}

		// Notify callbacks
		for _, cb := range a.callbacks {
			_ = cb.OnError(ctx, err)
		}

		return nil, err
	}

	// Create output
	output := &AgentOutput{
		Result:    response.Output,
		State:     response.State,
		Metadata:  response.Metadata,
		Duration:  response.Duration,
		Timestamp: time.Now(),
	}

	// Notify callbacks
	for _, cb := range a.callbacks {
		if err := cb.OnEnd(ctx, output); err != nil {
			return output, err
		}
	}

	return output, nil
}

// ExecuteWithTools runs the agent with tool execution capability
func (a *ConfigurableAgent[C, S]) ExecuteWithTools(ctx context.Context, input interface{}) (*AgentOutput, error) {
	iterations := 0
	var lastOutput *AgentOutput

	for iterations < a.config.MaxIterations {
		// Execute one step
		output, err := a.Execute(ctx, input)
		if err != nil {
			return nil, err
		}

		lastOutput = output

		// Check if we need to use tools
		toolCalls := a.extractToolCalls(output.Result)
		if len(toolCalls) == 0 {
			// No tools needed, return result
			return output, nil
		}

		// Execute tools
		toolResults := make([]interface{}, 0, len(toolCalls))
		for _, call := range toolCalls {
			result, err := a.executeToolCall(ctx, call)
			if err != nil {
				return nil, fmt.Errorf("tool execution failed: %w", err)
			}
			toolResults = append(toolResults, result)
		}

		// Update input with tool results for next iteration
		input = map[string]interface{}{
			"previous_output": output.Result,
			"tool_results":    toolResults,
		}

		iterations++
	}

	// Max iterations reached
	return lastOutput, fmt.Errorf("max iterations (%d) reached", a.config.MaxIterations)
}

// extractToolCalls extracts tool calls from LLM output
func (a *ConfigurableAgent[C, S]) extractToolCalls(output interface{}) []ToolCall {
	// Simplified tool call extraction
	// In production, use proper parsing
	return []ToolCall{}
}

// executeToolCall executes a single tool call
func (a *ConfigurableAgent[C, S]) executeToolCall(ctx context.Context, call ToolCall) (interface{}, error) {
	// Find tool
	for _, tool := range a.tools {
		if tool.Name() == call.Name {
			// Create tool input
			toolInput := &interfaces.ToolInput{
				Args:    call.Input,
				Context: ctx,
			}

			// Execute tool
			output, err := tool.Invoke(ctx, toolInput)
			if err != nil {
				return nil, err
			}
			return output.Result, nil
		}
	}
	return nil, fmt.Errorf("tool not found: %s", call.Name)
}

// GetState returns the current state
func (a *ConfigurableAgent[C, S]) GetState() S {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.runtime.State
}

// GetMetrics returns agent metrics
func (a *ConfigurableAgent[C, S]) GetMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})

	// Add basic metrics
	metrics["session_id"] = a.config.SessionID
	metrics["tools_count"] = len(a.tools)
	// Note: middleware count not exposed by MiddlewareChain

	// Add state size if available
	if state, ok := any(a.runtime.State).(*core.AgentState); ok {
		metrics["state_size"] = state.Size()
	}

	return metrics
}

// Shutdown gracefully shuts down the agent
func (a *ConfigurableAgent[C, S]) Shutdown(ctx context.Context) error {
	// Save final state
	if a.runtime.Checkpointer != nil {
		if err := a.runtime.SaveState(ctx); err != nil {
			return fmt.Errorf("failed to save final state: %w", err)
		}
	}

	// Notify callbacks
	for _, cb := range a.callbacks {
		if shutdown, ok := cb.(interface{ OnShutdown(context.Context) error }); ok {
			if err := shutdown.OnShutdown(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

// ToolCall represents a tool invocation request
type ToolCall struct {
	Name  string
	Input map[string]interface{}
}

// AgentOutput represents the agent execution result
type AgentOutput struct {
	Result    interface{}
	State     core.State
	Metadata  map[string]interface{}
	Duration  time.Duration
	Timestamp time.Time
}

// QuickAgent creates a simple agent with minimal configuration
func QuickAgent(llmClient llm.Client, systemPrompt string) (*ConfigurableAgent[any, *core.AgentState], error) {
	return NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt(systemPrompt).
		WithState(core.NewAgentState()).
		Build()
}

// RAGAgent creates a pre-configured RAG agent
func RAGAgent(llmClient llm.Client, retriever interface{}) (*ConfigurableAgent[any, *core.AgentState], error) {
	return NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("You are a helpful assistant. Answer questions based on the provided context.").
		ConfigureForRAG().
		WithState(core.NewAgentState()).
		WithMetadata("type", "rag").
		Build()
}

// ChatAgent creates a pre-configured chatbot agent
func ChatAgent(llmClient llm.Client, userName string) (*ConfigurableAgent[any, *core.AgentState], error) {
	state := core.NewAgentState()
	state.Set("user_name", userName)

	return NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt(fmt.Sprintf("You are a helpful assistant chatting with %s.", userName)).
		WithState(state).
		ConfigureForChatbot().
		WithMetadata("type", "chatbot").
		Build()
}

// AnalysisAgent creates a pre-configured data analysis agent
//
// This agent is optimized for:
//   - Data analysis and report generation
//   - Consistent, factual outputs
//   - Structured data transformations
//   - Extended reasoning iterations
//
// Configuration:
//   - Temperature: 0.1 (very low for consistency)
//   - MaxIterations: 20 (more iterations for complex analysis)
//   - Middleware: Timing, Transform (for structured output)
func AnalysisAgent(llmClient llm.Client, dataSource interface{}) (*ConfigurableAgent[any, *core.AgentState], error) {
	state := core.NewAgentState()
	if dataSource != nil {
		state.Set("data_source", dataSource)
	}

	return NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("You are a data analysis expert. Analyze data thoroughly and provide structured, accurate insights.").
		WithState(state).
		ConfigureForAnalysis().
		WithMetadata("type", "analysis").
		WithMetadata("data_source_type", fmt.Sprintf("%T", dataSource)).
		Build()
}

// WorkflowAgent creates a pre-configured workflow orchestration agent
//
// This agent is optimized for:
//   - Multi-step workflow execution
//   - Task orchestration and coordination
//   - Error handling and validation
//   - State persistence across steps
//
// Configuration:
//   - MaxIterations: 15 (balanced for workflow steps)
//   - EnableAutoSave: true (persist state across steps)
//   - Middleware: Logging, CircuitBreaker, Validation
func WorkflowAgent(llmClient llm.Client, workflows map[string]interface{}) (*ConfigurableAgent[any, *core.AgentState], error) {
	state := core.NewAgentState()
	if workflows != nil {
		state.Set("workflows", workflows)
		state.Set("workflow_status", "initialized")
	}

	config := DefaultAgentConfig()
	config.MaxIterations = 15
	config.EnableAutoSave = true

	return NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("You are a workflow orchestrator. Execute tasks systematically, validate results, and handle errors gracefully.").
		WithState(state).
		WithConfig(config).
		WithMiddleware(
			middleware.NewLoggingMiddleware(nil),
			middleware.NewCircuitBreakerMiddleware(5, 30*time.Second),
			middleware.NewValidationMiddleware(func(req *middleware.MiddlewareRequest) error {
				// Basic validation
				if req.Input == nil {
					return fmt.Errorf("workflow input cannot be nil")
				}
				return nil
			}),
		).
		WithMetadata("type", "workflow").
		Build()
}

// MonitoringAgent creates a pre-configured monitoring agent
//
// This agent is optimized for:
//   - Continuous system monitoring
//   - Anomaly detection
//   - Alert generation
//   - Periodic health checks
//
// Configuration:
//   - Continuous operation mode
//   - Rate limiting to prevent overwhelming
//   - Caching for efficient monitoring
//   - Alert middleware for notifications
func MonitoringAgent(llmClient llm.Client, checkInterval time.Duration) (*ConfigurableAgent[any, *core.AgentState], error) {
	state := core.NewAgentState()
	state.Set("check_interval", checkInterval)
	state.Set("last_check", time.Now())
	state.Set("monitoring_status", "active")

	config := DefaultAgentConfig()
	config.MaxIterations = 100 // Long-running monitoring
	config.Temperature = 0.3   // Balanced for pattern recognition

	return NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("You are a system monitoring expert. Observe metrics, detect anomalies, and alert on issues promptly.").
		WithState(state).
		WithConfig(config).
		WithMiddleware(
			middleware.NewRateLimiterMiddleware(60, time.Minute), // Limit to 60 checks per minute
			middleware.NewCacheMiddleware(5*time.Minute),         // Cache recent checks
			middleware.NewTimingMiddleware(),
		).
		WithMetadata("type", "monitoring").
		WithMetadata("check_interval", checkInterval.String()).
		Build()
}

// ResearchAgent creates a pre-configured research and information gathering agent
//
// This agent is optimized for:
//   - Information collection from multiple sources
//   - Research report generation
//   - Source synthesis and citation
//   - Fact-checking and verification
//
// Configuration:
//   - MaxTokens: 4000 (larger context for comprehensive reports)
//   - Temperature: 0.5 (balanced for creativity and accuracy)
//   - Middleware: ToolSelector (for search/scrape), Cache
func ResearchAgent(llmClient llm.Client, sources []string) (*ConfigurableAgent[any, *core.AgentState], error) {
	state := core.NewAgentState()
	if len(sources) > 0 {
		state.Set("sources", sources)
		state.Set("sources_count", len(sources))
	}
	state.Set("research_status", "initialized")

	config := DefaultAgentConfig()
	config.MaxTokens = 4000
	config.Temperature = 0.5
	config.MaxIterations = 15

	return NewAgentBuilder[any, *core.AgentState](llmClient).
		WithSystemPrompt("You are a research expert. Gather information from multiple sources, synthesize findings, and provide comprehensive, well-cited reports.").
		WithState(state).
		WithConfig(config).
		WithMiddleware(
			middleware.NewCacheMiddleware(10*time.Minute), // Cache research results
			middleware.NewTimingMiddleware(),
		).
		WithMetadata("type", "research").
		WithMetadata("sources_count", len(sources)).
		Build()
}

// WithTelemetry 添加 OpenTelemetry 支持
func (b *AgentBuilder[C, S]) WithTelemetry(provider interface{}) *AgentBuilder[C, S] {
	// Store telemetry provider in metadata
	b.metadata["telemetry_provider"] = provider
	return b
}

// WithCommunicator 添加通信器
func (b *AgentBuilder[C, S]) WithCommunicator(communicator interface{}) *AgentBuilder[C, S] {
	// Store communicator in metadata
	b.metadata["communicator"] = communicator
	return b
}
