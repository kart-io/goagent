package middleware_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/middleware"
)

// 装配入口的函数类型由编译器检查，不接受对象或任意值。
var _ func(interfaces.Tool, ...middleware.ToolMiddlewareFunc) interfaces.Tool = middleware.Wrap

func TestCleanupFunctionalMiddlewareOnionOrder(t *testing.T) {
	var order []string
	wrap := func(name string) middleware.ToolMiddlewareFunc {
		return func(_ interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
			return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				order = append(order, name+":前")
				out, err := next(ctx, input)
				order = append(order, name+":后")
				return out, err
			}
		}
	}
	tool := tools.NewBaseTool("顺序", "", `{}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		order = append(order, "执行")
		return &interfaces.ToolOutput{Result: "成功", Success: true}, nil
	})
	out, err := middleware.Wrap(tool, wrap("甲"), wrap("乙"), wrap("丙")).Invoke(context.Background(), &interfaces.ToolInput{})
	require.NoError(t, err)
	require.True(t, out.Success)
	require.Equal(t, []string{"甲:前", "乙:前", "丙:前", "执行", "丙:后", "乙:后", "甲:后"}, order)
}

func TestCleanupFunctionalMiddlewareErrorBoundaries(t *testing.T) {
	for _, phase := range []string{"前置", "执行", "后置"} {
		t.Run(phase, func(t *testing.T) {
			want := errors.New("边界错误")
			calls := 0
			observed := false
			tool := tools.NewBaseTool("边界", "", `{}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				calls++
				if phase == "执行" {
					return &interfaces.ToolOutput{Result: "部分结果"}, want
				}
				return &interfaces.ToolOutput{Result: "成功", Success: true}, nil
			})
			observer := func(_ interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
				return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
					out, err := next(ctx, input)
					observed = errors.Is(err, want)
					return out, err
				}
			}
			boundary := func(_ interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
				return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
					if phase == "前置" {
						return nil, want
					}
					out, err := next(ctx, input)
					if err == nil && phase == "后置" {
						return out, want
					}
					return out, err
				}
			}
			out, err := middleware.Wrap(tool, observer, boundary).Invoke(context.Background(), &interfaces.ToolInput{})
			require.ErrorIs(t, err, want)
			require.True(t, observed, "外层函数须观察到内层真实错误")
			if phase == "前置" {
				require.Zero(t, calls)
				require.Nil(t, out)
			} else {
				require.Equal(t, 1, calls)
				require.NotNil(t, out, "错误不能吞掉已生成结果")
			}
		})
	}
}

func TestCleanupLoggingCacheAndRateLimitCompose(t *testing.T) {
	tool := &mockToolForWrapper{name: "实际组合"}
	caching := middleware.NewCachingMiddleware()
	limit := middleware.NewRateLimitMiddleware(middleware.WithQPS(0), middleware.WithBurst(1))
	wrapped := middleware.Wrap(tool, middleware.Logging(middleware.WithLogger(nil)), caching.Wrap, limit.Wrap)
	input := &interfaces.ToolInput{Args: map[string]interface{}{"value": "甲"}}
	_, err := wrapped.Invoke(context.Background(), input)
	require.NoError(t, err)
	out, err := wrapped.Invoke(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, true, out.Metadata["cache_hit"])
	_, err = wrapped.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]interface{}{"value": "乙"}})
	require.Error(t, err, "不同输入缓存未命中时须经过真实限流")
	require.EqualValues(t, 1, tool.callCount.Load())
	allowed, rejected := limit.GetStats()
	require.EqualValues(t, 1, allowed)
	require.EqualValues(t, 1, rejected)
	require.Equal(t, map[string]interface{}{"value": "甲"}, input.Args)
}

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

// TestWrap_NoMiddleware 测试无中间件时的包装
func TestWrap_NoMiddleware(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	wrapped := middleware.Wrap(tool)

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

// TestWrap_SingleFunctionalMiddleware 测试单个函数式中间件
func TestWrap_SingleFunctionalMiddleware(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	// 使用缓存中间件
	cachingMW := middleware.Caching()
	wrapped := middleware.Wrap(tool, cachingMW)

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

// TestWrap_MultipleFunctionalMiddleware 测试多个函数式中间件
func TestWrap_MultipleFunctionalMiddleware(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	// 应用多个中间件：缓存 + 限流
	cachingMW := middleware.Caching()
	rateLimitMW := middleware.RateLimit(
		middleware.WithQPS(10),
		middleware.WithBurst(5),
	)

	wrapped := middleware.Wrap(tool,
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

// TestWrap_StatefulWrap 测试显式装配有状态对象。
func TestWrap_StatefulWrap(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	// 使用接口式中间件
	cachingMW := middleware.NewCachingMiddleware()

	// 创建一个包装器，将 CachingMiddleware.Wrap 转换为接口
	wrapped := middleware.Wrap(tool, cachingMW.Wrap)

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

// TestWrap_ToolMetadata 测试工具元数据保留
func TestWrap_ToolMetadata(t *testing.T) {
	tool := &mockToolForWrapper{name: "test_tool"}
	wrapped := middleware.Wrap(tool, middleware.Caching())

	// 验证工具元数据
	assert.Equal(t, "test_tool", wrapped.Name())
	assert.Equal(t, "A mock tool for wrapper testing", wrapped.Description())
	assert.Equal(t, `{"type": "object"}`, wrapped.ArgsSchema())
}

// TestWrap_Unwrap 测试 Unwrap 方法
func TestWrap_Unwrap(t *testing.T) {
	tool := &mockToolForWrapper{name: "test_tool"}
	wrapped := middleware.Wrap(tool, middleware.Caching())

	// 尝试类型断言获取原始工具
	if mw, ok := wrapped.(*middleware.WrappedTool); ok {
		originalTool := mw.Unwrap()
		assert.Equal(t, tool, originalTool)
	} else {
		t.Fatal("Wrapped tool should be *middleware.WrappedTool")
	}
}

// TestWrap_CachingEffectiveness 测试缓存中间件的有效性
func TestWrap_CachingEffectiveness(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	wrapped := middleware.Wrap(tool,
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

// TestWrap_RateLimitEffectiveness 测试限流中间件的有效性
func TestWrap_RateLimitEffectiveness(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	wrapped := middleware.Wrap(tool,
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

// TestWrap_Concurrent 测试并发场景
func TestWrap_Concurrent(t *testing.T) {
	ctx := context.Background()

	tool := &mockToolForWrapper{name: "test_tool"}
	wrapped := middleware.Wrap(tool,
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

	// 冷缓存不合并正在执行的请求；先验证并发调用，再验证热缓存命中。
	const numGoroutines = 50
	runWave := func(cached bool) {
		t.Helper()
		type invocation struct {
			output *interfaces.ToolOutput
			err    error
		}
		start := make(chan struct{})
		results := make(chan invocation, numGoroutines)
		for range numGoroutines {
			go func() {
				<-start
				output, err := wrapped.Invoke(ctx, input)
				results <- invocation{output, err}
			}()
		}
		close(start)
		for range numGoroutines {
			result := <-results
			require.NoError(t, result.err)
			require.NotNil(t, result.output)
			assert.True(t, result.output.Success)
			assert.Equal(t, "processed: test", result.output.Result)
			if cached {
				assert.Equal(t, true, result.output.Metadata["cache_hit"])
			}
		}
	}

	runWave(false)
	coldCalls := tool.callCount.Load()
	require.Positive(t, coldCalls)
	runWave(true)
	assert.Equal(t, coldCalls, tool.callCount.Load(), "热缓存并发命中不应再次调用工具")
}

// TestWrap_MixedTypes 测试混合类型的中间件
func TestWrap_MixedTypes(t *testing.T) {
	ctx := context.Background()
	tool := &mockToolForWrapper{name: "test_tool"}

	// 混合使用函数式中间件
	cachingMW := middleware.NewCachingMiddleware()
	rateLimitMW := middleware.RateLimit(middleware.WithQPS(10), middleware.WithBurst(5))

	wrapped := middleware.Wrap(tool,
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
