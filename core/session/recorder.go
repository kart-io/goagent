package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// Recorder syncs a session's conversation to the journal. It appends only what
// differs from the result derived from the journal: new messages are recorded
// one by one, and rewritten history is recorded as one history/rewrite. Only
// one Recorder should write messages to a session at a time; boundary/snapshot
// events appended by other writers in the meantime are skipped, and events of
// other types make writes return ErrConflict.
type Recorder struct {
	store  Store
	id     string
	seq    int64
	events []Event // 不含 boundary/snapshot
	system string
	tools  string
}

// OpenRecorder reads all events of a session (empty if the session does not
// exist) and returns the Recorder along with the events read.
func OpenRecorder(ctx context.Context, store Store, id string) (*Recorder, []Event, error) {
	if err := validID(id); err != nil {
		return nil, nil, err
	}
	events, err := store.Read(ctx, id, 0)
	if err != nil && !errors.Is(err, agentErrors.ErrSessionNotFound) {
		return nil, nil, err
	}
	r := &Recorder{store: store, id: id}
	r.absorb(events)
	return r, events, nil
}

func (r *Recorder) absorb(events []Event) {
	for _, ev := range cloneEvents(events) {
		r.seq = ev.Seq
		switch ev.Type {
		case constants.SessionEventBoundarySnapshot:
			continue
		case constants.SessionEventSystemMessage:
			var d MessageData
			if json.Unmarshal(ev.Data, &d) == nil {
				r.system = d.Message.Content
			}
		case constants.SessionEventToolDefinitions:
			r.tools = string(ev.Data)
		}
		r.events = append(r.events, ev)
	}
}

// Seq returns the last known sequence number.
func (r *Recorder) Seq() int64 { return r.seq }

// Events returns an independent copy of the known non-snapshot events.
func (r *Recorder) Events() []Event { return cloneEvents(r.events) }

// Append appends events and returns them with assigned sequence numbers.
func (r *Recorder) Append(ctx context.Context, events ...Event) ([]Event, error) {
	if len(events) == 0 {
		return nil, nil
	}
	for {
		last, err := r.store.Append(ctx, r.id, r.seq, events...)
		if err == nil {
			added := assign(events, last-int64(len(events)))
			r.absorb(added)
			return added, nil
		}
		if !errors.Is(err, agentErrors.ErrSessionConflict) {
			return nil, err
		}
		newer, rerr := r.store.Read(ctx, r.id, r.seq)
		if rerr != nil {
			return nil, rerr
		}
		for _, ev := range newer {
			if ev.Type != constants.SessionEventBoundarySnapshot {
				return nil, fmt.Errorf("%w：序号 %d 处出现了其他写入者的 %s", agentErrors.ErrSessionConflict, ev.Seq, ev.Type)
			}
		}
		if len(newer) == 0 {
			return nil, err
		}
		r.absorb(newer)
	}
}

// SyncSystem records the system prompt when it differs from the last recorded
// one.
func (r *Recorder) SyncSystem(ctx context.Context, system string) error {
	if system == r.system {
		return nil
	}
	ev, err := NewEvent(constants.SessionEventSystemMessage, MessageData{Message: llm.SystemMessage(system)})
	if err != nil {
		return err
	}
	_, err = r.Append(ctx, ev)
	return err
}

// SyncTools records the tool definitions when they differ from the last
// recorded ones.
func (r *Recorder) SyncTools(ctx context.Context, tools []ToolDefinition) error {
	if len(tools) == 0 {
		if r.tools == "" {
			return nil
		}
		// nil 与空切片都表示移除工具，统一编码以避免重复清空事件。
		tools = []ToolDefinition{}
	}
	ev, err := NewEvent(constants.SessionEventToolDefinitions, ToolsData{Tools: tools})
	if err != nil || string(ev.Data) == r.tools {
		return err
	}
	_, err = r.Append(ctx, ev)
	return err
}

// Sync makes the conversation derived from the journal equal to messages
// (excluding system). tools gives tool result information keyed by tool call
// ID.
func (r *Recorder) Sync(ctx context.Context, messages []llm.Message, tools map[string]ToolMeta) error {
	entries, err := Derive(r.events)
	if err != nil {
		return err
	}
	p := 0
	for p < len(entries) && p < len(messages) && sameMessage(entries[p].Message, messages[p]) {
		p++
	}
	data := func(m llm.Message) MessageData {
		d := MessageData{Message: m}
		if meta, ok := tools[m.ToolCallID]; ok && m.Role == "tool" {
			d.Tool = &meta
		}
		return d
	}
	var events []Event
	if p < len(entries) {
		// 改写从被改写的第一条消息所在事件开始，替代消息包含该事件中未改动的部分。
		start := p
		for start > 0 && entries[start-1].Seq == entries[p].Seq {
			start--
		}
		rewrite := RewriteData{}
		for _, m := range messages[start:] {
			rewrite.Messages = append(rewrite.Messages, data(m))
		}
		ev, err := NewEvent(constants.SessionEventHistoryRewrite, rewrite)
		if err != nil {
			return err
		}
		ev.Surface = &Surface{Start: entries[start].Seq, End: entries[len(entries)-1].Seq}
		events = append(events, ev)
	} else {
		for _, m := range messages[p:] {
			typ := constants.SessionEventUserMessage
			switch m.Role {
			case constants.RoleAssistant:
				typ = constants.SessionEventAssistantMessage
			case constants.RoleTool:
				typ = constants.SessionEventToolResult
			}
			ev, err := NewEvent(typ, data(m))
			if err != nil {
				return err
			}
			events = append(events, ev)
		}
	}
	_, err = r.Append(ctx, events...)
	return err
}

// Replayed reports whether messages equals the conversation derived from the
// journal at some event whose sequence number is not less than from: a
// recovering durable run that has replayed up to a previously recorded position
// does not need to record again.
func (r *Recorder) Replayed(from int64, messages []llm.Message) (bool, error) {
	for i, ev := range r.events {
		if ev.Seq < from {
			continue
		}
		derived, err := DeriveMessages(r.events[:i+1])
		if err != nil {
			return false, err
		}
		if len(derived) == len(messages) && sameMessages(derived, messages) {
			return true, nil
		}
	}
	return false, nil
}

func sameMessages(a, b []llm.Message) bool {
	for i := range a {
		if !sameMessage(a[i], b[i]) {
			return false
		}
	}
	return true
}

// sameMessage 按 JSON 编码比较：日志中的消息经过编解码，空切片与 nil 等差异不算改写。
func sameMessage(a, b llm.Message) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// Rewind rewinds the session to the state where messages after sequence number
// to are masked. expected is the last sequence number the caller saw (CAS): if
// new writes happened in between it returns ErrConflict, and the caller should
// look again before deciding. Masked events remain in the journal, and durable
// operation snapshots are unaffected.
func Rewind(ctx context.Context, store Store, id string, to, expected int64) (int64, error) {
	if to < 0 || to >= expected {
		return 0, agentErrors.Newf(agentErrors.CodeInvalidInput, "退回位置 %d 须在 [0, %d) 内", to, expected).WithComponent("session_recorder").WithOperation("rewind")
	}
	ev, err := NewEvent(constants.SessionEventRewind, struct {
		To int64 `json:"to"`
	}{to})
	if err != nil {
		return 0, err
	}
	ev.Surface = &Surface{Start: to + 1, End: expected}
	return store.Append(ctx, id, expected, ev)
}
