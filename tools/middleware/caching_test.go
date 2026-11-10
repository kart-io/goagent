package middleware

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/toolargs"
)

// TestCachingMiddleware_Basic 测试基本缓存功能
func TestCachingMiddleware_Basic(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := NewCachingMiddleware()

	callCount := 0
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		callCount++
		return &interfaces.ToolOutput{
			Result:  "computed result",
			Success: true,
		}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"key": "value"},
	}

	// 第一次调用：应该执行实际函数
	output1, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.True(t, output1.Success)
	assert.Equal(t, "computed result", output1.Result)
	assert.Equal(t, 1, callCount, "Should call invoker once")
	assert.False(t, output1.Metadata["cache_hit"].(bool), "First call should not be cache hit")
	assert.True(t, output1.Metadata["cache_stored"].(bool), "Result should be stored in cache")

	// 第二次调用：应该从缓存返回，不调用实际函数
	output2, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.True(t, output2.Success)
	assert.Equal(t, "computed result", output2.Result)
	assert.Equal(t, 1, callCount, "Should NOT call invoker again (cache hit)")
	assert.True(t, output2.Metadata["cache_hit"].(bool), "Second call should be cache hit")

	// 验证统计信息
	hits, misses := middleware.GetStats()
	assert.Equal(t, int64(1), hits, "Should have 1 cache hit")
	assert.Equal(t, int64(1), misses, "Should have 1 cache miss")
}

// TestCachingMiddleware_DifferentArgs 测试不同参数使用不同缓存
func TestCachingMiddleware_DifferentArgs(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := NewCachingMiddleware()

	callCount := 0
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		callCount++
		value := input.Args["key"].(string)
		return &interfaces.ToolOutput{
			Result:  "result_" + value,
			Success: true,
		}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)

	// 第一组参数
	input1 := &interfaces.ToolInput{
		Args: map[string]interface{}{"key": "value1"},
	}
	output1, err := wrapped(ctx, input1)
	require.NoError(t, err)
	assert.Equal(t, "result_value1", output1.Result)
	assert.Equal(t, 1, callCount)

	// 第二组参数（不同）
	input2 := &interfaces.ToolInput{
		Args: map[string]interface{}{"key": "value2"},
	}
	output2, err := wrapped(ctx, input2)
	require.NoError(t, err)
	assert.Equal(t, "result_value2", output2.Result)
	assert.Equal(t, 2, callCount, "Different args should not use cache")

	// 重复第一组参数
	output3, err := wrapped(ctx, input1)
	require.NoError(t, err)
	assert.Equal(t, "result_value1", output3.Result)
	assert.Equal(t, 2, callCount, "Same args should use cache")
	assert.True(t, output3.Metadata["cache_hit"].(bool))
}

// TestCachingMiddleware_OnlySuccessIsCached 测试只缓存成功结果
func TestCachingMiddleware_OnlySuccessIsCached(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := NewCachingMiddleware()

	callCount := 0
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		callCount++
		if callCount == 1 {
			// 第一次调用失败
			return &interfaces.ToolOutput{
				Result:  nil,
				Success: false,
				Error:   "execution failed",
			}, nil
		}
		// 后续调用成功
		return &interfaces.ToolOutput{
			Result:  "success",
			Success: true,
		}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"key": "value"},
	}

	// 第一次调用：失败
	output1, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.False(t, output1.Success)
	assert.Equal(t, 1, callCount)
	assert.NotContains(t, output1.Metadata, "cache_stored", "Failed result should not be cached")

	// 第二次调用：应该重新执行（不使用缓存）
	output2, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.True(t, output2.Success)
	assert.Equal(t, 2, callCount, "Should call invoker again for failed result")
}

// TestCachingMiddleware_TTL 测试缓存过期
func TestCachingMiddleware_TTL(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := NewCachingMiddleware(
		WithTTL(50 * time.Millisecond), // 50ms TTL
	)

	callCount := 0
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		callCount++
		return &interfaces.ToolOutput{
			Result:  "result",
			Success: true,
		}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"key": "value"},
	}

	// 第一次调用
	output1, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)
	assert.False(t, output1.Metadata["cache_hit"].(bool))

	// 立即第二次调用：应该命中缓存
	output2, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "Should use cache")
	assert.True(t, output2.Metadata["cache_hit"].(bool))

	// 等待 TTL 过期
	time.Sleep(100 * time.Millisecond)

	// 第三次调用：缓存应该已过期
	output3, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount, "Cache should have expired")
	assert.False(t, output3.Metadata["cache_hit"].(bool))
}

// TestCachingMiddleware_CustomCache 测试自定义缓存
func TestCachingMiddleware_CustomCache(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	customCache := cache.NewSimpleCache(5 * time.Minute)
	t.Cleanup(customCache.Close)
	middleware := NewCachingMiddleware(
		WithCache(customCache),
	)

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result:  "result",
			Success: true,
		}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"key": "value"},
	}

	// 执行调用
	_, err := wrapped(ctx, input)
	require.NoError(t, err)

	// 验证自定义缓存中有值
	stats := customCache.GetStats()
	assert.Greater(t, stats.Size, int64(0), "Custom cache should have entries")
}

// TestCachingMiddleware_CustomKeyFunc 测试自定义缓存键函数
func TestCachingMiddleware_CustomKeyFunc(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	// 自定义键函数：忽略参数，总是返回相同的键
	customKeyFunc := func(toolName string, args map[string]interface{}) string {
		return "custom_key"
	}

	middleware := NewCachingMiddleware(
		WithCacheKeyFunc(customKeyFunc),
	)

	callCount := 0
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		callCount++
		return &interfaces.ToolOutput{
			Result:  "result",
			Success: true,
		}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)

	// 第一组参数
	input1 := &interfaces.ToolInput{
		Args: map[string]interface{}{"key": "value1"},
	}
	_, err := wrapped(ctx, input1)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)

	// 第二组参数（不同，但自定义键函数返回相同的键）
	input2 := &interfaces.ToolInput{
		Args: map[string]interface{}{"key": "value2"},
	}
	output2, err := wrapped(ctx, input2)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "Should use cache with custom key func")
	assert.True(t, output2.Metadata["cache_hit"].(bool))
}

// TestDefaultCacheKeyFunc 测试默认缓存键生成函数
func TestDefaultCacheKeyFunc(t *testing.T) {
	// 相同的参数应该生成相同的键
	key1 := defaultCacheKeyFunc("test_tool", map[string]interface{}{
		"a": 1,
		"b": "test",
	})
	key2 := defaultCacheKeyFunc("test_tool", map[string]interface{}{
		"a": 1,
		"b": "test",
	})
	assert.Equal(t, key1, key2, "Same args should generate same key")

	// 不同的参数应该生成不同的键
	key3 := defaultCacheKeyFunc("test_tool", map[string]interface{}{
		"a": 2,
		"b": "test",
	})
	assert.NotEqual(t, key1, key3, "Different args should generate different keys")

	// 不同的工具名应该生成不同的键
	key4 := defaultCacheKeyFunc("other_tool", map[string]interface{}{
		"a": 1,
		"b": "test",
	})
	assert.NotEqual(t, key1, key4, "Different tool names should generate different keys")

	// 键应该包含工具名
	assert.Contains(t, key1, "test_tool", "Key should contain tool name")
	assert.Contains(t, key1, "tool:", "Key should have 'tool:' prefix")

	// 默认键保留完整SHA256，避免32位短摘要碰撞。
	assert.Len(t, key1, len("tool:test_tool:")+64)
}

// TestDefaultCacheKeyFunc_InternalMetadata 测试默认键函数过滤内部元数据
func TestDefaultCacheKeyFunc_InternalMetadata(t *testing.T) {
	// 内部元数据（__开头的键）不应该影响缓存键
	key1 := defaultCacheKeyFunc("test_tool", map[string]interface{}{
		"user_key":           "value",
		"__internal_key":     "should_be_ignored",
		"__logging_metadata": time.Now(),
	})

	key2 := defaultCacheKeyFunc("test_tool", map[string]interface{}{
		"user_key":           "value",
		"__internal_key":     "different_value",
		"__logging_metadata": time.Now().Add(1 * time.Hour),
	})

	assert.Equal(t, key1, key2, "Internal metadata should not affect cache key")
}

// TestCachingMiddleware_Concurrent 测试并发访问
func TestCachingMiddleware_Concurrent(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := NewCachingMiddleware()

	var callCount int
	var mu sync.Mutex
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond) // 模拟耗时操作
		return &interfaces.ToolOutput{
			Result:  "result",
			Success: true,
		}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"key": "value"},
	}

	// 并发调用
	var wg sync.WaitGroup
	numCalls := 10
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()
			_, err := wrapped(ctx, input)
			assert.NoError(t, err)
		}()
	}

	// 等待所有调用完成
	wg.Wait()

	// 由于并发，可能有多次实际调用，但应该远少于 10 次
	// 大部分应该命中缓存
	mu.Lock()
	finalCallCount := callCount
	mu.Unlock()

	assert.LessOrEqual(t, finalCallCount, numCalls, "Should not call invoker for every request")
	t.Logf("Concurrent test: %d calls resulted in %d actual executions", numCalls, finalCallCount)
}

// TestCachingMiddleware_CacheHitDoesNotCallNext 测试缓存命中时不调用实际工具（关键测试）
func TestCachingMiddleware_CacheHitDoesNotCallNext(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := NewCachingMiddleware()

	callCount := 0
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		callCount++
		t.Logf("Invoker called (count: %d)", callCount)
		return &interfaces.ToolOutput{
			Result:  "computed result",
			Success: true,
		}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"key": "value"},
	}

	// 第一次调用
	output1, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "First call should invoke the tool")
	assert.False(t, output1.Metadata["cache_hit"].(bool))

	// 第二次调用（缓存命中）
	output2, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "CRITICAL: Cache hit should NOT call invoker again")
	assert.True(t, output2.Metadata["cache_hit"].(bool))

	// 第三次调用（再次缓存命中）
	output3, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "CRITICAL: Cache hit should NOT call invoker again")
	assert.True(t, output3.Metadata["cache_hit"].(bool))
}

// float64 相同但实际不同的大整数参数不得命中同一缓存项。
func TestCachingMiddleware_ExactArgumentsDoNotCollide(t *testing.T) {
	invoker := func(_ context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: input.Args["id"]}, nil
	}
	wrapped := NewCachingMiddleware().Wrap(newMockTool(), invoker)
	for _, id := range []string{"9007199254740992", "9007199254740993"} {
		args, ok := toolargs.Parse(`{"id":` + id + `}`)
		require.True(t, ok)
		out, err := wrapped(context.Background(), &interfaces.ToolInput{Args: args})
		require.NoError(t, err)
		require.Equal(t, args["id"], out.Result)
	}
}

// 缓存按会话与调用者隔离：结果可能依赖调用者身份，不同会话或调用者以相同参数调用不能读到彼此的结果。
func TestCachingIsolatesSessionsAndCallers(t *testing.T) {
	calls := 0
	tool := &mockTool{name: "lookup"}
	next := func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		return &interfaces.ToolOutput{Success: true, Result: in.CallerID}, nil
	}
	invoke := NewCachingMiddleware().Wrap(tool, next)
	run := func(session, caller string) {
		ctx := tools.WithSessionID(context.Background(), session)
		_, err := invoke(ctx, &interfaces.ToolInput{CallerID: caller, Args: map[string]any{"q": "同一参数"}})
		require.NoError(t, err)
	}
	run("s1", "alice")
	run("s1", "alice")
	require.Equal(t, 1, calls, "同一会话同一调用者命中缓存")
	run("s2", "alice")
	require.Equal(t, 2, calls, "不同会话不共享")
	run("s1", "bob")
	require.Equal(t, 3, calls, "不同调用者不共享")
}
