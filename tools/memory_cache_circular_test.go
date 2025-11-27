package tools

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMemoryCache_CircularDependency 测试 MemoryToolCache 的循环依赖保护
func TestMemoryCache_CircularDependency(t *testing.T) {
	cache := NewMemoryToolCache(MemoryCacheConfig{
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

	// 为每个工具添加缓存条目
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

// TestMemoryCache_ComplexCircularDependency 测试复杂的循环依赖场景
func TestMemoryCache_ComplexCircularDependency(t *testing.T) {
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        200,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 设置复杂的依赖图
	cache.AddDependency("toolB", "toolA")
	cache.AddDependency("toolC", "toolA")
	cache.AddDependency("toolD", "toolB")
	cache.AddDependency("toolD", "toolC")
	cache.AddDependency("toolA", "toolD")

	// 为每个工具添加多个缓存条目
	tools := []string{"toolA", "toolB", "toolC", "toolD"}
	for _, tool := range tools {
		for i := 0; i < 3; i++ {
			key := fmt.Sprintf("%s:key%d", tool, i)
			err := cache.Set(ctx, key, &interfaces.ToolOutput{Result: key}, 5*time.Minute)
			require.NoError(t, err)
		}
	}

	// 验证初始状态
	initialSize := cache.Size()
	assert.Equal(t, 12, initialSize, "Should have 12 cache entries")

	// 触发 toolA 的失效
	count, err := cache.InvalidateByTool(ctx, "toolA")
	require.NoError(t, err)

	// 应该失效所有工具
	assert.Equal(t, 12, count, "All 12 entries should be invalidated")
	assert.Equal(t, 0, cache.Size(), "Cache should be empty after invalidation")
}

// TestMemoryCache_SelfDependency 测试自依赖不会导致无限递归
func TestMemoryCache_SelfDependency(t *testing.T) {
	cache := NewMemoryToolCache(MemoryCacheConfig{
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

// TestMemoryCache_InvalidateByPatternWithCircularDeps 测试 Pattern 失效的循环依赖处理
func TestMemoryCache_InvalidateByPatternWithCircularDeps(t *testing.T) {
	cache := NewMemoryToolCache(MemoryCacheConfig{
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

// TestMemoryCache_DeepCircularChain 测试深度循环依赖链
func TestMemoryCache_DeepCircularChain(t *testing.T) {
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        500,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 创建一个包含 30 个工具的循环依赖链
	chainLength := 30
	for i := 0; i < chainLength; i++ {
		current := fmt.Sprintf("tool%d", i)
		next := fmt.Sprintf("tool%d", (i+1)%chainLength)
		cache.AddDependency(next, current)
	}

	// 为每个工具添加缓存条目
	for i := 0; i < chainLength; i++ {
		tool := fmt.Sprintf("tool%d", i)
		for j := 0; j < 3; j++ {
			key := fmt.Sprintf("%s:key%d", tool, j)
			err := cache.Set(ctx, key, &interfaces.ToolOutput{Result: key}, 5*time.Minute)
			require.NoError(t, err)
		}
	}

	// 验证初始状态
	initialSize := cache.Size()
	assert.Equal(t, chainLength*3, initialSize, "Should have all cache entries")

	// 触发任意一个工具的失效
	startTime := time.Now()
	count, err := cache.InvalidateByTool(ctx, "tool0")
	duration := time.Since(startTime)

	require.NoError(t, err)
	assert.Equal(t, chainLength*3, count, "All entries should be invalidated")
	assert.Equal(t, 0, cache.Size(), "Cache should be empty")

	// 性能验证
	assert.Less(t, duration.Milliseconds(), int64(100),
		"Deep circular chain invalidation should complete quickly")

	t.Logf("Invalidated %d entries in %d tools in %v", count, chainLength, duration)
}

// TestMemoryCache_MultipleCircularGroups 测试多个独立的循环依赖组
func TestMemoryCache_MultipleCircularGroups(t *testing.T) {
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        200,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 创建3个独立的循环依赖组
	cache.AddDependency("A2", "A1")
	cache.AddDependency("A3", "A2")
	cache.AddDependency("A1", "A3")

	cache.AddDependency("B2", "B1")
	cache.AddDependency("B3", "B2")
	cache.AddDependency("B1", "B3")

	cache.AddDependency("C2", "C1")
	cache.AddDependency("C3", "C2")
	cache.AddDependency("C1", "C3")

	// 为每个工具添加缓存条目
	groups := [][]string{
		{"A1", "A2", "A3"},
		{"B1", "B2", "B3"},
		{"C1", "C2", "C3"},
	}

	for _, group := range groups {
		for _, tool := range group {
			for i := 0; i < 3; i++ {
				key := fmt.Sprintf("%s:key%d", tool, i)
				err := cache.Set(ctx, key, &interfaces.ToolOutput{Result: key}, 5*time.Minute)
				require.NoError(t, err)
			}
		}
	}

	// 验证初始状态
	assert.Equal(t, 27, cache.Size())

	// 失效组1
	count, err := cache.InvalidateByTool(ctx, "A1")
	require.NoError(t, err)
	assert.Equal(t, 9, count, "Should invalidate 9 entries from group 1")
	assert.Equal(t, 18, cache.Size(), "Groups 2 and 3 should remain")

	// 失效组2
	count, err = cache.InvalidateByTool(ctx, "B1")
	require.NoError(t, err)
	assert.Equal(t, 9, count)
	assert.Equal(t, 9, cache.Size())

	// 失效组3
	count, err = cache.InvalidateByTool(ctx, "C1")
	require.NoError(t, err)
	assert.Equal(t, 9, count)
	assert.Equal(t, 0, cache.Size())
}
