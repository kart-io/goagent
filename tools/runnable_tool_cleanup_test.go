package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func incrementInput() *interfaces.ToolInput {
	return &interfaces.ToolInput{Args: map[string]any{"n": float64(2)}}
}

// cleanupProbe records how the adapter invoked Close.
type cleanupProbe struct {
	mu        sync.Mutex
	closed    int
	remaining time.Duration
	doneAtRun error
}

func (p *cleanupProbe) close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed++
	p.doneAtRun = ctx.Err()
	if deadline, ok := ctx.Deadline(); ok {
		p.remaining = time.Until(deadline)
	}
	return nil
}

func probedFactory(p *cleanupProbe, calls *int) func(context.Context) (OwnedRunnable[int, int], error) {
	return func(context.Context) (OwnedRunnable[int, int], error) {
		return OwnedRunnable[int, int]{Runnable: numberRun(calls, false), Close: p.close}, nil
	}
}

func TestRunnableToolCleanupTimeoutValidation(t *testing.T) {
	calls := 0
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		ok      bool
	}{
		{"zero uses default", 0, true},
		{"one nanosecond", time.Nanosecond, true},
		{"one hour", time.Hour, true},
		{"minus one nanosecond", -time.Nanosecond, false},
		{"minus one second", -time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := runnableConfig()
			cfg.CleanupTimeout = tc.timeout
			fixed, fixedErr := NewRunnableTool(numberRun(&calls, false), cfg)
			owned, ownedErr := NewRunnableToolFactory(probedFactory(&cleanupProbe{}, &calls), cfg)
			if tc.ok {
				require.NoError(t, fixedErr)
				require.NoError(t, ownedErr)
				require.NotNil(t, fixed)
				require.NotNil(t, owned)
				return
			}
			require.ErrorContains(t, fixedErr, "cleanup timeout")
			require.ErrorContains(t, ownedErr, "cleanup timeout")
			require.Nil(t, fixed)
			require.Nil(t, owned)
		})
	}
}

func TestRunnableToolCleanupDeadline(t *testing.T) {
	for _, tc := range []struct {
		timeout, want time.Duration
	}{
		{0, 5 * time.Second},
		{50 * time.Millisecond, 50 * time.Millisecond},
		{time.Minute, time.Minute},
		{time.Hour, time.Hour},
	} {
		t.Run(fmt.Sprint(tc.timeout), func(t *testing.T) {
			p := &cleanupProbe{}
			calls := 0
			cfg := runnableConfig()
			cfg.CleanupTimeout = tc.timeout
			tool, err := NewRunnableToolFactory(probedFactory(p, &calls), cfg)
			require.NoError(t, err)
			out, err := tool.Invoke(context.Background(), incrementInput())
			require.NoError(t, err)
			require.Equal(t, 3, out.Result)
			require.Equal(t, 1, p.closed)
			require.NoError(t, p.doneAtRun)
			require.LessOrEqual(t, p.remaining, tc.want)
			require.Greater(t, p.remaining, tc.want-time.Second/2)
		})
	}
}

func TestRunnableToolCleanupIsDetachedFromParent(t *testing.T) {
	for _, mode := range []string{"canceled_during_run", "parent_deadline_shorter"} {
		t.Run(mode, func(t *testing.T) {
			p := &cleanupProbe{}
			cfg := runnableConfig()
			cfg.CleanupTimeout = time.Minute
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "parent_deadline_shorter" {
				parent, cancel = context.WithTimeout(parent, 20*time.Millisecond)
				defer cancel()
			}
			tool, err := NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
				run := core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, _ int, _ func(core.StreamChunk[int]) error) error {
					if mode == "canceled_during_run" {
						cancel()
					}
					<-ctx.Done()
					return ctx.Err()
				})
				return OwnedRunnable[int, int]{Runnable: run, Close: p.close}, nil
			}, cfg)
			require.NoError(t, err)
			_, err = tool.Invoke(parent, incrementInput())
			require.Error(t, err)
			require.Equal(t, 1, p.closed)
			require.NoError(t, p.doneAtRun, "parent cancellation must not cancel cleanup")
			require.Greater(t, p.remaining, 50*time.Second, "cleanup deadline comes from CleanupTimeout")
		})
	}
}

func TestRunnableToolSlowCleanupTimesOut(t *testing.T) {
	for _, tc := range []struct {
		name      string
		honorsCtx bool
	}{{"cooperative close", true}, {"close returns timeout error", false}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			cfg := runnableConfig()
			cfg.CleanupTimeout = 20 * time.Millisecond
			tool, err := NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
				return OwnedRunnable[int, int]{Runnable: numberRun(&calls, false), Close: func(ctx context.Context) error {
					if tc.honorsCtx {
						<-ctx.Done()
						return ctx.Err()
					}
					return errors.New("flush incomplete")
				}}, nil
			}, cfg)
			require.NoError(t, err)
			started := time.Now()
			out, err := tool.Invoke(context.Background(), incrementInput())
			require.Nil(t, out, "a failed cleanup must not expose a successful result")
			require.Error(t, err)
			if tc.honorsCtx {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.GreaterOrEqual(t, time.Since(started), cfg.CleanupTimeout)
			} else {
				require.ErrorContains(t, err, "flush incomplete")
			}
			require.Less(t, time.Since(started), 2*time.Second)
			require.Equal(t, 1, calls)
		})
	}
}

func TestRunnableToolCleanupOnEveryPath(t *testing.T) {
	for _, tc := range []struct {
		name       string
		build      func(*RunnableToolConfig[int, int], *cleanupProbe) func(context.Context) (OwnedRunnable[int, int], error)
		wantErr    string
		wantClosed int
	}{
		{"success", func(_ *RunnableToolConfig[int, int], p *cleanupProbe) func(context.Context) (OwnedRunnable[int, int], error) {
			calls := 0
			return probedFactory(p, &calls)
		}, "", 1},
		{"run error", func(_ *RunnableToolConfig[int, int], p *cleanupProbe) func(context.Context) (OwnedRunnable[int, int], error) {
			calls := 0
			return func(context.Context) (OwnedRunnable[int, int], error) {
				return OwnedRunnable[int, int]{Runnable: numberRun(&calls, true), Close: p.close}, nil
			}
		}, "execution failed", 1},
		{"encode error", func(c *RunnableToolConfig[int, int], p *cleanupProbe) func(context.Context) (OwnedRunnable[int, int], error) {
			c.Encode = func(int) (any, error) { return nil, errors.New("encode failed") }
			calls := 0
			return probedFactory(p, &calls)
		}, "encode failed", 1},
		{"waiting output", func(c *RunnableToolConfig[int, int], p *cleanupProbe) func(context.Context) (OwnedRunnable[int, int], error) {
			c.Encode = func(int) (any, error) { return nil, ErrToolWorkflowWaiting }
			calls := 0
			return probedFactory(p, &calls)
		}, ErrToolWorkflowWaiting.Error(), 1},
		{"factory error with partial cleanup", func(_ *RunnableToolConfig[int, int], p *cleanupProbe) func(context.Context) (OwnedRunnable[int, int], error) {
			return func(context.Context) (OwnedRunnable[int, int], error) {
				return OwnedRunnable[int, int]{Close: p.close}, errors.New("factory failed")
			}
		}, "factory failed", 1},
		{"nil runnable with cleanup", func(_ *RunnableToolConfig[int, int], p *cleanupProbe) func(context.Context) (OwnedRunnable[int, int], error) {
			return func(context.Context) (OwnedRunnable[int, int], error) {
				return OwnedRunnable[int, int]{Close: p.close}, nil
			}
		}, "cleanup required", 1},
		{"missing cleanup", func(_ *RunnableToolConfig[int, int], _ *cleanupProbe) func(context.Context) (OwnedRunnable[int, int], error) {
			calls := 0
			return func(context.Context) (OwnedRunnable[int, int], error) {
				return OwnedRunnable[int, int]{Runnable: numberRun(&calls, false)}, nil
			}
		}, "cleanup required", 0},
		{"decode error before factory", func(c *RunnableToolConfig[int, int], p *cleanupProbe) func(context.Context) (OwnedRunnable[int, int], error) {
			c.Decode = func(map[string]any) (int, error) { return 0, errors.New("decode failed") }
			calls := 0
			return probedFactory(p, &calls)
		}, "decode failed", 0},
		{"encode panic", func(c *RunnableToolConfig[int, int], p *cleanupProbe) func(context.Context) (OwnedRunnable[int, int], error) {
			c.Encode = func(int) (any, error) { panic("encode exploded") }
			calls := 0
			return probedFactory(p, &calls)
		}, "encode exploded", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &cleanupProbe{}
			cfg := runnableConfig()
			cfg.CleanupTimeout = 3 * time.Second
			factory := tc.build(&cfg, p)
			tool, err := NewRunnableToolFactory(factory, cfg)
			require.NoError(t, err)
			out, err := tool.Invoke(context.Background(), incrementInput())
			if tc.wantErr == "" {
				require.NoError(t, err)
				require.NotNil(t, out)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
				require.Nil(t, out)
			}
			require.Equal(t, tc.wantClosed, p.closed)
			if tc.wantClosed > 0 {
				require.NoError(t, p.doneAtRun)
				require.LessOrEqual(t, p.remaining, 3*time.Second)
				require.Greater(t, p.remaining, 2*time.Second)
			}
		})
	}
}

func TestRunnableToolConcurrentCleanupDeadlines(t *testing.T) {
	const workers = 32
	var closed atomic.Int64
	var bad atomic.Int64
	cfg := runnableConfig()
	cfg.CleanupTimeout = 2 * time.Second
	tool, err := NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
		calls := 0
		return OwnedRunnable[int, int]{Runnable: numberRun(&calls, false), Close: func(ctx context.Context) error {
			closed.Add(1)
			deadline, ok := ctx.Deadline()
			if !ok || ctx.Err() != nil || time.Until(deadline) > cfg.CleanupTimeout {
				bad.Add(1)
			}
			return nil
		}}, nil
	}, cfg)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := tool.Invoke(ctx, incrementInput())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int64(workers), closed.Load())
	require.Zero(t, bad.Load())
}

func journalDispatcher(t *testing.T, tool interfaces.Tool) (*Dispatcher, context.Context) {
	t.Helper()
	reg := NewRegistry()
	require.NoError(t, reg.Register(tool))
	d, err := NewDispatcher(reg, DispatchConfig{})
	require.NoError(t, err)
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	return d, durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
}

func TestDurableCleanupTimeoutIsNotCachedAsSuccess(t *testing.T) {
	var created atomic.Int64
	cfg := runnableConfig()
	cfg.CleanupTimeout = 10 * time.Millisecond
	tool, err := NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
		created.Add(1)
		calls := 0
		return OwnedRunnable[int, int]{Runnable: numberRun(&calls, false), Close: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}}, nil
	}, cfg)
	require.NoError(t, err)
	d, ctx := journalDispatcher(t, tool)
	req := ToolRequest{Name: "increment", Input: &interfaces.ToolInput{CallID: "slow-close", Args: map[string]any{"n": float64(2)}}}
	out, err := d.Invoke(ctx, req)
	require.Nil(t, out)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	out, err = d.Invoke(ctx, req)
	require.Nil(t, out)
	require.ErrorIs(t, err, durable.ErrOperationAttention)
	require.Equal(t, int64(1), created.Load(), "the opaque composite call is attempted at most once")
}

func TestCleanupTimeoutIsNotPartOfDurableIntent(t *testing.T) {
	var created atomic.Int64
	build := func(timeout time.Duration) interfaces.Tool {
		cfg := runnableConfig()
		cfg.CleanupTimeout = timeout
		tool, err := NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
			created.Add(1)
			calls := 0
			return OwnedRunnable[int, int]{Runnable: numberRun(&calls, false), Close: func(context.Context) error { return nil }}, nil
		}, cfg)
		require.NoError(t, err)
		return tool
	}
	d, ctx := journalDispatcher(t, build(time.Second))
	req := ToolRequest{Name: "increment", Input: &interfaces.ToolInput{CallID: "one", Args: map[string]any{"n": float64(2)}}}
	first, err := d.Invoke(ctx, req)
	require.NoError(t, err)
	reg := NewRegistry()
	require.NoError(t, reg.Register(build(time.Hour)))
	d2, err := NewDispatcher(reg, DispatchConfig{})
	require.NoError(t, err)
	second, err := d2.Invoke(ctx, req)
	require.NoError(t, err, "changing an operational timeout must not invalidate completed records")
	require.EqualValues(t, first.Result, second.Result)
	require.Equal(t, int64(1), created.Load())
}

type panicStringer struct{ id int }

func (p panicStringer) String() string { return fmt.Sprintf("stringer-%d", p.id) }

func TestRunnableToolPanicValueIsReported(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"string", "boom detail", "boom detail"},
		{"error", errors.New("wrapped failure"), "wrapped failure"},
		{"int", 42, "42"},
		{"stringer", panicStringer{id: 7}, "stringer-7"},
		{"struct", struct{ Code string }{"E1"}, "E1"},
		{"nil", nil, "nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			cfg := runnableConfig()
			cfg.Decode = func(map[string]any) (int, error) { panic(tc.value) }
			tool, err := NewRunnableTool(numberRun(&calls, false), cfg)
			require.NoError(t, err)
			out, err := tool.Invoke(context.Background(), incrementInput())
			require.Nil(t, out)
			var panicErr *agentErrors.PanicError
			require.ErrorAs(t, err, &panicErr)
			require.NotEmpty(t, panicErr.Stack)
			require.ErrorContains(t, err, tc.want)
			require.Zero(t, calls)
		})
	}
}

func TestRunnableToolPanicLocations(t *testing.T) {
	for _, tc := range []struct {
		location   string
		wantClosed int
	}{
		{"decode", 0},
		{"factory", 0},
		{"run", 1},
		{"encode", 1},
		{"close", 1},
	} {
		t.Run(tc.location, func(t *testing.T) {
			closeAttempts := 0
			cfg := runnableConfig()
			if tc.location == "decode" {
				cfg.Decode = func(map[string]any) (int, error) { panic("boom-decode") }
			}
			if tc.location == "encode" {
				cfg.Encode = func(int) (any, error) { panic("boom-encode") }
			}
			tool, err := NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
				if tc.location == "factory" {
					panic("boom-factory")
				}
				calls := 0
				run := numberRun(&calls, false)
				if tc.location == "run" {
					run = core.NewStreamRunnable(core.StreamModeFinal, func(context.Context, int, func(core.StreamChunk[int]) error) error {
						panic("boom-run")
					})
				}
				return OwnedRunnable[int, int]{Runnable: run, Close: func(context.Context) error {
					closeAttempts++
					if tc.location == "close" {
						panic("boom-close")
					}
					return nil
				}}, nil
			}, cfg)
			require.NoError(t, err)
			out, err := tool.Invoke(context.Background(), incrementInput())
			require.Nil(t, out)
			require.ErrorContains(t, err, "boom-"+tc.location)
			require.Equal(t, tc.wantClosed, closeAttempts)
		})
	}
}

func TestDispatcherSurfacesRunnableToolPanic(t *testing.T) {
	t.Run("plain", func(t *testing.T) {
		cfg := runnableConfig()
		cfg.Decode = func(map[string]any) (int, error) { panic("dispatcher-visible") }
		calls := 0
		tool, err := NewRunnableTool(numberRun(&calls, false), cfg)
		require.NoError(t, err)
		reg := NewRegistry()
		require.NoError(t, reg.Register(tool))
		d, err := NewDispatcher(reg, DispatchConfig{})
		require.NoError(t, err)
		out, err := d.Invoke(context.Background(), ToolRequest{Name: "increment", Input: incrementInput()})
		require.Nil(t, out)
		require.ErrorContains(t, err, "dispatcher-visible")
	})
	t.Run("durable", func(t *testing.T) {
		var panics atomic.Int64
		cfg := runnableConfig()
		cfg.Decode = func(map[string]any) (int, error) {
			panics.Add(1)
			panic("journaled-panic")
		}
		calls := 0
		tool, err := NewRunnableTool(numberRun(&calls, false), cfg)
		require.NoError(t, err)
		d, ctx := journalDispatcher(t, tool)
		req := ToolRequest{Name: "increment", Input: &interfaces.ToolInput{CallID: "panic", Args: map[string]any{"n": float64(2)}}}
		_, err = d.Invoke(ctx, req)
		require.ErrorContains(t, err, "journaled-panic")
		_, err = d.Invoke(ctx, req)
		require.ErrorIs(t, err, durable.ErrOperationAttention)
		require.Equal(t, int64(1), panics.Load(), "a panicking opaque call is not retried")
	})
}
