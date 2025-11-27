package tools

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShardedCache_CircularDependency 测试循环依赖不会导致栈溢出
func TestShardedCache_CircularDependency(t *testing.T) {
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      8,
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 设置循环依赖: A -> B -> C -> A
	cache.AddDependency("toolB", "toolA")
	cache.AddDependency("toolC", "toolB")
	cache.AddDependency("toolA", "toolC")

	// 为每个工具添加缓存条目（使用 extractToolNameFromKey 识别的格式）
	err := cache.Set(ctx, "toolA:key1", &interfaces.ToolOutput{Result: "a1"}, 5*time.Minute)
	require.NoError(t, err)

	err = cache.Set(ctx, "toolB:key1", &interfaces.ToolOutput{Result: "b1"}, 5*time.Minute)
	require.NoError(t, err)

	err = cache.Set(ctx, "toolC:key1", &interfaces.ToolOutput{Result: "c1"}, 5*time.Minute)
	require.NoError(t, err)

	// 验证初始状态
	assert.Equal(t, 3, cache.Size())

	// 触发 toolA 的失效，应该级联失效 B 和 C，但不会导致栈溢出
	count, err := cache.InvalidateByTool(ctx, "toolA")
	require.NoError(t, err)

	// 应该失效所有3个工具的缓存
	assert.Equal(t, 3, count, "All 3 tools should be invalidated")
	assert.Equal(t, 0, cache.Size(), "Cache should be empty after invalidation")
}

// TestShardedCache_ComplexCircularDependency 测试复杂的循环依赖场景
func TestShardedCache_ComplexCircularDependency(t *testing.T) {
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      8,
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 设置复杂的依赖图:
	// A -> B -> D
	// A -> C -> D
	// D -> A (形成环)
	cache.AddDependency("toolB", "toolA")
	cache.AddDependency("toolC", "toolA")
	cache.AddDependency("toolD", "toolB")
	cache.AddDependency("toolD", "toolC")
	cache.AddDependency("toolA", "toolD")

	// 为每个工具添加多个缓存条目
	tools := []string{"toolA", "toolB", "toolC", "toolD"}
	for _, tool := range tools {
		for i := 0; i < 3; i++ {
			key := tool + ":key" + string(rune('0'+i))
			err := cache.Set(ctx, key, &interfaces.ToolOutput{Result: key}, 5*time.Minute)
			require.NoError(t, err)
		}
	}

	// 验证初始状态
	initialSize := cache.Size()
	assert.Equal(t, 12, initialSize, "Should have 12 cache entries")

	// 触发 toolA 的失效，应该级联失效所有工具，但不会栈溢出
	count, err := cache.InvalidateByTool(ctx, "toolA")
	require.NoError(t, err)

	// 应该失效所有工具的所有缓存（包括 A 自己）
	assert.Equal(t, 12, count, "All 12 entries should be invalidated")
	assert.Equal(t, 0, cache.Size(), "Cache should be empty after invalidation")
}

// TestShardedCache_SelfDependency 测试自依赖不会导致无限递归
func TestShardedCache_SelfDependency(t *testing.T) {
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      8,
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 设置自依赖: A -> A
	cache.AddDependency("toolA", "toolA")

	// 添加缓存条目
	err := cache.Set(ctx, "toolA:key1", &interfaces.ToolOutput{Result: "a1"}, 5*time.Minute)
	require.NoError(t, err)

	// 触发失效，不应该导致无限递归
	count, err := cache.InvalidateByTool(ctx, "toolA")
	require.NoError(t, err)

	// 应该只失效1次
	assert.Equal(t, 1, count, "Should invalidate once")
	assert.Equal(t, 0, cache.Size(), "Cache should be empty")
}

// TestShardedCache_InvalidateByPatternWithCircularDeps 测试 InvalidateByPattern 的循环依赖处理
func TestShardedCache_InvalidateByPatternWithCircularDeps(t *testing.T) {
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      8,
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 设置循环依赖
	cache.AddDependency("toolB", "toolA")
	cache.AddDependency("toolA", "toolB")

	// 添加缓存条目
	err := cache.Set(ctx, "toolA:user123", &interfaces.ToolOutput{Result: "a1"}, 5*time.Minute)
	require.NoError(t, err)

	err = cache.Set(ctx, "toolB:user456", &interfaces.ToolOutput{Result: "b1"}, 5*time.Minute)
	require.NoError(t, err)

	// 使用正则表达式失效，应该触发级联失效但不会栈溢出
	count, err := cache.InvalidateByPattern(ctx, "^toolA:")
	require.NoError(t, err)

	// 应该失效两个工具
	assert.Equal(t, 2, count, "Should invalidate both tools due to circular dependency")
	assert.Equal(t, 0, cache.Size(), "Cache should be empty")
}

// TestShardedCache_NoDependency 测试没有依赖关系时的正常行为
func TestShardedCache_NoDependency(t *testing.T) {
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      8,
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 添加缓存条目，不设置依赖
	err := cache.Set(ctx, "toolA:key1", &interfaces.ToolOutput{Result: "a"}, 5*time.Minute)
	require.NoError(t, err)

	err = cache.Set(ctx, "toolB:key1", &interfaces.ToolOutput{Result: "b"}, 5*time.Minute)
	require.NoError(t, err)

	// 失效 toolA 不应该影响 toolB
	count, err := cache.InvalidateByTool(ctx, "toolA")
	require.NoError(t, err)

	assert.Equal(t, 1, count, "Should only invalidate toolA")
	assert.Equal(t, 1, cache.Size(), "toolB should still be cached")

	// 验证 toolB 仍然存在
	_, exists := cache.Get(ctx, "toolB:key1")
	assert.True(t, exists, "toolB cache should still exist")
}

// BenchmarkCircularDependencyInvalidation 基准测试循环依赖失效的性能
func BenchmarkCircularDependencyInvalidation(b *testing.B) {
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      32,
		Capacity:        10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 设置循环依赖链
	for i := 0; i < 10; i++ {
		tool := "tool" + string(rune('A'+i))
		nextTool := "tool" + string(rune('A'+(i+1)%10))
		cache.AddDependency(nextTool, tool)
	}

	// 为每个工具添加缓存条目
	addCacheEntries := func() {
		for i := 0; i < 10; i++ {
			tool := "tool" + string(rune('A'+i))
			for j := 0; j < 10; j++ {
				key := tool + ":key" + string(rune('0'+j))
				_ = cache.Set(ctx, key, &interfaces.ToolOutput{Result: key}, 5*time.Minute)
			}
		}
	}

	addCacheEntries()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// 触发失效
		_, _ = cache.InvalidateByTool(ctx, "toolA")

		// 重新填充缓存以便下次基准测试
		if i < b.N-1 {
			addCacheEntries()
		}
	}
}
