package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

func TestCacheVersion(t *testing.T) {
	ns := []string{"t1", "alice"}
	ctx := WithNamespace(context.Background(), ns...)
	version := func(t *testing.T, m *Memory) NamespaceVersion {
		t.Helper()
		got, err := m.CacheVersion(ctx, [][]string{ns})
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, ns, got[0].Namespace)
		return got[0]
	}
	remember := func(t *testing.T, m *Memory) {
		t.Helper()
		_, err := m.Remember(ctx, Record{ID: "a", Content: "用户喜欢咖啡", Importance: 0.5})
		require.NoError(t, err)
	}

	tests := []struct {
		name  string
		act   func(t *testing.T, m *Memory)
		check func(t *testing.T, m *Memory, before, after NamespaceVersion)
	}{
		{
			name: "新命名空间版本为零",
			act:  func(t *testing.T, m *Memory) {},
			check: func(t *testing.T, _ *Memory, _, after NamespaceVersion) {
				require.Equal(t, NamespaceVersion{Namespace: ns, Scope: initialScope}, after)
			},
		},
		{
			name: "Forget 抬高 LastForgetSeq 而不动 LastRewindSeq",
			act: func(t *testing.T, m *Memory) {
				remember(t, m)
				require.NoError(t, m.Forget(ctx, "a"))
			},
			check: func(t *testing.T, _ *Memory, before, after NamespaceVersion) {
				require.Greater(t, after.LastForgetSeq, before.LastForgetSeq)
				require.Equal(t, before.LastRewindSeq, after.LastRewindSeq)
				require.Equal(t, before.Scope, after.Scope)
			},
		},
		{
			name: "Rewound 抬高 LastRewindSeq 而不动 LastForgetSeq",
			act: func(t *testing.T, m *Memory) {
				remember(t, m)
				require.NoError(t, m.Rewound(ctx, "s", 4))
			},
			check: func(t *testing.T, m *Memory, before, after NamespaceVersion) {
				require.Greater(t, after.LastRewindSeq, before.LastRewindSeq)
				_, s, err := m.read(ctx, ns)
				require.NoError(t, err)
				require.Len(t, s.Markers, 1)
				for _, mk := range s.Markers {
					require.Equal(t, mk.Seq, after.LastRewindSeq, "LastRewindSeq 等于回退标记的 Seq")
				}
				require.Equal(t, before.LastForgetSeq, after.LastForgetSeq)
				require.Equal(t, before.Scope, after.Scope)
			},
		},
		{
			name: "Purge 改变 Scope",
			act: func(t *testing.T, m *Memory) {
				remember(t, m)
				require.NoError(t, m.Purge(ctx))
			},
			check: func(t *testing.T, _ *Memory, before, after NamespaceVersion) {
				require.NotEqual(t, before.Scope, after.Scope)
				require.Zero(t, after.LastForgetSeq)
				require.Zero(t, after.LastRewindSeq)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMemory(t, Config{})
			before := version(t, m)
			tt.act(t, m)
			tt.check(t, m, before, version(t, m))
		})
	}

	t.Run("重新打开同一目录后版本不变", func(t *testing.T) {
		dir := t.TempDir()
		open := func() *Memory {
			fs, err := durable.NewFileStore(dir)
			require.NoError(t, err)
			return newMemory(t, Config{Durable: fs})
		}
		m := open()
		remember(t, m)
		require.NoError(t, m.Forget(ctx, "a"))
		require.NoError(t, m.Rewound(ctx, "s", 4))
		want := version(t, m)
		require.NotZero(t, want.LastForgetSeq)
		require.NotZero(t, want.LastRewindSeq)
		require.Equal(t, want, version(t, open()))
	})

	t.Run("多个命名空间按命名空间排序", func(t *testing.T) {
		m := newMemory(t, Config{})
		got, err := m.CacheVersion(ctx, [][]string{{"t1", "bob"}, ns, {"t0", "z"}})
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.Equal(t, [][]string{{"t0", "z"}, {"t1", "alice"}, {"t1", "bob"}},
			[][]string{got[0].Namespace, got[1].Namespace, got[2].Namespace})
	})
}

// 升级前写入的 head 没有 last_rewind_seq 字段：解码为 0 且不报错。
func TestLastRewindSeqLegacyHead(t *testing.T) {
	st, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	legacy := `{"seq":3,"commit_id":"c","next_incarnation":2,"last_forget_seq":2,"base":{},"logical_bytes":0,"overlay_bytes":0,"overlay_items":0}`
	require.NoError(t, st.Save(context.Background(), headID, &durable.Snapshot{Version: headVersion, Plan: statePlan, RunID: headID, Data: []byte(legacy)}))
	h, err := loadHead(context.Background(), st)
	require.NoError(t, err)
	require.EqualValues(t, 2, h.LastForgetSeq)
	require.Zero(t, h.LastRewindSeq)
}

// 基础快照重写后 LastRewindSeq 仍在：head 与新基础快照的标量一致。
func TestLastRewindSeqSurvivesCompaction(t *testing.T) {
	ctx := context.Background()
	_, m := seedAndCompact(t, 3)
	_, err := m.mutate(ctx, testNS, "", func(t *tx) error { t.SetLastRewindSeq(t.nextSeq()); return nil })
	require.NoError(t, err)
	want := headOf(t, m).LastRewindSeq
	require.NotZero(t, want)

	require.NoError(t, m.compact(ctx, testNS, true, true))
	require.Equal(t, want, headOf(t, m).LastRewindSeq)
	reader := durableMemory(t, m.cfg.Durable, Config{StateCacheBytes: -1})
	_, s, err := reader.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, want, s.LastRewindSeq)
}

// 读取 head 期间发生 Purge：两次读到的 meta 不同时重读；每次都变则超过 readAttempts 后返回冲突。
func TestCacheVersionRetriesAcrossPurge(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		// purges 是在读取旧作用域 head 之前切换作用域的次数上限；-1 表示每次都切换。
		purges    int
		wantScope string
		wantErr   bool
	}{
		{name: "切换一次后重读并返回新作用域", purges: 1, wantScope: "new-1"},
		{name: "每次读取都切换时返回冲突", purges: -1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newHookStore(t)
			m := durableMemory(t, st, Config{})
			_, err := m.mutate(ctx, testNS, "", func(t *tx) error { t.SetLastRewindSeq(t.nextSeq()); return nil })
			require.NoError(t, err)
			cur, n := initialScope, 0
			st.h.before = func(op, scope, id string) error {
				if op == "load" && id == headID && scope == scopeName(testNS, cur) && (tt.purges < 0 || n < tt.purges) {
					n++
					cur = fmt.Sprintf("new-%d", n)
					setMeta(t, st, testNS, metaState{Current: cur, Purges: int64(n)})
				}
				return nil
			}
			got, err := m.CacheVersion(ctx, [][]string{testNS})
			if tt.wantErr {
				require.True(t, agentErrors.IsCode(err, agentErrors.CodeConflict), "%v", err)
				require.Equal(t, readAttempts, n, "每次尝试都读到了切换")
				return
			}
			require.NoError(t, err)
			require.Equal(t, []NamespaceVersion{{Namespace: testNS, Scope: tt.wantScope}}, got, "不返回旧作用域的版本")
			require.Equal(t, tt.purges, n)
		})
	}
}
