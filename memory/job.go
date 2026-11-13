package memory

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
)

// JobStatus 是抽取任务的状态。
type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
	// JobDropped 表示任务没有处理就被丢弃：处理前或处理中发生了遗忘（LastForgetSeq 大于 TurnSeq）或 Purge。
	JobDropped JobStatus = "dropped"
)

// Job 是一轮结束后登记的抽取任务，存于所属命名空间当前作用域的 state。Key 为"会话/turn/start 序号"，重复登记不产生新任务。
// Aborted 的轮次只提炼教训（reflection），其余轮次抽取事实与情景。
// 完成的任务只保留键与完成时间（Job 返回的 Status 为 JobDone）；失败与丢弃的任务保留错误信息，二者都由 Prune 在保留期后删除。
type Job struct {
	Key       string   `json:"key"`
	Namespace []string `json:"namespace"`
	Session   string   `json:"session"`
	From      int64    `json:"from"`
	To        int64    `json:"to"`
	Aborted   bool     `json:"aborted,omitzero"`
	// Scope 与 TurnSeq 取自轮次绑定（TurnMemory.Scope、Seq）：任务只写入登记时的作用域，
	// 登记之后作用域内发生过遗忘（LastForgetSeq > TurnSeq）时丢弃，避免重新学回被遗忘的内容。
	Scope     string    `json:"scope,omitempty"`
	TurnSeq   int64     `json:"turn_seq,omitzero"`
	Status    JobStatus `json:"status"`
	Attempts  int       `json:"attempts,omitzero"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (j Job) source() Source { return Source{Session: j.Session, From: j.From, To: j.To, Job: j.Key} }

// errJobStale 表示任务登记之后作用域内发生了遗忘，任务被丢弃。
var errJobStale = agentErrors.New(agentErrors.CodeConflict, "抽取任务登记之后发生了遗忘，任务被丢弃").WithComponent("memory")

// Enqueue 登记一轮的抽取任务；同一 Key 已登记时不做任何事。Builder 在轮次结束（含中止）时调用。
// 没有作用域（G0 的绑定）、作用域已不是当前的（跨越了 Purge）或 state 已达 MaxStateBytes 时丢弃任务、计入 DroppedJobs
// 并返回 nil：已完成轮次的回放会再次登记，不能报错（设计 3.2、4.3、4.5）。
func (m *Memory) Enqueue(ctx context.Context, j Job) error {
	if j.Key == "" || j.Session == "" || j.From <= 0 || j.To < j.From {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "抽取任务需要 Key、会话与合法的序号区间").WithComponent("memory").WithOperation("enqueue")
	}
	if err := validNamespace(j.Namespace); err != nil {
		return err
	}
	if j.Scope == "" {
		m.droppedJobs.Add(1)
		return nil
	}
	ns := slices.Clone(j.Namespace)
	j.Namespace = ns
	j.Status, j.Attempts, j.Error, j.CreatedAt = JobPending, 0, "", m.now()
	known := func(t *tx) bool {
		_, pending := t.Jobs.Get(j.Key)
		_, done := t.DoneJobs.Get(j.Key)
		return pending || done
	}
	_, err := m.mutate(ctx, ns, j.Scope, func(t *tx) error {
		if known(t) {
			return errNoChange
		}
		t.Jobs.Put(j.Key, j)
		return nil
	}, withBeforeSave(func(ctx context.Context, t *tx) error {
		if known(t) {
			return nil
		}
		return m.register(ctx, queueEntry{Namespace: ns, Scope: t.scope, Reason: queueJobs})
	}))
	if errors.Is(err, errScopeChanged) || errors.Is(err, errStateFull) {
		m.droppedJobs.Add(1)
		return nil
	}
	return err
}

// Job 返回当前命名空间中一个任务的状态。
func (m *Memory) Job(ctx context.Context, key string) (Job, bool, error) {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return Job{}, false, err
	}
	_, s, err := m.read(ctx, ns)
	if err != nil {
		return Job{}, false, err
	}
	if j, ok := s.Jobs[key]; ok {
		j.Namespace = slices.Clone(j.Namespace)
		return j, true, nil
	}
	if _, ok := s.DoneJobs[key]; ok {
		return Job{Key: key, Namespace: slices.Clone(ns), Status: JobDone}, true, nil
	}
	return Job{}, false, nil
}

// DroppedJobs 返回本实例没有处理就丢弃的任务数（跨越 Purge、没有作用域、state 超限或登记后发生了遗忘）。
func (m *Memory) DroppedJobs() int64 { return m.droppedJobs.Load() }

// pendingJobs 按登记时间返回 s 中待处理的任务。
func pendingJobs(s state) []Job {
	var out []Job
	for _, j := range s.Jobs {
		if j.Status == JobPending {
			out = append(out, j)
		}
	}
	slices.SortFunc(out, func(a, b Job) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.Key, b.Key))
	})
	return out
}

// hasPending 报告 tx 中是否还有待处理的任务。
func hasPending(t *tx) bool {
	for _, k := range t.Jobs.Keys() {
		if j, _ := t.Jobs.Get(k); j.Status == JobPending {
			return true
		}
	}
	return false
}

// finishJob 在锁内读取任务并按处理结果更新状态：完成的移入 DoneJobs；失败的计入尝试次数，达到 JobAttempts 时为 failed；
// 丢弃的为 dropped。任务已不是待处理（或作用域已切换）时不修改。标记任务结束不受 MaxStateBytes 限制。
func (m *Memory) finishJob(ctx context.Context, j Job, result error) error {
	now := m.now()
	_, err := m.mutate(ctx, j.Namespace, j.Scope, func(t *tx) error {
		cur, ok := t.Jobs.Get(j.Key)
		if !ok || cur.Status != JobPending {
			return errNoChange
		}
		switch {
		case result == nil:
			t.Jobs.Delete(j.Key)
			t.DoneJobs.Put(j.Key, now)
			return nil
		case errors.Is(result, errScopeChanged) || errors.Is(result, errJobStale):
			cur.Status = JobDropped
		default:
			cur.Attempts++
			if cur.Attempts >= m.cfg.JobAttempts {
				cur.Status = JobFailed
			}
		}
		cur.Error = result.Error()
		t.Jobs.Put(j.Key, cur)
		return nil
	}, shrinking())
	if errors.Is(err, errScopeChanged) {
		return nil // 任务随旧作用域一起删除
	}
	return err
}

// ---- queue 分片（设计 4.3）----

// queueShards 是 queue 分片数：登记按命名空间散列到 memory/queue/<n>，降低 Enqueue 之间的锁竞争。
const queueShards = 16

// queueReason 是登记的原因。
type queueReason string

const (
	// queueJobs：作用域中有待处理的任务。
	queueJobs queueReason = "jobs"
	// queueIndex：Forget 提交之后可能残留向量，等待维护执行 Reindex(false)。
	queueIndex queueReason = "index"
)

// queueEntry 是 queue 分片中的一条登记。
type queueEntry struct {
	Namespace []string    `json:"namespace"`
	Scope     string      `json:"scope"`
	Reason    queueReason `json:"reason"`
}

func (e queueEntry) key() string { return string(e.Reason) + ":" + scopeName(e.Namespace, e.Scope) }

type queueState struct {
	Entries map[string]queueEntry `json:"entries"`
}

func queueShardID(i uint32) string { return fmt.Sprintf("memory/queue/%d", i) }

func queueID(ns []string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(store.NamespaceKey(ns)))
	return queueShardID(h.Sum32() % queueShards)
}

// register 登记 e：先不加锁读取分片，已登记时不写。调用方持有 e 所在作用域的 state 锁（加锁顺序 state → 分片），
// 当前作用域的登记只有持有 state 锁才能移除，所以无锁读到的登记不会在提交前消失（设计 4.3 Enqueue 第 2 步）。
func (m *Memory) register(ctx context.Context, e queueEntry) error {
	id := queueID(e.Namespace)
	var q queueState
	if _, _, err := loadJSON(ctx, m.cfg.Durable, id, &q); err != nil {
		return err
	}
	if _, ok := q.Entries[e.key()]; ok {
		return nil
	}
	return editJSON(ctx, m.cfg.Durable, id, func(q *queueState) bool {
		if _, ok := q.Entries[e.key()]; ok {
			return false
		}
		if q.Entries == nil {
			q.Entries = map[string]queueEntry{}
		}
		q.Entries[e.key()] = e
		return true
	})
}

// unregister 移除登记。当前作用域的登记须经 settle 在 state 锁内移除；非当前作用域的可以直接移除。
func (m *Memory) unregister(ctx context.Context, e queueEntry) error {
	return editJSON(ctx, m.cfg.Durable, queueID(e.Namespace), func(q *queueState) bool {
		if _, ok := q.Entries[e.key()]; !ok {
			return false
		}
		delete(q.Entries, e.key())
		return true
	})
}

// settle 在 e 的作用域的 state 锁内移除登记，keep 对锁内的 state 返回真时保留；作用域已不是当前的直接移除。
// 持有 state 锁时不会有新的任务或遗忘提交，移除之后的提交会重新登记，因此不会漏掉。
func (m *Memory) settle(ctx context.Context, e queueEntry, keep func(*tx) bool) error {
	_, err := m.mutate(ctx, e.Namespace, e.Scope, func(*tx) error { return errNoChange },
		withBeforeSave(func(ctx context.Context, t *tx) error {
			if keep != nil && keep(t) {
				return nil
			}
			return m.unregister(ctx, e)
		}))
	if errors.Is(err, errScopeChanged) {
		return m.unregister(ctx, e)
	}
	return err
}

// queued 读取全部分片的登记，按键排序。
func (m *Memory) queued(ctx context.Context) ([]queueEntry, error) {
	var out []queueEntry
	for i := range uint32(queueShards) {
		var q queueState
		if _, _, err := loadJSON(ctx, m.cfg.Durable, queueShardID(i), &q); err != nil {
			return nil, err
		}
		for _, k := range slices.Sorted(maps.Keys(q.Entries)) {
			out = append(out, q.Entries[k])
		}
	}
	return out, nil
}

// ---- 维护（设计 4.3）----

// Maintain 执行不需要模型的维护：清理 retiring（含 RetireGrace 到期后的再次清理）；移除非当前作用域的登记；
// 处理"索引待清理"登记——先在 state 锁内移除登记再执行 Reindex(false)，期间提交的 Forget 会重新登记；
// 每 ReindexEvery 轮对有登记的命名空间执行 Reindex(false)，并回收超过 MemoryTombstoneRetention 的墓碑。
// Worker 每轮先调用它；不运行 Worker 的部署须运行 RunMaintenance。各步的错误合并返回，不影响其余步骤。
func (m *Memory) Maintain(ctx context.Context) error {
	errs := []error{m.cleanupRetiring(ctx)}
	entries, err := m.queued(ctx)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	periodic := m.maintenance.Add(1)%int64(m.cfg.ReindexEvery) == 0
	reindex := map[string][]string{}
	for _, e := range entries {
		scope, err := m.currentScope(ctx, e.Namespace)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		key := store.NamespaceKey(e.Namespace)
		switch {
		case scope != e.Scope:
			// 旧作用域的数据与索引由 retiring 清理；不取 state 锁（旧作用域的 Lock 可能已返回 ErrScopeRemoved）。
			errs = append(errs, m.unregister(ctx, e))
		case e.Reason == queueIndex:
			if err := m.settle(ctx, e, nil); err != nil {
				errs = append(errs, err)
				continue
			}
			reindex[key] = e.Namespace
		case periodic:
			reindex[key] = e.Namespace
		}
	}
	for _, key := range slices.Sorted(maps.Keys(reindex)) {
		errs = append(errs, m.Reindex(WithNamespace(ctx, reindex[key]...), false))
	}
	if periodic {
		errs = append(errs, m.cfg.Durable.(durable.Scoped).PruneRemoved(ctx, constants.MemoryTombstoneRetention))
	}
	return errors.Join(errs...)
}

// RunMaintenance 每隔 interval 调用一次 Maintain，直到 ctx 结束；interval 不大于 0 时取 DefaultMemoryWorkerInterval。
// 维护的错误交给 onError（可为 nil）后等待下一轮。没有运行 Worker 的部署必须运行它。
func (m *Memory) RunMaintenance(ctx context.Context, interval time.Duration, onError func(error)) error {
	if interval <= 0 {
		interval = constants.DefaultMemoryWorkerInterval
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		if err := m.Maintain(ctx); err != nil && onError != nil && ctx.Err() == nil {
			onError(err)
		}
		timer.Reset(interval)
	}
}
