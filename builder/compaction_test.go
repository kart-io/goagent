package builder

import (
	"context"
	"strings"
	"testing"

	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func callToolWith(id, name, args string) step {
	return step{run: func(_ context.Context, send func(*llm.StreamChunk) bool) {
		send(&llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall(id, name, args)}})
	}}
}

// 跨轮次压缩：日志保留原文，下一轮的请求只含摘要；recall_history 能取回本会话被压缩的原文，取不到其他会话的。
func TestCompactionAcrossSessionTurnsAndRecall(t *testing.T) {
	ctx := context.Background()
	log := session.NewMemoryStore()
	c, err := contextpolicy.NewCompaction(contextpolicy.CompactionConfig{MaxTokens: 60, KeepTokens: 20, Estimate: func(m llm.Message) int { return len([]rune(m.Content)) }})
	require.NoError(t, err)
	recall, err := contextpolicy.NewRecallTool(log)
	require.NoError(t, err)
	model := newScriptModel(
		answer(strings.Repeat("答", 30)),
		answer("第二轮回答"),
		callToolWith("r1", "recall_history", `{"query":"秘密编号"}`), answer("编号是 42"),
		callToolWith("r2", "recall_history", `{"query":"秘密编号"}`), answer("没有记录"),
	)
	agent, err := NewSimpleBuilder(model).WithTools(recall).WithSessionLog(log).WithSessionID("s").WithCompaction(c).WithMaxIterations(3).Build()
	require.NoError(t, err)

	_, err = agent.AsRunnable().Invoke(ctx, "秘密编号是 42，"+strings.Repeat("补", 30))
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "第二个问题")
	require.NoError(t, err)
	second := model.request(1).Messages
	require.True(t, slicesContainSummary(second), "第二轮请求应含摘要")
	for _, m := range second {
		require.NotContains(t, m.Content, "秘密编号", "被压缩的原文不应再发给模型")
	}

	out, err := agent.AsRunnable().Invoke(ctx, "编号是多少")
	require.NoError(t, err)
	require.Equal(t, "编号是 42", out.Result)
	result := lastMessages(model.request(3), 1)[0]
	require.Equal(t, "tool", result.Role)
	require.Contains(t, result.Content, "秘密编号是 42")

	_, err = agent.AsRunnable().Invoke(tools.WithSessionID(ctx, "other"), "编号是多少")
	require.NoError(t, err)
	other := lastMessages(model.request(5), 1)[0]
	require.NotContains(t, other.Content, "秘密编号", "不能读到其他会话的原文")

	events, err := log.Read(ctx, "s", 0)
	require.NoError(t, err)
	archived, err := session.Archived(events)
	require.NoError(t, err)
	require.NotEmpty(t, archived, "原文仍在日志中")
}

func slicesContainSummary(messages []llm.Message) bool {
	for _, m := range messages {
		if contextpolicy.IsSummary(m) {
			return true
		}
	}
	return false
}
