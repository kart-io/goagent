// Package contextpolicy 提供上下文治理的组件：Offload 把超长的工具结果换成引用并按需读回，ResultStore 保存不可变的证据。
package contextpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/kart-io/goagent/core/durable"
)

const resultVersion = "goagent-context-result-v1"

// ErrReleased 表示该运行键已经 Release，迟到的读写被拒绝。
var ErrReleased = errors.New("结果作用域已释放，拒绝读写")

// Ref identifies one immutable result within a host-owned run.
type Ref struct{ RunKey, Key, SHA256, Version string }

// ResultStore never overwrites a logical key with different evidence.
//
// Release 删除一次运行（Ref.RunKey）写入的全部证据，运行不存在时同样成功。释放后该运行键不可复用：
// 迟到的 PutIfAbsent 返回 ErrReleased 且不留数据，Get 同样返回 ErrReleased（持久版 Release 中途崩溃、证据尚未删除时也是如此）。
//
// 拒绝迟到读写依赖每个已释放运行键的一个墓碑。PruneReleased 回收释放时间早于 olderThan 的墓碑，内存版与持久版语义一致：
// 阈值内的迟到读写仍返回 ErrReleased；墓碑回收后 Get 返回 os.ErrNotExist，该运行键迟到的写入会成功并留下残留，
// 宿主应把 olderThan 设得大于最长的重试或恢复窗口（例如进程崩溃重启、重试），并定期调用。
type ResultStore interface {
	PutIfAbsent(context.Context, Ref, []byte) error
	Get(context.Context, Ref) ([]byte, error)
	Release(context.Context, string) error
	PruneReleased(ctx context.Context, olderThan time.Duration) error
}
type scopeKey struct{}

// WithResultScope supplies trusted run ownership, never model arguments.
// 宿主提供作用域时由宿主负责回收：运行结束且不再恢复后，用同一运行键调用 ResultStore.Release，此后不得复用该运行键。
func WithResultScope(ctx context.Context, runKey string) context.Context {
	return context.WithValue(ctx, scopeKey{}, runKey)
}

// ResultScope returns the trusted scope or the empty string.
func ResultScope(ctx context.Context) string { s, _ := ctx.Value(scopeKey{}).(string); return s }
func digest(b []byte) string                 { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// NewRef uses structural encoding to avoid collisions between delimiter-containing IDs.
func NewRef(run, policy, logical string, data []byte) Ref {
	b, _ := json.Marshal([]string{run, policy, logical})
	return Ref{RunKey: run, Key: digest(b), SHA256: digest(data), Version: resultVersion}
}
func validRef(ctx context.Context, ref Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ref.RunKey == "" || ref.RunKey != ResultScope(ctx) || ref.Key == "" || len(ref.SHA256) != 64 || ref.Version != resultVersion {
		return fmt.Errorf("invalid or out-of-scope result reference")
	}
	return nil
}

type entry struct {
	Ref  Ref
	Data []byte
}
type resultStore struct {
	mu sync.Mutex
	// memory 按 RunKey 分组，Release 整组删除并在 released 中记下释放时间。
	memory   map[string]map[string]entry
	released map[string]time.Time
	now      func() time.Time
	store    durable.Store
}

// NewMemoryStore creates a concurrency-safe evidence store.
// Release 后为该运行键保留一个墓碑（运行键与释放时间）以拒绝迟到的读写，由 PruneReleased 回收。
func NewMemoryStore() ResultStore {
	return &resultStore{memory: map[string]map[string]entry{}, released: map[string]time.Time{}, now: time.Now}
}

// NewDurableResultStore stores immutable evidence snapshots independently of operation records.
// 底层 store 实现 durable.Scoped 时（如 durable.FileStore）每次运行的证据写入独立作用域，Release 整体删除该作用域，
// 拒绝读写的保证由底层实现给出（FileStore 覆盖本机所有进程）；否则证据写入底层 store 本身，Release 与 PruneReleased 返回错误。
func NewDurableResultStore(store durable.Store) (ResultStore, error) {
	if store == nil {
		return nil, fmt.Errorf("result store required")
	}
	return &resultStore{store: store}, nil
}
func (s *resultStore) IsDurable() bool { return s.store != nil }
func resultID(ref Ref) string {
	return "goagent/context-result/" + durable.ChildKey(ref.RunKey, ref.Key)
}
func (s *resultStore) runStore(run string) durable.Store {
	if scoped, ok := s.store.(durable.Scoped); ok {
		return scoped.Scope(run)
	}
	return s.store
}

// load 读取持久证据，不存在时 found 为 false。
func load(ctx context.Context, store durable.Store, id string) (saved entry, found bool, err error) {
	snap, err := store.Load(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return saved, false, nil
	}
	if err != nil {
		return saved, false, err
	}
	if snap == nil || snap.RunID != id || snap.Version != resultVersion || json.Unmarshal(snap.Data, &saved) != nil {
		return saved, false, fmt.Errorf("invalid context result record")
	}
	return saved, true, nil
}

// releasedError 把底层作用域的 ErrScopeRemoved 转为 ErrReleased，并保留原错误链。
func releasedError(err error) error {
	if errors.Is(err, durable.ErrScopeRemoved) {
		return fmt.Errorf("%w: %w", ErrReleased, err)
	}
	return err
}
func verify(saved entry, ref Ref) error {
	if saved.Ref != ref || digest(saved.Data) != ref.SHA256 {
		return fmt.Errorf("result identity or content conflict")
	}
	return nil
}

func (s *resultStore) PutIfAbsent(ctx context.Context, ref Ref, data []byte) (err error) {
	if err = validRef(ctx, ref); err != nil {
		return err
	}
	if digest(data) != ref.SHA256 {
		return fmt.Errorf("result content digest mismatch")
	}
	id := resultID(ref)
	saved := entry{Ref: ref, Data: append([]byte(nil), data...)}
	if s.store == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.released[ref.RunKey]; ok {
			return ErrReleased
		}
		if old, ok := s.memory[ref.RunKey][id]; ok {
			return verify(old, ref)
		}
		if s.memory[ref.RunKey] == nil {
			s.memory[ref.RunKey] = map[string]entry{}
		}
		s.memory[ref.RunKey][id] = saved
		return nil
	}
	// 持锁内先读后写，并发写同一引用时后到者读到已有证据并核对内容。
	store := s.runStore(ref.RunKey)
	defer func() { err = releasedError(err) }()
	unlock, err := store.Lock(ctx, id)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	old, found, err := load(ctx, store, id)
	if err != nil {
		return err
	}
	if found {
		return verify(old, ref)
	}
	payload, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	return store.Save(ctx, id, &durable.Snapshot{RunID: id, Version: resultVersion, Data: payload})
}

// Get 不加锁：证据不可变且持久写入是原子的，读到后按摘要核对；读取不存在的引用不产生任何文件。
// 已释放的运行返回 ErrReleased，持久版由底层作用域检查墓碑。
func (s *resultStore) Get(ctx context.Context, ref Ref) ([]byte, error) {
	if err := validRef(ctx, ref); err != nil {
		return nil, err
	}
	id := resultID(ref)
	var saved entry
	found := false
	if s.store == nil {
		s.mu.Lock()
		_, released := s.released[ref.RunKey]
		saved, found = s.memory[ref.RunKey][id]
		s.mu.Unlock()
		if released {
			return nil, ErrReleased
		}
	} else {
		var err error
		if saved, found, err = load(ctx, s.runStore(ref.RunKey), id); err != nil {
			return nil, releasedError(err)
		}
	}
	if !found {
		return nil, os.ErrNotExist
	}
	if err := verify(saved, ref); err != nil {
		return nil, err
	}
	return append([]byte(nil), saved.Data...), nil
}

// Release 实现 ResultStore。
func (s *resultStore) Release(ctx context.Context, run string) error {
	if run == "" {
		return fmt.Errorf("result run key required")
	}
	if s.store == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.memory, run)
		if _, ok := s.released[run]; !ok {
			s.released[run] = s.now()
		}
		return nil
	}
	scoped, err := s.scoped()
	if err != nil {
		return err
	}
	return scoped.RemoveScope(ctx, run)
}

// PruneReleased 实现 ResultStore。重复 Release 不刷新释放时间，与 FileStore 墓碑的 mtime 一致。
func (s *resultStore) PruneReleased(ctx context.Context, olderThan time.Duration) error {
	if s.store != nil {
		scoped, err := s.scoped()
		if err != nil {
			return err
		}
		return scoped.PruneRemoved(ctx, olderThan)
	}
	if olderThan < 0 {
		return fmt.Errorf("墓碑保留时长不能为负：%v", olderThan)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Add(-olderThan)
	for run, at := range s.released {
		if at.Before(cutoff) {
			delete(s.released, run)
		}
	}
	return nil
}

func (s *resultStore) scoped() (durable.Scoped, error) {
	scoped, ok := s.store.(durable.Scoped)
	if !ok {
		return nil, fmt.Errorf("持久结果存储不支持释放：底层 store 未实现 durable.Scoped")
	}
	return scoped, nil
}
