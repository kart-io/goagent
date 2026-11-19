package logging_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/logging"
)

// slog 的 *Logger 不经适配即满足接口，宿主可直接传入 slog 或内嵌它的 slogx 日志器。
var _ logging.Logger = (*slog.Logger)(nil)

func TestDefaultFollowsLaterSetDefault(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	l := logging.Default()
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx := context.Background()
	l.DebugContext(ctx, "调试", "k", 1)
	l.InfoContext(ctx, "信息")
	l.WarnContext(ctx, "告警")
	l.ErrorContext(ctx, "错误")
	for _, want := range []string{"level=DEBUG msg=调试 k=1", "level=INFO msg=信息", "level=WARN msg=告警", "level=ERROR msg=错误"} {
		require.Contains(t, buf.String(), want)
	}
}

func TestOrDefaultAndDiscard(t *testing.T) {
	custom := slog.New(slog.DiscardHandler)
	require.Same(t, custom, logging.OrDefault(custom))
	require.Equal(t, logging.Default(), logging.OrDefault(nil))

	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	logging.Discard().ErrorContext(context.Background(), "丢弃")
	require.Empty(t, buf.String())
}

// 经 Default() 写出的记录，来源是调用方而不是 logging 包。
func TestDefaultReportsCallerSource(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug})))

	l := logging.Default()
	for _, write := range []func(context.Context, string, ...any){l.DebugContext, l.InfoContext, l.WarnContext, l.ErrorContext} {
		buf.Reset()
		write(context.Background(), "m", "k", "v")
		require.Contains(t, buf.String(), "logging_test.go:")
		require.Contains(t, buf.String(), "msg=m k=v")
	}
}
