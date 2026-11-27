package leasestore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
)

// SessionStore implements session.Store and session.Deleter on a
// SessionBackend. It shares the leases and renewal of the Store it was created
// from; closing that Store stops its locks.
//
// Lock is a lease in the session's namespace with the semantics of Store.Lock.
// Appending a boundary snapshot event verifies, in the same backend
// transaction, that this process holds session.DurableLockName(snapshot ID);
// a holder that was taken over gets errors.ErrLeaseLost, so a snapshot can
// only be appended while holding the lock.
type SessionStore struct {
	root    *Store
	backend SessionBackend
}

var (
	_ session.Store   = (*SessionStore)(nil)
	_ session.Deleter = (*SessionStore)(nil)
)

// NewSessionStore returns a session store on backend that locks through root.
func NewSessionStore(root *Store, backend SessionBackend) (*SessionStore, error) {
	if root == nil || backend == nil {
		return nil, fmt.Errorf("leasestore.NewSessionStore 需要 Store 与 SessionBackend")
	}
	return &SessionStore{root: root, backend: backend}, nil
}

func validSessionID(id string) error {
	if id == "" {
		return errors.New("会话 ID 不能为空")
	}
	return nil
}

// Lock implements session.Store.
func (s *SessionStore) Lock(ctx context.Context, id, name string) (func() error, error) {
	if err := validSessionID(id); err != nil {
		return nil, err
	}
	return s.root.lock(ctx, SessionNS(id), name)
}

// fences 返回 events 中各持久快照对应的本进程租约，按名称排序去重；未持有时返回 ErrLeaseLost。
func (s *SessionStore) fences(id string, events []session.Event) ([]Lease, error) {
	var leases []Lease
	for _, ev := range events {
		if ev.Type != constants.SessionEventBoundarySnapshot {
			continue
		}
		var d session.BoundaryData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return nil, fmt.Errorf("快照事件损坏：%w", err)
		}
		name := session.DurableLockName(d.ID)
		l, ok := s.root.held(SessionNS(id), name)
		if !ok {
			return nil, fmt.Errorf("%w：本进程未持有会话 %q 中 %q 的锁", agentErrors.ErrLeaseLost, id, name)
		}
		leases = append(leases, l)
	}
	// 同一会话可能包含多个快照，按租约名称排序，避免批次内快照顺序相反时后端死锁。
	slices.SortFunc(leases, func(a, b Lease) int { return strings.Compare(a.ID, b.ID) })
	return slices.Compact(leases), nil
}

func (s *SessionStore) plan(batch session.AppendBatch) (EventBatch, error) {
	fences, err := s.fences(batch.ID, batch.Events)
	if err != nil {
		return EventBatch{}, err
	}
	now := time.Now().UTC()
	n := len(batch.Events)
	plan := EventBatch{SessionID: batch.ID, Expected: batch.Expected, Fences: fences,
		Seqs: make([]int64, n), Types: make([]string, n), Data: make([][]byte, n)}
	for i, ev := range batch.Events {
		ev.Seq = batch.Expected + int64(i) + 1
		if ev.Time.IsZero() {
			ev.Time = now
		}
		plan.Seqs[i], plan.Types[i] = ev.Seq, ev.Type
		if plan.Data[i], err = json.Marshal(ev); err != nil {
			return EventBatch{}, err
		}
	}
	return plan, nil
}

// Append implements session.Store.
func (s *SessionStore) Append(ctx context.Context, id string, expected int64, events ...session.Event) (int64, error) {
	if err := validSessionID(id); err != nil {
		return 0, err
	}
	if len(events) == 0 {
		cur, err := s.backend.LastSeq(ctx, id)
		if err == nil && cur != expected {
			err = Conflict(id, expected, cur)
		}
		return cur, err
	}
	plan, err := s.plan(session.AppendBatch{ID: id, Expected: expected, Events: events})
	if err != nil {
		return 0, err
	}
	if err := s.backend.AppendEvents(ctx, []EventBatch{plan}, s.root.cfg.LeaseTTL); err != nil {
		return 0, err
	}
	return expected + int64(len(events)), nil
}

// AppendAll implements session.Store: all batches are applied in one backend
// transaction.
func (s *SessionStore) AppendAll(ctx context.Context, batches ...session.AppendBatch) ([]int64, error) {
	if err := session.ValidateBatches(batches); err != nil {
		return nil, err
	}
	if len(batches) == 0 {
		return nil, nil
	}
	plans := make([]EventBatch, len(batches))
	lasts := make([]int64, len(batches))
	for i, batch := range batches {
		plan, err := s.plan(batch)
		if err != nil {
			return nil, err
		}
		plans[i], lasts[i] = plan, batch.Expected+int64(len(batch.Events))
	}
	// 按会话 ID 排序，避免并发批量追加在后端互相等待行锁。
	slices.SortFunc(plans, func(a, b EventBatch) int { return strings.Compare(a.SessionID, b.SessionID) })
	if err := s.backend.AppendEvents(ctx, plans, s.root.cfg.LeaseTTL); err != nil {
		return nil, err
	}
	return lasts, nil
}

// Read implements session.Store.
func (s *SessionStore) Read(ctx context.Context, id string, after int64) ([]session.Event, error) {
	raws, err := s.backend.ReadEvents(ctx, id, after)
	if err != nil {
		return nil, err
	}
	events := make([]session.Event, len(raws))
	for i, raw := range raws {
		if err := json.Unmarshal(raw, &events[i]); err != nil {
			return nil, fmt.Errorf("会话 %q 的事件已损坏：%w", id, err)
		}
	}
	return events, nil
}

// List implements session.Store.
func (s *SessionStore) List(ctx context.Context) ([]string, error) {
	return s.backend.ListSessions(ctx)
}

// Delete implements session.Deleter.
func (s *SessionStore) Delete(ctx context.Context, id string) error {
	if err := validSessionID(id); err != nil {
		return err
	}
	return s.backend.DeleteSession(ctx, id)
}

// Fork implements session.Store.
func (s *SessionStore) Fork(ctx context.Context, src, dst string, upTo int64) error {
	if err := validSessionID(dst); err != nil {
		return err
	}
	return s.backend.ForkSession(ctx, src, dst, upTo)
}
