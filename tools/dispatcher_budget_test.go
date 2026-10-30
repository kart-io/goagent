package tools

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
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
	require.ErrorIs(t, err, budget.ErrBudgetExceeded)
	require.Equal(t, 1, calls)
	snap, err := controller.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, snap.ReservedToolCalls)
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
