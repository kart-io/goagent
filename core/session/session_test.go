package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

func msgEvent(t *testing.T, m llm.Message) Event {
	t.Helper()
	typ := map[string]string{"user": constants.SessionEventUserMessage, "assistant": constants.SessionEventAssistantMessage, "tool": constants.SessionEventToolResult}[m.Role]
	ev, err := NewEvent(typ, MessageData{Message: m})
	require.NoError(t, err)
	return ev
}

// 写入中崩溃留下的不完整行被忽略，下次追加前截掉。
func TestFileStoreTornTail(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	require.NoError(t, err)
	_, err = s.Append(ctx, "s", 0, msgEvent(t, llm.UserMessage("一")))
	require.NoError(t, err)
	f, err := os.OpenFile(s.path("s"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(`[{"seq":2,"type":"user/mess`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	fresh, err := NewFileStore(dir)
	require.NoError(t, err)
	events, err := fresh.Read(ctx, "s", 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	last, err := fresh.Append(ctx, "s", 1, msgEvent(t, llm.UserMessage("二")))
	require.NoError(t, err)
	require.EqualValues(t, 2, last)
	events, err = s.Read(ctx, "s", 0)
	require.NoError(t, err)
	require.Len(t, events, 2)

	data, err := os.ReadFile(filepath.Join(dir, digest("s")+".jsonl"))
	require.NoError(t, err)
	require.Equal(t, 3, strings.Count(string(data), "\n"), "header plus two batches; the torn line is gone")
}

func TestDerive(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	r, _, err := OpenRecorder(ctx, s, "s")
	require.NoError(t, err)
	call := llm.ToolCall{ID: "c1", Type: "function"}
	call.Function.Name, call.Function.Arguments = "lookup", `{"q":"x"}`
	convo := []llm.Message{
		llm.UserMessage("问题"), {Role: "assistant", ToolCalls: []llm.ToolCall{call}}, {Role: "tool", ToolCallID: "c1", Content: "结果"},
	}
	require.NoError(t, r.Sync(ctx, convo, map[string]ToolMeta{"c1": {Name: "lookup", Success: true}}))
	require.EqualValues(t, 3, r.Seq())
	require.NoError(t, r.Sync(ctx, convo, nil), "an unchanged conversation appends nothing")
	require.EqualValues(t, 3, r.Seq())

	// 压缩：前两条替换为一条摘要，之后追加新消息。
	compacted := []llm.Message{llm.UserMessage("[摘要] 查过 x"), convo[2], {Role: "assistant", Content: "答"}}
	require.NoError(t, r.Sync(ctx, compacted, nil))
	events, err := s.Read(ctx, "s", 0)
	require.NoError(t, err)
	require.Equal(t, constants.SessionEventHistoryRewrite, events[3].Type)
	require.Equal(t, &Surface{Start: 1, End: 3}, events[3].Surface)
	got, err := DeriveMessages(events)
	require.NoError(t, err)
	require.Equal(t, compacted, got)

	// 改写落在上次改写的中间时，从那次改写开始整体替换。
	again := []llm.Message{compacted[0], {Role: "tool", ToolCallID: "c1", Content: "截断的结果"}, compacted[2]}
	require.NoError(t, r.Sync(ctx, again, nil))
	events, _ = s.Read(ctx, "s", 0)
	got, _ = DeriveMessages(events)
	require.Equal(t, again, got)
	require.Equal(t, &Surface{Start: 4, End: 4}, events[len(events)-1].Surface)

	// 退回到第 4 个事件之后：最后一次改写被遮蔽。
	_, err = Rewind(ctx, s, "s", 4, 4)
	require.ErrorContains(t, err, "须在")
	_, err = Rewind(ctx, s, "s", 3, 4)
	require.ErrorIs(t, err, agentErrors.ErrSessionConflict, "a stale view cannot rewind")
	_, err = Rewind(ctx, s, "s", 4, LastSeq(events))
	require.NoError(t, err)
	events, _ = s.Read(ctx, "s", 0)
	got, _ = DeriveMessages(events)
	require.Equal(t, compacted, got)
}

func TestRecorderToleratesSnapshots(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	r, _, err := OpenRecorder(ctx, s, "s")
	require.NoError(t, err)
	require.NoError(t, r.Sync(ctx, []llm.Message{llm.UserMessage("一")}, nil))
	ds := DurableStore(s, "s")
	require.NoError(t, ds.Save(ctx, "op", &durable.Snapshot{Version: "v", RunID: "op"}))
	require.NoError(t, r.Sync(ctx, []llm.Message{llm.UserMessage("一"), {Role: "assistant", Content: "二"}}, nil), "durable snapshots in between are skipped")
	snap, err := ds.Load(ctx, "op")
	require.NoError(t, err)
	require.Equal(t, "v", snap.Version)

	other, _, err := OpenRecorder(ctx, s, "s")
	require.NoError(t, err)
	require.NoError(t, other.Sync(ctx, []llm.Message{llm.UserMessage("一"), {Role: "assistant", Content: "二"}, llm.UserMessage("他人")}, nil))
	err = r.Sync(ctx, []llm.Message{llm.UserMessage("一"), {Role: "assistant", Content: "二"}, llm.UserMessage("我")}, nil)
	require.ErrorIs(t, err, agentErrors.ErrSessionConflict, "messages from another writer are a conflict")
	_, err = ds.Load(ctx, "missing")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestExport(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	r, _, err := OpenRecorder(ctx, s, "s")
	require.NoError(t, err)
	require.NoError(t, r.SyncSystem(ctx, "你是助手"))
	require.NoError(t, r.SyncTools(ctx, []ToolDefinition{{Name: "lookup", Description: "查询", Parameters: json.RawMessage(`{"type":"object"}`)}}))
	c1 := llm.ToolCall{ID: "c1", Type: "function"}
	c1.Function.Name, c1.Function.Arguments = "lookup", `{"q":"a"}`
	c2 := llm.ToolCall{ID: "c2", Type: "function"}
	c2.Function.Name, c2.Function.Arguments = "lookup", `{"q":"b"}`
	convo := []llm.Message{
		llm.UserMessage("查 a 和 b"),
		{Role: "assistant", ToolCalls: []llm.ToolCall{c1, c2}},
		{Role: "tool", ToolCallID: "c1", Content: "A"}, {Role: "tool", ToolCallID: "c2", Content: "not found"},
		llm.UserMessage("顺便说明来源"),
		{Role: "assistant", Content: "a 是 A，b 没查到"},
	}
	require.NoError(t, r.Sync(ctx, convo, map[string]ToolMeta{"c1": {Name: "lookup", Success: true}, "c2": {Name: "lookup"}}))
	end, err := NewEvent(constants.SessionEventTurnEnd, TurnEnd{Answer: "a 是 A，b 没查到"})
	require.NoError(t, err)
	_, err = r.Append(ctx, end)
	require.NoError(t, err)

	events, err := s.Read(ctx, "s", 0)
	require.NoError(t, err)
	traj, err := BuildTrajectory(events)
	require.NoError(t, err)
	require.Equal(t, 1, traj.Turns)
	require.Equal(t, []ToolStat{{Name: "lookup", Calls: 2, Failures: 1}}, traj.Stats)

	var openai bytes.Buffer
	require.NoError(t, traj.WriteOpenAI(&openai))
	var line struct {
		Messages []map[string]any `json:"messages"`
		Tools    []map[string]any `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(openai.Bytes(), &line))
	require.Equal(t, "system", line.Messages[0]["role"])
	require.Len(t, line.Messages, 7)
	require.Equal(t, "c2", line.Messages[4]["tool_call_id"])
	require.Equal(t, "function", line.Tools[0]["type"])
	require.Equal(t, "lookup", line.Tools[0]["function"].(map[string]any)["name"])

	var sharegpt bytes.Buffer
	require.NoError(t, traj.WriteShareGPT(&sharegpt))
	var sg struct {
		Conversations []shareGPTTurn `json:"conversations"`
		System        string         `json:"system"`
		Tools         string         `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(sharegpt.Bytes(), &sg))
	require.Equal(t, "你是助手", sg.System)
	var from []string
	for _, c := range sg.Conversations {
		from = append(from, c.From)
	}
	require.Equal(t, []string{"human", "function_call", "observation", "gpt"}, from, "user and model sides alternate")
	var calls []map[string]any
	require.NoError(t, json.Unmarshal([]byte(sg.Conversations[1].Value), &calls))
	require.Equal(t, "lookup", calls[1]["name"])
	require.Equal(t, map[string]any{"q": "b"}, calls[1]["arguments"])
	require.Contains(t, sg.Conversations[2].Value, "顺便说明来源", "input steered after tool results is merged into the observation")
	var defs []ToolDefinition
	require.NoError(t, json.Unmarshal([]byte(sg.Tools), &defs))
	require.Equal(t, "lookup", defs[0].Name)

	empty := &Trajectory{Messages: []llm.Message{{Role: "assistant", Content: "x"}}}
	require.Error(t, empty.WriteShareGPT(&bytes.Buffer{}))
}

func TestLastStateSkipsUndoneTurns(t *testing.T) {
	end := func(seq int64, state string, aborted bool, surface *Surface) Event {
		d := TurnEnd{Aborted: aborted}
		if state != "" {
			d.State = json.RawMessage(state)
		}
		ev, err := NewEvent(constants.SessionEventTurnEnd, d)
		require.NoError(t, err)
		ev.Seq, ev.Surface = seq, surface
		return ev
	}
	rewind := Event{Seq: 4, Type: constants.SessionEventRewind, Surface: &Surface{Start: 2, End: 3}}
	tests := []struct {
		name   string
		events []Event
		state  string
		seq    int64
	}{
		{"没有状态", []Event{end(1, "", false, nil)}, "", 0},
		{"取最后一条", []Event{end(1, `{"n":1}`, false, nil), end(2, "", false, nil), end(3, `{"n":3}`, false, nil)}, `{"n":3}`, 3},
		{"退回撤销后续轮次", []Event{end(1, `{"n":1}`, false, nil), end(3, `{"n":3}`, false, nil), rewind}, `{"n":1}`, 1},
		{"中止的轮次不带状态", []Event{end(1, `{"n":1}`, false, nil), end(2, `{"n":2}`, true, &Surface{Start: 2, End: 2})}, `{"n":1}`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, seq, err := LastState(tt.events)
			require.NoError(t, err)
			require.Equal(t, tt.state, string(state))
			require.Equal(t, tt.seq, seq)
		})
	}
}

// 退回恢复的是退回位置当时的视图：退回到更早一次退回之前时，那次退回遮蔽的消息重新可见；被退回的轮次不计入导出。
// 每轮是一条用户消息加一个 turn/end，rewind 的参数是事件序号。
func TestRewindRestoresViewAtTarget(t *testing.T) {
	tests := []struct {
		name  string
		steps func(add func(int), rewind func(to int64))
		msgs  []string
		turns int
	}{
		{"单次退回", func(add func(int), rewind func(int64)) { add(2); rewind(2) }, []string{"m1"}, 1},
		{"退回到上次退回之前", func(add func(int), rewind func(int64)) { add(5); rewind(4); add(2); rewind(10) },
			[]string{"m1", "m2", "m3", "m4", "m5"}, 5},
		{"退回到上次退回之后", func(add func(int), rewind func(int64)) { add(3); rewind(2); add(2); rewind(9) },
			[]string{"m1", "m4"}, 2},
		{"连续退回", func(add func(int), rewind func(int64)) { add(3); rewind(4); rewind(2) }, []string{"m1"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := NewMemoryStore()
			var last int64
			n := 0
			appendEvent := func(ev Event) {
				var err error
				last, err = s.Append(ctx, "s", last, ev)
				require.NoError(t, err)
			}
			add := func(count int) {
				for range count {
					n++
					appendEvent(msgEvent(t, llm.UserMessage("m"+strconv.Itoa(n))))
					end, err := NewEvent(constants.SessionEventTurnEnd, TurnEnd{})
					require.NoError(t, err)
					appendEvent(end)
				}
			}
			rewind := func(to int64) {
				var err error
				last, err = Rewind(ctx, s, "s", to, last)
				require.NoError(t, err)
			}
			tt.steps(add, rewind)
			events, err := s.Read(ctx, "s", 0)
			require.NoError(t, err)
			traj, err := BuildTrajectory(events)
			require.NoError(t, err)
			var got []string
			for _, m := range traj.Messages {
				got = append(got, m.Content)
			}
			require.Equal(t, tt.msgs, got)
			require.Equal(t, tt.turns, traj.Turns)
		})
	}
}
