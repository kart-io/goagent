package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/require"
)

func increment(_ context.Context, in int) (int, error) { return in + 1, nil }
func compileLinear(t *testing.T, config ...Config) *core.StreamRunnable[int, int] {
	t.Helper()
	g := NewGraph[int](config...)
	require.NoError(t, g.AddNode("increment", increment))
	require.NoError(t, g.AddEdge(START, "increment"))
	require.NoError(t, g.AddEdge("increment", END))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)
	return r
}

func TestGraphBranchRunsOnlySelectedPathAndMerges(t *testing.T) {
	g := NewGraph[int]()
	require.NoError(t, g.AddNode("route", func(_ context.Context, in int) (int, error) { return in, nil }))
	require.NoError(t, g.AddNode("positive", func(_ context.Context, in int) (int, error) { return in * 2, nil }))
	require.NoError(t, g.AddNode("negative", func(_ context.Context, in int) (int, error) { return -in, nil }))
	require.NoError(t, g.AddNode("join", increment))
	require.NoError(t, g.AddEdge(START, "route"))
	require.NoError(t, g.AddBranch("route", func(_ context.Context, in int) (string, error) {
		if in >= 0 {
			return "positive", nil
		}
		return "negative", nil
	}, "positive", "negative"))
	require.NoError(t, g.AddEdge("positive", "join"))
	require.NoError(t, g.AddEdge("negative", "join"))
	require.NoError(t, g.AddEdge("join", END))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := r.Invoke(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, 7, out)
	out, err = r.Invoke(context.Background(), -3)
	require.NoError(t, err)
	require.Equal(t, 4, out)
	batch, err := r.WithConfig(core.RunnableConfig{MaxConcurrency: 2}).Batch(context.Background(), []int{2, -2, 0})
	require.NoError(t, err)
	require.Equal(t, []int{5, 3, 1}, batch)
}

func TestGraphLoopUsesUpdatedStateAndEnforcesBudget(t *testing.T) {
	g := NewGraph[int](Config{MaxSteps: 3})
	require.NoError(t, g.AddNode("increment", increment))
	require.NoError(t, g.AddEdge(START, "increment"))
	require.NoError(t, g.AddBranch("increment", func(_ context.Context, in int) (string, error) {
		if in >= 3 {
			return END, nil
		}
		return "increment", nil
	}, "increment", END))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := r.Invoke(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, 3, out)
	_, err = r.Invoke(context.Background(), -1)
	require.ErrorIs(t, err, ErrMaxSteps)
}

func TestGraphRejectsInvalidDefinitions(t *testing.T) {
	for _, scenario := range []string{"empty", "missing entry", "unknown target", "unreachable", "no exit", "missing route", "negative steps", "negative concurrency", "zero value"} {
		t.Run(scenario, func(t *testing.T) {
			g := NewGraph[int]()
			if scenario == "zero value" {
				g = &Graph[int]{}
				_, err := g.Compile(context.Background())
				require.Error(t, err)
				return
			}
			if scenario == "empty" {
				_, err := g.Compile(context.Background())
				require.Error(t, err)
				return
			}
			require.NoError(t, g.AddNode("a", increment))
			if scenario != "missing entry" {
				require.NoError(t, g.AddEdge(START, "a"))
			}
			switch scenario {
			case "unknown target":
				require.NoError(t, g.AddEdge("a", "missing"))
			case "unreachable":
				require.NoError(t, g.AddNode("unused", increment))
				require.NoError(t, g.AddEdge("unused", END))
				require.NoError(t, g.AddEdge("a", END))
			case "no exit":
				require.NoError(t, g.AddEdge("a", "a"))
			case "missing route":
			default:
				require.NoError(t, g.AddEdge("a", END))
			}
			if scenario == "negative steps" {
				g.config.MaxSteps = -1
			}
			if scenario == "negative concurrency" {
				g.config.MaxConcurrency = -1
			}
			_, err := g.Compile(context.Background())
			require.Error(t, err)
		})
	}
	g := NewGraph[int]()
	require.Error(t, g.AddNode(START, increment))
	require.Error(t, g.AddNode("", increment))
	require.Error(t, g.AddNode("nil", nil))
	require.NoError(t, g.AddNode("a", increment))
	require.Error(t, g.AddNode("a", increment))
	require.NoError(t, g.AddEdge(START, "a"))
	require.Error(t, g.AddEdge(START, "a"))
	require.Error(t, g.AddEdge(END, "a"))
	require.Error(t, g.AddEdge("a", START))
	require.NoError(t, g.AddEdge("a", END))
	require.Error(t, g.AddBranch("a", func(context.Context, int) (string, error) { return END, nil }, END))
	require.Error(t, g.AddRunnable("nil", (*core.StreamRunnable[int, int])(nil)))
}

func TestGraphCompileIsSnapshot(t *testing.T) {
	g := NewGraph[int]()
	require.NoError(t, g.AddNode("a", increment))
	require.NoError(t, g.AddEdge(START, "a"))
	require.NoError(t, g.AddEdge("a", END))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)
	require.NoError(t, g.AddNode("unreachable", increment))
	_, err = g.Compile(context.Background())
	require.Error(t, err)
	out, err := r.Invoke(context.Background(), 4)
	require.NoError(t, err)
	require.Equal(t, 5, out)
}

func TestGraphRejectsUndeclaredBranchAndPropagatesFailure(t *testing.T) {
	failure := errors.New("business error")
	for _, scenario := range []string{"node", "router", "unknown", "node panic", "router panic"} {
		t.Run(scenario, func(t *testing.T) {
			var downstream atomic.Int32
			g := NewGraph[int]()
			require.NoError(t, g.AddNode("a", func(_ context.Context, in int) (int, error) {
				if scenario == "node" {
					return 0, failure
				}
				if scenario == "node panic" {
					panic("node")
				}
				return in, nil
			}))
			require.NoError(t, g.AddNode("b", func(_ context.Context, in int) (int, error) { downstream.Add(1); return in, nil }))
			require.NoError(t, g.AddEdge(START, "a"))
			require.NoError(t, g.AddEdge("b", END))
			require.NoError(t, g.AddBranch("a", func(context.Context, int) (string, error) {
				if scenario == "router" {
					return "", failure
				}
				if scenario == "router panic" {
					panic("router")
				}
				return "undeclared", nil
			}, "b"))
			r, err := g.Compile(context.Background())
			require.NoError(t, err)
			_, err = r.Invoke(context.Background(), 1)
			require.Error(t, err)
			require.Zero(t, downstream.Load())
			if scenario == "node" || scenario == "router" {
				require.ErrorIs(t, err, failure)
			}
		})
	}
}

type workflowCallback struct {
	*core.BaseCallback
	starts, ends, failures atomic.Int32
}

func (c *workflowCallback) OnStart(context.Context, any) error   { c.starts.Add(1); return nil }
func (c *workflowCallback) OnEnd(context.Context, any) error     { c.ends.Add(1); return nil }
func (c *workflowCallback) OnError(context.Context, error) error { c.failures.Add(1); return nil }

func TestGraphRunnableCallbacksPipeAndCanceledContext(t *testing.T) {
	r := compileLinear(t)
	cb := &workflowCallback{BaseCallback: core.NewBaseCallback()}
	configured := r.WithCallbacks(cb)
	pipe := configured.Pipe(core.NewRunnableFunc(func(_ context.Context, in int) (any, error) { return in * 2, nil }))
	out, err := pipe.Invoke(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, 6, out)
	require.EqualValues(t, 1, cb.starts.Load())
	require.EqualValues(t, 1, cb.ends.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.Invoke(ctx, 0)
	require.ErrorIs(t, err, context.Canceled)
	_, err = r.Stream(ctx, 0)
	require.ErrorIs(t, err, context.Canceled)
	_, err = NewGraph[int]().Compile(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, r.GetConfig().Callbacks)
}
