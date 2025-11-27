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

// TestShardedCache_DeepCircularChain 测试深度循环依赖链（压力测试）
func TestShardedCache_DeepCircularChain(t *testing.T) {
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      16,
		Capacity:        1000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 创建一个包含 50 个工具的循环依赖链
	// tool0 -> tool1 -> tool2 -> ... -> tool49 -> tool0
	chainLength := 50
	for i := 0; i < chainLength; i++ {
		current := fmt.Sprintf("tool%d", i)
		next := fmt.Sprintf("tool%d", (i+1)%chainLength)
		cache.AddDependency(next, current)
	}

	// 为每个工具添加缓存条目
	for i := 0; i < chainLength; i++ {
		tool := fmt.Sprintf("tool%d", i)
		for j := 0; j < 5; j++ {
			key := fmt.Sprintf("%s:key%d", tool, j)
			err := cache.Set(ctx, key, &interfaces.ToolOutput{Result: key}, 5*time.Minute)
			require.NoError(t, err)
		}
	}

	// 验证初始状态
	initialSize := cache.Size()
	assert.Equal(t, chainLength*5, initialSize, "Should have all cache entries")

	// 触发任意一个工具的失效，应该级联失效所有工具
	startTime := time.Now()
	count, err := cache.InvalidateByTool(ctx, "tool0")
	duration := time.Since(startTime)

	require.NoError(t, err)
	assert.Equal(t, chainLength*5, count, "All entries should be invalidated")
	assert.Equal(t, 0, cache.Size(), "Cache should be empty")

	// 性能验证：即使是 50 个工具的循环链，也应该在合理时间内完成（< 100ms）
	assert.Less(t, duration.Milliseconds(), int64(100),
		"Deep circular chain invalidation should complete quickly")

	t.Logf("Invalidated %d entries in %d tools in %v", count, chainLength, duration)
}

// TestShardedCache_MultipleCircularGroups 测试多个独立的循环依赖组
func TestShardedCache_MultipleCircularGroups(t *testing.T) {
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      16,
		Capacity:        500,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 创建3个独立的循环依赖组
	// 组1: A1 -> A2 -> A3 -> A1
	cache.AddDependency("A2", "A1")
	cache.AddDependency("A3", "A2")
	cache.AddDependency("A1", "A3")

	// 组2: B1 -> B2 -> B3 -> B1
	cache.AddDependency("B2", "B1")
	cache.AddDependency("B3", "B2")
	cache.AddDependency("B1", "B3")

	// 组3: C1 -> C2 -> C3 -> C1
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
	assert.Equal(t, 27, cache.Size(), "Should have 27 cache entries (3 groups * 3 tools * 3 entries)")

	// 失效组1的一个工具，应该只影响组1
	count, err := cache.InvalidateByTool(ctx, "A1")
	require.NoError(t, err)
	assert.Equal(t, 9, count, "Should invalidate 9 entries from group 1")
	assert.Equal(t, 18, cache.Size(), "Groups 2 and 3 should remain")

	// 失效组2的一个工具
	count, err = cache.InvalidateByTool(ctx, "B1")
	require.NoError(t, err)
	assert.Equal(t, 9, count, "Should invalidate 9 entries from group 2")
	assert.Equal(t, 9, cache.Size(), "Only group 3 should remain")

	// 失效组3的一个工具
	count, err = cache.InvalidateByTool(ctx, "C1")
	require.NoError(t, err)
	assert.Equal(t, 9, count, "Should invalidate 9 entries from group 3")
	assert.Equal(t, 0, cache.Size(), "Cache should be empty")
}

// TestShardedCache_ComplexDependencyGraph 测试复杂的非树形依赖图
func TestShardedCache_ComplexDependencyGraph(t *testing.T) {
	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      16,
		Capacity:        500,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 创建一个复杂的依赖图（菱形 + 循环）
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	//     |
	//     E
	//     |
	//     A (形成大环)
	cache.AddDependency("B", "A")
	cache.AddDependency("C", "A")
	cache.AddDependency("D", "B")
	cache.AddDependency("D", "C")
	cache.AddDependency("E", "D")
	cache.AddDependency("A", "E") // 环

	// 为每个工具添加缓存条目
	tools := []string{"A", "B", "C", "D", "E"}
	for _, tool := range tools {
		for i := 0; i < 4; i++ {
			key := fmt.Sprintf("%s:key%d", tool, i)
			err := cache.Set(ctx, key, &interfaces.ToolOutput{Result: key}, 5*time.Minute)
			require.NoError(t, err)
		}
	}

	// 验证初始状态
	assert.Equal(t, 20, cache.Size(), "Should have 20 cache entries")

	// 触发 A 的失效，应该级联失效所有工具
	count, err := cache.InvalidateByTool(ctx, "A")
	require.NoError(t, err)

	// 应该失效所有工具
	assert.Equal(t, 20, count, "All tools should be invalidated")
	assert.Equal(t, 0, cache.Size(), "Cache should be empty")
}

// TestShardedCache_NoStackOverflow 压力测试：确保不会栈溢出
func TestShardedCache_NoStackOverflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stack overflow test in short mode")
	}

	cache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      32,
		Capacity:        5000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	ctx := context.Background()

	// 创建一个包含 100 个工具的密集依赖图
	// 每个工具依赖于前5个工具（如果存在）
	numTools := 100
	for i := 0; i < numTools; i++ {
		current := fmt.Sprintf("tool%d", i)

		// 依赖于前5个工具
		for j := 1; j <= 5 && i-j >= 0; j++ {
			dependsOn := fmt.Sprintf("tool%d", i-j)
			cache.AddDependency(current, dependsOn)
		}

		// 添加一些环
		if i >= 10 {
			// 每10个工具形成一个小环
			if i%10 == 0 {
				prev := fmt.Sprintf("tool%d", i-10)
				cache.AddDependency(prev, current)
			}
		}
	}

	// 为每个工具添加少量缓存条目
	for i := 0; i < numTools; i++ {
		tool := fmt.Sprintf("tool%d", i)
		key := fmt.Sprintf("%s:key", tool)
		err := cache.Set(ctx, key, &interfaces.ToolOutput{Result: key}, 5*time.Minute)
		require.NoError(t, err)
	}

	// 触发第一个工具的失效
	startTime := time.Now()
	count, err := cache.InvalidateByTool(ctx, "tool0")
	duration := time.Since(startTime)

	require.NoError(t, err)
	t.Logf("Invalidated %d entries in complex graph in %v", count, duration)

	// 如果没有栈溢出，测试就通过了
	assert.True(t, true, "No stack overflow occurred")
}
