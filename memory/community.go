package memory

import (
	"context"
	"fmt"
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
	if m.cfg.Graph == nil || m.cfg.Model == nil {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "社区摘要需要 Graph 与 Model").WithComponent("memory").WithOperation("refresh_communities")
	}
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	return m.syncCommunities(ctx, ns, true)
}

// syncCommunities 使已存的社区与当前的关系一致。summarize 为假时只删除过期的社区（不调用模型），由下一次刷新重建。
func (m *Memory) syncCommunities(ctx context.Context, ns []string, summarize bool) error {
	if m.cfg.Graph == nil || !m.cfg.Communities {
		return nil
	}
	entities, err := m.cfg.Graph.Entities(ctx, ns)
	if err != nil {
		return err
	}
	edges, err := m.cfg.Graph.Edges(ctx, ns)
	if err != nil {
		return err
	}
	existing, err := m.cfg.Graph.Communities(ctx, ns)
	if err != nil {
		return err
	}
	stored := map[string]Community{}
	for _, c := range existing {
		stored[c.ID] = c
	}
	names := map[string]string{}
	for _, e := range entities {
		names[e.ID] = e.Name
	}
	current := map[string]bool{}
	for _, c := range detectCommunities(ns, edges) {
		if old, ok := stored[c.ID]; ok && old.Fingerprint == c.Fingerprint {
			current[c.ID] = true
			continue
		}
		if !summarize {
			continue
		}
		if err := m.summarizeCommunity(ctx, &c, communityEdges(c.Members, edges), names); err != nil {
			return err
		}
		if err := m.cfg.Graph.PutCommunity(ctx, c); err != nil {
			return err
		}
		current[c.ID] = true
	}
	for _, c := range existing {
		if !current[c.ID] {
			if err := m.cfg.Graph.DeleteCommunity(ctx, ns, c.ID); err != nil {
				return err
			}
		}
	}
	return nil
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
