package memory

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/llm"
)

// hookIndex 在 Upsert 与 Clear 之前调用 before，用于制造交错与崩溃。
type hookIndex struct {
	Index
	before func(op string, ns []string, id string) error
}

func (h hookIndex) Upsert(ctx context.Context, ns []string, id string, v []float32) error {
	if err := h.before("upsert", ns, id); err != nil {
		return err
	}
	return h.Index.Upsert(ctx, ns, id, v)
}

func (h hookIndex) Clear(ctx context.Context, ns []string) error {
	if err := h.before("clear", ns, ""); err != nil {
		return err
	}
	return h.Index.Clear(ctx, ns)
}

// hookEmbedder 在嵌入前调用 before。
type hookEmbedder struct {
	llm.EmbeddingClient
	before func(text string)
}

func (h hookEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	h.before(text)
	return h.EmbeddingClient.Embed(ctx, text)
}

// Clear 只清精确的命名空间：父命名空间与子命名空间（ns#scope）中的键都保留。
func TestMemoryIndexClearExact(t *testing.T) {
	ctx := context.Background()
	parent, ns, child := []string{"u"}, []string{"u", "a"}, []string{"u", "a", "#s"}
	tests := []struct {
		name  string
		clear []string
		want  map[string][]string
	}{
		{"清父命名空间", parent, map[string][]string{"u": nil, "u/a": {"k"}, "u/a/#s": {"k"}}},
		{"清中间命名空间", ns, map[string][]string{"u": {"k"}, "u/a": nil, "u/a/#s": {"k"}}},
		{"清子命名空间", child, map[string][]string{"u": {"k"}, "u/a": {"k"}, "u/a/#s": nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x := NewMemoryIndex()
			spaces := map[string][]string{"u": parent, "u/a": ns, "u/a/#s": child}
			for _, s := range spaces {
				require.NoError(t, x.Upsert(ctx, s, "k", []float32{1, 0}))
			}
			require.NoError(t, x.Clear(ctx, tt.clear))
			for name, s := range spaces {
				ids, err := x.IDs(ctx, s)
				require.NoError(t, err)
				require.Equal(t, tt.want[name], ids, name)
			}
		})
	}
}

func TestIndexKeyRoundTrip(t *testing.T) {
	for _, k := range []indexKey{{"a", 1, 1, "m"}, {"a#b#3.4#x", 12, 7, modelTag("v#1")}, {"中文", 1 << 40, 2, ""}} {
		got, ok := parseIndexKey(k.String())
		require.True(t, ok, k.String())
		require.Equal(t, k, got)
	}
	for _, bad := range []string{"", "a", "a#1.1", "#1.1#m", "a#1#m", "a#x.1#m", "a#1.y#m"} {
		_, ok := parseIndexKey(bad)
		require.False(t, ok, bad)
	}
}

// 记录的每个版本只有一个可采用的键：Update 写新键、删旧键；Forget、Rewound 删键；召回不采用旧版本的键。
func TestIndexFollowsRecordVersions(t *testing.T) {
	x := NewMemoryIndex()
	m := newMemory(t, Config{Index: x, Embedder: bigramEmbedder{}, EmbedModel: "v1"})
	ctx := WithNamespace(context.Background(), "u")
	key := func(r Record) string { k := recordKey(r); k.Model = m.modelTag; return k.String() }

	r, err := m.Remember(ctx, Record{ID: "a", Content: "用户喜欢咖啡", Importance: 0.5})
	require.NoError(t, err)
	require.Equal(t, []string{key(r)}, indexKeys(x, []string{"u"}))

	r2, err := m.Update(ctx, "a", r.Revision, Patch{Content: new("用户改喝茶")})
	require.NoError(t, err)
	require.Equal(t, []string{key(r2)}, indexKeys(x, []string{"u"}), "Update 写新键、删旧键")

	// 旧版本的键残留（如复核前崩溃）时不被采用。
	require.NoError(t, x.Upsert(ctx, recordSpace.namespace([]string{"u"}, initialScope), key(r), mustEmbed(t, "用户喜欢咖啡")))
	got, err := m.Recall(ctx, Query{Text: "咖啡"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Less(t, got[0].Relevance, 0.01, "旧版本的键（与查询的余弦约 0.7）不贡献向量相似度")
	require.NoError(t, m.Reindex(ctx, false))
	require.Equal(t, []string{key(r2)}, indexKeys(x, []string{"u"}), "Reindex 删除确认过期的键")

	require.NoError(t, m.Forget(ctx, "a"))
	require.Empty(t, indexKeys(x, []string{"u"}))
	checkIndex(t, m, x, []string{"u"})
}

// 设计 7.1：Reindex 锁外嵌入期间 Update 提交并写入新键。Reindex 写入的旧版本键在复核时删除，新键不被删除。
func TestReindexDuringUpdate(t *testing.T) {
	x := NewMemoryIndex()
	var m *Memory
	var inReindex, fired atomic.Bool
	ctx := WithNamespace(context.Background(), "u")
	emb := hookEmbedder{EmbeddingClient: bigramEmbedder{}, before: func(text string) {
		if inReindex.Load() && text == "旧内容" && fired.CompareAndSwap(false, true) {
			r, err := m.Get(ctx, "a")
			require.NoError(t, err)
			_, err = m.Update(ctx, "a", r.Revision, Patch{Content: new("新内容")})
			require.NoError(t, err)
		}
	}}
	m = newMemory(t, Config{Index: x, Embedder: emb, EmbedModel: "v1"})
	_, err := m.Remember(ctx, Record{ID: "a", Content: "旧内容", Importance: 0.5})
	require.NoError(t, err)

	// 先清空索引：旧键不在 Reindex 列出的键中，只能由复核删除。
	require.NoError(t, x.Clear(ctx, recordSpace.namespace([]string{"u"}, initialScope)))
	inReindex.Store(true)
	require.NoError(t, m.Reindex(ctx, false))
	require.True(t, fired.Load())
	r, err := m.Get(ctx, "a")
	require.NoError(t, err)
	k := recordKey(r)
	k.Model = m.modelTag
	require.Equal(t, []string{k.String()}, indexKeys(x, []string{"u"}), "只剩新版本的键")
	checkIndex(t, m, x, []string{"u"})
}

// 设计 7.1：Remember 提交之后、写索引之前 Purge 完成了全部清理。迟到的键写入旧作用域，复核时删除；不变量 7 成立。
func TestLateIndexWriteAfterPurge(t *testing.T) {
	base := NewMemoryIndex()
	var m *Memory
	var fired atomic.Bool
	ctx := WithNamespace(context.Background(), testNS...)
	idx := hookIndex{Index: base, before: func(op string, _ []string, _ string) error {
		if op == "upsert" && fired.CompareAndSwap(false, true) {
			require.NoError(t, m.Purge(ctx))
		}
		return nil
	}}
	m = newMemory(t, Config{Index: idx, Embedder: bigramEmbedder{}, EmbedModel: "v1"})
	_, err := m.Remember(ctx, Record{ID: "a", Content: "机密：项目代号青鸟", Importance: 0.5})
	require.NoError(t, err)
	require.True(t, fired.Load())
	require.Empty(t, indexKeys(base, testNS), "复核删除了写入旧作用域的迟到键")
	checkIndex(t, m, base, testNS)
}

// 设计 4.3、7.2：Purge 在 RemoveScope 之后、Index.Clear 之前崩溃，之后的清理重做；宽限期内迟到写入旧作用域的键
// （复核之前崩溃留下的）由到期时的再次清理删除，之后条目移出 retiring。
func TestRetireClearsIndexAfterCrashAndGrace(t *testing.T) {
	base := NewMemoryIndex()
	errCrash := errors.New("模拟崩溃")
	var crash atomic.Bool
	idx := hookIndex{Index: base, before: func(op string, _ []string, _ string) error {
		if op == "clear" && crash.Load() {
			return errCrash
		}
		return nil
	}}
	m := newMemory(t, Config{Index: idx, Embedder: bigramEmbedder{}, EmbedModel: "v1", Graph: true})
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	ctx := WithNamespace(context.Background(), testNS...)
	_, err := m.Remember(ctx, Record{ID: "a", Content: "机密：项目代号青鸟", Importance: 0.5})
	require.NoError(t, err)
	old := recordSpace.namespace(testNS, initialScope)
	require.NoError(t, base.Upsert(ctx, entitySpace.namespace(testNS, initialScope), "qn#1.1#"+m.modelTag, mustEmbed(t, "青鸟")))

	crash.Store(true)
	require.ErrorIs(t, m.Purge(ctx), errCrash)
	require.NotEmpty(t, indexKeys(base, testNS), "Clear 之前崩溃，旧作用域的键仍在")
	crash.Store(false)

	require.NoError(t, m.cleanupRetiring(ctx))
	require.Empty(t, indexKeys(base, testNS), "清理重做 Clear")
	require.Contains(t, retiringScopes(t, m), scopeName(testNS, initialScope), "宽限期内保留条目")

	require.NoError(t, base.Upsert(ctx, old, "a#1.1#"+m.modelTag, mustEmbed(t, "青鸟")))
	now = now.Add(m.cfg.RetireGrace)
	require.NoError(t, m.cleanupRetiring(ctx))
	require.Empty(t, indexKeys(base, testNS), "到期时再次清理删除迟到的键")
	requireNoRetiring(t, m)
	checkIndex(t, m, base, testNS)
}

// Reindex 补写缺失的记录与实体键，删除无法解析、其他模型、已失效记录与已不存在对象的键；不写 state。
func TestReindexRepairsIndex(t *testing.T) {
	x := NewMemoryIndex()
	m := newMemory(t, Config{Index: x, Embedder: bigramEmbedder{}, EmbedModel: "v1", Graph: true})
	ctx := WithNamespace(context.Background(), "u")
	a, err := m.Remember(ctx, Record{ID: "a", Content: "用户喜欢咖啡", Importance: 0.5})
	require.NoError(t, err)
	_, err = m.Remember(ctx, Record{ID: "b", Content: "用户住在北京", Importance: 0.5})
	require.NoError(t, err)
	require.NoError(t, graphOf(m).PutEntity(ctx, Entity{ID: "bj", Namespace: []string{"u"}, Name: "北京", Type: "place"}))
	require.NoError(t, m.Forget(ctx, "b"))

	recs, ents := recordSpace.namespace([]string{"u"}, initialScope), entitySpace.namespace([]string{"u"}, initialScope)
	require.NoError(t, x.Clear(ctx, recs))
	for _, junk := range []string{"不可解析", "a#1.1#" + modelTag("v0"), "b#2.1#" + m.modelTag, "gone#9.1#" + m.modelTag} {
		require.NoError(t, x.Upsert(ctx, recs, junk, mustEmbed(t, "残留")))
	}
	require.NoError(t, x.Upsert(ctx, ents, "nobody#9.1#"+m.modelTag, mustEmbed(t, "残留")))
	_, before, err := m.read(ctx, []string{"u"})
	require.NoError(t, err)

	require.NoError(t, m.Reindex(ctx, false))
	_, s, err := m.read(ctx, []string{"u"})
	require.NoError(t, err)
	require.Equal(t, before.Seq, s.Seq, "Reindex 不写 state")
	ak, bj := recordKey(a), entityKey(s.Entities["bj"])
	ak.Model, bj.Model = m.modelTag, m.modelTag
	got := indexKeys(x, []string{"u"})
	want := []string{ak.String(), bj.String()}
	slices.Sort(want)
	require.Equal(t, want, got)
	checkIndex(t, m, x, []string{"u"})

	require.NoError(t, m.Reindex(ctx, true), "force 重写全部，结果相同")
	require.Equal(t, want, indexKeys(x, []string{"u"}))
}
