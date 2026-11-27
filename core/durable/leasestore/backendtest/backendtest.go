// Package backendtest is the conformance suite for leasestore backends. An
// implementation calls Run from its own tests; Run builds two leasestore
// instances on the same backends to simulate two processes and runs the
// durable and session store suites on them.
package backendtest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/durable/leasestore"
	"github.com/kart-io/goagent/core/durable/storetest"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/core/session/sessiontest"
)

// TTL 是用例使用的租约时长。
const TTL = time.Second

// Backends 是一个用例使用的后端：两个实例共享，初始为空。
type Backends struct {
	Backend leasestore.Backend
	// Session 为 nil 时跳过会话日志用例。
	Session leasestore.SessionBackend
	// Expire 使 (ns, id) 的租约在后端立即过期（不改令牌）；为 nil 时跳过租约用例。
	Expire func(t *testing.T, ns, id string)
}

// NewStore 在 b 上创建一个使用测试时长的 Store，测试结束时关闭。
func NewStore(t *testing.T, b leasestore.Backend) *leasestore.Store {
	t.Helper()
	s, err := leasestore.New(leasestore.Config{
		Backend:       b,
		LeaseTTL:      TTL,
		RenewInterval: TTL / 4,
		PollInterval:  10 * time.Millisecond,
		OnError:       func(op string, err error) { t.Logf("leasestore %s: %v", op, err) },
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	})
	return s
}

// Run 对 newBackends 返回的后端运行全部用例；每个用例调用一次 newBackends。
func Run(t *testing.T, newBackends func(t *testing.T) Backends) {
	probe := newBackends(t)
	t.Run("durable", func(t *testing.T) {
		expire := map[durable.Store]func(*testing.T, string, string){}
		var leases *storetest.Leases
		if probe.Expire != nil {
			leases = &storetest.Leases{
				TTL: TTL,
				Expire: func(t *testing.T, s durable.Store, id string) {
					stopRenew(s.(*leasestore.Store), "", id)
					expire[s](t, "", id)
				},
				Crash: func(t *testing.T, s durable.Store) { stopRenew(s.(*leasestore.Store), "", "") },
			}
		}
		storetest.Run(t, func(t *testing.T) (durable.Store, durable.Store) {
			b := newBackends(t)
			a, c := NewStore(t, b.Backend), NewStore(t, b.Backend)
			expire[a], expire[c] = b.Expire, b.Expire
			return a, c
		}, storetest.Options{Scoped: true, Leases: leases})
	})
	if probe.Session == nil {
		return
	}
	t.Run("session", func(t *testing.T) {
		type instance struct {
			root   *leasestore.Store
			expire func(*testing.T, string, string)
		}
		under := map[session.Store]instance{}
		var leases *sessiontest.Leases
		if probe.Expire != nil {
			leases = &sessiontest.Leases{
				TTL: TTL,
				Expire: func(t *testing.T, s session.Store, id, name string) {
					stopRenew(under[s].root, leasestore.SessionNS(id), name)
					under[s].expire(t, leasestore.SessionNS(id), name)
				},
				Crash: func(t *testing.T, s session.Store) { stopRenew(under[s].root, "", "") },
			}
		}
		sessiontest.Run(t, func(t *testing.T) (session.Store, session.Store) {
			b := newBackends(t)
			pair := [2]session.Store{}
			for i := range pair {
				root := NewStore(t, b.Backend)
				s, err := leasestore.NewSessionStore(root, b.Session)
				require.NoError(t, err)
				pair[i], under[s] = s, instance{root: root, expire: b.Expire}
			}
			return pair[0], pair[1]
		}, sessiontest.Options{Leases: leases})
	})
}

// stopRenew 停止续约且不释放；id 为空时停止全部。
func stopRenew(s *leasestore.Store, ns, id string) {
	s.StopRenew(func(l leasestore.Lease) bool { return id == "" || (l.NS == ns && l.ID == id) })
}
