package contextpolicy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

type recallResult struct {
	Messages []struct {
		Seq     int64  `json:"seq"`
		Role    string `json:"role"`
		Content string `json:"content"`
		Tool    string `json:"tool"`
	} `json:"messages"`
	NextBefore int64 `json:"next_before"`
}

// archivedSession 写入 4 条消息（第 3 条是工具结果），再把第 1~4 条改写为一条摘要。
func archivedSession(t *testing.T, store session.Store, id string) {
	t.Helper()
	msg := func(typ, role, content string, tool *session.ToolMeta) session.Event {
		e, err := session.NewEvent(typ, session.MessageData{Message: llm.Message{Role: role, Content: content}, Tool: tool})
		require.NoError(t, err)
		return e
	}
	rewrite, err := session.NewEvent(constants.SessionEventHistoryRewrite, session.RewriteData{
		Messages: []session.MessageData{{Message: llm.Message{Role: "user", Content: "摘要"}}},
	})
	require.NoError(t, err)
	rewrite.Surface = &session.Surface{Start: 1, End: 4}
	_, err = store.Append(context.Background(), id, 0,
		msg(constants.SessionEventUserMessage, "user", "苹果 一", nil),
		msg(constants.SessionEventAssistantMessage, "assistant", "香蕉 二", nil),
		msg(constants.SessionEventToolResult, "tool", "苹果 三", &session.ToolMeta{Name: "fetch", Success: true}),
		msg(constants.SessionEventUserMessage, "user", "苹果 四号很长的内容", nil),
		rewrite,
	)
	require.NoError(t, err)
}

func TestRecallToolInvoke(t *testing.T) {
	store := session.NewMemoryStore()
	archivedSession(t, store, "s1")
	r, err := NewRecallToolConfig(store, RecallConfig{MaxLimit: 2, MaxRunes: 4})
	require.NoError(t, err)
	ctx := tools.WithSessionID(context.Background(), "s1")

	invoke := func(args map[string]any) recallResult {
		t.Helper()
		out, err := r.Invoke(ctx, &interfaces.ToolInput{Args: args})
		require.NoError(t, err)
		require.True(t, out.Success)
		var res recallResult
		require.NoError(t, json.Unmarshal([]byte(out.Result.(string)), &res))
		return res
	}
	seqs := func(res recallResult) []int64 {
		var s []int64
		for _, m := range res.Messages {
			s = append(s, m.Seq)
		}
		return s
	}

	// limit 被 MaxLimit 收紧为 2：取最近两条，按序号升序返回，next_before 指向更早的分页。
	first := invoke(map[string]any{"limit": 50})
	require.Equal(t, []int64{3, 4}, seqs(first))
	require.Equal(t, int64(3), first.NextBefore)
	require.Equal(t, "fetch", first.Messages[0].Tool)
	require.Equal(t, "苹果 四…（已截断）", first.Messages[1].Content, "MaxRunes 按字符截断")

	second := invoke(map[string]any{"before": float64(first.NextBefore)})
	require.Equal(t, []int64{1, 2}, seqs(second))
	require.Zero(t, second.NextBefore, "没有更早的消息")

	require.Equal(t, []int64{1, 3}, seqs(invoke(map[string]any{"query": "苹果", "before": int64(4)})))
	require.Empty(t, invoke(map[string]any{"query": "不存在"}).Messages)

	// 只读取上下文中的会话：s2 没有被改写的消息，取不到 s1 的原文。
	e, err := session.NewEvent(constants.SessionEventUserMessage, session.MessageData{Message: llm.Message{Role: "user", Content: "苹果"}})
	require.NoError(t, err)
	_, err = store.Append(context.Background(), "s2", 0, e)
	require.NoError(t, err)
	out, err := r.Invoke(tools.WithSessionID(context.Background(), "s2"), nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"messages":null,"next_before":0}`, out.Result.(string))
}

func TestRecallToolRequiresSessionID(t *testing.T) {
	r, err := NewRecallTool(session.NewMemoryStore())
	require.NoError(t, err)
	_, err = r.Invoke(context.Background(), &interfaces.ToolInput{})
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))

	require.Equal(t, constants.ToolRecallHistory, r.Name())
	require.True(t, r.SupportsConcurrentCalls())
	require.NotEmpty(t, r.Description())

	_, err = NewRecallTool(nil)
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
}

func TestIntArg(t *testing.T) {
	for _, c := range []struct {
		in   any
		want int64
	}{{int64(3), 3}, {5, 5}, {float64(7.9), 7}, {"8", 0}, {nil, 0}} {
		require.Equal(t, c.want, intArg(c.in), "%#v", c.in)
	}
}
