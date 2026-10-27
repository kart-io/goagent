package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func runnableConfig() RunnableToolConfig[int, int] {
	return RunnableToolConfig[int, int]{Name: "increment", Description: "Increment a number", ArgsSchema: `{"type":"object","properties":{"n":{"type":"number"}},"required":["n"],"additionalProperties":false}`, Version: "v1", Decode: func(args map[string]any) (int, error) { return int(args["n"].(float64)), nil }, Encode: func(n int) (any, error) { return n, nil }}
}
func numberRun(calls *int, fail bool) *core.StreamRunnable[int, int] {
	return core.NewStreamRunnable(core.StreamModeFinal, func(_ context.Context, n int, emit func(core.StreamChunk[int]) error) error {
		*calls++
		if fail {
			return errors.New("execution failed")
		}
		return emit(core.StreamChunk[int]{Done: true, Data: n + 1})
	})
}
func TestRunnableToolCodecAndVersion(t *testing.T) {
	calls := 0
	cfg := runnableConfig()
	tool, err := NewRunnableTool(numberRun(&calls, false), cfg)
	require.NoError(t, err)
	out, err := tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"n": float64(2)}})
	require.NoError(t, err)
	require.Equal(t, 3, out.Result)
	require.Equal(t, 1, calls)
	for _, bad := range []func(*RunnableToolConfig[int, int]){func(c *RunnableToolConfig[int, int]) { c.Version = "" }, func(c *RunnableToolConfig[int, int]) { c.Decode = nil }, func(c *RunnableToolConfig[int, int]) { c.Encode = nil }, func(c *RunnableToolConfig[int, int]) { c.ArgsSchema = `{"type":"string"}` }} {
		c := cfg
		bad(&c)
		_, err = NewRunnableTool(numberRun(&calls, false), c)
		require.Error(t, err)
	}
	cfg.Decode = func(map[string]any) (int, error) { return 0, errors.New("decode failed") }
	tool, err = NewRunnableTool(numberRun(&calls, false), cfg)
	require.NoError(t, err)
	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"n": float64(2)}})
	require.ErrorContains(t, err, "decode failed")
	require.Equal(t, 1, calls)
}
func TestOpaqueToolReplayAndPending(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "failed"}[fail], func(t *testing.T) {
			calls := 0
			cfg := runnableConfig()
			tool, err := NewRunnableTool(numberRun(&calls, fail), cfg)
			require.NoError(t, err)
			reg := NewRegistry()
			require.NoError(t, reg.Register(tool))
			d, err := NewDispatcher(reg, DispatchConfig{})
			require.NoError(t, err)
			store, err := durable.NewFileStore(t.TempDir())
			require.NoError(t, err)
			ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
			req := ToolRequest{Name: "increment", Input: &interfaces.ToolInput{CallID: "one", Args: map[string]any{"n": float64(2)}}}
			_, first := d.Invoke(ctx, req)
			_, second := d.Invoke(ctx, req)
			if fail {
				require.Error(t, first)
				require.ErrorIs(t, second, durable.ErrOperationAttention)
			} else {
				require.NoError(t, first)
				require.NoError(t, second)
			}
			require.Equal(t, 1, calls)
			cfg.Version = "v2"
			changed, err := NewRunnableTool(numberRun(&calls, false), cfg)
			require.NoError(t, err)
			reg2 := NewRegistry()
			require.NoError(t, reg2.Register(changed))
			d2, err := NewDispatcher(reg2, DispatchConfig{})
			require.NoError(t, err)
			_, err = d2.Invoke(ctx, req)
			require.ErrorContains(t, err, "intent")
		})
	}
}
func TestWaitingOutputIsNotSuccess(t *testing.T) {
	calls := 0
	cfg := runnableConfig()
	cfg.Encode = func(int) (any, error) { return nil, ErrToolWorkflowWaiting }
	tool, err := NewRunnableTool(numberRun(&calls, false), cfg)
	require.NoError(t, err)
	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"n": float64(2)}})
	require.ErrorIs(t, err, ErrToolWorkflowWaiting)
}
