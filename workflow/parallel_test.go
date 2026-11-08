package workflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core"
)

func TestParallelOverlapsBoundsConcurrencyAndMergesInOrder(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	var active, maxActive atomic.Int32
	branch := func(name string, increment int) Branch[int] {
		return Branch[int]{Name: name, Run: func(ctx context.Context, in int) (int, error) {
			count := active.Add(1)
			defer active.Add(-1)
			for {
				old := maxActive.Load()
				if count <= old || maxActive.CompareAndSwap(old, count) {
					break
				}
			}
			started <- name
			select {
			case <-release:
				return in + increment, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}}
	}
	g := NewGraph[int](Config{MaxConcurrency: 2})
	require.NoError(t, g.AddParallel("fanout", ParallelConfig[int]{
		Branches: []Branch[int]{branch("a", 1), branch("b", 10), branch("c", 100)},
		Clone:    func(in int) int { return in },
		Merge: func(_ context.Context, in int, results []int) (int, error) {
			if !slices.Equal([]int{3, 12, 102}, results) {
				return 0, fmt.Errorf("out-of-order results: %v", results)
			}
			return results[0] + results[1] + results[2], nil
		},
	}))
	require.NoError(t, g.AddEdge(constants.WorkflowStart, "fanout"))
	require.NoError(t, g.AddEdge("fanout", constants.WorkflowEnd))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)
	type response struct {
		value int
		err   error
	}
	done := make(chan response, 1)
	go func() { value, err := r.Invoke(context.Background(), 2); done <- response{value, err} }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("branches did not overlap")
		}
	}
	require.EqualValues(t, 2, maxActive.Load())
	select {
	case name := <-started:
		t.Fatalf("third branch started before capacity available: %s", name)
	default:
	}
	close(release)
	select {
	case out := <-done:
		require.NoError(t, out.err)
		require.Equal(t, 117, out.value)
	case <-time.After(time.Second):
		t.Fatal("parallel execution stalled")
	}
	require.EqualValues(t, 0, active.Load())
	require.EqualValues(t, 2, maxActive.Load())
}

func TestParallelClonesMutableStateBeforeStarting(t *testing.T) {
	input := map[string]int{"value": 1}
	var cloneCalls atomic.Int32
	g := NewGraph[map[string]int](Config{MaxConcurrency: 2})
	mutate := func(value int) Node[map[string]int] {
		return func(_ context.Context, state map[string]int) (map[string]int, error) {
			if cloneCalls.Load() != 2 {
				return nil, fmt.Errorf("workers started before all inputs were cloned")
			}
			state["value"] = value
			return state, nil
		}
	}
	require.NoError(t, g.AddParallel("parallel", ParallelConfig[map[string]int]{
		Branches: []Branch[map[string]int]{{Name: "one", Run: mutate(10)}, {Name: "two", Run: mutate(20)}},
		Clone:    func(in map[string]int) map[string]int { cloneCalls.Add(1); return map[string]int{"value": in["value"]} },
		Merge: func(_ context.Context, original map[string]int, results []map[string]int) (map[string]int, error) {
			require.Equal(t, 1, original["value"])
			return map[string]int{"sum": results[0]["value"] + results[1]["value"]}, nil
		},
	}))
	require.NoError(t, g.AddEdge(constants.WorkflowStart, "parallel"))
	require.NoError(t, g.AddEdge("parallel", constants.WorkflowEnd))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := r.Invoke(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 30, out["sum"])
	require.Equal(t, 1, input["value"])
}

func TestParallelFailureAndPanicCancelSiblingsWithoutMerging(t *testing.T) {
	for _, panicBranch := range []bool{false, true} {
		siblingStarted := make(chan struct{})
		siblingStopped := make(chan struct{})
		failure := errors.New("branch failed")
		var merges atomic.Int32
		g := NewGraph[int](Config{MaxConcurrency: 2})
		require.NoError(t, g.AddParallel("parallel", ParallelConfig[int]{
			Branches: []Branch[int]{
				{Name: "failure", Run: func(ctx context.Context, in int) (int, error) {
					select {
					case <-siblingStarted:
					case <-ctx.Done():
						return 0, ctx.Err()
					}
					if panicBranch {
						panic("branch panic")
					}
					return 0, failure
				}},
				{Name: "waiting", Run: func(ctx context.Context, in int) (int, error) {
					close(siblingStarted)
					defer close(siblingStopped)
					<-ctx.Done()
					return 0, ctx.Err()
				}},
			}, Clone: func(in int) int { return in }, Merge: func(_ context.Context, in int, _ []int) (int, error) { merges.Add(1); return in, nil },
		}))
		require.NoError(t, g.AddEdge(constants.WorkflowStart, "parallel"))
		require.NoError(t, g.AddEdge("parallel", constants.WorkflowEnd))
		r, err := g.Compile(context.Background())
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err = r.Invoke(ctx, 0)
		cancel()
		require.Error(t, err)
		if !panicBranch {
			require.ErrorIs(t, err, failure)
		}
		select {
		case <-siblingStopped:
		default:
			t.Fatal("returned before sibling stopped")
		}
		require.Zero(t, merges.Load())
	}
}

func TestParallelCancellationAndMergeFailure(t *testing.T) {
	for _, mergeFails := range []bool{false, true} {
		started := make(chan struct{}, 1)
		stopped := make(chan struct{})
		failure := errors.New("merge failed")
		g := NewGraph[int]()
		require.NoError(t, g.AddParallel("parallel", ParallelConfig[int]{
			Branches: []Branch[int]{{Name: "worker", Run: func(ctx context.Context, in int) (int, error) {
				defer close(stopped)
				started <- struct{}{}
				if !mergeFails {
					<-ctx.Done()
					return 0, ctx.Err()
				}
				return in, nil
			}}},
			Clone: func(in int) int { return in }, Merge: func(context.Context, int, []int) (int, error) { return 0, failure },
		}))
		require.NoError(t, g.AddEdge(constants.WorkflowStart, "parallel"))
		require.NoError(t, g.AddEdge("parallel", constants.WorkflowEnd))
		r, err := g.Compile(context.Background())
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := r.Invoke(ctx, 1); done <- err }()
		<-started
		if !mergeFails {
			cancel()
		}
		select {
		case err := <-done:
			if mergeFails {
				require.ErrorIs(t, err, failure)
			} else {
				require.ErrorIs(t, err, context.Canceled)
			}
		case <-time.After(time.Second):
			t.Fatal("execution did not stop")
		}
		cancel()
		<-stopped
	}
}

func TestParallelRejectsInvalidConfiguration(t *testing.T) {
	for _, cfg := range []ParallelConfig[int]{
		{}, {Branches: []Branch[int]{{Name: "a", Run: increment}}},
		{Branches: []Branch[int]{{Name: "a", Run: nil}}, Clone: func(in int) int { return in }, Merge: func(_ context.Context, in int, _ []int) (int, error) { return in, nil }},
		{Branches: []Branch[int]{{Name: "a", Run: increment}, {Name: "a", Run: increment}}, Clone: func(in int) int { return in }, Merge: func(_ context.Context, in int, _ []int) (int, error) { return in, nil }},
	} {
		require.Error(t, NewGraph[int]().AddParallel("parallel", cfg))
	}
}

// 经 Adapt 的节点在独立 goroutine 中运行，取消后仍在收尾时图不能先返回，
// 否则节点的清理和副作用会发生在图结束之后。
func TestParallelWaitsForAdaptedNodeToSettle(t *testing.T) {
	failure := errors.New("branch failed")
	started := make(chan struct{})
	var settled atomic.Bool

	node := func(run func(context.Context) (int, error)) func(context.Context, int) (int, error) {
		adapted, err := Adapt(core.NewRunnableFunc(func(ctx context.Context, _ int) (int, error) { return run(ctx) }),
			func(in int) (int, error) { return in, nil },
			func(_ int, out int) (int, error) { return out, nil })
		require.NoError(t, err)
		return adapted.Invoke
	}

	g := NewGraph[int](Config{MaxConcurrency: 2})
	require.NoError(t, g.AddParallel("parallel", ParallelConfig[int]{
		Branches: []Branch[int]{
			{Name: "failure", Run: node(func(ctx context.Context) (int, error) {
				<-started
				return 0, failure
			})},
			{Name: "slow-cleanup", Run: node(func(ctx context.Context) (int, error) {
				close(started)
				<-ctx.Done()
				time.Sleep(50 * time.Millisecond)
				settled.Store(true)
				return 0, ctx.Err()
			})},
		},
		Clone: func(in int) int { return in },
		Merge: func(_ context.Context, in int, _ []int) (int, error) { return in, nil },
	}))
	require.NoError(t, g.AddEdge(constants.WorkflowStart, "parallel"))
	require.NoError(t, g.AddEdge("parallel", constants.WorkflowEnd))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)

	_, err = r.Invoke(context.Background(), 0)
	require.ErrorIs(t, err, failure)
	require.True(t, settled.Load(), "图在节点收尾前就返回了")
}

// 节点报错后可能还有收尾工作，单节点路径同样要等它结束。
func TestConsumeNodeWaitsForStreamCloseAfterCancellation(t *testing.T) {
	var settled atomic.Bool
	node := core.NewRunnableFunc(func(ctx context.Context, _ int) (int, error) {
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond)
		settled.Store(true)
		return 0, ctx.Err()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := consumeNode[int, int](ctx, node, 0, func(core.StreamChunk[int]) error { return nil })
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.True(t, settled.Load(), "consumeNode 在节点结束前就返回了")
}
