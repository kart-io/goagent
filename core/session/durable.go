package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/kart-io/goagent/core/durable"
)

// DurableStore 把会话 id 的日志作为 durable.Store：每次 Save 追加一条 boundary/snapshot 事件，Load 取该 ID 最近一次写入的快照。
// 持久操作的记录因此与对话位于同一份日志，按执行键（边界 + 次数）寻址；会话视图（DeriveMessages）忽略这些事件，
// Rewind 也不影响它们。Lock 委托给 Store.Lock。
func DurableStore(store Store, id string) durable.Store {
	return &durableStore{store: store, id: id, snaps: map[string]json.RawMessage{}}
}

type durableStore struct {
	store Store
	id    string

	mu    sync.Mutex
	seq   int64
	snaps map[string]json.RawMessage
}

func (s *durableStore) Lock(ctx context.Context, name string) (func() error, error) {
	return s.store.Lock(ctx, s.id, "durable/"+name)
}

// refresh 读入上次之后的快照事件；调用方持有 s.mu。
func (s *durableStore) refresh(ctx context.Context) error {
	events, err := s.store.Read(ctx, s.id, s.seq)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, ev := range events {
		s.seq = ev.Seq
		if ev.Type != TypeBoundarySnapshot {
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

// Save 追加快照。其他写入者（对话记录、其他持久操作）造成的序号冲突按最新序号重试：调用方持有该 ID 的锁，冲突不涉及同一 ID。
func (s *durableStore) Save(ctx context.Context, name string, snap *durable.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	ev, err := NewEvent(TypeBoundarySnapshot, BoundaryData{ID: name, Snapshot: raw})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		last, err := s.store.Append(ctx, s.id, s.seq, ev)
		if err == nil {
			s.seq = last
			s.snaps[name] = raw
			return nil
		}
		if !errors.Is(err, ErrConflict) {
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
