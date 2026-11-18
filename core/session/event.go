// Package session is an append-only session event journal: the conversation the
// model sees, turn boundaries, and durable operation snapshots are all recorded
// in the same journal. DeriveMessages derives the model history from the
// journal; DurableStore uses the journal as the storage for core/durable, so
// recovery and replay are based on the same journal. See sections 6 and 16 of
// docs/design/HARNESS_CORE_DESIGN.md for the design.
package session

import (
	"fmt"
	"slices"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// Event is an event in the journal. Seq is assigned by the Store and increases
// consecutively from 1.
type Event struct {
	Seq  int64           `json:"seq"`
	Type string          `json:"type"`
	Time time.Time       `json:"time"`
	Data json.RawMessage `json:"data,omitempty"`
	// Surface, when non-nil, masks message events whose sequence numbers are
	// within [Start, End]; the event's own replacement messages appear in the
	// masked position.
	Surface *Surface `json:"surface,omitzero"`
}

// Surface is a closed interval of masked event sequence numbers.
type Surface struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

func (s *Surface) covers(seq int64) bool { return s != nil && seq >= s.Start && seq <= s.End }

// TurnStart is the data of turn/start.
type TurnStart struct {
	Input string `json:"input"`
	// ExecutionKey is the execution key the host passed to the durable run
	// (before DurableScope derivation); resuming with the same execution key
	// continues this turn.
	ExecutionKey string `json:"execution_key,omitempty"`
	// Memory is this turn's long-term memory binding (an encoded
	// memory.TurnMemory); it is authoritative when resuming this turn and when
	// registering tasks for aborted turns.
	Memory json.RawMessage `json:"memory,omitempty"`
}

// TurnEnd is the data of turn/end.
type TurnEnd struct {
	Answer  string      `json:"answer,omitempty"`
	Aborted bool        `json:"aborted,omitzero"`
	Usage   *TokenUsage `json:"usage,omitzero"`
	// State is the encoded host state at the end of this turn, written by the
	// Builder's WithSessionState.
	State json.RawMessage `json:"state,omitempty"`
}

// TokenUsage is the token usage of a turn.
type TokenUsage = interfaces.TokenUsage

// MessageData is the data of a message event. Tool appears only in tool/result.
type MessageData struct {
	Message llm.Message `json:"message"`
	Tool    *ToolMeta   `json:"tool,omitzero"`
}

// ToolMeta is the result information of one tool call, used for export
// statistics.
type ToolMeta struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
}

// ToolsData is the data of tool/definitions.
type ToolsData struct {
	Tools []ToolDefinition `json:"tools"`
}

// ToolDefinition is a tool provided to the model; Parameters is a JSON Schema.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// RewriteData is the data of history/rewrite.
type RewriteData struct {
	Messages []MessageData `json:"messages"`
}

// BoundaryData is the data of boundary/snapshot: one Save of an ID in the
// durable.Store.
type BoundaryData struct {
	ID       string          `json:"id"`
	Snapshot json.RawMessage `json:"snapshot"`
}

// NewEvent constructs an event with data encoded as JSON. Seq is assigned by
// the Store.
func NewEvent(typ string, data any) (Event, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: typ, Time: time.Now().UTC(), Data: raw}, nil
}

// Entry is a derived message together with the sequence number of the event
// that provided it.
type Entry struct {
	Seq     int64
	Message llm.Message
	Tool    *ToolMeta
}

// DeriveMessages derives the model history (excluding system): it takes message
// events in order, then masks them by Surface and substitutes the replacement
// messages.
func DeriveMessages(events []Event) ([]llm.Message, error) {
	entries, err := Derive(events)
	if err != nil {
		return nil, err
	}
	out := make([]llm.Message, len(entries))
	for i, e := range entries {
		out[i] = e.Message
	}
	return out, nil
}

// Derive is the same as DeriveMessages but also returns the source event
// sequence number of each message. session/rewind and aborted turn/end events
// undo all events within their interval (including rewrites in it);
// history/rewrite replaces messages on the remaining events.
func Derive(events []Event) ([]Entry, error) {
	var entries []Entry
	for _, ev := range activeEvents(events) {
		var added []Entry
		switch ev.Type {
		case constants.SessionEventUserMessage, constants.SessionEventAssistantMessage, constants.SessionEventToolResult:
			var d MessageData
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				return nil, fmt.Errorf("事件 %d：%w", ev.Seq, err)
			}
			added = []Entry{{Seq: ev.Seq, Message: d.Message, Tool: d.Tool}}
		case constants.SessionEventHistoryRewrite:
			var d RewriteData
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				return nil, fmt.Errorf("事件 %d：%w", ev.Seq, err)
			}
			for _, m := range d.Messages {
				added = append(added, Entry{Seq: ev.Seq, Message: m.Message, Tool: m.Tool})
			}
		}
		if ev.Surface != nil {
			at := len(entries)
			kept := entries[:0:0]
			for _, e := range entries {
				if ev.Surface.covers(e.Seq) {
					at = min(at, len(kept))
					continue
				}
				kept = append(kept, e)
			}
			entries = append(kept[:at:at], append(added, kept[at:]...)...)
			continue
		}
		entries = append(entries, added...)
	}
	return entries, nil
}

// Archived returns the original messages that history/rewrite replaced and that
// have not been undone, in ascending sequence order: the original text replaced
// by compaction and offloading remains in the journal and can be retrieved. A
// rewrite carries unchanged messages within its interval into the replacement
// messages verbatim; those still-visible messages do not count as replaced.
func Archived(events []Event) ([]Entry, error) {
	derived, err := Derive(events)
	if err != nil {
		return nil, err
	}
	visible := make(map[int64]bool, len(derived))
	shown := make(map[string]bool, len(derived))
	for _, e := range derived {
		visible[e.Seq] = true
		if b, err := json.Marshal(e.Message); err == nil {
			shown[string(b)] = true
		}
	}
	var out []Entry
	for _, ev := range activeEvents(events) {
		switch ev.Type {
		case constants.SessionEventUserMessage, constants.SessionEventAssistantMessage, constants.SessionEventToolResult:
		default:
			continue
		}
		if visible[ev.Seq] {
			continue
		}
		var d MessageData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return nil, fmt.Errorf("事件 %d：%w", ev.Seq, err)
		}
		if b, err := json.Marshal(d.Message); err != nil || !shown[string(b)] {
			out = append(out, Entry{Seq: ev.Seq, Message: d.Message, Tool: d.Tool})
		}
	}
	return out, nil
}

// LastState returns the state and sequence number of the last turn/end with
// state that has not been undone; if there is none, state is empty and seq is
// 0.
func LastState(events []Event) (state json.RawMessage, seq int64, err error) {
	active := activeEvents(events)
	for i := len(active) - 1; i >= 0; i-- {
		ev := active[i]
		if ev.Type != constants.SessionEventTurnEnd {
			continue
		}
		var d TurnEnd
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return nil, 0, fmt.Errorf("事件 %d：%w", ev.Seq, err)
		}
		if len(d.State) > 0 {
			return d.State, ev.Seq, nil
		}
	}
	return nil, 0, nil
}

// Active returns the events not undone by session/rewind or aborted turn/end
// events.
func Active(events []Event) []Event { return activeEvents(events) }

// activeEvents 去掉被 session/rewind 与中止的 turn/end 撤销的事件。
// 退回恢复到序号 Start-1 时的视图，因此退回到更早的退回之前会让那次退回失效；中止的轮次只删去本轮事件。
func activeEvents(events []Event) []Event {
	type checkpoint struct {
		seq    int64
		active []Event
	}
	var active []Event
	marks := []checkpoint{{}}
	for i, ev := range events {
		if !undoes(ev) {
			active = append(active, ev)
			continue
		}
		if ev.Type == constants.SessionEventRewind {
			// 视图在两次撤销之间只追加，取 Start 之前最近的检查点再补上其后的事件即可。
			j := len(marks) - 1
			for marks[j].seq >= ev.Surface.Start {
				j--
			}
			active = slices.Clone(marks[j].active)
			for _, e := range events[:i] {
				if e.Seq > marks[j].seq && e.Seq < ev.Surface.Start {
					active = append(active, e)
				}
			}
		} else {
			active = slices.DeleteFunc(active, func(e Event) bool { return ev.Surface.covers(e.Seq) })
		}
		marks = append(marks, checkpoint{ev.Seq, slices.Clone(active)})
	}
	return active
}

// undoes 报告 ev 是否撤销其 Surface 区间内的事件。
func undoes(ev Event) bool {
	if ev.Surface == nil {
		return false
	}
	if ev.Type == constants.SessionEventRewind {
		return true
	}
	if ev.Type == constants.SessionEventTurnEnd {
		var d TurnEnd
		return json.Unmarshal(ev.Data, &d) == nil && d.Aborted
	}
	return false
}

// LastSeq returns the largest sequence number among the events, or 0 when there
// are none.
func LastSeq(events []Event) int64 {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].Seq
}

// OpenTurn returns the turn/start event of the last unfinished turn; ok is
// false if there is none.
func OpenTurn(events []Event) (start Event, ok bool) {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case constants.SessionEventTurnEnd:
			return Event{}, false
		case constants.SessionEventTurnStart:
			return events[i], true
		}
	}
	return Event{}, false
}
