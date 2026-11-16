package memory

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
)

type eraseCall struct {
	ns    []string
	scope string
}

// eraseRecorder 记录 OnErase 的调用；fail 为剩余的失败次数（负数表示一直失败），hook 在每次调用时运行（不持锁）。
type eraseRecorder struct {
	mu    sync.Mutex
	calls []eraseCall
	fail  int
	hook  func(ctx context.Context)
}

var errEraseFailed = errors.New("模拟擦除失败")

func (r *eraseRecorder) erase(ctx context.Context, ns []string, scope string) error {
	r.mu.Lock()
	r.calls = append(r.calls, eraseCall{ns: slices.Clone(ns), scope: scope})
	failed := r.fail != 0
	if r.fail > 0 {
		r.fail--
	}
	hook := r.hook
	r.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	if failed {
		return errEraseFailed
	}
	return nil
}

func (r *eraseRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *eraseRecorder) take() []eraseCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.calls
	r.calls = nil
	return out
}

func (r *eraseRecorder) set(fail int, hook func(context.Context)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail, r.hook = fail, hook
}

type eraseFixture struct {
	m   *Memory
	st  *hookStore
	rec *eraseRecorder
	ctx context.Context
	now time.Time
}

func (f *eraseFixture) advance(d time.Duration) { f.now = f.now.Add(d) }

func (f *eraseFixture) remember(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		_, err := f.m.Remember(f.ctx, Record{ID: id, Content: "用户喜欢咖啡 " + id, Importance: 0.5})
		require.NoError(t, err)
	}
}

// eraseEntries 返回 queue 中的擦除登记。
func (f *eraseFixture) eraseEntries(t *testing.T) []queueEntry {
	t.Helper()
	entries, err := f.m.queued(context.Background())
	require.NoError(t, err)
	var out []queueEntry
	for _, e := range entries {
		if e.Reason == queueErase {
			out = append(out, e)
		}
	}
	return out
}

func newEraseFixture(t *testing.T, withErase bool) *eraseFixture {
	t.Helper()
	f := &eraseFixture{st: newHookStore(t), rec: &eraseRecorder{}, ctx: WithNamespace(context.Background(), testNS...),
		now: time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)}
	cfg := Config{Durable: f.st}
	if withErase {
		cfg.OnErase = f.rec.erase
	}
	f.m = newMemory(t, cfg)
	f.m.now = func() time.Time { return f.now }
	return f
}

func TestOnErase(t *testing.T) {
	cur := eraseCall{ns: testNS, scope: initialScope}
	within := constants.DefaultMemoryEraseWithin
	tests := []struct {
		name      string
		withErase bool
		run       func(t *testing.T, f *eraseFixture)
	}{
		{
			name:      "Forget 提交后立即擦除当前作用域",
			withErase: true,
			run: func(t *testing.T, f *eraseFixture) {
				f.remember(t, "a")
				require.Empty(t, f.rec.take(), "写入不擦除")
				require.NoError(t, f.m.Forget(f.ctx, "a"))
				require.Equal(t, []eraseCall{cur}, f.rec.take())
				require.Equal(t, []queueEntry{{Namespace: testNS, Scope: initialScope, Reason: queueErase, Until: f.now.Add(within)}},
					f.eraseEntries(t))
			},
		},
		{
			name:      "Rewound 提交后立即擦除当前作用域",
			withErase: true,
			run: func(t *testing.T, f *eraseFixture) {
				f.remember(t, "a")
				require.NoError(t, f.m.Rewound(f.ctx, "s", 4))
				require.Equal(t, []eraseCall{cur}, f.rec.take())
				require.Equal(t, []queueEntry{{Namespace: testNS, Scope: initialScope, Reason: queueErase, Until: f.now.Add(within)}},
					f.eraseEntries(t))
			},
		},
		{
			name:      "Purge 之后 retire 擦除旧作用域，RemoveScope 失败也擦除",
			withErase: true,
			run: func(t *testing.T, f *eraseFixture) {
				f.remember(t, "a")
				errCrash := errors.New("模拟 RemoveScope 失败")
				f.st.h.before = func(op, _, _ string) error {
					if op == "removescope" {
						return errCrash
					}
					return nil
				}
				require.ErrorIs(t, f.m.Purge(f.ctx), errCrash)
				require.Equal(t, []eraseCall{cur}, f.rec.take(), "Purge 的清理擦除旧作用域")
				require.ErrorIs(t, f.m.Maintain(f.ctx), errCrash)
				require.Equal(t, []eraseCall{cur}, f.rec.take(), "维护的 retire 再次擦除旧作用域")
				f.st.h.before = nil
				f.rec.set(1, nil)
				require.ErrorIs(t, f.m.Maintain(f.ctx), errEraseFailed, "擦除失败时 retire 返回错误")
				require.Len(t, f.rec.take(), 1)
			},
		},
		{
			name:      "提交后的擦除失败交给维护重做",
			withErase: true,
			run: func(t *testing.T, f *eraseFixture) {
				f.remember(t, "a")
				f.rec.set(1, nil)
				require.NoError(t, f.m.Forget(f.ctx, "a"), "已经提交，擦除失败不返回")
				require.Equal(t, 1, f.rec.count())
				require.NoError(t, f.m.Forget(f.ctx, "a"))
				require.Equal(t, 1, f.rec.count(), "已遗忘时直接返回，不擦除")
				require.NoError(t, f.m.Maintain(f.ctx))
				require.Equal(t, 2, f.rec.count(), "维护重做擦除")
			},
		},
		{
			name:      "Until 之前每轮重做，到期后成功一次即注销",
			withErase: true,
			run: func(t *testing.T, f *eraseFixture) {
				f.remember(t, "a")
				require.NoError(t, f.m.Forget(f.ctx, "a"))
				f.rec.take()
				require.NoError(t, f.m.Maintain(f.ctx))
				require.Len(t, f.rec.take(), 1)
				f.advance(within - time.Nanosecond)
				require.NoError(t, f.m.Maintain(f.ctx))
				require.Len(t, f.rec.take(), 1)
				require.Len(t, f.eraseEntries(t), 1, "Until 之前保留登记")
				f.advance(time.Nanosecond)
				require.NoError(t, f.m.Maintain(f.ctx))
				require.Len(t, f.rec.take(), 1)
				require.Empty(t, f.eraseEntries(t), "到期后的擦除成功即注销")
				require.NoError(t, f.m.Maintain(f.ctx))
				require.Empty(t, f.rec.take(), "注销之后不再擦除")
			},
		},
		{
			name:      "到期后擦除仍失败时保留登记并返回错误",
			withErase: true,
			run: func(t *testing.T, f *eraseFixture) {
				f.remember(t, "a")
				require.NoError(t, f.m.Forget(f.ctx, "a"))
				f.advance(within + time.Minute)
				f.rec.set(-1, nil)
				require.ErrorIs(t, f.m.Maintain(f.ctx), errEraseFailed)
				require.Len(t, f.eraseEntries(t), 1)
				f.rec.set(0, nil)
				require.NoError(t, f.m.Maintain(f.ctx))
				require.Empty(t, f.eraseEntries(t))
			},
		},
		{
			name:      "窗口内再次 Forget 延长 Until",
			withErase: true,
			run: func(t *testing.T, f *eraseFixture) {
				f.remember(t, "a", "b")
				require.NoError(t, f.m.Forget(f.ctx, "a"))
				f.advance(time.Minute)
				require.NoError(t, f.m.Forget(f.ctx, "b"))
				got := f.eraseEntries(t)
				require.Len(t, got, 1)
				require.Equal(t, f.now.Add(within), got[0].Until)
			},
		},
		{
			name:      "到期擦除期间提交的 Forget 不被注销",
			withErase: true,
			run: func(t *testing.T, f *eraseFixture) {
				f.remember(t, "a", "b")
				require.NoError(t, f.m.Forget(f.ctx, "a"))
				f.advance(within)
				// 维护的擦除进行中（已读取登记、尚未注销）时另一条遗忘提交。
				// Forget 自己的擦除会再次进入 hook，sync.Once 会自锁，所以用标志。
				var fired bool
				f.rec.set(0, func(ctx context.Context) {
					if !fired {
						fired = true
						require.NoError(t, f.m.Forget(f.ctx, "b"))
					}
				})
				require.NoError(t, f.m.Maintain(f.ctx))
				got := f.eraseEntries(t)
				require.Len(t, got, 1, "新提交的登记保留")
				require.Equal(t, f.now.Add(within), got[0].Until)
			},
		},
		{
			name:      "作用域已变时注销登记而不按登记擦除",
			withErase: true,
			run: func(t *testing.T, f *eraseFixture) {
				f.remember(t, "a")
				require.NoError(t, f.m.Forget(f.ctx, "a"))
				require.NoError(t, f.m.Purge(f.ctx))
				f.rec.take()
				require.NoError(t, f.m.Maintain(f.ctx))
				require.Equal(t, []eraseCall{cur}, f.rec.take(), "只有 retire 擦除旧作用域一次")
				require.Empty(t, f.eraseEntries(t))
			},
		},
		{
			name:      "OnErase 为 nil 时不登记",
			withErase: false,
			run: func(t *testing.T, f *eraseFixture) {
				require.False(t, f.m.ErasesOnDelete())
				f.remember(t, "a")
				require.NoError(t, f.m.Forget(f.ctx, "a"))
				require.NoError(t, f.m.Rewound(f.ctx, "s", 4))
				require.Empty(t, f.eraseEntries(t))
				require.NoError(t, f.m.Purge(f.ctx))
				require.NoError(t, f.m.Maintain(f.ctx))
			},
		},
		{
			name:      "配置 OnErase 时报告擦除与生效时限",
			withErase: true,
			run: func(t *testing.T, f *eraseFixture) {
				require.True(t, f.m.ErasesOnDelete())
				require.Equal(t, within, f.m.EraseWithin())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, newEraseFixture(t, tt.withErase))
		})
	}
}

// 擦除按命名空间与作用域进行：两个会话各自完成一轮后的一次 Forget 擦除整个命名空间恰好一次。
func TestForgetErasesAcrossSessions(t *testing.T) {
	f := newEraseFixture(t, true)
	for _, s := range []string{"s1", "s2"} {
		_, err := f.m.Remember(f.ctx, Record{ID: s, Content: "会话 " + s + " 的事实", Importance: 0.5,
			Sources: []Source{{Session: s, From: 1, To: 2}}})
		require.NoError(t, err)
	}
	require.NoError(t, f.m.Forget(f.ctx, "s1"))
	require.Equal(t, []eraseCall{{ns: testNS, scope: initialScope}}, f.rec.take())
}
