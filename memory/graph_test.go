package memory

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/llm"
)

// graphMemory 按用户输入返回预设的关系抽取结果。
func graphMemory(t *testing.T, replies map[string]string) (*Memory, session.Store, *time.Time) {
	t.Helper()
	m, sessions, now, _ := graphMemoryWith(t, Config{}, replies)
	return m, sessions, now
}

// graphMemoryWith 在 cfg 上补齐存储、图谱与按用户输入应答抽取请求的模型。
func graphMemoryWith(t *testing.T, cfg Config, replies map[string]string) (*Memory, session.Store, *time.Time, *scriptedModel) {
	t.Helper()
	sessions := session.NewMemoryStore()
	model := &scriptedModel{reply: func(system, user string) (string, error) {
		if system == reconcilePrompt {
			return `{"op":"add"}`, nil
		}
		require.Contains(t, system, "relations", "启用图谱时抽取提示词要求输出关系")
		require.Contains(t, user, "对话时间：", "抽取时给出对话时间，用于换算相对时间")
		for key, reply := range replies {
			if strings.Contains(user, key) {
				return reply, nil
			}
		}
		return "", errors.New("没有预设的回复")
	}}
	cfg.Graph, cfg.Model, cfg.Sessions, cfg.Retention = true, model, sessions, Retention{InvalidRetention: time.Hour}
	m := newMemory(t, cfg)
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	return m, sessions, &now, model
}

func runTurn(t *testing.T, m *Memory, sessions session.Store, key, input string) {
	t.Helper()
	ctx := WithNamespace(context.Background(), "u")
	from, to := appendTurnAt(t, sessions, "s", m.now(), false, llm.UserMessage(input), llm.Message{Role: "assistant", Content: "好的"})
	// TurnSeq 与 Builder 相同，取轮次开始时的 Seq：之前的回退标记不拦截之后开始的轮次。
	require.NoError(t, m.Enqueue(ctx, Job{Key: key, Namespace: []string{"u"}, Session: "s", Scope: "0", From: from, To: to, TurnSeq: stateOf(t, m).Seq}))
	_, err := m.ProcessPending(ctx)
	require.NoError(t, err)
	j, _, err := m.Job(ctx, key)
	require.NoError(t, err)
	require.Equal(t, JobDone, j.Status, j.Error)
}

// 排他关系出现新客体时旧关系失效并指向新关系，非排他关系并存；实体按名称与别名去重；可查询某一时刻成立的关系。
func TestGraphTemporalRelations(t *testing.T) {
	m, sessions, now := graphMemory(t, map[string]string{
		"上海": `{"memories":[{"kind":"fact","content":"用户住在上海","importance":0.5},{"kind":"fact","content":"用户喜欢咖啡","importance":0.5}],"relations":[
			{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"住在","object":{"name":"上海市","type":"place","aliases":["上海"]},"fact":"用户住在上海","exclusive":true},
			{"from":1,"subject":{"name":"用户","type":"person"},"predicate":"喜欢","object":{"name":"咖啡","type":"product"},"fact":"用户喜欢咖啡","exclusive":false}]}`,
		"北京": `{"memories":[{"kind":"fact","content":"用户住在北京","importance":0.5},{"kind":"fact","content":"用户喜欢茶","importance":0.5}],"relations":[
			{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"住在","object":{"name":"北京","type":"place"},"fact":"用户住在北京","exclusive":true},
			{"from":1,"subject":{"name":"用户","type":"person"},"predicate":"喜欢","object":{"name":"茶","type":"product"},"fact":"用户喜欢茶","exclusive":false}]}`,
		"老家": `{"memories":[{"kind":"fact","content":"用户老家在上海","importance":0.5}],"relations":[
			{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"老家在","object":{"name":"上海","type":"place"},"fact":"用户老家在上海","exclusive":true}]}`,
	})
	ctx := WithNamespace(context.Background(), "u")
	runTurn(t, m, sessions, "s/1", "我住在上海，喜欢咖啡")
	moved := now.Add(30 * 24 * time.Hour)
	*now = moved
	runTurn(t, m, sessions, "s/2", "我搬到北京了，最近喜欢茶")
	runTurn(t, m, sessions, "s/3", "说说我的老家")

	entities, err := graphOf(m).Entities(ctx, []string{"u"})
	require.NoError(t, err)
	var names []string
	for _, e := range entities {
		names = append(names, e.Type+":"+e.Name)
	}
	require.ElementsMatch(t, []string{"person:用户", "place:上海市", "product:咖啡", "place:北京", "product:茶"}, names, "按别名去重：上海与上海市是同一实体")

	edges, err := graphOf(m).Edges(ctx, []string{"u"})
	require.NoError(t, err)
	byFact := map[string]Edge{}
	for _, e := range edges {
		byFact[e.Fact] = e
	}
	require.Len(t, byFact, 5)
	shanghai, beijing := byFact["用户住在上海"], byFact["用户住在北京"]
	require.False(t, shanghai.Valid())
	require.Equal(t, beijing.ID, shanghai.SupersededBy)
	require.True(t, shanghai.ValidAt(moved.Add(-time.Hour)), "搬家前住在上海成立")
	require.False(t, shanghai.ValidAt(moved.Add(time.Hour)))
	require.True(t, beijing.ValidAt(moved.Add(time.Hour)))
	require.True(t, byFact["用户喜欢咖啡"].Valid(), "非排他关系并存")
	require.True(t, byFact["用户喜欢茶"].Valid())
	require.True(t, byFact["用户老家在上海"].Valid(), "不同谓词互不影响")
}

func TestRelatedMultiHopAndRecallBlock(t *testing.T) {
	m := newMemory(t, Config{Graph: true, GraphHops: 2})
	g := graphOf(m)
	ctx := WithNamespace(context.Background(), "u")
	ns := []string{"u"}
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, e := range []Entity{
		{ID: "user", Name: "用户", Type: "person"},
		{ID: "acme", Name: "Acme 公司", Type: "org", Aliases: []string{"acme"}},
		{ID: "sz", Name: "深圳", Type: "place"},
		{ID: "old", Name: "旧公司", Type: "org"},
	} {
		e.Namespace = ns
		require.NoError(t, g.PutEntity(ctx, e))
	}
	for _, e := range []Edge{
		{ID: "e1", Subject: "user", Predicate: "就职于", Object: "acme", Fact: "用户就职于 Acme 公司"},
		{ID: "e2", Subject: "acme", Predicate: "位于", Object: "sz", Fact: "Acme 公司位于深圳"},
		{ID: "e3", Subject: "user", Predicate: "就职于", Object: "old", Fact: "用户就职于旧公司", InvalidAt: day, InvalidReason: ReasonSuperseded},
	} {
		e.Namespace, e.ValidFrom = ns, day
		require.NoError(t, g.PutEdge(ctx, e))
	}
	tests := []struct {
		name  string
		text  string
		names []string
		hops  int
		want  []string
	}{
		{"由名称种子扩展两跳", "", []string{"用户"}, 2, []string{"用户就职于 Acme 公司", "Acme 公司位于深圳"}},
		{"一跳", "", []string{"用户"}, 1, []string{"用户就职于 Acme 公司"}},
		{"别名出现在查询中", "ACME 在哪个城市", nil, 1, []string{"用户就职于 Acme 公司", "Acme 公司位于深圳"}},
		{"没有种子", "今天天气", nil, 2, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rels, err := m.Related(ctx, tt.text, tt.names, tt.hops, 10)
			require.NoError(t, err)
			var got []string
			for _, r := range rels.Relations {
				got = append(got, r.Fact)
			}
			require.ElementsMatch(t, tt.want, got)
		})
	}

	_, err := m.Remember(ctx, Record{Content: "用户最近在找房子", Entities: []string{"用户"}, Importance: 0.5})
	require.NoError(t, err)
	block, err := m.RecallBlock(ctx, "找房子")
	require.NoError(t, err)
	require.Contains(t, block, "Acme 公司位于深圳", "召回记录中的实体作为关系种子")
	require.NotContains(t, block, "旧公司", "失效关系不召回")
}

func TestGraphRewoundPurgeAndPrune(t *testing.T) {
	m, sessions, now := graphMemory(t, map[string]string{
		"上海": `{"memories":[{"kind":"fact","content":"用户住在上海","importance":0.5}],"relations":[
			{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"住在","object":{"name":"上海","type":"place"},"fact":"用户住在上海","exclusive":true}]}`,
	})
	ctx := WithNamespace(context.Background(), "u")
	runTurn(t, m, sessions, "s/1", "我住在上海")
	edges, err := graphOf(m).Edges(ctx, []string{"u"})
	require.NoError(t, err)
	require.Len(t, edges, 1)

	require.NoError(t, m.Rewound(ctx, "s", edges[0].Sources[0].From-1))
	edges, err = graphOf(m).Edges(ctx, []string{"u"})
	require.NoError(t, err)
	require.Empty(t, edges, "退回的轮次派生的关系随之删除")
	entities, err := graphOf(m).Entities(ctx, []string{"u"})
	require.NoError(t, err)
	require.Empty(t, entities, "名称只来自退回轮次的实体随之删除")
	*now = now.Add(2 * time.Hour)

	runTurn(t, m, sessions, "s/2", "我还是住在上海")
	require.NoError(t, m.Purge(ctx))
	entities, err = graphOf(m).Entities(ctx, []string{"u"})
	require.NoError(t, err)
	require.Empty(t, entities)
	edges, err = graphOf(m).Edges(ctx, []string{"u"})
	require.NoError(t, err)
	require.Empty(t, edges)
}

// nameEmbedder 给指定名称固定向量，其余文本取同一默认向量，用来模拟"Acme"与"爱克米"这类翻译名称的向量相近。
type nameEmbedder map[string][]float32

func (e nameEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if v, ok := e[text]; ok {
		return v, nil
	}
	return []float32{0, 0, 0, 1}, nil
}

// 名称不同的实体由向量找到候选、模型判定是否重复；模型编造的 ID 不被采纳；没有候选时不请求模型。
func TestEntityResolutionByModel(t *testing.T) {
	acmeID := stableID("u", "org", "acme 公司")
	cfg := Config{Index: NewMemoryIndex(), EmbedModel: "n", Embedder: nameEmbedder{
		"Acme 公司": {1, 0, 0, 0}, "爱克米": {0.95, 0.05, 0, 0}, "爱克米集团": {0.9, 0.1, 0, 0}, "用户": {0, 1, 0, 0}, "深圳": {0, 0, 1, 0},
	}}
	var judged []string
	m, sessions, _, model := graphMemoryWith(t, cfg, map[string]string{
		"上班": `{"memories":[{"kind":"fact","content":"用户就职于 Acme 公司","importance":0.5}],"relations":[{"from":0,"subject":{"name":"用户","type":"person"},"predicate":"就职于","object":{"name":"Acme 公司","type":"org"},"fact":"用户就职于 Acme 公司","exclusive":true}]}`,
		"招人": `{"memories":[{"kind":"fact","content":"爱克米位于深圳","importance":0.5}],"relations":[{"from":0,"subject":{"name":"爱克米","type":"org"},"predicate":"位于","object":{"name":"深圳","type":"place"},"fact":"爱克米位于深圳","exclusive":true}]}`,
		"集团": `{"memories":[{"kind":"fact","content":"爱克米集团收购了爱克米","importance":0.5}],"relations":[{"from":0,"subject":{"name":"爱克米集团","type":"org"},"predicate":"收购","object":{"name":"爱克米","type":"org"},"fact":"爱克米集团收购了爱克米","exclusive":false}]}`,
	})
	reply := model.reply
	model.reply = func(system, user string) (string, error) {
		if system != resolvePrompt {
			return reply(system, user)
		}
		judged = append(judged, user)
		if strings.Contains(user, "爱克米集团") {
			return `{"resolutions":[{"index":0,"duplicate_of":"不存在的id"}]}`, nil
		}
		return `{"resolutions":[{"index":0,"duplicate_of":"` + acmeID + `"}]}`, nil
	}
	ctx := WithNamespace(context.Background(), "u")
	runTurn(t, m, sessions, "s/1", "我在 Acme 上班")
	runTurn(t, m, sessions, "s/2", "爱克米最近在招人")
	runTurn(t, m, sessions, "s/3", "爱克米集团的新闻")

	require.Len(t, judged, 2, "第一轮没有已有实体，不请求模型")
	require.Contains(t, judged[0], acmeID)
	require.NotContains(t, judged[0], "深圳", "没有相似候选的实体不交给模型")

	entities, err := graphOf(m).Entities(ctx, []string{"u"})
	require.NoError(t, err)
	byName := map[string]Entity{}
	for _, e := range entities {
		byName[e.Name] = e
	}
	require.Len(t, byName, 4, "用户、Acme 公司、深圳、爱克米集团")
	require.Equal(t, acmeID, byName["Acme 公司"].ID)
	require.Contains(t, byName["Acme 公司"].Aliases, "爱克米", "判定为同一实体的名称并入别名")
	scope, err := m.currentScope(ctx, []string{"u"})
	require.NoError(t, err)
	keys, err := m.cfg.Index.IDs(ctx, entitySpace.namespace([]string{"u"}, scope))
	require.NoError(t, err)
	k := entityKey(byName["深圳"])
	k.Model = m.modelTag
	require.Contains(t, keys, k.String(), "实体名称向量写入 Index 的实体子命名空间")

	edges, err := graphOf(m).Edges(ctx, []string{"u"})
	require.NoError(t, err)
	for _, e := range edges {
		switch e.Predicate {
		case "位于":
			require.Equal(t, acmeID, e.Subject)
		case "收购":
			require.Equal(t, byName["爱克米集团"].ID, e.Subject, "编造的 ID 不被采纳，新建实体")
			require.Equal(t, acmeID, e.Object, "别名精确匹配")
		}
	}
}

// 迟到的旧事实：已有排他关系比新写入的关系生效更晚时，新关系写入即失效并指向已有关系，已有关系不受影响。
func TestApplyRelationsLateFact(t *testing.T) {
	m := newMemory(t, Config{Graph: true})
	ctx := WithNamespace(context.Background(), "u")
	march, jan := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	zero := 0
	live := func(city string) relation {
		return relation{From: &zero, Subject: entityRef{Name: "用户", Type: "person"}, Predicate: "住在", Object: entityRef{Name: city, Type: "place"}, Fact: "用户住在" + city, Exclusive: true}
	}
	require.NoError(t, m.applyRelations(ctx, Job{Key: "s/2", Namespace: []string{"u"}, Session: "s"}, march, []relation{live("北京")}, basisOf(t, m, "北京")))
	require.NoError(t, m.applyRelations(ctx, Job{Key: "s/1", Namespace: []string{"u"}, Session: "s"}, jan, []relation{live("上海")}, basisOf(t, m, "上海")))
	edges, err := graphOf(m).Edges(ctx, []string{"u"})
	require.NoError(t, err)
	byFact := map[string]Edge{}
	for _, e := range edges {
		byFact[e.Fact] = e
	}
	beijing, shanghai := byFact["用户住在北京"], byFact["用户住在上海"]
	require.True(t, beijing.Valid())
	require.Equal(t, march, shanghai.InvalidAt)
	require.Equal(t, beijing.ID, shanghai.SupersededBy)
	require.True(t, shanghai.ValidAt(jan.Add(24*time.Hour)), "一月到三月间住在上海")
}

// PageRank 以种子为中心排序：查询 Acme 时与 Acme 相连的关系排在"用户"的其他关系之前。
func TestRelatedPageRankRanking(t *testing.T) {
	m := newMemory(t, Config{Graph: true})
	g := graphOf(m)
	ctx := WithNamespace(context.Background(), "u")
	ns := []string{"u"}
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, e := range []Entity{{ID: "user", Name: "用户"}, {ID: "acme", Name: "Acme"}, {ID: "sz", Name: "深圳"}, {ID: "zs", Name: "张三"},
		{ID: "coffee", Name: "咖啡"}, {ID: "tea", Name: "茶"}, {ID: "cat", Name: "猫"}} {
		e.Namespace, e.Type = ns, "x"
		require.NoError(t, g.PutEntity(ctx, e))
	}
	for i, pair := range [][2]string{{"user", "acme"}, {"acme", "sz"}, {"acme", "zs"}, {"user", "coffee"}, {"user", "tea"}, {"user", "cat"}} {
		// 越晚写入的关系越新，排除新近性对排序的影响：与 Acme 无关的关系反而更新。
		require.NoError(t, g.PutEdge(ctx, Edge{ID: strconv.Itoa(i), Namespace: ns, Subject: pair[0], Predicate: "关联", Object: pair[1], Fact: pair[0] + "-" + pair[1], ValidFrom: day.Add(time.Duration(i) * time.Hour)}))
	}
	got, err := m.Related(ctx, "Acme 怎么样", nil, 2, 3)
	require.NoError(t, err)
	var facts []string
	for i, r := range got.Relations {
		facts = append(facts, r.Fact)
		if i > 0 {
			require.GreaterOrEqual(t, got.Relations[i-1].Score, r.Score)
		}
	}
	require.ElementsMatch(t, []string{"user-acme", "acme-sz", "acme-zs"}, facts)

	all, err := m.Related(ctx, "Acme 怎么样", nil, 2, 10)
	require.NoError(t, err)
	require.Len(t, all.Relations, 6, "两跳内的关系都在候选中")
}

func TestPersonalizedPageRankSumsToOne(t *testing.T) {
	edges := []Edge{{Subject: "a", Object: "b"}, {Subject: "b", Object: "c"}, {Subject: "c", Object: "a"}, {Subject: "c", Object: "d"}}
	p := personalizedPageRank(edges, map[string]float64{"a": 1})
	total := 0.0
	for _, v := range p {
		total += v
	}
	require.InDelta(t, 1, total, 1e-6)
	require.Greater(t, p["a"], p["d"], "离种子越远得分越低")
	require.Nil(t, personalizedPageRank(edges, map[string]float64{"x": 1}), "种子不在图中")
}

// 回退取代旧关系的轮次时，该轮派生的关系删除，旧关系恢复有效。
func TestRewoundRestoresSupersededEdge(t *testing.T) {
	m := newMemory(t, Config{Graph: true})
	ctx := WithNamespace(context.Background(), "u")
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	zero := 0
	live := func(city string) []relation {
		return []relation{{From: &zero, Subject: entityRef{Name: "用户", Type: "person"}, Predicate: "住在", Object: entityRef{Name: city, Type: "place"}, Fact: "用户住在" + city, Exclusive: true}}
	}
	require.NoError(t, m.applyRelations(ctx, Job{Key: "s/1", Namespace: []string{"u"}, Session: "s", From: 1, To: 3}, day, live("上海"), basisOf(t, m, "上海")))
	require.NoError(t, m.applyRelations(ctx, Job{Key: "s/2", Namespace: []string{"u"}, Session: "s", From: 4, To: 6}, day.Add(time.Hour), live("北京"), basisOf(t, m, "北京")))
	require.NoError(t, m.Rewound(ctx, "s", 3))
	edges, err := graphOf(m).Edges(ctx, []string{"u"})
	require.NoError(t, err)
	valid := map[string]bool{}
	for _, e := range edges {
		valid[e.Fact] = e.Valid()
		if e.Fact == "用户住在上海" {
			require.Empty(t, e.SupersededBy)
		}
	}
	require.Equal(t, map[string]bool{"用户住在上海": true}, valid, "退回轮次派生的关系删除，被它取代的关系恢复")
}

// Retrieve 返回实际写入召回块的记录与关系，Sessions 汇总二者的来源会话。
func TestRetrieveReportsSources(t *testing.T) {
	m := newMemory(t, Config{Graph: true})
	g := graphOf(m)
	ctx := WithNamespace(context.Background(), "u")
	ns := []string{"u"}
	for _, e := range []Entity{{ID: "user", Name: "用户"}, {ID: "acme", Name: "Acme"}} {
		e.Namespace, e.Type = ns, "x"
		require.NoError(t, g.PutEntity(ctx, e))
	}
	require.NoError(t, g.PutEdge(ctx, Edge{ID: "e", Namespace: ns, Subject: "user", Predicate: "就职于", Object: "acme", Fact: "用户就职于 Acme", Sources: []Source{{Session: "s3", From: 1}}}))
	_, err := m.Remember(ctx, Record{ID: "r", Content: "用户在 Acme 负责支付", Entities: []string{"Acme"}, Importance: 0.5,
		Sources: []Source{{Session: "s1", From: 1}, {Session: "s2", From: 1}}})
	require.NoError(t, err)

	got, err := m.Retrieve(ctx, "Acme 的工作")
	require.NoError(t, err)
	require.Len(t, got.Records, 1)
	require.Len(t, got.Relations, 1)
	require.Equal(t, []string{"s1", "s2", "s3"}, got.Sessions())
	block, err := m.RecallBlock(ctx, "Acme 的工作")
	require.NoError(t, err)
	require.Equal(t, got.Text, block)

	small := newMemory(t, Config{Durable: m.cfg.Durable, Graph: true, RecallRunes: 10})
	got, err = small.Retrieve(ctx, "Acme 的工作")
	require.NoError(t, err)
	require.Empty(t, got.Records, "超出预算未写入块的记录不报告")
	require.Empty(t, got.Text)
}

// basisOf 写入一条记录作为直接调用 applyRelations 时关系的依据，返回下标 0 指向它的 refs。
func basisOf(t *testing.T, m *Memory, content string) map[int]RecordRef {
	t.Helper()
	r, err := m.Remember(WithNamespace(context.Background(), "u"), Record{Content: content, Importance: 0.5})
	require.NoError(t, err)
	return map[int]RecordRef{0: refOf(r)}
}

// testGraph 在测试中直接读写图谱（不经抽取）。
type testGraph struct{ m *Memory }

func graphOf(m *Memory) testGraph { return testGraph{m} }

func (g testGraph) PutEntity(ctx context.Context, e Entity) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	_, err = g.m.mutate(ctx, ns, "", func(t *tx) error {
		if len(e.Names) == 0 {
			o := Origin{Record: g.basis(t, ns)}
			for _, n := range append([]string{e.Name}, e.Aliases...) {
				addName(t, &e, n, o)
			}
		}
		if old, ok := t.Entities.Get(e.ID); ok {
			e.Incarnation, e.Revision, e.ContentRev = old.Incarnation, old.Revision+1, old.ContentRev+1
		} else {
			e.Incarnation, e.Revision, e.ContentRev = t.incarnation(), 1, 1
		}
		t.Entities.Put(e.ID, e)
		g.m.cascadeTx(t, g.m.now())
		return nil
	})
	return err
}

// basis 返回测试图谱的依据记录，不存在时在 tx 中创建：直接写入的实体与关系以它为来源，遗忘它即撤销全部直接写入的图谱。
func (g testGraph) basis(t *tx, ns []string) RecordRef {
	const id = "graph-basis"
	if r, ok := t.Records.Get(id); ok {
		return refOf(r)
	}
	now := g.m.now()
	createRecord(t, Record{ID: id, Namespace: ns, Kind: KindFact, Content: "图谱依据", Importance: 1, CreatedAt: now, ValidFrom: now})
	r, _ := t.Records.Get(id)
	return refOf(r)
}

func (g testGraph) PutEdge(ctx context.Context, e Edge) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	_, err = g.m.mutate(ctx, ns, "", func(t *tx) error {
		if len(e.Derivations) == 0 {
			ref := g.basis(t, ns)
			for _, s := range append(slices.Clone(e.Sources), Source{})[:max(len(e.Sources), 1)] {
				e.Derivations = append(e.Derivations, Derivation{Origin: Origin{Record: ref, Source: s}, Fact: e.Fact, At: e.ValidFrom})
			}
		}
		if old, ok := t.Edges.Get(e.ID); ok {
			e.Incarnation, e.Revision, e.ContentRev = old.Incarnation, old.Revision+1, old.ContentRev+1
		} else {
			e.Incarnation, e.Revision, e.ContentRev = t.incarnation(), 1, 1
		}
		t.Edges.Put(e.ID, e)
		g.m.cascadeTx(t, g.m.now())
		return nil
	})
	return err
}

func (g testGraph) Entities(ctx context.Context, ns []string) ([]Entity, error) {
	_, s, err := g.m.read(ctx, ns)
	var out []Entity
	for _, id := range slices.Sorted(maps.Keys(s.Entities)) {
		out = append(out, s.Entities[id])
	}
	return out, err
}

func (g testGraph) Edges(ctx context.Context, ns []string) ([]Edge, error) {
	_, s, err := g.m.read(ctx, ns)
	return sortedEdges(s.Edges), err
}

func (g testGraph) Communities(ctx context.Context, ns []string) ([]Community, error) {
	_, s, err := g.m.read(ctx, ns)
	var out []Community
	for _, id := range slices.Sorted(maps.Keys(s.Communities)) {
		out = append(out, s.Communities[id])
	}
	return out, err
}
