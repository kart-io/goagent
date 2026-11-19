// Package contextpolicy provides context governance components: Offload
// replaces oversized tool results with references and reads them back on
// demand, and ResultStore stores immutable evidence.
package contextpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

const resultVersion = "goagent-context-result-v1"

// Ref identifies one immutable result within a host-owned run.
type Ref struct{ RunKey, Key, SHA256, Version string }

// ResultStore never overwrites a logical key with different evidence.
//
// Release deletes all evidence written by one run (Ref.RunKey) and also
// succeeds when the run does not exist. After release the run key cannot be
// reused: a late PutIfAbsent returns ErrReleased and leaves no data, and Get
// likewise returns ErrReleased (also when a durable Release crashed midway
// before the evidence was deleted).
//
// Rejecting late reads and writes relies on one tombstone per released run key.
// PruneReleased reclaims tombstones released earlier than olderThan, with the
// same semantics for the memory and durable implementations: late reads and
// writes within the threshold still return ErrReleased; after a tombstone is
// reclaimed, Get returns os.ErrNotExist and a late write for that run key
// succeeds and leaves residue. The host should set olderThan greater than the
// longest retry or recovery window (for example, process crash and restart,
// retries) and call it periodically.
type ResultStore interface {
	PutIfAbsent(context.Context, Ref, []byte) error
	Get(context.Context, Ref) ([]byte, error)
	Release(context.Context, string) error
	PruneReleased(ctx context.Context, olderThan time.Duration) error
}
type scopeKey struct{}

// WithResultScope supplies trusted run ownership, never model arguments.
// When the host provides the scope, the host is responsible for reclaiming it:
// after the run ends and will no longer resume, call ResultStore.Release with
// the same run key, and do not reuse that run key afterward.
func WithResultScope(ctx context.Context, runKey string) context.Context {
	return context.WithValue(ctx, scopeKey{}, runKey)
}

// ResultScope returns the trusted scope or the empty string.
func ResultScope(ctx context.Context) string { s, _ := ctx.Value(scopeKey{}).(string); return s }

func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

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
		return agentErrors.Newf(agentErrors.CodeAccessDenied, "invalid or out-of-scope result reference").WithComponent("builder/contextpolicy").WithOperation("valid_ref")
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
// After Release it keeps a tombstone (run key and release time) for the run key
// to reject late reads and writes; PruneReleased reclaims it.
func NewMemoryStore() ResultStore {
	return &resultStore{memory: map[string]map[string]entry{}, released: map[string]time.Time{}, now: time.Now}
}

// NewDurableResultStore stores immutable evidence snapshots independently of operation records.
// When the underlying store implements durable.Scoped (such as
// durable.FileStore), each run's evidence is written to its own scope and
// Release deletes that scope as a whole; the guarantee of rejecting reads and
// writes is provided by the underlying implementation (FileStore covers all
// processes on the host). Otherwise evidence is written to the underlying store
// itself, and Release and PruneReleased return errors.
func NewDurableResultStore(store durable.Store) (ResultStore, error) {
	if store == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "result store required").WithComponent("builder/contextpolicy").WithOperation("new_durable_result_store")
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
		return saved, false, agentErrors.Newf(agentErrors.CodeStateValidation, "invalid context result record").WithComponent("builder/contextpolicy").WithOperation("load")
	}
	return saved, true, nil
}

// releasedError 把底层作用域的 ErrScopeRemoved 转为 ErrReleased，并保留原错误链。
func releasedError(err error) error {
	if errors.Is(err, agentErrors.ErrScopeRemoved) {
		return fmt.Errorf("%w: %w", agentErrors.ErrResultScopeReleased, err)
	}
	return err
}

func verify(saved entry, ref Ref) error {
	if digest(saved.Data) != saved.Ref.SHA256 {
		return agentErrors.Newf(agentErrors.CodeStateValidation, "result identity or content conflict").WithComponent("builder/contextpolicy").WithOperation("verify")
	}
	if saved.Ref != ref {
		return agentErrors.Newf(agentErrors.CodeConflict, "result identity or content conflict").WithComponent("builder/contextpolicy").WithOperation("verify")
	}
	return nil
}

func (s *resultStore) PutIfAbsent(ctx context.Context, ref Ref, data []byte) (err error) {
	if err = validRef(ctx, ref); err != nil {
		return err
	}
	if digest(data) != ref.SHA256 {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "result content digest mismatch").WithComponent("builder/contextpolicy").WithOperation("put_if_absent")
	}
	id := resultID(ref)
	saved := entry{Ref: ref, Data: append([]byte(nil), data...)}
	if s.store == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.released[ref.RunKey]; ok {
			return agentErrors.ErrResultScopeReleased
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

// Get takes no lock: evidence is immutable and durable writes are atomic, so it
// verifies the digest after reading; reading a nonexistent reference creates no
// files. Released runs return ErrReleased; the durable implementation checks
// tombstones in the underlying scope.
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
			return nil, agentErrors.ErrResultScopeReleased
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

// Release implements ResultStore.
func (s *resultStore) Release(ctx context.Context, run string) error {
	if run == "" {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "result run key required").WithComponent("builder/contextpolicy").WithOperation("release")
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

// PruneReleased implements ResultStore. Repeated Release calls do not refresh
// the release time, consistent with the mtime of FileStore tombstones.
func (s *resultStore) PruneReleased(ctx context.Context, olderThan time.Duration) error {
	if s.store != nil {
		scoped, err := s.scoped()
		if err != nil {
			return err
		}
		return scoped.PruneRemoved(ctx, olderThan)
	}
	if olderThan < 0 {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "墓碑保留时长不能为负：%v", olderThan).WithComponent("builder/contextpolicy").WithOperation("prune_released")
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
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "持久结果存储不支持释放：底层 store 未实现 durable.Scoped").WithComponent("builder/contextpolicy").WithOperation("scoped")
	}
	return scoped, nil
}
