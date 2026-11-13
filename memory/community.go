package memory

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
)

// Community 是一组紧密相关的实体及其摘要（Zep 社区子图）。Fingerprint 由成员与涉及成员的有效关系决定，二者不变时不重写摘要。
type Community struct {
	ID          string    `json:"id"`
	Namespace   []string  `json:"namespace"`
	Members     []string  `json:"members"`
	Name        string    `json:"name"`
	Summary     string    `json:"summary"`
	Fingerprint string    `json:"fingerprint"`
	UpdatedAt   time.Time `json:"updated_at"`
	Incarnation int64     `json:"incarnation,omitzero"`
	Revision    int64     `json:"revision,omitzero"`
	ContentRev  int64     `json:"content_rev,omitzero"`
}

const communityPrompt = "以下是知识图谱中一组相互关联的实体之间的关系，每行一条，带生效日期。" +
	"写一段信息密集的摘要，保留重要的名称、角色、地点、日期与数量，不超过 300 字；再给出一个说明这组信息是关于什么的简短标题。" +
	`输出 JSON：{"name":"…","summary":"…"}。只输出 JSON。`

// detectCommunities 用标签传播（Graphiti）在有效关系上划分社区，返回成员不少于 CommunityMinSize 的社区（不含摘要）。
// 有效关系数超过 CommunityHubDegree 的枢纽实体（如"用户"）不参与传播，否则所有实体都会被它连成一个社区；
// 枢纽与社区成员之间的关系仍计入该社区的事实。
func detectCommunities(ns []string, edges []Edge) []Community {
	degree := map[string]int{}
	for _, e := range edges {
		if e.Valid() {
			degree[e.Subject]++
			degree[e.Object]++
		}
	}
	hub := func(id string) bool { return degree[id] > constants.DefaultMemoryCommunityHubDegree }
	neighbors := map[string]map[string]int{}
	for _, e := range edges {
		if !e.Valid() || e.Subject == e.Object || hub(e.Subject) || hub(e.Object) {
			continue
		}
		for _, pair := range [][2]string{{e.Subject, e.Object}, {e.Object, e.Subject}} {
			if neighbors[pair[0]] == nil {
				neighbors[pair[0]] = map[string]int{}
			}
			neighbors[pair[0]][pair[1]]++
		}
	}
	nodes := make([]string, 0, len(neighbors))
	label := map[string]string{}
	for id := range neighbors {
		nodes = append(nodes, id)
		label[id] = id
	}
	slices.Sort(nodes)
	// 异步更新：每个节点取邻居中权重最大的标签，并列时保留当前标签，否则取字典序最小者，保证结果确定且收敛。
	for range constants.MemoryLabelPropagationRounds {
		changed := false
		for _, id := range nodes {
			votes := map[string]int{}
			for n, w := range neighbors[id] {
				votes[label[n]] += w
			}
			top := 0
			for _, v := range votes {
				top = max(top, v)
			}
			best := label[id]
			if votes[best] < top {
				best = ""
				for l, v := range votes {
					if v == top && (best == "" || l < best) {
						best = l
					}
				}
			}
			if best != label[id] {
				label[id], changed = best, true
			}
		}
		if !changed {
			break
		}
	}
	groups := map[string][]string{}
	for _, id := range nodes {
		groups[label[id]] = append(groups[label[id]], id)
	}
	var out []Community
	for _, members := range groups {
		if len(members) < constants.DefaultMemoryCommunityMinSize {
			continue
		}
		slices.Sort(members)
		facts := communityEdges(members, edges)
		parts := append(slices.Clone(members), "\x01")
		for _, e := range facts {
			parts = append(parts, e.ID)
		}
		out = append(out, Community{ID: stableID(append(append(slices.Clone(ns), "community"), members...)...), Namespace: ns, Members: members, Fingerprint: stableID(parts...)})
	}
	slices.SortFunc(out, func(a, b Community) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// communityEdges 返回至少一端在 members 中的有效关系，按 ID 排序。
func communityEdges(members []string, edges []Edge) []Edge {
	var out []Edge
	for _, e := range edges {
		if e.Valid() && (slices.Contains(members, e.Subject) || slices.Contains(members, e.Object)) {
			out = append(out, e)
		}
	}
	return out
}

// RefreshCommunities 重新划分当前命名空间的社区：成员与关系都没变的社区保留原摘要，其余由模型重写摘要，消失的社区被删除。
// 启用 Config.Communities 时 Worker 在每个任务后调用。
func (m *Memory) RefreshCommunities(ctx context.Context) error {
	if !m.cfg.Graph || m.cfg.Model == nil {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "社区摘要需要 Graph 与 Model").WithComponent("memory").WithOperation("refresh_communities")
	}
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	return m.syncCommunities(ctx, ns)
}

// summarized 是锁外写好摘要的社区，以及写摘要时依据的关系版本（实例号, ContentRev）。
type summarized struct {
	c     Community
	basis map[string][2]int64
}

// syncCommunities 使已存的社区与当前的关系一致：读取后在锁外为变化的社区写摘要，再在 mutate 内按锁内的关系复核并写入。
func (m *Memory) syncCommunities(ctx context.Context, ns []string) error {
	if !m.cfg.Graph || !m.cfg.Communities {
		return nil
	}
	scope, s, err := m.read(ctx, ns)
	if err == nil {
		err = checkJobScope(ctx, scope)
	}
	if err != nil {
		return err
	}
	edges := sortedEdges(s.Edges)
	names := map[string]string{}
	for id, e := range s.Entities {
		names[id] = e.Name
	}
	current := map[string]bool{}
	var fresh []summarized
	for _, c := range detectCommunities(ns, edges) {
		current[c.ID] = true
		if old, ok := s.Communities[c.ID]; ok && old.Fingerprint == c.Fingerprint {
			continue
		}
		facts := communityEdges(c.Members, edges)
		basis := make(map[string][2]int64, len(facts))
		for _, e := range facts {
			basis[e.ID] = [2]int64{e.Incarnation, e.ContentRev}
		}
		if err := m.summarizeCommunity(ctx, &c, facts, names); err != nil {
			return err
		}
		fresh = append(fresh, summarized{c: c, basis: basis})
	}
	stale := false
	for id := range s.Communities {
		stale = stale || !current[id]
	}
	if len(fresh) == 0 && !stale {
		return nil
	}
	_, err = m.mutate(ctx, ns, scope, func(t *tx) error {
		syncCommunitiesTx(t, ns, fresh)
		return nil
	})
	return err
}

// syncCommunitiesTx 按锁内的关系重新划分社区：已存社区的指纹与之相同的保留；fresh 中划分与关系版本都与锁内相同的写入；
// 其余已存社区删除，由下一次刷新重写摘要。fresh 为空时只删除过期的社区（Rewound 使用）。
func syncCommunitiesTx(t *tx, ns []string, fresh []summarized) {
	edges := make([]Edge, 0, t.Edges.Len())
	byID := map[string]Edge{}
	for _, k := range t.Edges.Keys() {
		e, _ := t.Edges.Get(k)
		edges = append(edges, e)
		byID[k] = e
	}
	current := map[string]Community{}
	for _, c := range detectCommunities(ns, edges) {
		current[c.ID] = c
	}
	write := map[string]Community{}
	for _, f := range fresh {
		cur, ok := current[f.c.ID]
		if !ok || cur.Fingerprint != f.c.Fingerprint {
			continue
		}
		same := true
		for id, v := range f.basis {
			e := byID[id]
			same = same && e.Incarnation == v[0] && e.ContentRev == v[1]
		}
		if same {
			write[f.c.ID] = f.c
		}
	}
	for _, id := range t.Communities.Keys() {
		c, _ := t.Communities.Get(id)
		if _, ok := write[id]; ok {
			continue
		}
		if cur, ok := current[id]; !ok || cur.Fingerprint != c.Fingerprint {
			t.Communities.Delete(id)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(write)) {
		c := write[id]
		if prev, ok := t.Communities.Get(id); ok {
			c.Incarnation, c.Revision, c.ContentRev = prev.Incarnation, prev.Revision+1, prev.ContentRev+1
		} else {
			c.Incarnation, c.Revision, c.ContentRev = t.incarnation(), 1, 1
		}
		t.Communities.Put(id, c)
	}
}

// sortedEdges 返回按 ID 排序的关系。
func sortedEdges(m map[string]Edge) []Edge {
	out := make([]Edge, 0, len(m))
	for _, id := range slices.Sorted(maps.Keys(m)) {
		out = append(out, m[id])
	}
	return out
}

// summarizeCommunity 以社区中最近的 CommunityFacts 条关系请求模型写摘要。
func (m *Memory) summarizeCommunity(ctx context.Context, c *Community, facts []Edge, names map[string]string) error {
	slices.SortFunc(facts, func(a, b Edge) int {
		if x := b.ValidFrom.Compare(a.ValidFrom); x != 0 {
			return x
		}
		return strings.Compare(a.ID, b.ID)
	})
	var sb strings.Builder
	for _, e := range facts[:min(len(facts), constants.DefaultMemoryCommunityFacts)] {
		fmt.Fprintf(&sb, "- [%s] %s\n", e.ValidFrom.Format(time.DateOnly), e.Fact)
	}
	var out struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}
	if err := m.chatJSON(ctx, communityPrompt, sb.String(), &out); err != nil {
		return err
	}
	c.Name, c.Summary, c.UpdatedAt = strings.TrimSpace(out.Name), strings.TrimSpace(out.Summary), m.now()
	if c.Name == "" {
		members := make([]string, 0, len(c.Members))
		for _, id := range c.Members {
			members = append(members, names[id])
		}
		c.Name = strings.Join(members[:min(len(members), 3)], "、")
	}
	return nil
}
