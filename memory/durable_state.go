package memory

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
)

// durable 存储中的布局，设计见 docs/design/MEMORY_G1_STORAGE.md 第 2、3 节。
const (
	stateVersion = "memory-state/1"
	statePlan    = "memory"
	// initialScope 是命名空间的初始作用域：meta 不存在时所有写入方都落在它上面，首写无需锁 meta。
	initialScope = "0"
	retiringID   = "memory/retiring"
)

func metaID(ns []string) string { return "memory/meta/" + store.NamespaceKey(ns) }

func scopeName(ns []string, scope string) string {
	return "memory:" + store.NamespaceKey(ns) + "#" + scope
}

// metaState 是一个命名空间的作用域状态，位于根作用域；只有 Purge 与迁移在 meta 锁内写它。
type metaState struct {
	Current   string    `json:"current"`
	Purges    int64     `json:"purges"`
	Migrating string    `json:"migrating,omitempty"`
	Migrated  time.Time `json:"migrated,omitzero"`
}

// retiringEntry 是一个已切换、待删除的作用域。
type retiringEntry struct {
	Namespace []string  `json:"namespace"`
	Since     time.Time `json:"since"`
}

// retiringState 是全局待删除作用域，键为作用域名；与 meta 在同一次 SaveAll 中提交。
type retiringState struct {
	Scopes map[string]retiringEntry `json:"scopes"`
}

// state 是一个命名空间、一个作用域的全部数据（还原后的形式）；存储为基础快照加 head 的覆盖层，见 state_head.go。
type state struct {
	Seq             int64                `json:"seq"`
	CommitID        string               `json:"commit_id"`
	NextIncarnation int64                `json:"next_incarnation"`
	LastForgetSeq   int64                `json:"last_forget_seq"`
	MigrationID     string               `json:"migration_id,omitempty"`
	Records         map[string]Record    `json:"records"`
	Entities        map[string]Entity    `json:"entities"`
	Edges           map[string]Edge      `json:"edges"`
	Communities     map[string]Community `json:"communities"`
	Jobs            map[string]Job       `json:"jobs"`
	DoneJobs        map[string]time.Time `json:"done_jobs"`
}

// init 补齐空映射，使修改函数不必判空。
func (s *state) init() {
	if s.Records == nil {
		s.Records = map[string]Record{}
	}
	if s.Entities == nil {
		s.Entities = map[string]Entity{}
	}
	if s.Edges == nil {
		s.Edges = map[string]Edge{}
	}
	if s.Communities == nil {
		s.Communities = map[string]Community{}
	}
	if s.Jobs == nil {
		s.Jobs = map[string]Job{}
	}
	if s.DoneJobs == nil {
		s.DoneJobs = map[string]time.Time{}
	}
}

// errScopeChanged 表示锁住的作用域已不是当前作用域（期间发生了 Purge），这次写入被放弃。
var errScopeChanged = agentErrors.New(agentErrors.CodeConflict, "长期记忆的作用域已改变（期间发生了 Purge）").WithComponent("memory")

// scopeRemoved 把作用域删除统一视为作用域改变：两者都说明期间发生了 Purge。
func scopeRemoved(err error) error {
	if errors.Is(err, agentErrors.ErrScopeRemoved) {
		return errScopeChanged
	}
	return err
}

// loadJSON 读取 id 的快照并解码；不存在时返回 false。
func loadJSON(ctx context.Context, st durable.Store, id string, v any) (bool, []byte, error) {
	snap, err := st.Load(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if snap.Version != stateVersion || snap.Plan != statePlan {
		return false, nil, agentErrors.Newf(agentErrors.CodeStateLoad, "长期记忆快照 %q 的版本 %q 无法识别，需要升级", id, snap.Version).WithComponent("memory").WithOperation("load")
	}
	if err := json.Unmarshal(snap.Data, v); err != nil {
		return false, nil, fmt.Errorf("长期记忆快照 %q：%w", id, err)
	}
	return true, snap.Data, nil
}

// encodeSnapshot 编码 meta、retiring 等小快照；state 的编码见 encodeState。
func encodeSnapshot(id string, v any) (*durable.Snapshot, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &durable.Snapshot{Version: stateVersion, Plan: statePlan, RunID: id, Data: data}, nil
}

// scoped 返回命名空间 ns 的作用域 scope 的存储视图。
func (m *Memory) scoped(ns []string, scope string) durable.Store {
	return m.cfg.Durable.(durable.Scoped).Scope(scopeName(ns, scope))
}

// loadMeta 读取命名空间的作用域状态；不存在时为初始作用域。
func (m *Memory) loadMeta(ctx context.Context, ns []string) (metaState, error) {
	meta := metaState{Current: initialScope}
	_, _, err := loadJSON(ctx, m.cfg.Durable, metaID(ns), &meta)
	return meta, err
}

// mutateOptions 是 mutate 的可选行为。
type mutateOptions struct {
	// beforeSave 在持有 head 锁、修改之前运行，可以做加锁 I/O（只用于登记 queue）。
	beforeSave func(ctx context.Context, t *tx) error
	// shrinking 为真时不受 MaxStateBytes 限制：删除类操作必须总能提交。
	shrinking bool
	// opKey 是幂等键：已提交过同一个键时不再修改（增量提交 5.3）。
	opKey string
}

type mutateOption func(*mutateOptions)

func withBeforeSave(fn func(context.Context, *tx) error) mutateOption {
	return func(o *mutateOptions) { o.beforeSave = fn }
}

func shrinking() mutateOption { return func(o *mutateOptions) { o.shrinking = true } }

func withOpKey(key string) mutateOption { return func(o *mutateOptions) { o.opKey = key } }

// mutateRetries 是锁丢失（ErrLeaseLost）后的总尝试次数（重做 2 次）。
const mutateRetries = 3

// mutate 在 head 锁内修改命名空间 ns 当前作用域的 state 并提交，返回提交后的 state（与缓存共享，只读）。
// expectScope 非空时要求当前作用域与之相同。加锁后重新 Load(head) 并按 CommitID 核对缓存，不会在其他进程已提交的旧版本上修改。
// fn 只能经 tx 读写，不得做 I/O，可能被调用多次（锁丢失时整体重做），必须是纯函数。
// 作用域改变或被删除时返回 errScopeChanged；提交结果无法判断时返回 ErrMemoryCommitUnknown。
// 提交成功后、释放 head 锁之后，覆盖层达到条件时压缩（增量提交 4.1）：达到硬上限时同步等待，否则只尝试一次。
func (m *Memory) mutate(ctx context.Context, ns []string, expectScope string, fn func(*tx) error, opts ...mutateOption) (state, error) {
	ns = slices.Clone(ns) // 缓存中的对象共享 ns，不能引用调用方可能修改的切片
	var o mutateOptions
	for _, opt := range opts {
		opt(&o)
	}
	for attempt := 1; ; attempt++ {
		s, h, err := m.mutateOnce(ctx, ns, expectScope, fn, o)
		if errors.Is(err, agentErrors.ErrLeaseLost) && attempt < mutateRetries {
			continue
		}
		if err != nil {
			return s, err
		}
		// 提交已经生效；压缩失败只计数，由下一次写入或 Maintain 重试。
		if hard := h.OverlayBytes >= m.overlayHard; hard || m.needsCompaction(h) {
			if cerr := m.compact(ctx, ns, false, hard); cerr != nil {
				m.compactErrors.Add(1)
			}
		}
		return s, nil
	}
}

func (m *Memory) mutateOnce(ctx context.Context, ns []string, expectScope string, fn func(*tx) error, o mutateOptions) (state, head, error) {
	meta, err := m.loadMeta(ctx, ns)
	if err != nil {
		return state{}, head{}, err
	}
	scope := meta.Current
	if expectScope != "" && expectScope != scope {
		return state{}, head{}, errScopeChanged
	}
	st := m.scoped(ns, scope)
	unlock, err := st.Lock(ctx, headID)
	if err != nil {
		return state{}, head{}, scopeRemoved(err)
	}
	defer func() { _ = unlock() }()
	// 加锁之后再核对：锁住的作用域可能在加锁期间被 Purge 切走，不依赖墓碑保留多久。
	if meta, err = m.loadMeta(ctx, ns); err != nil {
		return state{}, head{}, err
	}
	if meta.Current != scope {
		return state{}, head{}, errScopeChanged
	}
	h, err := loadHead(ctx, st)
	if err != nil {
		return state{}, head{}, scopeRemoved(err)
	}
	// 持 head 锁时活动槽位不会被改写（压缩只写非活动槽位，切换需要 head 锁）。
	v, err := m.materialize(ctx, st, ns, scope, h)
	if err != nil {
		return state{}, head{}, scopeRemoved(err)
	}
	if o.opKey != "" {
		if _, done := h.Ops[o.opKey]; done {
			return v.cur, h, nil
		}
	}
	t := newTx(v.cur, v.base, ns)
	if o.beforeSave != nil {
		if err := o.beforeSave(ctx, t); err != nil {
			return state{}, head{}, err
		}
	}
	if err := fn(t); err != nil {
		return state{}, head{}, err
	}
	if t.err != nil {
		return state{}, head{}, t.err
	}
	nh := h
	nh.stateScalars = t.scalars
	nh.Seq, nh.CommitID = h.Seq+1, uuid.NewString()
	nh.LogicalBytes = h.LogicalBytes + t.logical
	if limit := m.cfg.MaxStateBytes; !o.shrinking && nh.LogicalBytes > limit && nh.LogicalBytes > h.LogicalBytes {
		return state{}, head{}, agentErrors.Newf(agentErrors.CodeStateSave, "长期记忆的数据量 %d 字节超过上限 %d：调小 Retention.MaxRecords 或缩短保留期", nh.LogicalBytes, limit).WithComponent("memory").WithOperation("mutate")
	}
	ov := v.ov.merge(t.overlay(nh.Seq))
	if ov.erasing() && nh.ErasePending.IsZero() {
		nh.ErasePending = m.now()
	}
	if o.opKey != "" {
		nh.Ops = m.recordOp(h.Ops, o.opKey, nh.Seq)
	}
	if nh.Overlay, nh.OverlayBytes, err = encodeOverlay(ov); err != nil {
		return state{}, head{}, err
	}
	nh.OverlayItems = ov.items()
	cur := t.working()
	cur.Seq, cur.CommitID = nh.Seq, nh.CommitID
	key := store.NamespaceKey(ns)
	// 提交不随调用方取消中断（取消发生在写入途中时结果同样未知），但有时间上限：存储挂起时不能无限持锁，超时按结果未知处理。
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.commitTimeout)
	defer cancel()
	if err := m.commitHead(commitCtx, st, nh); err != nil {
		if errors.Is(err, agentErrors.ErrMemoryCommitUnknown) {
			m.cache.drop(key)
		}
		return state{}, head{}, err
	}
	nv := view{cur: cur, base: v.base, baseIdx: v.baseIdx, ov: ov, idx: newRecallIndex(v.baseIdx, ov)}
	m.cache.put(nv.entry(key, scope, h.Base, nh.CommitID, nh.OverlayBytes))
	return cur, nh, nil
}

// recordOp 返回加入幂等键 key 之后的集合：只保留 opsTTL 内的、最近 opsKeep 个。不修改 ops（它属于缓存中的 head）。
func (m *Memory) recordOp(ops map[string]opMark, key string, seq int64) map[string]opMark {
	now := m.now()
	out := make(map[string]opMark, len(ops)+1)
	for k, v := range ops {
		if now.Sub(v.At) < opsTTL {
			out[k] = v
		}
	}
	out[key] = opMark{Seq: seq, At: now}
	if len(out) > opsKeep {
		keys := slices.SortedFunc(maps.Keys(out), func(a, b string) int { return cmp.Compare(out[a].Seq, out[b].Seq) })
		for _, k := range keys[:len(out)-opsKeep] {
			delete(out, k)
		}
	}
	return out
}

// clone 复制各映射，值按结构体复制；值内的切片仍与原 state 共享（tx 的读写都经拷贝，修改函数改不到它们）。
func (s state) clone() state {
	s.Records, s.Entities, s.Edges = maps.Clone(s.Records), maps.Clone(s.Entities), maps.Clone(s.Edges)
	s.Communities, s.Jobs, s.DoneJobs = maps.Clone(s.Communities), maps.Clone(s.Jobs), maps.Clone(s.DoneJobs)
	s.init()
	return s
}

// readAttempts 是 read 在作用域改变（Purge）或基础快照被改写（压缩）时的重读次数上限。
const readAttempts = 3

// read 不加锁地读取命名空间 ns 当前作用域的 state：Load(meta) → Load(head) →（还原）→ Load(meta)，两次作用域相同才采用，
// 避免在 Purge 提交前读 meta、提交后读 state，从而在 Purge 返回之后仍读到旧作用域的数据。state 不存在或作用域已删除时为空。
// 返回的 state 与缓存及其他读者共享，只读。
func (m *Memory) read(ctx context.Context, ns []string) (string, state, error) {
	scope, v, err := m.readView(ctx, ns)
	return scope, v.cur, err
}

// readView 与 read 相同，另返回召回的查询结构。
func (m *Memory) readView(ctx context.Context, ns []string) (string, view, error) {
	ns = slices.Clone(ns)
	for range readAttempts {
		meta, err := m.loadMeta(ctx, ns)
		if err != nil {
			return "", view{}, err
		}
		v, err := m.readScope(ctx, ns, meta.Current)
		if errors.Is(err, errBaseChanged) {
			continue
		}
		if err != nil {
			return "", view{}, err
		}
		after, err := m.loadMeta(ctx, ns)
		if err != nil {
			return "", view{}, err
		}
		if after.Current == meta.Current {
			v.cur.init()
			return meta.Current, v, nil
		}
	}
	return "", view{}, agentErrors.Newf(agentErrors.CodeConflict, "长期记忆的作用域或基础快照在读取期间反复改变").WithComponent("memory").WithOperation("read")
}

func (m *Memory) readScope(ctx context.Context, ns []string, scope string) (view, error) {
	st := m.scoped(ns, scope)
	h, err := loadHead(ctx, st)
	if err == nil {
		var v view
		if v, err = m.materialize(ctx, st, ns, scope, h); err == nil {
			return v, nil
		}
	}
	if errors.Is(err, agentErrors.ErrScopeRemoved) {
		return view{}, nil
	}
	return view{}, err
}
