package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storemem "github.com/kart-io/goagent/store/memory"
)

func TestCommunitiesRequireGraph(t *testing.T) {
	_, err := New(Config{Store: storemem.New(), Communities: true})
	require.Error(t, err)
}

// 社区按标签传播划分；成员与关系不变时不重写摘要；关系变化只重写所在社区；退回的关系使社区摘要被删除，下一次刷新重建。
func TestCommunitiesLifecycle(t *testing.T) {
	st := storemem.New()
	g := NewGraph(st)
	var summarized []string
	model := &scriptedModel{reply: func(system, user string) (string, error) {
		require.Equal(t, communityPrompt, system)
		summarized = append(summarized, user)
		name := "咖啡"
		if strings.Contains(user, "Acme") {
			name = "工作"
		}
		return `{"name":"` + name + `","summary":"摘要：` + strings.ReplaceAll(strings.TrimSpace(user), "\n", "；") + `"}`, nil
	}}
	m := newMemory(t, Config{Store: st, Graph: g, Model: model, Communities: true})
	ctx := WithNamespace(context.Background(), "u")
	ns := []string{"u"}
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, e := range []Entity{{ID: "acme", Name: "Acme"}, {ID: "sz", Name: "深圳"}, {ID: "zs", Name: "张三"},
		{ID: "coffee", Name: "咖啡"}, {ID: "latte", Name: "拿铁"}, {ID: "yn", Name: "云南"}, {ID: "a", Name: "孤立甲"}, {ID: "b", Name: "孤立乙"}} {
		e.Namespace, e.Type = ns, "x"
		require.NoError(t, g.PutEntity(ctx, e))
	}
	edge := func(id, s, o, fact string) Edge {
		return Edge{ID: id, Namespace: ns, Subject: s, Predicate: "关联", Object: o, Fact: fact, ValidFrom: day}
	}
	for _, e := range []Edge{edge("1", "acme", "sz", "Acme 位于深圳"), edge("2", "zs", "acme", "张三就职于 Acme"),
		edge("3", "latte", "coffee", "拿铁是一种咖啡"), edge("4", "coffee", "yn", "咖啡豆产自云南"), edge("5", "a", "b", "只有两个成员")} {
		require.NoError(t, g.PutEdge(ctx, e))
	}

	require.NoError(t, m.RefreshCommunities(ctx))
	communities, err := g.Communities(ctx, ns)
	require.NoError(t, err)
	var members [][]string
	for _, c := range communities {
		members = append(members, c.Members)
		require.NotEmpty(t, c.Summary)
	}
	require.ElementsMatch(t, [][]string{{"acme", "sz", "zs"}, {"coffee", "latte", "yn"}}, members, "少于三个成员的不成为社区")
	require.Len(t, summarized, 2)

	require.NoError(t, m.RefreshCommunities(ctx))
	require.Len(t, summarized, 2, "没有变化时不重写摘要")

	added := edge("6", "sz", "zs", "张三住在深圳")
	added.Source = Source{Session: "s", From: 10, To: 12}
	require.NoError(t, g.PutEdge(ctx, added))
	require.NoError(t, m.RefreshCommunities(ctx))
	require.Len(t, summarized, 3, "只重写关系变化的社区")
	require.Contains(t, summarized[2], "张三住在深圳")

	got, err := m.Related(ctx, "Acme 的情况", nil, 2, 5)
	require.NoError(t, err)
	require.Len(t, got.Communities, 1, "只返回包含高分实体的社区")
	require.Equal(t, "工作", got.Communities[0].Name)
	block, err := m.RecallBlock(ctx, "Acme 的情况")
	require.NoError(t, err)
	require.Contains(t, block, "[主题 工作] 摘要：")

	require.NoError(t, m.Rewound(ctx, "s", 9))
	communities, err = g.Communities(ctx, ns)
	require.NoError(t, err)
	require.Len(t, communities, 1, "摘要包含退回的关系的社区被删除")
	require.Len(t, summarized, 3, "退回时不请求模型")
	require.NoError(t, m.RefreshCommunities(ctx))
	communities, err = g.Communities(ctx, ns)
	require.NoError(t, err)
	require.Len(t, communities, 2)
	require.NotContains(t, summarized[3], "张三住在深圳")

	require.NoError(t, m.Purge(ctx))
	communities, err = g.Communities(ctx, ns)
	require.NoError(t, err)
	require.Empty(t, communities)
}

// 枢纽实体不参与划分：否则与"用户"相连的所有实体会成为一个社区。
func TestDetectCommunitiesSkipsHub(t *testing.T) {
	var edges []Edge
	add := func(s, o string) { edges = append(edges, Edge{ID: s + o, Subject: s, Object: o, Predicate: "p"}) }
	for _, leaf := range []string{"a1", "a2", "a3", "b1", "b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3", "e1"} {
		add("user", leaf)
	}
	for _, g := range []string{"a", "b"} {
		add(g+"1", g+"2")
		add(g+"2", g+"3")
	}
	var members [][]string
	for _, c := range detectCommunities([]string{"u"}, edges) {
		members = append(members, c.Members)
	}
	require.ElementsMatch(t, [][]string{{"a1", "a2", "a3"}, {"b1", "b2", "b3"}}, members)
}
