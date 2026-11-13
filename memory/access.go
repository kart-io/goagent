package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
)

// accessID 是作用域内的访问记录（TrackAccess），与 state 分开存放、独立加锁：召回不写 state。
const accessID = "memory/access"

// accessEntry 是一条记录最近一次被召回的时间与累计次数。
type accessEntry struct {
	At time.Time `json:"at"`
	N  int64     `json:"n"`
}

// loadAccess 读取作用域的访问记录；不存在或作用域已删除时为空。
func (m *Memory) loadAccess(ctx context.Context, ns []string, scope string) (map[string]accessEntry, error) {
	a := map[string]accessEntry{}
	_, _, err := loadJSON(ctx, m.scoped(ns, scope), accessID, &a)
	if errors.Is(err, agentErrors.ErrScopeRemoved) {
		return map[string]accessEntry{}, nil
	}
	return a, err
}

// editAccess 在访问记录的锁内修改并保存。
func (m *Memory) editAccess(ctx context.Context, ns []string, scope string, fn func(map[string]accessEntry)) error {
	err := editJSON(ctx, m.scoped(ns, scope), accessID, func(a *map[string]accessEntry) bool {
		if *a == nil {
			*a = map[string]accessEntry{}
		}
		fn(*a)
		return true
	})
	if errors.Is(err, agentErrors.ErrScopeRemoved) {
		return nil
	}
	return err
}

// editJSON 锁住 id，读取、修改并保存；fn 返回 false 时不保存。
func editJSON[T any](ctx context.Context, st durable.Store, id string, fn func(*T) bool) error {
	unlock, err := st.Lock(ctx, id)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()
	var v T
	if _, _, err := loadJSON(ctx, st, id, &v); err != nil {
		return err
	}
	if !fn(&v) {
		return nil
	}
	snap, err := encodeSnapshot(id, v)
	if err != nil {
		return err
	}
	return st.Save(context.WithoutCancel(ctx), id, snap)
}

// accessBatcher 合并召回产生的访问，按 (命名空间, 作用域) 由一个后台协程串行写入：同一键同时最多一次写入，期间到达的访问并入下一批。
// 写入失败时丢弃这一批（访问时间只影响排序与淘汰）。
type accessBatcher struct {
	mu      sync.Mutex
	pending map[string]*accessBatch
	wg      sync.WaitGroup
}

type accessBatch struct {
	ns      []string
	scope   string
	hits    map[string]time.Time
	running bool
}

// touch 登记 out 中的记录在此刻被召回。TrackAccess 关闭时不做任何事。
func (m *Memory) touch(ns []string, scope string, out []Scored) {
	if !m.cfg.TrackAccess || len(out) == 0 {
		return
	}
	now := m.now()
	b := &m.access
	key := store.NamespaceKey(ns) + "#" + scope
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		b.pending = map[string]*accessBatch{}
	}
	p := b.pending[key]
	if p == nil {
		p = &accessBatch{ns: ns, scope: scope}
		b.pending[key] = p
	}
	if p.hits == nil {
		p.hits = map[string]time.Time{}
	}
	for _, r := range out {
		p.hits[r.ID] = now
	}
	if p.running {
		return
	}
	p.running = true
	b.wg.Add(1)
	go m.flushAccess(key, p)
}

// flushAccess 写入键 key 的待写访问，直到没有新的访问为止。
func (m *Memory) flushAccess(key string, p *accessBatch) {
	b := &m.access
	defer b.wg.Done()
	for {
		b.mu.Lock()
		hits := p.hits
		p.hits = nil
		if len(hits) == 0 {
			p.running = false
			delete(b.pending, key)
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), m.commitTimeout)
		_ = m.editAccess(ctx, p.ns, p.scope, func(a map[string]accessEntry) {
			for id, at := range hits {
				e := a[id]
				e.N++
				if at.After(e.At) {
					e.At = at
				}
				a[id] = e
			}
		})
		cancel()
	}
}

// waitAccess 等待已登记的访问写完（测试与关闭时使用）。
func (m *Memory) waitAccess() { m.access.wg.Wait() }
