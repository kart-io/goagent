package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// validLog 用 Recorder、DurableStore 与 Rewind 写出覆盖全部已知类型的日志：
// 完成轮、改写、快照、中止轮、退回与自定义类型。
func validLog(t *testing.T) []Event {
	t.Helper()
	ctx := context.Background()
	store := NewMemoryStore()
	rec, _, err := OpenRecorder(ctx, store, "s")
	require.NoError(t, err)
	turn := func(input string) {
		ev, err := NewEvent(constants.SessionEventTurnStart, TurnStart{Input: input, ExecutionKey: input})
		require.NoError(t, err)
		_, err = rec.Append(ctx, ev)
		require.NoError(t, err)
	}
	end := func(d TurnEnd, surface *Surface) {
		ev, err := NewEvent(constants.SessionEventTurnEnd, d)
		require.NoError(t, err)
		ev.Surface = surface
		_, err = rec.Append(ctx, ev)
		require.NoError(t, err)
	}
	custom, err := NewEvent("host/custom", map[string]int{"n": 1})
	require.NoError(t, err)
	_, err = rec.Append(ctx, custom)
	require.NoError(t, err)

	require.NoError(t, rec.SyncSystem(ctx, "系统"))
	require.NoError(t, rec.SyncTools(ctx, []ToolDefinition{{Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}))
	turn("一")
	tc := llm.ToolCall{ID: "c1", Type: "function"}
	tc.Function.Name, tc.Function.Arguments = "lookup", "{}"
	call := llm.Message{Role: constants.RoleAssistant, ToolCalls: []llm.ToolCall{tc}}
	history := []llm.Message{llm.UserMessage("一"), call, {Role: "tool", ToolCallID: "c1", Content: "结果"}, llm.AssistantMessage("答")}
	require.NoError(t, rec.Sync(ctx, history, map[string]ToolMeta{"c1": {Name: "lookup", Success: true}}))
	require.NoError(t, DurableStore(store, "s").Save(ctx, "op", &durable.Snapshot{Data: json.RawMessage(`{}`)}))
	require.NoError(t, rec.Sync(ctx, []llm.Message{llm.UserMessage("摘要"), llm.AssistantMessage("答")}, nil))
	end(TurnEnd{Answer: "答"}, nil)

	turn("二")
	open := rec.Seq()
	require.NoError(t, rec.Sync(ctx, []llm.Message{llm.UserMessage("摘要"), llm.AssistantMessage("答"), llm.UserMessage("二")}, nil))
	end(TurnEnd{Aborted: true}, &Surface{Start: open, End: rec.Seq()})

	_, err = Rewind(ctx, store, "s", open-1, rec.Seq())
	require.NoError(t, err)
	events, err := store.Read(ctx, "s", 0)
	require.NoError(t, err)
	return events
}

func TestValidateAcceptsWriterOutput(t *testing.T) {
	events := validLog(t)
	var types []string
	for _, ev := range events {
		types = append(types, ev.Type)
	}
	for _, typ := range []string{
		constants.SessionEventTurnStart, constants.SessionEventTurnEnd, constants.SessionEventSystemMessage,
		constants.SessionEventToolDefinitions, constants.SessionEventUserMessage, constants.SessionEventAssistantMessage,
		constants.SessionEventToolResult, constants.SessionEventHistoryRewrite, constants.SessionEventRewind,
		constants.SessionEventBoundarySnapshot, "host/custom",
	} {
		require.Contains(t, types, typ, "样例日志须覆盖全部类型")
	}
	require.NoError(t, Validate(events))
	require.NoError(t, Validate(nil))

	// 截断在轮次中途的前缀（如 Fork 到任意序号）仍然合法：未结束的轮次由下一轮补记中止。
	for n := range events {
		require.NoError(t, Validate(events[:n]), "前缀 %d", n)
	}
}

func TestValidateRejectsCorruptLog(t *testing.T) {
	base := validLog(t)
	find := func(events []Event, typ string, pred func(Event) bool) int {
		for i, ev := range events {
			if ev.Type == typ && (pred == nil || pred(ev)) {
				return i
			}
		}
		t.Fatalf("样例日志缺少 %s", typ)
		return -1
	}
	aborted := func(ev Event) bool { return ev.Surface != nil }
	completed := func(ev Event) bool { return ev.Surface == nil }
	at := func(typ string, pred func(Event) bool, edit func(*Event)) func([]Event) []Event {
		return func(events []Event) []Event {
			edit(&events[find(events, typ, pred)])
			return events
		}
	}
	data := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		require.NoError(t, err)
		return raw
	}
	for _, tc := range []struct {
		name   string
		mutate func([]Event) []Event
		want   string
	}{
		{"序号跳跃", func(e []Event) []Event { return append(e[:2:2], e[3:]...) }, "序号应为 3"},
		{"序号不从 1 开始", func(e []Event) []Event { return e[1:] }, "序号应为 1"},
		{"类型为空", at("host/custom", nil, func(ev *Event) { ev.Type = "" }), "类型为空"},
		{"数据非法", at("host/custom", nil, func(ev *Event) { ev.Data = json.RawMessage(`{`) }), "合法 JSON"},
		{"数据与类型不符", at(constants.SessionEventToolDefinitions, nil, func(ev *Event) { ev.Data = json.RawMessage(`{"tools":1}`) }), "事件"},
		{"助手消息角色错误", at(constants.SessionEventAssistantMessage, nil, func(ev *Event) { ev.Data = data(MessageData{Message: llm.UserMessage("x")}) }), "消息角色"},
		{"工具结果角色错误", at(constants.SessionEventToolResult, nil, func(ev *Event) { ev.Data = data(MessageData{Message: llm.AssistantMessage("x")}) }), "消息角色"},
		{"用户消息记成工具", at(constants.SessionEventUserMessage, nil, func(ev *Event) { ev.Data = data(MessageData{Message: llm.Message{Role: "tool"}}) }), "消息角色"},
		{"系统消息角色错误", at(constants.SessionEventSystemMessage, nil, func(ev *Event) { ev.Data = data(MessageData{Message: llm.UserMessage("x")}) }), "消息角色"},
		{"快照缺 ID", at(constants.SessionEventBoundarySnapshot, nil, func(ev *Event) { ev.Data = data(BoundaryData{}) }), "缺少 ID"},
		{"轮次嵌套", at(constants.SessionEventTurnEnd, completed, func(ev *Event) {
			ev.Type, ev.Data = constants.SessionEventTurnStart, data(TurnStart{})
		}), "尚未结束"},
		{"结束无开始", at(constants.SessionEventTurnStart, nil, func(ev *Event) { ev.Type = "host/custom" }), "没有未结束的轮次"},
		{"中止轮缺 Surface", at(constants.SessionEventTurnEnd, aborted, func(ev *Event) { ev.Surface = nil }), "撤销"},
		{"中止轮撤销范围错位", at(constants.SessionEventTurnEnd, aborted, func(ev *Event) { ev.Surface.Start++ }), "撤销"},
		{"完成轮带 Surface", at(constants.SessionEventTurnEnd, completed, func(ev *Event) { ev.Surface = &Surface{Start: 1, End: 1} }), "完成的轮次"},
		{"消息带 Surface", at(constants.SessionEventUserMessage, nil, func(ev *Event) { ev.Surface = &Surface{Start: 1, End: 1} }), "不能带 Surface"},
		{"改写缺 Surface", at(constants.SessionEventHistoryRewrite, nil, func(ev *Event) { ev.Surface = nil }), "缺少 Surface"},
		{"改写覆盖未来", at(constants.SessionEventHistoryRewrite, nil, func(ev *Event) { ev.Surface.End = ev.Seq }), "之前的非空区间"},
		{"改写区间倒置", at(constants.SessionEventHistoryRewrite, nil, func(ev *Event) { ev.Surface.Start = ev.Surface.End + 1 }), "之前的非空区间"},
		{"改写端点非消息", at(constants.SessionEventHistoryRewrite, nil, func(ev *Event) { ev.Surface.Start = 1 }), "不是消息事件"},
		{"退回缺 Surface", at(constants.SessionEventRewind, nil, func(ev *Event) { ev.Surface = nil }), "缺少 Surface"},
		{"中止轮漏掉本轮消息", at(constants.SessionEventTurnEnd, aborted, func(ev *Event) { ev.Surface.End = ev.Surface.Start }), "未撤销其后的消息"},
		{"退回区间未到本事件前", at(constants.SessionEventRewind, nil, func(ev *Event) { ev.Surface.End-- }), "退回区间须止于"},
		{"未知类型带 Surface", at("host/custom", nil, func(ev *Event) { ev.Surface = &Surface{Start: 1, End: 1} }), "不能带 Surface"},
		{"退回起点为 0", at(constants.SessionEventRewind, nil, func(ev *Event) { ev.Surface.Start = 0 }), "之前的非空区间"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.mutate(cloneEvents(base)))
			require.ErrorIs(t, err, agentErrors.ErrSessionInvalidLog)
			require.ErrorContains(t, err, tc.want)
		})
	}
}
