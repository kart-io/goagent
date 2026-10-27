package tools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestOwnedRunnableCleanup(t *testing.T) {
	for _, mode := range []string{"success", "invoke_error", "partial_factory", "close_error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			closed := 0
			calls := 0
			cfg := runnableConfig()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tool, err := NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
				run := numberRun(&calls, mode == "invoke_error")
				if mode == "cancel" {
					run = core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, _ int, _ func(core.StreamChunk[int]) error) error {
						cancel()
						return ctx.Err()
					})
				}
				owned := OwnedRunnable[int, int]{Runnable: run, Close: func(cleanCtx context.Context) error {
					require.NoError(t, cleanCtx.Err())
					closed++
					if mode == "close_error" {
						return errors.New("close failed")
					}
					return nil
				}}
				if mode == "partial_factory" {
					return owned, errors.New("factory failed")
				}
				return owned, nil
			}, cfg)
			require.NoError(t, err)
			_, err = tool.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"n": float64(2)}})
			if mode == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, 1, closed)
		})
	}
}
func TestModelToolRequiresRootBudget(t *testing.T) {
	var created atomic.Int64
	cfg := runnableConfig()
	cfg.ModelInvoking = true
	tool, err := NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
		created.Add(1)
		calls := 0
		return OwnedRunnable[int, int]{Runnable: numberRun(&calls, false), Close: func(context.Context) error { return nil }}, nil
	}, cfg)
	require.NoError(t, err)
	req := &interfaces.ToolInput{Args: map[string]any{"n": float64(2)}}
	_, err = tool.Invoke(context.Background(), req)
	require.ErrorContains(t, err, "budget")
	require.Zero(t, created.Load())
	c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1})
	require.NoError(t, err)
	ctx := budget.WithController(context.Background(), c)
	_, err = tool.Invoke(ctx, req)
	require.NoError(t, err)
	s, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	_, err = tool.Invoke(durable.WithJournal(durable.WithExecutionKey(ctx, "node"), s), req)
	require.ErrorContains(t, err, "durable")
	require.Equal(t, int64(1), created.Load())
}
func TestNestedDispatcherScope(t *testing.T) {
	var dispatcher *Dispatcher
	reg := NewRegistry()
	require.NoError(t, reg.Register(NewFunctionTool("recursive", "recursive", `{"type":"object"}`, func(ctx context.Context, _ map[string]any) (any, error) {
		return dispatcher.Invoke(ctx, ToolRequest{Name: "recursive", Input: &interfaces.ToolInput{Args: map[string]any{}}})
	})))
	var err error
	dispatcher, err = NewDispatcher(reg, DispatchConfig{MaxConcurrency: 1, Timeout: time.Second})
	require.NoError(t, err)
	_, err = dispatcher.Invoke(context.Background(), ToolRequest{Name: "recursive", Input: &interfaces.ToolInput{Args: map[string]any{}}})
	require.ErrorIs(t, err, ErrDispatcherReentry)
}
func TestOwnedAgentIsolation(t *testing.T) {
	calls := 0
	closed := 0
	cfg := runnableConfig()
	tool, err := NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
		calls++
		session := calls
		return OwnedRunnable[int, int]{Runnable: core.NewStreamRunnable(core.StreamModeFinal, func(_ context.Context, _ int, emit func(core.StreamChunk[int]) error) error {
			return emit(core.StreamChunk[int]{Done: true, Data: session})
		}), Close: func(context.Context) error { closed++; return nil }}, nil
	}, cfg)
	require.NoError(t, err)
	for i := 1; i <= 2; i++ {
		out, err := tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"n": float64(0)}})
		require.NoError(t, err)
		require.Equal(t, i, out.Result)
	}
	require.Equal(t, 2, closed)
}
func TestNestedDepthBoundary(t *testing.T) {
	var tool interfaces.Tool
	cfg := runnableConfig()
	calls := 0
	run := core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, n int, emit func(core.StreamChunk[int]) error) error {
		calls++
		if n > 0 {
			_, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"n": float64(n - 1)}})
			if err != nil {
				return err
			}
		}
		return emit(core.StreamChunk[int]{Done: true, Data: 0})
	})
	var err error
	tool, err = NewRunnableTool(run, cfg)
	require.NoError(t, err)
	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"n": float64(7)}})
	require.NoError(t, err)
	require.Equal(t, 8, calls)
	calls = 0
	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"n": float64(8)}})
	require.ErrorIs(t, err, ErrNestedExecutionLimit)
	require.Equal(t, 8, calls)
}
