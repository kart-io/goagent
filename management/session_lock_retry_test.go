package management_test

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/session"
	mg "github.com/kart-io/goagent/management"
	mt "github.com/kart-io/goagent/management/internal/managementtest"
)

// 执行锁等待 5 秒，等待时间需覆盖一次锁超时与退避。
const sessionLockWait = 20 * time.Second

func sessionLockStartCommand(t *testing.T, f *mt.Fixture, taskID string) mg.Command {
	t.Helper()
	for _, doc := range offlineExecutionDocuments(t, f, "commands", mg.Query{TaskID: taskID}) {
		var cmd mg.Command
		require.NoError(t, json.Unmarshal(doc.Data, &cmd), "读取命令失败")
		if cmd.Kind == "start" {
			return cmd
		}
	}
	t.Fatal("缺少启动命令")
	return mg.Command{}
}

func sessionLockFixture(t *testing.T) (*mt.Fixture, mg.Session, *retryRuntime, func() error) {
	t.Helper()
	runtime := &retryRuntime{}
	factory := &retryFactory{build: func(context.Context, mg.Resources) (mg.Binding, error) {
		return mg.Binding{Runtime: runtime}, nil
	}}
	f, se := retryFixture(t, factory, "")
	f.StartWorker(t)
	retryReady(t, f, se.ID)
	se, err := f.Service.GetSession(context.Background(), f.Actor, se.ID)
	require.NoError(t, err, "读取会话失败")
	// 模拟崩溃进程尚未过期的执行租约。
	unlock, err := f.Sessions.Lock(context.Background(), se.LogID, "management/execution")
	require.NoError(t, err, "占用执行锁失败")
	t.Cleanup(func() { _ = unlock() })
	return f, se, runtime, unlock
}

func TestSessionLockBusyRequeuesUntilLeaseFrees(t *testing.T) {
	ctx := context.Background()
	f, se, runtime, unlock := sessionLockFixture(t)
	task := offlineExecutionSubmit(t, f, se, "task")
	start := sessionLockStartCommand(t, f, task.ID)
	var blocked mg.Command
	require.Eventually(t, func() bool {
		var err error
		blocked, err = f.Service.GetCommand(ctx, f.Actor, start.ID)
		return err == nil && blocked.State == "blocked"
	}, sessionLockWait, 10*time.Millisecond, "锁占用时命令应进入 blocked")
	require.Equal(t, "session_locked", blocked.ErrorKind, "应记录会话锁占用")
	preserved, err := f.Service.GetTask(ctx, f.Actor, task.ID)
	require.NoError(t, err, "读取任务失败")
	require.False(t, preserved.State.Terminal(), "锁占用不得终结任务")
	require.Equal(t, mg.Queued, preserved.State, "应恢复 claim 前状态")
	require.Nil(t, preserved.FinishedAt, "阻塞不能写终态时间")
	require.Zero(t, runtime.runs.Load(), "未取得执行锁不得运行")
	attempts := retryAttempts(t, f, task.ID)
	require.NotEmpty(t, attempts, "应保留阻塞 Attempt")
	require.Equal(t, "blocked", attempts[0].Outcome, "锁占用不能记为业务失败")

	require.NoError(t, unlock(), "释放执行锁失败")
	require.Eventually(t, func() bool {
		got, err := f.Service.GetTask(ctx, f.Actor, task.ID)
		return err == nil && got.State == mg.Succeeded
	}, sessionLockWait, 10*time.Millisecond, "锁释放后任务应完成")
	retryCommand(t, f, start.ID, "applied")
	require.Equal(t, int32(1), runtime.runs.Load(), "任务只能运行一次")
	for _, doc := range offlineExecutionDocuments(t, f, "outbox", mg.Query{}) {
		var delivery mg.Delivery
		require.NoError(t, json.Unmarshal(doc.Data, &delivery), "读取投递失败")
		require.NotEqual(t, "task.failed", delivery.Kind, "锁占用不得发业务失败事件")
	}
}

func TestSessionLockBusyCancelResolvesCanceled(t *testing.T) {
	ctx := context.Background()
	f, se, runtime, _ := sessionLockFixture(t)
	task := offlineExecutionSubmit(t, f, se, "task")
	// 在 worker 等锁期间取消，结果须经阻塞路径落为 Canceled。
	var running mg.Task
	require.Eventually(t, func() bool {
		var err error
		running, err = f.Service.GetTask(ctx, f.Actor, task.ID)
		return err == nil && running.State == mg.Running
	}, 5*time.Second, 5*time.Millisecond, "任务应进入运行")
	canceled, err := f.Service.CancelTask(ctx, f.Actor, mg.CancelTaskRequest{IdempotencyKey: "cancel", TaskID: task.ID, ExpectedRevision: running.Revision})
	require.NoError(t, err, "取消失败")
	require.Equal(t, mg.Canceling, canceled.State, "运行期取消应先进入 Canceling")
	require.Eventually(t, func() bool {
		got, err := f.Service.GetTask(ctx, f.Actor, task.ID)
		return err == nil && got.State == mg.Canceled
	}, sessionLockWait, 10*time.Millisecond, "锁占用期间的取消应落为 Canceled")
	offlineExecutionWaitCommands(t, f, task.ID)
	offlineExecutionReleased(t, f, se)
	require.Zero(t, runtime.runs.Load(), "已取消任务不得运行")
}

type failingLockSessions struct {
	*session.MemoryStore
}

func (failingLockSessions) Lock(context.Context, string, string) (func() error, error) {
	return nil, fs.ErrPermission
}

func TestSessionLockOtherErrorStillFails(t *testing.T) {
	runtime := &retryRuntime{}
	factory := &retryFactory{build: func(context.Context, mg.Resources) (mg.Binding, error) {
		return mg.Binding{Runtime: runtime}, nil
	}}
	f, se := retryFixture(t, factory, "", func(cfg *mg.Config) {
		cfg.Sessions = failingLockSessions{cfg.Sessions.(*session.MemoryStore)}
	})
	f.StartWorker(t)
	retryReady(t, f, se.ID)
	task := offlineExecutionSubmit(t, f, se, "task")
	offlineExecutionWaitTask(t, f, task.ID, mg.Failed)
	offlineExecutionWaitCommands(t, f, task.ID)
	require.Len(t, retryAttempts(t, f, task.ID), 1, "非锁占用错误不得重试")
	require.Zero(t, runtime.runs.Load(), "未取得执行锁不得运行")
}
