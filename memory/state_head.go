package memory

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/gob"
	"errors"
	"fmt"
	"maps"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
)

// 增量提交的存储布局与协议，设计见 docs/design/MEMORY_G1_INCREMENTAL.md 第 2～4 节：
// head 是唯一的提交点，含标量、基础快照指针与按键合并的覆盖层；基础快照在两个槽位间轮换，只由压缩者写入非活动槽位。
const (
	headVersion = "memory-head/3"
	headV2      = "memory-head/2" // 与 /3 格式相同，可读取
	headID      = "memory/head"
	compactID   = "memory/compact"
)

func baseID(slot int) string { return fmt.Sprintf("memory/base/%d", slot) }

// 压缩的触发条件（增量提交 4.1）；Memory 上的同名字段可在测试中调小。
const (
	overlaySoftBytes = 256 << 10
	overlayHardBytes = 1 << 20
	overlayMaxItems  = 2000
	// baseShareDiv：覆盖层达到基础快照解压后大小的 1/20（5%）时压缩。
	baseShareDiv = 20
	// opsKeep、opsTTL 是幂等键的保留条数与时长。
	opsKeep = 256
	opsTTL  = 24 * time.Hour
)

// overlay 是自基础快照以来的变更，按 (类型, 键) 合并。
type overlay struct {
	Records     map[string]change[Record]
	Entities    map[string]change[Entity]
	Edges       map[string]change[Edge]
	Communities map[string]change[Community]
	Jobs        map[string]change[Job]
	DoneJobs    map[string]change[time.Time]
}

func (o overlay) items() int {
	return len(o.Records) + len(o.Entities) + len(o.Edges) + len(o.Communities) + len(o.Jobs) + len(o.DoneJobs)
}

// merge 返回 o ⊕ d（d 中的键覆盖 o）；不修改 o，o 可能与缓存共享。
func (o overlay) merge(d overlay) overlay {
	return overlay{
		Records: mergeMap(o.Records, d.Records), Entities: mergeMap(o.Entities, d.Entities), Edges: mergeMap(o.Edges, d.Edges),
		Communities: mergeMap(o.Communities, d.Communities), Jobs: mergeMap(o.Jobs, d.Jobs), DoneJobs: mergeMap(o.DoneJobs, d.DoneJobs),
	}
}

func mergeMap[T any](a, b map[string]change[T]) map[string]change[T] {
	out := make(map[string]change[T], len(a)+len(b))
	maps.Copy(out, a)
	for k, c := range b {
		// 合并时保留擦除标记：基础快照中的旧内容直到压缩才消失。
		if prev, ok := out[k]; ok && prev.Erase {
			c.Erase = true
		}
		out[k] = c
	}
	return out
}

// after 返回 Seq > seq 的变更：压缩时保留期间新提交的部分。
func (o overlay) after(seq int64) overlay {
	return overlay{Records: filterSeq(o.Records, seq), Entities: filterSeq(o.Entities, seq), Edges: filterSeq(o.Edges, seq),
		Communities: filterSeq(o.Communities, seq), Jobs: filterSeq(o.Jobs, seq), DoneJobs: filterSeq(o.DoneJobs, seq)}
}

func filterSeq[T any](m map[string]change[T], seq int64) map[string]change[T] {
	out := map[string]change[T]{}
	for k, c := range m {
		if c.Seq > seq {
			out[k] = c
		}
	}
	return out
}

// erasing 报告覆盖层中是否有须从基础快照中擦除的变更。
func (o overlay) erasing() bool {
	return anyErase(o.Records) || anyErase(o.Entities) || anyErase(o.Edges) || anyErase(o.Communities) || anyErase(o.Jobs) || anyErase(o.DoneJobs)
}

func anyErase[T any](m map[string]change[T]) bool {
	for _, c := range m {
		if c.Erase {
			return true
		}
	}
	return false
}

// apply 返回 base ⊕ o：复制 base 的映射后应用变更，base 不变。
func (o overlay) apply(base state) state {
	s := base.clone()
	applyMap(s.Records, o.Records)
	applyMap(s.Entities, o.Entities)
	applyMap(s.Edges, o.Edges)
	applyMap(s.Communities, o.Communities)
	applyMap(s.Jobs, o.Jobs)
	applyMap(s.DoneJobs, o.DoneJobs)
	return s
}

func applyMap[T any](m map[string]T, changes map[string]change[T]) {
	for k, c := range changes {
		if c.Deleted {
			delete(m, k)
		} else {
			m[k] = c.Value
		}
	}
}

// setNamespace 与 stripNamespace：对象的 Namespace 不写入覆盖层，解码时补回（与基础快照相同，见 G1 3.4）。
func (o overlay) withNamespace(ns []string) overlay {
	return overlay{
		Records:     mapValues(o.Records, func(v *Record) { v.Namespace = ns }),
		Entities:    mapValues(o.Entities, func(v *Entity) { v.Namespace = ns }),
		Edges:       mapValues(o.Edges, func(v *Edge) { v.Namespace = ns }),
		Communities: mapValues(o.Communities, func(v *Community) { v.Namespace = ns }),
		Jobs:        mapValues(o.Jobs, func(v *Job) { v.Namespace = ns }),
		DoneJobs:    o.DoneJobs,
	}
}

func mapValues[T any](m map[string]change[T], f func(*T)) map[string]change[T] {
	out := make(map[string]change[T], len(m))
	for k, c := range m {
		if !c.Deleted {
			f(&c.Value)
		}
		out[k] = c
	}
	return out
}

func encodeOverlay(o overlay) (string, int, error) {
	if o.items() == 0 {
		return "", 0, nil
	}
	var plain bytes.Buffer
	if err := gob.NewEncoder(&plain).Encode(o.withNamespace(nil)); err != nil {
		return "", 0, err
	}
	return base64.StdEncoding.EncodeToString(zstdEncoder.EncodeAll(plain.Bytes(), nil)), plain.Len(), nil
}

func decodeOverlay(h head, ns []string) (overlay, error) {
	var o overlay
	if h.Overlay == "" {
		if h.OverlayBytes != 0 {
			return overlay{}, badFraming("覆盖层为空但长度不为零")
		}
		return o.withNamespace(ns), nil
	}
	packed, err := base64.StdEncoding.DecodeString(h.Overlay)
	if err != nil {
		return overlay{}, corruptState(err)
	}
	plain, err := decompress(packed, h.OverlayBytes)
	if err != nil {
		return overlay{}, corruptState(err)
	}
	if err := gob.NewDecoder(bytes.NewReader(plain)).Decode(&o); err != nil {
		return overlay{}, corruptState(err)
	}
	if o.items() != h.OverlayItems {
		return overlay{}, badFraming("覆盖层的项数与 head 不符")
	}
	for _, seqs := range [][]int64{seqsOf(o.Records), seqsOf(o.Entities), seqsOf(o.Edges), seqsOf(o.Communities), seqsOf(o.Jobs), seqsOf(o.DoneJobs)} {
		for _, seq := range seqs {
			if seq <= h.Base.Seq || seq > h.Seq {
				return overlay{}, badFraming("覆盖层的变更不在 (base.seq, head.seq] 之内")
			}
		}
	}
	return o.withNamespace(ns), nil
}

func seqsOf[T any](m map[string]change[T]) []int64 {
	out := make([]int64, 0, len(m))
	for _, c := range m {
		out = append(out, c.Seq)
	}
	return out
}

// baseRef 指向 head 所依据的基础快照；CommitID 为空表示没有基础快照（空 state）。
type baseRef struct {
	Slot     int    `json:"slot"`
	Seq      int64  `json:"seq"`
	CommitID string `json:"commit_id,omitempty"`
	RawBytes int    `json:"raw_bytes"`
}

// opMark 记录一个幂等键提交时的 Seq 与时间。
type opMark struct {
	Seq int64     `json:"seq"`
	At  time.Time `json:"at"`
}

// head 是提交点（增量提交 2.1）。字段顺序即 JSON 顺序：小字段在前，覆盖层在最后。
type head struct {
	stateScalars
	Base         baseRef           `json:"base"`
	LogicalBytes int               `json:"logical_bytes"`
	OverlayBytes int               `json:"overlay_bytes"`
	OverlayItems int               `json:"overlay_items"`
	ErasePending time.Time         `json:"erase_pending_since,omitzero"`
	StaleSlot    *int              `json:"stale_slot,omitempty"` // 已切换走、尚未擦除的旧槽位
	Ops          map[string]opMark `json:"ops,omitempty"`
	Overlay      string            `json:"overlay,omitempty"`
}

// loadHead 读取 head；不存在时为空 head。
func loadHead(ctx context.Context, st durable.Store) (head, error) {
	snap, err := st.Load(ctx, headID)
	if errors.Is(err, os.ErrNotExist) {
		return head{}, nil
	}
	if err != nil {
		return head{}, err
	}
	if (snap.Version != headVersion && snap.Version != headV2) || snap.Plan != statePlan {
		return head{}, agentErrors.Newf(agentErrors.CodeStateLoad, "长期记忆 head 的版本 %q 无法识别，需要升级", snap.Version).WithComponent("memory").WithOperation("load")
	}
	var h head
	if err := json.Unmarshal(snap.Data, &h); err != nil {
		return head{}, corruptState(err)
	}
	return h, nil
}

func saveHead(ctx context.Context, st durable.Store, h head) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return st.Save(ctx, headID, &durable.Snapshot{Version: headVersion, Plan: statePlan, RunID: headID, Data: data})
}

// errBaseChanged 表示 head 引用的基础快照已被后来的压缩改写，读者从 Load(head) 重来。
var errBaseChanged = agentErrors.New(agentErrors.CodeConflict, "长期记忆的基础快照在读取期间被压缩改写").WithComponent("memory")

// view 是还原出的 state 及其来源：基础快照与覆盖层，以及召回的查询结构（都可能与缓存共享，只读）。
type view struct {
	cur     state
	base    state
	baseIdx *lazyIndex
	deps    *depIndex
	ov      overlay
	idx     *recallIndex
}

func (v view) entry(key, scope string, ref baseRef, headCommit string, ovRaw int) *cacheEntry {
	return &cacheEntry{key: key, scope: scope, ref: ref, base: v.base, baseIdx: v.baseIdx, deps: v.deps, headCommit: headCommit, cur: v.cur, ov: v.ov, ovRaw: ovRaw, idx: v.idx}
}

// materialize 还原 head 对应的 state（增量提交 3.1）：结果只由（基础快照, head）决定，缓存命中时不解码。
// 同一作用域、同一提交的并发未命中只解码一次；解码不随某一个调用方取消，各调用方仍可各自因 ctx 结束而返回。
func (m *Memory) materialize(ctx context.Context, st durable.Store, ns []string, scope string, h head) (view, error) {
	key := store.NamespaceKey(ns)
	if e, ok := m.cache.head(key, scope, h.CommitID); ok {
		return view{cur: e.cur, base: e.base, baseIdx: e.baseIdx, deps: e.deps, ov: e.ov, idx: e.idx}, nil
	}
	ch := m.decodes.DoChan(key+"#"+scope+"#"+h.CommitID, func() (any, error) {
		start := time.Now()
		v, err := m.decode(context.WithoutCancel(ctx), st, ns, scope, h)
		if err == nil {
			m.cache.decoded(time.Since(start))
			m.cache.put(v.entry(key, scope, h.Base, h.CommitID, h.OverlayBytes))
		}
		return v, err
	})
	select {
	case r := <-ch:
		if r.Err != nil {
			return view{}, r.Err
		}
		return r.Val.(view), nil
	case <-ctx.Done():
		return view{}, ctx.Err()
	}
}

// decode 读取并解码基础快照（缓存中有时复用），叠加 head 的覆盖层。
func (m *Memory) decode(ctx context.Context, st durable.Store, ns []string, scope string, h head) (view, error) {
	base, baseIdx, deps, err := m.loadBase(ctx, st, ns, scope, h.Base)
	if err != nil {
		return view{}, err
	}
	ov, err := decodeOverlay(h, ns)
	if err != nil {
		return view{}, err
	}
	cur := base
	if ov.items() > 0 {
		cur = ov.apply(base)
	}
	cur.Seq, cur.CommitID, cur.NextIncarnation, cur.LastForgetSeq, cur.MigrationID = h.Seq, h.CommitID, h.NextIncarnation, h.LastForgetSeq, h.MigrationID
	return view{cur: cur, base: base, baseIdx: baseIdx, deps: deps, ov: ov, idx: newRecallIndex(baseIdx, ov)}, nil
}

// loadBase 返回 ref 指向的已解码基础快照、它的召回索引（解码时在后台开始构建）与反向依赖；槽位的 commit_id 与 ref 不同时返回 errBaseChanged。
func (m *Memory) loadBase(ctx context.Context, st durable.Store, ns []string, scope string, ref baseRef) (state, *lazyIndex, *depIndex, error) {
	if ref.CommitID == "" {
		var s state
		s.init()
		return s, nil, newDepIndex(s), nil
	}
	if e, ok := m.cache.base(store.NamespaceKey(ns), scope, ref.CommitID); ok {
		return e.base, e.baseIdx, e.deps, nil
	}
	snap, err := st.Load(ctx, baseID(ref.Slot))
	if errors.Is(err, os.ErrNotExist) {
		return state{}, nil, nil, errBaseChanged
	}
	if err != nil {
		return state{}, nil, nil, err
	}
	if (snap.Version != stateBlobVersion && snap.Version != stateBlobV2) || snap.Plan != statePlan {
		return state{}, nil, nil, agentErrors.Newf(agentErrors.CodeStateLoad, "长期记忆基础快照的版本 %q 无法识别，需要升级", snap.Version).WithComponent("memory").WithOperation("load")
	}
	blob, err := decodeBlob(snap.Data)
	if err != nil {
		return state{}, nil, nil, err
	}
	if blob.CommitID != ref.CommitID {
		return state{}, nil, nil, errBaseChanged
	}
	s, err := decodeState(blob, ns)
	if err != nil {
		return state{}, nil, nil, err
	}
	s.init()
	return s, newLazyIndex(s.Records, m.cache != nil), newDepIndex(s), nil
}

// needsCompaction 报告 head 是否达到压缩条件（增量提交 4.1）。
func (m *Memory) needsCompaction(h head) bool {
	return h.OverlayBytes >= m.overlaySoft || h.OverlayBytes >= m.overlayHard ||
		(m.baseShare > 0 && h.Base.RawBytes > 0 && h.OverlayBytes > 0 && h.OverlayBytes*m.baseShare >= h.Base.RawBytes) ||
		h.OverlayItems >= m.overlayItems ||
		(!h.ErasePending.IsZero() && m.now().Sub(h.ErasePending) >= m.eraseWithin) ||
		h.StaleSlot != nil
}

// scrubbed 是擦除后写入旧槽位的占位快照：空 state，commit_id 与任何 head 都不匹配。
func scrubbedBase() (*durable.Snapshot, error) {
	var s state
	s.init()
	s.CommitID = "scrubbed"
	data, _, err := encodeState(&s)
	if err != nil {
		return nil, err
	}
	return &durable.Snapshot{Version: stateBlobVersion, Plan: statePlan, RunID: "memory/base", Data: data}, nil
}

// compactLockWait 是常规压缩等待压缩锁的时间：拿不到说明已有压缩者，跳过。
const compactLockWait = 200 * time.Millisecond

// compact 压缩命名空间 ns 当前作用域的覆盖层（增量提交 4.2）：耗时的基础快照写入不持 head 锁，只在切换 head 时短暂持锁。
// force 为真时不检查触发条件；wait 为真时等待压缩锁（硬上限的同步压缩）。
func (m *Memory) compact(ctx context.Context, ns []string, force, wait bool) error {
	meta, err := m.loadMeta(ctx, ns)
	if err != nil {
		return err
	}
	scope := meta.Current
	st := m.scoped(ns, scope)
	lockCtx, cancel := ctx, context.CancelFunc(func() {})
	if !wait {
		lockCtx, cancel = context.WithTimeout(ctx, compactLockWait)
	}
	unlockC, err := st.Lock(lockCtx, compactID)
	cancel()
	if err != nil {
		if errors.Is(err, agentErrors.ErrLockUnavailable) && !wait {
			return nil
		}
		return scopeRemoved(err)
	}
	defer func() { _ = unlockC() }()

	h0, err := loadHead(ctx, st)
	if err != nil {
		return scopeRemoved(err)
	}
	if !force && !m.needsCompaction(h0) {
		return nil
	}
	v0, err := m.materialize(ctx, st, ns, scope, h0)
	if err != nil {
		return scopeRemoved(err)
	}
	commitCtx, cancelCommit := context.WithTimeout(context.WithoutCancel(ctx), m.commitTimeout)
	defer cancelCommit()

	// 写新基础快照到非活动槽位：持有压缩锁，没有其他写入方会写槽位；活动槽位只在 head 切换走之后才会被改写。
	newSlot := 0
	if h0.Base.CommitID != "" {
		newSlot = 1 - h0.Base.Slot
	}
	if h0.OverlayItems > 0 || h0.Base.CommitID == "" {
		nb := v0.cur
		nb.Seq, nb.CommitID = h0.Seq, uuid.NewString()
		data, raw, err := encodeState(&nb)
		if err != nil {
			return err
		}
		if err := saveSlot(ctx, commitCtx, st, newSlot, &durable.Snapshot{Version: stateBlobVersion, Plan: statePlan, RunID: baseID(newSlot), Data: data}); err != nil {
			return scopeRemoved(err)
		}
		ref := baseRef{Slot: newSlot, Seq: h0.Seq, CommitID: nb.CommitID, RawBytes: raw}
		if err := m.switchBase(ctx, commitCtx, st, ns, scope, h0, ref, nb); err != nil {
			return err
		}
	}
	// 擦除切换走的旧槽位，再清除标记。
	return m.scrubStale(ctx, commitCtx, st, ns, scope)
}

// saveSlot 持槽位自己的锁写入（存储契约：Save 只在持有该 ID 的锁时调用；pgstore 会校验）。
// 加锁顺序：memory/compact → memory/base/N（写完即释放）→ memory/head；读者不加锁、写入方只持 head 锁，不会成环。
func saveSlot(ctx, commitCtx context.Context, st durable.Store, slot int, snap *durable.Snapshot) error {
	unlock, err := st.Lock(ctx, baseID(slot))
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()
	return st.Save(commitCtx, baseID(slot), snap)
}

// switchBase 在 head 锁内把 head 切到新基础快照，保留压缩期间新提交的变更。
func (m *Memory) switchBase(ctx, commitCtx context.Context, st durable.Store, ns []string, scope string, h0 head, ref baseRef, nb state) error {
	unlock, err := st.Lock(ctx, headID)
	if err != nil {
		return scopeRemoved(err)
	}
	defer func() { _ = unlock() }()
	if meta, err := m.loadMeta(ctx, ns); err != nil || meta.Current != scope {
		return errors.Join(err, errScopeChanged)
	}
	h1, err := loadHead(ctx, st)
	if err != nil {
		return scopeRemoved(err)
	}
	if h1.Base != h0.Base {
		return agentErrors.New(agentErrors.CodeConflict, "长期记忆的基础快照在压缩期间被切换").WithComponent("memory").WithOperation("compact")
	}
	v1, err := m.materialize(ctx, st, ns, scope, h1)
	if err != nil {
		return scopeRemoved(err)
	}
	kept := v1.ov.after(h0.Seq)
	nh := h1
	nh.CommitID = uuid.NewString()
	nh.Base = ref
	nh.LogicalBytes = v1.cur.logicalBytes()
	if h0.Base.CommitID != "" {
		old := h0.Base.Slot
		nh.StaleSlot = &old
	}
	nh.ErasePending = time.Time{}
	if kept.erasing() {
		nh.ErasePending = m.now()
	}
	if nh.Overlay, nh.OverlayBytes, err = encodeOverlay(kept); err != nil {
		return err
	}
	nh.OverlayItems = kept.items()
	if err := m.commitHead(commitCtx, st, nh); err != nil {
		return err
	}
	m.compactions.Add(1)
	cur := v1.cur
	cur.CommitID = nh.CommitID
	nb.init()
	baseIdx := newLazyIndex(nb.Records, m.cache != nil)
	nv := view{cur: cur, base: nb, baseIdx: baseIdx, deps: newDepIndex(nb), ov: kept.withNamespace(ns), idx: newRecallIndex(baseIdx, kept)}
	m.cache.put(nv.entry(store.NamespaceKey(ns), scope, ref, nh.CommitID, nh.OverlayBytes))
	return nil
}

// scrubStale 擦除 head 标记的旧槽位并清除标记；没有标记时什么也不做。
func (m *Memory) scrubStale(ctx, commitCtx context.Context, st durable.Store, ns []string, scope string) error {
	h, err := loadHead(ctx, st)
	if err != nil {
		return scopeRemoved(err)
	}
	if h.StaleSlot == nil || *h.StaleSlot == h.Base.Slot {
		return nil
	}
	slot := *h.StaleSlot
	placeholder, err := scrubbedBase()
	if err != nil {
		return err
	}
	// 旧槽位不被 head 引用；只有持压缩锁者写槽位。
	if err := saveSlot(ctx, commitCtx, st, slot, placeholder); err != nil {
		return scopeRemoved(err)
	}
	unlock, err := st.Lock(ctx, headID)
	if err != nil {
		return scopeRemoved(err)
	}
	defer func() { _ = unlock() }()
	if meta, err := m.loadMeta(ctx, ns); err != nil || meta.Current != scope {
		return errors.Join(err, errScopeChanged)
	}
	h, err = loadHead(ctx, st)
	if err != nil {
		return scopeRemoved(err)
	}
	if h.StaleSlot == nil || *h.StaleSlot != slot {
		return nil
	}
	prev := h.CommitID
	h.StaleSlot = nil
	h.CommitID = uuid.NewString()
	if err := m.commitHead(commitCtx, st, h); err != nil {
		return err
	}
	key := store.NamespaceKey(ns)
	if e, ok := m.cache.head(key, scope, prev); ok {
		ne := *e
		ne.headCommit, ne.cur.CommitID = h.CommitID, h.CommitID
		m.cache.put(&ne)
	}
	return nil
}

// commitHead 保存 head 并判定结果（与 mutate 第 7 步相同的错误分类）。
func (m *Memory) commitHead(commitCtx context.Context, st durable.Store, h head) error {
	err := saveHead(commitCtx, st, h)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, agentErrors.ErrLeaseLost):
		return err
	case errors.Is(err, agentErrors.ErrScopeRemoved):
		return errScopeChanged
	}
	if got, lerr := loadHead(commitCtx, st); lerr == nil && got.CommitID == h.CommitID {
		return nil
	}
	// 读不到本次的 CommitID 也不能断定失败：服务端可能稍后才完成提交。
	return fmt.Errorf("%w：%w", agentErrors.ErrMemoryCommitUnknown, err)
}
