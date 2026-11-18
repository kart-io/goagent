package durable

import (
	"context"
	"errors"
	"os"
	"slices"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
)

const (
	inboxVersion = "goagent-inbox-v1"
)

// InboxEntry is an unacknowledged input in the durable inbox. Seq increases
// within an inbox and is never reused.
type InboxEntry struct {
	Seq  uint64 `json:"seq"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type inboxState struct {
	Next    uint64       `json:"next"`
	Entries []InboxEntry `json:"entries"`
	Seen    []string     `json:"seen,omitempty"`
}

// Inbox stores inputs received during a run in the journal Store. Once Append
// returns the entry is persisted, and it survives restarts until Ack.
// Acknowledgement and the claim record are not written together, so if a crash
// happens between them the entry remains: claimers must deduplicate by Seq.
type Inbox struct {
	store Store
	id    string
}

// OpenInbox opens the inbox of the run that ctx belongs to and returns the
// unacknowledged entries; it returns nil when ctx is not a durable run.
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
		return nil, agentErrors.Newf(agentErrors.CodeStateValidation, "invalid durable inbox record").WithComponent("durable_inbox").WithOperation("load")
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

// Append appends an input and returns its sequence number. When it returns an
// error the caller should treat the input as not accepted; on errors with an
// unknown commit outcome the entry may already be persisted and will still be
// processed after recovery.
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

// AppendOnce is like Append but appends only once per key: the key and the
// entry are committed in the same write, so writing again after the entry is
// acknowledged does not produce a second entry. Only the most recent
// constants.DurableInboxSeenLimit keys are remembered. Nothing is written when
// duplicate is true.
func (b *Inbox) AppendOnce(ctx context.Context, key, kind, text string) (seq uint64, duplicate bool, err error) {
	err = b.update(ctx, func(state *inboxState) bool {
		if slices.Contains(state.Seen, key) {
			duplicate = true
			return false
		}
		state.Next++
		seq = state.Next
		state.Entries = append(state.Entries, InboxEntry{Seq: seq, Kind: kind, Text: text})
		state.Seen = append(state.Seen, key)
		if over := len(state.Seen) - constants.DurableInboxSeenLimit; over > 0 {
			state.Seen = state.Seen[over:]
		}
		return true
	})
	if err != nil {
		return 0, false, err
	}
	return seq, duplicate, nil
}

// Ack acknowledges that entries have been claimed. It is safe to call
// repeatedly; already acknowledged sequence numbers are ignored.
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
