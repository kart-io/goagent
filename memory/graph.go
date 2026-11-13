package memory

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kart-io/goagent/constants"
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
	// Incarnation、Revision、ContentRev 的含义与 Record 相同。
	Incarnation int64 `json:"incarnation,omitzero"`
	Revision    int64 `json:"revision,omitzero"`
	ContentRev  int64 `json:"content_rev,omitzero"`
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
	Incarnation  int64     `json:"incarnation,omitzero"`
	Revision     int64     `json:"revision,omitzero"`
	ContentRev   int64     `json:"content_rev,omitzero"`
}

// Valid 报告关系是否仍然成立。
func (e Edge) Valid() bool { return e.InvalidAt.IsZero() }

// ValidAt 报告关系在 t 时是否成立。
func (e Edge) ValidAt(t time.Time) bool {
	return !t.Before(e.ValidFrom) && (e.InvalidAt.IsZero() || t.Before(e.InvalidAt))
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
// 仍无匹配的新建。返回引用 key 到实体的映射（含合并后的别名）；不写入，由调用方在 mutate 中按 putEntity 写入。
// known 须是调用方自有的副本：合并别名会修改其中的实体。
func (m *Memory) resolveEntities(ctx context.Context, ns []string, known []Entity, refs []entityRef) (map[string]Entity, error) {
	resolved := map[string]Entity{}
	var pending []*unresolved
	for _, ref := range refs {
		ref.Name, ref.Type = strings.TrimSpace(ref.Name), normalizeName(ref.Type)
		if _, ok := resolved[ref.key()]; ok {
			continue
		}
		if i := slices.IndexFunc(known, func(e Entity) bool { return e.Type == ref.Type && intersects(e.names(), ref.names()) }); i >= 0 {
			known[i].mergeAliases(append([]string{ref.Name}, ref.Aliases...)...)
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
		for _, key := range p.keys {
			resolved[key] = e
		}
	}
	// 同一实体可能先被引用、后又合并了别名：统一取最终的版本。
	for key, e := range resolved {
		if i := slices.IndexFunc(known, func(k Entity) bool { return k.ID == e.ID }); i >= 0 {
			resolved[key] = known[i]
		}
	}
	return resolved, nil
}

// putEntity 在 tx 中写入解析出的实体：不存在时新建；已存在时把名称与别名合并到锁内的当前版本（合并可交换，与读取后的修改不冲突）。
func putEntity(t *tx, e Entity) {
	cur, ok := t.Entities.Get(e.ID)
	if !ok {
		e.Incarnation, e.Revision, e.ContentRev = t.incarnation(), 1, 1
		t.Entities.Put(e.ID, e)
		return
	}
	if cur.mergeAliases(append([]string{e.Name}, e.Aliases...)...) {
		cur.Revision++
		cur.ContentRev++
		t.Entities.Put(cur.ID, cur)
	}
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
// 实体判重（模型与嵌入）在锁外完成；关系的去重与失效在 mutate 内按锁内的关系重新计算，一次提交。
func (m *Memory) applyRelations(ctx context.Context, j Job, at time.Time, rels []relation) error {
	rels = slices.DeleteFunc(slices.Clone(rels), func(r relation) bool {
		return strings.TrimSpace(r.Predicate) == "" || strings.TrimSpace(r.Subject.Name) == "" || strings.TrimSpace(r.Object.Name) == ""
	})
	if !m.cfg.Graph || len(rels) == 0 {
		return nil
	}
	ns := j.Namespace
	scope, s, err := m.read(ctx, ns)
	if err == nil {
		err = checkJobScope(ctx, scope)
	}
	if err != nil {
		return err
	}
	known := make([]Entity, 0, len(s.Entities))
	for _, id := range slices.Sorted(maps.Keys(s.Entities)) {
		known = append(known, entityKind.clone(s.Entities[id]))
	}
	var refs []entityRef
	for _, r := range rels {
		refs = append(refs, r.Subject, r.Object)
	}
	entities, err := m.resolveEntities(ctx, ns, known, refs)
	if err != nil {
		return err
	}
	now := m.now()
	_, err = m.mutate(ctx, ns, scope, func(t *tx) error {
		for _, key := range slices.Sorted(maps.Keys(entities)) {
			putEntity(t, entities[key])
		}
		for i, r := range rels {
			pred := strings.TrimSpace(r.Predicate)
			subj, obj := entities[r.Subject.key()], entities[r.Object.key()]
			id := stableID(j.Key, "relation", strconv.Itoa(i))
			keys := t.Edges.Keys()
			if slices.ContainsFunc(keys, func(k string) bool {
				e, _ := t.Edges.Get(k)
				return e.Valid() && e.ID != id && e.Subject == subj.ID && e.Predicate == pred && e.Object == obj.ID
			}) {
				continue
			}
			fact := strings.TrimSpace(r.Fact)
			if fact == "" {
				fact = subj.Name + " " + pred + " " + obj.Name
			}
			edge := Edge{ID: id, Namespace: ns, Subject: subj.ID, Predicate: pred, Object: obj.ID, Fact: fact, ValidFrom: at, CreatedAt: now,
				Source: j.source()}
			var superseded []Edge
			for _, k := range keys {
				old, _ := t.Edges.Get(k)
				if !r.Exclusive || !old.Valid() || old.ID == id || old.Subject != subj.ID || old.Predicate != pred || old.Object == obj.ID {
					continue
				}
				if old.ValidFrom.After(at) {
					edge.InvalidAt, edge.SupersededBy = old.ValidFrom, old.ID
					continue
				}
				superseded = append(superseded, old)
			}
			// 重做同一任务时覆盖上次写入的关系，保留实例号。
			if prev, ok := t.Edges.Get(id); ok {
				edge.Incarnation, edge.Revision, edge.ContentRev = prev.Incarnation, prev.Revision+1, prev.ContentRev
				if prev.Fact != edge.Fact || prev.Subject != edge.Subject || prev.Predicate != edge.Predicate || prev.Object != edge.Object {
					edge.ContentRev++
				}
			} else {
				edge.Incarnation, edge.Revision, edge.ContentRev = t.incarnation(), 1, 1
			}
			t.Edges.Put(id, edge)
			for _, old := range superseded {
				old.InvalidAt, old.SupersededBy = at, id
				old.Revision++
				t.Edges.Put(old.ID, old)
			}
		}
		return nil
	})
	return err
}
