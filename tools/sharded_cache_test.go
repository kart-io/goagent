package tools

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShardedToolCache_BasicOperations(t *testing.T) {
	ctx := context.Background()
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      8,
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	t.Run("Set and Get", func(t *testing.T) {
		output := &ToolOutput{Result: "test result", Success: true}
		err := cache.Set(ctx, "key1", output, 5*time.Minute)
		require.NoError(t, err)

		retrieved, found := cache.Get(ctx, "key1")
		assert.True(t, found)
		assert.Equal(t, output.Result, retrieved.Result)
		assert.Equal(t, output.Success, retrieved.Success)
	})

	t.Run("Cache miss", func(t *testing.T) {
		_, found := cache.Get(ctx, "nonexistent")
		assert.False(t, found)
	})

	t.Run("Delete", func(t *testing.T) {
		output := &ToolOutput{Result: "to delete", Success: true}
		_ = cache.Set(ctx, "key2", output, 5*time.Minute)

		err := cache.Delete(ctx, "key2")
		require.NoError(t, err)

		_, found := cache.Get(ctx, "key2")
		assert.False(t, found)
	})

	t.Run("Clear", func(t *testing.T) {
		// Add multiple entries
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("clear_key_%d", i)
			output := &ToolOutput{Result: fmt.Sprintf("result_%d", i), Success: true}
			_ = cache.Set(ctx, key, output, 5*time.Minute)
		}

		assert.True(t, cache.Size() >= 10)

		err := cache.Clear()
		require.NoError(t, err)
		assert.Equal(t, 0, cache.Size())
	})
}

func TestShardedToolCache_Invalidation(t *testing.T) {
	ctx := context.Background()
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      8,
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	t.Run("InvalidateByPattern", func(t *testing.T) {
		// Populate cache
		_ = cache.Set(ctx, "search_tool:hash1", &ToolOutput{Result: "1", Success: true}, 5*time.Minute)
		_ = cache.Set(ctx, "search_tool:hash2", &ToolOutput{Result: "2", Success: true}, 5*time.Minute)
		_ = cache.Set(ctx, "calc_tool:hash3", &ToolOutput{Result: "3", Success: true}, 5*time.Minute)

		assert.Equal(t, 3, cache.Size())

		count, err := cache.InvalidateByPattern(ctx, "^search_tool:.*")
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		assert.Equal(t, 1, cache.Size())

		// calc_tool should still exist
		_, found := cache.Get(ctx, "calc_tool:hash3")
		assert.True(t, found)
	})

	t.Run("InvalidateByTool", func(t *testing.T) {
		_ = cache.Clear()

		// Populate cache
		_ = cache.Set(ctx, "tool1:hash1", &ToolOutput{Result: "1", Success: true}, 5*time.Minute)
		_ = cache.Set(ctx, "tool1:hash2", &ToolOutput{Result: "2", Success: true}, 5*time.Minute)
		_ = cache.Set(ctx, "tool2:hash3", &ToolOutput{Result: "3", Success: true}, 5*time.Minute)

		count, err := cache.InvalidateByTool(ctx, "tool1")
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		assert.Equal(t, 1, cache.Size())
	})
}

func TestShardedToolCache_Concurrency(t *testing.T) {
	ctx := context.Background()
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      32,
		Capacity:        1000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	const numGoroutines = 100
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	var setOps, getOps atomic.Int64
	var hits, misses atomic.Int64

	// Concurrent writes and reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key_%d_%d", id, j)
				output := &ToolOutput{Result: fmt.Sprintf("result_%d_%d", id, j), Success: true}

				// Set operation
				if err := cache.Set(ctx, key, output, 5*time.Minute); err == nil {
					setOps.Add(1)
				}

				// Get operation
				if retrieved, found := cache.Get(ctx, key); found {
					hits.Add(1)
					assert.Equal(t, output.Result, retrieved.Result)
				} else {
					misses.Add(1)
				}
				getOps.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// Verify operations completed
	assert.Equal(t, int64(numGoroutines*opsPerGoroutine), setOps.Load())
	assert.Equal(t, int64(numGoroutines*opsPerGoroutine), getOps.Load())
	assert.Greater(t, hits.Load(), int64(0))

	t.Logf("Operations: Sets=%d, Gets=%d, Hits=%d, Misses=%d",
		setOps.Load(), getOps.Load(), hits.Load(), misses.Load())
}

func TestShardedToolCache_LRUEviction(t *testing.T) {
	ctx := context.Background()
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      4,
		Capacity:        20, // Small capacity to trigger eviction
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	// Fill cache beyond capacity
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("key_%d", i)
		output := &ToolOutput{Result: fmt.Sprintf("result_%d", i), Success: true}
		_ = cache.Set(ctx, key, output, 5*time.Minute)
	}

	// Cache should be at capacity (not exceed it)
	assert.LessOrEqual(t, cache.Size(), 20)

	// Recent items should be in cache
	for i := 20; i < 30; i++ {
		key := fmt.Sprintf("key_%d", i)
		_, found := cache.Get(ctx, key)
		if !found {
			t.Logf("Key %s not found (might be in different shard)", key)
		}
	}
}

func TestShardedToolCache_TTLExpiration(t *testing.T) {
	ctx := context.Background()
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      4,
		Capacity:        100,
		DefaultTTL:      100 * time.Millisecond,
		CleanupInterval: 50 * time.Millisecond,
	})
	defer cache.Close()

	// Add item with short TTL
	output := &ToolOutput{Result: "expiring", Success: true}
	_ = cache.Set(ctx, "expiring_key", output, 100*time.Millisecond)

	// Should exist initially
	_, found := cache.Get(ctx, "expiring_key")
	assert.True(t, found)

	// Wait for expiration
	time.Sleep(200 * time.Millisecond)

	// Should be expired
	_, found = cache.Get(ctx, "expiring_key")
	assert.False(t, found)
}

func TestShardedToolCache_Dependencies(t *testing.T) {
	ctx := context.Background()
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      8,
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	// Set up dependencies: tool2 depends on tool1
	cache.AddDependency("tool2", "tool1")

	// Populate cache
	_ = cache.Set(ctx, "tool1:hash1", &ToolOutput{Result: "1", Success: true}, 5*time.Minute)
	_ = cache.Set(ctx, "tool2:hash2", &ToolOutput{Result: "2", Success: true}, 5*time.Minute)

	assert.Equal(t, 2, cache.Size())

	// Invalidate tool1, should cascade to tool2
	count, err := cache.InvalidateByTool(ctx, "tool1")
	require.NoError(t, err)
	assert.Equal(t, 2, count) // Both tools should be invalidated
	assert.Equal(t, 0, cache.Size())
}

// BenchmarkShardedCache_ConcurrentAccess benchmarks concurrent access
func BenchmarkShardedCache_ConcurrentAccess(b *testing.B) {
	ctx := context.Background()
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      32,
		Capacity:        10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key_%d", i)
		output := &ToolOutput{Result: fmt.Sprintf("result_%d", i), Success: true}
		_ = cache.Set(ctx, key, output, 5*time.Minute)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key_%d", i%1000)
			if i%2 == 0 {
				cache.Get(ctx, key)
			} else {
				output := &ToolOutput{Result: fmt.Sprintf("new_%d", i), Success: true}
				cache.Set(ctx, key, output, 5*time.Minute)
			}
			i++
		}
	})
}

// BenchmarkComparison compares sharded vs non-sharded cache
func BenchmarkComparison(b *testing.B) {
	ctx := context.Background()

	b.Run("Normal", func(b *testing.B) {
		cache := NewMemoryToolCache(MemoryCacheConfig{
			Capacity:        10000,
			DefaultTTL:      5 * time.Minute,
			CleanupInterval: 10 * time.Minute,
		})
		defer cache.Close()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("key_%d", i%1000)
				output := &ToolOutput{Result: fmt.Sprintf("result_%d", i), Success: true}
				cache.Set(ctx, key, output, 5*time.Minute)
				cache.Get(ctx, key)
				i++
			}
		})
	})

	b.Run("Sharded", func(b *testing.B) {
		cache := NewShardedToolCache(ShardedCacheConfig{
			ShardCount:      32,
			Capacity:        10000,
			DefaultTTL:      5 * time.Minute,
			CleanupInterval: 10 * time.Minute,
		})
		defer cache.Close()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("key_%d", i%1000)
				output := &ToolOutput{Result: fmt.Sprintf("result_%d", i), Success: true}
				cache.Set(ctx, key, output, 5*time.Minute)
				cache.Get(ctx, key)
				i++
			}
		})
	})
}