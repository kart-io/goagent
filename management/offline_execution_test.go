package management_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/stretchr/testify/require"
)

// 只轮询持久状态；断言与结果读取留在测试协程，避免异步回调调用 FailNow。
func offlineExecutionWaitTask(t *testing.T, f *managementtest.Fixture, id string, state mg.TaskState) mg.Task {
	t.Helper()
	ctx := context.Background()
	require.Eventually(t, func() bool {
		task, err := f.Service.GetTask(ctx, f.Actor, id)
		return err == nil && task.State == state
	}, 5*time.Second, 5*time.Millisecond, "任务应进入状态 %s", state)
	task, err := f.Service.GetTask(ctx, f.Actor, id)
	require.NoError(t, err)
	return task
}

func offlineExecutionDocuments(t *testing.T, f *managementtest.Fixture, kind string, q mg.Query) []mg.Document {
	t.Helper()
	docs, err := f.Store.List(context.Background(), kind, mg.Selection{TenantID: f.Actor.TenantID, Query: q, Visibility: mg.Visibility{All: true}})
	require.NoError(t, err)
	return docs
}

func offlineExecutionSubmit(t *testing.T, f *managementtest.Fixture, se mg.Session, key string) mg.Task {
	t.Helper()
	task, err := f.Service.SubmitTask(context.Background(), f.Actor, mg.SubmitTaskRequest{IdempotencyKey: key, SessionID: se.ID, Input: []byte(`{"请求":"执行任务"}`)})
	require.NoError(t, err)
	require.Equal(t, mg.Queued, task.State)
	return task
}

func offlineExecutionReleased(t *testing.T, f *managementtest.Fixture, se mg.Session) {
	t.Helper()
	got, err := f.Service.GetSession(context.Background(), f.Actor, se.ID)
	require.NoError(t, err)
	require.Equal(t, "ready", got.Status)
	require.Empty(t, got.ActiveTaskID, "终态任务必须释放会话")
}

func offlineExecutionWaitCommands(t *testing.T, f *managementtest.Fixture, taskID string) {
	t.Helper()
	require.Eventually(t, func() bool {
		docs, err := f.Store.List(context.Background(), "commands", mg.Selection{TenantID: f.Actor.TenantID, Query: mg.Query{TaskID: taskID}, Visibility: mg.Visibility{All: true}})
		if err != nil || len(docs) == 0 {
			return false
		}
		for _, doc := range docs {
			var cmd mg.Command
			if json.Unmarshal(doc.Data, &cmd) != nil || cmd.State != "applied" {
				return false
			}
		}
		return true
	}, 5*time.Second, 5*time.Millisecond, "任务的持久命令应全部应用")
}

func TestOfflineExecutionTaskOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		result    mg.RunResult
		err       error
		state     mg.TaskState
		errorKind string
	}{
		{name: "成功", result: mg.RunResult{Outcome: "completed", Output: []byte(`{"结果":"完成"}`)}, state: mg.Succeeded},
		{name: "业务失败", result: mg.RunResult{Outcome: "failed", ErrorKind: "business_rejected"}, state: mg.Failed, errorKind: "business_rejected"},
		{name: "执行报错", err: errors.New("离线执行失败"), state: mg.Failed, errorKind: "execution"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := managementtest.NewFixture(t)
			se := f.ReadySession(t)
			var calls atomic.Int32
			f.Factory.RunFunc = func(context.Context, mg.RunSpec) (mg.RunResult, error) {
				calls.Add(1)
				return tc.result, tc.err
			}
			task := offlineExecutionSubmit(t, f, se, "执行")
			f.StartWorker(t)
			got := offlineExecutionWaitTask(t, f, task.ID, tc.state)
			require.Equal(t, tc.errorKind, got.ErrorKind)
			require.NotNil(t, got.FinishedAt)
			require.Equal(t, task.ID, got.RunID)
			require.Equal(t, task.ID, got.ExecutionKey)
			if tc.state == mg.Succeeded {
				require.JSONEq(t, string(tc.result.Output), string(got.Result))
				require.NotEmpty(t, got.ResultRef)
			}
			// 相同请求在完成后仍返回原接纳结果，不产生第二条执行命令。
			again := offlineExecutionSubmit(t, f, se, "执行")
			require.Equal(t, task.ID, again.ID)
			require.Equal(t, int32(1), calls.Load())
			run, err := f.Service.GetRun(ctx, f.Actor, task.ID)
			require.NoError(t, err)
			require.Len(t, run.Attempts, 1)
			require.Equal(t, 1, run.Attempts[0].Number)
			require.Equal(t, string(tc.state), run.Attempts[0].Outcome)
			require.NotNil(t, run.Attempts[0].FinishedAt)
			require.NotEmpty(t, run.Attempts[0].WorkerID)
			offlineExecutionWaitCommands(t, f, task.ID)
			require.Len(t, offlineExecutionDocuments(t, f, "commands", mg.Query{TaskID: task.ID}), 1)
			docs := offlineExecutionDocuments(t, f, "outbox", mg.Query{})
			require.Len(t, docs, 1)
			var delivery mg.Delivery
			require.NoError(t, json.Unmarshal(docs[0].Data, &delivery))
			require.Equal(t, task.ID, delivery.ResourceID)
			require.Equal(t, "task."+string(tc.state), delivery.Kind)
			require.Equal(t, "pending", delivery.State)
			offlineExecutionReleased(t, f, se)
		})
	}
}

func TestOfflineExecutionInputResume(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	var calls atomic.Int32
	resumed := make(chan mg.RunSpec, 1)
	point := mg.WaitPoint{ID: "input-wait", Kind: "input", Key: "answer", IntentHash: "input-intent", Info: []byte(`{"提示":"请补充输入"}`)}
	f.Factory.RunFunc = func(_ context.Context, spec mg.RunSpec) (mg.RunResult, error) {
		if calls.Add(1) == 1 {
			return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{point}}, nil
		}
		select {
		case resumed <- spec:
		default:
		}
		return mg.RunResult{Outcome: "completed", Output: []byte(spec.ResumeInputs[point.Key])}, nil
	}
	task := offlineExecutionSubmit(t, f, se, "等待输入")
	f.StartWorker(t)
	waiting := offlineExecutionWaitTask(t, f, task.ID, mg.WaitingInput)
	require.Len(t, waiting.Waits, 1)
	require.Equal(t, int64(1), waiting.Waits[0].Epoch)
	require.Nil(t, waiting.FinishedAt)
	active, err := f.Service.GetSession(ctx, f.Actor, se.ID)
	require.NoError(t, err)
	require.Equal(t, task.ID, active.ActiveTaskID)
	require.Empty(t, offlineExecutionDocuments(t, f, "outbox", mg.Query{}))
	req := mg.ResumeTaskRequest{IdempotencyKey: "恢复输入", TaskID: task.ID, ExpectedRevision: waiting.Revision, WaitID: point.ID, Input: []byte(`{"回答":"确认"}`)}
	cmd, err := f.Service.ResumeTask(ctx, f.Actor, req)
	require.NoError(t, err)
	again, err := f.Service.ResumeTask(ctx, f.Actor, req)
	require.NoError(t, err)
	require.Equal(t, cmd.ID, again.ID)
	got := offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
	require.JSONEq(t, string(req.Input), string(got.Result))
	require.Empty(t, got.Waits)
	require.Equal(t, int32(2), calls.Load())
	require.Eventually(t, func() bool { return len(resumed) == 1 }, time.Second, time.Millisecond)
	spec := <-resumed
	require.Equal(t, task.ExecutionKey, spec.Task.ExecutionKey)
	require.Equal(t, task.Budget, spec.Task.Budget)
	require.Equal(t, map[string]string{point.Key: string(req.Input)}, spec.ResumeInputs)
	run, err := f.Service.GetRun(ctx, f.Actor, task.ID)
	require.NoError(t, err)
	require.Len(t, run.Attempts, 2)
	outcomes := map[int]string{}
	for _, attempt := range run.Attempts {
		require.NotNil(t, attempt.FinishedAt)
		outcomes[attempt.Number] = attempt.Outcome
	}
	require.Equal(t, map[int]string{1: string(mg.WaitingInput), 2: string(mg.Succeeded)}, outcomes)
	offlineExecutionWaitCommands(t, f, task.ID)
	offlineExecutionReleased(t, f, se)
}

func TestOfflineExecutionApprovalDecisions(t *testing.T) {
	for _, tc := range []struct{ name, decision, state string }{
		{"同意", "approve", "approved"}, {"拒绝", "reject", "rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := managementtest.NewFixture(t)
			se := f.ReadySession(t)
			var calls, effects atomic.Int32
			answers := make(chan mg.ApprovalAnswer, 1)
			point := mg.WaitPoint{ID: "approval-wait", Kind: "approval", Key: "refund", IntentHash: "refund-intent", Info: []byte(`{"金额":50}`)}
			f.Factory.RunFunc = func(_ context.Context, spec mg.RunSpec) (mg.RunResult, error) {
				if calls.Add(1) == 1 {
					return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{point}}, nil
				}
				var answer mg.ApprovalAnswer
				if err := json.Unmarshal([]byte(spec.ResumeInputs[point.Key]), &answer); err != nil {
					return mg.RunResult{}, err
				}
				select {
				case answers <- answer:
				default:
				}
				if answer.Decision == "approve" {
					effects.Add(1)
				}
				return mg.RunResult{Outcome: "completed", Output: []byte(spec.ResumeInputs[point.Key])}, nil
			}
			task := offlineExecutionSubmit(t, f, se, "审批任务")
			f.StartWorker(t)
			waiting := offlineExecutionWaitTask(t, f, task.ID, mg.WaitingApproval)
			require.Zero(t, effects.Load())
			page, err := f.Service.ListApprovals(ctx, f.Actor, mg.Query{TaskID: task.ID, Statuses: []string{"pending"}})
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			ap := page.Items[0]
			require.Equal(t, waiting.Waits[0], ap.Wait)
			_, err = f.Service.ResumeTask(ctx, f.Actor, mg.ResumeTaskRequest{IdempotencyKey: "绕过审批", TaskID: task.ID, ExpectedRevision: waiting.Revision, WaitID: point.ID, Input: []byte(`"approve"`)})
			require.ErrorIs(t, err, agentErrors.ErrManagementApprovalRequired)
			req := mg.DecisionRequest{IdempotencyKey: "审批决定", ApprovalID: ap.ID, ExpectedRevision: ap.Revision, Decision: tc.decision, Reason: "离线审核结论"}
			decided, err := f.Service.DecideApproval(ctx, f.Actor, req)
			require.NoError(t, err)
			again, err := f.Service.DecideApproval(ctx, f.Actor, req)
			require.NoError(t, err)
			require.Equal(t, decided.CommandID, again.CommandID)
			offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
			require.Equal(t, int32(2), calls.Load())
			wantEffects := int32(0)
			if tc.decision == "approve" {
				wantEffects = 1
			}
			require.Equal(t, wantEffects, effects.Load())
			require.Eventually(t, func() bool { return len(answers) == 1 }, time.Second, time.Millisecond)
			require.Equal(t, mg.ApprovalAnswer{ApprovalID: ap.ID, Decision: tc.decision, IntentHash: point.IntentHash}, <-answers)
			stored, err := f.Service.GetApproval(ctx, f.Actor, ap.ID)
			require.NoError(t, err)
			require.Equal(t, tc.state, stored.State)
			require.Equal(t, tc.decision, stored.Decision)
			require.Equal(t, f.Actor, stored.Actor)
			require.Equal(t, req.Reason, stored.Reason)
			require.NotNil(t, stored.DecidedAt)
			require.Equal(t, "applied", stored.CommandState)
			require.Equal(t, decided.CommandID, stored.CommandID)
			req.IdempotencyKey = "重复决定"
			req.ExpectedRevision = stored.Revision
			_, err = f.Service.DecideApproval(ctx, f.Actor, req)
			require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
			offlineExecutionWaitCommands(t, f, task.ID)
			require.Len(t, offlineExecutionDocuments(t, f, "commands", mg.Query{TaskID: task.ID}), 2)
			offlineExecutionReleased(t, f, se)
		})
	}
}

func TestOfflineExecutionCancel(t *testing.T) {
	for _, state := range []mg.TaskState{mg.Queued, mg.Running, mg.WaitingInput, mg.WaitingApproval} {
		t.Run(string(state), func(t *testing.T) {
			ctx := context.Background()
			f := managementtest.NewFixture(t)
			se := f.ReadySession(t)
			var calls atomic.Int32
			exited := make(chan struct{}, 1)
			f.Factory.RunFunc = func(ctx context.Context, _ mg.RunSpec) (mg.RunResult, error) {
				calls.Add(1)
				if state == mg.WaitingInput || state == mg.WaitingApproval {
					kind := "input"
					if state == mg.WaitingApproval {
						kind = "approval"
					}
					return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "cancel-wait", Kind: kind, Key: "cancel-key", IntentHash: "cancel-intent", Info: []byte(`{}`)}}}, nil
				}
				<-ctx.Done()
				select {
				case exited <- struct{}{}:
				default:
				}
				return mg.RunResult{}, ctx.Err()
			}
			task := offlineExecutionSubmit(t, f, se, "取消任务")
			if state != mg.Queued {
				f.StartWorker(t)
				task = offlineExecutionWaitTask(t, f, task.ID, state)
				if state == mg.Running {
					require.Eventually(t, func() bool { return calls.Load() == 1 }, time.Second, time.Millisecond, "取消前必须实际进入运行回调")
				}
			}
			cancelled, err := f.Service.CancelTask(ctx, f.Actor, mg.CancelTaskRequest{IdempotencyKey: "取消", TaskID: task.ID, ExpectedRevision: task.Revision, Reason: "用户撤回请求"})
			require.NoError(t, err)
			require.True(t, cancelled.CancelRequested)
			if state == mg.Running {
				require.Equal(t, mg.Canceling, cancelled.State)
			} else {
				require.Equal(t, mg.Canceled, cancelled.State)
			}
			if state == mg.Queued {
				f.StartWorker(t)
			}
			got := offlineExecutionWaitTask(t, f, task.ID, mg.Canceled)
			require.NotNil(t, got.FinishedAt)
			offlineExecutionWaitCommands(t, f, task.ID)
			if state == mg.Queued {
				require.Zero(t, calls.Load(), "排队取消不能调用运行时")
				require.Empty(t, offlineExecutionDocuments(t, f, "attempts", mg.Query{TaskID: task.ID}))
			} else {
				require.Equal(t, int32(1), calls.Load())
			}
			if state == mg.Running {
				require.Eventually(t, func() bool { return len(exited) == 1 }, time.Second, time.Millisecond, "运行时应收到取消信号")
			}
			if state == mg.WaitingApproval {
				page, err := f.Service.ListApprovals(ctx, f.Actor, mg.Query{TaskID: task.ID})
				require.NoError(t, err)
				require.Len(t, page.Items, 1)
				ap := page.Items[0]
				require.Equal(t, "superseded", ap.State)
				_, err = f.Service.DecideApproval(ctx, f.Actor, mg.DecisionRequest{IdempotencyKey: "取消后审批", ApprovalID: ap.ID, ExpectedRevision: ap.Revision, Decision: "approve"})
				require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
			}
			_, err = f.Service.ResumeTask(ctx, f.Actor, mg.ResumeTaskRequest{IdempotencyKey: "取消后恢复", TaskID: task.ID, ExpectedRevision: got.Revision, WaitID: "cancel-wait", Input: []byte(`{}`)})
			require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
			offlineExecutionReleased(t, f, se)
		})
	}
}

type offlineExecutionSink func(context.Context, mg.Delivery) error

func (fn offlineExecutionSink) Accept(ctx context.Context, d mg.Delivery) error { return fn(ctx, d) }

func offlineExecutionDispatcher(t *testing.T, s *mg.Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mg.NewOutboxDispatcher(s).Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("外部待办投递器未退出")
		}
	})
}

func offlineExecutionWaitDelivery(t *testing.T, f *managementtest.Fixture, id, state string) mg.Delivery {
	t.Helper()
	require.Eventually(t, func() bool {
		doc, err := f.Store.Get(context.Background(), "outbox", f.Actor.TenantID, id)
		var d mg.Delivery
		return err == nil && json.Unmarshal(doc.Data, &d) == nil && d.State == state && d.Attempts > 0
	}, 5*time.Second, 5*time.Millisecond, "投递记录应进入状态 %s", state)
	doc, err := f.Store.Get(context.Background(), "outbox", f.Actor.TenantID, id)
	require.NoError(t, err)
	var d mg.Delivery
	require.NoError(t, json.Unmarshal(doc.Data, &d))
	return d
}

func TestOfflineExecutionOutboxRetriesStableDelivery(t *testing.T) {
	f := managementtest.NewFixture(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	var calls atomic.Int32
	seen := make(chan mg.Delivery, 2)
	cfg := f.Config()
	cfg.Now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	cfg.Delivery = offlineExecutionSink(func(_ context.Context, d mg.Delivery) error {
		select {
		case seen <- d:
		default:
		}
		if calls.Add(1) == 1 {
			return errors.New("接收方暂不可用")
		}
		return nil
	})
	var err error
	f.Service, err = mg.New(cfg)
	require.NoError(t, err)
	se := f.ReadySession(t)
	task := offlineExecutionSubmit(t, f, se, "投递任务")
	f.StartWorker(t)
	offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
	docs := offlineExecutionDocuments(t, f, "outbox", mg.Query{})
	require.Len(t, docs, 1)
	offlineExecutionDispatcher(t, f.Service)
	pending := offlineExecutionWaitDelivery(t, f, docs[0].ID, "pending")
	require.Equal(t, 1, pending.Attempts)
	require.True(t, pending.AvailableAt.After(cfg.Now()), "失败投递必须持久化退避时间")
	// 推进受原子变量保护的时钟，不依赖真实退避时间或修改投递记录。
	clock.Store(pending.AvailableAt.UnixNano())
	done := offlineExecutionWaitDelivery(t, f, pending.ID, "done")
	require.Equal(t, 2, done.Attempts)
	require.Equal(t, int32(2), calls.Load())
	require.Eventually(t, func() bool { return len(seen) == 2 }, time.Second, time.Millisecond)
	first, second := <-seen, <-seen
	require.Equal(t, first.ID, second.ID, "重试应复用稳定投递标识供接收端去重")
	require.Equal(t, first.Payload, second.Payload)
	require.Equal(t, task.ID, second.ResourceID)
	require.Equal(t, "task.succeeded", second.Kind)
	require.JSONEq(t, `{"task_id":"`+task.ID+`","state":"succeeded"}`, string(second.Payload))
	require.Len(t, offlineExecutionDocuments(t, f, "outbox", mg.Query{}), 1)
	run, err := f.Service.GetRun(context.Background(), f.Actor, task.ID)
	require.NoError(t, err)
	require.Equal(t, mg.Succeeded, run.Task.State)
	require.Len(t, run.Attempts, 1, "投递重试不能重新执行任务")
}

func TestOfflineExecutionPruneDependencies(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	cfg := f.Config()
	cfg.Now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	cfg.Limits = mg.DefaultLimits()
	cfg.Limits.TaskRetention = time.Hour
	cfg.Limits.IdempotencyRetention = time.Hour
	cfg.Limits.AuditRetention = time.Hour
	cfg.Delivery = offlineExecutionSink(func(context.Context, mg.Delivery) error { return nil })
	var err error
	f.Service, err = mg.New(cfg)
	require.NoError(t, err)
	se := f.ReadySession(t)
	var calls atomic.Int32
	f.Factory.RunFunc = func(context.Context, mg.RunSpec) (mg.RunResult, error) {
		switch calls.Add(1) {
		case 1:
			return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "retained-approval", Kind: "approval", Key: "approval-key", IntentHash: "retained-intent", Info: []byte(`{}`)}}}, nil
		case 2:
			return mg.RunResult{Outcome: "completed", Output: []byte(`true`)}, nil
		}
		return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "retained-input", Kind: "input", Key: "retained-key", Info: []byte(`{}`)}}}, nil
	}
	finished := offlineExecutionSubmit(t, f, se, "过期待办")
	f.StartWorker(t)
	offlineExecutionWaitTask(t, f, finished.ID, mg.WaitingApproval)
	approvals, err := f.Service.ListApprovals(ctx, f.Actor, mg.Query{TaskID: finished.ID})
	require.NoError(t, err)
	require.Len(t, approvals.Items, 1)
	ap := approvals.Items[0]
	_, err = f.Service.DecideApproval(ctx, f.Actor, mg.DecisionRequest{IdempotencyKey: "保留期审批", ApprovalID: ap.ID, ExpectedRevision: ap.Revision, Decision: "approve"})
	require.NoError(t, err)
	offlineExecutionWaitTask(t, f, finished.ID, mg.Succeeded)
	active := offlineExecutionSubmit(t, f, se, "仍在等待")
	offlineExecutionWaitTask(t, f, active.ID, mg.WaitingInput)
	counts, err := f.Service.Prune(ctx, f.Actor)
	require.NoError(t, err)
	require.Zero(t, counts["tasks"], "保留期内的终态任务不能删除")
	clock.Add(int64(2 * time.Hour))
	counts, err = f.Service.Prune(ctx, f.Actor)
	require.NoError(t, err)
	require.Zero(t, counts["tasks"], "未确认投递应阻止任务回收")
	require.Positive(t, counts["idempotency"])
	require.Positive(t, counts["audit"])
	_, err = f.Service.GetTask(ctx, f.Actor, finished.ID)
	require.NoError(t, err)
	// 只有已终止且依赖全部确认的任务可回收；未终止任务的过期幂等键仍需保留。
	keys := offlineExecutionDocuments(t, f, "idempotency", mg.Query{})
	require.Len(t, keys, 1)
	var retained mg.Idempotency
	require.NoError(t, json.Unmarshal(keys[0].Data, &retained))
	require.Equal(t, "task.submit", retained.Operation)
	require.Equal(t, "仍在等待", retained.Key)
	require.True(t, retained.ExpiresAt.Before(cfg.Now()))
	var accepted mg.Task
	require.NoError(t, json.Unmarshal(retained.Response, &accepted))
	require.Equal(t, active.ID, accepted.ID)
	docs := offlineExecutionDocuments(t, f, "outbox", mg.Query{})
	require.Len(t, docs, 1)
	offlineExecutionDispatcher(t, f.Service)
	offlineExecutionWaitDelivery(t, f, docs[0].ID, "done")
	blocked := mg.Command{Meta: mg.Meta{ID: "unconfirmed-command", TenantID: f.Actor.TenantID, Revision: 1, CreatedAt: cfg.Now()}, TaskID: finished.ID, SessionID: se.ID, Kind: "resume", State: "pending", Actor: f.Actor, AvailableAt: cfg.Now().Add(time.Hour)}
	require.NoError(t, f.Store.Put(ctx, "commands", blocked))
	counts, err = f.Service.Prune(ctx, f.Actor)
	require.NoError(t, err)
	require.Zero(t, counts["tasks"], "未确认命令也必须阻止回收")
	blocked.State = "conflict"
	blocked.Revision++
	require.NoError(t, f.Store.Put(ctx, "commands", blocked))
	// 删除任务失败时，先前删除的审批、尝试和命令也必须随事务回滚。
	failure := errors.New("注入任务删除失败")
	f.Store.FailNext("delete", "tasks", failure)
	_, err = f.Service.Prune(ctx, f.Actor)
	require.ErrorIs(t, err, failure)
	_, err = f.Service.GetTask(ctx, f.Actor, finished.ID)
	require.NoError(t, err)
	for _, kind := range []string{"commands", "attempts", "approvals"} {
		require.NotEmpty(t, offlineExecutionDocuments(t, f, kind, mg.Query{TaskID: finished.ID}), "回滚应保留依赖记录 %s", kind)
	}
	counts, err = f.Service.Prune(ctx, f.Actor)
	require.NoError(t, err)
	require.Equal(t, 1, counts["tasks"])
	_, err = f.Service.GetTask(ctx, f.Actor, finished.ID)
	require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
	for _, kind := range []string{"commands", "attempts", "approvals"} {
		require.Empty(t, offlineExecutionDocuments(t, f, kind, mg.Query{TaskID: finished.ID}), "应回收依赖记录 %s", kind)
	}
	offlineExecutionWaitTask(t, f, active.ID, mg.WaitingInput)
	require.Len(t, offlineExecutionDocuments(t, f, "attempts", mg.Query{TaskID: active.ID}), 1)
	_, err = f.Service.GetSession(ctx, f.Actor, se.ID)
	require.NoError(t, err)
	require.Len(t, offlineExecutionDocuments(t, f, "versions", mg.Query{AgentID: se.AgentID}), 1)
	events, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	require.NotEmpty(t, events, "Prune 不应清理宿主持有的会话日志")
	counts, err = f.Service.Prune(ctx, f.Actor)
	require.NoError(t, err)
	require.Zero(t, counts["tasks"], "重复回收应无额外删除")
}

func TestOfflineExecutionInvalidRuntimeResults(t *testing.T) {
	for _, tc := range []struct {
		name, errorKind string
		state           mg.TaskState
		run             func(context.Context, mg.RunSpec) (mg.RunResult, error)
	}{
		{"运行时异常", "panic", mg.Attention, func(context.Context, mg.RunSpec) (mg.RunResult, error) { panic("注入运行时异常") }},
		{"缺少结果类型", "missing_outcome", mg.Failed, func(context.Context, mg.RunSpec) (mg.RunResult, error) { return mg.RunResult{}, nil }},
		{"输出超限", "output_limit", mg.Failed, func(context.Context, mg.RunSpec) (mg.RunResult, error) {
			return mg.RunResult{Outcome: "completed", Output: []byte(`"123456789"`)}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := managementtest.NewFixture(t)
			cfg := f.Config()
			cfg.Limits = mg.DefaultLimits()
			cfg.Limits.MaxOutputBytes = 8
			var err error
			f.Service, err = mg.New(cfg)
			require.NoError(t, err)
			se := f.ReadySession(t)
			f.Factory.RunFunc = tc.run
			task := offlineExecutionSubmit(t, f, se, "异常执行")
			f.StartWorker(t)
			got := offlineExecutionWaitTask(t, f, task.ID, tc.state)
			require.Equal(t, tc.errorKind, got.ErrorKind)
			require.Empty(t, got.Result, "异常输出不能成为成功结果")
			run, err := f.Service.GetRun(context.Background(), f.Actor, task.ID)
			require.NoError(t, err)
			require.Len(t, run.Attempts, 1)
			require.Equal(t, string(tc.state), run.Attempts[0].Outcome)
			require.NotNil(t, run.Attempts[0].FinishedAt, "异常退出也应结束当前尝试")
			if tc.state == mg.Attention {
				require.Nil(t, got.FinishedAt)
				require.Empty(t, offlineExecutionDocuments(t, f, "outbox", mg.Query{}), "待人工处理不能通知任务已终结")
			} else {
				offlineExecutionReleased(t, f, se)
			}
		})
	}
}

type offlineExecutionUnknownFactory struct {
	*managementtest.Factory
	cleaned atomic.Int32
}

func (f *offlineExecutionUnknownFactory) Build(context.Context, mg.Version, mg.Resources) (mg.Binding, error) {
	return mg.Binding{Runtime: f, Cleanup: func(context.Context) error { f.cleaned.Add(1); return nil }}, nil
}

func (*offlineExecutionUnknownFactory) Inspect(context.Context, mg.RunSpec) (mg.RunEvidence, error) {
	return mg.RunEvidence{State: "unknown", Result: mg.RunResult{Operations: []durable.OperationRef{{ParentKey: "external", ID: "payment"}}}}, nil
}

func TestOfflineExecutionUnknownEvidenceStopsEffects(t *testing.T) {
	f := managementtest.NewFixture(t)
	var calls atomic.Int32
	f.Factory.RunFunc = func(context.Context, mg.RunSpec) (mg.RunResult, error) {
		calls.Add(1)
		return mg.RunResult{Outcome: "completed", Output: []byte(`true`)}, nil
	}
	factory := &offlineExecutionUnknownFactory{Factory: f.Factory}
	cfg := f.Config()
	cfg.Factories = map[string]mg.Factory{"offline": factory}
	var err error
	f.Service, err = mg.New(cfg)
	require.NoError(t, err)
	se := f.ReadySession(t)
	task := offlineExecutionSubmit(t, f, se, "未知执行证据")
	f.StartWorker(t)
	got := offlineExecutionWaitTask(t, f, task.ID, mg.Attention)
	require.Equal(t, "unknown_evidence", got.ErrorKind)
	require.Equal(t, []durable.OperationRef{{ParentKey: "external", ID: "payment"}}, got.Operations)
	require.Zero(t, calls.Load(), "未知副作用证据必须阻止普通重跑")
	require.Equal(t, int32(1), factory.cleaned.Load(), "未进入运行时也应清理已构造的绑定")
	active, err := f.Service.GetSession(context.Background(), f.Actor, se.ID)
	require.NoError(t, err)
	require.Equal(t, task.ID, active.ActiveTaskID, "未核对的副作用仍应占用会话")
}

func offlineExecutionWaitCommand(t *testing.T, f *managementtest.Fixture, id, state string) mg.Command {
	t.Helper()
	require.Eventually(t, func() bool {
		cmd, err := f.Service.GetCommand(context.Background(), f.Actor, id)
		return err == nil && cmd.State == state
	}, 5*time.Second, 5*time.Millisecond, "命令应进入状态 %s", state)
	cmd, err := f.Service.GetCommand(context.Background(), f.Actor, id)
	require.NoError(t, err)
	return cmd
}

func TestOfflineExecutionDurableInputRejectionAndApproval(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	var calls atomic.Int32
	inputInfo := json.RawMessage(`{"提示":"输入正整数"}`)
	approvalInfo := json.RawMessage(`{"kind":"approval","intent_hash":"durable-intent"}`)
	f.Factory.RunFunc = func(ctx context.Context, spec mg.RunSpec) (mg.RunResult, error) {
		calls.Add(1)
		for key, input := range spec.ResumeInputs {
			ctx = durable.WithResume(ctx, key, input)
		}
		ctx = durable.AllowInterrupt(ctx)
		amount, err := durable.AwaitInput(ctx, "amount", inputInfo, func(raw json.RawMessage) error {
			var n int
			if json.Unmarshal(raw, &n) != nil || n <= 0 {
				return errors.New("必须为正整数")
			}
			return nil
		})
		if err != nil {
			// 重复等待叶子应合并为同一个等待点，不能重复建审批或增加等待代次。
			return mg.RunResult{}, errors.Join(err, err)
		}
		_, err = durable.AwaitInput(ctx, "confirm", approvalInfo, nil)
		if err != nil {
			return mg.RunResult{}, err
		}
		return mg.RunResult{Outcome: "completed", Output: amount}, nil
	}
	task := offlineExecutionSubmit(t, f, se, "真实持久等待")
	f.StartWorker(t)
	waiting := offlineExecutionWaitTask(t, f, task.ID, mg.WaitingInput)
	require.Len(t, waiting.Waits, 1)
	point := waiting.Waits[0]
	require.Equal(t, "amount", point.InterruptID)
	require.Equal(t, durable.ChildKey(task.ExecutionKey, "interrupt/amount"), point.Key)
	require.JSONEq(t, string(inputInfo), string(point.Info))
	require.NotEmpty(t, point.IntentHash, "普通输入等待应从描述计算意图摘要")
	bad, err := f.Service.ResumeTask(ctx, f.Actor, mg.ResumeTaskRequest{IdempotencyKey: "无效输入", TaskID: task.ID, ExpectedRevision: waiting.Revision, WaitID: point.ID, Input: []byte(`-1`)})
	require.NoError(t, err)
	rejected := offlineExecutionWaitCommand(t, f, bad.ID, "conflict")
	require.Equal(t, "validation_rejected", rejected.ErrorKind)
	waiting = offlineExecutionWaitTask(t, f, task.ID, mg.WaitingInput)
	require.Len(t, waiting.Waits, 1)
	require.Equal(t, point.ID, waiting.Waits[0].ID)
	require.Equal(t, point.Epoch, waiting.Waits[0].Epoch)
	require.True(t, waiting.Waits[0].Rejected)
	require.Equal(t, "必须为正整数", waiting.Waits[0].Reason)
	_, err = f.Service.ResumeTask(ctx, f.Actor, mg.ResumeTaskRequest{IdempotencyKey: "修正输入", TaskID: task.ID, ExpectedRevision: waiting.Revision, WaitID: point.ID, Input: []byte(`7`)})
	require.NoError(t, err)
	waiting = offlineExecutionWaitTask(t, f, task.ID, mg.WaitingApproval)
	require.Len(t, waiting.Waits, 1)
	require.Equal(t, "confirm", waiting.Waits[0].InterruptID)
	require.Equal(t, "durable-intent", waiting.Waits[0].IntentHash)
	require.Greater(t, waiting.Waits[0].Epoch, point.Epoch)
	page, err := f.Service.ListApprovals(ctx, f.Actor, mg.Query{TaskID: task.ID})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	ap := page.Items[0]
	_, err = f.Service.DecideApproval(ctx, f.Actor, mg.DecisionRequest{IdempotencyKey: "确认持久等待", ApprovalID: ap.ID, ExpectedRevision: ap.Revision, Decision: "approve"})
	require.NoError(t, err)
	got := offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
	require.JSONEq(t, `7`, string(got.Result), "审批恢复时应重放已持久化的有效输入")
	require.Equal(t, int32(4), calls.Load())
	answer, ok, err := durable.InterruptAnswer(ctx, session.DurableStore(f.Sessions, se.LogID), point.Key, "amount")
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `7`, string(answer))
}

func TestOfflineExecutionDisabledAgentUnblocksOriginalCommand(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	var calls atomic.Int32
	f.Factory.RunFunc = func(context.Context, mg.RunSpec) (mg.RunResult, error) {
		calls.Add(1)
		return mg.RunResult{Outcome: "completed", Output: []byte(`true`)}, nil
	}
	task := offlineExecutionSubmit(t, f, se, "停用后恢复")
	docs := offlineExecutionDocuments(t, f, "commands", mg.Query{TaskID: task.ID})
	require.Len(t, docs, 1)
	agent, err := f.Service.GetAgent(ctx, f.Actor, se.AgentID)
	require.NoError(t, err)
	agent, err = f.Service.SetAgentEnabled(ctx, f.Actor, mg.SetEnabledRequest{IdempotencyKey: "停用代理", AgentID: agent.ID, ExpectedRevision: agent.Revision, Enabled: false})
	require.NoError(t, err)
	f.StartWorker(t)
	blocked := offlineExecutionWaitCommand(t, f, docs[0].ID, "blocked")
	require.Equal(t, "agent_disabled", blocked.ErrorKind)
	require.Zero(t, calls.Load())
	require.Empty(t, offlineExecutionDocuments(t, f, "attempts", mg.Query{TaskID: task.ID}), "阻塞接纳不能消耗执行尝试")
	_, err = f.Service.SetAgentEnabled(ctx, f.Actor, mg.SetEnabledRequest{IdempotencyKey: "重新启用代理", AgentID: agent.ID, ExpectedRevision: agent.Revision, Enabled: true})
	require.NoError(t, err)
	offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
	applied := offlineExecutionWaitCommand(t, f, blocked.ID, "applied")
	require.Empty(t, applied.ErrorKind, "解除阻塞后应清除旧错误分类")
	require.Equal(t, int32(1), calls.Load())
	require.Len(t, offlineExecutionDocuments(t, f, "commands", mg.Query{TaskID: task.ID}), 1, "重新启用应消费原命令")
}

type offlineExecutionResumePolicy struct {
	mg.TenantAuthorizer
	denied atomic.Bool
}

func (p *offlineExecutionResumePolicy) Authorize(ctx context.Context, actor mg.Actor, action string, ref mg.ResourceRef) error {
	if action == "task.resume" && p.denied.Load() {
		return agentErrors.ErrManagementForbidden
	}
	return p.TenantAuthorizer.Authorize(ctx, actor, action, ref)
}

func TestOfflineExecutionResumeRechecksPermission(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	policy := &offlineExecutionResumePolicy{}
	cfg := f.Config()
	cfg.Authorizer = policy
	var err error
	f.Service, err = mg.New(cfg)
	require.NoError(t, err)
	se := f.ReadySession(t)
	var calls atomic.Int32
	f.Factory.RunFunc = func(_ context.Context, spec mg.RunSpec) (mg.RunResult, error) {
		if calls.Add(1) == 1 {
			return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "permission-wait", Kind: "input", Key: "permission-input", Info: []byte(`{}`)}}}, nil
		}
		return mg.RunResult{Outcome: "completed", Output: []byte(spec.ResumeInputs["permission-input"])}, nil
	}
	task := offlineExecutionSubmit(t, f, se, "恢复时重查权限")
	w := f.StartWorker(t)
	waiting := offlineExecutionWaitTask(t, f, task.ID, mg.WaitingInput)
	stop, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, w.Shutdown(stop))
	cmd, err := f.Service.ResumeTask(ctx, f.Actor, mg.ResumeTaskRequest{IdempotencyKey: "已接纳恢复", TaskID: task.ID, ExpectedRevision: waiting.Revision, WaitID: waiting.Waits[0].ID, Input: []byte(`42`)})
	require.NoError(t, err)
	// 在接纳后、消费前撤回权限，验证 Worker 不沿用 Service 之前的授权结果。
	policy.denied.Store(true)
	f.StartWorker(t)
	blocked := offlineExecutionWaitCommand(t, f, cmd.ID, "blocked")
	require.Equal(t, "permission_denied", blocked.ErrorKind)
	require.Equal(t, int32(1), calls.Load())
	stillWaiting := offlineExecutionWaitTask(t, f, task.ID, mg.WaitingInput)
	require.Equal(t, waiting.Revision, stillWaiting.Revision, "权限失败应回滚任务变更")
	policy.denied.Store(false)
	got := offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
	require.JSONEq(t, `42`, string(got.Result))
	require.Equal(t, int32(2), calls.Load())
	applied := offlineExecutionWaitCommand(t, f, cmd.ID, "applied")
	require.Empty(t, applied.ErrorKind)
}
