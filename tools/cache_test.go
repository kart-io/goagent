package tools

import (
	"context"
	"testing"
	"time"
)

func TestMemoryToolCache(t *testing.T) {
	ctx := context.Background()

	config := MemoryCacheConfig{
		Capacity:        10,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Second,
	}

	cache := NewMemoryToolCache(config)

	t.Run("Set and Get", func(t *testing.T) {
		key := "test_key"
		output := &ToolOutput{
			Result:  "test result",
			Success: true,
		}

		err := cache.Set(ctx, key, output, 1*time.Minute)
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		retrieved, found := cache.Get(ctx, key)
		if !found {
			t.Fatal("Expected to find cached item")
		}

		if retrieved.Result != output.Result {
			t.Errorf("Expected result %v, got %v", output.Result, retrieved.Result)
		}
	})

	t.Run("Cache miss", func(t *testing.T) {
		_, found := cache.Get(ctx, "nonexistent_key")
		if found {
			t.Error("Expected cache miss for nonexistent key")
		}
	})

	t.Run("TTL expiration", func(t *testing.T) {
		key := "expiring_key"
		output := &ToolOutput{Result: "data", Success: true}

		err := cache.Set(ctx, key, output, 100*time.Millisecond)
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		// 立即获取应该成功
		_, found := cache.Get(ctx, key)
		if !found {
			t.Error("Expected to find item immediately after set")
		}

		// 等待过期
		time.Sleep(200 * time.Millisecond)

		_, found = cache.Get(ctx, key)
		if found {
			t.Error("Expected item to be expired")
		}
	})

	t.Run("LRU eviction", func(t *testing.T) {
		smallCache := NewMemoryToolCache(MemoryCacheConfig{
			Capacity:        2,
			DefaultTTL:      5 * time.Minute,
			CleanupInterval: 10 * time.Minute,
		})

		// 添加 3 个项，应该淘汰最老的
		_ = smallCache.Set(ctx, "key1", &ToolOutput{Result: "1", Success: true}, 5*time.Minute)
		_ = smallCache.Set(ctx, "key2", &ToolOutput{Result: "2", Success: true}, 5*time.Minute)
		_ = smallCache.Set(ctx, "key3", &ToolOutput{Result: "3", Success: true}, 5*time.Minute)

		// key1 应该被淘汰
		_, found := smallCache.Get(ctx, "key1")
		if found {
			t.Error("Expected key1 to be evicted")
		}

		// key2 和 key3 应该存在
		_, found = smallCache.Get(ctx, "key2")
		if !found {
			t.Error("Expected key2 to exist")
		}

		_, found = smallCache.Get(ctx, "key3")
		if !found {
			t.Error("Expected key3 to exist")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		key := "delete_test"
		output := &ToolOutput{Result: "data", Success: true}

		_ = cache.Set(ctx, key, output, 1*time.Minute)

		err := cache.Delete(ctx, key)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		_, found := cache.Get(ctx, key)
		if found {
			t.Error("Expected item to be deleted")
		}
	})

	t.Run("Clear", func(t *testing.T) {
		_ = cache.Set(ctx, "key1", &ToolOutput{Result: "1", Success: true}, 1*time.Minute)
		_ = cache.Set(ctx, "key2", &ToolOutput{Result: "2", Success: true}, 1*time.Minute)

		err := cache.Clear()
		if err != nil {
			t.Fatalf("Clear failed: %v", err)
		}

		if cache.Size() != 0 {
			t.Errorf("Expected size 0 after clear, got %d", cache.Size())
		}
	})

	t.Run("Statistics", func(t *testing.T) {
		testCache := NewMemoryToolCache(config)

		// 测试命中
		key := "stats_test"
		_ = testCache.Set(ctx, key, &ToolOutput{Result: "data", Success: true}, 1*time.Minute)
		testCache.Get(ctx, key)

		// 测试未命中
		testCache.Get(ctx, "nonexistent")

		stats := testCache.GetStats()

		if stats.Hits.Load() != 1 {
			t.Errorf("Expected 1 hit, got %d", stats.Hits.Load())
		}

		if stats.Misses.Load() != 1 {
			t.Errorf("Expected 1 miss, got %d", stats.Misses.Load())
		}

		hitRate := stats.HitRate()
		expectedRate := 0.5
		if hitRate < expectedRate-0.01 || hitRate > expectedRate+0.01 {
			t.Errorf("Expected hit rate %.2f, got %.2f", expectedRate, hitRate)
		}
	})
}

func TestCachedTool(t *testing.T) {
	ctx := context.Background()

	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        10,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})

	// 创建模拟工具
	executionCount := 0
	baseTool := NewBaseTool(
		"test_tool",
		"A test tool",
		`{"type": "object"}`,
		func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			executionCount++
			return &ToolOutput{
				Result:  "result",
				Success: true,
			}, nil
		},
	)

	cachedTool := NewCachedTool(baseTool, cache, 1*time.Minute)

	t.Run("Cache hit on second call", func(t *testing.T) {
		input := &ToolInput{
			Args: map[string]interface{}{"param": "value"},
		}

		// 第一次调用
		_, err := cachedTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("First invocation failed: %v", err)
		}

		if executionCount != 1 {
			t.Errorf("Expected execution count 1, got %d", executionCount)
		}

		// 第二次调用（应该命中缓存）
		_, err = cachedTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("Second invocation failed: %v", err)
		}

		if executionCount != 1 {
			t.Errorf("Expected execution count still 1 (cache hit), got %d", executionCount)
		}
	})

	t.Run("Different inputs result in cache miss", func(t *testing.T) {
		executionCount = 0

		input1 := &ToolInput{Args: map[string]interface{}{"param": "value1"}}
		input2 := &ToolInput{Args: map[string]interface{}{"param": "value2"}}

		_, _ = cachedTool.Invoke(ctx, input1)
		_, _ = cachedTool.Invoke(ctx, input2)

		if executionCount != 2 {
			t.Errorf("Expected 2 executions for different inputs, got %d", executionCount)
		}
	})
}

func BenchmarkMemoryToolCache_Set(b *testing.B) {
	ctx := context.Background()
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        1000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})

	output := &ToolOutput{Result: "benchmark data", Success: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := string(rune('A' + (i % 26)))
		_ = cache.Set(ctx, key, output, 1*time.Minute)
	}
}

func BenchmarkMemoryToolCache_Get(b *testing.B) {
	ctx := context.Background()
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        1000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})

	// 预填充缓存
	output := &ToolOutput{Result: "benchmark data", Success: true}
	for i := 0; i < 100; i++ {
		key := string(rune('A' + (i % 26)))
		_ = cache.Set(ctx, key, output, 1*time.Minute)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := string(rune('A' + (i % 26)))
		cache.Get(ctx, key)
	}
}

// BenchmarkCacheKeyGeneration tests the optimized cache key generation
// which uses direct hashing instead of JSON marshaling.
func BenchmarkCacheKeyGeneration(b *testing.B) {
	baseTool := NewBaseTool(
		"benchmark_tool",
		"A tool for benchmarking",
		`{"type": "object"}`,
		func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			return &ToolOutput{Result: "result", Success: true}, nil
		},
	)

	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})

	cachedTool := NewCachedTool(baseTool, cache, 1*time.Minute)

	// Test input with various types
	input := &ToolInput{
		Args: map[string]interface{}{
			"string_param":  "test_value",
			"int_param":     42,
			"float_param":   3.14,
			"bool_param":    true,
			"array_param":   []interface{}{"a", "b", "c"},
			"nested_param":  map[string]interface{}{"key": "value"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cachedTool.generateCacheKey(input)
	}
}

// BenchmarkCacheKeyGenerationSimple tests cache key generation with simple input.
func BenchmarkCacheKeyGenerationSimple(b *testing.B) {
	baseTool := NewBaseTool(
		"simple_tool",
		"A simple tool",
		`{"type": "object"}`,
		func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			return &ToolOutput{Result: "result", Success: true}, nil
		},
	)

	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})

	cachedTool := NewCachedTool(baseTool, cache, 1*time.Minute)

	input := &ToolInput{
		Args: map[string]interface{}{
			"query": "test query",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cachedTool.generateCacheKey(input)
	}
}
