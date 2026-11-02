package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
)

const inboxVersion = "goagent-inbox-v1"

// InboxEntry 是持久收件箱中尚未确认的一条输入。Seq 在同一收件箱内递增且不复用。
type InboxEntry struct {
	Seq  uint64 `json:"seq"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type inboxState struct {
	Next    uint64       `json:"next"`
	Entries []InboxEntry `json:"entries"`
}

// Inbox 把运行中收到的输入保存在日志 Store 中，Append 返回即已落盘，Ack 之前重启不会丢失。
// 确认和领取记录不在同一次写入中，崩溃在两者之间时条目还在：领取方须按 Seq 去重。
type Inbox struct {
	store Store
	id    string
}

// OpenInbox 打开 ctx 所在运行的收件箱并返回未确认的条目；ctx 不是持久运行时返回 nil。
func OpenInbox(ctx context.Context) (*Inbox, []InboxEntry, error) {
	if !HasJournal(ctx) {
		return nil, nil, nil
	}
	box := &Inbox{store: JournalStore(ctx), id: "goagent/inbox/" + ExecutionKey(ctx)}
	state, err := box.load(ctx)
	if err != nil {
		return nil, nil, err
	}
	return box, state.Entries, nil
}

func (b *Inbox) load(ctx context.Context) (*inboxState, error) {
	snapshot, err := b.store.Load(ctx, b.id)
	if errors.Is(err, os.ErrNotExist) {
		return &inboxState{}, nil
	}
	if err != nil {
		return nil, err
	}
	var state inboxState
	if snapshot == nil || snapshot.Version != inboxVersion || snapshot.RunID != b.id || json.Unmarshal(snapshot.Data, &state) != nil {
		return nil, fmt.Errorf("invalid durable inbox record")
	}
	return &state, nil
}

// update 在该收件箱的锁内读取并修改状态；change 返回假时不写入。
func (b *Inbox) update(ctx context.Context, change func(*inboxState) bool) (err error) {
	unlock, err := b.store.Lock(ctx, b.id)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	state, err := b.load(ctx)
	if err != nil {
		return err
	}
	if !change(state) {
		return nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return b.store.Save(ctx, b.id, &Snapshot{RunID: b.id, Version: inboxVersion, Data: data})
}

// Append 追加一条输入并返回序号。返回错误时调用方应视为未接受；提交结果未知的错误下条目可能已落盘，恢复后仍会被处理。
func (b *Inbox) Append(ctx context.Context, kind, text string) (seq uint64, err error) {
	err = b.update(ctx, func(state *inboxState) bool {
		state.Next++
		seq = state.Next
		state.Entries = append(state.Entries, InboxEntry{Seq: seq, Kind: kind, Text: text})
		return true
	})
	if err != nil {
		return 0, err
	}
	return seq, nil
}

// Ack 确认条目已被领取。可重复调用，已确认的序号被忽略。
func (b *Inbox) Ack(ctx context.Context, seqs ...uint64) error {
	return b.update(ctx, func(state *inboxState) bool {
		kept := slices.DeleteFunc(slices.Clone(state.Entries), func(e InboxEntry) bool { return slices.Contains(seqs, e.Seq) })
		if len(kept) == len(state.Entries) {
			return false
		}
		state.Entries = kept
		return true
	})
}
