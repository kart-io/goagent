package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/agents"
	"github.com/kart-io/goagent/agents/react"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/performance"
)

func main() {
	ctx := context.Background()

	fmt.Println("=== Cached Agent Performance Demo ===")
	fmt.Println()

	// Demo 1: Cached Supervisor Agent
	fmt.Println("--- Demo 1: Cached Supervisor Agent ---")
	demoCachedSupervisor(ctx)

	fmt.Println("\n" + strings.Repeat("=", 60) + "\n")

	// Demo 2: Cached ReAct Agent
	fmt.Println("--- Demo 2: Cached ReAct Agent ---")
	demoCachedReAct(ctx)

	fmt.Println("\n" + strings.Repeat("=", 60) + "\n")

	// Demo 3: Cache vs No Cache comparison
	fmt.Println("--- Demo 3: Cache vs No Cache Comparison ---")
	demoCacheComparison(ctx)

	fmt.Println("\n" + strings.Repeat("=", 60) + "\n")

	// Demo 4: Cache statistics
	fmt.Println("--- Demo 4: Cache Statistics ---")
	demoCacheStatistics(ctx)
}

func demoCachedSupervisor(ctx context.Context) {
	// Create mock LLM client
	llmClient := createMockLLMClient()

	// Create supervisor with caching enabled
	config := agents.DefaultSupervisorConfig()
	config.CacheConfig = &performance.CacheConfig{
		TTL:             10 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 5 * time.Minute,
		EnableStats:     true,
	}

	// Use the helper function to create cached supervisor
	cachedSupervisor := agents.NewCachedSupervisorAgent(llmClient, config)
	defer func() {
		if closer, ok := cachedSupervisor.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	// Get underlying supervisor to add sub-agents
	// Note: In production, you'd add sub-agents before wrapping with cache
	supervisor := agents.NewSupervisorAgent(llmClient, config)
	supervisor.AddSubAgent("data_analyzer", createDataAnalyzerAgent())
	supervisor.AddSubAgent("report_generator", createReportAgent())

	// Re-wrap with cache
	cachedSupervisor = performance.NewCachedAgent(supervisor, *config.CacheConfig)
	defer func() {
		if ca, ok := cachedSupervisor.(*performance.CachedAgent); ok {
			_ = ca.Close()
		}
	}()

	// Execute same query multiple times
	input := &core.AgentInput{
		Task:      "Analyze sales data and generate report",
		Timestamp: time.Now(),
	}

	// First call - cache miss
	fmt.Println("Executing first call (cache miss)...")
	start := time.Now()
	result1, err := cachedSupervisor.Invoke(ctx, input)
	firstCallDuration := time.Since(start)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("First call duration: %v\n", firstCallDuration)
	fmt.Printf("Result: %v\n", result1.Status)

	// Second call - cache hit
	fmt.Println("\nExecuting second call (cache hit)...")
	start = time.Now()
	result2, err := cachedSupervisor.Invoke(ctx, input)
	secondCallDuration := time.Since(start)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Second call duration: %v\n", secondCallDuration)
	fmt.Printf("Result: %v\n", result2.Status)

	// Calculate speedup
	if secondCallDuration > 0 {
		speedup := float64(firstCallDuration) / float64(secondCallDuration)
		fmt.Printf("\nSpeedup: %.2fx\n", speedup)
	}

	// Get cache statistics
	if cachedAgent, ok := cachedSupervisor.(*performance.CachedAgent); ok {
		stats := cachedAgent.Stats()
		fmt.Printf("\nCache Statistics:\n")
		fmt.Printf("  Hits: %d, Misses: %d\n", stats.Hits, stats.Misses)
		fmt.Printf("  Hit rate: %.2f%%\n", stats.HitRate)
	}
}

func demoCachedReAct(ctx context.Context) {
	// Create mock LLM client and tools
	llmClient := createMockLLMClient()
	mockTools := createMockTools()

	// Create ReAct agent with caching
	config := react.ReActConfig{
		Name:        "cached-react",
		Description: "ReAct agent with caching enabled",
		LLM:         llmClient,
		Tools:       mockTools,
		MaxSteps:    10,
	}

	cacheConfig := &performance.CacheConfig{
		TTL:         5 * time.Minute,
		MaxSize:     500,
		EnableStats: true,
	}

	cachedAgent := react.NewCachedReActAgent(config, cacheConfig)
	defer func() {
		if closer, ok := cachedAgent.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	// Execute same reasoning task multiple times
	input := &core.AgentInput{
		Task:      "What is 2 + 2?",
		Timestamp: time.Now(),
	}

	// First execution - cache miss
	fmt.Println("Executing first call (cache miss)...")
	start := time.Now()
	result1, err := cachedAgent.Invoke(ctx, input)
	firstDuration := time.Since(start)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("First execution: %v\n", firstDuration)
	fmt.Printf("Result: %v\n", result1.Status)

	// Second execution - cache hit
	fmt.Println("\nExecuting second call (cache hit)...")
	start = time.Now()
	result2, err := cachedAgent.Invoke(ctx, input)
	secondDuration := time.Since(start)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Second execution: %v\n", secondDuration)
	fmt.Printf("Result: %v\n", result2.Status)

	// Calculate speedup
	if secondDuration > 0 {
		speedup := float64(firstDuration) / float64(secondDuration)
		fmt.Printf("\nSpeedup: %.2fx\n", speedup)
	}

	// Get cache statistics
	if cachedReact, ok := cachedAgent.(*performance.CachedAgent); ok {
		stats := cachedReact.Stats()
		fmt.Printf("\nCache Statistics:\n")
		fmt.Printf("  Hits: %d, Misses: %d\n", stats.Hits, stats.Misses)
		fmt.Printf("  Hit rate: %.2f%%\n", stats.HitRate)
	}
}

func demoCacheComparison(ctx context.Context) {
	llmClient := createMockLLMClient()

	// Create uncached supervisor
	config := agents.DefaultSupervisorConfig()
	uncachedSupervisor := agents.NewSupervisorAgent(llmClient, config)
	uncachedSupervisor.AddSubAgent("analyzer", createDataAnalyzerAgent())

	// Create cached supervisor
	config.CacheConfig = &performance.CacheConfig{
		TTL:         10 * time.Minute,
		MaxSize:     1000,
		EnableStats: true,
	}
	supervisor := agents.NewSupervisorAgent(llmClient, config)
	supervisor.AddSubAgent("analyzer", createDataAnalyzerAgent())
	cachedSupervisor := performance.NewCachedAgent(supervisor, *config.CacheConfig)

	input := &core.AgentInput{
		Task:      "Quick analysis task",
		Timestamp: time.Now(),
	}

	// Run 10 times each
	iterations := 10
	fmt.Printf("Running %d iterations...\n\n", iterations)

	// Uncached
	uncachedStart := time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = uncachedSupervisor.Invoke(ctx, input)
	}
	uncachedTotal := time.Since(uncachedStart)

	// Cached (first call is miss, rest are hits)
	cachedStart := time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = cachedSupervisor.Invoke(ctx, input)
	}
	cachedTotal := time.Since(cachedStart)

	fmt.Printf("Uncached total: %v (avg: %v/call)\n",
		uncachedTotal, uncachedTotal/time.Duration(iterations))
	fmt.Printf("Cached total: %v (avg: %v/call)\n",
		cachedTotal, cachedTotal/time.Duration(iterations))

	if cachedTotal > 0 {
		improvement := float64(uncachedTotal) / float64(cachedTotal)
		fmt.Printf("\nPerformance improvement: %.2fx\n", improvement)
	}

	// Cleanup
	_ = cachedSupervisor.Close()
}

func demoCacheStatistics(ctx context.Context) {
	llmClient := createMockLLMClient()

	config := agents.DefaultSupervisorConfig()
	config.CacheConfig = &performance.CacheConfig{
		TTL:         10 * time.Minute,
		MaxSize:     1000,
		EnableStats: true,
	}

	supervisor := agents.NewSupervisorAgent(llmClient, config)
	supervisor.AddSubAgent("analyzer", createDataAnalyzerAgent())
	cachedAgent := performance.NewCachedAgent(supervisor, *config.CacheConfig)

	// Execute various tasks
	tasks := []string{
		"Analyze sales data",
		"Generate report",
		"Analyze sales data", // Repeat
		"Calculate metrics",
		"Analyze sales data", // Repeat again
		"Generate report",    // Repeat
	}

	fmt.Println("Executing diverse tasks...")
	for i, task := range tasks {
		input := &core.AgentInput{
			Task:      task,
			Timestamp: time.Now(),
		}
		_, _ = cachedAgent.Invoke(ctx, input)
		fmt.Printf("  Task %d: %s\n", i+1, task)
	}

	// Get cache statistics
	stats := cachedAgent.Stats()
	fmt.Printf("\nCache Statistics:\n")
	fmt.Printf("  Total requests: %d\n", stats.Hits+stats.Misses)
	fmt.Printf("  Cache hits: %d\n", stats.Hits)
	fmt.Printf("  Cache misses: %d\n", stats.Misses)
	fmt.Printf("  Hit rate: %.2f%%\n", stats.HitRate)
	fmt.Printf("  Cache size: %d/%d entries\n", stats.Size, stats.MaxSize)
	fmt.Printf("  Avg hit time: %v\n", stats.AvgHitTime)
	fmt.Printf("  Avg miss time: %v\n", stats.AvgMissTime)

	if stats.AvgHitTime > 0 {
		speedup := float64(stats.AvgMissTime) / float64(stats.AvgHitTime)
		fmt.Printf("  Speedup on hits: %.2fx\n", speedup)
	}

	// Cleanup
	_ = cachedAgent.Close()
}

// Helper functions to create mock agents and tools

func createMockLLMClient() llm.Client {
	return &mockLLMClient{}
}

type mockLLMClient struct{}

func (m *mockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	// Simulate LLM delay
	time.Sleep(100 * time.Millisecond)
	return &llm.CompletionResponse{
		Content: "Final Answer: Mock response",
		Usage: &interfaces.TokenUsage{
			PromptTokens:     25,
			CompletionTokens: 25,
			TotalTokens:      50,
		},
	}, nil
}

func (m *mockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// Simulate LLM delay
	time.Sleep(100 * time.Millisecond)

	// Return mock task decomposition
	content := `[
		{"id": "task_1", "type": "analysis", "description": "Analyze data", "priority": 1},
		{"id": "task_2", "type": "report", "description": "Generate report", "priority": 2}
	]`

	return &llm.CompletionResponse{
		Content: content,
		Usage: &interfaces.TokenUsage{
			PromptTokens:     50,
			CompletionTokens: 50,
			TotalTokens:      100,
		},
	}, nil
}

func (m *mockLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (m *mockLLMClient) IsAvailable() bool {
	return true
}

func createDataAnalyzerAgent() core.Agent {
	return &mockAgent{name: "data_analyzer", delay: 50 * time.Millisecond}
}

func createReportAgent() core.Agent {
	return &mockAgent{name: "report_generator", delay: 50 * time.Millisecond}
}

type mockAgent struct {
	name  string
	delay time.Duration
}

func (m *mockAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	time.Sleep(m.delay)
	return &core.AgentOutput{
		Result:    fmt.Sprintf("%s result for: %s", m.name, input.Task),
		Status:    interfaces.StatusSuccess,
		Message:   "Completed",
		Timestamp: time.Now(),
		Latency:   m.delay,
	}, nil
}

func (m *mockAgent) Name() string {
	return m.name
}

func (m *mockAgent) Description() string {
	return fmt.Sprintf("Mock %s agent", m.name)
}

func (m *mockAgent) Capabilities() []string {
	return []string{"mock"}
}

func (m *mockAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput], 1)
	go func() {
		defer close(ch)
		output, err := m.Invoke(ctx, input)
		ch <- core.StreamChunk[*core.AgentOutput]{Data: output, Error: err, Done: true}
	}()
	return ch, nil
}

func (m *mockAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	outputs := make([]*core.AgentOutput, len(inputs))
	for i, input := range inputs {
		output, err := m.Invoke(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

func (m *mockAgent) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return nil
}

func (m *mockAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return m
}

func (m *mockAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return m
}

func createMockTools() []interfaces.Tool {
	return []interfaces.Tool{
		&mockTool{name: "calculator", delay: 10 * time.Millisecond},
		&mockTool{name: "search", delay: 20 * time.Millisecond},
	}
}

type mockTool struct {
	name  string
	delay time.Duration
}

func (t *mockTool) Name() string {
	return t.name
}

func (t *mockTool) Description() string {
	return fmt.Sprintf("Mock %s tool", t.name)
}

func (t *mockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	time.Sleep(t.delay)
	return &interfaces.ToolOutput{
		Result:  fmt.Sprintf("%s result", t.name),
		Success: true,
	}, nil
}

func (t *mockTool) ArgsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"input": {"type": "string", "description": "Tool input"}
		}
	}`
}
