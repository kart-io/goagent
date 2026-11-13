package memory

import (
	"context"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/kart-io/goagent/constants"
)

// Relation 是召回的一条关系及其两端实体的名称。
type Relation struct {
	Edge
	SubjectName, ObjectName string
	// Hops 是距离种子实体的跳数，从 1 起。
	Hops int
	// Score 是两端实体的 Personalized PageRank 得分之和。
	Score float64
}

// GraphRecall 是一次图谱召回的结果。
type GraphRecall struct {
	Relations []Relation
	// Communities 是包含高分实体的社区（需启用 Config.Communities），按成员得分之和排序。
	Communities []Community
}

// Related 从种子实体出发召回关系（HippoRAG）：取距种子至多 hops 跳的有效关系构成的子图，
// 以种子为重启分布运行 Personalized PageRank，按两端实体得分之和返回至多 limit 条关系，以及得分最高的社区。
// 种子为名称或别名出现在 text 中的实体，以及 names 中的实体；种子权重为其有效关系数的倒数（节点特异性），
// 避免"用户"这类与几乎所有关系相连的实体主导排序。
func (m *Memory) Related(ctx context.Context, text string, names []string, hops, limit int) (GraphRecall, error) {
	if !m.cfg.Graph || limit <= 0 || hops <= 0 {
		return GraphRecall{}, nil
	}
	ns, err := requireNamespace(ctx)
	if err != nil {
		return GraphRecall{}, err
	}
	_, s, err := m.read(ctx, ns)
	if err != nil {
		return GraphRecall{}, err
	}
	return m.relatedOf(s, text, names, hops, limit), nil
}

// relatedOf 是 Related 在一份 state 上的计算；返回的关系与社区已复制。
func (m *Memory) relatedOf(s state, text string, names []string, hops, limit int) GraphRecall {
	if !m.cfg.Graph || limit <= 0 || hops <= 0 {
		return GraphRecall{}
	}
	edges := sortedEdges(s.Edges)
	edges = slices.DeleteFunc(edges, func(e Edge) bool { return !e.Valid() })
	byID := map[string]Entity{}
	degree := map[string]int{}
	for _, e := range edges {
		degree[e.Subject]++
		degree[e.Object]++
	}
	lowered := strings.ToLower(text)
	wanted := map[string]bool{}
	for _, n := range names {
		wanted[normalizeName(n)] = true
	}
	dist := map[string]int{}
	reset := map[string]float64{}
	for _, id := range slices.Sorted(maps.Keys(s.Entities)) {
		e := s.Entities[id]
		byID[e.ID] = e
		if degree[e.ID] == 0 {
			continue
		}
		for _, n := range e.names() {
			if wanted[n] || strings.Contains(lowered, n) {
				dist[e.ID], reset[e.ID] = 0, 1/float64(degree[e.ID])
			}
		}
	}
	// 按层扩展，记录每条关系第一次被到达时的跳数。
	reached := map[string]int{}
	for hop := 1; hop <= hops; hop++ {
		for _, e := range edges {
			if _, ok := reached[e.ID]; ok {
				continue
			}
			ds, okS := dist[e.Subject]
			do, okO := dist[e.Object]
			if (okS && ds == hop-1) || (okO && do == hop-1) {
				reached[e.ID] = hop
			}
		}
		for _, e := range edges {
			if reached[e.ID] != hop {
				continue
			}
			for _, id := range []string{e.Subject, e.Object} {
				if _, ok := dist[id]; !ok {
					dist[id] = hop
				}
			}
		}
	}
	var sub []Edge
	for _, e := range edges {
		if _, ok := reached[e.ID]; ok {
			sub = append(sub, e)
		}
	}
	scores := personalizedPageRank(sub, reset)
	out := make([]Relation, 0, len(sub))
	for _, e := range sub {
		out = append(out, Relation{Edge: edgeKind.clone(e), SubjectName: byID[e.Subject].Name, ObjectName: byID[e.Object].Name, Hops: reached[e.ID], Score: scores[e.Subject] + scores[e.Object]})
	}
	slices.SortFunc(out, func(a, b Relation) int {
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		if c := b.ValidFrom.Compare(a.ValidFrom); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	result := GraphRecall{Relations: out[:min(limit, len(out))]}
	if !m.cfg.Communities || len(scores) == 0 {
		return result
	}
	var communities []Community
	for _, id := range slices.Sorted(maps.Keys(s.Communities)) {
		communities = append(communities, s.Communities[id])
	}
	weight := map[string]float64{}
	for _, c := range communities {
		for _, id := range c.Members {
			weight[c.ID] += scores[id]
		}
	}
	communities = slices.DeleteFunc(communities, func(c Community) bool { return weight[c.ID] == 0 })
	slices.SortStableFunc(communities, func(a, b Community) int {
		switch {
		case weight[a.ID] > weight[b.ID]:
			return -1
		case weight[a.ID] < weight[b.ID]:
			return 1
		}
		return 0
	})
	for _, c := range communities[:min(m.cfg.GraphCommunities, len(communities))] {
		result.Communities = append(result.Communities, communityKind.clone(c))
	}
	return result
}

// personalizedPageRank 在以 edges 为无向边（两点间的多条边各计一次）的图上计算 Personalized PageRank：
// 每步以 Damping 的概率沿边游走，否则按 reset 重启（HippoRAG 取 0.5）。图中每个节点至少有一条边，没有悬挂节点。
func personalizedPageRank(edges []Edge, reset map[string]float64) map[string]float64 {
	index := map[string]int{}
	var nodes []string
	var adj [][]int
	add := func(id string) int {
		i, ok := index[id]
		if !ok {
			i = len(nodes)
			index[id] = i
			nodes, adj = append(nodes, id), append(adj, nil)
		}
		return i
	}
	for _, e := range edges {
		s, o := add(e.Subject), add(e.Object)
		adj[s], adj[o] = append(adj[s], o), append(adj[o], s)
	}
	restart := make([]float64, len(nodes))
	total := 0.0
	for id, w := range reset {
		if i, ok := index[id]; ok && w > 0 {
			restart[i] = w
			total += w
		}
	}
	if total == 0 {
		return nil
	}
	for i := range restart {
		restart[i] /= total
	}
	d := constants.DefaultMemoryPageRankDamping
	p := slices.Clone(restart)
	for range constants.MemoryPageRankIterations {
		next := make([]float64, len(nodes))
		for i, pi := range p {
			share := d * pi / float64(len(adj[i]))
			for _, j := range adj[i] {
				next[j] += share
			}
		}
		delta := 0.0
		for i := range next {
			next[i] += (1 - d) * restart[i]
			delta += math.Abs(next[i] - p[i])
		}
		p = next
		if delta < constants.MemoryPageRankTolerance {
			break
		}
	}
	out := make(map[string]float64, len(nodes))
	for i, id := range nodes {
		out[id] = p[i]
	}
	return out
}
