package leasestore_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable/leasestore"
	"github.com/kart-io/goagent/core/durable/leasestore/backendtest"
	agentErrors "github.com/kart-io/goagent/errors"
)

// memBackend 是测试用的内存后端：一把互斥锁让每个方法都是原子操作。
type memBackend struct {
	mu       sync.Mutex
	leases   map[[2]string]memLease
	snaps    map[[2]string][]byte
	tombs    map[string]time.Time
	sessions map[string][][]byte
}

type memLease struct {
	token   string
	expires time.Time
}

func newMemBackend() *memBackend {
	return &memBackend{leases: map[[2]string]memLease{}, snaps: map[[2]string][]byte{}, tombs: map[string]time.Time{}, sessions: map[string][][]byte{}}
}

func (m *memBackend) removed(ns string) error {
	if _, ok := m.tombs[ns]; ok {
		return agentErrors.ErrScopeRemoved
	}
	return nil
}

func (m *memBackend) AcquireLease(_ context.Context, l leasestore.Lease, _ string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.removed(l.NS); err != nil {
		return false, err
	}
	k := [2]string{l.NS, l.ID}
	if cur, ok := m.leases[k]; ok && !cur.expires.Before(time.Now()) {
		return false, nil
	}
	m.leases[k] = memLease{token: l.Token, expires: time.Now().Add(ttl)}
	return true, nil
}

func (m *memBackend) ReleaseLease(_ context.Context, l leasestore.Lease) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := [2]string{l.NS, l.ID}
	if m.leases[k].token == l.Token {
		delete(m.leases, k)
	}
	return nil
}

// verify 校验令牌并续约，须在持锁时调用。
func (m *memBackend) verify(l leasestore.Lease, ttl time.Duration) error {
	k := [2]string{l.NS, l.ID}
	if cur, ok := m.leases[k]; !ok || cur.token != l.Token {
		return agentErrors.ErrLeaseLost
	}
	m.leases[k] = memLease{token: l.Token, expires: time.Now().Add(ttl)}
	return nil
}

func (m *memBackend) RenewLeases(_ context.Context, leases []leasestore.Lease, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range leases {
		_ = m.verify(l, ttl)
	}
	return nil
}

func (m *memBackend) DeleteExpiredLeases(_ context.Context, grace time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, l := range m.leases {
		if l.expires.Before(time.Now().Add(-grace)) {
			delete(m.leases, k)
		}
	}
	return nil
}

func (m *memBackend) LoadSnapshot(_ context.Context, ns, id string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.removed(ns); err != nil {
		return nil, false, err
	}
	data, ok := m.snaps[[2]string{ns, id}]
	return data, ok, nil
}

func (m *memBackend) SaveSnapshots(_ context.Context, ns string, writes []leasestore.SnapshotWrite, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.removed(ns); err != nil {
		return err
	}
	for _, w := range writes {
		if cur, ok := m.leases[[2]string{ns, w.Lease.ID}]; !ok || cur.token != w.Lease.Token {
			return agentErrors.ErrLeaseLost
		}
	}
	for _, w := range writes {
		_ = m.verify(w.Lease, ttl)
		m.snaps[[2]string{ns, w.Lease.ID}] = w.Data
	}
	return nil
}

func (m *memBackend) purge(ns string) {
	for k := range m.snaps {
		if k[0] == ns {
			delete(m.snaps, k)
		}
	}
	for k := range m.leases {
		if k[0] == ns {
			delete(m.leases, k)
		}
	}
}

func (m *memBackend) RemoveScope(_ context.Context, ns string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tombs[ns]; !ok {
		m.tombs[ns] = time.Now()
	}
	m.purge(ns)
	return nil
}

func (m *memBackend) PruneScopes(_ context.Context, olderThan time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ns, at := range m.tombs {
		if at.Before(time.Now().Add(-olderThan)) {
			m.purge(ns)
			delete(m.tombs, ns)
		}
	}
	return nil
}

func (m *memBackend) LastSeq(_ context.Context, id string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.sessions[id])), nil
}

func (m *memBackend) AppendEvents(_ context.Context, batches []leasestore.EventBatch, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range batches {
		for _, l := range b.Fences {
			if cur, ok := m.leases[[2]string{l.NS, l.ID}]; !ok || cur.token != l.Token {
				return agentErrors.ErrLeaseLost
			}
		}
		if cur := int64(len(m.sessions[b.SessionID])); cur != b.Expected {
			return leasestore.Conflict(b.SessionID, b.Expected, cur)
		}
	}
	for _, b := range batches {
		for _, l := range b.Fences {
			_ = m.verify(l, ttl)
		}
		if len(b.Data) > 0 {
			m.sessions[b.SessionID] = append(m.sessions[b.SessionID], b.Data...)
		}
	}
	return nil
}

func (m *memBackend) ReadEvents(_ context.Context, id string, after int64) ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	events, ok := m.sessions[id]
	if !ok {
		return nil, agentErrors.ErrSessionNotFound
	}
	if after >= int64(len(events)) {
		return nil, nil
	}
	return append([][]byte(nil), events[max(after, 0):]...), nil
}

func (m *memBackend) ListSessions(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func (m *memBackend) ForkSession(_ context.Context, src, dst string, upTo int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	events, ok := m.sessions[src]
	if !ok {
		return agentErrors.ErrSessionNotFound
	}
	if _, ok := m.sessions[dst]; ok {
		return agentErrors.ErrSessionExists
	}
	n := int64(len(events))
	if upTo > 0 {
		n = min(n, upTo)
	}
	m.sessions[dst] = append([][]byte{}, events[:n]...)
	return nil
}

func (m *memBackend) DeleteSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

func (m *memBackend) expire(_ *testing.T, ns, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := [2]string{ns, id}
	if l, ok := m.leases[k]; ok {
		l.expires = time.Now().Add(-time.Second)
		m.leases[k] = l
	}
}

func TestConformance(t *testing.T) {
	backendtest.Run(t, func(*testing.T) backendtest.Backends {
		m := newMemBackend()
		return backendtest.Backends{Backend: m, Session: m, Expire: m.expire}
	})
}
