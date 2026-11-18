package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// DurableStore exposes the journal of session id as a durable.Store: each Save
// appends a boundary/snapshot event, and Load returns the most recently written
// snapshot for the ID. Durable operation records therefore live in the same
// journal as the conversation and are addressed by execution key (boundary +
// count); the session view (DeriveMessages) ignores these events, and Rewind
// does not affect them. Lock delegates to Store.Lock with the name
// DurableLockName(name).
func DurableStore(store Store, id string) durable.Store {
	return &durableStore{store: store, id: id, snaps: map[string]json.RawMessage{}}
}

// DurableLockName is the name DurableStore uses to lock the session for
// snapshot name. Stores that implement locks as leases use it to verify the
// holder when appending that snapshot.
func DurableLockName(name string) string { return "durable/" + name }

type durableStore struct {
	store Store
	id    string

	mu    sync.Mutex
	seq   int64
	snaps map[string]json.RawMessage
}

func (s *durableStore) Lock(ctx context.Context, name string) (func() error, error) {
	return s.store.Lock(ctx, s.id, DurableLockName(name))
}

// refresh 读入上次之后的快照事件；调用方持有 s.mu。
func (s *durableStore) refresh(ctx context.Context) error {
	events, err := s.store.Read(ctx, s.id, s.seq)
	if errors.Is(err, agentErrors.ErrSessionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, ev := range events {
		s.seq = ev.Seq
		if ev.Type != constants.SessionEventBoundarySnapshot {
			continue
		}
		var d BoundaryData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return fmt.Errorf("事件 %d：%w", ev.Seq, err)
		}
		s.snaps[d.ID] = d.Snapshot
	}
	return nil
}

func (s *durableStore) Load(ctx context.Context, name string) (*durable.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(ctx); err != nil {
		return nil, err
	}
	raw, ok := s.snaps[name]
	if !ok {
		return nil, fmt.Errorf("快照 %s: %w", name, os.ErrNotExist)
	}
	var snap durable.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("快照 %s 损坏: %w", name, err)
	}
	return &snap, nil
}

// Save appends a snapshot.
func (s *durableStore) Save(ctx context.Context, name string, snap *durable.Snapshot) error {
	return s.SaveAll(ctx, durable.Write{ID: name, Snapshot: snap})
}

// SaveAll implements durable.Store: all snapshots are in the same session and
// are written as one Append. Sequence conflicts caused by other writers
// (conversation records, other durable operations) are retried at the latest
// sequence number: the caller holds the locks for these IDs, so the conflicts
// do not involve them.
func (s *durableStore) SaveAll(ctx context.Context, writes ...durable.Write) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := durable.ValidateWrites(writes); err != nil {
		return err
	}
	if len(writes) == 0 {
		return nil
	}
	events := make([]Event, len(writes))
	raws := make([]json.RawMessage, len(writes))
	for i, write := range writes {
		raw, err := json.Marshal(write.Snapshot)
		if err != nil {
			return err
		}
		raws[i] = raw
		if events[i], err = NewEvent(constants.SessionEventBoundarySnapshot, BoundaryData{ID: write.ID, Snapshot: raw}); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		last, err := s.store.Append(ctx, s.id, s.seq, events...)
		if err == nil {
			s.seq = last
			for i, write := range writes {
				s.snaps[write.ID] = raws[i]
			}
			return nil
		}
		if !errors.Is(err, agentErrors.ErrSessionConflict) {
			return err
		}
		before := s.seq
		if err := s.refresh(ctx); err != nil {
			return err
		}
		if s.seq == before {
			return err
		}
	}
}
