package tools

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/middleware"
)

// mockToolForWrapper 是用于测试包装器的模拟工具
type mockToolForWrapper struct {
	name        string
	callCount   atomic.Int32 // 使用原子操作保证线程安全
	shouldError bool
}

func (m *mockToolForWrapper) Name() string {
	return m.name
}

func (m *mockToolForWrapper) Description() string {
	return "A mock tool for wrapper testing"
}

func (m *mockToolForWrapper) ArgsSchema() string {
	return `{"type": "object"}`
}

func (m *mockToolForWrapper) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	m.callCount.Add(1)

	if m.shouldError {
		return &interfaces.ToolOutput{
			Success: false,
			Error:   "mock error",
		}, nil
	}

	value := "default"
	if val, ok := input.Args["value"].(string); ok {
		value = val
	}

	return &interfaces.ToolOutput{
		Result:  "processed: " + value,
		Success: true,
	}, nil
}

// TestWithMiddleware_NoMiddleware 测试无中间件时的包装
func TestWithMiddleware_NoMiddleware(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	wrapped := WithMiddleware(tool)

	// 应该返回原始工具
	assert.Equal(t, tool, wrapped)

	// 验证功能正常
	output, err := wrapped.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{"value": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, "processed: test", output.Result)
	assert.Equal(t, int32(1), tool.callCount.Load())
}

// TestWithMiddleware_SingleFunctionalMiddleware 测试单个函数式中间件
func TestWithMiddleware_SingleFunctionalMiddleware(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	// 使用缓存中间件
	cachingMW := middleware.Caching()
	wrapped := WithMiddleware(tool, cachingMW)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"value": "test"},
	}

	// 第一次调用
	output1, err := wrapped.Invoke(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, "processed: test", output1.Result)
	assert.Equal(t, int32(1), tool.callCount.Load())
	assert.False(t, output1.Metadata["cache_hit"].(bool))

	// 第二次调用（缓存命中）
	output2, err := wrapped.Invoke(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, "processed: test", output2.Result)
	assert.Equal(t, int32(1), tool.callCount.Load(), "Cache hit should not call tool again")
	assert.True(t, output2.Metadata["cache_hit"].(bool))
}

// TestWithMiddleware_MultipleFunctionalMiddleware 测试多个函数式中间件
func TestWithMiddleware_MultipleFunctionalMiddleware(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	// 应用多个中间件：缓存 + 限流
	cachingMW := middleware.Caching()
	rateLimitMW := middleware.RateLimit(
		middleware.WithQPS(10),
		middleware.WithBurst(5),
	)

	wrapped := WithMiddleware(tool,
		cachingMW,   // 外层
		rateLimitMW, // 内层
	)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"value": "test"},
	}

	// 第一次调用
	output1, err := wrapped.Invoke(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, "processed: test", output1.Result)
	assert.Equal(t, int32(1), tool.callCount.Load())

	// 验证中间件元数据
	assert.Contains(t, output1.Metadata, "cache_stored") // 来自 caching
	assert.Contains(t, output1.Metadata, "rate_limited") // 来自 rate_limit

	// 第二次调用（缓存命中，不触发限流和工具调用）
	output2, err := wrapped.Invoke(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, "processed: test", output2.Result)
	assert.Equal(t, int32(1), tool.callCount.Load(), "Cache hit should skip tool and rate limit")
	assert.True(t, output2.Metadata["cache_hit"].(bool))
}

// TestWithMiddleware_InterfaceMiddleware 测试接口式中间件（旧接口）
func TestWithMiddleware_InterfaceMiddleware(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	// 使用接口式中间件
	cachingMW := middleware.NewCachingMiddleware()

	// 创建一个包装器，将 CachingMiddleware.Wrap 转换为接口
	wrapped := WithMiddleware(tool, cachingMW.Wrap)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"value": "test"},
	}

	// 第一次调用
	_, err := wrapped.Invoke(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, int32(1), tool.callCount.Load())

	// 第二次调用（缓存命中）
	output2, err := wrapped.Invoke(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, int32(1), tool.callCount.Load())
	assert.True(t, output2.Metadata["cache_hit"].(bool))
}

// TestWithMiddleware_ToolMetadata 测试工具元数据保留
func TestWithMiddleware_ToolMetadata(t *testing.T) {
	tool := &mockToolForWrapper{name: "test_tool"}
	wrapped := WithMiddleware(tool, middleware.Caching())

	// 验证工具元数据
	assert.Equal(t, "test_tool", wrapped.Name())
	assert.Equal(t, "A mock tool for wrapper testing", wrapped.Description())
	assert.Equal(t, `{"type": "object"}`, wrapped.ArgsSchema())
}

// TestWithMiddleware_Unwrap 测试 Unwrap 方法
func TestWithMiddleware_Unwrap(t *testing.T) {
	tool := &mockToolForWrapper{name: "test_tool"}
	wrapped := WithMiddleware(tool, middleware.Caching())

	// 尝试类型断言获取原始工具
	if mw, ok := wrapped.(*MiddlewareTool); ok {
		originalTool := mw.Unwrap()
		assert.Equal(t, tool, originalTool)
	} else {
		t.Fatal("Wrapped tool should be *MiddlewareTool")
	}
}

// TestWithMiddleware_CachingEffectiveness 测试缓存中间件的有效性
func TestWithMiddleware_CachingEffectiveness(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	wrapped := WithMiddleware(tool,
		middleware.Caching(
			middleware.WithTTL(1*time.Second),
		),
	)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"value": "test"},
	}

	// 执行 10 次相同的调用
	for i := 0; i < 10; i++ {
		output, err := wrapped.Invoke(ctx, input)
		require.NoError(t, err)
		assert.Equal(t, "processed: test", output.Result)
	}

	// 工具应该只被调用一次（其余9次命中缓存）
	assert.Equal(t, int32(1), tool.callCount.Load(), "interfaces.Tool should only be called once with caching")
}

// TestWithMiddleware_RateLimitEffectiveness 测试限流中间件的有效性
func TestWithMiddleware_RateLimitEffectiveness(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	wrapped := WithMiddleware(tool,
		middleware.RateLimit(
			middleware.WithQPS(10),
			middleware.WithBurst(3),
		),
	)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"value": "test"},
	}

	successCount := 0
	errorCount := 0

	// 执行 10 次调用
	for i := 0; i < 10; i++ {
		_, err := wrapped.Invoke(ctx, input)
		if err != nil {
			errorCount++
		} else {
			successCount++
		}
	}

	// 只有前 3 个请求应该成功（突发容量）
	assert.Equal(t, 3, successCount, "Only burst requests should succeed")
	assert.Equal(t, 7, errorCount, "Remaining requests should be rate limited")
}

// TestWithMiddleware_Concurrent 测试并发场景
func TestWithMiddleware_Concurrent(t *testing.T) {
	ctx := context.Background()

	tool := &mockToolForWrapper{name: "test_tool"}
	wrapped := WithMiddleware(tool,
		middleware.Caching( // LRU cache is thread-safe
			middleware.WithTTL(1*time.Minute),
		),
		middleware.RateLimit(
			middleware.WithQPS(100),
			middleware.WithBurst(50),
		),
	)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"value": "test"},
	}

	// 并发执行 50 次
	const numGoroutines = 50
	done := make(chan bool, numGoroutines)
	var successCount atomic.Int32

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { done <- true }()
			output, err := wrapped.Invoke(ctx, input)
			if err == nil && output.Success {
				successCount.Add(1)
			}
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// 由于缓存，工具调用次数应该远小于并发数
	t.Logf("interfaces.Tool call count: %d, Success count: %d", tool.callCount.Load(), successCount.Load())
	assert.LessOrEqual(t, tool.callCount.Load(), int32(10), "Caching should reduce tool calls")
	assert.Greater(t, int(successCount.Load()), 0, "Some requests should succeed")
}

// TestWithMiddleware_MixedTypes 测试混合类型的中间件
func TestWithMiddleware_MixedTypes(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	// 混合使用函数式中间件
	cachingMW := middleware.NewCachingMiddleware()
	rateLimitMW := middleware.RateLimit(middleware.WithQPS(10), middleware.WithBurst(5))

	wrapped := WithMiddleware(tool,
		cachingMW.Wrap, // 使用 Wrap 方法
		rateLimitMW,    // 函数式中间件
	)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{"value": "test"},
	}

	// 第一次调用
	_, err := wrapped.Invoke(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, int32(1), tool.callCount.Load())

	// 第二次调用（缓存命中，不触发限流）
	output2, err := wrapped.Invoke(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, int32(1), tool.callCount.Load())
	assert.True(t, output2.Metadata["cache_hit"].(bool))
}
