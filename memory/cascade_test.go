package memory

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// derivedGraph 是级联测试的夹具：记录与由记录派生的关系都经公开接口或 applyRelations 写入。
type derivedGraph struct {
	t   *testing.T
	m   *Memory
	ctx context.Context
	n   int
}

func newDerivedGraph(t *testing.T, cfg Config) *derivedGraph {
	cfg.Graph = true
	m := newMemory(t, cfg)
	return &derivedGraph{t: t, m: m, ctx: WithNamespace(context.Background(), "u")}
}

// record 写入一条有效记录，返回其当前版本。
func (g *derivedGraph) record(r Record) Record {
	g.t.Helper()
	if r.Importance == 0 {
		r.Importance = 0.5
	}
	out, err := g.m.Remember(g.ctx, r)
	require.NoError(g.t, err)
	return out
}

// relate 以 basis 为依据写入一条关系，每次调用视为一个新轮次（会话 s，轮次序号递增）。
func (g *derivedGraph) relate(basis Record, r relation) {
	g.t.Helper()
	g.n++
	zero := 0
	r.From = &zero
	j := Job{Key: "s/" + strconv.Itoa(g.n), Namespace: []string{"u"}, Session: "s", From: int64(10 * g.n), To: int64(10*g.n + 1)}
	require.NoError(g.t, g.m.applyRelations(g.ctx, j, time.Date(2026, 1, g.n, 0, 0, 0, 0, time.UTC), []relation{r}, map[int]RecordRef{0: refOf(basis)}))
}

func (g *derivedGraph) state() state {
	g.t.Helper()
	_, s, err := g.m.read(g.ctx, []string{"u"})
	require.NoError(g.t, err)
	checkInvariants(g.t, []string{"u"}, s)
	return s
}

func (g *derivedGraph) edge(fact string) (Edge, bool) {
	for _, e := range g.state().Edges {
		if slices.ContainsFunc(e.Derivations, func(d Derivation) bool { return d.Fact == fact }) {
			return e, true
		}
	}
	return Edge{}, false
}

func (g *derivedGraph) entity(name string) (Entity, bool) {
	for _, e := range g.state().Entities {
		if slices.ContainsFunc(e.Names, func(n EntityName) bool { return n.Name == name }) {
			return e, true
		}
	}
	return Entity{}, false
}

func rel(subj, pred, obj, fact string) relation {
	return relation{Subject: entityRef{Name: subj, Type: "x"}, Predicate: pred, Object: entityRef{Name: obj, Type: "x"}, Fact: fact}
}

// 遗忘：只由被遗忘记录派生的关系与名称删除；有其他派生的关系仍有效，陈述改为另一条派生的。
func TestForgetCascadesDerivations(t *testing.T) {
	g := newDerivedGraph(t, Config{})
	a := g.record(Record{Content: "用户住在上海，喜欢咖啡"})
	b := g.record(Record{Content: "用户常住上海"})
	g.relate(a, rel("用户", "住在", "上海", "用户住在上海"))
	g.relate(b, rel("用户", "住在", "上海", "用户常住上海"))
	g.relate(a, rel("用户", "喜欢", "咖啡", "用户喜欢咖啡"))

	lives, ok := g.edge("用户住在上海")
	require.True(t, ok)
	require.Len(t, lives.Derivations, 2, "同三元组的有效关系追加派生，不新建")
	require.Equal(t, "用户常住上海", lives.Fact, "陈述取最新的派生")

	require.NoError(t, g.m.Forget(g.ctx, a.ID))
	s := g.state()
	require.Len(t, s.Edges, 1, "只由被遗忘记录派生的关系删除")
	lives = s.Edges[lives.ID]
	require.True(t, lives.Valid())
	require.Equal(t, "用户常住上海", lives.Fact)
	require.Len(t, lives.Derivations, 1)
	_, ok = g.entity("咖啡")
	require.False(t, ok, "名称只来自被遗忘记录的实体删除")
	raw, err := json.Marshal(s)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "用户住在上海", "被遗忘记录的陈述不留在状态中")
	require.NotContains(t, string(raw), "咖啡")
}

// 遗忘使实体的显示名被删除时改名：别名随之重算，旧名称的向量键移出索引，新名称写入。
func TestForgetRenamesEntity(t *testing.T) {
	x := NewMemoryIndex()
	g := newDerivedGraph(t, Config{Index: x, Embedder: bigramEmbedder{}, EmbedModel: "v1"})
	a := g.record(Record{Content: "用户住在上海市"})
	b := g.record(Record{Content: "用户住在沪"})
	g.relate(a, relation{Subject: entityRef{Name: "用户", Type: "x"}, Predicate: "住在", Object: entityRef{Name: "上海市", Type: "x", Aliases: []string{"沪"}}, Fact: "用户住在上海市"})
	g.relate(b, relation{Subject: entityRef{Name: "用户", Type: "x"}, Predicate: "到过", Object: entityRef{Name: "沪", Type: "x", Aliases: []string{"上海"}}, Fact: "用户到过沪"})
	before, ok := g.entity("上海市")
	require.True(t, ok)
	require.Equal(t, "上海市", before.Name)
	require.ElementsMatch(t, []string{"沪", "上海"}, before.Aliases)

	require.NoError(t, g.m.Forget(g.ctx, a.ID))
	after, ok := g.entity("沪")
	require.True(t, ok)
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, "沪", after.Name, "显示名被删除时取第一个存活的名称")
	require.Equal(t, []string{"上海"}, after.Aliases)
	require.Equal(t, before.ContentRev+1, after.ContentRev)
	old, cur := entityKey(before), entityKey(after)
	old.Model, cur.Model = g.m.modelTag, g.m.modelTag
	keys := indexKeys(x, []string{"u"})
	require.NotContains(t, keys, old.String(), "旧名称的向量移出索引")
	require.Contains(t, keys, cur.String(), "新名称写入索引")
	checkIndex(t, g.m, x, []string{"u"})
}

// 合并链：A 合并出 B，B 合并出 C，遗忘 A 后 B、C 都被清空；派生自它们的关系删除。
func TestForgetErasesMergeChain(t *testing.T) {
	g := newDerivedGraph(t, Config{})
	a := g.record(Record{Content: "用户在 Acme 工作"})
	b := g.record(Record{Content: "用户在 Acme 负责支付", MergedFrom: []RecordRef{refOf(a)}})
	c := g.record(Record{Content: "用户在 Acme 负责支付与风控", MergedFrom: []RecordRef{refOf(b)}})
	other := g.record(Record{Content: "用户喜欢茶"})
	g.relate(c, rel("用户", "就职于", "Acme", "用户就职于 Acme"))

	require.NoError(t, g.m.Forget(g.ctx, a.ID))
	s := g.state()
	for _, id := range []string{b.ID, c.ID} {
		r := s.Records[id]
		require.Equal(t, ReasonUnderived, r.InvalidReason, id)
		require.Empty(t, r.Content, "合并记录的正文被清空")
		require.Nil(t, r.Keywords)
	}
	require.True(t, s.Records[other.ID].Valid(), "无关记录不受影响")
	require.Empty(t, s.Edges, "派生自被清空记录的关系删除")
	require.Empty(t, s.Entities)
}

// 遗忘概括它的情景之一时反思被清空，其余情景的 Reflected 复位一次。
func TestForgetErasesReflection(t *testing.T) {
	g := newDerivedGraph(t, Config{})
	var eps []Record
	for i := range 3 {
		eps = append(eps, g.record(Record{Kind: KindEpisode, Content: "第 " + strconv.Itoa(i) + " 次加班", Reflected: true}))
	}
	refs := []RecordRef{refOf(eps[0]), refOf(eps[1]), refOf(eps[2])}
	insight := g.record(Record{Kind: KindReflection, Content: "用户经常加班", ReflectedFrom: refs})

	require.NoError(t, g.m.Forget(g.ctx, eps[0].ID))
	s := g.state()
	r := s.Records[insight.ID]
	require.Equal(t, ReasonUnderived, r.InvalidReason)
	require.Empty(t, r.Content)
	for _, e := range eps[1:] {
		cur := s.Records[e.ID]
		require.False(t, cur.Reflected, "其余情景可以重新参与反思")
		require.Equal(t, e.Revision+1, cur.Revision, "只复位一次")
	}
}

// 依据情景被替代或过期时反思不受影响：反思本来就是为了在情景衰减后留存结论。
func TestReflectionSurvivesSupersededBasis(t *testing.T) {
	g := newDerivedGraph(t, Config{})
	ep := g.record(Record{Kind: KindEpisode, Content: "周五加班", Reflected: true})
	insight := g.record(Record{Kind: KindReflection, Content: "用户经常加班", ReflectedFrom: []RecordRef{refOf(ep)}})
	_, err := g.m.mutate(g.ctx, []string{"u"}, "", func(t *tx) error {
		r, _ := t.Records.Get(ep.ID)
		t.Records.Put(r.ID, r.invalidated(g.m.now(), ReasonExpired, ""))
		g.m.cascadeTx(t, g.m.now())
		return nil
	})
	require.NoError(t, err)
	require.True(t, g.state().Records[insight.ID].Valid())
}

// B4：A 被合并进 B，B 又被 C 否定；遗忘 A 后 B 被清空，回退 C 时 B 不恢复，状态中不再出现 A 的内容。
func TestErasedSupersededRecordNotRestored(t *testing.T) {
	g := newDerivedGraph(t, Config{})
	a := g.record(Record{Content: "用户的车牌是沪A12345"})
	b := g.record(Record{Content: "用户的车牌是沪A12345，车是白色", MergedFrom: []RecordRef{refOf(a)}})
	c := g.record(Record{Content: "用户已经卖掉了车", Sources: []Source{{Session: "s", From: 5, To: 6}}})
	_, err := g.m.mutate(g.ctx, []string{"u"}, "", func(t *tx) error {
		r, _ := t.Records.Get(b.ID)
		t.Records.Put(r.ID, r.invalidated(g.m.now(), ReasonSuperseded, c.ID))
		return nil
	})
	require.NoError(t, err)

	require.NoError(t, g.m.Forget(g.ctx, a.ID))
	require.NoError(t, g.m.Rewound(g.ctx, "s", 4))
	s := g.state()
	require.Equal(t, ReasonSuperseded, s.Records[b.ID].InvalidReason, "擦除过的记录不恢复")
	raw, err := json.Marshal(s)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "沪A12345")
}

// 只改置顶或重要度不触及图谱；修改正文时，只由旧正文派生的关系删除。
func TestUpdateCascadesOnlyOnContent(t *testing.T) {
	g := newDerivedGraph(t, Config{})
	a := g.record(Record{Content: "用户住在上海"})
	b := g.record(Record{Content: "用户常住上海"})
	g.relate(a, rel("用户", "喜欢", "咖啡", "用户喜欢咖啡"))
	g.relate(a, rel("用户", "住在", "上海", "用户住在上海"))
	g.relate(b, rel("用户", "住在", "上海", "用户常住上海"))
	before := g.state()

	pinned, imp := true, 0.9
	a, err := g.m.Update(g.ctx, a.ID, a.Revision, Patch{Pinned: &pinned, Importance: &imp})
	require.NoError(t, err)
	after := g.state()
	require.Equal(t, before.Edges, after.Edges, "不改正文时关系不变")
	require.Equal(t, before.Entities, after.Entities)

	content := "用户住在上海浦东"
	_, err = g.m.Update(g.ctx, a.ID, a.Revision, Patch{Content: &content})
	require.NoError(t, err)
	s := g.state()
	require.Len(t, s.Edges, 1, "只由旧正文派生的关系删除")
	for _, e := range s.Edges {
		require.Equal(t, "用户常住上海", e.Fact)
		require.True(t, e.Valid())
	}
	_, ok := g.entity("咖啡")
	require.False(t, ok)
}

// 有效的派生记录失效（未擦除）时关系 underived 但保留；记录恢复时关系随之恢复。
func TestEdgeUnderivedFollowsRecordValidity(t *testing.T) {
	g := newDerivedGraph(t, Config{})
	a := g.record(Record{Content: "用户住在上海"})
	g.relate(a, rel("用户", "住在", "上海", "用户住在上海"))
	set := func(valid bool) {
		_, err := g.m.mutate(g.ctx, []string{"u"}, "", func(t *tx) error {
			r, _ := t.Records.Get(a.ID)
			if valid {
				r.InvalidAt, r.InvalidReason = time.Time{}, ""
			} else {
				r = r.invalidated(g.m.now(), ReasonExpired, "")
			}
			t.Records.Put(r.ID, r)
			g.m.cascadeTx(t, g.m.now())
			return nil
		})
		require.NoError(t, err)
	}
	set(false)
	e, ok := g.edge("用户住在上海")
	require.True(t, ok, "依据仍在、只是失效时关系保留")
	require.Equal(t, ReasonUnderived, e.InvalidReason)
	u, _ := g.entity("用户")
	require.False(t, u.Live, "名称来源都不成立时实体不存活")
	set(true)
	e, _ = g.edge("用户住在上海")
	require.True(t, e.Valid(), "依据恢复时关系恢复")
}

// 社区指纹随关系陈述与端点名称变化，摘要依据的版本据此复核。
func TestCommunityFingerprintTracksFactsAndNames(t *testing.T) {
	edges := []Edge{
		{ID: "1", Subject: "a", Object: "b", Predicate: "p", ContentRev: 1, Derivations: []Derivation{{}}},
		{ID: "2", Subject: "b", Object: "c", Predicate: "p", ContentRev: 1, Derivations: []Derivation{{}}},
	}
	names := map[string]string{"a": "甲", "b": "乙", "c": "丙"}
	fp := func() string {
		cs := detectCommunities([]string{"u"}, edges, names)
		require.Len(t, cs, 1)
		return cs[0].Fingerprint
	}
	base := fp()
	edges[1].ContentRev = 2
	changedFact := fp()
	require.NotEqual(t, base, changedFact, "陈述改变时指纹改变")
	names["c"] = "丁"
	require.NotEqual(t, changedFact, fp(), "名称改变时指纹改变")
}

// 没有派生的关系（G1 写入的旧数据）不成立；任何一次级联都会删除它与没有名称的实体。
func TestLegacyGraphFilteredAndPruned(t *testing.T) {
	g := newDerivedGraph(t, Config{})
	_, err := g.m.mutate(g.ctx, []string{"u"}, "", func(t *tx) error {
		t.Entities.Put("a", Entity{ID: "a", Name: "甲", Incarnation: t.incarnation()})
		t.Entities.Put("b", Entity{ID: "b", Name: "乙", Incarnation: t.incarnation()})
		t.Edges.Put("e", Edge{ID: "e", Subject: "a", Predicate: "p", Object: "b", Fact: "甲 p 乙", Incarnation: t.incarnation()})
		return nil
	})
	require.NoError(t, err)
	got, err := g.m.Related(g.ctx, "甲", nil, 2, 10)
	require.NoError(t, err)
	require.Empty(t, got.Relations, "读取时过滤旧关系")
	require.NoError(t, g.m.Prune(g.ctx))
	s := g.state()
	require.Empty(t, s.Edges, "Prune 删除旧关系")
	require.Empty(t, s.Entities, "Prune 删除旧实体")
}

// 来源数量有上限：枢纽实体的名称来源与关系的派生不超过 maxOrigins，先删不成立的。
func TestOriginsCapped(t *testing.T) {
	g := newDerivedGraph(t, Config{})
	var first Record
	for i := range maxOrigins + 5 {
		r := g.record(Record{Content: "用户第 " + strconv.Itoa(i) + " 次提到 Acme"})
		if i == 0 {
			first = r
		}
		g.relate(r, rel("用户", "提到", "Acme", "用户提到 Acme"))
	}
	u, ok := g.entity("用户")
	require.True(t, ok)
	require.Len(t, u.Names[0].Origins, maxOrigins)
	e, _ := g.edge("用户提到 Acme")
	require.Len(t, e.Derivations, maxOrigins)
	require.False(t, slices.ContainsFunc(e.Derivations, func(d Derivation) bool { return d.Record.ID == first.ID }), "超出时删除最早的")
}

// 关系的依据记录不再是两端实体的名称来源时（来源超出上限被删除），遗忘它仍要删除这条关系的派生；压缩之后由反向依赖找到。
func TestForgetFindsEdgeWithoutNameOrigin(t *testing.T) {
	g := newDerivedGraph(t, Config{})
	first := g.record(Record{Content: "用户去过 Acme"})
	g.relate(first, rel("用户", "去过", "Acme", "用户去过 Acme"))
	for i := range maxOrigins {
		r := g.record(Record{Content: "用户第 " + strconv.Itoa(i) + " 次说喜欢 Acme"})
		g.relate(r, rel("用户", "喜欢", "Acme", "用户喜欢 Acme"))
	}
	for _, name := range []string{"用户", "Acme"} {
		e, ok := g.entity(name)
		require.True(t, ok)
		require.False(t, slices.ContainsFunc(e.Names[0].Origins, func(o Origin) bool { return o.Record.ID == first.ID }))
	}
	require.NoError(t, g.m.compact(g.ctx, []string{"u"}, true, true))
	require.NoError(t, g.m.Forget(g.ctx, first.ID))
	_, ok := g.edge("用户去过 Acme")
	require.False(t, ok)
}

// 整理合并：update 之后派生自目标的关系与名称仍成立并指向新记录；合并记录被遗忘后关系失效。invalidate 不转移派生。
func TestReconcileTransfersDerivations(t *testing.T) {
	for _, op := range []string{"update", "invalidate"} {
		t.Run(op, func(t *testing.T) {
			m, sessions, _, model := graphMemoryWith(t, Config{}, map[string]string{
				"我住在上海": `{"memories":[{"kind":"fact","content":"用户住在上海","importance":0.5}],"relations":[
					{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"住在","object":{"name":"上海","type":"place"},"fact":"用户住在上海","exclusive":true}]}`,
				"浦东": `{"memories":[{"kind":"fact","content":"用户住在上海浦东","importance":0.5}],"relations":[]}`,
			})
			extract := model.reply
			model.reply = func(system, user string) (string, error) {
				if system != reconcilePrompt {
					return extract(system, user)
				}
				_, rest, _ := strings.Cut(user, "- id=")
				target, _, _ := strings.Cut(rest, "：")
				return `{"op":"` + op + `","target":"` + target + `","content":"用户住在上海浦东"}`, nil
			}
			ctx := WithNamespace(context.Background(), "u")
			runTurn(t, m, sessions, "s/1", "我住在上海")
			runTurn(t, m, sessions, "s/2", "我搬到浦东了")
			_, s, err := m.read(ctx, []string{"u"})
			require.NoError(t, err)
			checkInvariants(t, []string{"u"}, s)
			var merged Record
			for _, r := range s.Records {
				if r.Valid() {
					merged = r
				}
			}
			require.Equal(t, "用户住在上海浦东", merged.Content)
			require.Len(t, s.Edges, 1)
			var edge Edge
			for _, e := range s.Edges {
				edge = e
			}
			toMerged := slices.ContainsFunc(edge.Derivations, func(d Derivation) bool { return d.Record == refOf(merged) })
			if op == "invalidate" {
				require.False(t, toMerged, "因矛盾被替代的记录，派生不转移")
				require.False(t, edge.Valid())
				return
			}
			require.True(t, toMerged, "派生转移到合并记录")
			require.True(t, edge.Valid())
			for _, e := range s.Entities {
				require.True(t, e.Live, "名称来源同样转移，实体仍存活")
			}

			require.NoError(t, m.Forget(ctx, merged.ID))
			_, s, err = m.read(ctx, []string{"u"})
			require.NoError(t, err)
			checkInvariants(t, []string{"u"}, s)
			for _, e := range s.Edges {
				require.False(t, e.Valid(), "合并记录被遗忘后关系失效")
				require.Equal(t, "用户住在上海", e.Fact)
			}
		})
	}
}

// G1 写入的 head 与基础快照（/2）能被读取，包括 head /3 引用基础快照 /2 的状态；新的写入只用 /3。
func TestReadsG1StateFormat(t *testing.T) {
	ctx := WithNamespace(context.Background(), testNS...)
	st := newHookStore(t)
	m := durableMemory(t, st, Config{Graph: true})
	_, err := m.mutate(ctx, testNS, "", addRecord("a"))
	require.NoError(t, err)
	require.NoError(t, m.compact(ctx, testNS, true, true))
	_, err = m.mutate(ctx, testNS, "", addRecord("b"))
	require.NoError(t, err)

	sc := st.Scope(scopeName(testNS, initialScope))
	downgrade := func(id, from, to string) bool {
		snap, err := sc.Load(ctx, id)
		if errors.Is(err, os.ErrNotExist) {
			return false
		}
		require.NoError(t, err)
		require.Equal(t, from, snap.Version)
		snap.Version = to
		unlock, err := sc.Lock(ctx, id)
		require.NoError(t, err)
		require.NoError(t, sc.Save(ctx, id, snap))
		unlock()
		return true
	}
	bases := 0
	for slot := range 2 {
		if downgrade(baseID(slot), stateBlobVersion, stateBlobV2) {
			bases++
		}
	}
	require.Positive(t, bases, "压缩写出了基础快照")
	check := func(msg string) {
		_, s, err := durableMemory(t, st, Config{Graph: true}).read(ctx, testNS)
		require.NoError(t, err, msg)
		require.ElementsMatch(t, []string{"a", "b"}, slices.Collect(maps.Keys(s.Records)), msg)
	}
	check("head /3 引用基础快照 /2")
	downgrade(headID, headVersion, headV2)
	check("head /2")

	_, err = durableMemory(t, st, Config{Graph: true}).mutate(ctx, testNS, "", addRecord("c"))
	require.NoError(t, err)
	snap, err := sc.Load(ctx, headID)
	require.NoError(t, err)
	require.Equal(t, headVersion, snap.Version, "新的写入只用 /3")
}

// 性质测试：随机交替写入、派生、合并、遗忘、修改正文、回退与 Prune，每步之后不变量成立（含级联重算已收敛）。
func TestCascadeProperty(t *testing.T) {
	for seed := range uint64(6) {
		t.Run(strconv.FormatUint(seed, 10), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, 11))
			g := newDerivedGraph(t, Config{})
			names := []string{"用户", "上海", "Acme", "咖啡", "张三"}
			preds := []string{"住在", "喜欢", "认识"}
			valid := func() []Record {
				var out []Record
				s := g.state()
				for _, id := range slices.Sorted(maps.Keys(s.Records)) {
					if r := s.Records[id]; r.Valid() {
						out = append(out, r)
					}
				}
				return out
			}
			pick := func(rs []Record) Record { return rs[rng.IntN(len(rs))] }
			for step := range 80 {
				rs := valid()
				switch op := rng.IntN(10); {
				case op < 3 || len(rs) == 0:
					r := Record{Content: "事实 " + strconv.Itoa(step), Sources: []Source{{Session: "s", From: int64(step), To: int64(step)}}}
					if len(rs) > 0 && rng.IntN(3) == 0 {
						r.MergedFrom = []RecordRef{refOf(pick(rs))}
					}
					if len(rs) > 0 && rng.IntN(4) == 0 {
						r.Kind, r.ReflectedFrom = KindReflection, []RecordRef{refOf(pick(rs)), refOf(pick(rs))}
					}
					g.record(r)
				case op < 6:
					s, o := names[rng.IntN(len(names))], names[rng.IntN(len(names))]
					p := preds[rng.IntN(len(preds))]
					r := rel(s, p, o, s+p+o)
					r.Exclusive = p == "住在"
					g.relate(pick(rs), r)
				case op < 7:
					require.NoError(t, g.m.Forget(g.ctx, pick(rs).ID))
				case op < 8:
					r := pick(rs)
					content := r.Content + "（修订）"
					_, err := g.m.Update(g.ctx, r.ID, r.Revision, Patch{Content: &content})
					require.NoError(t, err)
				case op < 9:
					require.NoError(t, g.m.Rewound(g.ctx, "s", int64(rng.IntN(step+1)*10)))
				default:
					require.NoError(t, g.m.Prune(g.ctx))
				}
				// 压缩之后级联从基础快照的反向依赖找候选，不压缩时全部对象都在覆盖层中。
				if rng.IntN(4) == 0 {
					require.NoError(t, g.m.compact(g.ctx, []string{"u"}, true, true))
				}
				g.state()
			}
		})
	}
}

func TestSameNameMatchesNormalize(t *testing.T) {
	names := []string{"", " ", "张三", " 张三 ", "张 三", "张三 ", "ABC", "abc", "a b", "ab", "a  b", "a b ", "a\tb", "Ab C", "ab c", "a bc", "\xff", "\xfe", "ΣΑ", "σα", "İ", "i"}
	for _, a := range names {
		for _, b := range names {
			require.Equal(t, normalizeName(a) == normalizeName(b), sameName(a, b), "%q %q", a, b)
		}
	}
}
