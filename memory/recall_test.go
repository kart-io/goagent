package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

func TestSnapshotHydrateRefsOnly(t *testing.T) {
	ctx := WithNamespace(context.Background(), "t1", "alice")
	m := newMemory(t, Config{})
	coffee, err := m.Remember(ctx, Record{Content: "用户喜欢喝美式咖啡", Importance: 0.8})
	require.NoError(t, err)
	_, err = m.Remember(ctx, Record{Content: "用户每天早上喝咖啡", Importance: 0.5, Pinned: true})
	require.NoError(t, err)

	s, err := m.Snapshot(ctx, "咖啡")
	require.NoError(t, err)
	require.Equal(t, RecallProtocol, s.Protocol)
	require.Equal(t, TurnMemory{Write: []string{"t1", "alice"}, Read: [][]string{{"t1", "alice"}}, Scope: initialScope, Seq: 2}, s.Turn, "绑定记下当前作用域与两次写入后的提交序号")
	require.Len(t, s.Refs, 2)
	encoded, err := json.Marshal(s)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "咖啡", "快照只含引用")

	got, adopted, err := m.Hydrate(ctx, s, s.Refs)
	require.NoError(t, err)
	require.Equal(t, s.Refs, adopted)
	require.True(t, strings.HasPrefix(got.Text, RecallPrefix))
	require.Contains(t, got.Text, "[核心", "钉住的记录在前")
	block, err := m.RecallBlock(ctx, "咖啡")
	require.NoError(t, err)
	require.Equal(t, block, got.Text, "Retrieve 等同于 Snapshot 后 Hydrate")

	require.NoError(t, m.Forget(ctx, coffee.ID))
	got, adopted, err = m.Hydrate(ctx, s, s.Refs)
	require.NoError(t, err)
	require.Len(t, adopted, 1, "已遗忘的记录被丢弃")
	require.NotContains(t, got.Text, "美式咖啡")
}

func TestHydrateBindings(t *testing.T) {
	alice := WithNamespace(context.Background(), "t1", "alice")
	m := newMemory(t, Config{})
	_, err := m.Remember(alice, Record{Content: "用户喜欢喝美式咖啡", Importance: 0.8})
	require.NoError(t, err)
	s, err := m.Snapshot(alice, "咖啡")
	require.NoError(t, err)

	cases := []struct {
		name    string
		ctx     context.Context
		snap    func(Snapshot) Snapshot
		err     error
		adopted int
	}{
		{"命名空间改变", WithNamespace(context.Background(), "t1", "bob"), func(s Snapshot) Snapshot { return s }, agentErrors.ErrMemoryNamespaceChanged, 0},
		{"空快照同样核对命名空间", WithNamespace(context.Background(), "t1", "bob"), func(s Snapshot) Snapshot { s.Refs = nil; return s }, agentErrors.ErrMemoryNamespaceChanged, 0},
		{"协议改变时召回为空", alice, func(s Snapshot) Snapshot { s.Protocol = "old"; return s }, nil, 0},
		{"召回配置改变时召回为空", alice, func(s Snapshot) Snapshot { s.Identity = "other"; return s }, nil, 0},
		{"不在读绑定内的引用被丢弃", alice, func(s Snapshot) Snapshot { s.Turn.Read = nil; return s }, nil, 0},
		{"绑定一致", alice, func(s Snapshot) Snapshot { return s }, nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := tc.snap(s)
			_, adopted, err := m.Hydrate(tc.ctx, snap, snap.Refs)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Len(t, adopted, tc.adopted)
		})
	}
}

func TestDigestBindsSubsetAndNamespace(t *testing.T) {
	s := Snapshot{Protocol: RecallProtocol, Identity: "id", Turn: TurnMemory{Write: []string{"a"}}}
	refs := []Ref{{Kind: RefRecord, Namespace: []string{"a"}, ID: "1"}}
	d1, err := s.Digest(refs)
	require.NoError(t, err)
	d2, err := s.Digest(refs)
	require.NoError(t, err)
	require.Equal(t, d1, d2)
	other := s
	other.Turn.Write = []string{"b"}
	d3, err := other.Digest(refs)
	require.NoError(t, err)
	require.NotEqual(t, d1, d3)
	d4, err := s.Digest(nil)
	require.NoError(t, err)
	require.NotEqual(t, d1, d4)
}

// 轮次绑定优先于宿主之后的绑定：记忆工具在轮次内写入轮次开始时的命名空间。
func TestTurnBindingScopesWrites(t *testing.T) {
	m := newMemory(t, Config{})
	alice := WithNamespace(context.Background(), "t1", "alice")
	turn, err := m.Turn(alice)
	require.NoError(t, err)
	ctx := WithTurn(WithNamespace(context.Background(), "t1", "bob"), turn)
	_, err = m.Remember(ctx, Record{Content: "用户住在杭州"})
	require.NoError(t, err)
	got, err := m.Recall(alice, Query{Text: "杭州"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NoError(t, CheckTurn(alice, turn))
	require.ErrorIs(t, CheckTurn(WithNamespace(context.Background(), "t1", "bob"), turn), agentErrors.ErrMemoryNamespaceChanged)
}

func TestRenderTurnSkipsMemoryTools(t *testing.T) {
	var events []session.Event
	add := func(typ string, d session.MessageData) {
		ev, err := session.NewEvent(typ, d)
		require.NoError(t, err)
		ev.Seq = int64(len(events) + 1)
		events = append(events, ev)
	}
	add(constants.SessionEventUserMessage, session.MessageData{Message: llm.UserMessage("我喜欢什么")})
	call := func(id, name, args string) llm.ToolCall {
		c := llm.ToolCall{ID: id, Type: "function"}
		c.Function.Name, c.Function.Arguments = name, args
		return c
	}
	add(constants.SessionEventAssistantMessage, session.MessageData{Message: llm.Message{Role: "assistant", Content: "我查一下", ToolCalls: []llm.ToolCall{
		call("m1", constants.ToolMemorySearch, `{"query":"喜欢"}`), call("w1", "weather", `{"city":"杭州"}`),
	}}})
	add(constants.SessionEventToolResult, session.MessageData{Message: llm.Message{Role: "tool", ToolCallID: "m1", Content: "用户喜欢美式咖啡"}})
	add(constants.SessionEventToolResult, session.MessageData{Message: llm.Message{Role: "tool", ToolCallID: "w1", Content: "晴"}, Tool: &session.ToolMeta{Name: "weather", Success: true}})
	add(constants.SessionEventUserMessage, session.MessageData{Message: llm.UserMessage(RecallPrefix + "旧召回")})

	text := renderTurn(events, 1, int64(len(events)))
	require.NotContains(t, text, "美式咖啡", "记忆工具的结果不进入抽取")
	require.NotContains(t, text, constants.ToolMemorySearch)
	require.NotContains(t, text, "旧召回")
	require.Contains(t, text, "我查一下", "同一条消息的普通正文保留")
	require.Contains(t, text, "weather")
	require.Contains(t, text, "晴")
}
