package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
)

func bufferLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func succeed(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return &interfaces.ToolOutput{Result: "结果", Success: true}, nil
}

func callTwice(t *testing.T, invoke ToolInvoker) {
	t.Helper()
	input := &interfaces.ToolInput{Args: map[string]interface{}{"k": "v"}}
	for range 2 {
		_, _ = invoke(context.Background(), input)
	}
}

func TestContextLoggerOptions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(*slog.Logger) ToolInvoker
		want  []string
	}{
		{
			name: "调用日志",
			build: func(l *slog.Logger) ToolInvoker {
				return NewLoggingMiddleware(WithContextLogger(l)).Wrap(newMockTool(), succeed)
			},
			want: []string{"level=INFO msg=\"Tool invocation started\" tool=test_tool", "level=INFO msg=\"Tool invocation completed\" tool=test_tool"},
		},
		{
			name: "缓存",
			build: func(l *slog.Logger) ToolInvoker {
				return NewCachingMiddleware(WithCacheContextLogger(l)).Wrap(newMockTool(), succeed)
			},
			want: []string{"level=DEBUG msg=工具缓存未命中 tool=test_tool", "level=DEBUG msg=工具缓存命中 tool=test_tool"},
		},
		{
			name: "限流",
			build: func(l *slog.Logger) ToolInvoker {
				return NewRateLimitMiddleware(WithQPS(0.001), WithBurst(1), WithRateLimitContextLogger(l)).Wrap(newMockTool(), succeed)
			},
			want: []string{"level=WARN msg=工具调用被限流拒绝 tool=test_tool"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			callTwice(t, tc.build(bufferLogger(&buf)))
			for _, w := range tc.want {
				require.Contains(t, buf.String(), w)
			}
		})
	}
}

// 未设置时调用日志写入 slog.Default()，显式传 nil 关闭。
func TestContextLoggerDefaults(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var def bytes.Buffer
	slog.SetDefault(bufferLogger(&def))

	callTwice(t, NewLoggingMiddleware().Wrap(newMockTool(), succeed))
	require.Equal(t, 4, strings.Count(def.String(), "msg=\"Tool invocation"))

	def.Reset()
	callTwice(t, NewLoggingMiddleware(WithContextLogger(nil)).Wrap(newMockTool(), succeed))
	require.Empty(t, def.String())

}
