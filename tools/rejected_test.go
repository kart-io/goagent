package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func journalForTest(t *testing.T, store durable.Store) context.Context {
	t.Helper()
	return durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
}

func singleToolDispatcher(t *testing.T, tool interfaces.Tool) *Dispatcher {
	t.Helper()
	reg := NewRegistry()
	require.NoError(t, reg.Register(tool))
	d, err := NewDispatcher(reg, DispatchConfig{})
	require.NoError(t, err)
	return d
}

func TestRejectedIsJournaledResultNotFailure(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := journalForTest(t, store)
	calls := 0
	d := singleToolDispatcher(t, NewBaseTool("refund", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		return Rejected("订单不存在", map[string]any{"order_id": "A1"}), nil
	}))
	req := ToolRequest{Name: "refund", Input: &interfaces.ToolInput{CallID: "c1", Args: map[string]any{}}}
	for range 2 {
		out, err := d.Invoke(ctx, req)
		require.NoError(t, err)
		message, ok := RejectedResult(out)
		require.True(t, ok)
		require.Equal(t, "订单不存在", message)
	}
	require.Equal(t, 1, calls, "业务拒绝记为已完成，恢复时只回放")

	out, err := d.Invoke(context.Background(), ToolRequest{Name: "refund", Input: &interfaces.ToolInput{Args: map[string]any{}}})
	require.NoError(t, err)
	_, ok := RejectedResult(out)
	require.True(t, ok)
}

func TestUnsuccessfulResultOtherThanRejectedStillFails(t *testing.T) {
	for _, out := range []*interfaces.ToolOutput{
		{Success: false, Result: "x"},
		{Success: false, Result: map[string]any{"error": "rejected", "message": "m"}, Error: "boom"},
		{Success: false, Result: map[string]any{"error": "other", "message": "m"}},
	} {
		d := singleToolDispatcher(t, NewBaseTool("op", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return out, nil
		}))
		_, err := d.Invoke(context.Background(), ToolRequest{Name: "op", Input: &interfaces.ToolInput{Args: map[string]any{}}})
		require.ErrorContains(t, err, "failed")
	}
	_, ok := RejectedResult(nil)
	require.False(t, ok)
}

type failSecondSave struct {
	durable.Store
	saves int
}

func (s *failSecondSave) Save(ctx context.Context, id string, v *durable.Snapshot) error {
	s.saves++
	if s.saves == 2 {
		return errors.New("crash before result was saved")
	}
	return s.Store.Save(ctx, id, v)
}

type reconcilingTool struct {
	*BaseTool
	lookup func(*interfaces.ToolInput) (*interfaces.ToolOutput, bool, error)
	keys   []string
}

func (r *reconcilingTool) Reconcile(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, bool, error) {
	r.keys = append(r.keys, in.ExecutionKey)
	return r.lookup(in)
}

func TestDispatcherReconcilesAmbiguousCall(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lookup  func(*interfaces.ToolInput) (*interfaces.ToolOutput, bool, error)
		calls   int
		result  any
		wantErr string
	}{
		{"已生效", func(in *interfaces.ToolInput) (*interfaces.ToolOutput, bool, error) {
			return &interfaces.ToolOutput{Success: true, Result: "receipt-from-lookup"}, true, nil
		}, 1, "receipt-from-lookup", ""},
		{"已生效但被拒绝", func(in *interfaces.ToolInput) (*interfaces.ToolOutput, bool, error) {
			return Rejected("余额不足", nil), true, nil
		}, 1, nil, ""},
		{"未生效", func(in *interfaces.ToolInput) (*interfaces.ToolOutput, bool, error) { return nil, false, nil }, 2, "executed", ""},
		{"核对失败", func(in *interfaces.ToolInput) (*interfaces.ToolOutput, bool, error) {
			return nil, false, errors.New("lookup unavailable")
		}, 1, nil, "lookup unavailable"},
		{"核对结果不合法", func(in *interfaces.ToolInput) (*interfaces.ToolOutput, bool, error) {
			return &interfaces.ToolOutput{Success: false, Result: "x"}, true, nil
		}, 1, nil, "reconciled an unsuccessful result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := durable.NewFileStore(t.TempDir())
			require.NoError(t, err)
			ctx := journalForTest(t, &failSecondSave{Store: store})
			calls := 0
			var effectKey string
			tool := &reconcilingTool{lookup: tc.lookup, BaseTool: NewBaseTool("pay", "test", `{"type":"object"}`, func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				calls++
				effectKey = in.ExecutionKey
				return &interfaces.ToolOutput{Success: true, Result: "executed"}, nil
			})}
			d := singleToolDispatcher(t, tool)
			req := ToolRequest{Name: "pay", Input: &interfaces.ToolInput{CallID: "c1", Args: map[string]any{}}}
			_, err = d.Invoke(ctx, req)
			require.ErrorContains(t, err, "crash")
			out, err := d.Invoke(ctx, req)
			require.Equal(t, tc.calls, calls)
			require.Equal(t, []string{effectKey}, tool.keys, "核对使用与原调用相同的执行键")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			if tc.result != nil {
				require.Equal(t, tc.result, out.Result)
			} else {
				_, ok := RejectedResult(out)
				require.True(t, ok)
			}
		})
	}
}
