package memory

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// 级联重算，设计见 docs/design/MEMORY_G2_DERIVATION.md 第 3 节：结果只取决于当前状态，重复运行不再产生写入。
// 阶段依次为记录（合并与反思的依据，循环到不动点）、实体名称、关系；实体只依赖记录，关系依赖记录与实体，所以各遍历一次即收敛。

// cascaded 是一次级联重算的结果：提交之后要更新的索引，以及图谱中变化涉及的实体。
type cascaded struct {
	records []Record        // 由有效变为无效的记录（旧版本），移出记录索引
	dropped []indexKey      // 删除或改名的实体的旧键
	renamed []Entity        // 改名的实体，按新名称写入
	touched map[string]bool // 变化的实体，以及变化的关系的两端
}

func (c *cascaded) merge(o cascaded) {
	c.records, c.dropped, c.renamed = append(c.records, o.records...), append(c.dropped, o.dropped...), append(c.renamed, o.renamed...)
	for id := range o.touched {
		c.touch(id)
	}
}

func (c *cascaded) touch(ids ...string) {
	if c.touched == nil {
		c.touched = map[string]bool{}
	}
	for _, id := range ids {
		c.touched[id] = true
	}
}

// erased 报告记录的正文是否已被擦除：被遗忘，或因依据被遗忘而清空。
func erased(r Record) bool { return !r.Valid() && r.Content == "" }

// 来源（派生或名称来源）按所指记录的状态分为四种。
const (
	originLive  = iota // 记录有效、版本一致：成立
	originIdle         // 记录版本一致但已失效：保留，不成立（替代者被回退时可以恢复）
	originDrop         // 记录不存在或版本落后：删除（版本只增不减，不会再成立）
	originErase        // 记录已擦除：删除，并擦除基础快照中的旧内容
)

func originOf(t *tx, ref RecordRef) int {
	r, ok := t.Records.m[ref.ID]
	switch {
	case !ok:
		return originDrop
	case erased(r):
		return originErase
	case r.Incarnation != ref.Incarnation || r.ContentRev != ref.ContentRev:
		return originDrop
	case r.Valid():
		return originLive
	default:
		return originIdle
	}
}

// cascade 在 tx 中按当前状态重算依据关系，只检查本次修改的对象和依赖它们的对象：
// 每次提交都运行级联，提交前的状态已收敛，其余对象不会过期。社区由调用方按 cascaded.touched 处理（syncGraph）。
func cascade(t *tx, now time.Time) cascaded { return cascadeOver(t, now, false) }

// cascadeAll 检查全部对象，Prune 用它收敛任何原因留下的不一致。
func cascadeAll(t *tx, now time.Time) cascaded { return cascadeOver(t, now, true) }

func cascadeOver(t *tx, now time.Time, all bool) cascaded {
	var out cascaded
	cascadeRecords(t, now, all, &out)
	cascadeEntities(t, all, &out)
	cascadeEdges(t, now, all, &out)
	return out
}

// changedSet 是本次修改过的键。修改通常只有几个，线性比较比哈希查找快；级联对每个对象都要查。
type changedSet struct {
	ids []string
	m   map[string]bool
}

func changedOf[T any](tb *table[T]) changedSet {
	if len(tb.changes) > 16 {
		m := make(map[string]bool, len(tb.changes))
		for k := range tb.changes {
			m[k] = true
		}
		return changedSet{m: m}
	}
	return changedSet{ids: slices.Collect(maps.Keys(tb.changes))}
}

func (s changedSet) has(id string) bool {
	if s.m != nil {
		return s.m[id]
	}
	return slices.Contains(s.ids, id)
}

func (s changedSet) empty() bool { return len(s.ids) == 0 && len(s.m) == 0 }

func (s changedSet) hasRef(refs []RecordRef) bool {
	return slices.ContainsFunc(refs, func(r RecordRef) bool { return s.has(r.ID) })
}

// cascadeTx 运行级联重算，并删除受影响的社区。
func (m *Memory) cascadeTx(t *tx, now time.Time) cascaded {
	out := cascade(t, now)
	m.syncGraph(t, out)
	return out
}

// syncGraph 删除成员涉及图谱变化的社区：它们的摘要可能含已删除或已改变的陈述与名称（B1）。
// 不在锁内重新划分（标签传播是全图计算），由 Worker 之后的 syncCommunities 重写摘要。
// 改名的实体出现在邻居社区的摘要中，所以它的邻居也视为涉及。
func (m *Memory) syncGraph(t *tx, c cascaded) {
	if len(c.touched) == 0 || !m.cfg.Graph || !m.cfg.Communities || t.Communities.Len() == 0 {
		return
	}
	hit := c.touched
	if len(c.renamed) > 0 {
		hit = maps.Clone(c.touched)
		renamed := map[string]bool{}
		for _, e := range c.renamed {
			renamed[e.ID] = true
		}
		for _, e := range t.Edges.m {
			if renamed[e.Subject] || renamed[e.Object] {
				hit[e.Subject], hit[e.Object] = true, true
			}
		}
	}
	for _, id := range t.Communities.Keys() {
		if slices.ContainsFunc(t.Communities.m[id].Members, func(x string) bool { return hit[x] }) {
			t.Communities.Delete(id)
		}
	}
}

// cascadeRecords：MergedFrom 或 ReflectedFrom 含擦除过的记录时，清空正文并擦除（underived）；
// 有效的反思所依据的情景被回退时失效（underived），保留正文。反思由有效变为无效时，其余仍有效的依据情景的 Reflected 复位。
func cascadeRecords(t *tx, now time.Time, all bool, out *cascaded) {
	erasedRef := func(ref RecordRef) bool { r, ok := t.Records.m[ref.ID]; return ok && erased(r) }
	rewoundRef := func(ref RecordRef) bool { r, ok := t.Records.m[ref.ID]; return ok && r.InvalidReason == ReasonRewound }
	for changed := true; changed; {
		changed = false
		dirty := changedOf(t.Records)
		if !all && dirty.empty() {
			return
		}
		only := nearby(t, t.Records, t.ov.Records, all, func(d *depIndex, add func([]string)) {
			for id := range t.Records.changes {
				add(d.records[id])
			}
		})
		derived := keysWhere(t.Records.m, only, func(r Record) bool {
			return r.InvalidReason != ReasonForgotten && !erased(r) && len(r.MergedFrom)+len(r.ReflectedFrom) > 0 &&
				(all || dirty.has(r.ID) || dirty.hasRef(r.MergedFrom) || dirty.hasRef(r.ReflectedFrom))
		})
		for _, id := range derived {
			old := t.Records.m[id]
			r, _ := t.Records.Get(id)
			switch {
			case slices.ContainsFunc(r.MergedFrom, erasedRef) || slices.ContainsFunc(r.ReflectedFrom, erasedRef):
				if r.Valid() {
					r = r.invalidated(now, ReasonUnderived, "")
				} else {
					r.Revision++
				}
				r.Content, r.Keywords, r.Entities = "", nil, nil
				r.ContentRev++
				t.Records.Put(id, r)
				t.Records.MarkErasure(id)
			case r.Valid() && slices.ContainsFunc(r.ReflectedFrom, rewoundRef):
				t.Records.Put(id, r.invalidated(now, ReasonUnderived, ""))
			default:
				continue
			}
			changed = true
			if old.Valid() {
				out.records = append(out.records, old)
				resetReflected(t, r.ReflectedFrom)
			}
		}
	}
}

// resetReflected 使反思失效后，其余仍有效的依据情景可以重新参与反思；Revision 加一，使进行中的反思放弃提交。
func resetReflected(t *tx, refs []RecordRef) {
	for _, ref := range refs {
		if r, ok := t.Records.Get(ref.ID); ok && r.Valid() && r.Reflected {
			r.Reflected = false
			r.Revision++
			t.Records.Put(r.ID, r)
		}
	}
}

// cascadeEntities：删除不再成立的名称来源；名称全部删除时删除实体，否则重算存活状态与显示名。
func cascadeEntities(t *tx, all bool, out *cascaded) {
	records, entities := changedOf(t.Records), changedOf(t.Entities)
	if !all && records.empty() && entities.empty() {
		return
	}
	affected := func(e Entity) bool {
		return all || entities.has(e.ID) || slices.ContainsFunc(e.Names, func(n EntityName) bool {
			return slices.ContainsFunc(n.Origins, func(o Origin) bool { return records.has(o.Record.ID) })
		})
	}
	only := nearby(t, t.Entities, t.ov.Entities, all, func(d *depIndex, add func([]string)) {
		for id := range t.Records.changes {
			add(d.entities[id])
		}
	})
	for _, id := range keysWhere(t.Entities.m, only, func(e Entity) bool { return affected(e) && entityStale(t, e) }) {
		e, _ := t.Entities.Get(id)
		old, erase := t.Entities.m[id], false
		names := e.Names[:0]
		for _, n := range e.Names {
			origins := n.Origins[:0]
			n.Live = false
			for _, o := range n.Origins {
				switch originOf(t, o.Record) {
				case originErase:
					erase = true
					continue
				case originDrop:
					continue
				case originLive:
					n.Live = true
				}
				origins = append(origins, o)
			}
			if n.Origins = origins; len(origins) > 0 {
				names = append(names, n)
			}
		}
		e.Names = names
		out.touch(id)
		if len(names) == 0 {
			t.Entities.Delete(id)
			out.dropped = append(out.dropped, entityKey(old))
			continue
		}
		e.Live = slices.ContainsFunc(names, func(n EntityName) bool { return n.Live })
		e.Name, e.Aliases = displayNames(e)
		if e.Name != old.Name {
			e.ContentRev++
			out.dropped, out.renamed = append(out.dropped, entityKey(old)), append(out.renamed, e)
		}
		e.Revision++
		t.Entities.Put(id, e)
		if erase {
			t.Entities.MarkErasure(id)
		}
	}
}

// entityStale 报告实体是否需要重算：有要删除的来源，或物化的状态与名称不一致。
func entityStale(t *tx, e Entity) bool {
	if len(e.Names) == 0 {
		return true
	}
	live := false
	for _, n := range e.Names {
		nl := false
		for _, o := range n.Origins {
			switch originOf(t, o.Record) {
			case originErase, originDrop:
				return true
			case originLive:
				nl = true
			}
		}
		if nl != n.Live {
			return true
		}
		live = live || nl
	}
	if live != e.Live {
		return true
	}
	return !displayNamesHold(e)
}

// displayNamesHold 报告 e.Name 与 e.Aliases 是否等于 displayNames(e)，不分配内存（级联每次检查全部实体）。
func displayNamesHold(e Entity) bool {
	usable := func(n EntityName) bool { return n.Live || !e.Live }
	i := slices.IndexFunc(e.Names, func(n EntityName) bool { return sameName(n.Name, e.Name) })
	if i < 0 || !usable(e.Names[i]) {
		return false
	}
	k := 0
	for _, n := range e.Names {
		if !usable(n) || sameName(n.Name, e.Name) {
			continue
		}
		if k >= len(e.Aliases) || e.Aliases[k] != n.Name {
			return false
		}
		k++
	}
	return k == len(e.Aliases)
}

// sameName 报告 normalizeName(a) == normalizeName(b)，不分配内存。
func sameName(a, b string) bool {
	if a == b {
		return true
	}
	a, b = strings.TrimLeftFunc(a, unicode.IsSpace), strings.TrimLeftFunc(b, unicode.IsSpace)
	for {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		sa, sb := a == "" || unicode.IsSpace(ra), b == "" || unicode.IsSpace(rb)
		if sa || sb {
			if sa != sb {
				return false
			}
			a, b = strings.TrimLeftFunc(a, unicode.IsSpace), strings.TrimLeftFunc(b, unicode.IsSpace)
			if a == "" || b == "" {
				return a == b
			}
			continue
		}
		if unicode.ToLower(ra) != unicode.ToLower(rb) {
			return false
		}
		a, b = a[na:], b[nb:]
	}
}

// displayNames 返回实体的显示名与别名：显示名不变，除非它已被删除，或实体存活而它不存活（此时取第一个存活的名称）；
// 别名是显示名以外的存活名称，实体不存活时为其余全部名称。
func displayNames(e Entity) (string, []string) {
	usable := func(n EntityName) bool { return n.Live || !e.Live }
	name := e.Name
	if i := slices.IndexFunc(e.Names, func(n EntityName) bool { return normalizeName(n.Name) == normalizeName(e.Name) }); i < 0 || !usable(e.Names[i]) {
		name = e.Names[slices.IndexFunc(e.Names, usable)].Name
	}
	var aliases []string
	for _, n := range e.Names {
		if usable(n) && normalizeName(n.Name) != normalizeName(name) {
			aliases = append(aliases, n.Name)
		}
	}
	return name, aliases
}

// cascadeEdges：删除不再成立的派生；派生全部删除时删除关系；否则重算陈述、来源与有效性。
// superseded 的关系保持失效（由 Rewound 恢复），其余关系在两端实体存活且有成立的派生时有效，否则失效（underived）。
func cascadeEdges(t *tx, now time.Time, all bool, out *cascaded) {
	records, entities, edges := changedOf(t.Records), changedOf(t.Entities), changedOf(t.Edges)
	if !all && records.empty() && entities.empty() && edges.empty() {
		return
	}
	affected := func(e Edge) bool {
		return all || edges.has(e.ID) || entities.has(e.Subject) || entities.has(e.Object) ||
			slices.ContainsFunc(e.Derivations, func(d Derivation) bool { return records.has(d.Record.ID) })
	}
	only := nearby(t, t.Edges, t.ov.Edges, all, func(d *depIndex, add func([]string)) {
		for id := range t.Records.changes {
			add(d.edges[id])
		}
		for id := range t.Entities.changes {
			add(d.endpoints[id])
		}
	})
	for _, id := range keysWhere(t.Edges.m, only, func(e Edge) bool { return affected(e) && edgeStale(t, e) }) {
		e, _ := t.Edges.Get(id)
		old, erase := t.Edges.m[id], false
		ds := e.Derivations[:0]
		for _, d := range e.Derivations {
			switch originOf(t, d.Record) {
			case originErase:
				erase = true
				continue
			case originDrop:
				continue
			}
			ds = append(ds, d)
		}
		e.Derivations = ds
		out.touch(e.Subject, e.Object)
		if len(ds) == 0 {
			t.Edges.Delete(id)
			continue
		}
		e.Fact, e.Sources = edgeFact(t, e), edgeSources(e)
		if e.Fact != old.Fact {
			e.ContentRev++
		}
		switch valid := edgeHolds(t, e); {
		case valid && !e.InvalidAt.IsZero():
			e.InvalidAt, e.InvalidReason = time.Time{}, ""
		case !valid && e.InvalidAt.IsZero():
			e.InvalidAt, e.InvalidReason = now, ReasonUnderived
		}
		e.Revision++
		t.Edges.Put(id, e)
		if erase {
			t.Edges.MarkErasure(id)
		}
	}
}

// edgeHolds 报告关系按派生与实体是否应当有效；superseded 的关系不由级联恢复。
func edgeHolds(t *tx, e Edge) bool {
	if e.InvalidReason == ReasonSuperseded {
		return false
	}
	live := func(id string) bool { x, ok := t.Entities.m[id]; return ok && x.Live }
	return live(e.Subject) && live(e.Object) &&
		slices.ContainsFunc(e.Derivations, func(d Derivation) bool { return originOf(t, d.Record) == originLive })
}

func edgeStale(t *tx, e Edge) bool {
	if len(e.Derivations) == 0 {
		return true
	}
	for _, d := range e.Derivations {
		if s := originOf(t, d.Record); s == originErase || s == originDrop {
			return true
		}
	}
	if e.InvalidReason != ReasonSuperseded && edgeHolds(t, e) != e.InvalidAt.IsZero() {
		return true
	}
	return e.Fact != edgeFact(t, e) || !sourcesHold(e)
}

// sourcesHold 报告 e.Sources 是否等于 edgeSources(e)，不分配内存。
func sourcesHold(e Edge) bool {
	k := 0
	for _, d := range e.Derivations {
		if d.Source == (Source{}) || slices.Contains(e.Sources[:k], d.Source) {
			continue
		}
		if k >= len(e.Sources) || e.Sources[k] != d.Source {
			return false
		}
		k++
	}
	return k == len(e.Sources)
}

// keysWhere 按字典序返回 m 中满足 keep 的键，only 不为 nil 时只看其中的键；结果与映射的随机顺序无关，又不必对全部键排序。
func keysWhere[T any](m map[string]T, only map[string]bool, keep func(T) bool) []string {
	var out []string
	if only == nil {
		for k, v := range m {
			if keep(v) {
				out = append(out, k)
			}
		}
	}
	for k := range only {
		if v, ok := m[k]; ok && keep(v) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// depIndex 是基础快照的反向依赖：记录 → 依据它的记录、名称来源指向它的实体、派生指向它的关系；实体 → 以它为端点的关系。
// 基础快照不变，第一次级联时构建一次，基于同一快照的各次提交共享；之后改过的对象由级联另行检查（nearby）。
type depIndex struct {
	once                     sync.Once
	base                     state
	records, entities, edges map[string][]string
	endpoints                map[string][]string
}

func newDepIndex(base state) *depIndex { return &depIndex{base: base} }

func (d *depIndex) get() *depIndex {
	d.once.Do(func() {
		d.records, d.entities, d.edges, d.endpoints = map[string][]string{}, map[string][]string{}, map[string][]string{}, map[string][]string{}
		for id, r := range d.base.Records {
			for _, ref := range slices.Concat(r.MergedFrom, r.ReflectedFrom) {
				d.records[ref.ID] = append(d.records[ref.ID], id)
			}
		}
		for id, e := range d.base.Entities {
			for _, n := range e.Names {
				for _, o := range n.Origins {
					d.entities[o.Record.ID] = append(d.entities[o.Record.ID], id)
				}
			}
		}
		for id, e := range d.base.Edges {
			for _, x := range e.Derivations {
				d.edges[x.Record.ID] = append(d.edges[x.Record.ID], id)
			}
			d.endpoints[e.Subject] = append(d.endpoints[e.Subject], id)
			d.endpoints[e.Object] = append(d.endpoints[e.Object], id)
		}
		d.base = state{}
	})
	return d
}

// nearby 返回级联需要检查的 tb 的键：基础快照中依赖本次修改的对象（from 从反向依赖中取），加上基础快照之后改过的对象
// （覆盖层 ov 与本次修改）。结果是候选，仍须按当前值核对。all 为真或没有反向依赖时返回 nil，表示检查全部对象。
func nearby[T any](t *tx, tb *table[T], ov map[string]change[T], all bool, from func(*depIndex, func([]string))) map[string]bool {
	if all || t.deps == nil {
		return nil
	}
	out := map[string]bool{}
	add := func(ids []string) {
		for _, id := range ids {
			out[id] = true
		}
	}
	from(t.deps.get(), add)
	for id := range ov {
		out[id] = true
	}
	for id := range tb.changes {
		out[id] = true
	}
	return out
}

// edgeFact 取成立派生中最新的陈述；没有成立的派生时取全部派生中最新的。同一时间按记录 ID 取较大者，结果确定。
func edgeFact(t *tx, e Edge) string {
	best, bestLive := -1, false
	for i, d := range e.Derivations {
		live := originOf(t, d.Record) == originLive
		switch {
		case best < 0, live && !bestLive:
		case live != bestLive:
			continue
		default:
			if b := e.Derivations[best]; !d.At.After(b.At) && !(d.At.Equal(b.At) && d.Record.ID > b.Record.ID) {
				continue
			}
		}
		best, bestLive = i, live
	}
	return e.Derivations[best].Fact
}

func edgeSources(e Edge) []Source {
	var out []Source
	for _, d := range e.Derivations {
		if d.Source != (Source{}) && !slices.Contains(out, d.Source) {
			out = append(out, d.Source)
		}
	}
	return out
}

// addOrigin 加入一个来源：同一记录的来源只留一条（新的替换旧的），超出 maxOrigins 时先删不成立的，再删最早加入的。
func addOrigin[T any](t *tx, list []T, v T, origin func(T) Origin) []T {
	id := origin(v).Record.ID
	list = slices.DeleteFunc(list, func(x T) bool { return origin(x).Record.ID == id })
	list = append(list, v)
	for len(list) > maxOrigins {
		i := slices.IndexFunc(list, func(x T) bool { return originOf(t, origin(x).Record) != originLive })
		list = slices.Delete(list, max(i, 0), max(i, 0)+1)
	}
	return list
}

func addDerivation(t *tx, ds []Derivation, d Derivation) []Derivation {
	return addOrigin(t, ds, d, func(d Derivation) Origin { return d.Origin })
}

// addName 把名称（原文）连同来源加入实体；规范化后相同的名称合并来源。
func addName(t *tx, e *Entity, name string, o Origin) {
	if name = strings.TrimSpace(name); name == "" {
		return
	}
	i := slices.IndexFunc(e.Names, func(n EntityName) bool { return normalizeName(n.Name) == normalizeName(name) })
	if i < 0 {
		e.Names = append(e.Names, EntityName{Name: name})
		i = len(e.Names) - 1
	}
	e.Names[i].Origins = addOrigin(t, e.Names[i].Origins, o, func(o Origin) Origin { return o })
}

// indexAfter 在提交之后更新级联涉及的索引：失效记录与旧实体键移出，改名的实体按新名称写入。
func (m *Memory) indexAfter(ctx context.Context, ns []string, scope string, c cascaded) error {
	errs := []error{
		m.dropIndex(ctx, ns, scope, recordSpace, keysOf(c.records)...),
		m.dropIndex(ctx, ns, scope, entitySpace, c.dropped...),
	}
	if m.cfg.Index == nil || len(c.renamed) == 0 {
		return errors.Join(errs...)
	}
	keys, vecs := make([]indexKey, 0, len(c.renamed)), make([][]float32, 0, len(c.renamed))
	for _, e := range c.renamed {
		vec, err := m.embed(ctx, e.Name)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		keys, vecs = append(keys, entityKey(e)), append(vecs, vec)
	}
	return errors.Join(append(errs, m.writeIndex(ctx, ns, scope, entitySpace, keys, vecs))...)
}
