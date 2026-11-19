package observe

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/logging"
	"github.com/kart-io/goagent/core/usage"
)

func textLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func TestLogObserverLevels(t *testing.T) {
	for _, tc := range []struct {
		event Event
		level string
	}{
		{Event{Type: "model_start"}, "DEBUG"},
		{Event{Type: "model_end"}, "INFO"},
		{Event{Type: "tool_replayed"}, "INFO"},
		{Event{Type: "agent_waiting", ErrorKind: "waiting"}, "INFO"},
		{Event{Type: "model_error", ErrorKind: "execution"}, "ERROR"},
		{Event{Type: "node_error", ErrorKind: "panic"}, "ERROR"},
		{Event{Type: "model_error", ErrorKind: "canceled"}, "WARN"},
		{Event{Type: "model_cache_read_error"}, "WARN"},
		{Event{Type: "model_retry_start"}, "WARN"},
		{Event{Type: "model_usage"}, "DEBUG"},
		{Event{Type: "model_cache_hit"}, "DEBUG"},
	} {
		t.Run(tc.event.Type+"/"+tc.event.ErrorKind, func(t *testing.T) {
			var buf bytes.Buffer
			NewLogObserver(textLogger(&buf)).Observe(context.Background(), tc.event)
			require.Equal(t, "level="+tc.level+" msg="+tc.event.Type+orEmpty(tc.event.ErrorKind)+"\n", buf.String())
		})
	}
}

func orEmpty(kind string) string {
	if kind == "" {
		return ""
	}
	return " error_kind=" + kind
}

// 生命周期经 AddObserver 同时投递给既有观察者与日志；日志只含事件元数据，不含原始错误文本。
func TestLogObserverRecordsLifecycleWithoutRawError(t *testing.T) {
	var buf bytes.Buffer
	sink := &collector{}
	ctx := AddObserver(WithObserver(context.Background(), sink), NewLogObserver(textLogger(&buf)))
	ctx = WithRunID(ctx, "run-1")
	ctx = usage.WithInvocation(ctx, usage.Invocation{Owner: usage.Owner{RunID: "run-1", AgentID: "客服"}})
	_, end := Start(ctx, Event{Type: "model", Path: "turn", OperationKey: "op"})
	end(errors.New("secret-provider-error"), false)

	require.Len(t, sink.events, 2)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	require.True(t, strings.HasPrefix(lines[0], "level=DEBUG msg=model_start run_id=run-1 path=turn operation_key=op agent_id=客服"), lines[0])
	require.True(t, strings.HasPrefix(lines[1], "level=ERROR msg=model_error run_id=run-1 path=turn operation_key=op duration="), lines[1])
	require.Contains(t, lines[1], "error_kind=execution")
	require.NotContains(t, buf.String(), "secret")
}

func TestLogObserverWritesUsage(t *testing.T) {
	var buf bytes.Buffer
	ev := &usage.ModelEvidence{Provider: "deepseek", Model: "chat", Attempt: 1}
	ev.Tokens.PromptTokens, ev.Tokens.CompletionTokens, ev.Tokens.TotalTokens = 10, 5, 15
	NewLogObserver(textLogger(&buf)).Observe(context.Background(), Event{Type: "model_usage", Usage: ev})
	require.Equal(t, "level=DEBUG msg=model_usage usage.provider=deepseek usage.model=chat usage.attempt=1 usage.prompt_tokens=10 usage.completion_tokens=5 usage.total_tokens=15\n", buf.String())
}

func TestAddObserverIsolatesPanics(t *testing.T) {
	sink := &collector{}
	ctx := AddObserver(WithObserver(context.Background(), panicObserver{}), sink)
	Emit(ctx, Event{Type: "model_usage"})
	require.Len(t, sink.events, 1)

	only := &collector{}
	Emit(AddObserver(context.Background(), only), Event{Type: "model_usage"})
	require.Len(t, only.events, 1)
}

// 事件不是在某个调用点产生的：处理器开启 AddSource 时也不能把本观察者记成来源。
func TestLogObserverRecordsNoSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		logger func(*bytes.Buffer) logging.Logger
	}{
		{"slog.Logger", func(buf *bytes.Buffer) logging.Logger {
			return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{AddSource: true}))
		}},
		{"logging.Default", func(buf *bytes.Buffer) logging.Logger {
			prev := slog.Default()
			t.Cleanup(func() { slog.SetDefault(prev) })
			slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{AddSource: true})))
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			NewLogObserver(tc.logger(&buf)).Observe(context.Background(), Event{Type: "model_error", ErrorKind: "execution"})
			require.Contains(t, buf.String(), "level=ERROR msg=model_error error_kind=execution")
			require.NotContains(t, buf.String(), "source=")
		})
	}
}
