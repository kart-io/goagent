package middleware

import (
	"context"
	"fmt"

	agentErrors "github.com/kart-io/goagent/errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	coremiddleware "github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// LLMToolSelectorMiddleware intelligently selects relevant tools using an LLM
type LLMToolSelectorMiddleware struct {
	*coremiddleware.BaseMiddleware
	Model          llm.Client          // Cheaper model for selection
	MaxTools       int                 // Maximum tools to select
	AlwaysInclude  []string            // Tools to always include
	SelectionCache map[string][]string // Cache for tool selections
	CacheTTL       time.Duration
	mu             sync.RWMutex
}

// NewLLMToolSelectorMiddleware creates a new tool selector middleware
func NewLLMToolSelectorMiddleware(model llm.Client, maxTools int) *LLMToolSelectorMiddleware {
	return &LLMToolSelectorMiddleware{
		BaseMiddleware: coremiddleware.NewBaseMiddleware("llm-tool-selector"),
		Model:          model,
		MaxTools:       maxTools,
		AlwaysInclude:  []string{},
		SelectionCache: make(map[string][]string),
		CacheTTL:       5 * time.Minute,
	}
}

// WithAlwaysInclude sets tools that should always be included
func (m *LLMToolSelectorMiddleware) WithAlwaysInclude(tools ...string) *LLMToolSelectorMiddleware {
	m.AlwaysInclude = append(m.AlwaysInclude, tools...)
	return m
}

// Process selects relevant tools before the main model call
func (m *LLMToolSelectorMiddleware) Process(ctx context.Context, state core.State) (core.State, error) {
	// Get all available tools
	toolsVal, ok := state.Get("tools")
	if !ok {
		return state, nil // No tools found
	}
	allTools, ok := toolsVal.([]interfaces.Tool)
	if !ok || len(allTools) == 0 {
		return state, nil // No tools to select from
	}

	// Get user query
	queryVal, ok := state.Get("query")
	if !ok {
		return state, nil // No query found
	}
	query, ok := queryVal.(string)
	if !ok {
		return state, nil // No query to process
	}

	// Check cache
	cacheKey := m.getCacheKey(query, allTools)
	if cached := m.getCachedSelection(cacheKey); cached != nil {
		state.Set("tools", m.filterTools(allTools, cached))
		return state, nil
	}

	// Select tools using LLM
	selectedNames, err := m.selectTools(ctx, query, allTools)
	if err != nil {
		// Log error but don't fail - use all tools as fallback
		fmt.Printf("Tool selection failed: %v\n", err)
		return state, nil
	}

	// Cache the selection
	m.cacheSelection(cacheKey, selectedNames)

	// Filter tools and update state
	selectedTools := m.filterTools(allTools, selectedNames)
	state.Set("tools", selectedTools)
	state.Set("tool_selection_metadata", map[string]interface{}{
		"original_count": len(allTools),
		"selected_count": len(selectedTools),
		"selected_names": selectedNames,
	})

	return state, nil
}

// selectTools uses the LLM to select relevant tools
func (m *LLMToolSelectorMiddleware) selectTools(ctx context.Context, query string, allTools []interfaces.Tool) ([]string, error) {
	// Build tool descriptions
	toolDescriptions := m.buildToolDescriptions(allTools)

	// Create selection prompt
	prompt := fmt.Sprintf(`
		User Query: %s

		Available Tools:
		%s

		Select up to %d most relevant tools for this query.
		Consider tool capabilities and how they might be combined.
		Return tool names as a comma-separated list.

		Selected tools:
	`, query, toolDescriptions, m.MaxTools)

	// Call LLM
	response, err := m.Model.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.SystemMessage("You are an expert at selecting the most relevant tools for a given task."),
			llm.UserMessage(prompt),
		},
		Temperature: 0.3, // Lower temperature for consistent selection
		MaxTokens:   100,
	})
	if err != nil {
		return nil, err
	}

	// Parse selected tools
	selectedNames := m.parseToolSelection(response.Content)

	// Ensure always-include tools are present
	selectedNames = m.ensureAlwaysIncluded(selectedNames)

	return selectedNames, nil
}

// buildToolDescriptions creates descriptions for all tools
func (m *LLMToolSelectorMiddleware) buildToolDescriptions(allTools []interfaces.Tool) string {
	descriptions := []string{}
	for _, tool := range allTools {
		desc := fmt.Sprintf("- %s: %s", tool.Name(), tool.Description())
		descriptions = append(descriptions, desc)
	}
	return strings.Join(descriptions, "\n")
}

// parseToolSelection parses the LLM response to extract tool names
func (m *LLMToolSelectorMiddleware) parseToolSelection(response string) []string {
	// Clean up response
	response = strings.TrimSpace(response)
	response = strings.Trim(response, "[]{}\"'")

	// Split by comma
	parts := strings.Split(response, ",")

	selected := []string{}
	for _, part := range parts {
		name := strings.TrimSpace(part)
		name = strings.Trim(name, "\"'")
		if name != "" {
			selected = append(selected, name)
		}
	}

	return selected
}

// ensureAlwaysIncluded ensures certain tools are always selected
func (m *LLMToolSelectorMiddleware) ensureAlwaysIncluded(selected []string) []string {
	// Create a set of selected tools
	selectedSet := make(map[string]bool)
	for _, name := range selected {
		selectedSet[name] = true
	}

	// Add always-include tools
	for _, name := range m.AlwaysInclude {
		if !selectedSet[name] {
			selected = append(selected, name)
			selectedSet[name] = true
		}
	}

	// Trim to max tools if needed
	if len(selected) > m.MaxTools {
		selected = selected[:m.MaxTools]
	}

	return selected
}

// filterTools filters the tool list based on selected names
func (m *LLMToolSelectorMiddleware) filterTools(allTools []interfaces.Tool, selectedNames []string) []interfaces.Tool {
	// Create set for fast lookup
	selectedSet := make(map[string]bool)
	for _, name := range selectedNames {
		selectedSet[name] = true
	}

	// Filter tools
	filtered := []interfaces.Tool{}
	for _, tool := range allTools {
		if selectedSet[tool.Name()] {
			filtered = append(filtered, tool)
		}
	}

	return filtered
}

// getCacheKey generates a cache key for tool selection
func (m *LLMToolSelectorMiddleware) getCacheKey(query string, tools []interfaces.Tool) string {
	// Simple key based on query and tool count
	// In production, could hash the tool names as well
	return fmt.Sprintf("%s_%d", query, len(tools))
}

// getCachedSelection retrieves cached tool selection
func (m *LLMToolSelectorMiddleware) getCachedSelection(key string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if selection, exists := m.SelectionCache[key]; exists {
		return selection
	}
	return nil
}

// cacheSelection caches tool selection
func (m *LLMToolSelectorMiddleware) cacheSelection(key string, selection []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SelectionCache[key] = selection

	// Simple cache eviction after TTL
	go func() {
		time.Sleep(m.CacheTTL)
		m.mu.Lock()
		delete(m.SelectionCache, key)
		m.mu.Unlock()
	}()
}

// DynamicPromptMiddleware generates dynamic prompts based on runtime context
type DynamicPromptMiddleware struct {
	*coremiddleware.BaseMiddleware
	PromptGenerators []PromptGenerator
	mu               sync.RWMutex
}

// PromptGenerator generates prompts based on context
type PromptGenerator struct {
	Name      string
	Priority  int
	Condition func(core.State) bool
	Generate  func(core.State) string
}

// NewDynamicPromptMiddleware creates a new dynamic prompt middleware
func NewDynamicPromptMiddleware() *DynamicPromptMiddleware {
	return &DynamicPromptMiddleware{
		BaseMiddleware:   coremiddleware.NewBaseMiddleware("dynamic-prompt"),
		PromptGenerators: []PromptGenerator{},
	}
}

// AddGenerator adds a prompt generator
func (m *DynamicPromptMiddleware) AddGenerator(generator PromptGenerator) *DynamicPromptMiddleware {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PromptGenerators = append(m.PromptGenerators, generator)

	// Sort by priority
	sort.Slice(m.PromptGenerators, func(i, j int) bool {
		return m.PromptGenerators[i].Priority > m.PromptGenerators[j].Priority
	})

	return m
}

// Process generates dynamic prompts
func (m *DynamicPromptMiddleware) Process(ctx context.Context, state core.State) (core.State, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect applicable prompts
	prompts := []string{}

	for _, generator := range m.PromptGenerators {
		if generator.Condition(state) {
			prompt := generator.Generate(state)
			if prompt != "" {
				prompts = append(prompts, prompt)
			}
		}
	}

	// Combine prompts
	if len(prompts) > 0 {
		promptVal, _ := state.Get("system_prompt")
		currentPrompt, _ := promptVal.(string)
		enhancedPrompt := strings.Join(append([]string{currentPrompt}, prompts...), "\n\n")
		state.Set("system_prompt", enhancedPrompt)
		state.Set("prompt_enhancements", prompts)
	}

	return state, nil
}

// LLMToolEmulatorMiddleware emulates tool execution using an LLM
type LLMToolEmulatorMiddleware struct {
	*coremiddleware.BaseMiddleware
	Model         llm.Client
	EmulatedTools map[string]bool // Tools to emulate
	EmulateAll    bool            // Emulate all tools
	mu            sync.RWMutex
}

// NewLLMToolEmulatorMiddleware creates a new tool emulator
func NewLLMToolEmulatorMiddleware(model llm.Client) *LLMToolEmulatorMiddleware {
	return &LLMToolEmulatorMiddleware{
		BaseMiddleware: coremiddleware.NewBaseMiddleware("tool-emulator"),
		Model:          model,
		EmulatedTools:  make(map[string]bool),
		EmulateAll:     false,
	}
}

// SetEmulatedTools sets which tools to emulate
func (m *LLMToolEmulatorMiddleware) SetEmulatedTools(tools ...string) *LLMToolEmulatorMiddleware {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, tool := range tools {
		m.EmulatedTools[tool] = true
	}
	return m
}

// SetEmulateAll sets whether to emulate all tools
func (m *LLMToolEmulatorMiddleware) SetEmulateAll(emulateAll bool) *LLMToolEmulatorMiddleware {
	m.EmulateAll = emulateAll
	return m
}

// Process intercepts tool calls and emulates them
func (m *LLMToolEmulatorMiddleware) Process(ctx context.Context, state core.State) (core.State, error) {
	// Check if there's a tool call to intercept
	toolCallVal, ok := state.Get("pending_tool_call")
	if !ok {
		return state, nil
	}
	toolCall, ok := toolCallVal.(map[string]interface{})
	if !ok {
		return state, nil
	}

	toolName := toolCall["name"].(string)

	// Check if we should emulate this tool
	if !m.shouldEmulate(toolName) {
		return state, nil
	}

	// Emulate tool execution
	result, err := m.emulateTool(ctx, toolName, toolCall["input"])
	if err != nil {
		return state, err
	}

	// Replace tool result
	state.Set("tool_result", result)
	state.Set("tool_emulated", true)

	return state, nil
}

// shouldEmulate checks if a tool should be emulated
func (m *LLMToolEmulatorMiddleware) shouldEmulate(toolName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.EmulateAll {
		return true
	}

	return m.EmulatedTools[toolName]
}

// emulateTool uses LLM to emulate tool execution
func (m *LLMToolEmulatorMiddleware) emulateTool(ctx context.Context, toolName string, input interface{}) (interface{}, error) {
	prompt := fmt.Sprintf(`
		Emulate the execution of the following tool:
		Tool: %s
		Input: %v

		Generate a realistic output that this tool would produce.
		Be consistent and accurate in your emulation.

		Output:
	`, toolName, input)

	response, err := m.Model.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.SystemMessage("You are emulating tool execution for testing purposes."),
			llm.UserMessage(prompt),
		},
		Temperature: 0.5,
		MaxTokens:   500,
	})
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to emulate tool").
			WithComponent("tool_emulator").
			WithOperation("emulate_tool").
			WithContext("tool_name", toolName)
	}

	return response.Content, nil
}

// AdaptiveMiddleware adapts behavior based on runtime performance
type AdaptiveMiddleware struct {
	*coremiddleware.BaseMiddleware
	Metrics       *PerformanceMetrics
	Adaptations   []Adaptation
	CurrentConfig map[string]interface{}
	mu            sync.RWMutex
}

// PerformanceMetrics tracks performance metrics
type PerformanceMetrics struct {
	ResponseTimes  []time.Duration
	ErrorRates     []float64
	TokenUsage     []int
	SuccessRate    float64
	AverageLatency time.Duration
	mu             sync.RWMutex
}

// Adaptation defines an adaptation rule
type Adaptation struct {
	Name      string
	Condition func(*PerformanceMetrics) bool
	Apply     func(map[string]interface{})
}

// NewAdaptiveMiddleware creates a new adaptive middleware
func NewAdaptiveMiddleware() *AdaptiveMiddleware {
	return &AdaptiveMiddleware{
		BaseMiddleware: coremiddleware.NewBaseMiddleware("adaptive"),
		Metrics:        NewPerformanceMetrics(),
		Adaptations:    []Adaptation{},
		CurrentConfig:  make(map[string]interface{}),
	}
}

// NewPerformanceMetrics creates new performance metrics
func NewPerformanceMetrics() *PerformanceMetrics {
	return &PerformanceMetrics{
		ResponseTimes: []time.Duration{},
		ErrorRates:    []float64{},
		TokenUsage:    []int{},
	}
}

// AddAdaptation adds an adaptation rule
func (m *AdaptiveMiddleware) AddAdaptation(adaptation Adaptation) *AdaptiveMiddleware {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Adaptations = append(m.Adaptations, adaptation)
	return m
}

// Process applies adaptations based on performance
func (m *AdaptiveMiddleware) Process(ctx context.Context, state core.State) (core.State, error) {
	// Record start time
	startTime := time.Now()

	// Apply current adaptations
	m.applyAdaptations(state)

	// Process normally
	// (This would typically call the next middleware in chain)

	// Record metrics
	m.recordMetrics(startTime, state)

	// Check for new adaptations
	m.checkAdaptations()

	return state, nil
}

// applyAdaptations applies current configuration to state
func (m *AdaptiveMiddleware) applyAdaptations(state core.State) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for key, value := range m.CurrentConfig {
		state.Set(key, value)
	}
}

// recordMetrics records performance metrics
func (m *AdaptiveMiddleware) recordMetrics(startTime time.Time, state core.State) {
	duration := time.Since(startTime)

	m.Metrics.mu.Lock()
	defer m.Metrics.mu.Unlock()

	// Add response time
	m.Metrics.ResponseTimes = append(m.Metrics.ResponseTimes, duration)

	// Keep only last 100 measurements
	if len(m.Metrics.ResponseTimes) > 100 {
		m.Metrics.ResponseTimes = m.Metrics.ResponseTimes[1:]
	}

	// Calculate average latency
	total := time.Duration(0)
	for _, d := range m.Metrics.ResponseTimes {
		total += d
	}
	if len(m.Metrics.ResponseTimes) > 0 {
		m.Metrics.AverageLatency = total / time.Duration(len(m.Metrics.ResponseTimes))
	}

	// Update success rate based on state
	errVal, _ := state.Get("error")
	if err, ok := errVal.(error); ok && err != nil {
		// Error occurred
		m.Metrics.SuccessRate = m.Metrics.SuccessRate*0.95 + 0.0*0.05 // Exponential moving average
	} else {
		// Success
		m.Metrics.SuccessRate = m.Metrics.SuccessRate*0.95 + 1.0*0.05
	}
}

// checkAdaptations checks if any adaptations should be applied
func (m *AdaptiveMiddleware) checkAdaptations() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, adaptation := range m.Adaptations {
		if adaptation.Condition(m.Metrics) {
			adaptation.Apply(m.CurrentConfig)
		}
	}
}

// ContextEnrichmentMiddleware enriches context with additional information
type ContextEnrichmentMiddleware struct {
	*coremiddleware.BaseMiddleware
	Enrichers []ContextEnricher
	Cache     map[string]interface{}
	CacheTTL  time.Duration
	mu        sync.RWMutex
}

// ContextEnricher enriches context with additional data
type ContextEnricher struct {
	Name    string
	Enrich  func(context.Context, core.State) (map[string]interface{}, error)
	Async   bool
	Timeout time.Duration
}

// NewContextEnrichmentMiddleware creates a new context enrichment middleware
func NewContextEnrichmentMiddleware() *ContextEnrichmentMiddleware {
	return &ContextEnrichmentMiddleware{
		BaseMiddleware: coremiddleware.NewBaseMiddleware("context-enrichment"),
		Enrichers:      []ContextEnricher{},
		Cache:          make(map[string]interface{}),
		CacheTTL:       5 * time.Minute,
	}
}

// AddEnricher adds a context enricher
func (m *ContextEnrichmentMiddleware) AddEnricher(enricher ContextEnricher) *ContextEnrichmentMiddleware {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Enrichers = append(m.Enrichers, enricher)
	return m
}

// Process enriches the context with additional data
func (m *ContextEnrichmentMiddleware) Process(ctx context.Context, state core.State) (core.State, error) {
	enrichedData := make(map[string]interface{})

	// Run enrichers
	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := []error{}

	for _, enricher := range m.Enrichers {
		if enricher.Async {
			// Run asynchronously
			wg.Add(1)
			go func(e ContextEnricher) {
				defer wg.Done()

				enrichCtx := ctx
				if e.Timeout > 0 {
					var cancel context.CancelFunc
					enrichCtx, cancel = context.WithTimeout(ctx, e.Timeout)
					defer cancel()
				}

				data, err := e.Enrich(enrichCtx, state)

				mu.Lock()
				defer mu.Unlock()

				if err != nil {
					errors = append(errors, agentErrors.Wrap(err, agentErrors.CodeMiddlewareExecution, "enricher failed").
						WithComponent("context_enrichment").
						WithOperation("enrich").
						WithContext("enricher_name", e.Name))
				} else {
					for k, v := range data {
						enrichedData[k] = v
					}
				}
			}(enricher)
		} else {
			// Run synchronously
			data, err := enricher.Enrich(ctx, state)
			if err != nil {
				errors = append(errors, agentErrors.Wrap(err, agentErrors.CodeMiddlewareExecution, "enricher failed").
					WithComponent("context_enrichment").
					WithOperation("enrich").
					WithContext("enricher_name", enricher.Name))
			} else {
				for k, v := range data {
					enrichedData[k] = v
				}
			}
		}
	}

	// Wait for async enrichers
	wg.Wait()

	// Add enriched data to state
	if len(enrichedData) > 0 {
		contextVal, _ := state.Get("context")
		currentContext, _ := contextVal.(map[string]interface{})
		if currentContext == nil {
			currentContext = make(map[string]interface{})
		}

		for k, v := range enrichedData {
			currentContext[k] = v
		}

		state.Set("context", currentContext)
		state.Set("context_enriched", true)
	}

	// Log errors but don't fail
	if len(errors) > 0 {
		state.Set("enrichment_errors", errors)
	}

	return state, nil
}
