package memory

import (
	"maps"
	"slices"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// 增量提交的修改接口，设计见 docs/design/MEMORY_G1_INCREMENTAL.md 3.3：修改函数只能经 tx 读写，读到的是值的拷贝，
// 写入是整体替换并记入变更集；变更集按键合并进 head 的覆盖层。

// change 是一个键的最后一次变更：值或删除（墓碑）。Erase 表示旧内容须从基础快照中物理擦除（第 6 节）。
type change[T any] struct {
	Seq     int64
	Deleted bool
	Erase   bool
	Value   T
}

// kind 描述一类对象：拷贝、规范化（补 Namespace、空切片为 nil）与逻辑大小。
type kind[T any] struct {
	clone     func(T) T
	normalize func(*T, []string) bool // 返回 false 表示对象属于其他命名空间
	size      func(string, T) int
}

// table 是一类对象在一次修改中的视图：m 是工作副本（已含本次修改），changes 是本次的变更。
type table[T any] struct {
	k       kind[T]
	m       map[string]T
	base    map[string]T // 基础快照中的同类对象，用于判断删除是否需要擦除
	changes map[string]change[T]
	t       *tx
}

// Get 返回 key 的值的拷贝。
func (tb *table[T]) Get(key string) (T, bool) {
	v, ok := tb.m[key]
	if ok {
		v = tb.k.clone(v)
	}
	return v, ok
}

// Len 返回对象个数。
func (tb *table[T]) Len() int { return len(tb.m) }

// Keys 按字典序返回全部键：修改函数据此遍历，结果与映射的随机顺序无关。
func (tb *table[T]) Keys() []string {
	return slices.Sorted(maps.Keys(tb.m))
}

// Put 以 v 整体替换 key 的值。
func (tb *table[T]) Put(key string, v T) {
	v = tb.k.clone(v)
	if !tb.k.normalize(&v, tb.t.ns) && tb.t.err == nil {
		tb.t.err = agentErrors.Newf(agentErrors.CodeInvalidInput, "长期记忆对象 %q 不属于命名空间 %v", key, tb.t.ns).WithComponent("memory").WithOperation("mutate")
	}
	if old, ok := tb.m[key]; ok {
		tb.t.logical -= tb.k.size(key, old)
	}
	tb.t.logical += tb.k.size(key, v)
	tb.m[key] = v
	c := tb.changes[key]
	tb.changes[key] = change[T]{Value: v, Erase: c.Erase}
}

// Delete 删除 key；基础快照中有它时，这次删除需要物理擦除。
func (tb *table[T]) Delete(key string) {
	old, ok := tb.m[key]
	if !ok {
		return
	}
	tb.t.logical -= tb.k.size(key, old)
	delete(tb.m, key)
	_, inBase := tb.base[key]
	tb.changes[key] = change[T]{Deleted: true, Erase: inBase}
}

// MarkErasure 表示 key 在基础快照中的旧内容须物理擦除（例如遗忘：记录保留但内容不再可用）。
func (tb *table[T]) MarkErasure(key string) {
	if _, inBase := tb.base[key]; !inBase {
		return
	}
	c, ok := tb.changes[key]
	if !ok {
		v, exists := tb.m[key]
		c = change[T]{Value: v, Deleted: !exists}
	}
	c.Erase = true
	tb.changes[key] = c
}

// tx 是一次 mutate 的修改接口。
type tx struct {
	ns          []string
	scope       string // 本次修改所在的作用域
	Records     *table[Record]
	Entities    *table[Entity]
	Edges       *table[Edge]
	Communities *table[Community]
	Jobs        *table[Job]
	DoneJobs    *table[time.Time]
	Markers     *table[rewindMarker]
	scalars     stateScalars
	orig        stateScalars
	deps        *depIndex // 基础快照的反向依赖；为 nil 时级联逐个检查全部对象
	ov          overlay   // 基础快照之后已提交的变更：其中的对象不在 deps 中，级联逐个检查
	logical     int       // 本次修改带来的逻辑大小变化
	err         error
}

// newTx 在 cur 的工作副本上开始一次修改；base 是 cur 所依据的基础快照。
func newTx(cur, base state, ns []string) *tx {
	w := cur.clone()
	t := &tx{ns: ns, scalars: cur.scalars(), orig: cur.scalars()}
	t.Records = &table[Record]{k: recordKind, m: w.Records, base: base.Records, changes: map[string]change[Record]{}, t: t}
	t.Entities = &table[Entity]{k: entityKind, m: w.Entities, base: base.Entities, changes: map[string]change[Entity]{}, t: t}
	t.Edges = &table[Edge]{k: edgeKind, m: w.Edges, base: base.Edges, changes: map[string]change[Edge]{}, t: t}
	t.Communities = &table[Community]{k: communityKind, m: w.Communities, base: base.Communities, changes: map[string]change[Community]{}, t: t}
	t.Jobs = &table[Job]{k: jobKind, m: w.Jobs, base: base.Jobs, changes: map[string]change[Job]{}, t: t}
	t.DoneJobs = &table[time.Time]{k: doneKind, m: w.DoneJobs, base: base.DoneJobs, changes: map[string]change[time.Time]{}, t: t}
	t.Markers = &table[rewindMarker]{k: markerKind, m: w.Markers, base: base.Markers, changes: map[string]change[rewindMarker]{}, t: t}
	return t
}

// incarnation 分配一个实例号（从 1 起），使 NextIncarnation 始终大于已分配的实例号（不变量 1）。
// changed 报告这次修改是否改动了任何对象或标量。
func (t *tx) changed() bool {
	return len(t.Records.changes)+len(t.Entities.changes)+len(t.Edges.changes)+len(t.Communities.changes)+len(t.Jobs.changes)+len(t.DoneJobs.changes)+len(t.Markers.changes) > 0 ||
		t.scalars != t.orig
}

func (t *tx) incarnation() int64 {
	n := max(t.scalars.NextIncarnation, 1)
	t.scalars.NextIncarnation = n + 1
	return n
}

// nextSeq 是这次修改提交后的 Seq。
func (t *tx) nextSeq() int64 { return t.scalars.Seq + 1 }

func (t *tx) NextIncarnation() int64     { return t.scalars.NextIncarnation }
func (t *tx) SetNextIncarnation(n int64) { t.scalars.NextIncarnation = n }
func (t *tx) LastForgetSeq() int64       { return t.scalars.LastForgetSeq }
func (t *tx) SetLastForgetSeq(n int64)   { t.scalars.LastForgetSeq = n }
func (t *tx) MarkerHorizon() int64       { return t.scalars.MarkerHorizon }
func (t *tx) SetMarkerHorizon(n int64)   { t.scalars.MarkerHorizon = n }
func (t *tx) MigrationID() string        { return t.scalars.MigrationID }
func (t *tx) SetMigrationID(id string)   { t.scalars.MigrationID = id }

// working 返回工作副本（已含本次修改）。
func (t *tx) working() state {
	return state{Seq: t.scalars.Seq, CommitID: t.scalars.CommitID, NextIncarnation: t.scalars.NextIncarnation,
		LastForgetSeq: t.scalars.LastForgetSeq, MarkerHorizon: t.scalars.MarkerHorizon, MigrationID: t.scalars.MigrationID,
		Records: t.Records.m, Entities: t.Entities.m, Edges: t.Edges.m, Communities: t.Communities.m, Jobs: t.Jobs.m, DoneJobs: t.DoneJobs.m,
		Markers: t.Markers.m}
}

// overlay 返回本次的变更集，各项的 Seq 为 seq。
func (t *tx) overlay(seq int64) overlay {
	return overlay{
		Records: stamp(t.Records.changes, seq), Entities: stamp(t.Entities.changes, seq), Edges: stamp(t.Edges.changes, seq),
		Communities: stamp(t.Communities.changes, seq), Jobs: stamp(t.Jobs.changes, seq), DoneJobs: stamp(t.DoneJobs.changes, seq),
		Markers: stamp(t.Markers.changes, seq),
	}
}

func stamp[T any](in map[string]change[T], seq int64) map[string]change[T] {
	out := make(map[string]change[T], len(in))
	for k, c := range in {
		c.Seq = seq
		out[k] = c
	}
	return out
}

// ---- 各类对象：拷贝（含切片，使修改函数改不到共享的值）、规范化与逻辑大小 ----

func normNS(objNS *[]string, ns []string) bool {
	ok := len(*objNS) == 0 || slices.Equal(*objNS, ns)
	*objNS = ns
	return ok
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

func strLen(ss []string) int {
	n := 0
	for _, s := range ss {
		n += len(s) + 2
	}
	return n
}

func sourcesLen(ss []Source) int {
	n := 0
	for _, s := range ss {
		n += 24 + len(s.Session) + len(s.Job)
	}
	return n
}

func refsLen(rs []RecordRef) int {
	n := 0
	for _, r := range rs {
		n += 16 + len(r.ID)
	}
	return n
}

func originLen(o Origin) int { return 16 + len(o.Record.ID) + sourcesLen([]Source{o.Source}) }

var recordKind = kind[Record]{
	clone: func(r Record) Record {
		r.Keywords, r.Entities, r.Sources = slices.Clone(r.Keywords), slices.Clone(r.Entities), slices.Clone(r.Sources)
		r.MergedFrom, r.ReflectedFrom = slices.Clone(r.MergedFrom), slices.Clone(r.ReflectedFrom)
		return r
	},
	normalize: func(r *Record, ns []string) bool {
		r.Keywords, r.Entities, r.Sources = nilIfEmpty(r.Keywords), nilIfEmpty(r.Entities), nilIfEmpty(r.Sources)
		r.MergedFrom, r.ReflectedFrom = nilIfEmpty(r.MergedFrom), nilIfEmpty(r.ReflectedFrom)
		return normNS(&r.Namespace, ns)
	},
	size: func(_ string, r Record) int {
		return 112 + len(r.ID) + len(r.Kind) + len(r.Content) + strLen(r.Keywords) + strLen(r.Entities) + len(r.SupersededBy) + len(r.InvalidReason) +
			sourcesLen(r.Sources) + refsLen(r.MergedFrom) + refsLen(r.ReflectedFrom)
	},
}

var entityKind = kind[Entity]{
	clone: func(e Entity) Entity {
		e.Aliases, e.Names = slices.Clone(e.Aliases), slices.Clone(e.Names)
		for i := range e.Names {
			e.Names[i].Origins = slices.Clone(e.Names[i].Origins)
		}
		return e
	},
	normalize: func(e *Entity, ns []string) bool {
		e.Aliases, e.Names = nilIfEmpty(e.Aliases), nilIfEmpty(e.Names)
		for i := range e.Names {
			e.Names[i].Origins = nilIfEmpty(e.Names[i].Origins)
		}
		return normNS(&e.Namespace, ns)
	},
	size: func(_ string, e Entity) int {
		n := 72 + len(e.ID) + len(e.Name) + len(e.Type) + strLen(e.Aliases)
		for _, name := range e.Names {
			n += 8 + len(name.Name)
			for _, o := range name.Origins {
				n += originLen(o)
			}
		}
		return n
	},
}

var edgeKind = kind[Edge]{
	clone: func(e Edge) Edge {
		e.Sources, e.Derivations = slices.Clone(e.Sources), slices.Clone(e.Derivations)
		return e
	},
	normalize: func(e *Edge, ns []string) bool {
		e.Sources, e.Derivations = nilIfEmpty(e.Sources), nilIfEmpty(e.Derivations)
		return normNS(&e.Namespace, ns)
	},
	size: func(_ string, e Edge) int {
		n := 96 + len(e.ID) + len(e.Subject) + len(e.Predicate) + len(e.Object) + len(e.Fact) + len(e.SupersededBy) + len(e.InvalidReason) + sourcesLen(e.Sources)
		for _, d := range e.Derivations {
			n += originLen(d.Origin) + 16 + len(d.Fact)
		}
		return n
	},
}

var communityKind = kind[Community]{
	clone: func(c Community) Community { c.Members = slices.Clone(c.Members); return c },
	normalize: func(c *Community, ns []string) bool {
		c.Members = nilIfEmpty(c.Members)
		return normNS(&c.Namespace, ns)
	},
	size: func(_ string, c Community) int {
		return 56 + len(c.ID) + strLen(c.Members) + len(c.Name) + len(c.Summary) + len(c.Fingerprint)
	},
}

var jobKind = kind[Job]{
	clone:     func(j Job) Job { return j },
	normalize: func(j *Job, ns []string) bool { return normNS(&j.Namespace, ns) },
	size:      func(_ string, j Job) int { return 64 + len(j.Key) + len(j.Session) + len(j.Status) + len(j.Error) },
}

var doneKind = kind[time.Time]{
	clone:     func(t time.Time) time.Time { return t },
	normalize: func(*time.Time, []string) bool { return true },
	size:      func(k string, _ time.Time) int { return 16 + len(k) },
}

var markerKind = kind[rewindMarker]{
	clone:     func(m rewindMarker) rewindMarker { return m },
	normalize: func(*rewindMarker, []string) bool { return true },
	size:      func(k string, m rewindMarker) int { return 40 + len(k) + len(m.Session) },
}

// logicalBytes 是 state 的逻辑大小：各对象按 kind.size 计的长度之和，MaxStateBytes 按它判断（增量提交第 7 节）。
func (s *state) logicalBytes() int {
	n := 0
	for k, v := range s.Records {
		n += recordKind.size(k, v)
	}
	for k, v := range s.Entities {
		n += entityKind.size(k, v)
	}
	for k, v := range s.Edges {
		n += edgeKind.size(k, v)
	}
	for k, v := range s.Communities {
		n += communityKind.size(k, v)
	}
	for k, v := range s.Jobs {
		n += jobKind.size(k, v)
	}
	for k, v := range s.DoneJobs {
		n += doneKind.size(k, v)
	}
	for k, v := range s.Markers {
		n += markerKind.size(k, v)
	}
	return n
}
