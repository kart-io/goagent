// Package session 是追加写的会话事件日志：模型看到的对话、轮次边界与持久操作的快照都记在同一份日志中。
// DeriveMessages 从日志推导模型历史；DurableStore 把日志作为 core/durable 的存储，恢复与回放基于同一份日志。
// 设计见 docs/design/HARNESS_CORE_DESIGN.md 第 6、16 节。
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

// Event 是日志中的一条事件。Seq 由 Store 分配，从 1 起连续递增。
type Event struct {
	Seq  int64           `json:"seq"`
	Type string          `json:"type"`
	Time time.Time       `json:"time"`
	Data json.RawMessage `json:"data,omitempty"`
	// Surface 非空时遮蔽序号在 [Start, End] 内的消息事件；事件自带的替代消息出现在被遮蔽的位置。
	Surface *Surface `json:"surface,omitzero"`
}

// Surface 是被遮蔽的事件序号区间（闭区间）。
type Surface struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

func (s *Surface) covers(seq int64) bool { return s != nil && seq >= s.Start && seq <= s.End }

// TurnStart 是 turn/start 的数据。
type TurnStart struct {
	Input string `json:"input"`
	// ExecutionKey 是宿主传给持久运行的执行键（DurableScope 派生之前）：以同一执行键恢复时续写该轮。
	ExecutionKey string `json:"execution_key,omitempty"`
}

// TurnEnd 是 turn/end 的数据。
type TurnEnd struct {
	Answer  string      `json:"answer,omitempty"`
	Aborted bool        `json:"aborted,omitzero"`
	Usage   *TokenUsage `json:"usage,omitzero"`
	// State 是本轮结束时宿主状态的编码，由 Builder 的 WithSessionState 写入。
	State json.RawMessage `json:"state,omitempty"`
}

// TokenUsage 是一轮的用量。
type TokenUsage = interfaces.TokenUsage

// MessageData 是消息事件的数据。Tool 只在 tool/result 中出现。
type MessageData struct {
	Message llm.Message `json:"message"`
	Tool    *ToolMeta   `json:"tool,omitzero"`
}

// ToolMeta 是一次工具调用的结果信息，供导出统计。
type ToolMeta struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
}

// ToolsData 是 tool/definitions 的数据。
type ToolsData struct {
	Tools []ToolDefinition `json:"tools"`
}

// ToolDefinition 是提供给模型的一个工具，Parameters 为 JSON Schema。
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// RewriteData 是 history/rewrite 的数据。
type RewriteData struct {
	Messages []MessageData `json:"messages"`
}

// BoundaryData 是 boundary/snapshot 的数据：durable.Store 中 ID 的一次 Save。
type BoundaryData struct {
	ID       string          `json:"id"`
	Snapshot json.RawMessage `json:"snapshot"`
}

// NewEvent 构造一条事件，data 编码为 JSON。Seq 由 Store 分配。
func NewEvent(typ string, data any) (Event, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: typ, Time: time.Now().UTC(), Data: raw}, nil
}

// Entry 是推导出的一条消息与提供它的事件序号。
type Entry struct {
	Seq     int64
	Message llm.Message
	Tool    *ToolMeta
}

// DeriveMessages 推导模型历史（不含 system）：按顺序取消息事件，再按 Surface 遮蔽并代入替代消息。
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

// Derive 与 DeriveMessages 相同，另返回每条消息的来源事件序号。
// session/rewind 与中止的 turn/end 撤销其区间内的全部事件（包括其中的改写）；history/rewrite 在其余事件上替换消息。
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

// LastState 返回未被撤销的最后一个带状态的 turn/end 中的状态及其序号；没有时 state 为空、seq 为 0。
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

// LastSeq 返回事件中最大的序号，没有事件时为 0。
func LastSeq(events []Event) int64 {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].Seq
}

// OpenTurn 返回最后一个未结束的轮次的 turn/start 事件；没有时 ok 为假。
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
