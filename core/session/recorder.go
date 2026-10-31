package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kart-io/goagent/llm"
)

// Recorder 把一个会话的对话同步到日志。它只追加与日志推导结果不同的部分：新增的消息逐条记录，
// 被改写的历史记为一条 history/rewrite。同一会话同时只应有一个 Recorder 写入消息；
// 其间其他写入者追加的 boundary/snapshot 事件被跳过，其他类型的事件使写入返回 ErrConflict。
type Recorder struct {
	store  Store
	id     string
	seq    int64
	events []Event // 不含 boundary/snapshot
	system string
	tools  string
}

// OpenRecorder 读取会话的全部事件（会话不存在时为空）并返回 Recorder 与读到的事件。
func OpenRecorder(ctx context.Context, store Store, id string) (*Recorder, []Event, error) {
	if err := validID(id); err != nil {
		return nil, nil, err
	}
	events, err := store.Read(ctx, id, 0)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, nil, err
	}
	r := &Recorder{store: store, id: id}
	r.absorb(events)
	return r, events, nil
}

func (r *Recorder) absorb(events []Event) {
	for _, ev := range events {
		r.seq = ev.Seq
		switch ev.Type {
		case TypeBoundarySnapshot:
			continue
		case TypeSystemMessage:
			var d MessageData
			if json.Unmarshal(ev.Data, &d) == nil {
				r.system = d.Message.Content
			}
		case TypeToolDefinitions:
			r.tools = string(ev.Data)
		}
		r.events = append(r.events, ev)
	}
}

// Seq 返回已知的最后序号。
func (r *Recorder) Seq() int64 { return r.seq }

// Events 返回已知的非快照事件。
func (r *Recorder) Events() []Event { return r.events }

// Append 追加事件，返回分配了序号的事件。
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
		if !errors.Is(err, ErrConflict) {
			return nil, err
		}
		newer, rerr := r.store.Read(ctx, r.id, r.seq)
		if rerr != nil {
			return nil, rerr
		}
		for _, ev := range newer {
			if ev.Type != TypeBoundarySnapshot {
				return nil, fmt.Errorf("%w：序号 %d 处出现了其他写入者的 %s", ErrConflict, ev.Seq, ev.Type)
			}
		}
		if len(newer) == 0 {
			return nil, err
		}
		r.absorb(newer)
	}
}

// SyncSystem 在 system 提示词与上次记录的不同时记录它。
func (r *Recorder) SyncSystem(ctx context.Context, system string) error {
	if system == "" || system == r.system {
		return nil
	}
	ev, err := NewEvent(TypeSystemMessage, MessageData{Message: llm.SystemMessage(system)})
	if err != nil {
		return err
	}
	_, err = r.Append(ctx, ev)
	return err
}

// SyncTools 在工具定义与上次记录的不同时记录它。
func (r *Recorder) SyncTools(ctx context.Context, tools []ToolDefinition) error {
	if len(tools) == 0 {
		return nil
	}
	ev, err := NewEvent(TypeToolDefinitions, ToolsData{Tools: tools})
	if err != nil || string(ev.Data) == r.tools {
		return err
	}
	_, err = r.Append(ctx, ev)
	return err
}

// Sync 使日志推导出的对话等于 messages（不含 system）。tools 按工具调用 ID 给出工具结果的信息。
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
		ev, err := NewEvent(TypeHistoryRewrite, rewrite)
		if err != nil {
			return err
		}
		ev.Surface = &Surface{Start: entries[start].Seq, End: entries[len(entries)-1].Seq}
		events = append(events, ev)
	} else {
		for _, m := range messages[p:] {
			typ := TypeUserMessage
			switch m.Role {
			case "assistant":
				typ = TypeAssistantMessage
			case "tool":
				typ = TypeToolResult
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

// sameMessage 按 JSON 编码比较：日志中的消息经过编解码，空切片与 nil 等差异不算改写。
func sameMessage(a, b llm.Message) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// Rewind 把会话退回到序号 to 之后的消息被遮蔽的状态。expected 是调用方看到的最后序号（CAS）：
// 其间有新的写入时返回 ErrConflict，调用方应重新查看后再决定。被遮蔽的事件仍保留在日志中，持久操作的快照不受影响。
func Rewind(ctx context.Context, store Store, id string, to, expected int64) (int64, error) {
	if to < 0 || to >= expected {
		return 0, fmt.Errorf("退回位置 %d 须在 [0, %d) 内", to, expected)
	}
	ev, err := NewEvent(TypeRewind, struct {
		To int64 `json:"to"`
	}{to})
	if err != nil {
		return 0, err
	}
	ev.Surface = &Surface{Start: to + 1, End: expected}
	return store.Append(ctx, id, expected, ev)
}
