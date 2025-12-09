package middleware

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRateLimitMiddleware_Basic 测试基本限流功能
func TestRateLimitMiddleware_Basic(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := NewRateLimitMiddleware(
		WithQPS(10),  // 10 QPS
		WithBurst(2), // 允许突发 2 个
	)

	var callCount int
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

	// 前两个请求应该通过（在突发容量内）
	output1, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.True(t, output1.Success)
	assert.Equal(t, 1, callCount)

	output2, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.True(t, output2.Success)
	assert.Equal(t, 2, callCount)

	// 第三个请求应该被限流拒绝（突发容量已满，QPS还没补充）
	output3, err := wrapped(ctx, input)
	require.Error(t, err)
	assert.Nil(t, output3)
	assert.Contains(t, err.Error(), "rate limit exceeded")
	assert.Equal(t, 2, callCount, "Should not call invoker when rate limited")

	// 验证统计信息
	allowed, rejected := middleware.GetStats()
	assert.Equal(t, int64(2), allowed)
	assert.Equal(t, int64(1), rejected)
}

// TestRateLimitMiddleware_GlobalVsPerTool 测试全局限流 vs 按工具限流
func TestRateLimitMiddleware_GlobalVsPerTool(t *testing.T) {
	ctx := context.Background()

	t.Run("Global", func(t *testing.T) {
		middleware := NewRateLimitMiddleware(
			WithQPS(5),
			WithBurst(3),
		)

		tool1 := &mockTool{name: "tool1"}
		tool2 := &mockTool{name: "tool2"}

		callCount := 0
		invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			callCount++
			return &interfaces.ToolOutput{Success: true}, nil
		}

		wrapped1 := middleware.Wrap(tool1, invoker)
		wrapped2 := middleware.Wrap(tool2, invoker)

		input := &interfaces.ToolInput{Args: map[string]interface{}{}}

		// 工具1消耗 2 个令牌
		_, err := wrapped1(ctx, input)
		require.NoError(t, err)
		_, err = wrapped1(ctx, input)
		require.NoError(t, err)

		// 工具2只能消耗 1 个令牌（突发容量 3，已用 2）
		_, err = wrapped2(ctx, input)
		require.NoError(t, err)

		// 第4个请求应该被拒绝（全局限流）
		_, err = wrapped2(ctx, input)
		require.Error(t, err, "Should be rate limited globally")

		assert.Equal(t, 3, callCount, "Only 3 calls should succeed")
	})

	t.Run("PerTool", func(t *testing.T) {
		middleware := NewRateLimitMiddleware(
			WithQPS(5),
			WithBurst(2),
			WithPerToolRateLimit(), // 每个工具独立限流
		)

		tool1 := &mockTool{name: "tool1"}
		tool2 := &mockTool{name: "tool2"}

		callCount := 0
		invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			callCount++
			return &interfaces.ToolOutput{Success: true}, nil
		}

		wrapped1 := middleware.Wrap(tool1, invoker)
		wrapped2 := middleware.Wrap(tool2, invoker)

		input := &interfaces.ToolInput{Args: map[string]interface{}{}}

		// 工具1消耗 2 个令牌
		_, err := wrapped1(ctx, input)
		require.NoError(t, err)
		_, err = wrapped1(ctx, input)
		require.NoError(t, err)

		// 工具2有自己的令牌桶，也可以消耗 2 个令牌
		_, err = wrapped2(ctx, input)
		require.NoError(t, err, "Tool2 should have its own rate limit")
		_, err = wrapped2(ctx, input)
		require.NoError(t, err, "Tool2 should have its own rate limit")

		assert.Equal(t, 4, callCount, "All 4 calls should succeed with per-tool limiting")

		// 第5个请求（tool1）应该被拒绝
		_, err = wrapped1(ctx, input)
		require.Error(t, err)

		// 第6个请求（tool2）也应该被拒绝
		_, err = wrapped2(ctx, input)
		require.Error(t, err)
	})
}

// TestRateLimitMiddleware_WithWaitTimeout 测试等待超时
func TestRateLimitMiddleware_WithWaitTimeout(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := NewRateLimitMiddleware(
		WithQPS(10),
		WithBurst(1),
		WithWaitTimeout(100*time.Millisecond), // 等待 100ms
	)

	callCount := 0
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		callCount++
		return &interfaces.ToolOutput{Success: true}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	// 第一个请求立即通过
	_, err := wrapped(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)

	// 第二个请求会等待令牌补充（100ms 内应该能补充，QPS=10 => 100ms/token）
	start := time.Now()
	_, err = wrapped(ctx, input)
	duration := time.Since(start)

	require.NoError(t, err, "Should succeed after waiting")
	assert.Equal(t, 2, callCount)
	assert.Greater(t, duration, 50*time.Millisecond, "Should have waited for token")
}

// TestRateLimitMiddleware_Concurrent 测试并发限流
func TestRateLimitMiddleware_Concurrent(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	qps := 100.0
	burst := 10
	middleware := NewRateLimitMiddleware(
		WithQPS(qps),
		WithBurst(burst),
	)

	var callCount atomic.Int32
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		callCount.Add(1)
		return &interfaces.ToolOutput{Success: true}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	// 并发发送 50 个请求
	numRequests := 50
	var wg sync.WaitGroup
	wg.Add(numRequests)

	successCount := atomic.Int32{}
	errorCount := atomic.Int32{}

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			_, err := wrapped(ctx, input)
			if err != nil {
				errorCount.Add(1)
			} else {
				successCount.Add(1)
			}
		}()
	}

	wg.Wait()

	// 验证限流生效
	success := successCount.Load()
	errors := errorCount.Load()

	t.Logf("Success: %d, Errors: %d, Total: %d", success, errors, success+errors)

	// 突发容量为 10，所以最多 10 个请求立即成功，其余被拒绝
	assert.Equal(t, int32(numRequests), success+errors, "All requests should complete")
	assert.LessOrEqual(t, success, int32(burst), "Success count should not exceed burst capacity")
	assert.Greater(t, errors, int32(0), "Some requests should be rate limited")
}

// TestRateLimitMiddleware_QPS 测试 QPS 限制
func TestRateLimitMiddleware_QPS(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping QPS test in short mode")
	}

	ctx := context.Background()
	tool := newMockTool()

	qps := 10.0
	middleware := NewRateLimitMiddleware(
		WithQPS(qps),
		WithBurst(1),                   // 使用小的突发容量以更准确测试 QPS
		WithWaitTimeout(5*time.Second), // 允许等待
	)

	callCount := 0
	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		callCount++
		return &interfaces.ToolOutput{Success: true}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	// 发送 15 个请求，测试 QPS（减少请求数以缩短测试时间）
	numRequests := 15
	start := time.Now()

	for i := 0; i < numRequests; i++ {
		_, err := wrapped(ctx, input)
		require.NoError(t, err)
	}

	duration := time.Since(start)

	// 计算实际 QPS
	actualQPS := float64(numRequests) / duration.Seconds()

	t.Logf("Requests: %d, Duration: %v, Actual QPS: %.2f", numRequests, duration, actualQPS)

	// 实际 QPS 应该接近配置的 QPS（允许 30% 误差，因为令牌桶算法的特性）
	expectedMin := qps * 0.7
	expectedMax := qps * 1.3
	assert.Greater(t, actualQPS, expectedMin, "QPS should be at least 70%% of configured")
	assert.Less(t, actualQPS, expectedMax, "QPS should not exceed 130%% of configured")
}

// TestRateLimitMiddleware_Metadata 测试元数据添加
func TestRateLimitMiddleware_Metadata(t *testing.T) {
	ctx := context.Background()
	tool := newMockTool()

	middleware := NewRateLimitMiddleware(
		WithQPS(10),
		WithBurst(10),
	)

	invoker := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{
			Result:  "result",
			Success: true,
		}, nil
	}

	wrapped := middleware.Wrap(tool, invoker)
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}

	output, err := wrapped(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output.Metadata)

	// 验证添加了限流元数据
	assert.Contains(t, output.Metadata, "rate_limited")
	assert.False(t, output.Metadata["rate_limited"].(bool), "Should not be rate limited")
}
