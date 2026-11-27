package session

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// Store persists session journals. Implementations must satisfy:
//   - After Append/AppendAll returns the caller may reuse the events; values
//     returned by Read, and the source and branch of Fork, are isolated from
//     each other, including Data and Surface.
//   - Append atomically appends all events or none, using expected for
//     compare-and-swap (CAS): when expected is 0 the session must not exist or
//     must be empty, and a missing session is created; on success it assigns
//     consecutive sequence numbers starting at expected+1, persists the events,
//     and returns the last sequence number.
//   - AppendAll commits appends to several sessions atomically: it writes only
//     if every Expected matches, and either all are visible or none are,
//     including after a crash. The return values correspond one-to-one with
//     batches and are each session's last sequence number after the append. If
//     any Expected does not match it returns ErrConflict without writing.
//     Duplicate IDs are rejected without writing; empty batches do nothing; a
//     batch with empty Events only takes part in the comparison. On errors with
//     an unknown commit outcome (such as a dropped connection), the caller must
//     Read again to find out.
//   - Read returns events with sequence numbers greater than after, reading
//     complete events of successful Appends; when the session does not exist it
//     returns ErrNotFound.
//   - Fork copies the events in src with sequence numbers not greater than upTo
//     verbatim into a new session dst (all events when upTo is 0); if dst
//     already exists it returns ErrExists.
//   - Lock locks a name within a session with the same semantics as
//     durable.Store.Lock (waits until acquired, is not reentrant, is released
//     within bounded time after the holder exits, and returns an error wrapping
//     durable.ErrLockUnavailable when ctx ends); DurableStore uses it to
//     implement the durable.Store lock.
type Store interface {
	Append(ctx context.Context, id string, expected int64, events ...Event) (last int64, err error)
	AppendAll(ctx context.Context, batches ...AppendBatch) (lasts []int64, err error)
	Read(ctx context.Context, id string, after int64) ([]Event, error)
	List(ctx context.Context) ([]string, error)
	Fork(ctx context.Context, src, dst string, upTo int64) error
	Lock(ctx context.Context, id, name string) (unlock func() error, err error)
}

// Deleter is an optional Store capability: Delete removes a session and all of
// its events, and succeeds when the session does not exist. Sessions forked
// from it are independent copies and stay. The caller must make sure nothing
// still appends to the session; a later Append with expected 0 recreates it.
type Deleter interface {
	Delete(ctx context.Context, id string) error
}

// AppendBatch is one session's append in Store.AppendAll.
type AppendBatch struct {
	ID       string
	Expected int64
	Events   []Event
}

// ValidateBatches checks the arguments to AppendAll; Store implementations call
// it before writing.
func ValidateBatches(batches []AppendBatch) error {
	seen := make(map[string]bool, len(batches))
	for _, batch := range batches {
		if err := validID(batch.ID); err != nil {
			return err
		}
		if seen[batch.ID] {
			return agentErrors.Newf(agentErrors.CodeInvalidInput, "批量追加的会话重复：%q", batch.ID).WithComponent("session_store").WithOperation("validate_batches")
		}
		seen[batch.ID] = true
	}
	return nil
}

func validID(id string) error {
	if id == "" {
		return agentErrors.New(agentErrors.CodeInvalidInput, "会话 ID 不能为空").WithComponent("session").WithOperation("validate_id")
	}
	return nil
}

// assign 为事件分配序号并补全时间。
func assign(events []Event, expected int64) []Event {
	out := cloneEvents(events)
	for i, ev := range out {
		ev.Seq = expected + int64(i) + 1
		if ev.Time.IsZero() {
			ev.Time = time.Now().UTC()
		}
		out[i] = ev
	}
	return out
}

// cloneEvents 在日志边界隔离事件中全部可写引用。
func cloneEvents(events []Event) []Event {
	out := slices.Clone(events)
	for i := range out {
		out[i].Data = slices.Clone(out[i].Data)
		if out[i].Surface != nil {
			surface := *out[i].Surface
			out[i].Surface = &surface
		}
	}
	return out
}

func forkRange(events []Event, upTo int64) []Event {
	if upTo <= 0 {
		return events
	}
	n := sort.Search(len(events), func(i int) bool { return events[i].Seq > upTo })
	return events[:n]
}

// MemoryStore is an in-process Store for tests and for hosts that do not need
// persistence.
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string][]Event
	locks    map[string]chan struct{}
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: map[string][]Event{}, locks: map[string]chan struct{}{}}
}

func (s *MemoryStore) Append(ctx context.Context, id string, expected int64, events ...Event) (int64, error) {
	if err := validID(id); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.sessions[id]
	if LastSeq(cur) != expected {
		return 0, fmt.Errorf("%w：期望序号 %d，当前 %d", agentErrors.ErrSessionConflict, expected, LastSeq(cur))
	}
	s.sessions[id] = append(cur, assign(events, expected)...)
	return expected + int64(len(events)), nil
}

// AppendAll implements Store.
func (s *MemoryStore) AppendAll(ctx context.Context, batches ...AppendBatch) ([]int64, error) {
	if err := ValidateBatches(batches); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, batch := range batches {
		if cur := LastSeq(s.sessions[batch.ID]); cur != batch.Expected {
			return nil, fmt.Errorf("%w：会话 %q 期望序号 %d，当前 %d", agentErrors.ErrSessionConflict, batch.ID, batch.Expected, cur)
		}
	}
	lasts := make([]int64, len(batches))
	for i, batch := range batches {
		lasts[i] = batch.Expected + int64(len(batch.Events))
		if len(batch.Events) > 0 {
			s.sessions[batch.ID] = append(s.sessions[batch.ID], assign(batch.Events, batch.Expected)...)
		}
	}
	return lasts, nil
}

func (s *MemoryStore) Read(ctx context.Context, id string, after int64) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events, ok := s.sessions[id]
	if !ok {
		return nil, agentErrors.ErrSessionNotFound
	}
	n := sort.Search(len(events), func(i int) bool { return events[i].Seq > after })
	return cloneEvents(events[n:]), nil
}

// Delete implements Deleter.
func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	if err := validID(id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *MemoryStore) List(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *MemoryStore) Fork(ctx context.Context, src, dst string, upTo int64) error {
	if err := validID(dst); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events, ok := s.sessions[src]
	if !ok {
		return agentErrors.ErrSessionNotFound
	}
	if _, ok := s.sessions[dst]; ok {
		return agentErrors.ErrSessionExists
	}
	s.sessions[dst] = cloneEvents(forkRange(events, upTo))
	return nil
}

func (s *MemoryStore) Lock(ctx context.Context, id, name string) (func() error, error) {
	key := id + "\x00" + name
	for {
		s.mu.Lock()
		held, ok := s.locks[key]
		if !ok {
			ch := make(chan struct{})
			s.locks[key] = ch
			s.mu.Unlock()
			var once sync.Once
			return func() error {
				once.Do(func() {
					s.mu.Lock()
					delete(s.locks, key)
					s.mu.Unlock()
					close(ch)
				})
				return nil
			}, nil
		}
		s.mu.Unlock()
		select {
		case <-held:
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %w", agentErrors.ErrLockUnavailable, ctx.Err())
		}
	}
}
