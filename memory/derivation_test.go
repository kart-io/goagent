package memory

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/stretchr/testify/require"
)

func TestStateV2AndV3MigrateToV4(t *testing.T) {
	for _, version := range []string{"2", "3"} {
		t.Run(version, func(t *testing.T) {
			st := newHookStore(t)
			m := manualCompaction(durableMemory(t, st, Config{StateCacheBytes: -1}))
			ctx := WithNamespace(context.Background(), testNS...)
			scope := m.scoped(testNS, initialScope)
			// 固定快照分别由 /2 写者 6cf097ee 和 /3 写者 1c36b871 生成。
			for _, f := range []struct{ name, id, version string }{{"head", headID, "memory-head/"}, {"base", baseID(0), "memory-state-blob/"}} {
				data, err := os.ReadFile("testdata/state-v" + version + "/" + f.name + ".json")
				require.NoError(t, err)
				unlock, err := scope.Lock(ctx, f.id)
				require.NoError(t, err)
				require.NoError(t, scope.Save(ctx, f.id, &durable.Snapshot{Version: f.version + version, Plan: statePlan, RunID: f.id, Data: data}))
				require.NoError(t, unlock())
			}
			_, s, err := m.read(ctx, testNS)
			require.NoError(t, err)
			require.Contains(t, s.Records, "r00")
			require.Empty(t, s.Derivations)
			require.Zero(t, s.RequestSeq)
			require.NoError(t, m.MarkDerivationsDirty(ctx))
			require.NoError(t, m.compact(ctx, testNS, true, true))
			h, err := scope.Load(ctx, headID)
			require.NoError(t, err)
			require.Equal(t, "memory-head/4", h.Version)
			b, err := scope.Load(ctx, baseID(headOf(t, m).Base.Slot))
			require.NoError(t, err)
			require.Equal(t, "memory-state-blob/4", b.Version)
			_, s, err = m.read(ctx, testNS)
			require.NoError(t, err)
			require.Contains(t, s.Records, "r00")
			require.Len(t, s.Derivations, 2)
		})
	}
}

func TestOldReaderRejectsV4(t *testing.T) {
	_, m := seedAndCompact(t, 1)
	st := m.scoped(testNS, initialScope)
	// 冻结自 1c36b871 的 loadHead 与 loadBase 版本判定。
	for _, tc := range []struct{ id, v2, v3 string }{{headID, "memory-head/2", "memory-head/3"}, {baseID(headOf(t, m).Base.Slot), "memory-state-blob/2", "memory-state-blob/3"}} {
		snap, err := st.Load(context.Background(), tc.id)
		require.NoError(t, err)
		oldReaderAccepts := (snap.Version == tc.v3 || snap.Version == tc.v2) && snap.Plan == statePlan
		require.False(t, oldReaderAccepts)
	}
}

func TestDirtyFieldsSurviveCompaction(t *testing.T) {
	ctx := context.Background()
	st, m := seedAndCompact(t, 2)
	r := durableMemory(t, st, Config{StateCacheBytes: -1})
	w := DerivationWork{DirtySeq: 7, DoneSeq: 2, GeneratorVersion: "v", Attempts: 5, RetryAt: time.Unix(200, 0).UTC(), LastErrorCode: "extract", Paused: true, BlockedBudgetRootKey: "root"}
	_, err := m.mutate(ctx, testNS, "", func(x *tx) error { x.Derivations.Put("community", w); x.scalars.RequestSeq = 29; return nil })
	require.NoError(t, err)
	check := func() {
		t.Helper()
		_, s, e := r.read(ctx, testNS)
		require.NoError(t, e)
		require.Equal(t, w, s.Derivations["community"])
		require.EqualValues(t, 29, s.RequestSeq)
		require.Equal(t, s.logicalBytes(), headOf(t, m).LogicalBytes)
	}
	check()
	require.NoError(t, m.compact(ctx, testNS, true, true))
	check()
	base, _, _, err := r.loadBase(ctx, r.scoped(testNS, initialScope), testNS, initialScope, headOf(t, m).Base)
	require.NoError(t, err)
	require.EqualValues(t, 29, base.RequestSeq)
	require.Equal(t, w, base.Derivations["community"])
	_, err = m.mutate(ctx, testNS, "", func(x *tx) error { x.Derivations.Delete("community"); return nil })
	require.NoError(t, err)
	require.NoError(t, m.compact(ctx, testNS, true, true))
	_, s, err := r.read(ctx, testNS)
	require.NoError(t, err)
	require.Empty(t, s.Derivations)
}

func TestCrashBetweenQueueAndDirtyCommitLeavesDiscoverableWork(t *testing.T) {
	st := newHookStore(t)
	m := workerMemory(t, Config{Durable: st})
	ctx := WithNamespace(context.Background(), testNS...)
	st.h.before = func(op, scope, id string) error {
		if op == "save" && scope != "" && id == headID {
			return errors.New("中断")
		}
		return nil
	}
	require.Error(t, m.MarkDerivationsDirty(ctx))
	st.h.before = nil
	entries, err := m.queued(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, queueDerivations, entries[0].Reason)
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Empty(t, s.Derivations)
	require.NoError(t, m.Maintain(ctx))
	requireQueueEmpty(t, m)
	require.NoError(t, m.MarkDerivationsDirty(ctx))
	require.NoError(t, m.Maintain(ctx))
	entries, err = m.queued(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestExplicitDerivationRetryPreservesBudgetAndRequestSeq(t *testing.T) {
	_, m := seedAndCompact(t, 2)
	ctx := WithNamespace(context.Background(), testNS...)
	w := DerivationWork{DirtySeq: 13, DoneSeq: 8, Attempts: 5, Paused: true, RetryAt: time.Unix(200, 0).UTC(), LastErrorCode: "failure", BlockedBudgetRootKey: "window", GeneratorVersion: "v1"}
	_, err := m.mutate(ctx, testNS, "", func(x *tx) error { x.scalars.RequestSeq = 31; x.Derivations.Put("community", w); return nil })
	require.NoError(t, err)
	require.NoError(t, m.MarkDerivationsDirty(ctx))
	_, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.EqualValues(t, 31, s.RequestSeq)
	require.Equal(t, DerivationWork{DirtySeq: 14, DoneSeq: 8, BlockedBudgetRootKey: "window", GeneratorVersion: "v1"}, s.Derivations["community"])
	require.Equal(t, DerivationWork{DirtySeq: 1}, s.Derivations["reflection"])
}

func TestDerivationScopePurgeAndRollback(t *testing.T) {
	st, m := seedAndCompact(t, 1)
	ctx := WithNamespace(context.Background(), testNS...)
	require.NoError(t, m.MarkDerivationsDirty(ctx))
	_, err := m.mutate(ctx, testNS, "", func(x *tx) error { x.scalars.RequestSeq = 9; return nil })
	require.NoError(t, err)
	require.NoError(t, m.compact(ctx, testNS, true, true))
	st.h.before = func(op, scope, id string) error {
		if op == "save" && scope != "" && id == headID {
			return errors.New("未提交")
		}
		return nil
	}
	require.Error(t, m.MarkDerivationsDirty(ctx))
	st.h.before = nil
	reader := durableMemory(t, st, Config{StateCacheBytes: -1})
	_, s, err := reader.read(ctx, testNS)
	require.NoError(t, err)
	require.EqualValues(t, 1, s.Derivations["community"].DirtySeq)
	require.EqualValues(t, 9, s.RequestSeq)
	require.NoError(t, m.Purge(ctx))
	require.NoError(t, m.Maintain(ctx))
	newScope, s, err := reader.read(ctx, testNS)
	require.NoError(t, err)
	require.NotEqual(t, initialScope, newScope)
	require.Empty(t, s.Derivations)
	require.Zero(t, s.RequestSeq)
	requireQueueEmpty(t, m)
	require.NoError(t, m.MarkDerivationsDirty(ctx))
	entries, err := m.queued(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, newScope, entries[0].Scope)
}
