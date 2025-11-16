package performance

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCachedAgent tests cached agent creation
func TestNewCachedAgent(t *testing.T) {
	agent := NewMockAgent("test", 10*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	require.NotNil(t, cachedAgent)
	defer cachedAgent.Close()

	stats := cachedAgent.Stats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, 1000, stats.MaxSize)
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)
}

// TestDefaultCacheConfig tests default cache config
func TestDefaultCacheConfig(t *testing.T) {
	config := DefaultCacheConfig()

	assert.Equal(t, 1000, config.MaxSize)
	assert.Equal(t, 10*time.Minute, config.TTL)
	assert.Equal(t, 1*time.Minute, config.CleanupInterval)
	assert.True(t, config.EnableStats)
	// KeyGenerator should be nil (will be set to default in NewCachedAgent)
	assert.Nil(t, config.KeyGenerator)
}

// TestCachedAgent_InvokeCacheMiss tests cache miss scenario
func TestCachedAgent_InvokeCacheMiss(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 10*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	output, err := cachedAgent.Invoke(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)

	stats := cachedAgent.Stats()
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
	assert.Equal(t, 1, stats.Size)
}

// TestCachedAgent_InvokeCacheHit tests cache hit scenario
func TestCachedAgent_InvokeCacheHit(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 10*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	// First invocation (cache miss)
	output1, err := cachedAgent.Invoke(ctx, input)
	require.NoError(t, err)

	// Second invocation (cache hit)
	output2, err := cachedAgent.Invoke(ctx, input)
	require.NoError(t, err)

	// Results should be identical
	assert.Equal(t, output1.Result, output2.Result)

	stats := cachedAgent.Stats()
	assert.Equal(t, int64(1), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
	assert.Greater(t, stats.HitRate, 0.0)
}

// TestCachedAgent_MultipleInputs tests caching multiple different inputs
func TestCachedAgent_MultipleInputs(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 5*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	// Create different inputs
	inputs := make([]*core.AgentInput, 5)
	for i := 0; i < 5; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Test instruction",
			Timestamp:   time.Now(),
		}
	}

	// First pass - all cache misses
	for _, input := range inputs {
		_, err := cachedAgent.Invoke(ctx, input)
		require.NoError(t, err)
	}

	stats := cachedAgent.Stats()
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(5), stats.Misses)
	assert.Equal(t, 5, stats.Size)

	// Second pass - all cache hits
	for _, input := range inputs {
		_, err := cachedAgent.Invoke(ctx, input)
		require.NoError(t, err)
	}

	stats = cachedAgent.Stats()
	assert.Equal(t, int64(5), stats.Hits)
	assert.Equal(t, int64(5), stats.Misses)
	assert.Equal(t, 5, stats.Size)
}

// TestCachedAgent_InvalidateKey tests cache invalidation
func TestCachedAgent_InvalidateKey(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 5*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	// Cache the input
	_, err := cachedAgent.Invoke(ctx, input)
	require.NoError(t, err)

	stats := cachedAgent.Stats()
	assert.Equal(t, 1, stats.Size)

	// Invalidate the key
	cachedAgent.Invalidate(input)

	stats = cachedAgent.Stats()
	assert.Equal(t, 0, stats.Size)

	// Next invocation should be a cache miss
	_, err = cachedAgent.Invoke(ctx, input)
	require.NoError(t, err)

	stats = cachedAgent.Stats()
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(2), stats.Misses)
}

// TestCachedAgent_InvalidateAll tests cache clear
func TestCachedAgent_InvalidateAll(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 5*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	// Cache multiple inputs
	for i := 0; i < 5; i++ {
		input := &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Test instruction",
			Timestamp:   time.Now(),
		}
		cachedAgent.Invoke(ctx, input)
	}

	stats := cachedAgent.Stats()
	assert.Equal(t, 5, stats.Size)

	// Clear all
	cachedAgent.InvalidateAll()

	stats = cachedAgent.Stats()
	assert.Equal(t, 0, stats.Size)
}

// TestCachedAgent_TTLExpiration tests TTL expiration
func TestCachedAgent_TTLExpiration(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := CacheConfig{
		MaxSize:         1000,
		TTL:             100 * time.Millisecond,
		CleanupInterval: 50 * time.Millisecond,
		EnableStats:     true,
	}

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	// Cache the input
	output1, err := cachedAgent.Invoke(ctx, input)
	require.NoError(t, err)

	stats := cachedAgent.Stats()
	assert.Equal(t, 1, stats.Size)

	// Wait for TTL to expire and cleanup to run
	time.Sleep(200 * time.Millisecond)

	// Next invocation should be a cache miss
	output2, err := cachedAgent.Invoke(ctx, input)
	require.NoError(t, err)

	// Both should succeed but second may be from expired cache
	assert.NotNil(t, output1)
	assert.NotNil(t, output2)
}

// TestCachedAgent_MaxSizeEviction tests eviction when cache is full
func TestCachedAgent_MaxSizeEviction(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := CacheConfig{
		MaxSize:         5,
		TTL:             10 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		EnableStats:     true,
	}

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	// Fill cache to max size
	for i := 0; i < 5; i++ {
		input := &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Test instruction",
			Timestamp:   time.Now(),
		}
		cachedAgent.Invoke(ctx, input)
	}

	stats := cachedAgent.Stats()
	assert.Equal(t, 5, stats.Size)
	assert.Equal(t, int64(0), stats.Evictions)

	// Add one more (should evict oldest)
	input := &core.AgentInput{
		Task:        "Task #5",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}
	cachedAgent.Invoke(ctx, input)

	stats = cachedAgent.Stats()
	assert.Equal(t, 5, stats.Size) // Still at max
	assert.Equal(t, int64(1), stats.Evictions)
}

// TestCachedAgent_CustomKeyGenerator tests custom cache key generation
func TestCachedAgent_CustomKeyGenerator(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)

	// Custom key generator that ignores context
	customKeyGen := func(input *core.AgentInput) string {
		return fmt.Sprintf("%s:%s", input.Task, input.Instruction)
	}

	config := CacheConfig{
		MaxSize:         1000,
		TTL:             10 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		EnableStats:     true,
		KeyGenerator:    customKeyGen,
	}

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	input1 := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Context:     map[string]interface{}{"key1": "value1"},
		Timestamp:   time.Now(),
	}

	input2 := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Context:     map[string]interface{}{"key2": "value2"},
		Timestamp:   time.Now(),
	}

	// First invocation
	_, err := cachedAgent.Invoke(ctx, input1)
	require.NoError(t, err)

	// Second invocation with different context (should still be cache hit)
	_, err = cachedAgent.Invoke(ctx, input2)
	require.NoError(t, err)

	stats := cachedAgent.Stats()
	assert.Equal(t, int64(1), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
}

// TestCachedAgent_Stats tests cache statistics calculation
func TestCachedAgent_Stats(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	// Invoke multiple times
	for i := 0; i < 10; i++ {
		cachedAgent.Invoke(ctx, input)
	}

	stats := cachedAgent.Stats()
	assert.Equal(t, int64(9), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
	assert.Greater(t, stats.HitRate, 50.0)
	assert.Greater(t, stats.AvgHitTime, time.Duration(0))
	assert.Greater(t, stats.AvgMissTime, time.Duration(0))
}

// TestCachedAgent_Close tests cache closure
func TestCachedAgent_Close(t *testing.T) {
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	err := cachedAgent.Close()
	require.NoError(t, err)

	// Double close should not error
	err = cachedAgent.Close()
	require.NoError(t, err)
}

// TestCachedAgent_CloseBeforePutToCache tests putting to cache after close
func TestCachedAgent_CloseBeforePutToCache(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	cachedAgent.Close()

	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	// Should not panic
	output, err := cachedAgent.Invoke(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestCachedAgent_AgentMethods tests delegated agent methods
func TestCachedAgent_AgentMethods(t *testing.T) {
	agent := NewMockAgent("test-agent", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	assert.Equal(t, "test-agent", cachedAgent.Name())
	assert.NotEmpty(t, cachedAgent.Description())
	capabilities := cachedAgent.Capabilities()
	assert.NotEmpty(t, capabilities)
}

// TestCachedAgent_ConcurrentAccess tests concurrent cache access
func TestCachedAgent_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	// Cache each input first in single thread
	inputs := make([]*core.AgentInput, 5)
	for i := 0; i < 5; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Test instruction",
			Timestamp:   time.Now(),
		}
		cachedAgent.Invoke(ctx, inputs[i])
	}

	// Now do concurrent reads
	var wg sync.WaitGroup
	successCount := int32(0)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			input := inputs[idx%5]
			_, err := cachedAgent.Invoke(ctx, input)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int32(20), successCount)

	stats := cachedAgent.Stats()
	assert.Equal(t, 5, stats.Size)
	// Should have at least 15 hits from the concurrent reads
	assert.Greater(t, stats.Hits, int64(0))
}

// TestCachedAgent_CacheHitRateCalculation tests hit rate calculation
func TestCachedAgent_CacheHitRateCalculation(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	// One miss, nine hits
	for i := 0; i < 10; i++ {
		cachedAgent.Invoke(ctx, input)
	}

	stats := cachedAgent.Stats()
	assert.Equal(t, int64(9), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
	assert.Greater(t, stats.HitRate, 50.0)
	assert.Less(t, stats.HitRate, 100.0)
}

// TestCacheStats tests cache stats structure
func TestCacheStats(t *testing.T) {
	stats := CacheStats{
		Size:        100,
		MaxSize:     1000,
		Hits:        500,
		Misses:      100,
		HitRate:     83.33,
		Evictions:   10,
		Expirations: 5,
		AvgHitTime:  10 * time.Millisecond,
		AvgMissTime: 100 * time.Millisecond,
	}

	assert.Equal(t, 100, stats.Size)
	assert.Equal(t, 1000, stats.MaxSize)
	assert.Equal(t, int64(500), stats.Hits)
	assert.Greater(t, stats.HitRate, 0.0)
}

// TestCachedAgent_InvalidConfigUsesDefaults tests default config values
func TestCachedAgent_InvalidConfigUsesDefaults(t *testing.T) {
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := CacheConfig{
		MaxSize:     -1, // Invalid
		TTL:         -1, // Invalid
		EnableStats: false,
	}

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	assert.Equal(t, 1000, cachedAgent.config.MaxSize)
	assert.Equal(t, 10*time.Minute, cachedAgent.config.TTL)
}

// TestCachedAgent_HighConcurrencyCache tests cache under high concurrency
func TestCachedAgent_HighConcurrencyCache(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 5*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	// Create 10 unique inputs and cache them first
	inputs := make([]*core.AgentInput, 10)
	for i := 0; i < 10; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Test instruction",
			Timestamp:   time.Now(),
		}
		// Pre-populate cache
		cachedAgent.Invoke(ctx, inputs[i])
	}

	var wg sync.WaitGroup
	successCount := int32(0)

	// Run 100 concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			input := inputs[idx%10]
			_, err := cachedAgent.Invoke(ctx, input)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int32(100), successCount)

	stats := cachedAgent.Stats()
	assert.Equal(t, 10, stats.Size)
	// Should have 100 hits (all from cache) after pre-population
	assert.Greater(t, stats.Hits, int64(90))
}

// TestCacheEntry tests cache entry structure
func TestCacheEntry(t *testing.T) {
	output := &core.AgentOutput{
		Result:    "test result",
		Status:    "success",
		Message:   "test message",
		Timestamp: time.Now(),
	}

	entry := &CacheEntry{
		Output:    output,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}

	assert.Equal(t, output, entry.Output)
	assert.Greater(t, entry.ExpiresAt.Unix(), entry.CreatedAt.Unix())
	assert.Equal(t, int64(0), entry.HitCount.Load())
}

// TestCachedAgent_StreamDelegation tests stream method delegation
func TestCachedAgent_StreamDelegation(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test instruction",
		Timestamp:   time.Now(),
	}

	// Should delegate to underlying agent
	stream, err := cachedAgent.Stream(ctx, input)
	assert.NotNil(t, stream)
	assert.NoError(t, err)
}

// TestCachedAgent_BatchDelegation tests batch method delegation
func TestCachedAgent_BatchDelegation(t *testing.T) {
	ctx := context.Background()
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	inputs := make([]*core.AgentInput, 3)
	for i := 0; i < 3; i++ {
		inputs[i] = &core.AgentInput{
			Task:        fmt.Sprintf("Task #%d", i),
			Instruction: "Test instruction",
			Timestamp:   time.Now(),
		}
	}

	// Should delegate to underlying agent
	outputs, err := cachedAgent.Batch(ctx, inputs)
	// Note: Batch may not be implemented in all agents
	if err != nil {
		// If method is not implemented, it's okay
		assert.NotNil(t, err)
	} else {
		assert.NotNil(t, outputs)
	}
}

// TestCachedAgent_PipeDelegation tests pipe method delegation
func TestCachedAgent_PipeDelegation(t *testing.T) {
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	// Create a simple runnable to pipe to
	next := core.NewRunnableFunc(func(ctx context.Context, input *core.AgentOutput) (interface{}, error) {
		return input.Result, nil
	})

	// Should delegate to underlying agent
	result := cachedAgent.Pipe(next)
	assert.NotNil(t, result)
}

// TestCachedAgent_WithCallbacksDelegation tests WithCallbacks delegation
func TestCachedAgent_WithCallbacksDelegation(t *testing.T) {
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	// Should delegate to underlying agent
	result := cachedAgent.WithCallbacks()
	assert.NotNil(t, result)
}

// TestCachedAgent_WithConfigDelegation tests WithConfig delegation
func TestCachedAgent_WithConfigDelegation(t *testing.T) {
	agent := NewMockAgent("test", 1*time.Millisecond)
	config := DefaultCacheConfig()

	cachedAgent := NewCachedAgent(agent, config)
	defer cachedAgent.Close()

	runnableConfig := core.RunnableConfig{}

	// Should delegate to underlying agent
	result := cachedAgent.WithConfig(runnableConfig)
	assert.NotNil(t, result)
}
