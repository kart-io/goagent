package management_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/adapters"
	mt "github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/kart-io/goagent/revision"
	"github.com/stretchr/testify/require"
)

// mode=1 只允许预检成功；随后 Build 与后续预检的读取均失败，直到测试解除故障。
type retryArchive struct {
	snapshot revision.AgentRevision
	mode     atomic.Int32
	failures atomic.Int32
}

func (a *retryArchive) Load(context.Context, string) (revision.AgentRevision, error) {
	if a.mode.CompareAndSwap(1, 2) || a.mode.Load() == 0 {
		return a.snapshot, nil
	}
	a.failures.Add(1)
	return revision.AgentRevision{}, fs.ErrNotExist
}

type retryRuntime struct {
	kind     string
	runs     atomic.Int32
	inspects atomic.Int32
	inspect  func(context.Context, mg.RunSpec) (mg.RunEvidence, error)
}

func (r *retryRuntime) Inspect(ctx context.Context, spec mg.RunSpec) (mg.RunEvidence, error) {
	r.inspects.Add(1)
	if r.inspect != nil {
		return r.inspect(ctx, spec)
	}
	return mg.RunEvidence{State: "absent"}, nil
}

func (r *retryRuntime) Run(_ context.Context, spec mg.RunSpec) (mg.RunResult, error) {
	r.runs.Add(1)
	if r.kind != "" && len(spec.ResumeInputs) == 0 {
		return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "wait", Key: "wait", Kind: r.kind, IntentHash: "intent", Info: []byte(`{}`)}}}, nil
	}
	output, err := json.Marshal(spec.ResumeInputs)
	return mg.RunResult{Outcome: "completed", Output: output}, err
}

type retryFactory struct {
	check func() error
	build func(context.Context, mg.Resources) (mg.Binding, error)
}

func (f *retryFactory) Validate(context.Context, mg.Version) (mg.Capabilities, error) {
	if f.check != nil {
		if err := f.check(); err != nil {
			return mg.Capabilities{}, err
		}
	}
	return mg.Capabilities{Input: true, Approval: true, Reconcile: true}, nil
}

func (f *retryFactory) Build(ctx context.Context, _ mg.Version, resources mg.Resources) (mg.Binding, error) {
	return f.build(ctx, resources)
}

func retryFixture(t *testing.T, factory mg.Factory, revisionID string, configure ...func(*mg.Config)) (*mt.Fixture, mg.Session) {
	t.Helper()
	f := mt.NewFixture(t)
	cfg := f.Config()
	cfg.Factories = map[string]mg.Factory{"retry": factory}
	for _, change := range configure {
		change(&cfg)
	}
	var err error
	f.Service, err = mg.New(cfg)
	require.NoError(t, err, "构造服务失败")
	ctx := context.Background()
	agent, err := f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "agent", Name: "构建恢复"})
	require.NoError(t, err, "注册 Agent 失败")
	_, err = f.Service.SaveVersion(ctx, f.Actor, mg.SaveVersionRequest{IdempotencyKey: "version", AgentID: agent.ID, Version: "v1", ImplementationVersion: "impl-v1", RunnerKind: "retry", Config: []byte(`{}`), RevisionID: revisionID})
	require.NoError(t, err, "保存版本失败")
	agent, err = f.Service.PublishVersion(ctx, f.Actor, mg.PublishRequest{IdempotencyKey: "publish", AgentID: agent.ID, Version: "v1", ExpectedRevision: agent.Revision})
	require.NoError(t, err, "发布版本失败")
	se, err := f.Service.CreateSession(ctx, f.Actor, mg.CreateSessionRequest{IdempotencyKey: "session", AgentID: agent.ID})
	require.NoError(t, err, "创建会话失败")
	return f, se
}

func retryReady(t *testing.T, f *mt.Fixture, id string) {
	t.Helper()
	require.Eventually(t, func() bool {
		se, err := f.Service.GetSession(context.Background(), f.Actor, id)
		return err == nil && se.Status == "ready"
	}, 5*time.Second, 5*time.Millisecond, "会话准备超时")
}

func retryCommand(t *testing.T, f *mt.Fixture, id, state string) mg.Command {
	t.Helper()
	var cmd mg.Command
	require.Eventually(t, func() bool {
		var err error
		cmd, err = f.Service.GetCommand(context.Background(), f.Actor, id)
		return err == nil && cmd.State == state
	}, 5*time.Second, 5*time.Millisecond, "命令未进入 %s", state)
	return cmd
}

func retryAttempts(t *testing.T, f *mt.Fixture, taskID string) []mg.Attempt {
	t.Helper()
	docs := offlineExecutionDocuments(t, f, "attempts", mg.Query{TaskID: taskID, Limit: 10000})
	var result []mg.Attempt
	for _, doc := range docs {
		var attempt mg.Attempt
		require.NoError(t, json.Unmarshal(doc.Data, &attempt), "读取 Attempt 失败")
		result = append(result, attempt)
	}
	return result
}

func TestBuildRetryArchivedResumeKeepsTaskAndCommand(t *testing.T) {
	for _, kind := range []string{"input", "approval"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			snapshot, err := revision.Seal(revision.Definition{Config: []byte(`{}`), Policy: revision.Reference{ID: "policy", Version: "1"}, Runtime: revision.Runtime{Kind: "native", Version: "impl-v1", Digest: strings.Repeat("1", 64)}}, map[string][]byte{"payload": []byte("旧修订")})
			require.NoError(t, err, "封存失败")
			archive := &retryArchive{snapshot: snapshot}
			runtime := &retryRuntime{kind: kind}
			var builds atomic.Int32
			factory, err := adapters.NewArchivedFactory(archive, map[revision.Runtime]adapters.RevisionLoader{snapshot.Definition.Runtime: {
				Check: func(context.Context, revision.AgentRevision) (mg.Capabilities, error) {
					return mg.Capabilities{Input: true, Approval: true}, nil
				},
				Build: func(context.Context, adapters.RevisionEnvironment, mg.Resources) (mg.Binding, error) {
					builds.Add(1)
					return mg.Binding{Runtime: runtime}, nil
				},
			}})
			require.NoError(t, err, "构造归档加载器失败")
			f, se := retryFixture(t, factory, snapshot.ID)
			f.StartWorker(t)
			retryReady(t, f, se.ID)
			task := offlineExecutionSubmit(t, f, se, "task")
			state := mg.WaitingInput
			if kind == "approval" {
				state = mg.WaitingApproval
			}
			waiting := offlineExecutionWaitTask(t, f, task.ID, state)
			archive.mode.Store(1)
			var command mg.Command
			if kind == "input" {
				command, err = f.Service.ResumeTask(ctx, f.Actor, mg.ResumeTaskRequest{IdempotencyKey: "answer", TaskID: task.ID, ExpectedRevision: waiting.Revision, WaitID: "wait", Input: []byte(`"answer"`)})
			} else {
				approvals, e := f.Service.ListApprovals(ctx, f.Actor, mg.Query{TaskID: task.ID})
				require.NoError(t, e, "读取审批失败")
				require.Len(t, approvals.Items, 1, "应只有原审批")
				ap, e := f.Service.DecideApproval(ctx, f.Actor, mg.DecisionRequest{IdempotencyKey: "answer", ApprovalID: approvals.Items[0].ID, ExpectedRevision: approvals.Items[0].Revision, Decision: "approve"})
				require.NoError(t, e, "批准失败")
				command, err = f.Service.GetCommand(ctx, f.Actor, ap.CommandID)
			}
			require.NoError(t, err, "创建恢复命令失败")
			blocked := retryCommand(t, f, command.ID, "blocked")
			require.Equal(t, "build_unavailable", blocked.ErrorKind, "必须命中 Build 失败窗口")
			require.True(t, blocked.AvailableAt.After(time.Now()), "Build 失败必须退避，避免快速累积 Attempt")
			require.Equal(t, command.Input, blocked.Input, "原输入或审批决定不能丢失")
			preserved, err := f.Service.GetTask(ctx, f.Actor, task.ID)
			require.NoError(t, err, "读取阻塞任务失败")
			require.Equal(t, state, preserved.State, "应恢复 claim 前等待状态")
			require.Equal(t, waiting.Waits, preserved.Waits, "等待点和 Epoch 必须保留")
			require.Equal(t, waiting.ExecutionKey, preserved.ExecutionKey, "执行身份不能变化")
			require.Equal(t, snapshot.ID, preserved.RevisionID, "归档身份不能变化")
			require.False(t, preserved.RecoveryNeeded, "不得凭空增加恢复责任")
			require.Nil(t, preserved.FinishedAt, "阻塞不能写终态时间")
			locked, err := f.Service.GetSession(ctx, f.Actor, se.ID)
			require.NoError(t, err, "读取会话失败")
			require.Equal(t, task.ID, locked.ActiveTaskID, "阻塞期间必须保留原任务占用")
			require.Equal(t, int32(1), builds.Load(), "归档失败不得调用宿主 Build")
			require.Equal(t, int32(1), runtime.runs.Load(), "阻塞时不得运行任务")
			require.Equal(t, int32(1), runtime.inspects.Load(), "阻塞时不得核对运行时")
			require.Positive(t, archive.failures.Load(), "必须实际触发归档读取失败")
			attempts := retryAttempts(t, f, task.ID)
			require.Len(t, attempts, 2, "首次等待与一次阻塞各保留 Attempt")
			for _, at := range attempts {
				require.NotNil(t, at.FinishedAt, "阻塞 Attempt 必须结束")
			}
			require.Equal(t, "blocked", attempts[1].Outcome, "失败构建不能记为业务失败")
			archive.mode.Store(0)
			done := offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
			require.Equal(t, task.RunID, done.RunID, "必须恢复同一个 Run")
			retryCommand(t, f, command.ID, "applied")
			require.Equal(t, int32(2), runtime.runs.Load(), "恢复输入只能执行一次")
			var inputs map[string]string
			require.NoError(t, json.Unmarshal(done.Result, &inputs), "解析恢复输入失败")
			if kind == "input" {
				require.Equal(t, `"answer"`, inputs["wait"], "原输入未恢复")
			} else {
				var answer mg.ApprovalAnswer
				require.NoError(t, json.Unmarshal([]byte(inputs["wait"]), &answer), "解析审批输入失败")
				require.Equal(t, command.ApprovalID, answer.ApprovalID, "应使用原审批")
				require.Equal(t, "approve", answer.Decision, "原审批决定丢失")
			}
			for _, doc := range offlineExecutionDocuments(t, f, "outbox", mg.Query{}) {
				var delivery mg.Delivery
				require.NoError(t, json.Unmarshal(doc.Data, &delivery), "读取投递失败")
				require.NotEqual(t, "task.failed", delivery.Kind, "前置依赖失败不得发业务失败事件")
			}
		})
	}
}

func TestBuildRetryGenericErrorAndUnsafeCleanupDoNotRetry(t *testing.T) {
	for _, scenario := range []string{"generic", "cleanup-error", "runtime-returned", "archived-runtime-returned", "run-marker"} {
		t.Run(scenario, func(t *testing.T) {
			var builds, cleans atomic.Int32
			runtime := &retryRuntime{}
			var factory mg.Factory = &retryFactory{build: func(context.Context, mg.Resources) (mg.Binding, error) {
				builds.Add(1)
				binding := mg.Binding{Cleanup: func(context.Context) error {
					cleans.Add(1)
					if scenario == "cleanup-error" {
						return fs.ErrPermission
					}
					return nil
				}}
				if scenario == "generic" {
					return binding, fs.ErrNotExist
				}
				if scenario == "runtime-returned" || scenario == "archived-runtime-returned" {
					binding.Runtime = runtime
				}
				if scenario == "run-marker" {
					binding.Runtime = retryRunError{}
					return binding, nil
				}
				return binding, &mg.RetryableBuildError{Err: fs.ErrNotExist}
			}}
			revisionID := ""
			if scenario == "archived-runtime-returned" {
				snapshot, err := revision.Seal(revision.Definition{Config: []byte(`{}`), Policy: revision.Reference{ID: "policy", Version: "1"}, Runtime: revision.Runtime{Kind: "native", Version: "impl-v1", Digest: strings.Repeat("1", 64)}}, nil)
				require.NoError(t, err, "封存真实 adapter 测试失败")
				host := factory.(*retryFactory)
				factory, err = adapters.NewArchivedFactory(&retryArchive{snapshot: snapshot}, map[revision.Runtime]adapters.RevisionLoader{snapshot.Definition.Runtime: {
					Check: func(ctx context.Context, _ revision.AgentRevision) (mg.Capabilities, error) {
						return host.Validate(ctx, mg.Version{})
					},
					Build: func(ctx context.Context, _ adapters.RevisionEnvironment, r mg.Resources) (mg.Binding, error) {
						return host.build(ctx, r)
					},
				}})
				require.NoError(t, err, "创建真实 adapter 失败")
				revisionID = snapshot.ID
			}
			f, se := retryFixture(t, factory, revisionID)
			f.StartWorker(t)
			retryReady(t, f, se.ID)
			task := offlineExecutionSubmit(t, f, se, "task")
			offlineExecutionWaitTask(t, f, task.ID, mg.Failed)
			offlineExecutionWaitCommands(t, f, task.ID)
			require.Equal(t, int32(1), builds.Load(), "没有安全重试承诺的错误不能重试")
			require.Equal(t, int32(1), cleans.Load(), "失败清理必须执行一次")
			require.Len(t, retryAttempts(t, f, task.ID), 1, "终态错误不得产生重试 Attempt")
		})
	}
}

type retryRunError struct{}

func (retryRunError) Inspect(context.Context, mg.RunSpec) (mg.RunEvidence, error) {
	return mg.RunEvidence{State: "absent"}, nil
}

func (retryRunError) Run(context.Context, mg.RunSpec) (mg.RunResult, error) {
	return mg.RunResult{}, &mg.RetryableBuildError{Err: fs.ErrNotExist}
}

func TestBuildRetryCancelRacePreservesPriorRecovery(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		name := "new-task"
		if recovery {
			name = "prior-attempt"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var blocked atomic.Bool
			var builds atomic.Int32
			runtime := &retryRuntime{inspect: func(context.Context, mg.RunSpec) (mg.RunEvidence, error) {
				return mg.RunEvidence{State: "completed", Result: mg.RunResult{Outcome: "completed", Output: []byte(`"此前已完成"`)}}, nil
			}}
			factory := &retryFactory{
				check: func() error {
					if blocked.Load() {
						return fs.ErrNotExist
					}
					return nil
				},
				build: func(context.Context, mg.Resources) (mg.Binding, error) {
					if builds.Add(1) == 1 {
						close(entered)
						<-release
						blocked.Store(true)
						return mg.Binding{}, &mg.RetryableBuildError{Err: fs.ErrNotExist}
					}
					return mg.Binding{Runtime: runtime}, nil
				},
			}
			f, se := retryFixture(t, factory, "")
			// 先准备会话，再暂停 worker，以便构造持久的旧恢复状态。
			workerCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- mg.NewWorker(f.Service).Run(workerCtx) }()
			retryReady(t, f, se.ID)
			cancel()
			require.NoError(t, <-done, "会话准备工作器退出失败")
			task := offlineExecutionSubmit(t, f, se, "task")
			if recovery {
				task.RecoveryNeeded = true
				task.Revision++
				require.NoError(t, f.Store.Put(ctx, "tasks", task), "设置旧恢复状态失败")
			}
			f.StartWorker(t)
			t.Cleanup(unblock)
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("未进入构建窗口")
			}
			current, err := f.Service.GetTask(ctx, f.Actor, task.ID)
			require.NoError(t, err, "读取运行任务失败")
			canceled, err := f.Service.CancelTask(ctx, f.Actor, mg.CancelTaskRequest{IdempotencyKey: "cancel", TaskID: task.ID, ExpectedRevision: current.Revision})
			require.NoError(t, err, "并发取消失败")
			require.Equal(t, mg.Canceling, canceled.State, "运行期取消应先保留责任")
			unblock()
			if !recovery {
				offlineExecutionWaitTask(t, f, task.ID, mg.Canceled)
				require.Equal(t, int32(1), builds.Load(), "已知未执行的新任务无需再次构建即可取消")
				offlineExecutionReleased(t, f, se)
				return
			}
			restored := offlineExecutionWaitTask(t, f, task.ID, mg.Queued)
			require.True(t, restored.CancelRequested, "回滚不能覆盖并发取消")
			require.True(t, restored.RecoveryNeeded, "本次未执行不等于此前未执行")
			require.Greater(t, restored.Revision, canceled.Revision, "必须基于最新修订继续更新")
			require.Nil(t, restored.FinishedAt, "待核对的任务不能写终态")
			// 再次取消也不能绕过已恢复的旧执行核对。
			restored, err = f.Service.CancelTask(ctx, f.Actor, mg.CancelTaskRequest{IdempotencyKey: "cancel-again", TaskID: task.ID, ExpectedRevision: restored.Revision})
			require.NoError(t, err, "再次取消失败")
			require.Equal(t, mg.Queued, restored.State, "再次取消不能抹掉旧执行责任")
			require.True(t, restored.RecoveryNeeded, "再次取消必须保留恢复标记")
			blocked.Store(false)
			finished := offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
			require.JSONEq(t, `"此前已完成"`, string(finished.Result), "旧完成证据必须先于取消生效")
			require.Equal(t, int32(1), runtime.inspects.Load(), "恢复后必须核对旧执行")
			require.Zero(t, runtime.runs.Load(), "已有完成证据不能再次运行")
		})
	}
}

func TestBuildRetryErrorPreservesCause(t *testing.T) {
	err := &mg.RetryableBuildError{Err: fs.ErrPermission}
	require.ErrorIs(t, err, fs.ErrPermission, "重试承诺必须保留原始原因")
	var marker *mg.RetryableBuildError
	require.True(t, errors.As(err, &marker), "宿主显式标记必须可识别")
}

type retryReconciliations struct {
	mu     sync.Mutex
	record mg.Reconciliation
}

func (r *retryReconciliations) Lookup(context.Context, mg.Actor, string) (mg.Reconciliation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.record, nil
}

func TestBuildRetryAttentionCancelStillInspectsCompletedEvidence(t *testing.T) {
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var unavailable atomic.Bool
	var builds atomic.Int32
	runtime := &retryRuntime{}
	operations := []durable.OperationRef{{ParentKey: "parent", ID: "effect"}}
	runtime.inspect = func(context.Context, mg.RunSpec) (mg.RunEvidence, error) {
		if runtime.inspects.Load() == 1 {
			return mg.RunEvidence{State: "unknown", Result: mg.RunResult{Operations: operations}}, nil
		}
		return mg.RunEvidence{State: "completed", Result: mg.RunResult{Outcome: "completed", Output: []byte(`"先前已完成"`)}}, nil
	}
	factory := &retryFactory{
		check: func() error {
			if unavailable.Load() {
				return fs.ErrNotExist
			}
			return nil
		},
		build: func(context.Context, mg.Resources) (mg.Binding, error) {
			if builds.Add(1) == 2 {
				close(entered)
				<-release
				unavailable.Store(true)
				return mg.Binding{}, &mg.RetryableBuildError{Err: fs.ErrNotExist}
			}
			return mg.Binding{Runtime: runtime}, nil
		},
	}
	reader := &retryReconciliations{}
	f, se := retryFixture(t, factory, "", func(cfg *mg.Config) { cfg.Reconciliations = reader })
	f.StartWorker(t)
	t.Cleanup(unblock)
	retryReady(t, f, se.ID)
	task := offlineExecutionSubmit(t, f, se, "task")
	attention := offlineExecutionWaitTask(t, f, task.ID, mg.Attention)
	require.False(t, attention.RecoveryNeeded, "测试必须覆盖无 RecoveryNeeded 的真实 Attention")
	_, err := f.Service.CancelTask(ctx, f.Actor, mg.CancelTaskRequest{IdempotencyKey: "cancel-before-reconcile", TaskID: task.ID, ExpectedRevision: attention.Revision})
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict, "没有核对命令时必须拒绝取消，保留人工恢复入口")
	unchanged, err := f.Service.GetTask(ctx, f.Actor, task.ID)
	require.NoError(t, err, "读取未取消状态失败")
	require.False(t, unchanged.CancelRequested, "拒绝取消不能封死 ResumeTask")
	require.Equal(t, attention.Revision, unchanged.Revision, "拒绝操作不得改变任务修订号")
	versions := offlineExecutionDocuments(t, f, "versions", mg.Query{AgentID: se.AgentID})
	require.Len(t, versions, 1, "应存在唯一版本")
	var version mg.Version
	require.NoError(t, json.Unmarshal(versions[0].Data, &version), "读取版本失败")
	reader.mu.Lock()
	reader.record = mg.Reconciliation{ID: "proof", TenantID: f.Actor.TenantID, TaskID: task.ID, VersionHash: version.ConfigHash, EvidenceHash: "proof-hash", Resolution: "applied", VerifiedBy: f.Actor.Subject, VerifiedAt: time.Now(), OperationRefs: operations}
	reader.mu.Unlock()
	command, err := f.Service.ResumeTask(ctx, f.Actor, mg.ResumeTaskRequest{IdempotencyKey: "reconcile", TaskID: task.ID, ExpectedRevision: attention.Revision, ReconciliationRef: "proof"})
	require.NoError(t, err, "接纳真实核对恢复命令失败")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("未进入核对构建窗口")
	}
	running, err := f.Service.GetTask(ctx, f.Actor, task.ID)
	require.NoError(t, err, "读取运行状态失败")
	_, err = f.Service.CancelTask(ctx, f.Actor, mg.CancelTaskRequest{IdempotencyKey: "cancel", TaskID: task.ID, ExpectedRevision: running.Revision})
	require.NoError(t, err, "并发取消失败")
	unblock()
	retryCommand(t, f, command.ID, "blocked")
	restored := offlineExecutionWaitTask(t, f, task.ID, mg.Attention)
	require.True(t, restored.CancelRequested, "恢复 Attention 必须保留取消")
	require.False(t, restored.RecoveryNeeded, "不得靠改写 RecoveryNeeded 伪造恢复状态")
	restored, err = f.Service.CancelTask(ctx, f.Actor, mg.CancelTaskRequest{IdempotencyKey: "cancel-again", TaskID: task.ID, ExpectedRevision: restored.Revision})
	require.NoError(t, err, "再次取消失败")
	require.Equal(t, mg.Attention, restored.State, "CancelTask 不得跳过 Attention 的核对")
	require.Nil(t, restored.FinishedAt, "未知结果不得被提前终结")
	unavailable.Store(false)
	completed := offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
	require.JSONEq(t, `"先前已完成"`, string(completed.Result), "完成证据必须优先于取消")
	require.Equal(t, int32(2), runtime.inspects.Load(), "start 不得在 Inspect 前终结 Attention")
	require.Zero(t, runtime.runs.Load(), "核对已有结果不能重新运行")
	retryCommand(t, f, command.ID, "applied")
}

func TestBuildRetryAttemptNumbersPageAndPreserveGaps(t *testing.T) {
	ctx := context.Background()
	var blocked atomic.Bool
	runtime := &retryRuntime{}
	factory := &retryFactory{
		check: func() error {
			if blocked.Load() {
				return fs.ErrNotExist
			}
			return nil
		},
		build: func(context.Context, mg.Resources) (mg.Binding, error) { return mg.Binding{Runtime: runtime}, nil },
	}
	f, se := retryFixture(t, factory, "")
	f.StartWorker(t)
	retryReady(t, f, se.ID)
	blocked.Store(true)
	task := offlineExecutionSubmit(t, f, se, "task")
	// 201 条跨过内部 200 条分页边界，并以稀疏序号证明不能使用条数 + 1。
	created := time.Now().Add(-time.Hour).UTC()
	err := f.Store.WithinTx(ctx, func(tx mg.Tx) error {
		for i := range 201 {
			at := mg.Attempt{Meta: mg.Meta{ID: fmt.Sprintf("old-%03d", i), TenantID: f.Actor.TenantID, Revision: 1, CreatedAt: created}, TaskID: task.ID, Number: i + 5, Outcome: "blocked", FinishedAt: &created}
			if i == 200 {
				at.Number = 10001
			}
			raw, err := json.Marshal(at)
			if err != nil {
				return err
			}
			if err = tx.Insert(ctx, mg.Document{Kind: "attempts", TenantID: at.TenantID, ID: at.ID, Revision: at.Revision, CreatedAt: at.CreatedAt, Data: raw}); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err, "准备跨页历史 Attempt 失败")
	blocked.Store(false)
	offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
	attempts := retryAttempts(t, f, task.ID)
	require.Len(t, attempts, 202, "必须保留历史并新增一个 Attempt")
	require.Equal(t, 10002, attempts[len(attempts)-1].Number, "必须使用跨页最大历史序号 + 1")
}
