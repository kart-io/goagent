package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// lockedModel 是可被多个 Worker 并发调用的脚本模型，记录调用与最大并发数。
type lockedModel struct {
	reply  func(system, user string) (string, error)
	mu     sync.Mutex
	calls  []string
	active atomic.Int32
	peak   atomic.Int32
}

func (m *lockedModel) Provider() constants.Provider { return constants.Provider("fake") }
func (m *lockedModel) CompleteStream(_ context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	n := m.active.Add(1)
	defer m.active.Add(-1)
	for p := m.peak.Load(); n > p && !m.peak.CompareAndSwap(p, n); p = m.peak.Load() {
	}
	user := req.Messages[len(req.Messages)-1].Content
	m.mu.Lock()
	m.calls = append(m.calls, user)
	m.mu.Unlock()
	text, err := m.reply(req.Messages[0].Content, user)
	if err != nil {
		return nil, err
	}
	ch := make(chan *llm.StreamChunk, 2)
	ch <- &llm.StreamChunk{Delta: text, Content: text}
	ch <- &llm.StreamChunk{Done: true, Content: text, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

// emptyJob 是没有对话内容的任务：处理时不调用模型，直接完成。
func emptyJob(key string, ns []string, scope string) Job {
	return Job{Key: key, Namespace: ns, Session: "none", From: 1, To: 1, Scope: scope}
}

// workerMemory 返回可以运行 Worker 的实例（模型对任何请求报错），会话 "none" 只有序号 1 的轮次开始事件，供 emptyJob 使用。
func workerMemory(t *testing.T, cfg Config) *Memory {
	t.Helper()
	if cfg.Model == nil {
		cfg.Model = &lockedModel{reply: func(string, string) (string, error) { return "", errors.New("不应调用模型") }}
	}
	sessions := session.NewMemoryStore()
	ctx := context.Background()
	rec, _, err := session.OpenRecorder(ctx, sessions, "none")
	require.NoError(t, err)
	start, err := session.NewEvent(constants.SessionEventTurnStart, session.TurnStart{Input: "-"})
	require.NoError(t, err)
	_, err = rec.Append(ctx, start)
	require.NoError(t, err)
	cfg.Sessions = sessions
	return newMemory(t, cfg)
}

// 任务存于命名空间当前作用域的 state：登记时写入 queue 分片，完成后只保留键并移除登记；Prune 按保留期删除结束的任务。
func TestJobLifecycle(t *testing.T) {
	m := workerMemory(t, Config{Retention: Retention{InvalidRetention: time.Hour}})
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)

	_, _, err := m.Job(context.Background(), "k")
	require.Error(t, err, "Job 需要上下文中的命名空间")
	require.NoError(t, m.Enqueue(ctx, emptyJob("k", ns, initialScope)))
	j, ok, err := m.Job(ctx, "k")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, JobPending, j.Status)
	checkQueue(t, m, ns)
	_, ok, err = m.Job(WithNamespace(context.Background(), "other"), "k")
	require.NoError(t, err)
	require.False(t, ok, "任务只在所属命名空间可见")

	n, err := m.ProcessPending(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	j, ok, err = m.Job(ctx, "k")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, Job{Key: "k", Namespace: ns, Status: JobDone}, j, "完成的任务只保留键")
	requireQueueEmpty(t, m)
	require.NoError(t, m.Enqueue(ctx, emptyJob("k", ns, initialScope)), "重复登记不产生新任务")
	requireQueueEmpty(t, m)

	_, err = m.mutate(ctx, ns, "", func(t *tx) error {
		t.Jobs.Put("failed", Job{Key: "failed", Status: JobFailed, CreatedAt: now})
		return nil
	})
	require.NoError(t, err)
	now = now.Add(2 * time.Hour)
	require.NoError(t, m.Prune(ctx))
	_, s, err := m.read(ctx, ns)
	require.NoError(t, err)
	require.Empty(t, s.Jobs)
	require.Empty(t, s.DoneJobs, "Prune 删除超过保留期的结束任务")
	checkInvariants(t, ns, s)
}

// 设计 7.1：Enqueue 与 Worker 移除登记随机交错 1000 次，没有任务丢失：全部完成，登记全部移除。
func TestEnqueueRacesWorker(t *testing.T) {
	m := workerMemory(t, Config{})
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)
	const writers, perWriter = 4, 250
	var wg sync.WaitGroup
	var enqueued atomic.Bool
	for w := range writers {
		wg.Go(func() {
			for i := range perWriter {
				require.NoError(t, m.Enqueue(ctx, emptyJob(fmt.Sprintf("w%d-%d", w, i), ns, initialScope)))
			}
		})
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !enqueued.Load() {
			_, err := m.ProcessPending(ctx)
			require.NoError(t, err)
		}
	}()
	wg.Wait()
	enqueued.Store(true)
	<-done
	checkQueue(t, m, ns)
	_, err := m.ProcessPending(ctx)
	require.NoError(t, err)

	_, s, err := m.read(ctx, ns)
	require.NoError(t, err)
	require.Empty(t, s.Jobs)
	require.Len(t, s.DoneJobs, writers*perWriter, "没有任务丢失")
	requireQueueEmpty(t, m)
	checkInvariants(t, ns, s)
}

// 设计 7.1：两个 Worker 实例共用存储，同一时刻只有一个在处理，每个任务只调用一次模型。
func TestTwoWorkersMutuallyExclusive(t *testing.T) {
	sessions := session.NewMemoryStore()
	model := &lockedModel{reply: func(string, string) (string, error) {
		time.Sleep(time.Millisecond)
		return `{"memories":[]}`, nil
	}}
	fs, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	cfg := Config{Durable: fs, Model: model, Sessions: sessions}
	a, b := newMemory(t, cfg), newMemory(t, cfg)
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)
	const jobs = 8
	for i := range jobs {
		from, to := appendTurn(t, sessions, "s", false, llm.UserMessage(fmt.Sprintf("第 %d 轮", i)), llm.Message{Role: "assistant", Content: "好的"})
		require.NoError(t, a.Enqueue(ctx, Job{Key: fmt.Sprint(i), Namespace: ns, Session: "s", From: from, To: to, Scope: initialScope}))
	}
	var wg sync.WaitGroup
	for _, m := range []*Memory{a, b, a, b} {
		wg.Go(func() {
			_, err := m.ProcessPending(ctx)
			require.NoError(t, err)
		})
	}
	wg.Wait()
	require.Len(t, model.calls, jobs, "每个任务只调用一次模型")
	require.EqualValues(t, 1, model.peak.Load(), "同一时刻只有一个 Worker 在处理")
	for i := range jobs {
		j, _, err := b.Job(ctx, fmt.Sprint(i))
		require.NoError(t, err)
		require.Equal(t, JobDone, j.Status)
	}
	requireQueueEmpty(t, a)
}

// 设计 7.1：state 接近 MaxStateBytes 时 Enqueue 丢弃并计数，Forget 与 Prune 仍然提交。
func TestEnqueueDropsWhenStateFull(t *testing.T) {
	fs, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)
	seed := newMemory(t, Config{Durable: fs})
	for _, id := range []string{"a", "b"} {
		_, err := seed.Remember(ctx, Record{ID: id, Content: "用户喜欢咖啡", Importance: 0.5})
		require.NoError(t, err)
	}
	h, err := loadHead(ctx, seed.scoped(ns, initialScope))
	require.NoError(t, err)

	m := workerMemory(t, Config{Durable: fs, MaxStateBytes: int(h.LogicalBytes) + 8, Retention: Retention{TTL: map[Kind]time.Duration{KindFact: time.Nanosecond}}})
	require.NoError(t, m.Enqueue(ctx, emptyJob("k", ns, initialScope)))
	require.EqualValues(t, 1, m.DroppedJobs())
	_, ok, err := m.Job(ctx, "k")
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, m.Forget(ctx, "a"))
	require.NoError(t, m.Prune(ctx))
	r, err := m.Get(ctx, "b")
	require.NoError(t, err)
	require.Equal(t, ReasonExpired, r.InvalidReason)

	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	requireQueueEmpty(t, m)
}

// 设计 7.1：Worker 的锁被接管时，在心跳处发现并结束本轮，余下的任务留待接管者处理。
func TestWorkerLeaseLostEndsRound(t *testing.T) {
	st := newHookStore(t)
	m := workerMemory(t, Config{Durable: st})
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)
	for _, k := range []string{"a", "b"} {
		require.NoError(t, m.Enqueue(ctx, emptyJob(k, ns, initialScope)))
	}
	st.h.before = func(op, scope, id string) error {
		if op == "save" && scope == "" && id == workerID {
			return agentErrors.ErrLeaseLost
		}
		return nil
	}
	n, err := m.ProcessPending(ctx)
	require.ErrorIs(t, err, agentErrors.ErrLeaseLost)
	require.Equal(t, 1, n, "心跳失败后不再处理下一个任务")
	_, s, err := m.read(ctx, ns)
	require.NoError(t, err)
	require.Len(t, pendingJobs(s), 1)
	checkQueue(t, m, ns)

	st.h.before = nil
	n, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	requireQueueEmpty(t, m)
}

// 设计 7.2：Forget 提交之后、删除向量之前崩溃，之后没有新的轮次：维护按"索引待清理"登记清掉残留，不变量 7 成立。
func TestMaintainCleansIndexAfterForgetCrash(t *testing.T) {
	base := NewMemoryIndex()
	errCrash := errors.New("模拟崩溃")
	var crash atomic.Bool
	idx := hookIndex{Index: base, before: func(op string, _ []string, _ string) error {
		if op == "delete" && crash.Load() {
			return errCrash
		}
		return nil
	}}
	m := newMemory(t, Config{Index: idx, Embedder: bigramEmbedder{}, EmbedModel: "v1"})
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)
	_, err := m.Remember(ctx, Record{ID: "a", Content: "机密：项目代号青鸟", Importance: 0.5})
	require.NoError(t, err)

	crash.Store(true)
	require.ErrorIs(t, m.Forget(ctx, "a"), errCrash)
	crash.Store(false)
	require.Len(t, indexKeys(base, ns), 1, "崩溃留下被遗忘记录的键")
	entries, err := m.queued(ctx)
	require.NoError(t, err)
	require.Equal(t, []queueEntry{{Namespace: ns, Scope: initialScope, Reason: queueIndex}}, entries)

	require.NoError(t, m.Maintain(ctx), "没有配置模型也能维护")
	require.Empty(t, indexKeys(base, ns))
	requireQueueEmpty(t, m)
	checkIndex(t, m, base, ns)
}

// 设计 7.2：只用宿主 API 的部署（没有 Worker），Purge 在 RemoveScope 之前崩溃：RunMaintenance 补完清理。
func TestRunMaintenanceCompletesPurge(t *testing.T) {
	st := newHookStore(t)
	x := NewMemoryIndex()
	m := newMemory(t, Config{Durable: st, Index: x, Embedder: bigramEmbedder{}, EmbedModel: "v1"})
	ctx := WithNamespace(context.Background(), testNS...)
	_, err := m.Remember(ctx, Record{ID: "a", Content: "机密：项目代号青鸟", Importance: 0.5})
	require.NoError(t, err)
	errCrash := errors.New("模拟崩溃")
	st.h.before = func(op, _, _ string) error {
		if op == "removescope" {
			return errCrash
		}
		return nil
	}
	require.ErrorIs(t, m.Purge(ctx), errCrash)
	st.h.before = nil
	later := time.Now().Add(m.cfg.RetireGrace)
	m.now = func() time.Time { return later }

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- m.RunMaintenance(runCtx, time.Millisecond, func(err error) { t.Error(err) }) }()
	require.Eventually(t, func() bool {
		var ret retiringState
		_, _, err := loadJSON(ctx, st, retiringID, &ret)
		return err == nil && len(ret.Scopes) == 0
	}, 10*time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	_, err = loadHead(ctx, m.scoped(testNS, initialScope))
	require.ErrorIs(t, err, agentErrors.ErrScopeRemoved)
	require.Empty(t, indexKeys(x, testNS))
}

// 设计 7.2：Enqueue 写入登记之后、保存任务之前崩溃：只多一条登记，由下一轮 Worker 移除。
func TestEnqueueCrashAfterRegister(t *testing.T) {
	st := newHookStore(t)
	m := workerMemory(t, Config{Durable: st})
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)
	errCrash := errors.New("模拟崩溃")
	st.h.before = func(op, scope, id string) error {
		if op == "save" && scope != "" && id == headID {
			return errCrash
		}
		return nil
	}
	require.Error(t, m.Enqueue(ctx, emptyJob("k", ns, initialScope)))
	st.h.before = nil
	entries, err := m.queued(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1, "登记已写入")
	_, ok, err := m.Job(ctx, "k")
	require.NoError(t, err)
	require.False(t, ok, "任务没有保存")

	n, err := m.ProcessPending(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	requireQueueEmpty(t, m)
}

// 任务的提交之前崩溃（G2 设计第 6 节）：任务仍待处理，重做从头计划，记录 ID 确定，不产生重复。
func TestWorkerRedoAfterCrashBeforeCommit(t *testing.T) {
	st := newHookStore(t)
	sessions := session.NewMemoryStore()
	model := &lockedModel{reply: func(system, user string) (string, error) {
		if system == reconcilePrompt {
			return `{"op":"add"}`, nil
		}
		return `{"memories":[{"kind":"fact","content":"用户住在上海","importance":0.8},{"kind":"fact","content":"用户养了一只猫","importance":0.6}]}`, nil
	}}
	m := newMemory(t, Config{Durable: st, Model: model, Sessions: sessions})
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)
	from, to := appendTurn(t, sessions, "s", false, llm.UserMessage("我住在上海，养了一只猫"), llm.Message{Role: "assistant", Content: "好的"})
	require.NoError(t, m.Enqueue(ctx, Job{Key: "k", Namespace: ns, Session: "s", From: from, To: to, Scope: initialScope}))

	// 任务的唯一一次提交崩溃：什么都没有写入，任务仍待处理。
	errCrash := errors.New("模拟崩溃")
	var saves atomic.Int32
	st.h.before = func(op, scope, id string) error {
		if op == "save" && scope != "" && id == headID && saves.Add(1) == 1 {
			return errCrash
		}
		return nil
	}
	_, err := m.ProcessPending(ctx)
	require.NoError(t, err)
	j, _, err := m.Job(ctx, "k")
	require.NoError(t, err)
	require.Equal(t, JobPending, j.Status, "失败计入尝试次数后重试")
	require.Equal(t, 1, j.Attempts)
	_, s, err := m.read(ctx, ns)
	require.NoError(t, err)
	require.Empty(t, s.Records)

	st.h.before = nil
	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	_, s, err = m.read(ctx, ns)
	require.NoError(t, err)
	require.Len(t, s.Records, 2, "重做没有重复创建")
	j, _, err = m.Job(ctx, "k")
	require.NoError(t, err)
	require.Equal(t, JobDone, j.Status)
	checkInvariants(t, ns, s)
}

// 登记之后作用域内发生了遗忘（LastForgetSeq > TurnSeq）的任务被丢弃：处理前发生的不调用模型，处理中发生的在下一次写入时中止。
func TestJobDroppedAfterForget(t *testing.T) {
	sessions := session.NewMemoryStore()
	var m *Memory
	ns := []string{"u"}
	ctx := WithNamespace(context.Background(), ns...)
	model := &lockedModel{reply: func(system, user string) (string, error) {
		require.NoError(t, m.Forget(ctx, "b"), "处理中遗忘")
		return `{"memories":[{"kind":"fact","content":"用户喜欢咖啡","importance":0.8}]}`, nil
	}}
	m = newMemory(t, Config{Model: model, Sessions: sessions})
	for _, id := range []string{"a", "b"} {
		_, err := m.Remember(ctx, Record{ID: id, Content: "旧记忆" + id, Importance: 0.5})
		require.NoError(t, err)
	}
	enqueue := func(key string) {
		turn, err := m.Turn(ctx)
		require.NoError(t, err)
		from, to := appendTurn(t, sessions, "s", false, llm.UserMessage("我喜欢咖啡"), llm.Message{Role: "assistant", Content: "好的"})
		require.NoError(t, m.Enqueue(ctx, Job{Key: key, Namespace: ns, Session: "s", From: from, To: to, Scope: turn.Scope, TurnSeq: turn.Seq}))
	}
	enqueue("before")
	require.NoError(t, m.Forget(ctx, "a"))
	_, err := m.ProcessPending(ctx)
	require.NoError(t, err)
	j, _, err := m.Job(ctx, "before")
	require.NoError(t, err)
	require.Equal(t, JobDropped, j.Status)
	require.Empty(t, model.calls, "处理前已发生遗忘的任务不调用模型")

	enqueue("during")
	_, err = m.ProcessPending(ctx)
	require.NoError(t, err)
	j, _, err = m.Job(ctx, "during")
	require.NoError(t, err)
	require.Equal(t, JobDropped, j.Status)
	require.Len(t, model.calls, 1)
	require.EqualValues(t, 2, m.DroppedJobs())
	_, s, err := m.read(ctx, ns)
	require.NoError(t, err)
	for _, r := range s.Records {
		require.False(t, strings.Contains(r.Content, "咖啡"), "中止的任务没有写入")
	}
	requireQueueEmpty(t, m)
}

// 处理结果与 errScopeChanged、errJobStale 同为 CodeConflict 时，按失败计尝试次数，不当作过期任务丢弃（AgentError.Is 只比较错误码）。
func TestFinishJobClassifiesSentinelsByIdentity(t *testing.T) {
	cases := []struct {
		name     string
		result   error
		status   JobStatus
		attempts int
		dropped  int64
	}{
		{"读取反复冲突的普通错误", agentErrors.Newf(agentErrors.CodeConflict, "长期记忆的作用域或基础快照在读取期间反复改变").WithComponent("memory"), JobPending, 1, 0},
		{"记录冲突", conflict("长期记忆记录 %q 已存在：修改用 Update", "r"), JobPending, 1, 0},
		{"任务过期", errJobStale, JobDropped, 0, 1},
		{"包装的任务过期", fmt.Errorf("处理：%w", errJobStale), JobDropped, 0, 1},
		{"作用域改变", errScopeChanged, JobDropped, 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := workerMemory(t, Config{JobAttempts: 3})
			ns := []string{"u"}
			ctx := WithNamespace(context.Background(), ns...)
			j := emptyJob("k", ns, initialScope)
			require.NoError(t, m.Enqueue(ctx, j))
			require.NoError(t, m.finishJob(ctx, j, c.result))
			got, ok, err := m.Job(ctx, "k")
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, c.status, got.Status)
			require.Equal(t, c.attempts, got.Attempts)
			require.Equal(t, c.dropped, m.DroppedJobs())
		})
	}
}
