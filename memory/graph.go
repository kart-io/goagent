package memory

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
)

// Entity 是图中的实体节点，按命名空间、类型与规范化名称去重；Aliases 记录指向同一实体的其他名称。
// 配置了 Embedder 时 Vector 是名称的单位向量，用于找出名称不同的候选重复实体。
type Entity struct {
	ID         string    `json:"id"`
	Namespace  []string  `json:"namespace"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Aliases    []string  `json:"aliases,omitempty"`
	Vector     []float64 `json:"vector,omitempty"`
	EmbedModel string    `json:"embed_model,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Edge 是带有效期的关系（Zep 时序图）：Subject、Object 为实体 ID，Fact 是可读的陈述。
// 与新事实矛盾的边写入 InvalidAt 并以 SupersededBy 指向新边，不删除，因而可追溯某一时刻成立的关系。
type Edge struct {
	ID           string    `json:"id"`
	Namespace    []string  `json:"namespace"`
	Subject      string    `json:"subject"`
	Predicate    string    `json:"predicate"`
	Object       string    `json:"object"`
	Fact         string    `json:"fact"`
	ValidFrom    time.Time `json:"valid_from"`
	InvalidAt    time.Time `json:"invalid_at,omitzero"`
	SupersededBy string    `json:"superseded_by,omitempty"`
	Source       Source    `json:"source,omitzero"`
	CreatedAt    time.Time `json:"created_at"`
}

// Valid 报告关系是否仍然成立。
func (e Edge) Valid() bool { return e.InvalidAt.IsZero() }

// ValidAt 报告关系在 t 时是否成立。
func (e Edge) ValidAt(t time.Time) bool {
	return !t.Before(e.ValidFrom) && (e.InvalidAt.IsZero() || t.Before(e.InvalidAt))
}

// Graph 保存实体、关系与社区。
type Graph interface {
	PutEntity(ctx context.Context, e Entity) error
	PutEdge(ctx context.Context, e Edge) error
	PutCommunity(ctx context.Context, c Community) error
	Entities(ctx context.Context, namespace []string) ([]Entity, error)
	Edges(ctx context.Context, namespace []string) ([]Edge, error)
	Communities(ctx context.Context, namespace []string) ([]Community, error)
	DeleteEdge(ctx context.Context, namespace []string, id string) error
	DeleteCommunity(ctx context.Context, namespace []string, id string) error
	Clear(ctx context.Context, namespace []string) error
}

// NewGraph 把实体、关系与社区以 JSON 文本存入 st 的 {"memory","entities"|"edges"|"communities",命名空间...}。
func NewGraph(st store.Store) Graph { return &storeGraph{st: st} }

type storeGraph struct{ st store.Store }

func entitiesNS(ns []string) []string    { return append([]string{"memory", "entities"}, ns...) }
func edgesNS(ns []string) []string       { return append([]string{"memory", "edges"}, ns...) }
func communitiesNS(ns []string) []string { return append([]string{"memory", "communities"}, ns...) }

func (g *storeGraph) PutEntity(ctx context.Context, e Entity) error {
	if e.ID == "" || strings.TrimSpace(e.Name) == "" {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "实体需要 ID 与名称").WithComponent("memory").WithOperation("put_entity")
	}
	if err := validNamespace(e.Namespace); err != nil {
		return err
	}
	return putJSON(ctx, g.st, entitiesNS(e.Namespace), e.ID, e)
}

func (g *storeGraph) PutEdge(ctx context.Context, e Edge) error {
	if e.ID == "" || e.Subject == "" || e.Object == "" || strings.TrimSpace(e.Predicate) == "" {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "关系需要 ID、主体、谓词与客体").WithComponent("memory").WithOperation("put_edge")
	}
	if err := validNamespace(e.Namespace); err != nil {
		return err
	}
	return putJSON(ctx, g.st, edgesNS(e.Namespace), e.ID, e)
}

func (g *storeGraph) PutCommunity(ctx context.Context, c Community) error {
	if c.ID == "" || len(c.Members) == 0 {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "社区需要 ID 与成员").WithComponent("memory").WithOperation("put_community")
	}
	if err := validNamespace(c.Namespace); err != nil {
		return err
	}
	return putJSON(ctx, g.st, communitiesNS(c.Namespace), c.ID, c)
}

func (g *storeGraph) Entities(ctx context.Context, ns []string) ([]Entity, error) {
	out, err := listJSON[Entity](ctx, g.st, entitiesNS(ns))
	slices.SortFunc(out, func(a, b Entity) int { return strings.Compare(a.ID, b.ID) })
	return out, err
}

func (g *storeGraph) Edges(ctx context.Context, ns []string) ([]Edge, error) {
	out, err := listJSON[Edge](ctx, g.st, edgesNS(ns))
	slices.SortFunc(out, func(a, b Edge) int { return strings.Compare(a.ID, b.ID) })
	return out, err
}

func (g *storeGraph) Communities(ctx context.Context, ns []string) ([]Community, error) {
	out, err := listJSON[Community](ctx, g.st, communitiesNS(ns))
	slices.SortFunc(out, func(a, b Community) int { return strings.Compare(a.ID, b.ID) })
	return out, err
}

func (g *storeGraph) DeleteEdge(ctx context.Context, ns []string, id string) error {
	if err := g.st.Delete(ctx, edgesNS(ns), id); err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

func (g *storeGraph) DeleteCommunity(ctx context.Context, ns []string, id string) error {
	if err := g.st.Delete(ctx, communitiesNS(ns), id); err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

func (g *storeGraph) Clear(ctx context.Context, ns []string) error {
	for _, space := range [][]string{entitiesNS(ns), edgesNS(ns), communitiesNS(ns)} {
		if err := g.st.Clear(ctx, space); err != nil {
			return err
		}
	}
	return nil
}

func putJSON(ctx context.Context, st store.Store, ns []string, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return st.Put(ctx, ns, key, string(b))
}

func listJSON[T any](ctx context.Context, st store.Store, ns []string) ([]T, error) {
	values, err := st.Search(ctx, ns, nil)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(values))
	for _, v := range values {
		text, ok := v.Value.(string)
		if !ok {
			return nil, agentErrors.Newf(agentErrors.CodeStoreSerialization, "记忆图数据不是 JSON 文本").WithComponent("memory").WithOperation("decode")
		}
		var item T
		if err := json.Unmarshal([]byte(text), &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// normalizeName 用于实体去重：小写、去首尾空白并合并内部空白。
func normalizeName(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// entityRef 是抽取结果中的实体。
type entityRef struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Aliases []string `json:"aliases"`
}

// key 标识一轮抽取中的同一实体引用。
func (r entityRef) key() string { return normalizeName(r.Type) + "\x00" + normalizeName(r.Name) }

func (r entityRef) names() []string { return normalizedNames(r.Name, r.Aliases) }

func (e Entity) names() []string { return normalizedNames(e.Name, e.Aliases) }

// normalizedNames 返回名称与别名去重后的规范化形式。
func normalizedNames(name string, aliases []string) []string {
	var out []string
	for _, n := range append([]string{name}, aliases...) {
		if k := normalizeName(n); k != "" && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

func intersects(a, b []string) bool {
	return slices.ContainsFunc(a, func(x string) bool { return slices.Contains(b, x) })
}

// mergeAliases 把实体尚未有的名称加入别名，报告是否有变化。
func (e *Entity) mergeAliases(names ...string) bool {
	have, changed := e.names(), false
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" && !slices.Contains(have, normalizeName(n)) {
			e.Aliases, have, changed = append(e.Aliases, n), append(have, normalizeName(n)), true
		}
	}
	return changed
}

// relation 是抽取结果中的关系；Exclusive 表示主体在该谓词上只能有一个客体（如"住在"），新客体使旧关系失效。
type relation struct {
	Subject   entityRef `json:"subject"`
	Predicate string    `json:"predicate"`
	Object    entityRef `json:"object"`
	Fact      string    `json:"fact"`
	Exclusive bool      `json:"exclusive"`
}

const resolvePrompt = "你维护知识图谱中的实体。对每个新实体，判断它是否与某个候选实体指同一个现实对象（同一对象的不同叫法、简称、翻译或拼写），输出 JSON：" +
	`{"resolutions":[{"index":0,"duplicate_of":"候选实体的 id；不是同一对象时为空串"}]}。` +
	"只在确定时给出 id；相关但不同的对象（如同名的两个人、母公司与子公司）不是同一对象。只输出 JSON。"

// unresolved 是一轮中按名称匹配不到已有实体的引用；名称或别名相交的引用合并为一个。
type unresolved struct {
	ref        entityRef
	keys       []string
	vector     []float64
	candidates []Entity
}

// resolveEntities 把一轮抽取的实体引用解析为实体（Graphiti 式判重）：先按类型与规范化名称（含别名）精确匹配；
// 匹配不到的，取同类型中名称向量或字符对相似度不低于 EntitySimilarity 的实体作为候选，由模型一次判定是否为同一对象；
// 仍无匹配的新建。返回引用 key 到实体的映射。
func (m *Memory) resolveEntities(ctx context.Context, ns []string, known []Entity, refs []entityRef) (map[string]Entity, error) {
	resolved := map[string]Entity{}
	var pending []*unresolved
	for _, ref := range refs {
		ref.Name, ref.Type = strings.TrimSpace(ref.Name), normalizeName(ref.Type)
		if _, ok := resolved[ref.key()]; ok {
			continue
		}
		if i := slices.IndexFunc(known, func(e Entity) bool { return e.Type == ref.Type && intersects(e.names(), ref.names()) }); i >= 0 {
			if known[i].mergeAliases(append([]string{ref.Name}, ref.Aliases...)...) {
				if err := m.cfg.Graph.PutEntity(ctx, known[i]); err != nil {
					return nil, err
				}
			}
			resolved[ref.key()] = known[i]
			continue
		}
		if i := slices.IndexFunc(pending, func(p *unresolved) bool { return p.ref.Type == ref.Type && intersects(p.ref.names(), ref.names()) }); i >= 0 {
			pending[i].ref.Aliases = append(pending[i].ref.Aliases, append([]string{ref.Name}, ref.Aliases...)...)
			pending[i].keys = append(pending[i].keys, ref.key())
			continue
		}
		pending = append(pending, &unresolved{ref: ref, keys: []string{ref.key()}})
	}
	if err := m.entityCandidates(ctx, known, pending); err != nil {
		return nil, err
	}
	duplicates, err := m.judgeDuplicates(ctx, pending)
	if err != nil {
		return nil, err
	}
	for i, p := range pending {
		var e Entity
		if target, ok := duplicates[i]; ok {
			k := slices.IndexFunc(known, func(e Entity) bool { return e.ID == target })
			known[k].mergeAliases(append([]string{p.ref.Name}, p.ref.Aliases...)...)
			e = known[k]
		} else {
			e = Entity{ID: stableID(append(slices.Clone(ns), p.ref.Type, normalizeName(p.ref.Name))...), Namespace: ns, Name: p.ref.Name, Type: p.ref.Type, CreatedAt: m.now()}
			e.mergeAliases(p.ref.Aliases...)
			if p.vector != nil {
				e.Vector, e.EmbedModel = p.vector, m.cfg.EmbedModel
			}
			known = append(known, e)
		}
		if err := m.cfg.Graph.PutEntity(ctx, e); err != nil {
			return nil, err
		}
		for _, key := range p.keys {
			resolved[key] = e
		}
	}
	return resolved, nil
}

// entityCandidates 为每个未匹配的引用找同类型中最相似的至多 EntityCandidates 个实体：
// 相似度取名称向量余弦（需 Embedder）与名称字符对重合的较大者。翻译或别称（如"Acme"与"爱克米"）只能靠向量找到。
func (m *Memory) entityCandidates(ctx context.Context, known []Entity, pending []*unresolved) error {
	for _, p := range pending {
		if m.cfg.Embedder != nil {
			raw, err := m.cfg.Embedder.Embed(ctx, p.ref.Name)
			if err != nil {
				return fmt.Errorf("生成实体名称向量：%w", err)
			}
			if p.vector, err = normalize(raw); err != nil {
				return err
			}
		}
		grams := bigrams(strings.Join(p.ref.names(), " "))
		type scored struct {
			e     Entity
			score float64
		}
		var cands []scored
		for _, e := range known {
			if e.Type != p.ref.Type {
				continue
			}
			s := overlap(grams, bigrams(strings.Join(e.names(), " ")))
			if p.vector != nil && e.EmbedModel == m.cfg.EmbedModel && len(e.Vector) == len(p.vector) {
				s = max(s, dot(p.vector, e.Vector))
			}
			if s >= constants.DefaultMemoryEntitySimilarity {
				cands = append(cands, scored{e, s})
			}
		}
		slices.SortFunc(cands, func(a, b scored) int {
			if a.score != b.score {
				if a.score > b.score {
					return -1
				}
				return 1
			}
			return strings.Compare(a.e.ID, b.e.ID)
		})
		for _, c := range cands[:min(len(cands), constants.DefaultMemoryEntityCandidates)] {
			p.candidates = append(p.candidates, c.e)
		}
	}
	return nil
}

// judgeDuplicates 一次请求模型判定全部有候选的引用，返回下标到重复实体 ID 的映射；只接受该引用自己的候选，模型编造的 ID 视为不重复。
func (m *Memory) judgeDuplicates(ctx context.Context, pending []*unresolved) (map[int]string, error) {
	var sb strings.Builder
	for i, p := range pending {
		if len(p.candidates) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "新实体 [%d] 名称：%s；类型：%s；别名：%s\n候选：\n", i, p.ref.Name, p.ref.Type, strings.Join(p.ref.Aliases, "、"))
		for _, c := range p.candidates {
			fmt.Fprintf(&sb, "- id=%s 名称：%s；别名：%s\n", c.ID, c.Name, strings.Join(c.Aliases, "、"))
		}
	}
	out := map[int]string{}
	if sb.Len() == 0 {
		return out, nil
	}
	var reply struct {
		Resolutions []struct {
			Index       int    `json:"index"`
			DuplicateOf string `json:"duplicate_of"`
		} `json:"resolutions"`
	}
	if err := m.chatJSON(ctx, resolvePrompt, sb.String(), &reply); err != nil {
		return nil, err
	}
	for _, r := range reply.Resolutions {
		if r.Index >= 0 && r.Index < len(pending) && slices.ContainsFunc(pending[r.Index].candidates, func(e Entity) bool { return e.ID == r.DuplicateOf }) {
			out[r.Index] = r.DuplicateOf
		}
	}
	return out, nil
}

func dot(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// applyRelations 写入一轮抽取出的关系，at 是该轮发生的时间。相同的有效关系不重复写入。
// 排他谓词上的其他有效关系在 at 失效并指向新关系；已有关系比新关系生效更晚时（迟到的旧事实），新关系写入即失效并指向已有关系。
func (m *Memory) applyRelations(ctx context.Context, j Job, at time.Time, rels []relation) error {
	rels = slices.DeleteFunc(slices.Clone(rels), func(r relation) bool {
		return strings.TrimSpace(r.Predicate) == "" || strings.TrimSpace(r.Subject.Name) == "" || strings.TrimSpace(r.Object.Name) == ""
	})
	if m.cfg.Graph == nil || len(rels) == 0 {
		return nil
	}
	ns := j.Namespace
	known, err := m.cfg.Graph.Entities(ctx, ns)
	if err != nil {
		return err
	}
	edges, err := m.cfg.Graph.Edges(ctx, ns)
	if err != nil {
		return err
	}
	var refs []entityRef
	for _, r := range rels {
		refs = append(refs, r.Subject, r.Object)
	}
	entities, err := m.resolveEntities(ctx, ns, known, refs)
	if err != nil {
		return err
	}
	for i, r := range rels {
		pred := strings.TrimSpace(r.Predicate)
		subj, obj := entities[r.Subject.key()], entities[r.Object.key()]
		id := stableID(j.Key, "relation", strconv.Itoa(i))
		if slices.ContainsFunc(edges, func(e Edge) bool {
			return e.Valid() && e.ID != id && e.Subject == subj.ID && e.Predicate == pred && e.Object == obj.ID
		}) {
			continue
		}
		fact := strings.TrimSpace(r.Fact)
		if fact == "" {
			fact = subj.Name + " " + pred + " " + obj.Name
		}
		edge := Edge{ID: id, Namespace: ns, Subject: subj.ID, Predicate: pred, Object: obj.ID, Fact: fact, ValidFrom: at, CreatedAt: m.now(),
			Source: j.source()}
		var superseded []int
		for k, old := range edges {
			if !r.Exclusive || !old.Valid() || old.ID == id || old.Subject != subj.ID || old.Predicate != pred || old.Object == obj.ID {
				continue
			}
			if old.ValidFrom.After(at) {
				edge.InvalidAt, edge.SupersededBy = old.ValidFrom, old.ID
				continue
			}
			superseded = append(superseded, k)
		}
		if err := m.cfg.Graph.PutEdge(ctx, edge); err != nil {
			return err
		}
		for _, k := range superseded {
			edges[k].InvalidAt, edges[k].SupersededBy = at, id
			if err := m.cfg.Graph.PutEdge(ctx, edges[k]); err != nil {
				return err
			}
		}
		edges = append(slices.DeleteFunc(edges, func(e Edge) bool { return e.ID == id }), edge)
	}
	return nil
}
