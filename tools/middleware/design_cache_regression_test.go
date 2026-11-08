package middleware

import (
	"bytes"
	"context"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"

	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestDesignCachePreservesCompleteOutput(t *testing.T) {
	ctx := context.Background()
	borrowed := cache.NewSimpleCache(0)
	defer borrowed.Close()
	runs := 0
	type resultMap map[string][]string
	wrapped := Caching(WithCache(borrowed))(newMockTool(), func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		runs++
		return &interfaces.ToolOutput{Success: true, Result: resultMap{"evidence": {"原始结果"}}, Metadata: map[string]any{"nested": map[string]string{"value": "原始元数据"}}, AdditionalContext: []string{"补充证据"}, Labels: map[string]string{"source": "test"}}, nil
	})
	in := &interfaces.ToolInput{Args: map[string]any{"query": "相同请求"}}
	first, err := wrapped(ctx, in)
	require.NoError(t, err)
	first.Result.(resultMap)["evidence"][0] = "被修改的结果"
	first.Metadata["nested"].(map[string]string)["value"] = "被修改的元数据"
	first.AdditionalContext[0] = "被修改的上下文"
	first.Labels["source"] = "modified"
	second, err := wrapped(ctx, in)
	require.NoError(t, err)
	require.Equal(t, 1, runs)
	require.Equal(t, resultMap{"evidence": {"原始结果"}}, second.Result)
	require.Equal(t, map[string]string{"value": "原始元数据"}, second.Metadata["nested"])
	require.Equal(t, []string{"补充证据"}, second.AdditionalContext)
	require.Equal(t, map[string]string{"source": "test"}, second.Labels)
	second.Result.(resultMap)["evidence"][0] = "再次修改"
	third, err := wrapped(ctx, in)
	require.NoError(t, err)
	require.Equal(t, resultMap{"evidence": {"原始结果"}}, third.Result)
}

func TestDesignInjectedCacheDoesNotLeakDefaultWorker(t *testing.T) {
	count := func() int {
		var b bytes.Buffer
		require.NoError(t, pprof.Lookup("goroutine").WriteTo(&b, 2))
		return strings.Count(b.String(), "cache.(*SimpleCache).cleanup(")
	}
	before := count()
	for range 20 {
		_ = NewCachingMiddleware(WithCache(cache.NewNoOpCache()))
	}
	runtime.GC()
	for range 50 {
		runtime.Gosched()
	}
	require.LessOrEqual(t, count(), before, "注入缓存不得额外创建无法关闭的默认清理协程")
}

func TestDesignSharedCacheInvalidEntriesAreMisses(t *testing.T) {
	for _, bad := range []any{"其他组件的值", (*interfaces.ToolOutput)(nil), &interfaces.ToolOutput{Success: true, Result: func() {}}} {
		c := cache.NewSimpleCache(0)
		defer c.Close()
		require.NoError(t, c.Set(context.Background(), "shared", bad, 0))
		calls := 0
		wrapped := Caching(WithCache(c), WithCacheKeyFunc(func(string, map[string]any) string { return "shared" }))(newMockTool(), func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			calls++
			return &interfaces.ToolOutput{Success: true, Result: "新结果"}, nil
		})
		require.NotPanics(t, func() {
			out, err := wrapped(context.Background(), &interfaces.ToolInput{Args: map[string]any{}})
			require.NoError(t, err)
			require.Equal(t, "新结果", out.Result)
		})
		require.Equal(t, 1, calls, "不可用缓存值必须降级为真实调用")
	}
}

func TestDesignCacheWriteFailureDoesNotClaimStored(t *testing.T) {
	wrapped := Caching(WithCache(cache.NewNoOpCache()))(newMockTool(), func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "真实结果"}, nil
	})
	out, err := wrapped(context.Background(), &interfaces.ToolInput{Args: map[string]any{}})
	require.NoError(t, err)
	require.Equal(t, "真实结果", out.Result)
	require.NotEqual(t, true, out.Metadata["cache_stored"])
}

func TestDesignCacheRejectsNilToolOutput(t *testing.T) {
	wrapped := Caching()(newMockTool(), func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) { return nil, nil })
	require.NotPanics(t, func() {
		_, err := wrapped(context.Background(), &interfaces.ToolInput{Args: map[string]any{}})
		require.Error(t, err)
	})
}
