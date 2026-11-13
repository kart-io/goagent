package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

// stateOf 读取命名空间 u 的 state 并校验不变量。
func stateOf(t *testing.T, m *Memory) state {
	t.Helper()
	_, s, err := m.read(context.Background(), []string{"u"})
	require.NoError(t, err)
	checkInvariants(t, []string{"u"}, s)
	return s
}

func TestRememberCreatesOnce(t *testing.T) {
	m := vectorMemory(t)
	ctx := WithNamespace(context.Background(), "u")
	a, err := m.Remember(ctx, Record{ID: "a", Content: "用户喜欢咖啡", Importance: 0.5})
	require.NoError(t, err)
	b, err := m.Remember(ctx, Record{Content: "用户住在深圳", Importance: 0.5})
	require.NoError(t, err)
	require.Equal(t, [2]int64{1, 1}, [2]int64{a.Revision, a.ContentRev})
	require.Positive(t, a.Incarnation)
	require.NotEqual(t, a.Incarnation, b.Incarnation)

	_, err = m.Remember(ctx, Record{ID: "a", Content: "用户喜欢茶", Importance: 0.5})
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeConflict), "ID 已存在：%v", err)
	got, err := m.Get(ctx, "a")
	require.NoError(t, err)
	require.Equal(t, "用户喜欢咖啡", got.Content, "冲突时不覆盖")

	_, err = m.Remember(ctx, Record{ID: "x", Content: "失效", Importance: 0.5, InvalidAt: time.Now()})
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidInput), "失效时间与原因须同时设置")
	stateOf(t, m)
}

func TestUpdate(t *testing.T) {
	str := func(s string) *string { return &s }
	f := func(v float64) *float64 { return &v }
	tests := []struct {
		name               string
		expect             int64
		id                 string
		patch              Patch
		forget             bool
		code               agentErrors.ErrorCode
		revision, content  int64
		wantText, wantHits string
	}{
		{name: "只改重要度", expect: 1, id: "a", patch: Patch{Importance: f(0.9)}, revision: 2, content: 1, wantText: "用户喜欢咖啡"},
		{name: "正文不变不算内容修改", expect: 1, id: "a", patch: Patch{Content: str("用户喜欢咖啡")}, revision: 2, content: 1, wantText: "用户喜欢咖啡"},
		{name: "修改正文并重写向量", expect: 1, id: "a", patch: Patch{Content: str("用户喜欢乌龙茶")}, revision: 2, content: 2, wantText: "用户喜欢乌龙茶", wantHits: "乌龙茶"},
		{name: "修订号过期", expect: 2, id: "a", patch: Patch{Importance: f(0.9)}, code: agentErrors.CodeConflict},
		{name: "记录已失效", expect: 2, id: "a", forget: true, patch: Patch{Importance: f(0.9)}, code: agentErrors.CodeConflict},
		{name: "记录不存在", expect: 1, id: "nope", patch: Patch{Importance: f(0.9)}, code: agentErrors.CodeStoreNotFound},
		{name: "修改后不合法", expect: 1, id: "a", patch: Patch{Importance: f(2)}, code: agentErrors.CodeInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := vectorMemory(t)
			ctx := WithNamespace(context.Background(), "u")
			_, err := m.Remember(ctx, Record{ID: "a", Content: "用户喜欢咖啡", Importance: 0.5})
			require.NoError(t, err)
			if tt.forget {
				require.NoError(t, m.Forget(ctx, "a"))
			}
			got, err := m.Update(ctx, tt.id, tt.expect, tt.patch)
			if tt.code != "" {
				require.True(t, agentErrors.IsCode(err, tt.code), "%v", err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, [2]int64{tt.revision, tt.content}, [2]int64{got.Revision, got.ContentRev})
			require.Equal(t, tt.wantText, got.Content)
			if tt.wantHits != "" {
				hits, err := m.Recall(ctx, Query{Text: tt.wantHits})
				require.NoError(t, err)
				require.Equal(t, []string{tt.wantText}, contents(hits))
			}
			stateOf(t, m)
		})
	}
}

func TestForget(t *testing.T) {
	x := NewMemoryIndex()
	m := newMemory(t, Config{Index: x, Embedder: bigramEmbedder{}, EmbedModel: "v1", RollbackWindow: time.Hour})
	ctx := WithNamespace(context.Background(), "u")
	_, err := m.Remember(ctx, Record{ID: "a", Content: "用户的银行卡号是 1234", Keywords: []string{"银行卡"}, Entities: []string{"用户"}, Importance: 0.5})
	require.NoError(t, err)

	require.NoError(t, m.Forget(ctx, "a"))
	s := stateOf(t, m)
	r := s.Records["a"]
	require.Equal(t, ReasonForgotten, r.InvalidReason)
	require.Empty(t, r.Content)
	require.Equal(t, s.Seq, s.LastForgetSeq, "LastForgetSeq 是遗忘提交的 Seq")
	require.Empty(t, indexKeys(x, []string{"u"}), "移出索引")
	checkIndex(t, m, x, []string{"u"})
	got, err := m.Recall(ctx, Query{Text: "银行卡"})
	require.NoError(t, err)
	require.Empty(t, got)

	require.NoError(t, m.Forget(ctx, "a"), "已遗忘时直接返回")
	require.Equal(t, s.Seq, stateOf(t, m).Seq, "重复遗忘不提交")
	_, err = m.Update(ctx, "a", r.Revision, Patch{Content: new(string)})
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeConflict), "遗忘是终态")
	require.True(t, agentErrors.IsCode(m.Forget(ctx, "nope"), agentErrors.CodeStoreNotFound))

	log, err := m.Deletions(ctx)
	require.NoError(t, err)
	require.Len(t, log.Forgotten, 1, "重复遗忘不重复记日志")
	require.Equal(t, "a", log.Forgotten[0].ID)
}

// 删除日志先于遗忘：遗忘的提交失败时，日志中已有这一条，记录仍然有效。
func TestForgetLogsBeforeCommit(t *testing.T) {
	st := newHookStore(t)
	m := durableMemory(t, st, Config{RollbackWindow: time.Hour})
	ctx := WithNamespace(context.Background(), "u")
	_, err := m.Remember(ctx, Record{ID: "a", Content: "内容", Importance: 0.5})
	require.NoError(t, err)
	crash := errors.New("提交前崩溃")
	st.h.before = func(op, scope, id string) error {
		if op == "save" && id == headID {
			return crash
		}
		return nil
	}
	require.ErrorIs(t, m.Forget(ctx, "a"), crash)
	st.h.before = nil
	log, err := m.Deletions(ctx)
	require.NoError(t, err)
	require.Len(t, log.Forgotten, 1)
	require.True(t, stateOf(t, m).Records["a"].Valid())

	// 日志写入失败时不遗忘。
	st.h.before = func(op, scope, id string) error {
		if op == "save" && id == deletionsID([]string{"u"}) {
			return crash
		}
		return nil
	}
	require.ErrorIs(t, m.Forget(ctx, "a"), crash)
	st.h.before = nil
	require.True(t, stateOf(t, m).Records["a"].Valid())
}

// 退回：来自退回轮次的记录与关系失效（rewound）；只被它们取代的旧记录恢复；被遗忘的记录与被遗忘者取代的记录不动。一次提交，没有其他变化时也提交（写回退标记）。
func TestRewoundRestoresOnlyRewoundSuperseders(t *testing.T) {
	m := newMemory(t, Config{Graph: true})
	ctx := WithNamespace(context.Background(), "u")
	src := func(from int64) []Source { return []Source{{Session: "s", From: from}} }
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := m.mutate(ctx, []string{"u"}, "", func(t *tx) error {
		for _, r := range []Record{
			{ID: "old", Sources: src(1)},
			{ID: "new", Sources: src(5)},
			{ID: "old2", Sources: src(1)},
			{ID: "forgot", Sources: src(6)},
			{ID: "keep", Sources: src(2)},
		} {
			r.Kind, r.Content, r.Namespace, r.CreatedAt, r.ValidFrom = KindFact, r.ID, []string{"u"}, day, day
			createRecord(t, r)
		}
		old, _ := t.Records.Get("old")
		t.Records.Put("old", old.invalidated(day, ReasonSuperseded, "new"))
		old2, _ := t.Records.Get("old2")
		t.Records.Put("old2", old2.invalidated(day, ReasonSuperseded, "forgot"))
		f, _ := t.Records.Get("forgot")
		forget(t, f, day)
		return nil
	})
	require.NoError(t, err)
	before := stateOf(t, m)

	require.NoError(t, m.Rewound(ctx, "s", 3))
	s := stateOf(t, m)
	require.Equal(t, before.Seq+1, s.Seq, "一次提交")
	reasons := map[string]InvalidReason{}
	for id, r := range s.Records {
		reasons[id] = r.InvalidReason
	}
	require.Equal(t, map[string]InvalidReason{"old": "", "new": ReasonRewound, "old2": ReasonSuperseded, "forgot": ReasonForgotten, "keep": ""}, reasons)
	require.Equal(t, before.Records["old"].Revision+1, s.Records["old"].Revision)

	require.NoError(t, m.Rewound(ctx, "s", 3))
	again := stateOf(t, m)
	require.Equal(t, s.Seq+1, again.Seq, "没有其他变化时也提交，写入回退标记")
	require.Len(t, again.Markers, 2)
	require.Equal(t, s.Records, again.Records)
}

func TestPrune(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		retention Retention
		records   []Record
		want      map[string]InvalidReason // 缺席表示已删除
	}{
		{
			name:      "删除超过保留期的失效记录",
			retention: Retention{InvalidRetention: time.Hour},
			records: []Record{
				{ID: "stale", InvalidAt: now.Add(-2 * time.Hour), InvalidReason: ReasonSuperseded},
				{ID: "fresh", InvalidAt: now.Add(-time.Minute), InvalidReason: ReasonExpired},
				{ID: "valid"},
			},
			want: map[string]InvalidReason{"fresh": ReasonExpired, "valid": ""},
		},
		{
			name:      "TTL 到期改为 expired",
			retention: Retention{InvalidRetention: time.Hour, TTL: map[Kind]time.Duration{KindEpisode: 24 * time.Hour}},
			records: []Record{
				{ID: "ep", Kind: KindEpisode, CreatedAt: now.Add(-48 * time.Hour)},
				{ID: "fact", CreatedAt: now.Add(-48 * time.Hour)},
			},
			want: map[string]InvalidReason{"ep": ReasonExpired, "fact": ""},
		},
		{
			name:      "超出容量淘汰得分最低的未钉住记录（按创建时间计新近性）",
			retention: Retention{InvalidRetention: time.Hour, MaxRecords: 1},
			records: []Record{
				{ID: "older", CreatedAt: now.Add(-30 * 24 * time.Hour)},
				{ID: "newer", CreatedAt: now.Add(-time.Hour)},
				{ID: "pinned", Pinned: true, CreatedAt: now.Add(-60 * 24 * time.Hour)},
			},
			want: map[string]InvalidReason{"older": ReasonExpired, "newer": "", "pinned": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMemory(t, Config{Retention: tt.retention})
			m.now = func() time.Time { return now }
			ctx := WithNamespace(context.Background(), "u")
			_, err := m.mutate(ctx, []string{"u"}, "", func(t *tx) error {
				for _, r := range tt.records {
					r.Content, r.Importance, r.Namespace = r.ID, 0.5, []string{"u"}
					if r.Kind == "" {
						r.Kind = KindFact
					}
					if r.CreatedAt.IsZero() {
						r.CreatedAt = now
					}
					createRecord(t, r)
				}
				return nil
			})
			require.NoError(t, err)
			require.NoError(t, m.Prune(ctx))
			got := map[string]InvalidReason{}
			for id, r := range stateOf(t, m).Records {
				got[id] = r.InvalidReason
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDeletionLogPrune(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	m := newMemory(t, Config{RollbackWindow: time.Hour})
	ctx := WithNamespace(context.Background(), "u")
	for i, id := range []string{"a", "b"} {
		at := now.Add(time.Duration(i) * 2 * time.Hour)
		m.now = func() time.Time { return at }
		_, err := m.Remember(ctx, Record{ID: id, Content: id, Importance: 0.5})
		require.NoError(t, err)
		require.NoError(t, m.Forget(ctx, id))
	}
	require.NoError(t, m.Purge(ctx))
	log, err := m.Deletions(ctx)
	require.NoError(t, err)
	require.Len(t, log.Forgotten, 2)
	require.Len(t, log.Purges, 1)

	m.now = func() time.Time { return now.Add(150 * time.Minute) }
	require.NoError(t, m.Prune(ctx))
	log, err = m.Deletions(ctx)
	require.NoError(t, err)
	require.Equal(t, []Deletion{{ID: "b", At: now.Add(2 * time.Hour)}}, log.Forgotten, "只保留窗口内的条目")
	require.Len(t, log.Purges, 1)

	off := newMemory(t, Config{})
	_, err = off.Remember(ctx, Record{ID: "a", Content: "a", Importance: 0.5})
	require.NoError(t, err)
	require.NoError(t, off.Forget(ctx, "a"))
	log, err = off.Deletions(ctx)
	require.NoError(t, err)
	require.Empty(t, log.Forgotten, "RollbackWindow 为 0 时不写日志")
}

// Purge 切换到新的空作用域并删除旧作用域：返回时旧作用域已物理删除、retiring 为空（不变量 8），索引与召回为空，
// 删除日志与 meta 在同一次提交中记下这次清空，之后的写入落在新作用域、可以复用旧 ID。
func TestPurgeSwitchesScope(t *testing.T) {
	x := NewMemoryIndex()
	m := manualCompaction(newMemory(t, Config{Index: x, Embedder: bigramEmbedder{}, EmbedModel: "v1", Graph: true, TrackAccess: true, RollbackWindow: time.Hour}))
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	ctx := WithNamespace(context.Background(), testNS...)
	_, err := m.Remember(ctx, Record{ID: "a", Content: "机密：项目代号青鸟", Importance: 0.5})
	require.NoError(t, err)
	require.NoError(t, graphOf(m).PutEntity(ctx, Entity{ID: "qn", Namespace: testNS, Name: "青鸟", Type: "project"}))
	require.NoError(t, m.compact(ctx, testNS, true, true))
	_, err = m.Recall(ctx, Query{Text: "青鸟"})
	require.NoError(t, err)
	m.waitAccess()
	require.Contains(t, scopeText(t, m), "青鸟")

	require.NoError(t, m.Purge(ctx))
	_, err = loadHead(ctx, m.scoped(testNS, initialScope))
	require.ErrorIs(t, err, agentErrors.ErrScopeRemoved, "Purge 返回时旧作用域已物理删除")
	meta, err := m.loadMeta(ctx, testNS)
	require.NoError(t, err)
	require.EqualValues(t, 1, meta.Purges)
	require.NotEqual(t, initialScope, meta.Current)
	require.Contains(t, retiringScopes(t, m), scopeName(testNS, initialScope), "宽限期内旧作用域留在 retiring 中")
	log, err := m.Deletions(ctx)
	require.NoError(t, err)
	require.Equal(t, []Deletion{{ID: initialScope, At: now}}, log.Purges, "删除日志记下被清空的作用域")

	scope, s, err := m.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, meta.Current, scope)
	require.Empty(t, s.Records)
	require.Empty(t, s.Entities)
	got, err := m.Recall(ctx, Query{Text: "青鸟"})
	require.NoError(t, err)
	require.Empty(t, got)
	require.Empty(t, indexKeys(x, testNS), "Purge 返回时旧作用域的记录与实体索引已清空")
	checkIndex(t, m, x, testNS)
	now = now.Add(m.cfg.RetireGrace)
	require.NoError(t, m.cleanupRetiring(ctx))
	requireNoRetiring(t, m)
	a, err := m.loadAccess(ctx, testNS, meta.Current)
	require.NoError(t, err)
	require.Empty(t, a, "新作用域没有访问记录")

	_, err = m.Remember(ctx, Record{ID: "a", Content: "新内容", Importance: 0.5})
	require.NoError(t, err, "清空后可以复用旧 ID")
	_, s, err = m.read(ctx, testNS)
	require.NoError(t, err)
	require.Equal(t, "新内容", s.Records["a"].Content)
}

// TrackAccess：开启时召回在返回后异步记录访问，新近性与容量淘汰按最近访问计；关闭时召回不产生任何写入，新近性按创建时间计。
func TestTrackAccess(t *testing.T) {
	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, track := range []bool{false, true} {
		t.Run(map[bool]string{false: "关闭", true: "开启"}[track], func(t *testing.T) {
			m := newMemory(t, Config{TrackAccess: track, Retention: Retention{InvalidRetention: time.Hour, MaxRecords: 1}})
			now := day
			m.now = func() time.Time { return now }
			ctx := WithNamespace(context.Background(), "u")
			_, err := m.Remember(ctx, Record{ID: "old", Content: "用户喜欢咖啡", CreatedAt: day.Add(-30 * 24 * time.Hour), Importance: 0.5})
			require.NoError(t, err)
			_, err = m.Remember(ctx, Record{ID: "new", Content: "用户住在深圳", CreatedAt: day.Add(-24 * time.Hour), Importance: 0.5})
			require.NoError(t, err)
			seq := stateOf(t, m).Seq

			got, err := m.Recall(ctx, Query{Text: "咖啡"})
			require.NoError(t, err)
			require.Equal(t, []string{"old"}, []string{got[0].ID})
			m.waitAccess()
			require.Equal(t, seq, stateOf(t, m).Seq, "召回不写 state")
			a, err := m.loadAccess(ctx, []string{"u"}, initialScope)
			require.NoError(t, err)
			if track {
				require.Equal(t, map[string]accessEntry{"old": {At: day, N: 1}}, a)
			} else {
				require.Empty(t, a)
			}

			now = day.Add(time.Hour)
			require.NoError(t, m.Prune(ctx))
			s := stateOf(t, m)
			survivor := "new"
			if track {
				survivor = "old"
			}
			require.True(t, s.Records[survivor].Valid(), "保留 %s", survivor)
			require.Equal(t, 1, countValid(s))
		})
	}
}

func countValid(s state) int {
	n := 0
	for _, r := range s.Records {
		if r.Valid() {
			n++
		}
	}
	return n
}
