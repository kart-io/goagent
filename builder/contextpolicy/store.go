// Package contextpolicy defines execution-local context transforms and immutable evidence references.
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

	"github.com/kart-io/goagent/core/durable"
)

const resultVersion = "goagent-context-result-v1"

// Ref identifies one immutable result within a host-owned run.
type Ref struct{ RunKey, Key, SHA256, Version string }

// ResultStore never overwrites a logical key with different evidence.
type ResultStore interface {
	PutIfAbsent(context.Context, Ref, []byte) error
	Get(context.Context, Ref) ([]byte, error)
}
type scopeKey struct{}

// WithResultScope supplies trusted run ownership, never model arguments.
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
	mu     sync.Mutex
	memory map[string]entry
	store  durable.Store
}

// NewMemoryStore creates a concurrency-safe evidence store.
func NewMemoryStore() ResultStore { return &resultStore{memory: map[string]entry{}} }

// NewDurableResultStore stores immutable evidence snapshots independently of operation records.
func NewDurableResultStore(store durable.Store) (ResultStore, error) {
	if store == nil {
		return nil, fmt.Errorf("result store required")
	}
	return &resultStore{store: store}, nil
}
func (s *resultStore) IsDurable() bool { return s.store != nil }
func (s *resultStore) access(ctx context.Context, ref Ref, data []byte, put bool) (out []byte, err error) {
	if err = validRef(ctx, ref); err != nil {
		return nil, err
	}
	if put && digest(data) != ref.SHA256 {
		return nil, fmt.Errorf("result content digest mismatch")
	}
	id := "goagent/context-result/" + durable.ChildKey(ref.RunKey, ref.Key)
	s.mu.Lock()
	defer s.mu.Unlock()
	var saved entry
	found := false
	if s.store == nil {
		saved, found = s.memory[id]
	} else {
		unlock, e := s.store.Lock(ctx, id)
		if e != nil {
			return nil, e
		}
		defer func() { err = errors.Join(err, unlock()) }()
		snap, e := s.store.Load(ctx, id)
		if errors.Is(e, os.ErrNotExist) {
		} else if e != nil {
			return nil, e
		} else {
			if snap == nil || snap.RunID != id || snap.Version != resultVersion || json.Unmarshal(snap.Data, &saved) != nil {
				return nil, fmt.Errorf("invalid context result record")
			}
			found = true
		}
	}
	if found {
		if saved.Ref != ref || digest(saved.Data) != ref.SHA256 {
			return nil, fmt.Errorf("result identity or content conflict")
		}
		return append([]byte(nil), saved.Data...), nil
	}
	if !put {
		return nil, os.ErrNotExist
	}
	saved = entry{Ref: ref, Data: append([]byte(nil), data...)}
	if s.store == nil {
		s.memory[id] = saved
	} else {
		payload, e := json.Marshal(saved)
		if e != nil {
			return nil, e
		}
		if e = s.store.Save(ctx, id, &durable.Snapshot{RunID: id, Version: resultVersion, Data: payload}); e != nil {
			return nil, e
		}
	}
	return append([]byte(nil), data...), nil
}
func (s *resultStore) PutIfAbsent(ctx context.Context, ref Ref, data []byte) error {
	_, err := s.access(ctx, ref, data, true)
	return err
}
func (s *resultStore) Get(ctx context.Context, ref Ref) ([]byte, error) {
	return s.access(ctx, ref, nil, false)
}
