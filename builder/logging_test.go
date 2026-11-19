package builder

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/llm"
)

func debugLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func runToEnd(t *testing.T, agent *ConfigurableAgent[*core.AgentState], ctx context.Context) {
	t.Helper()
	events, err := agent.ExecuteStream(ctx, "q")
	require.NoError(t, err)
	for range events {
	}
}

func TestWithLoggerRecordsRunLifecycle(t *testing.T) {
	var buf bytes.Buffer
	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "ok"}, {Done: true, FinishReason: "stop"}}}
	model.chunks[0].Delta = "模型答复正文"
	agent, err := NewSimpleBuilder(model).WithLogger(debugLogger(&buf)).Build()
	require.NoError(t, err)
	sink := &orderedSink{}
	runToEnd(t, agent, observe.WithObserver(context.Background(), sink))

	logged := buf.String()
	for _, typ := range []string{"agent_start", "model_start", "model_end", "agent_end"} {
		require.Contains(t, logged, "msg="+typ+" ", "日志缺少 %s：\n%s", typ, logged)
		require.Contains(t, sink.seq, typ, "宿主已绑定的观察者仍须收到 %s", typ)
	}
	require.Contains(t, logged, "level=INFO msg=agent_end ")
	require.NotContains(t, logged, "模型答复正文", "日志只含元数据，不含模型输出")
}

func TestWithoutLoggerDoesNotLogRun(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	slog.SetDefault(debugLogger(&buf))

	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "ok"}, {Done: true, FinishReason: "stop"}}}
	agent, err := NewSimpleBuilder(model).Build()
	require.NoError(t, err)
	runToEnd(t, agent, context.Background())
	require.Empty(t, buf.String())
}

// 嵌套 Agent 继承外层 ctx：外层已绑定日志时内层不再追加，同一事件只记一条。
func TestBindLoggerOncePerRun(t *testing.T) {
	var outer, inner bytes.Buffer
	a := &ConfigurableAgent[*core.AgentState]{config: &AgentConfig{Logger: debugLogger(&outer)}}
	b := &ConfigurableAgent[*core.AgentState]{config: &AgentConfig{Logger: debugLogger(&inner)}}
	ctx := b.bindLogger(a.bindLogger(context.Background()))
	observe.Emit(ctx, observe.Event{Type: "model_usage"})
	require.Equal(t, 1, strings.Count(outer.String(), "msg=model_usage"))
	require.Empty(t, inner.String())
}
