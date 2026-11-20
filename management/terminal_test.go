package management_test

import (
	"context"
	"errors"
	"io/fs"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	mg "github.com/kart-io/goagent/management"
	mt "github.com/kart-io/goagent/management/internal/managementtest"
)

type failingRuntime struct{}

func (failingRuntime) Inspect(context.Context, mg.RunSpec) (mg.RunEvidence, error) {
	return mg.RunEvidence{State: "absent"}, nil
}

func (failingRuntime) Run(context.Context, mg.RunSpec) (mg.RunResult, error) {
	return mg.RunResult{}, errors.New("执行失败")
}

// terminalFixture 准备好会话后停掉 worker，由用例决定何时启动。
func terminalFixture(t *testing.T, build func(context.Context, mg.Resources) (mg.Binding, error)) (*mt.Fixture, mg.Session) {
	t.Helper()
	f, se := retryFixture(t, &retryFactory{build: build}, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mg.NewWorker(f.Service).Run(ctx) }()
	retryReady(t, f, se.ID)
	cancel()
	require.NoError(t, <-done)
	return f, se
}

func staticBinding(runtime mg.Runtime) func(context.Context, mg.Resources) (mg.Binding, error) {
	return func(context.Context, mg.Resources) (mg.Binding, error) { return mg.Binding{Runtime: runtime}, nil }
}

func cancelTask(t *testing.T, f *mt.Fixture, id string) mg.Task {
	t.Helper()
	task, err := f.Service.GetTask(context.Background(), f.Actor, id)
	require.NoError(t, err)
	task, err = f.Service.CancelTask(context.Background(), f.Actor, mg.CancelTaskRequest{IdempotencyKey: "cancel-" + id, TaskID: id, ExpectedRevision: task.Revision, Reason: "用户取消"})
	require.NoError(t, err)
	return task
}

func terminalDeliveries(t *testing.T, f *mt.Fixture, taskID string) []mg.Delivery {
	t.Helper()
	var out []mg.Delivery
	for _, doc := range offlineExecutionDocuments(t, f, "outbox", mg.Query{}) {
		var d mg.Delivery
		require.NoError(t, json.Unmarshal(doc.Data, &d))
		if d.ResourceID == taskID {
			out = append(out, d)
		}
	}
	return out
}

// 每条终态路径都在同一事务内释放会话、作废审批，并恰好写一条终态投递。
func TestEveryTerminalTransitionEmitsOneDelivery(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state mg.TaskState
		run   func(t *testing.T) (*mt.Fixture, mg.Session, string)
	}{
		{"直接取消 Queued", mg.Canceled, func(t *testing.T) (*mt.Fixture, mg.Session, string) {
			f, se := terminalFixture(t, staticBinding(&retryRuntime{}))
			task := offlineExecutionSubmit(t, f, se, "task")
			require.Equal(t, mg.Canceled, cancelTask(t, f, task.ID).State)
			return f, se, task.ID
		}},
		{"直接取消 WaitingApproval", mg.Canceled, func(t *testing.T) (*mt.Fixture, mg.Session, string) {
			f, se := terminalFixture(t, staticBinding(&retryRuntime{kind: "approval"}))
			f.StartWorker(t)
			task := offlineExecutionSubmit(t, f, se, "task")
			offlineExecutionWaitTask(t, f, task.ID, mg.WaitingApproval)
			require.NotEmpty(t, offlineExecutionDocuments(t, f, "approvals", mg.Query{TaskID: task.ID}))
			require.Equal(t, mg.Canceled, cancelTask(t, f, task.ID).State)
			return f, se, task.ID
		}},
		{"认领时已请求取消", mg.Canceled, func(t *testing.T) (*mt.Fixture, mg.Session, string) {
			f, se := terminalFixture(t, staticBinding(&retryRuntime{}))
			task := offlineExecutionSubmit(t, f, se, "task")
			task.CancelRequested = true
			task.Revision++
			require.NoError(t, f.Store.Put(context.Background(), "tasks", task))
			f.StartWorker(t)
			return f, se, task.ID
		}},
		{"worker 成功", mg.Succeeded, func(t *testing.T) (*mt.Fixture, mg.Session, string) {
			f, se := terminalFixture(t, staticBinding(&retryRuntime{}))
			f.StartWorker(t)
			return f, se, offlineExecutionSubmit(t, f, se, "task").ID
		}},
		{"worker 失败", mg.Failed, func(t *testing.T) (*mt.Fixture, mg.Session, string) {
			f, se := terminalFixture(t, staticBinding(failingRuntime{}))
			f.StartWorker(t)
			return f, se, offlineExecutionSubmit(t, f, se, "task").ID
		}},
		{"构建阻塞时取消", mg.Canceled, func(t *testing.T) (*mt.Fixture, mg.Session, string) {
			entered, release := make(chan struct{}), make(chan struct{})
			var builds atomic.Int32
			f, se := terminalFixture(t, func(context.Context, mg.Resources) (mg.Binding, error) {
				if builds.Add(1) == 1 {
					close(entered)
					<-release
					return mg.Binding{}, &mg.RetryableBuildError{Err: fs.ErrNotExist}
				}
				return mg.Binding{Runtime: &retryRuntime{}}, nil
			})
			f.StartWorker(t)
			task := offlineExecutionSubmit(t, f, se, "task")
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("未进入构建窗口")
			}
			require.Equal(t, mg.Canceling, cancelTask(t, f, task.ID).State)
			close(release)
			return f, se, task.ID
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, se, id := tc.run(t)
			task := offlineExecutionWaitTask(t, f, id, tc.state)
			require.NotNil(t, task.FinishedAt)
			offlineExecutionReleased(t, f, se)
			deliveries := terminalDeliveries(t, f, id)
			require.Len(t, deliveries, 1)
			require.Equal(t, "task."+string(tc.state), deliveries[0].Kind)
			require.JSONEq(t, `{"task_id":"`+id+`","state":"`+string(tc.state)+`"}`, string(deliveries[0].Payload))
			for _, doc := range offlineExecutionDocuments(t, f, "approvals", mg.Query{TaskID: id}) {
				var ap mg.Approval
				require.NoError(t, json.Unmarshal(doc.Data, &ap))
				require.NotEqual(t, "pending", ap.State, "终态任务不能留下待决审批")
			}
		})
	}
}

// flakyOutboxStore 让投递结果的保存失败 failures 次；领取（applying）照常写入。
type flakyOutboxStore struct {
	*mt.Store
	failures atomic.Int32
}

func (s *flakyOutboxStore) WithinTx(ctx context.Context, fn func(mg.Tx) error) error {
	return s.Store.WithinTx(ctx, func(tx mg.Tx) error { return fn(flakyOutboxTx{tx, s}) })
}

type flakyOutboxTx struct {
	mg.Tx
	s *flakyOutboxStore
}

func (t flakyOutboxTx) CAS(ctx context.Context, d mg.Document, expected int64) error {
	var delivery mg.Delivery
	if d.Kind == "outbox" && json.Unmarshal(d.Data, &delivery) == nil && delivery.State != "applying" && t.s.failures.Add(-1) >= 0 {
		return errors.New("保存投递结果失败")
	}
	return t.Tx.CAS(ctx, d, expected)
}

type panickySink struct {
	mu       sync.Mutex
	accepted map[string]int
}

func (s *panickySink) Accept(_ context.Context, d mg.Delivery) error {
	s.mu.Lock()
	s.accepted[d.ID]++
	first := s.accepted[d.ID] == 1
	s.mu.Unlock()
	if d.ID == "panics" && first {
		panic("接收端崩溃")
	}
	return nil
}

// 单条结果保存失败或接收端 panic 都不能让投递器退出，其余投递照常完成。
func TestOutboxRunSurvivesSaveFailureAndPanic(t *testing.T) {
	ctx := context.Background()
	store := &flakyOutboxStore{Store: mt.NewStore()}
	store.failures.Store(1)
	sink := &panickySink{accepted: map[string]int{}}
	var mu sync.Mutex
	var reports []string
	f := mt.NewFixture(t)
	cfg := f.Config()
	cfg.Store, cfg.Delivery = store, sink
	cfg.Limits = mg.Limits{OutboxPollInterval: 5 * time.Millisecond, DeliveryBackoffMax: 10 * time.Millisecond}
	cfg.Report = func(kind string, err error) {
		mu.Lock()
		reports = append(reports, kind)
		mu.Unlock()
	}
	svc, err := mg.New(cfg)
	require.NoError(t, err)
	past := time.Now().Add(-time.Minute).UTC()
	for _, id := range []string{"panics", "saves-late", "plain"} {
		require.NoError(t, store.Put(ctx, "outbox", mg.Delivery{Meta: mg.Meta{ID: id, TenantID: f.Actor.TenantID, Revision: 1, CreatedAt: past}, Kind: "task.succeeded", ResourceID: id, Payload: []byte(`{}`), State: "pending", AvailableAt: past}))
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- mg.NewOutboxDispatcher(svc).Run(runCtx) }()
	exited := false
	require.Eventually(t, func() bool {
		select {
		case err := <-done:
			exited = true
			t.Errorf("投递器提前退出：%v", err)
			return true
		default:
		}
		for _, id := range []string{"panics", "saves-late", "plain"} {
			doc, err := store.Get(ctx, "outbox", f.Actor.TenantID, id)
			var d mg.Delivery
			if err != nil || json.Unmarshal(doc.Data, &d) != nil || d.State != "done" {
				return false
			}
		}
		return true
	}, 5*time.Second, 5*time.Millisecond)
	require.False(t, exited)
	cancel()
	require.NoError(t, <-done)
	sink.mu.Lock()
	require.Equal(t, 2, sink.accepted["panics"], "panic 计为一次失败后重投")
	sink.mu.Unlock()
	mu.Lock()
	require.Contains(t, reports, "outbox_save")
	mu.Unlock()
}
