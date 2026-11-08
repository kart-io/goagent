package tools

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/middleware"
	"github.com/stretchr/testify/require"
)

func TestDesignMiddlewareObjectUsesWrap(t *testing.T) {
	tool := &mockToolForWrapper{name: "cache_object"}
	wrapped := WithMiddleware(tool, middleware.NewCachingMiddleware().Wrap)
	in := &interfaces.ToolInput{Args: map[string]any{"value": "同一请求"}}
	_, err := wrapped.Invoke(context.Background(), in)
	require.NoError(t, err)
	out, err := wrapped.Invoke(context.Background(), in)
	require.NoError(t, err)
	require.EqualValues(t, 1, tool.callCount.Load(), "传入提供Wrap的中间件对象应真正启用缓存")
	require.Equal(t, true, out.Metadata["cache_hit"])
}

func TestDesignNilMiddlewareFailsAtAssembly(t *testing.T) {
	require.Panics(t, func() {
		WithMiddleware(&mockToolForWrapper{name: "invalid"}, nil)
	}, "空函数须在装配时被拒绝")
}

func TestDesignMiddlewareMustReturnInvoker(t *testing.T) {
	require.Panics(t, func() {
		WithMiddleware(&mockToolForWrapper{name: "invalid"}, func(interfaces.Tool, middleware.ToolInvoker) middleware.ToolInvoker { return nil })
	}, "函数返回空调用器须在装配时被拒绝")
}
