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

// Store 保存会话日志。实现须满足：
//   - Append/AppendAll 返回后调用方可复用事件；Read 返回值以及 Fork 的源与分支相互隔离，包括 Data 与 Surface。
//   - Append 原子地追加全部事件或都不追加，按 expected 做比较并交换（CAS）：expected 为 0 时会话须不存在或为空，
//     会话不存在时创建；成功时为事件分配从 expected+1 起的连续序号并持久化后返回最后序号。
//   - AppendAll 把多个会话的追加作为一次原子提交：全部 Expected 都匹配才写入，要么全部可见，要么都不可见，崩溃后同样如此。
//     返回值与 batches 一一对应，是各会话追加后的最后序号。任一 Expected 不匹配时返回 ErrConflict 且不写入。
//     ID 重复时拒绝且不写入；batches 为空时不做任何事；Events 为空的批次只参与比较。
//     提交结果未知的错误（如连接中断）下调用方须重新 Read 判断。
//   - Read 返回序号大于 after 的事件，读到的是已成功 Append 的完整事件；会话不存在时返回 ErrNotFound。
//   - Fork 把 src 中序号不大于 upTo 的事件原样复制为新会话 dst（upTo 为 0 时复制全部）；dst 已存在时返回 ErrExists。
//   - Lock 为会话中的一个名称加锁，语义同 durable.Store.Lock（等待到获得为止、不可重入、持有者退出后有界时间内释放，
//     ctx 结束时返回包装 durable.ErrLockUnavailable 的错误）；DurableStore 用它实现 durable.Store 的锁。
type Store interface {
	Append(ctx context.Context, id string, expected int64, events ...Event) (last int64, err error)
	AppendAll(ctx context.Context, batches ...AppendBatch) (lasts []int64, err error)
	Read(ctx context.Context, id string, after int64) ([]Event, error)
	List(ctx context.Context) ([]string, error)
	Fork(ctx context.Context, src, dst string, upTo int64) error
	Lock(ctx context.Context, id, name string) (unlock func() error, err error)
}

// AppendBatch 是 Store.AppendAll 对一个会话的追加。
type AppendBatch struct {
	ID       string
	Expected int64
	Events   []Event
}

// ValidateBatches 检查 AppendAll 的参数，供 Store 实现在写入前调用。
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

// MemoryStore 是进程内的 Store，用于测试与不需要持久化的宿主。
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string][]Event
	locks    map[string]chan struct{}
}

// NewMemoryStore 创建空的 MemoryStore。
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

// AppendAll 实现 Store。
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
