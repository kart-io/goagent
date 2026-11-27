package leasestore

import (
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// ErrClosed means the Store is closed and accepts no new Lock.
var ErrClosed = errors.New("leasestore 已关闭")

// Config configures a Store. A zero duration selects the default.
type Config struct {
	// Backend carries locking, reads, writes and scope operations.
	Backend Backend
	// RenewBackend carries renewal and expired-lease cleanup, so that they do
	// not queue behind foreground work (for example a small dedicated
	// connection pool). nil uses Backend.
	RenewBackend Backend
	// LeaseTTL defaults to 30s and must exceed a failover of the backend. 0
	// selects the default; it cannot mean "no expiry", because a lease that
	// never expires would keep a crashed holder's lock forever.
	LeaseTTL time.Duration
	// RenewInterval defaults to LeaseTTL/3. 0 selects the default; it cannot
	// mean "never renew" (New requires 0 < RenewInterval < LeaseTTL).
	RenewInterval time.Duration
	// PollInterval defaults to 50ms (plus up to the same again in jitter) and
	// only paces waiting for another instance to release a lock. 0 selects the
	// default; it cannot mean "unlimited", because a zero interval would
	// busy-loop on the backend.
	PollInterval time.Duration
	// OpTimeout bounds internal operations (acquire, release, renew, cleanup);
	// the default is 3s. 0 selects the default; it cannot mean "no timeout",
	// because an unbounded backend call would stall renewal and shutdown.
	OpTimeout time.Duration
	// ExpiredGrace is how long an expired lease is kept before cleanup; the
	// default is 10*LeaseTTL. 0 selects the default; it cannot mean "keep
	// forever" or "clean immediately", because the former leaks leases and the
	// latter races a slow holder's renewal.
	ExpiredGrace time.Duration
	// Owner is stored with the lease for diagnostics only; the default is
	// hostname:pid.
	Owner string
	// OnError receives errors swallowed in the background and on unlock
	// (renew, cleanup, release); it may be nil.
	OnError func(op string, err error)
}

// Store implements durable.Store and durable.Scoped on a Backend.
//
// A lock is a lease with an expiry: it is renewed in the background, and after
// the holder crashes it expires after LeaseTTL so another instance can take
// over. Every write carries the lease token, and a holder that was taken over
// gets errors.ErrLeaseLost. Inside one process each ID also has a gate held
// until that holder unlocks, so an old holder never borrows a new holder's
// token.
type Store struct {
	cfg Config

	mu      sync.Mutex
	closed  bool
	gates   map[string]*gate
	owners  map[string]*hold
	holders sync.WaitGroup

	stop      chan struct{}
	renewDone chan struct{}
}

// gate 是进程内某个 (ns, id) 的闸门，refs 计数等待者与持有者，归零时从表中删除。
type gate struct {
	sem  chan struct{}
	refs int
}

// hold 是本进程持有的一个租约；renew 由 mu 保护，置 false 后不再续约。
type hold struct {
	lease Lease
	renew bool
}

var (
	_ durable.Store  = (*Store)(nil)
	_ durable.Scoped = (*Store)(nil)
)

// New validates cfg and starts background renewal.
func New(cfg Config) (*Store, error) {
	if cfg.Backend == nil {
		return nil, fmt.Errorf("leasestore 需要 Backend")
	}
	if cfg.RenewBackend == nil {
		cfg.RenewBackend = cfg.Backend
	}
	if cfg.LeaseTTL == 0 {
		cfg.LeaseTTL = 30 * time.Second
	}
	if cfg.RenewInterval == 0 {
		cfg.RenewInterval = cfg.LeaseTTL / 3
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 50 * time.Millisecond
	}
	if cfg.OpTimeout == 0 {
		cfg.OpTimeout = 3 * time.Second
	}
	if cfg.ExpiredGrace == 0 {
		cfg.ExpiredGrace = 10 * cfg.LeaseTTL
	}
	if cfg.LeaseTTL < 0 || cfg.RenewInterval <= 0 || cfg.RenewInterval >= cfg.LeaseTTL || cfg.PollInterval < 0 || cfg.OpTimeout < 0 || cfg.ExpiredGrace < 0 {
		return nil, fmt.Errorf("leasestore 配置无效：须满足 0 < RenewInterval < LeaseTTL，其余时长非负")
	}
	if cfg.Owner == "" {
		host, _ := os.Hostname()
		cfg.Owner = fmt.Sprintf("%s:%d", host, os.Getpid())
	}
	s := &Store{cfg: cfg, gates: map[string]*gate{}, owners: map[string]*hold{}, stop: make(chan struct{}), renewDone: make(chan struct{})}
	go s.background()
	return s, nil
}

func key(ns, id string) string { return ns + "\x00" + id }

// ScopeNS is the namespace a Backend receives for the scope name; the prefix
// keeps it apart from the root namespace "".
func ScopeNS(name string) string { return "s:" + name }

// SessionNS is the namespace a Backend receives for the locks of session id.
func SessionNS(id string) string { return "session:" + id }

func (s *Store) report(op string, err error) {
	if err != nil && s.cfg.OnError != nil {
		s.cfg.OnError(op, err)
	}
}

func (s *Store) opContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.cfg.OpTimeout)
}

func lockUnavailable(ctx context.Context) error {
	return fmt.Errorf("%w: %w", agentErrors.ErrLockUnavailable, ctx.Err())
}

// Lock implements durable.Store.
func (s *Store) Lock(ctx context.Context, id string) (func() error, error) {
	return s.lock(ctx, "", id)
}

// Load implements durable.Store.
func (s *Store) Load(ctx context.Context, id string) (*durable.Snapshot, error) {
	return s.load(ctx, "", id)
}

// Save implements durable.Store.
func (s *Store) Save(ctx context.Context, id string, snap *durable.Snapshot) error {
	return s.saveAll(ctx, "", []durable.Write{{ID: id, Snapshot: snap}})
}

// SaveAll implements durable.Store: one backend transaction verifies every
// lease and saves every snapshot.
func (s *Store) SaveAll(ctx context.Context, writes ...durable.Write) error {
	return s.saveAll(ctx, "", writes)
}

func (s *Store) enterGate(k string) *gate {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.gates[k]
	if g == nil {
		g = &gate{sem: make(chan struct{}, 1)}
		s.gates[k] = g
	}
	g.refs++
	return g
}

func (s *Store) leaveGate(k string, g *gate, held bool) {
	if held {
		<-g.sem
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if g.refs--; g.refs == 0 {
		delete(s.gates, k)
	}
}

func (s *Store) lock(ctx context.Context, ns, id string) (func() error, error) {
	s.mu.Lock()
	closed := s.closed
	if !closed {
		s.holders.Add(1)
	}
	s.mu.Unlock()
	if closed {
		return nil, ErrClosed
	}
	unlock, err := s.acquire(ctx, ns, id)
	if err != nil {
		s.holders.Done()
	}
	return unlock, err
}

func (s *Store) acquire(ctx context.Context, ns, id string) (func() error, error) {
	k := key(ns, id)
	g := s.enterGate(k)
	select {
	case g.sem <- struct{}{}:
	case <-ctx.Done():
		s.leaveGate(k, g, false)
		return nil, lockUnavailable(ctx)
	}
	for {
		if ctx.Err() != nil {
			s.leaveGate(k, g, true)
			return nil, lockUnavailable(ctx)
		}
		l := Lease{NS: ns, ID: id, Token: crand.Text()}
		ok, err := s.tryAcquire(ctx, l)
		if err != nil {
			s.leaveGate(k, g, true)
			if ctx.Err() != nil {
				return nil, lockUnavailable(ctx)
			}
			return nil, err
		}
		if ok {
			h := &hold{lease: l, renew: true}
			s.mu.Lock()
			if ctx.Err() != nil {
				s.mu.Unlock()
				// 已提交但尚未交给调用方，按自身令牌清理，不能登记为活跃持有者。
				s.release(l)
				s.leaveGate(k, g, true)
				return nil, lockUnavailable(ctx)
			}
			s.owners[k] = h
			s.mu.Unlock()
			return s.unlocker(k, g, h), nil
		}
		wait := s.cfg.PollInterval + rand.N(s.cfg.PollInterval+1)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			s.leaveGate(k, g, true)
			return nil, lockUnavailable(ctx)
		}
	}
}

// tryAcquire 响应调用方取消，同时受 OpTimeout 限制。后端报告写入结果不确定时，
// 用独立期限补偿：取消不能取消补偿。
func (s *Store) tryAcquire(caller context.Context, l Lease) (bool, error) {
	ctx, cancel := context.WithTimeout(caller, s.cfg.OpTimeout)
	defer cancel()
	ok, err := s.cfg.Backend.AcquireLease(ctx, l, s.cfg.Owner, s.cfg.LeaseTTL)
	var u uncertain
	if errors.As(err, &u) {
		s.release(l)
		return false, u.err
	}
	return ok, err
}

func (s *Store) release(l Lease) {
	ctx, cancel := s.opContext()
	defer cancel()
	s.report("delete_lease", s.cfg.Backend.ReleaseLease(ctx, l))
}

// unlocker 返回的函数可重复调用。租约已被接管或删除失败时返回 nil，经 OnError 报告：此前的写入都受令牌保护。
func (s *Store) unlocker(k string, g *gate, h *hold) func() error {
	var once sync.Once
	return func() error {
		once.Do(func() {
			s.mu.Lock()
			if s.owners[k] == h {
				delete(s.owners, k)
			}
			s.mu.Unlock()
			s.release(h.lease)
			s.leaveGate(k, g, true)
			s.holders.Done()
		})
		return nil
	}
}

// held 返回本进程持有的 (ns, id) 租约。
func (s *Store) held(ns, id string) (Lease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.owners[key(ns, id)]
	if h == nil {
		return Lease{}, false
	}
	return h.lease, true
}

func (s *Store) load(ctx context.Context, ns, id string) (*durable.Snapshot, error) {
	data, found, err := s.cfg.Backend.LoadSnapshot(ctx, ns, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("持久快照 %q 不存在：%w", id, os.ErrNotExist)
	}
	var snap durable.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("持久快照 %q 已损坏：%w", id, err)
	}
	return &snap, nil
}

// saveAll 按 ID 排序后交给后端，避免并发批量写入互相等待行锁。
func (s *Store) saveAll(ctx context.Context, ns string, writes []durable.Write) error {
	if err := durable.ValidateWrites(writes); err != nil {
		return err
	}
	if len(writes) == 0 {
		return nil
	}
	rows := make([]SnapshotWrite, len(writes))
	for i, write := range writes {
		l, ok := s.held(ns, write.ID)
		if !ok {
			return fmt.Errorf("%w：本进程未持有 %q 的锁", agentErrors.ErrLeaseLost, write.ID)
		}
		data, err := json.Marshal(write.Snapshot)
		if err != nil {
			return err
		}
		rows[i] = SnapshotWrite{Lease: l, Data: data}
	}
	slices.SortFunc(rows, func(a, b SnapshotWrite) int { return strings.Compare(a.Lease.ID, b.Lease.ID) })
	return s.cfg.Backend.SaveSnapshots(ctx, ns, rows, s.cfg.LeaseTTL)
}

// scope 是 Store 的作用域，与根 Store 共享进程内闸门、持有表与续约。
type scope struct {
	root *Store
	ns   string
}

// Scope implements durable.Scoped: it returns an independent namespace without
// writing anything.
func (s *Store) Scope(name string) durable.Store { return &scope{root: s, ns: ScopeNS(name)} }

func (c *scope) Lock(ctx context.Context, id string) (func() error, error) {
	return c.root.lock(ctx, c.ns, id)
}

func (c *scope) Load(ctx context.Context, id string) (*durable.Snapshot, error) {
	return c.root.load(ctx, c.ns, id)
}

func (c *scope) Save(ctx context.Context, id string, snap *durable.Snapshot) error {
	return c.root.saveAll(ctx, c.ns, []durable.Write{{ID: id, Snapshot: snap}})
}

func (c *scope) SaveAll(ctx context.Context, writes ...durable.Write) error {
	return c.root.saveAll(ctx, c.ns, writes)
}

// RemoveScope implements durable.Scoped. It does not wait for holders in
// progress.
func (s *Store) RemoveScope(ctx context.Context, name string) error {
	return s.cfg.Backend.RemoveScope(ctx, ScopeNS(name))
}

// PruneRemoved implements durable.Scoped. After a tombstone is reclaimed a
// late write to the same name succeeds, so olderThan must exceed the longest
// retry or recovery window.
func (s *Store) PruneRemoved(ctx context.Context, olderThan time.Duration) error {
	if olderThan < 0 {
		return fmt.Errorf("墓碑保留时长不能为负：%v", olderThan)
	}
	return s.cfg.Backend.PruneScopes(ctx, olderThan)
}

// StopRenew stops renewing the held leases match selects, without releasing
// them: the holder still believes it holds the lock. It simulates a paused or
// crashed holder in tests of a Backend.
func (s *Store) StopRenew(match func(Lease) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.owners {
		if match(h.lease) {
			h.renew = false
		}
	}
}

// background 定期批量续约本进程持有的租约，并清理过期已久的租约。
func (s *Store) background() {
	defer close(s.renewDone)
	ticker := time.NewTicker(s.cfg.RenewInterval)
	defer ticker.Stop()
	lastCleanup := time.Now()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
		}
		s.renew()
		if time.Since(lastCleanup) >= s.cfg.ExpiredGrace {
			lastCleanup = time.Now()
			// 持久操作 ID 由执行键派生，崩溃留下的租约可能永远不会再被获取；
			// 删除是安全的，旧持有者写入时因令牌不存在而被拒。
			ctx, cancel := s.opContext()
			s.report("cleanup", s.cfg.RenewBackend.DeleteExpiredLeases(ctx, s.cfg.ExpiredGrace))
			cancel()
		}
	}
}

func (s *Store) renew() {
	var leases []Lease
	s.mu.Lock()
	for _, h := range s.owners {
		if h.renew {
			leases = append(leases, h.lease)
		}
	}
	s.mu.Unlock()
	if len(leases) == 0 {
		return
	}
	ctx, cancel := s.opContext()
	defer cancel()
	s.report("renew", s.cfg.RenewBackend.RenewLeases(ctx, leases, s.cfg.LeaseTTL))
}

// Close rejects new Locks, waits until ctx ends for current holders to unlock,
// then stops renewal and releases the remaining leases. It does not close the
// Backend. On timeout it returns the ctx error, and later writes of holders
// still running may fail because their lease was taken over.
func (s *Store) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.holders.Wait()
		close(done)
	}()
	var err error
	select {
	case <-done:
	case <-ctx.Done():
		err = ctx.Err()
	}
	close(s.stop)
	<-s.renewDone
	s.mu.Lock()
	remaining := make([]Lease, 0, len(s.owners))
	for _, h := range s.owners {
		h.renew = false
		remaining = append(remaining, h.lease)
	}
	s.mu.Unlock()
	for _, l := range remaining {
		s.release(l)
	}
	return err
}
