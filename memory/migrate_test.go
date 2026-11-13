package memory

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/store"
	storemem "github.com/kart-io/goagent/store/memory"
)

var legacyDay = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// legacyFixture 按 G0 的布局写入旧数据：命名空间 u 有一条有效记录、一条被取代的、一条没有取代者的失效记录，
// 以及实体、关系、社区；子命名空间 {u,x} 有一条记录；任务分属 u 与 other；旧索引在 u 与 {u,x} 下各有一个向量。
func legacyFixture(t *testing.T) (*storemem.Store, *MemoryIndex) {
	t.Helper()
	ctx := context.Background()
	st := storemem.New()
	put := func(ns []string, key string, v map[string]any) {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		require.NoError(t, st.Put(ctx, ns, key, string(b)))
	}
	u := []string{"u"}
	record := func(id, content string, extra map[string]any) map[string]any {
		r := map[string]any{"id": id, "namespace": u, "kind": "fact", "content": content, "keywords": []string{"咖啡"},
			"entities": []string{"coffee"}, "importance": 0.6, "valid_from": legacyDay, "created_at": legacyDay,
			"last_access": legacyDay.Add(time.Hour), "access_count": 3, "embed_model": "old-model",
			"sources": []map[string]any{{"session": "s", "from": 1, "to": 2, "job": "s/turn/1"}}}
		for k, v := range extra {
			r[k] = v
		}
		return r
	}
	put(legacyNS("records", u), "a", record("a", "用户喜欢拿铁", map[string]any{"pinned": true, "reflected": true}))
	put(legacyNS("records", u), "b", record("b", "用户喜欢美式", map[string]any{"invalid_at": legacyDay.Add(2 * time.Hour), "superseded_by": "a"}))
	put(legacyNS("records", u), "c", record("c", "用户住在深圳", map[string]any{"invalid_at": legacyDay.Add(3 * time.Hour)}))
	put(legacyNS("entities", u), "coffee", map[string]any{"id": "coffee", "namespace": u, "name": "咖啡", "type": "x"})
	put(legacyNS("edges", u), "e1", map[string]any{"id": "e1", "namespace": u, "subject": "user", "predicate": "喜欢", "object": "coffee"})
	put(legacyNS("communities", u), "c1", map[string]any{"id": "c1", "namespace": u, "members": []string{"a", "b", "c"}})
	put(legacyNS("records", []string{"u", "x"}), "z", record("z", "子命名空间的记录", map[string]any{"namespace": []string{"u", "x"}}))
	put(legacyJobsNS, "s/turn/1", map[string]any{"key": "s/turn/1", "namespace": u, "session": "s", "from": 1, "to": 2, "status": "done", "created_at": legacyDay})
	put(legacyJobsNS, "t/turn/1", map[string]any{"key": "t/turn/1", "namespace": []string{"other"}, "session": "t", "from": 1, "to": 2, "status": "pending", "created_at": legacyDay})
	idx := NewMemoryIndex()
	require.NoError(t, idx.Upsert(ctx, u, "a", []float32{1, 0}))
	require.NoError(t, idx.Upsert(ctx, []string{"u", "x"}, "z", []float32{0, 1}))
	return st, idx
}

func migrationMemory(t *testing.T, cfg Config) *Memory {
	t.Helper()
	cfg.Index, cfg.EmbedModel, cfg.Graph = NewMemoryIndex(), "bigram", true
	if cfg.Embedder == nil {
		cfg.Embedder = bigramEmbedder{}
	}
	return newMemory(t, cfg)
}

// 设计 7.4：记录逐字段迁入；没有取代者的失效记录记为已遗忘并清空正文；关系、实体、社区与任务不迁入，任务列在报告中；
// ClearLegacy 只清这个命名空间的四类数据、它的任务键与旧向量。
func TestMigrateFromStore(t *testing.T) {
	legacy, oldIdx := legacyFixture(t)
	m := workerMemory(t, Config{Index: NewMemoryIndex(), Embedder: bigramEmbedder{}, EmbedModel: "bigram", Graph: true})
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)

	report, err := m.MigrateFromStore(context.Background(), MigrationSource{Store: legacy, Index: oldIdx}, [][]string{ns}, MigrateOptions{ClearLegacy: true})
	require.NoError(t, err)
	require.Equal(t, []NamespaceMigration{{Namespace: ns, Status: MigrationImported, Records: 3, Cleared: true}}, report.Namespaces)
	require.Equal(t, []string{"s/turn/1", "t/turn/1"}, []string{report.Jobs[0].Key, report.Jobs[1].Key}, "报告列出全部旧任务")
	require.Equal(t, [][]string{{"other"}}, report.Unlisted)

	_, s, err := m.read(ctx, ns)
	require.NoError(t, err)
	checkInvariants(t, ns, s)
	checkIndex(t, m, m.cfg.Index.(*MemoryIndex), ns)
	require.Empty(t, s.Entities)
	require.Empty(t, s.Edges)
	require.Empty(t, s.Communities)
	require.Empty(t, s.Jobs)
	require.Equal(t, s.Seq, s.LastForgetSeq, "迁移之前登记的任务一律视为过期")
	require.NotEmpty(t, s.MigrationID)
	sources := []Source{{Session: "s", From: 1, To: 2, Job: "s/turn/1"}}
	want := Record{ID: "a", Namespace: ns, Kind: KindFact, Content: "用户喜欢拿铁", Keywords: []string{"咖啡"}, Entities: []string{"coffee"},
		Importance: 0.6, Pinned: true, ValidFrom: legacyDay, Sources: sources, CreatedAt: legacyDay, Revision: 1, ContentRev: 1, Reflected: true}
	got := s.Records["a"]
	want.Incarnation = got.Incarnation
	require.Equal(t, want, got)
	b := s.Records["b"]
	require.Equal(t, ReasonSuperseded, b.InvalidReason)
	require.Equal(t, "用户喜欢美式", b.Content)
	require.Equal(t, legacyDay.Add(2*time.Hour), b.InvalidAt)
	c := s.Records["c"]
	require.Equal(t, ReasonForgotten, c.InvalidReason)
	require.Empty(t, c.Content)
	require.Nil(t, c.Keywords)
	require.Nil(t, c.Entities)
	meta, err := m.loadMeta(ctx, ns)
	require.NoError(t, err)
	require.Empty(t, meta.Migrating)
	require.False(t, meta.Migrated.IsZero())

	got2, err := m.Recall(ctx, Query{Text: "用户喜欢拿铁", K: 3})
	require.NoError(t, err)
	require.Len(t, got2, 1)
	require.Equal(t, "a", got2[0].Record.ID)

	bg := context.Background()
	for _, kind := range legacyKinds {
		values, err := legacy.Search(bg, legacyNS(kind, ns), nil)
		require.NoError(t, err)
		require.Empty(t, values, "旧 %s 已清理", kind)
	}
	keys, err := legacy.List(bg, legacyJobsNS)
	require.NoError(t, err)
	require.Equal(t, []string{"t/turn/1"}, keys, "只删这个命名空间的任务键")
	values, err := legacy.Search(bg, legacyNS("records", []string{"u", "x"}), nil)
	require.NoError(t, err)
	require.Len(t, values, 1, "子命名空间不受影响")
	ids, err := oldIdx.IDs(bg, ns)
	require.NoError(t, err)
	require.Empty(t, ids)
	ids, err = oldIdx.IDs(bg, []string{"u", "x"})
	require.NoError(t, err)
	require.Equal(t, []string{"z"}, ids)

	// G0 时期的任务没有作用域，原样重新登记一律丢弃；手动设置了作用域、但 TurnSeq 小于迁移时 LastForgetSeq 的也被丢弃。
	require.NoError(t, m.Enqueue(ctx, report.Jobs[0]))
	require.EqualValues(t, 1, m.DroppedJobs())
	old := emptyJob("old", ns, initialScope)
	old.TurnSeq = s.LastForgetSeq - 1
	require.NoError(t, m.Enqueue(ctx, old))
	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	j, _, err := m.Job(ctx, "old")
	require.NoError(t, err)
	require.Equal(t, JobDropped, j.Status)
}

// 设计 7.4：已迁移的命名空间不再导入，包括迁移后 Purge、或遗忘全部记录并物理删除之后。
func TestMigrateRerunDoesNotImport(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after func(t *testing.T, m *Memory, ctx context.Context)
		want  int // 重跑后 state 中的记录数
	}{
		{name: "直接重跑", after: func(*testing.T, *Memory, context.Context) {}, want: 3},
		{name: "Purge 之后", after: func(t *testing.T, m *Memory, ctx context.Context) { require.NoError(t, m.Purge(ctx)) }},
		{name: "遗忘全部并 Prune 之后", after: func(t *testing.T, m *Memory, ctx context.Context) {
			require.NoError(t, m.Forget(ctx, "a"))
			now := time.Now().Add(2 * time.Hour)
			m.now = func() time.Time { return now }
			require.NoError(t, m.Prune(ctx))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			legacy, _ := legacyFixture(t)
			m := migrationMemory(t, Config{Retention: Retention{InvalidRetention: time.Hour}})
			ns := []string{"u"}
			ctx := WithNamespace(context.Background(), ns...)
			src := MigrationSource{Store: legacy}
			_, err := m.MigrateFromStore(ctx, src, [][]string{ns}, MigrateOptions{})
			require.NoError(t, err)
			tc.after(t, m, ctx)
			_, before, err := m.read(ctx, ns)
			require.NoError(t, err)

			report, err := m.MigrateFromStore(ctx, src, [][]string{ns}, MigrateOptions{})
			require.NoError(t, err)
			require.Equal(t, MigrationAlreadyDone, report.Namespaces[0].Status)
			_, s, err := m.read(ctx, ns)
			require.NoError(t, err)
			require.Len(t, s.Records, tc.want)
			require.Equal(t, before.Seq, s.Seq, "重跑不写 state")
		})
	}
}

// 设计 7.4：迁移之前新命名空间已被写入时报告冲突，不导入也不清理，meta 不留下迁移标记。
func TestMigrateConflictWhenDataExists(t *testing.T) {
	legacy, oldIdx := legacyFixture(t)
	m := migrationMemory(t, Config{})
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)
	_, err := m.Remember(ctx, Record{ID: "new", Content: "迁移前写入", Importance: 0.5})
	require.NoError(t, err)

	report, err := m.MigrateFromStore(ctx, MigrationSource{Store: legacy, Index: oldIdx}, [][]string{ns}, MigrateOptions{ClearLegacy: true})
	require.Error(t, err)
	require.Equal(t, MigrationConflict, report.Namespaces[0].Status)
	require.False(t, report.Namespaces[0].Cleared)
	_, s, err := m.read(ctx, ns)
	require.NoError(t, err)
	require.Len(t, s.Records, 1)
	values, err := legacy.Search(ctx, legacyNS("records", ns), nil)
	require.NoError(t, err)
	require.Len(t, values, 3, "冲突时不清理旧数据")
	meta, err := m.loadMeta(ctx, ns)
	require.NoError(t, err)
	require.Empty(t, meta.Migrating)
	require.True(t, meta.Migrated.IsZero())
}

// failingStore 让旧存储的一次 Clear 失败，模拟 ClearLegacy 中途崩溃。
type failingStore struct {
	store.Store
	failClear atomic.Bool
}

func (s *failingStore) Clear(ctx context.Context, ns []string) error {
	if slices.Contains(ns, "edges") && s.failClear.CompareAndSwap(true, false) {
		return errors.New("注入：清理中途崩溃")
	}
	return s.Store.Clear(ctx, ns)
}

type flakyEmbedder struct {
	bigramEmbedder
	fail atomic.Bool
}

func (e *flakyEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if e.fail.CompareAndSwap(true, false) {
		return nil, errors.New("注入：嵌入失败")
	}
	return e.bigramEmbedder.Embed(ctx, text)
}

// 设计 7.2：迁移在阶段一之后、阶段二之后、阶段三之后（补写向量失败）与 ClearLegacy 中途失败，重跑后与一次成功的迁移相同：
// 不重复导入，空 state 不会被标记为已迁移，迁移标记未清除期间 Purge 被拒绝。
func TestMigrateCrashThenRerun(t *testing.T) {
	ns := []string{"u"}
	clean := func(t *testing.T) state {
		legacy, idx := legacyFixture(t)
		m := migrationMemory(t, Config{})
		_, err := m.MigrateFromStore(context.Background(), MigrationSource{Store: legacy, Index: idx}, [][]string{ns}, MigrateOptions{ClearLegacy: true})
		require.NoError(t, err)
		_, s, err := m.read(context.Background(), ns)
		require.NoError(t, err)
		return s
	}(t)
	for _, tc := range []struct {
		name      string
		inject    func(h *storeHooks, src *failingStore, emb *flakyEmbedder)
		migrating bool // 失败之后 meta 仍有迁移标记
	}{
		{name: "阶段一之后", migrating: true, inject: func(h *storeHooks, _ *failingStore, _ *flakyEmbedder) {
			var once atomic.Bool
			h.before = func(op, scope, id string) error {
				if op == "save" && scope != "" && once.CompareAndSwap(false, true) {
					return errors.New("注入：阶段二提交前崩溃")
				}
				return nil
			}
		}},
		{name: "阶段二之后", migrating: true, inject: func(h *storeHooks, _ *failingStore, _ *flakyEmbedder) {
			var metaSaves atomic.Int32
			h.before = func(op, scope, id string) error {
				if op == "save" && id == metaID(ns) && metaSaves.Add(1) == 2 {
					return errors.New("注入：阶段三提交前崩溃")
				}
				return nil
			}
		}},
		{name: "阶段三之后", inject: func(_ *storeHooks, _ *failingStore, emb *flakyEmbedder) { emb.fail.Store(true) }},
		{name: "ClearLegacy 中途", inject: func(_ *storeHooks, src *failingStore, _ *flakyEmbedder) { src.failClear.Store(true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			legacy, idx := legacyFixture(t)
			src := &failingStore{Store: legacy}
			hs := newHookStore(t)
			emb := &flakyEmbedder{}
			m := migrationMemory(t, Config{Durable: hs, Embedder: emb})
			ctx := WithNamespace(context.Background(), ns...)
			legacySrc := MigrationSource{Store: src, Index: idx}
			tc.inject(hs.h, src, emb)

			report, err := m.MigrateFromStore(context.Background(), legacySrc, [][]string{ns}, MigrateOptions{ClearLegacy: true})
			require.Error(t, err)
			require.Equal(t, MigrationFailed, report.Namespaces[0].Status)
			meta, err := m.loadMeta(ctx, ns)
			require.NoError(t, err)
			require.Equal(t, tc.migrating, meta.Migrating != "")
			if tc.migrating {
				require.True(t, meta.Migrated.IsZero(), "没有完成阶段三不标记为已迁移")
				require.Error(t, m.Purge(ctx), "迁移进行中拒绝 Purge")
			}

			hs.h.before = nil
			report, err = m.MigrateFromStore(context.Background(), legacySrc, [][]string{ns}, MigrateOptions{ClearLegacy: true})
			require.NoError(t, err)
			require.True(t, report.Namespaces[0].Cleared)
			_, s, err := m.read(ctx, ns)
			require.NoError(t, err)
			checkInvariants(t, ns, s)
			checkIndex(t, m, m.cfg.Index.(*MemoryIndex), ns)
			require.Equal(t, clean.Records, s.Records, "与一次成功的迁移相同，不重复导入")
			require.Equal(t, clean.NextIncarnation, s.NextIncarnation)
			require.Equal(t, clean.Seq, s.Seq)
			meta, err = m.loadMeta(ctx, ns)
			require.NoError(t, err)
			require.Empty(t, meta.Migrating)
			require.False(t, meta.Migrated.IsZero())
			values, err := legacy.Search(ctx, legacyNS("edges", ns), nil)
			require.NoError(t, err)
			require.Empty(t, values)
		})
	}
}

// 阶段一之后、阶段二之前有写入（迁移期间没有停止流量）：阶段二在锁内发现数据，报告冲突并撤销迁移标记，Purge 不会一直被拒绝。
func TestMigrateConflictAfterPhaseOne(t *testing.T) {
	legacy, _ := legacyFixture(t)
	hs := newHookStore(t)
	m := migrationMemory(t, Config{Durable: hs})
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)
	var once atomic.Bool
	hs.h.afterSave = func(scope, id string) error {
		if scope == "" && id == metaID(ns) && once.CompareAndSwap(false, true) {
			_, err := m.Remember(ctx, Record{ID: "new", Content: "迁移期间写入", Importance: 0.5})
			require.NoError(t, err)
		}
		return nil
	}

	report, err := m.MigrateFromStore(ctx, MigrationSource{Store: legacy}, [][]string{ns}, MigrateOptions{ClearLegacy: true})
	require.Error(t, err)
	require.Equal(t, MigrationConflict, report.Namespaces[0].Status)
	_, s, err := m.read(ctx, ns)
	require.NoError(t, err)
	require.Equal(t, []string{"new"}, slices.Sorted(maps.Keys(s.Records)))
	require.Empty(t, s.MigrationID)
	meta, err := m.loadMeta(ctx, ns)
	require.NoError(t, err)
	require.Empty(t, meta.Migrating)
	require.NoError(t, m.Purge(ctx))
}
