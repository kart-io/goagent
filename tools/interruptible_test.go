package tools

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func confirmTool(t *testing.T, version string, calls *int) *FunctionTool {
	t.Helper()
	b := NewFunctionToolBuilder("confirm").WithDescription("向用户确认").WithArgsSchema(`{"type":"object","properties":{}}`).
		WithFunction(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
			*calls++
			raw, err := durable.AwaitInput(ctx, "confirm", json.RawMessage(`{"question":"确认？"}`), nil)
			if err != nil {
				return nil, err
			}
			return string(raw), nil
		})
	if version != "" {
		b = b.WithInterruptible(version)
	}
	tool, err := b.Build()
	require.NoError(t, err)
	return tool
}

func interruptCtx(t *testing.T) context.Context {
	t.Helper()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	return durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
}

func dispatcherWith(t *testing.T, cfg DispatchConfig, tool interfaces.Tool) *Dispatcher {
	t.Helper()
	reg := NewRegistry()
	require.NoError(t, reg.Register(tool))
	d, err := NewDispatcher(reg, cfg)
	require.NoError(t, err)
	return d
}

func confirmRequest() ToolRequest {
	return ToolRequest{Name: "confirm", Input: &interfaces.ToolInput{CallID: "c1", Args: map[string]any{}}}
}

func TestInterruptibleToolPausesAndResumes(t *testing.T) {
	ctx := interruptCtx(t)
	calls := 0
	d := dispatcherWith(t, DispatchConfig{}, confirmTool(t, "v1", &calls))
	_, err := d.Invoke(ctx, confirmRequest())
	waits := durable.Waits(err)
	require.Len(t, waits, 1, "%v", err)
	var c durable.InterruptContinuation
	require.NoError(t, json.Unmarshal(waits[0].Continuation, &c))
	require.Equal(t, "confirm", c.ID)
	key := waits[0].Path[len(waits[0].Path)-1].Key

	out, err := d.Invoke(durable.WithResume(ctx, key, `"yes"`), confirmRequest())
	require.NoError(t, err)
	require.Equal(t, "\"yes\"", out.Result)
	require.Equal(t, 2, calls, "续跑重新执行整个工具")
}

func TestToolWithoutInterruptibleCannotPause(t *testing.T) {
	ctx := interruptCtx(t)
	calls := 0
	d := dispatcherWith(t, DispatchConfig{}, confirmTool(t, "", &calls))
	_, err := d.Invoke(ctx, confirmRequest())
	require.ErrorIs(t, err, durable.ErrWaitNotResumable)
	require.Nil(t, durable.Waits(err))
}

func TestInterruptibleToolRequiresVersion(t *testing.T) {
	calls := 0
	tool := confirmTool(t, "", &calls)
	tool.interruptible = true
	reg := NewRegistry()
	require.NoError(t, reg.Register(tool))
	_, err := NewDispatcher(reg, DispatchConfig{})
	require.ErrorContains(t, err, "必须声明版本")
}

// FunctionTool 开启中断必须声明版本，intent 哈希随之变化，旧记录会被检测到，不会悄悄按新策略处理。
func TestEnablingInterruptibleIsDetectedByIntent(t *testing.T) {
	ctx := interruptCtx(t)
	plainCalls := 0
	plain, err := NewFunctionToolBuilder("confirm").WithArgsSchema(`{"type":"object","properties":{}}`).
		WithFunction(func(context.Context, map[string]interface{}) (interface{}, error) { plainCalls++; return "ok", nil }).Build()
	require.NoError(t, err)
	_, err = dispatcherWith(t, DispatchConfig{MaxAttempts: 1}, plain).Invoke(ctx, confirmRequest())
	require.NoError(t, err)

	calls := 0
	_, err = dispatcherWith(t, DispatchConfig{MaxAttempts: 1}, confirmTool(t, "v1", &calls)).Invoke(ctx, confirmRequest())
	require.ErrorContains(t, err, "intent")
	require.Zero(t, calls)
}
