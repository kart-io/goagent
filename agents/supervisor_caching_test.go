package agents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/performance"
)

// TestCachedSupervisorAgent tests caching functionality for supervisor agents
func TestCachedSupervisorAgent(t *testing.T) {
	ctx := context.Background()

	t.Run("CacheHit", func(t *testing.T) {
		// Create cached supervisor
		llmClient := createMockLLM()
		config := DefaultSupervisorConfig()
		config.CacheConfig = &performance.CacheConfig{
			TTL:         10 * time.Minute,
			MaxSize:     100,
			EnableStats: true,
		}

		// Add sub-agents
		supervisor := NewSupervisorAgent(llmClient, config)
		supervisor.AddSubAgent("test_agent", createTestAgent())
		cachedSupervisor := performance.NewCachedAgent(supervisor, *config.CacheConfig)
		defer closeCachedAgent(cachedSupervisor)

		input := &core.AgentInput{
			Task:      "Test task",
			Timestamp: time.Now(),
		}

		// First call - cache miss
		start := time.Now()
		result1, err := cachedSupervisor.Invoke(ctx, input)
		firstDuration := time.Since(start)
		require.NoError(t, err)
		require.NotNil(t, result1)

		// Second call - cache hit
		start = time.Now()
		result2, err := cachedSupervisor.Invoke(ctx, input)
		secondDuration := time.Since(start)
		require.NoError(t, err)
		require.NotNil(t, result2)

		// Cache hit should be much faster
		assert.Less(t, secondDuration, firstDuration,
			"Cache hit should be faster than cache miss")

		// Verify cache statistics
		stats := cachedSupervisor.Stats()
		assert.Equal(t, int64(1), stats.Hits, "Should have 1 cache hit")
		assert.Equal(t, int64(1), stats.Misses, "Should have 1 cache miss")
		assert.Greater(t, stats.HitRate, 0.0, "Hit rate should be > 0")
	})

	t.Run("CacheMiss", func(t *testing.T) {
		llmClient := createMockLLM()
		config := DefaultSupervisorConfig()
		config.CacheConfig = &performance.CacheConfig{
			TTL:         10 * time.Minute,
			MaxSize:     100,
			EnableStats: true,
		}

		supervisor := NewSupervisorAgent(llmClient, config)
		supervisor.AddSubAgent("test_agent", createTestAgent())
		cachedSupervisor := performance.NewCachedAgent(supervisor, *config.CacheConfig)
		defer closeCachedAgent(cachedSupervisor)

		// Different inputs should cause cache misses
		input1 := &core.AgentInput{Task: "Task 1", Timestamp: time.Now()}
		input2 := &core.AgentInput{Task: "Task 2", Timestamp: time.Now()}

		_, err := cachedSupervisor.Invoke(ctx, input1)
		require.NoError(t, err)

		_, err = cachedSupervisor.Invoke(ctx, input2)
		require.NoError(t, err)

		// Verify both were cache misses
		stats := cachedSupervisor.Stats()
		assert.Equal(t, int64(0), stats.Hits, "Should have 0 cache hits")
		assert.Equal(t, int64(2), stats.Misses, "Should have 2 cache misses")
	})

	t.Run("CacheExpiration", func(t *testing.T) {
		llmClient := createMockLLM()
		config := DefaultSupervisorConfig()
		config.CacheConfig = &performance.CacheConfig{
			TTL:             100 * time.Millisecond, // Short TTL for testing
			MaxSize:         100,
			CleanupInterval: 50 * time.Millisecond,
			EnableStats:     true,
		}

		supervisor := NewSupervisorAgent(llmClient, config)
		supervisor.AddSubAgent("test_agent", createTestAgent())
		cachedSupervisor := performance.NewCachedAgent(supervisor, *config.CacheConfig)
		defer closeCachedAgent(cachedSupervisor)

		input := &core.AgentInput{Task: "Expiring task", Timestamp: time.Now()}

		// First call
		_, err := cachedSupervisor.Invoke(ctx, input)
		require.NoError(t, err)

		// Second call immediately - should hit cache
		_, err = cachedSupervisor.Invoke(ctx, input)
		require.NoError(t, err)

		stats := cachedSupervisor.Stats()
		assert.Equal(t, int64(1), stats.Hits, "Should have 1 cache hit before expiration")

		// Wait for expiration
		time.Sleep(200 * time.Millisecond)

		// Third call after expiration - should be cache miss
		_, err = cachedSupervisor.Invoke(ctx, input)
		require.NoError(t, err)

		stats = cachedSupervisor.Stats()
		assert.Equal(t, int64(2), stats.Misses, "Should have 2 cache misses after expiration")
	})

	t.Run("CacheStatistics", func(t *testing.T) {
		llmClient := createMockLLM()
		config := DefaultSupervisorConfig()
		config.CacheConfig = &performance.CacheConfig{
			TTL:         10 * time.Minute,
			MaxSize:     100,
			EnableStats: true,
		}

		supervisor := NewSupervisorAgent(llmClient, config)
		supervisor.AddSubAgent("test_agent", createTestAgent())
		cachedSupervisor := performance.NewCachedAgent(supervisor, *config.CacheConfig)
		defer closeCachedAgent(cachedSupervisor)

		// Execute mixed operations
		tasks := []string{"Task A", "Task B", "Task A", "Task C", "Task A", "Task B"}

		for _, task := range tasks {
			input := &core.AgentInput{Task: task, Timestamp: time.Now()}
			_, _ = cachedSupervisor.Invoke(ctx, input)
		}

		// Verify statistics
		stats := cachedSupervisor.Stats()

		// Should have 3 unique tasks (A, B, C)
		assert.Equal(t, int64(3), stats.Misses, "Should have 3 unique tasks (misses)")
		// Task A hit twice, Task B hit once = 3 hits
		assert.Equal(t, int64(3), stats.Hits, "Should have 3 cache hits")
		assert.Equal(t, 3, stats.Size, "Cache should contain 3 entries")

		// Check hit rate
		expectedHitRate := (3.0 / 6.0) * 100.0 // 3 hits out of 6 total
		assert.InDelta(t, expectedHitRate, stats.HitRate, 0.1, "Hit rate should be ~50%")
	})

	t.Run("PerformanceGain", func(t *testing.T) {
		llmClient := createMockLLM()
		config := DefaultSupervisorConfig()

		// Uncached supervisor
		uncachedSupervisor := NewSupervisorAgent(llmClient, config)
		uncachedSupervisor.AddSubAgent("test_agent", createTestAgent())

		// Cached supervisor
		config.CacheConfig = &performance.CacheConfig{
			TTL:         10 * time.Minute,
			MaxSize:     100,
			EnableStats: true,
		}
		supervisor := NewSupervisorAgent(llmClient, config)
		supervisor.AddSubAgent("test_agent", createTestAgent())
		cachedSupervisor := performance.NewCachedAgent(supervisor, *config.CacheConfig)
		defer closeCachedAgent(cachedSupervisor)

		input := &core.AgentInput{Task: "Performance test", Timestamp: time.Now()}

		// Measure uncached performance (5 calls)
		uncachedStart := time.Now()
		for i := 0; i < 5; i++ {
			_, _ = uncachedSupervisor.Invoke(ctx, input)
		}
		uncachedDuration := time.Since(uncachedStart)

		// Measure cached performance (5 calls, 1 miss + 4 hits)
		cachedStart := time.Now()
		for i := 0; i < 5; i++ {
			_, _ = cachedSupervisor.Invoke(ctx, input)
		}
		cachedDuration := time.Since(cachedStart)

		// Cached version should be significantly faster
		speedup := float64(uncachedDuration) / float64(cachedDuration)
		assert.Greater(t, speedup, 2.0,
			fmt.Sprintf("Cache should provide >2x speedup, got %.2fx", speedup))

		t.Logf("Performance gain: %.2fx (uncached: %v, cached: %v)",
			speedup, uncachedDuration, cachedDuration)
	})
}

// Helper functions

func createMockLLM() llm.Client {
	return &mockLLMForCaching{}
}

type mockLLMForCaching struct{}

func (m *mockLLMForCaching) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	// Simulate LLM processing delay
	time.Sleep(50 * time.Millisecond)
	return &llm.CompletionResponse{
		Content: "Final Answer: Mock response",
		Usage: &interfaces.TokenUsage{
			PromptTokens:     25,
			CompletionTokens: 25,
			TotalTokens:      50,
		},
	}, nil
}

func (m *mockLLMForCaching) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// Simulate LLM processing delay
	time.Sleep(50 * time.Millisecond)

	content := `[
		{"id": "task_1", "type": "test", "description": "Test task", "priority": 1}
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

func (m *mockLLMForCaching) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (m *mockLLMForCaching) IsAvailable() bool {
	return true
}

func createTestAgent() core.Agent {
	return &testAgentForCaching{delay: 30 * time.Millisecond}
}

type testAgentForCaching struct {
	delay time.Duration
}

func (t *testAgentForCaching) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	time.Sleep(t.delay)
	return &core.AgentOutput{
		Result:    fmt.Sprintf("Result for: %s", input.Task),
		Status:    interfaces.StatusSuccess,
		Message:   "Success",
		Timestamp: time.Now(),
		Latency:   t.delay,
	}, nil
}

func (t *testAgentForCaching) Name() string {
	return "test-agent"
}

func (t *testAgentForCaching) Description() string {
	return "Test agent for caching"
}

func (t *testAgentForCaching) Capabilities() []string {
	return []string{"test"}
}

func (t *testAgentForCaching) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput], 1)
	go func() {
		defer close(ch)
		output, err := t.Invoke(ctx, input)
		ch <- core.StreamChunk[*core.AgentOutput]{Data: output, Error: err, Done: true}
	}()
	return ch, nil
}

func (t *testAgentForCaching) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	outputs := make([]*core.AgentOutput, len(inputs))
	for i, input := range inputs {
		output, err := t.Invoke(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

func (t *testAgentForCaching) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return nil
}

func (t *testAgentForCaching) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return t
}

func (t *testAgentForCaching) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return t
}

func closeCachedAgent(agent *performance.CachedAgent) {
	agent.Close()
}
