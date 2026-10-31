package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/kart-io/goagent/core/durable"
)

// ErrConflict 表示 Append 的期望序号不是会话当前的最后序号：会话已被他人写入，调用方须重新读取后决定。
var ErrConflict = errors.New("会话日志已被并发写入")

// ErrNotFound 表示会话不存在。
var ErrNotFound = errors.New("会话不存在")

// ErrExists 表示 Fork 的目标会话已存在。
var ErrExists = errors.New("会话已存在")

// Store 保存会话日志。实现须满足：
//   - Append 原子地追加全部事件或都不追加，按 expected 做比较并交换（CAS）：expected 为 0 时会话须不存在或为空，
//     会话不存在时创建；成功时为事件分配从 expected+1 起的连续序号并持久化后返回最后序号。
//   - Read 返回序号大于 after 的事件，读到的是已成功 Append 的完整事件；会话不存在时返回 ErrNotFound。
//   - Fork 把 src 中序号不大于 upTo 的事件原样复制为新会话 dst（upTo 为 0 时复制全部）；dst 已存在时返回 ErrExists。
//   - Lock 为会话中的一个名称加锁，语义同 durable.Store.Lock（等待到获得为止、不可重入、持有者退出后有界时间内释放，
//     ctx 结束时返回包装 durable.ErrLockUnavailable 的错误）；DurableStore 用它实现 durable.Store 的锁。
type Store interface {
	Append(ctx context.Context, id string, expected int64, events ...Event) (last int64, err error)
	Read(ctx context.Context, id string, after int64) ([]Event, error)
	List(ctx context.Context) ([]string, error)
	Fork(ctx context.Context, src, dst string, upTo int64) error
	Lock(ctx context.Context, id, name string) (unlock func() error, err error)
}

func validID(id string) error {
	if id == "" {
		return errors.New("会话 ID 不能为空")
	}
	return nil
}

// assign 为事件分配序号并补全时间。
func assign(events []Event, expected int64) []Event {
	out := make([]Event, len(events))
	for i, ev := range events {
		ev.Seq = expected + int64(i) + 1
		if ev.Time.IsZero() {
			ev.Time = time.Now().UTC()
		}
		out[i] = ev
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
		return 0, fmt.Errorf("%w：期望序号 %d，当前 %d", ErrConflict, expected, LastSeq(cur))
	}
	s.sessions[id] = append(cur, assign(events, expected)...)
	return expected + int64(len(events)), nil
}

func (s *MemoryStore) Read(ctx context.Context, id string, after int64) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events, ok := s.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	n := sort.Search(len(events), func(i int) bool { return events[i].Seq > after })
	return slices.Clone(events[n:]), nil
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
		return ErrNotFound
	}
	if _, ok := s.sessions[dst]; ok {
		return ErrExists
	}
	s.sessions[dst] = slices.Clone(forkRange(events, upTo))
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
			return nil, fmt.Errorf("%w: %w", durable.ErrLockUnavailable, ctx.Err())
		}
	}
}
