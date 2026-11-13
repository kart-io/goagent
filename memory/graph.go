package memory

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kart-io/goagent/constants"
)

// Entity 是图中的实体节点，按命名空间、类型与规范化名称去重。Names 是实体的全部名称及其来源，由级联重算维护：
// 来源记录被遗忘时名称随之删除，名称全部删除时实体删除；至少一个名称存活时 Live 为真，不存活的实体不参与召回与社区。
// Name 是显示名，取自存活的名称；Aliases 是 Name 以外的存活名称（实体不存活时为其余全部名称），二者都是物化值。
// 配置了 Index 时名称向量存于 Index 的实体子命名空间，用于找出名称不同的候选重复实体。
type Entity struct {
	ID        string       `json:"id"`
	Namespace []string     `json:"namespace"`
	Name      string       `json:"name"`
	Type      string       `json:"type"`
	Aliases   []string     `json:"aliases,omitempty"`
	Names     []EntityName `json:"names,omitempty"`
	Live      bool         `json:"live,omitzero"`
	CreatedAt time.Time    `json:"created_at"`
	// Incarnation、Revision 的含义与 Record 相同；ContentRev 只在 Name 改变时加一，名称向量的键据此区分版本。
	Incarnation int64 `json:"incarnation,omitzero"`
	Revision    int64 `json:"revision,omitzero"`
	ContentRev  int64 `json:"content_rev,omitzero"`
}

// EntityName 是实体的一个名称（规范化前的原文）及其来源；至少一个来源成立时 Live 为真。
type EntityName struct {
	Name    string   `json:"name"`
	Origins []Origin `json:"origins,omitempty"`
	Live    bool     `json:"live,omitzero"`
}

// RecordRef 指向记录的一个版本。
type RecordRef struct {
	ID          string `json:"id"`
	Incarnation int64  `json:"incarnation,omitzero"`
	ContentRev  int64  `json:"content_rev,omitzero"`
}

func refOf(r Record) RecordRef {
	return RecordRef{ID: r.ID, Incarnation: r.Incarnation, ContentRev: r.ContentRev}
}

// Origin 是关系或实体名称的一个来源：依据的记录版本，以及产生它的轮次（回退据此删除）。
type Origin struct {
	Record RecordRef `json:"record"`
	Source Source    `json:"source,omitzero"`
}

// Derivation 是关系的一次派生：依据的记录与该次抽取的陈述；At 是对话时间，整理转移时沿用。
type Derivation struct {
	Origin
	Fact string    `json:"fact"`
	At   time.Time `json:"at"`
}

// maxOrigins 是一条关系的派生、一个名称的来源的上限，超出时先删不成立的，再删最早的。
const maxOrigins = 256

// Edge 是带有效期的关系（Zep 时序图）：Subject、Object 为实体 ID。关系由记录派生（Derivations），
// 至少一条派生成立、两端实体存活时有效，否则失效（underived）；派生全部删除时关系删除。Fact 是成立派生中最新的陈述，Sources 是派生的轮次，二者都是物化值。
// 与新事实矛盾的边写入 InvalidAt（superseded）并以 SupersededBy 指向新边，不删除，因而可追溯某一时刻成立的关系。
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
	// InvalidReason 只有 superseded 与 underived。
	InvalidReason InvalidReason `json:"invalid_reason,omitempty"`
	Sources       []Source      `json:"sources,omitempty"`
	Derivations   []Derivation  `json:"derivations,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	Incarnation   int64         `json:"incarnation,omitzero"`
	Revision      int64         `json:"revision,omitzero"`
	ContentRev    int64         `json:"content_rev,omitzero"`
}

// Valid 报告关系是否仍然成立。没有派生的关系（G1 写入的旧数据）不成立。
func (e Edge) Valid() bool { return e.InvalidAt.IsZero() && len(e.Derivations) > 0 }

// ValidAt 报告关系在 t 时是否成立。
func (e Edge) ValidAt(t time.Time) bool {
	return len(e.Derivations) > 0 && !t.Before(e.ValidFrom) && (e.InvalidAt.IsZero() || t.Before(e.InvalidAt))
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
	From      *int      `json:"from"` // 依据的记忆在本轮抽取结果中的下标
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
	vector     []float32
	candidates []Entity
}

// resolveEntities 把一轮抽取的实体引用解析为实体（Graphiti 式判重）：先按类型与规范化名称（含别名）精确匹配；
// 匹配不到的，取同类型中名称向量或字符对相似度不低于 EntitySimilarity 的实体作为候选，由模型一次判定是否为同一对象；
// 仍无匹配的新建。返回引用 key 到实体的映射（含合并后的别名），以及新建实体的名称向量；不写入，由调用方在 mutate 中加入名称来源。
// known 须是调用方自有的副本，取自作用域 scope：合并别名会修改其中的实体。
func (m *Memory) resolveEntities(ctx context.Context, ns []string, scope string, known []Entity, refs []entityRef) (map[string]Entity, map[string][]float32, error) {
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
	if err := m.entityCandidates(ctx, ns, scope, known, pending); err != nil {
		return nil, nil, err
	}
	duplicates, err := m.judgeDuplicates(ctx, pending)
	if err != nil {
		return nil, nil, err
	}
	vectors := map[string][]float32{}
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
				vectors[e.ID] = p.vector
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
	return resolved, vectors, nil
}

// entityCandidates 为每个未匹配的引用找同类型中最相似的至多 EntityCandidates 个实体：
// 相似度取名称向量余弦（需 Index）与名称字符对重合的较大者。翻译或别称（如"Acme"与"爱克米"）只能靠向量找到。
// 向量在实体子命名空间中检索全部已知实体，只采用 known 中仍是该版本的键。
func (m *Memory) entityCandidates(ctx context.Context, ns []string, scope string, known []Entity, pending []*unresolved) error {
	keys := make(map[string]indexKey, len(known))
	for _, e := range known {
		keys[e.ID] = entityKey(e)
	}
	for _, p := range pending {
		similarity := map[string]float64{}
		if m.cfg.Index != nil {
			var err error
			if p.vector, err = m.embed(ctx, p.ref.Name); err != nil {
				return fmt.Errorf("生成实体名称向量：%w", err)
			}
			if len(known) > 0 {
				hits, err := m.cfg.Index.Search(ctx, entitySpace.namespace(ns, scope), p.vector, len(known))
				if err != nil {
					return err
				}
				for _, h := range hits {
					if k, ok := parseIndexKey(h.ID); ok && k.Model == m.modelTag && keys[k.ID] == (indexKey{ID: k.ID, Incarnation: k.Incarnation, ContentRev: k.ContentRev}) {
						similarity[k.ID] = max(similarity[k.ID], h.Score)
					}
				}
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
			s := max(overlap(grams, bigrams(strings.Join(e.names(), " "))), similarity[e.ID])
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

// applyRelations 写入一轮抽取出的关系，at 是该轮发生的时间。每条关系以 From 指向本轮整理出的记录（refs），没有依据的关系丢弃。
// 两端实体的名称连同依据加入实体；已有相同的有效关系时只追加一条派生，否则新建关系。
// 排他谓词上的其他有效关系在 at 失效（superseded）并指向新关系；已有关系比新关系生效更晚时（迟到的旧事实），新关系写入即失效并指向已有关系。
// 实体判重（模型与嵌入）在锁外完成；名称、关系的去重与失效在 mutate 内按锁内的状态重新计算并级联，一次提交。
func (m *Memory) applyRelations(ctx context.Context, j Job, at time.Time, rels []relation, refs map[int]RecordRef) error {
	rels = slices.DeleteFunc(slices.Clone(rels), func(r relation) bool {
		if r.From == nil {
			return true
		}
		_, ok := refs[*r.From]
		return !ok || strings.TrimSpace(r.Predicate) == "" || strings.TrimSpace(r.Subject.Name) == "" || strings.TrimSpace(r.Object.Name) == ""
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
		if len(s.Entities[id].Names) > 0 {
			known = append(known, entityKind.clone(s.Entities[id]))
		}
	}
	var ends []entityRef
	for _, r := range rels {
		ends = append(ends, r.Subject, r.Object)
	}
	entities, vectors, err := m.resolveEntities(ctx, ns, scope, known, ends)
	if err != nil {
		return err
	}
	now := m.now()
	var created []indexKey
	var createdVecs [][]float32
	var c cascaded
	_, err = m.mutate(ctx, ns, scope, func(t *tx) error {
		created, createdVecs, c = nil, nil, cascaded{}
		touched, fresh := map[string]*Entity{}, map[string]bool{}
		// entity 返回引用解析到的实体在本次提交中的工作副本；期间被删除的按原 ID 重建。
		entity := func(r entityRef) *Entity {
			res := entities[r.key()]
			if e, ok := touched[res.ID]; ok {
				return e
			}
			cur, ok := t.Entities.Get(res.ID)
			if ok {
				cur.Revision++
			} else {
				cur = Entity{ID: res.ID, Namespace: ns, Name: strings.TrimSpace(res.Name), Type: res.Type, CreatedAt: now,
					Incarnation: t.incarnation(), Revision: 1, ContentRev: 1}
				fresh[res.ID] = true
			}
			touched[res.ID] = &cur
			return &cur
		}
		valid := map[[3]string]string{}
		for _, id := range t.Edges.Keys() {
			if e := t.Edges.m[id]; e.Valid() {
				valid[[3]string{e.Subject, e.Predicate, e.Object}] = id
			}
		}
		for i, r := range rels {
			pred := strings.TrimSpace(r.Predicate)
			o := Origin{Record: refs[*r.From], Source: j.source()}
			subj, obj := entity(r.Subject), entity(r.Object)
			for _, side := range []struct {
				e   *Entity
				ref entityRef
			}{{subj, r.Subject}, {obj, r.Object}} {
				for _, n := range append([]string{side.ref.Name}, side.ref.Aliases...) {
					addName(t, side.e, n, o)
				}
			}
			fact := strings.TrimSpace(r.Fact)
			if fact == "" {
				fact = strings.TrimSpace(r.Subject.Name) + " " + pred + " " + strings.TrimSpace(r.Object.Name)
			}
			d := Derivation{Origin: o, Fact: fact, At: at}
			id := stableID(j.Key, "relation", strconv.Itoa(i))
			triple := [3]string{subj.ID, pred, obj.ID}
			if k, ok := valid[triple]; ok && k != id {
				e, _ := t.Edges.Get(k)
				e.Derivations = addDerivation(t, e.Derivations, d)
				e.Revision++
				t.Edges.Put(k, e)
				continue
			}
			edge := Edge{ID: id, Namespace: ns, Subject: subj.ID, Predicate: pred, Object: obj.ID, Fact: fact, ValidFrom: at, CreatedAt: now,
				Derivations: []Derivation{d}}
			var superseded []string
			for _, k := range t.Edges.Keys() {
				old := t.Edges.m[k]
				if !r.Exclusive || !old.Valid() || old.ID == id || old.Subject != subj.ID || old.Predicate != pred || old.Object == obj.ID {
					continue
				}
				if old.ValidFrom.After(at) {
					edge.InvalidAt, edge.SupersededBy, edge.InvalidReason = old.ValidFrom, old.ID, ReasonSuperseded
					continue
				}
				superseded = append(superseded, k)
			}
			// 重做同一任务时覆盖上次写入的关系，保留实例号与其他任务追加的派生。
			if prev, ok := t.Edges.Get(id); ok {
				edge.Incarnation, edge.Revision, edge.ContentRev = prev.Incarnation, prev.Revision+1, prev.ContentRev
				if [3]string{prev.Subject, prev.Predicate, prev.Object} == triple {
					edge.Derivations = addDerivation(t, prev.Derivations, d)
				}
				if prev.Fact != edge.Fact || prev.Subject != edge.Subject || prev.Predicate != edge.Predicate || prev.Object != edge.Object {
					edge.ContentRev++
				}
			} else {
				edge.Incarnation, edge.Revision, edge.ContentRev = t.incarnation(), 1, 1
			}
			edge.Sources = edgeSources(edge)
			t.Edges.Put(id, edge)
			if edge.Valid() {
				valid[triple] = id
			}
			for _, k := range superseded {
				old, _ := t.Edges.Get(k)
				old.InvalidAt, old.SupersededBy, old.InvalidReason = at, id, ReasonSuperseded
				old.Revision++
				t.Edges.Put(k, old)
				delete(valid, [3]string{old.Subject, old.Predicate, old.Object})
			}
		}
		for _, id := range slices.Sorted(maps.Keys(touched)) {
			t.Entities.Put(id, *touched[id])
		}
		c = cascade(t, now)
		c.touch(slices.Collect(maps.Keys(touched))...)
		m.syncGraph(t, c)
		for _, id := range slices.Sorted(maps.Keys(fresh)) {
			if cur, ok := t.Entities.Get(id); ok && vectors[id] != nil {
				created, createdVecs = append(created, entityKey(cur)), append(createdVecs, vectors[id])
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return errors.Join(m.writeIndex(ctx, ns, scope, entitySpace, created, createdVecs), m.indexAfter(ctx, ns, scope, c))
}
