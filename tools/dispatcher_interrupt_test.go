package tools

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

// effectTool 每次执行都产生一次副作用；首次执行可模拟副作用生效后应答丢失（等到 ctx 结束才返回错误），
// 或在成功返回时让调用方放弃。
type effectTool struct {
	effects    atomic.Int32
	reconciles atomic.Int32
	lostAck    bool
	cancel     context.CancelFunc
}

func (t *effectTool) Name() string        { return "pay" }
func (t *effectTool) Description() string { return "pay" }
func (t *effectTool) ArgsSchema() string  { return `{"type":"object"}` }
func (t *effectTool) Invoke(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if t.effects.Add(1) == 1 {
		if t.lostAck {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if t.cancel != nil {
			t.cancel()
		}
	}
	return &interfaces.ToolOutput{Success: true, Result: "paid"}, nil
}

func (t *effectTool) Reconcile(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, bool, error) {
	t.reconciles.Add(1)
	return &interfaces.ToolOutput{Success: true, Result: "paid"}, t.effects.Load() > 0, nil
}

// 超时或调用方取消打断了已产生副作用的工具，恢复时不能再执行一次：
// 超时的结果未知，先经 Reconcile 核对；已经成功返回的结果照常记录并回放。
func TestDispatcherDoesNotRepeatInterruptedEffect(t *testing.T) {
	for _, test := range []struct {
		name       string
		config     DispatchConfig
		tool       func(cancel context.CancelFunc) *effectTool
		reconciles int32
	}{
		{"副作用生效后应答超过工具超时", DispatchConfig{Timeout: 20 * time.Millisecond}, func(context.CancelFunc) *effectTool {
			return &effectTool{lostAck: true}
		}, 1},
		{"成功返回时调用方取消", DispatchConfig{}, func(cancel context.CancelFunc) *effectTool {
			return &effectTool{cancel: cancel}
		}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := durable.NewFileStore(t.TempDir())
			require.NoError(t, err)
			base := durable.WithJournal(durable.WithExecutionKey(context.Background(), "run"), store)
			ctx, cancel := context.WithCancel(base)
			defer cancel()
			tool := test.tool(cancel)
			reg := NewRegistry()
			require.NoError(t, reg.Register(tool))
			d, err := NewDispatcher(reg, test.config)
			require.NoError(t, err)
			req := ToolRequest{Name: "pay", Input: &interfaces.ToolInput{Args: map[string]any{}, CallID: "c1"}}

			_, err = d.Invoke(ctx, req)
			require.Error(t, err)
			out, err := d.Invoke(base, req)
			require.NoError(t, err)
			require.Equal(t, "paid", out.Result)
			require.EqualValues(t, 1, tool.effects.Load(), "副作用只能发生一次")
			require.Equal(t, test.reconciles, tool.reconciles.Load())
		})
	}
}
