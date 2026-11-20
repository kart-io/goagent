package adapters

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/memstore"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
)

// fixedReconciliations 返回测试写入的核对记录，模拟宿主保存的不可变核对证据。
type fixedReconciliations struct {
	mu     sync.Mutex
	record mg.Reconciliation
}

func (r *fixedReconciliations) Lookup(_ context.Context, _ mg.Actor, id string) (mg.Reconciliation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record.ID != id {
		return mg.Reconciliation{}, agentErrors.ErrManagementNotFound
	}
	return r.record, nil
}

// fakeTool 计数每次真实执行；run 决定本次执行的结果，n 从 1 开始。
type fakeTool struct {
	*tools.BaseTool
	calls atomic.Int32
	run   func(ctx context.Context, n int) (*interfaces.ToolOutput, error)
	// single 为 true 时声明只执行一次（写工具）；concurrent 为 true 时声明可并发。
	single, concurrent bool
}

func newFakeTool(name string, single, concurrent bool, run func(ctx context.Context, n int) (*interfaces.ToolOutput, error)) *fakeTool {
	f := &fakeTool{run: run, single: single, concurrent: concurrent}
	f.BaseTool = tools.NewBaseTool(name, name, `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return f.run(ctx, int(f.calls.Add(1)))
	})
	return f
}

func (f *fakeTool) SupportsConcurrentCalls() bool { return f.concurrent }

// writeTool 声明 MaxAttempts 1：崩溃后不自动重跑。
type writeTool struct{ *fakeTool }

func (w writeTool) DurableOperationPolicy() durable.OperationPolicy {
	return durable.OperationPolicy{MaxAttempts: 1}
}

// reconcilableWrite 在 found 返回 true 时给出回执，reconciles 记录工具核对被调用的次数。
type reconcilableWrite struct {
	writeTool
	found      atomic.Bool
	reconciles atomic.Int32
}

func (r *reconcilableWrite) Reconcile(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, bool, error) {
	r.reconciles.Add(1)
	if !r.found.Load() {
		return nil, false, nil
	}
	return &interfaces.ToolOutput{Success: true, Result: "receipt-42"}, true, nil
}

// blockUntilCrash 第一次执行时产生副作用并阻塞到工作器退出；之后的执行立即成功。
func blockUntilCrash(started chan<- struct{}) func(ctx context.Context, n int) (*interfaces.ToolOutput, error) {
	return func(ctx context.Context, n int) (*interfaces.ToolOutput, error) {
		if n == 1 {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return &interfaces.ToolOutput{Success: true, Result: "done"}, nil
	}
}

func effectUnknownOnce(ctx context.Context, n int) (*interfaces.ToolOutput, error) {
	if n == 1 {
		return nil, durable.ErrEffectUnknown
	}
	return &interfaces.ToolOutput{Success: true, Result: "done"}, nil
}

type nativeHarness struct {
	t        *testing.T
	ctx      context.Context
	svc      *mg.Service
	actor    mg.Actor
	sessions *session.MemoryStore
	store    *memstore.Store
	version  mg.Version
	se       mg.Session
	reader   *fixedReconciliations
	stop     func()
}

func newNativeHarness(t *testing.T, model llm.Client, toolset []interfaces.Tool, parallel int, ask bool) *nativeHarness {
	t.Helper()
	h := &nativeHarness{t: t, ctx: context.Background(), actor: mg.Actor{TenantID: "tenant", Subject: "operator"}, sessions: session.NewMemoryStore(), reader: &fixedReconciliations{}}
	factory := NativeFactory{
		Check: func(context.Context, mg.Version) error { return nil },
		BuildAgent: func(_ context.Context, _ mg.Version, _ mg.Resources, approver tools.Approver) (*builder.SimpleAgentBuilder, func(context.Context) error, error) {
			reg := tools.NewRegistry()
			for _, tool := range toolset {
				if err := reg.Register(tool); err != nil {
					return nil, nil, err
				}
			}
			cfg := tools.DispatchConfig{}
			if ask {
				cfg.Pipeline = &tools.Pipeline{
					Pre: []tools.PreHook{func(context.Context, *tools.Call) (tools.PreDecision, error) {
						return tools.PreDecision{Kind: tools.PreAsk}, nil
					}},
					Approver: approver,
				}
			}
			d, err := tools.NewDispatcher(reg, cfg)
			if err != nil {
				return nil, nil, err
			}
			b := builder.NewSimpleBuilder(model).WithTools(d.Tools()...).WithMaxIterations(4)
			if parallel > 1 {
				b = b.WithParallelToolCalls(parallel)
			}
			return b, nil, nil
		},
	}
	h.store = memstore.New()
	svc, err := mg.New(mg.Config{Store: h.store, Sessions: h.sessions, HistoryReader: memstore.History{Sessions: h.sessions}, Authorizer: mg.TenantAuthorizer{}, Factories: map[string]mg.Factory{"native": factory}, Reconciliations: h.reader})
	require.NoError(t, err)
	h.svc = svc
	agent, err := svc.RegisterAgent(h.ctx, h.actor, mg.RegisterAgentRequest{IdempotencyKey: "agent", Name: "核对"})
	require.NoError(t, err)
	h.version, err = svc.SaveVersion(h.ctx, h.actor, mg.SaveVersionRequest{IdempotencyKey: "version", AgentID: agent.ID, Version: "v1", RunnerKind: "native", ImplementationVersion: "impl", Config: []byte(`{}`)})
	require.NoError(t, err)
	agent, err = svc.PublishVersion(h.ctx, h.actor, mg.PublishRequest{IdempotencyKey: "publish", AgentID: agent.ID, Version: h.version.Version, ExpectedRevision: agent.Revision})
	require.NoError(t, err)
	h.se, err = svc.CreateSession(h.ctx, h.actor, mg.CreateSessionRequest{IdempotencyKey: "session", AgentID: agent.ID})
	require.NoError(t, err)
	h.startWorker()
	t.Cleanup(func() { h.stopWorker() })
	require.Eventually(t, func() bool {
		h.se, err = svc.GetSession(h.ctx, h.actor, h.se.ID)
		return err == nil && h.se.Status == "ready"
	}, 5*time.Second, 5*time.Millisecond)
	return h
}

func (h *nativeHarness) startWorker() {
	ctx, cancel := context.WithCancel(h.ctx)
	done := make(chan error, 1)
	go func() { done <- mg.NewWorker(h.svc).Run(ctx) }()
	h.stop = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			h.t.Error("工作器未退出")
		}
	}
}

func (h *nativeHarness) stopWorker() {
	if h.stop != nil {
		h.stop()
		h.stop = nil
	}
}

// crash 模拟进程在工具执行中退出：停掉工作器，等任务交还恢复后启动新工作器。
func (h *nativeHarness) crash(taskID string, between func()) {
	h.stopWorker()
	task := h.task(taskID)
	require.Equal(h.t, mg.Queued, task.State, "error_kind=%q", task.ErrorKind)
	require.True(h.t, task.RecoveryNeeded)
	if between != nil {
		between()
	}
	h.startWorker()
}

func (h *nativeHarness) submit(key string) mg.Task {
	task, err := h.svc.SubmitTask(h.ctx, h.actor, mg.SubmitTaskRequest{IdempotencyKey: key, SessionID: h.se.ID, Input: []byte(`"执行"`)})
	require.NoError(h.t, err)
	return task
}

func (h *nativeHarness) task(id string) mg.Task {
	task, err := h.svc.GetTask(h.ctx, h.actor, id)
	require.NoError(h.t, err)
	return task
}

func (h *nativeHarness) wait(id string, state mg.TaskState) mg.Task {
	h.t.Helper()
	var task mg.Task
	deadline := time.Now().Add(5 * time.Second)
	for {
		task = h.task(id)
		if task.State == state {
			return task
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("任务未进入 %s：当前 %s error_kind=%q operations=%v", state, task.State, task.ErrorKind, task.Operations)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// resolve 写入核对记录并以它恢复 attention 任务。
func (h *nativeHarness) resolve(task mg.Task, resolution string) {
	h.reader.mu.Lock()
	h.reader.record = mg.Reconciliation{ID: "proof-" + task.ID, TenantID: h.actor.TenantID, TaskID: task.ID, VersionHash: h.version.ConfigHash, EvidenceHash: "evidence", Resolution: resolution, VerifiedBy: h.actor.Subject, VerifiedAt: time.Now(), OperationRefs: task.Operations}
	h.reader.mu.Unlock()
	_, err := h.svc.ResumeTask(h.ctx, h.actor, mg.ResumeTaskRequest{IdempotencyKey: "resolve-" + task.ID, TaskID: task.ID, ExpectedRevision: task.Revision, ReconciliationRef: "proof-" + task.ID})
	require.NoError(h.t, err)
}

// native 返回只读的适配器实例，用于直接检查 Inspect/Reconcile。
func (h *nativeHarness) native() *native {
	return &native{r: mg.Resources{Sessions: h.sessions, Session: h.se}}
}

// rewrite 把本轮工具记录改写为崩溃后可能留下的状态。
func (h *nativeHarness) rewrite(task mg.Task, id, status, kind string) {
	_, records, _, err := h.native().currentTurn(h.ctx, mg.RunSpec{Task: task})
	require.NoError(h.t, err)
	for _, record := range records {
		if !strings.HasSuffix(record.ID, "/"+id) {
			continue
		}
		record.Status, record.FailureKind, record.Value = status, kind, json.RawMessage(`null`)
		data, err := json.Marshal(record)
		require.NoError(h.t, err)
		storageID := durable.OperationStorageID(record.Key)
		require.NoError(h.t, session.DurableStore(h.sessions, h.se.LogID).Save(h.ctx, storageID, &durable.Snapshot{RunID: storageID, Version: "goagent-operation-v2", Data: data}))
		return
	}
	h.t.Fatalf("本轮没有调用 %s 的工具记录", id)
}

// toolResult 返回最后一次模型请求中给定调用的工具结果。
func toolResult(t *testing.T, model *mocks.ScriptedClient, callID string) string {
	t.Helper()
	requests := model.Requests()
	require.NotEmpty(t, requests)
	for _, m := range requests[len(requests)-1].Messages {
		if m.ToolCallID == callID {
			return m.Content
		}
	}
	t.Fatalf("最后一次模型请求没有 %s 的工具结果", callID)
	return ""
}

func writeRound(calls ...string) mocks.ScriptedRule {
	round := mocks.ToolCallReply("工具轮次")
	for _, name := range calls {
		round.Reply.ToolCalls = append(round.Reply.ToolCalls, mocks.ToolCall("call-"+name, name, `{"target":"A1"}`))
	}
	return round
}

// 四种中断记录在尝试次数用尽时都进入 attention，并暴露带 ParentKey 的未决操作。
func TestInterruptedRecordsExposeOperations(t *testing.T) {
	for _, tc := range []struct {
		name, status, kind string
	}{
		{"pending", "pending", ""},
		{"failed/canceled", "failed", "canceled"},
		{"failed/deadline", "failed", "deadline"},
		{"failed/effect_unknown", "failed", "effect_unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := mocks.NewScriptedClient(writeRound("write"))
			started := make(chan struct{})
			run := blockUntilCrash(started)
			if tc.kind == "effect_unknown" {
				run = effectUnknownOnce
			}
			write := writeTool{newFakeTool("write", true, false, run)}
			h := newNativeHarness(t, model, []interfaces.Tool{write}, 1, false)
			task := h.submit("task")
			if tc.kind != "effect_unknown" {
				<-started
				h.crash(task.ID, func() {
					if tc.kind != "canceled" {
						h.rewrite(h.task(task.ID), "call-write", tc.status, tc.kind)
					}
				})
			}
			task = h.wait(task.ID, mg.Attention)
			require.Equal(t, "unknown_effect", task.ErrorKind)
			require.Len(t, task.Operations, 1)
			require.True(t, strings.HasPrefix(task.Operations[0].ID, "tool/") && strings.HasSuffix(task.Operations[0].ID, "/call-write"), task.Operations[0].ID)
			require.NotEmpty(t, task.Operations[0].ParentKey)
			require.Equal(t, int32(1), write.calls.Load())
		})
	}
}

// 核对接口只校验并返回 absent；worker 据此带着核对结论调用 Run，由 Dispatcher 定案。
func TestNativeReconcileReturnsAbsent(t *testing.T) {
	caps, err := NativeFactory{Check: func(context.Context, mg.Version) error { return nil }, BuildAgent: func(context.Context, mg.Version, mg.Resources, tools.Approver) (*builder.SimpleAgentBuilder, func(context.Context) error, error) {
		return nil, nil, nil
	}}.Validate(context.Background(), mg.Version{})
	require.NoError(t, err)
	require.True(t, caps.Reconcile)

	model := mocks.NewScriptedClient(writeRound("write"), mocks.TextReply("结束", "完成"))
	write := writeTool{newFakeTool("write", true, false, effectUnknownOnce)}
	h := newNativeHarness(t, model, []interfaces.Tool{write}, 1, false)
	task := h.wait(h.submit("task").ID, mg.Attention)
	rec := mg.Reconciliation{Resolution: "not_applied", OperationRefs: task.Operations}
	evidence, err := h.native().Reconcile(h.ctx, mg.RunSpec{Task: task}, rec)
	require.NoError(t, err)
	require.Equal(t, mg.RunEvidence{State: "absent"}, evidence)

	// 只有 Run 拿到 spec.Reconciliation 时，工具结果才会是核对结论。
	h.resolve(task, "not_applied")
	h.wait(task.ID, mg.Succeeded)
	require.Contains(t, toolResult(t, model, "call-write"), "operation was not applied")
	require.Equal(t, int32(1), write.calls.Load())
}

func TestNativeReconcileAppliedSettlesWithoutRerun(t *testing.T) {
	model := mocks.NewScriptedClient(writeRound("write"), mocks.TextReply("结束", "完成"))
	started := make(chan struct{})
	write := &reconcilableWrite{writeTool: writeTool{newFakeTool("write", true, false, blockUntilCrash(started))}}
	h := newNativeHarness(t, model, []interfaces.Tool{write}, 1, false)
	task := h.submit("task")
	<-started
	h.crash(task.ID, nil)
	task = h.wait(task.ID, mg.Attention)
	require.Len(t, task.Operations, 1)

	write.found.Store(true)
	h.resolve(task, "applied")
	h.wait(task.ID, mg.Succeeded)
	require.Equal(t, int32(1), write.calls.Load(), "已生效的写操作不能重跑")
	require.Contains(t, toolResult(t, model, "call-write"), "receipt-42")
}

func TestReconcileAppliedWithoutEvidenceUsesFixedText(t *testing.T) {
	model := mocks.NewScriptedClient(writeRound("write"), mocks.TextReply("结束", "完成"))
	write := &reconcilableWrite{writeTool: writeTool{newFakeTool("write", true, false, effectUnknownOnce)}}
	h := newNativeHarness(t, model, []interfaces.Tool{write}, 1, false)
	task := h.wait(h.submit("task").ID, mg.Attention)

	h.resolve(task, "applied")
	h.wait(task.ID, mg.Succeeded)
	require.Equal(t, int32(1), write.calls.Load())
	require.Contains(t, toolResult(t, model, "call-write"), "operation was verified as applied; output unavailable")
}

func TestNativeReconcileNotAppliedOverridesToolReconcile(t *testing.T) {
	model := mocks.NewScriptedClient(writeRound("write"), mocks.TextReply("结束", "完成"))
	started := make(chan struct{})
	write := &reconcilableWrite{writeTool: writeTool{newFakeTool("write", true, false, blockUntilCrash(started))}}
	h := newNativeHarness(t, model, []interfaces.Tool{write}, 1, false)
	task := h.submit("task")
	<-started
	h.crash(task.ID, nil)
	task = h.wait(task.ID, mg.Attention)

	// 工具此时会报告已生效，但人工结论优先，工具核对不再被调用。
	write.found.Store(true)
	before := write.reconciles.Load()
	h.resolve(task, "not_applied")
	h.wait(task.ID, mg.Succeeded)
	require.Equal(t, before, write.reconciles.Load())
	require.Equal(t, int32(1), write.calls.Load())
	require.Contains(t, toolResult(t, model, "call-write"), "operation was not applied; call again if still needed")
}

func TestAutoReconcileSettlesWithoutAttention(t *testing.T) {
	model := mocks.NewScriptedClient(writeRound("write"), mocks.TextReply("结束", "完成"))
	started := make(chan struct{})
	write := &reconcilableWrite{writeTool: writeTool{newFakeTool("write", true, false, blockUntilCrash(started))}}
	write.found.Store(true)
	h := newNativeHarness(t, model, []interfaces.Tool{write}, 1, false)
	task := h.submit("task")
	<-started
	h.crash(task.ID, nil)
	h.wait(task.ID, mg.Succeeded)
	require.Equal(t, int32(1), write.calls.Load())
	require.Contains(t, toolResult(t, model, "call-write"), "receipt-42")
}

// 同一轮中写工具结果未知、读工具被中断：只有写工具交给宿主，读工具在续跑时重跑。
func TestReconcileOnlyAppliesToListedCalls(t *testing.T) {
	model := mocks.NewScriptedClient(writeRound("write", "read"), mocks.TextReply("结束", "完成"))
	started := make(chan struct{})
	write := writeTool{newFakeTool("write", true, true, effectUnknownOnce)}
	read := newFakeTool("read", false, true, blockUntilCrash(started))
	h := newNativeHarness(t, model, []interfaces.Tool{write, read}, 2, false)
	task := h.submit("task")
	<-started
	// 写工具的结果未知记录落盘后再模拟崩溃。
	require.Eventually(t, func() bool {
		_, records, _, err := h.native().currentTurn(h.ctx, mg.RunSpec{Task: h.task(task.ID)})
		require.NoError(t, err)
		for _, r := range records {
			if strings.HasSuffix(r.ID, "/call-write") && r.FailureKind == "effect_unknown" {
				return true
			}
		}
		return false
	}, 5*time.Second, 5*time.Millisecond)
	// 退出时终止事件可能丢失，任务交还恢复；两种情况下新工作器都把写工具交给宿主。
	h.stopWorker()
	h.startWorker()
	task = h.wait(task.ID, mg.Attention)
	require.Len(t, task.Operations, 1)
	require.True(t, strings.HasSuffix(task.Operations[0].ID, "/call-write"), "读工具不交给宿主：%v", task.Operations)

	h.resolve(task, "applied")
	h.wait(task.ID, mg.Succeeded)
	require.Equal(t, int32(1), write.calls.Load())
	require.Equal(t, int32(2), read.calls.Load(), "被中断的读工具按默认策略重跑")
	require.Contains(t, toolResult(t, model, "call-write"), "operation was verified as applied")
	require.Equal(t, "done", toolResult(t, model, "call-read"))
}

func TestNativeReadToolPendingAutoResumes(t *testing.T) {
	model := mocks.NewScriptedClient(writeRound("read"), mocks.TextReply("结束", "完成"))
	started := make(chan struct{})
	read := newFakeTool("read", false, true, blockUntilCrash(started))
	h := newNativeHarness(t, model, []interfaces.Tool{read}, 1, false)
	task := h.submit("task")
	<-started
	h.crash(task.ID, func() { h.rewrite(h.task(task.ID), "call-read", "pending", "") })
	task = h.wait(task.ID, mg.Succeeded)
	require.Empty(t, task.Operations, "读工具中断不交给宿主")
	require.Equal(t, int32(2), read.calls.Load())
}

func TestNativeReconcileRejectsPartialOperations(t *testing.T) {
	model := mocks.NewScriptedClient(writeRound("write_a", "write_b"))
	a := writeTool{newFakeTool("write_a", true, true, effectUnknownOnce)}
	b := writeTool{newFakeTool("write_b", true, true, effectUnknownOnce)}
	h := newNativeHarness(t, model, []interfaces.Tool{a, b}, 2, false)
	task := h.wait(h.submit("task").ID, mg.Attention)
	require.Len(t, task.Operations, 2)
	n := h.native()
	spec := mg.RunSpec{Task: task}
	for _, tc := range []struct {
		name string
		refs []durable.OperationRef
	}{
		{"缺少操作", task.Operations[:1]},
		{"重复操作", []durable.OperationRef{task.Operations[0], task.Operations[0], task.Operations[1]}},
		{"未知操作", append([]durable.OperationRef{{ParentKey: task.Operations[0].ParentKey, ID: "tool/other"}}, task.Operations...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := n.Reconcile(h.ctx, spec, mg.Reconciliation{Resolution: "applied", OperationRefs: tc.refs})
			require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
		})
	}
	evidence, err := n.Reconcile(h.ctx, spec, mg.Reconciliation{Resolution: "applied", OperationRefs: []durable.OperationRef{task.Operations[1], task.Operations[0]}})
	require.NoError(t, err)
	require.Equal(t, "absent", evidence.State)
}

// 上一轮遗留的中断记录不属于本任务：既不计入未决集合，也不能被本任务核对。
func TestFollowupTurnIgnoresPreviousTurnRecords(t *testing.T) {
	ctx := context.Background()
	store := session.NewMemoryStore()
	const logID = "log"
	appendEvent := func(typ string, data any) {
		ev, err := session.NewEvent(typ, data)
		require.NoError(t, err)
		var seq int64
		if events, err := store.Read(ctx, logID, 0); err == nil && len(events) > 0 {
			seq = events[len(events)-1].Seq
		}
		_, err = store.Append(ctx, logID, seq, ev)
		require.NoError(t, err)
	}
	appendEvent(constants.SessionEventTurnStart, session.TurnStart{Input: "第一轮", ExecutionKey: "first"})
	jctx := durable.WithJournal(durable.WithExecutionKey(ctx, "first-parent"), session.DurableStore(store, logID))
	_, err := durable.Do(jctx, "tool/call-1", json.RawMessage(`{}`), durable.OperationPolicy{MaxAttempts: 1}, func(context.Context) (json.RawMessage, error) {
		return nil, durable.ErrEffectUnknown
	})
	var opErr *durable.OperationError
	require.ErrorAs(t, err, &opErr)
	appendEvent(constants.SessionEventTurnEnd, session.TurnEnd{Aborted: true})
	appendEvent(constants.SessionEventTurnStart, session.TurnStart{Input: "第二轮", ExecutionKey: "second"})

	n := &native{r: mg.Resources{Sessions: store, Session: mg.Session{LogID: logID}}}
	spec := mg.RunSpec{Task: mg.Task{ExecutionKey: "second"}}
	evidence, err := n.Inspect(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, "absent", evidence.State)
	evidence, err = n.Reconcile(ctx, spec, mg.Reconciliation{Resolution: "applied"})
	require.NoError(t, err, "上一轮的未决记录不能要求本任务核对")
	require.Equal(t, "absent", evidence.State)
	_, err = n.Reconcile(ctx, spec, mg.Reconciliation{Resolution: "applied", OperationRefs: []durable.OperationRef{{ParentKey: opErr.ParentKey, ID: opErr.ID}}})
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
}

func TestApprovalInfoCarriesArguments(t *testing.T) {
	model := mocks.NewScriptedClient(writeRound("write"))
	write := writeTool{newFakeTool("write", true, false, effectUnknownOnce)}
	h := newNativeHarness(t, model, []interfaces.Tool{write}, 1, true)
	task := h.wait(h.submit("task").ID, mg.WaitingApproval)
	require.Len(t, task.Waits, 1)
	var info struct {
		Tool      string         `json:"tool"`
		Arguments map[string]any `json:"arguments"`
	}
	require.NoError(t, json.Unmarshal(task.Waits[0].Info, &info))
	require.Equal(t, "write", info.Tool)
	require.Equal(t, map[string]any{"target": "A1"}, info.Arguments)
	require.True(t, strings.Contains(string(task.Waits[0].Info), `"arguments"`))
}

// 放弃核对：Attention 任务取消后保留原因与未决操作，会话接受新任务，新任务看不到被取消的轮次。
func TestCancelAttentionTask(t *testing.T) {
	model := mocks.NewScriptedClient(writeRound("write"), mocks.TextReply("新任务", "好的"))
	write := writeTool{newFakeTool("write", true, false, effectUnknownOnce)}
	h := newNativeHarness(t, model, []interfaces.Tool{write}, 1, false)
	task := h.wait(h.submit("task").ID, mg.Attention)
	operations := task.Operations
	require.Len(t, operations, 1)

	canceled, err := h.svc.CancelTask(h.ctx, h.actor, mg.CancelTaskRequest{IdempotencyKey: "cancel", TaskID: task.ID, ExpectedRevision: task.Revision, Reason: "放弃核对"})
	require.NoError(t, err)
	require.Equal(t, mg.Canceled, canceled.State)
	require.Equal(t, "unknown_effect", canceled.ErrorKind, "保留原 attention 原因")
	require.Equal(t, operations, canceled.Operations, "未决操作不定案")
	require.NotNil(t, canceled.FinishedAt)

	docs, err := h.store.List(h.ctx, "audit", mg.Selection{TenantID: h.actor.TenantID, Visibility: mg.Visibility{All: true}})
	require.NoError(t, err)
	found := false
	for _, doc := range docs {
		var a mg.Audit
		require.NoError(t, json.Unmarshal(doc.Data, &a))
		if a.ResourceID == task.ID && a.Operation == "cancel" {
			found = true
			require.Equal(t, h.actor, a.Actor)
			require.Equal(t, "放弃核对", a.Reason)
		}
	}
	require.True(t, found, "审计记录取消人")

	next := h.wait(h.submit("next").ID, mg.Succeeded)
	require.Equal(t, int32(1), write.calls.Load())
	requests := model.Requests()
	for _, m := range requests[len(requests)-1].Messages {
		require.Empty(t, m.ToolCalls, "新任务的请求中不能有悬空的工具调用")
		require.Empty(t, m.ToolCallID)
	}
	require.NotEqual(t, task.ID, next.ID)
}
