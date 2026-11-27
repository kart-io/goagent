package leasestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// stubBackend 只实现租约的获取与释放，记录引擎的调用；其余方法未实现，调用即 panic。
type stubBackend struct {
	Backend
	acquire  func(ctx context.Context, l Lease) (bool, error)
	acquired []Lease
	released []Lease
}

func (b *stubBackend) AcquireLease(ctx context.Context, l Lease, _ string, _ time.Duration) (bool, error) {
	b.acquired = append(b.acquired, l)
	return b.acquire(ctx, l)
}

func (b *stubBackend) ReleaseLease(_ context.Context, l Lease) error {
	b.released = append(b.released, l)
	return nil
}

func newTestStore(t *testing.T, b Backend) *Store {
	t.Helper()
	s, err := New(Config{Backend: b, LeaseTTL: time.Minute, PollInterval: time.Millisecond})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s
}

// 取消后不能留下闸门、持有者登记或未结束的持有者计数。
func requireIdle(t *testing.T, s *Store) {
	t.Helper()
	s.mu.Lock()
	owners, gates := len(s.owners), len(s.gates)
	s.mu.Unlock()
	require.Zero(t, owners, "不能登记持有者")
	require.Zero(t, gates, "必须释放本地闸门")
	done := make(chan struct{})
	go func() { s.holders.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("仍有未完成的持有者计数")
	}
}

func TestLockFailureLeavesNoState(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name string
		// ctx 返回 Lock 使用的上下文；acquire 可在后端调用中取消它。
		ctx     func() (context.Context, context.CancelFunc)
		acquire func(cancel context.CancelFunc) func(context.Context, Lease) (bool, error)
		wantErr []error
		// calls 是后端被调用的次数，released 是引擎补偿释放的次数。
		calls, released int
	}{
		{name: "已取消的调用方不触达后端",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			wantErr: []error{agentErrors.ErrLockUnavailable, context.Canceled}},
		{name: "已过期的调用方不触达后端",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			wantErr: []error{agentErrors.ErrLockUnavailable, context.DeadlineExceeded}},
		{name: "后端错误原样返回",
			acquire: func(context.CancelFunc) func(context.Context, Lease) (bool, error) {
				return func(context.Context, Lease) (bool, error) { return false, boom }
			},
			wantErr: []error{boom}, calls: 1},
		{name: "结果不确定时补偿释放",
			acquire: func(context.CancelFunc) func(context.Context, Lease) (bool, error) {
				return func(context.Context, Lease) (bool, error) { return false, Uncertain(boom) }
			},
			wantErr: []error{boom}, calls: 1, released: 1},
		{name: "获取成功后才发现取消须释放",
			acquire: func(cancel context.CancelFunc) func(context.Context, Lease) (bool, error) {
				return func(context.Context, Lease) (bool, error) { cancel(); return true, nil }
			},
			wantErr: []error{agentErrors.ErrLockUnavailable, context.Canceled}, calls: 1, released: 1},
		{name: "等待他人释放时取消",
			acquire: func(cancel context.CancelFunc) func(context.Context, Lease) (bool, error) {
				return func(context.Context, Lease) (bool, error) { cancel(); return false, nil }
			},
			wantErr: []error{agentErrors.ErrLockUnavailable, context.Canceled}, calls: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if c.ctx != nil {
				ctx, cancel = c.ctx()
			}
			defer cancel()
			b := &stubBackend{}
			if c.acquire != nil {
				b.acquire = c.acquire(cancel)
			}
			s := newTestStore(t, b)
			unlock, err := s.Scope("tenant").Lock(ctx, "run")
			require.Nil(t, unlock)
			for _, want := range c.wantErr {
				require.ErrorIs(t, err, want)
			}
			require.Len(t, b.acquired, c.calls)
			require.Len(t, b.released, c.released)
			if c.released == 1 {
				require.Equal(t, b.acquired[0], b.released[0], "补偿须使用本次获取的令牌")
				require.Equal(t, ScopeNS("tenant"), b.released[0].NS)
			}
			requireIdle(t, s)
		})
	}
}

func TestUnlockReleasesOnceAndCloseRejectsLock(t *testing.T) {
	b := &stubBackend{acquire: func(context.Context, Lease) (bool, error) { return true, nil }}
	s := newTestStore(t, b)
	unlock, err := s.Lock(context.Background(), "run")
	require.NoError(t, err)
	require.NoError(t, unlock())
	require.NoError(t, unlock(), "重复解锁不报错")
	require.Len(t, b.released, 1)
	requireIdle(t, s)

	require.NoError(t, s.Close(context.Background()))
	_, err = s.Lock(context.Background(), "run")
	require.ErrorIs(t, err, ErrClosed)
}

// 不持锁的写入不触达后端。
func TestSaveRequiresHeldLock(t *testing.T) {
	s := newTestStore(t, &stubBackend{})
	err := s.Save(context.Background(), "run", snapshotForTest())
	require.ErrorIs(t, err, agentErrors.ErrLeaseLost)
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"缺少后端", Config{}},
		{"续约间隔不小于租约时长", Config{Backend: &stubBackend{}, LeaseTTL: time.Second, RenewInterval: time.Second}},
		{"负的租约时长", Config{Backend: &stubBackend{}, LeaseTTL: -time.Second}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := New(c.cfg)
			require.Error(t, err)
		})
	}
}

func snapshotForTest() *durable.Snapshot { return &durable.Snapshot{Version: "v1", RunID: "run"} }
