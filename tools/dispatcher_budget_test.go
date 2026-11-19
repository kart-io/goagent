package tools

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestDispatcherRootToolBudgetDoesNotChargeReplay(t *testing.T) {
	calls := 0
	reg := NewRegistry()
	require.NoError(t, reg.Register(NewBaseTool("lookup", "查询", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		return &interfaces.ToolOutput{Success: true, Result: "结果"}, nil
	})))
	dispatcher, err := NewDispatcher(reg, DispatchConfig{})
	require.NoError(t, err)
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	cfg := budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1, MaxToolCalls: 1}
	controller, err := budget.NewDurable(cfg, store)
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(budget.WithController(context.Background(), controller), "parent"), store)
	req := ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{CallID: "one", Args: map[string]any{}}}
	_, err = dispatcher.Invoke(ctx, req)
	require.NoError(t, err)
	_, err = dispatcher.Invoke(ctx, req)
	require.NoError(t, err)
	req.Input.CallID = "two"
	_, err = dispatcher.Invoke(ctx, req)
	require.ErrorIs(t, err, agentErrors.ErrModelBudgetExceeded)
	require.Equal(t, 1, calls)
	snap, err := controller.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, snap.ReservedToolCalls)
}

// 中断后的重跑是新的执行，同样消耗额度；任何中断与重启组合下，工具体的执行次数都不超过 MaxToolCalls。
func TestDispatcherToolBudgetBoundsExecutionsAcrossRestart(t *testing.T) {
	for _, tc := range []struct {
		name      string
		max       int
		wantErr   error
		wantCalls int
	}{
		{"额度只够首次执行", 1, agentErrors.ErrModelBudgetExceeded, 1},
		{"额度允许重跑", 2, nil, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := durable.NewFileStore(t.TempDir())
			require.NoError(t, err)
			calls := 0
			started := make(chan struct{})
			reg := NewRegistry()
			require.NoError(t, reg.Register(NewBaseTool("send", "发送", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				calls++
				if calls == 1 {
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return &interfaces.ToolOutput{Success: true, Result: "已发送"}, nil
			})))
			// 每次运行新建控制器与调度器，模拟进程重启后只剩持久存储。
			run := func(ctx context.Context) error {
				controller, err := budget.NewDurable(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1, MaxToolCalls: tc.max}, store)
				require.NoError(t, err)
				dispatcher, err := NewDispatcher(reg, DispatchConfig{})
				require.NoError(t, err)
				ctx = durable.WithJournal(durable.WithExecutionKey(budget.WithController(ctx, controller), "parent"), store)
				_, err = dispatcher.Invoke(ctx, ToolRequest{Name: "send", Input: &interfaces.ToolInput{CallID: "one", Args: map[string]any{}}})
				return err
			}

			ctx, cancel := context.WithCancel(context.Background())
			go func() { <-started; cancel() }()
			require.ErrorIs(t, run(ctx), context.Canceled)

			err = run(context.Background())
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantCalls, calls)
			controller, err := budget.NewDurable(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1, MaxToolCalls: tc.max}, store)
			require.NoError(t, err)
			snap, err := controller.Snapshot(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.wantCalls, snap.ReservedToolCalls)
		})
	}
}

func TestDispatcherHonorsRootDeadlineWithoutHostBind(t *testing.T) {
	reg := NewRegistry()
	calls := 0
	require.NoError(t, reg.Register(NewBaseTool("lookup", "查询", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		return &interfaces.ToolOutput{Success: true, Result: "结果"}, nil
	})))
	dispatcher, err := NewDispatcher(reg, DispatchConfig{})
	require.NoError(t, err)
	controller, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1, DeadlineUnixMilli: time.Now().Add(-time.Second).UnixMilli()})
	require.NoError(t, err)
	_, err = dispatcher.Invoke(budget.WithController(context.Background(), controller), ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{Args: map[string]any{}}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, calls)
}

func TestDispatcherRejectsVolatileJournalBudgetBeforeRecording(t *testing.T) {
	for _, inputKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "上下文执行键", true: "宿主输入执行键"}[inputKey], func(t *testing.T) {
			calls := 0
			reg := NewRegistry()
			require.NoError(t, reg.Register(NewBaseTool("lookup", "查询", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				calls++
				return &interfaces.ToolOutput{Success: true}, nil
			})))
			d, err := NewDispatcher(reg, DispatchConfig{})
			require.NoError(t, err)
			c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1, MaxToolCalls: 1})
			require.NoError(t, err)
			store, err := durable.NewFileStore(t.TempDir())
			require.NoError(t, err)
			ctx := durable.WithJournal(budget.WithController(context.Background(), c), store)
			in := &interfaces.ToolInput{CallID: "one", Args: map[string]any{}}
			if inputKey {
				in.ExecutionKey = "parent"
			} else {
				ctx = durable.WithExecutionKey(ctx, "parent")
			}
			_, err = d.Invoke(ctx, ToolRequest{Name: "lookup", Input: in})
			require.Error(t, err)
			require.Zero(t, calls)
			_, err = durable.InspectOperation(durable.WithExecutionKey(ctx, "parent"), "tool/one")
			require.ErrorIs(t, err, os.ErrNotExist, "配置错误不得留下失败日志")
		})
	}
}
