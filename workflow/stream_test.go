package workflow

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/require"
)

// rawRunnable deliberately bypasses core's checked source to exercise the graph
// boundary against user implementations that violate the stream contract.
type rawRunnable struct {
	core.Runnable[int, int]
	chunks    []core.StreamChunk[int]
	err       error
	nilStream bool
}

func (r *rawRunnable) Stream(context.Context, int) (<-chan core.StreamChunk[int], error) {
	if r.nilStream || r.err != nil {
		return nil, r.err
	}
	chunks := make(chan core.StreamChunk[int], len(r.chunks))
	for _, chunk := range r.chunks {
		chunks <- chunk
	}
	close(chunks)
	return chunks, nil
}

func TestGraphForwardsNodeEventsButOnlyGraphFinalizes(t *testing.T) {
	g := NewGraph[int]()
	node := core.NewStreamRunnable(core.StreamModeTokens, func(ctx context.Context, in int, emit func(core.StreamChunk[int]) error) error {
		if err := emit(core.StreamChunk[int]{Event: &core.StreamEvent{Type: "text", Delta: "token", Step: "answer"}}); err != nil {
			return err
		}
		return emit(core.StreamChunk[int]{Done: true, Data: in + 10})
	})
	require.NoError(t, g.AddRunnable("model", node))
	require.NoError(t, g.AddNode("increment", increment))
	require.NoError(t, g.AddEdge(START, "model"))
	require.NoError(t, g.AddEdge("model", "increment"))
	require.NoError(t, g.AddEdge("increment", END))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)
	require.Equal(t, core.StreamModeSteps, r.StreamMode())
	stream, err := r.Stream(context.Background(), 1)
	require.NoError(t, err)
	var types, steps []string
	var finals []int
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		if chunk.Done {
			finals = append(finals, chunk.Data)
		}
		if chunk.Event != nil {
			types = append(types, chunk.Event.Type)
			steps = append(steps, chunk.Event.Step)
			if chunk.Event.Type == "text" {
				require.Equal(t, "token", chunk.Event.Delta)
			}
		}
	}
	require.Equal(t, []string{"node_start", "text", "node_end", "node_start", "node_end"}, types)
	require.Equal(t, []string{"model", "model/answer", "model", "increment", "increment"}, steps)
	require.Equal(t, []int{12}, finals)
}

func TestGraphRejectsBrokenNodeStreamsBeforeDownstream(t *testing.T) {
	failure := errors.New("late error")
	for _, scenario := range []struct {
		name  string
		node  *rawRunnable
		cause error
	}{
		{"missing final", &rawRunnable{chunks: []core.StreamChunk[int]{{Data: 1}}}, io.ErrUnexpectedEOF},
		{"duplicate final", &rawRunnable{chunks: []core.StreamChunk[int]{{Done: true, Data: 1}, {Done: true, Data: 2}}}, nil},
		{"late error", &rawRunnable{chunks: []core.StreamChunk[int]{{Done: true, Data: 1}, {Error: failure}}}, failure},
		{"late data", &rawRunnable{chunks: []core.StreamChunk[int]{{Done: true, Data: 1}, {Data: 2}}}, nil},
		{"initial error", &rawRunnable{err: failure}, failure},
		{"nil stream", &rawRunnable{nilStream: true}, nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var downstream atomic.Int32
			g := NewGraph[int]()
			require.NoError(t, g.AddRunnable("broken", scenario.node))
			require.NoError(t, g.AddNode("downstream", func(_ context.Context, in int) (int, error) { downstream.Add(1); return in, nil }))
			require.NoError(t, g.AddEdge(START, "broken"))
			require.NoError(t, g.AddEdge("broken", "downstream"))
			require.NoError(t, g.AddEdge("downstream", END))
			r, err := g.Compile(context.Background())
			require.NoError(t, err)
			cb := &workflowCallback{BaseCallback: core.NewBaseCallback()}
			stream, err := r.WithCallbacks(cb).Stream(context.Background(), 1)
			require.NoError(t, err)
			terminal := 0
			for chunk := range stream {
				if chunk.Done {
					terminal++
					require.Error(t, chunk.Error)
					if scenario.cause != nil {
						require.ErrorIs(t, chunk.Error, scenario.cause)
					}
				}
			}
			require.Equal(t, 1, terminal)
			require.Zero(t, downstream.Load())
			require.EqualValues(t, 1, cb.failures.Load())
			require.Zero(t, cb.ends.Load())
		})
	}
}

func TestGraphStreamCancellationStopsNodeProducer(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	node := core.NewStreamRunnable(core.StreamModeTokens, func(ctx context.Context, in int, emit func(core.StreamChunk[int]) error) error {
		close(started)
		defer close(stopped)
		for {
			if err := emit(core.StreamChunk[int]{Event: &core.StreamEvent{Type: "text", Delta: "chunk"}}); err != nil {
				return err
			}
		}
	})
	g := NewGraph[int]()
	require.NoError(t, g.AddRunnable("producer", node))
	require.NoError(t, g.AddEdge(START, "producer"))
	require.NoError(t, g.AddEdge("producer", END))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := r.Stream(ctx, 0)
	require.NoError(t, err)
	<-stream
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("node did not start")
	}
	cancel()
	for chunk := range stream {
		require.False(t, chunk.Done && chunk.Error == nil)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("node producer leaked")
	}
}

func TestParallelEventsAreScopedAndGraphFinishesOnce(t *testing.T) {
	g := NewGraph[int]()
	require.NoError(t, g.AddParallel("fork", ParallelConfig[int]{Branches: []Branch[int]{{Name: "a", Run: increment}, {Name: "b", Run: increment}}, Clone: func(in int) int { return in }, Merge: func(_ context.Context, _ int, out []int) (int, error) { return out[0] + out[1], nil }}))
	require.NoError(t, g.AddEdge(START, "fork"))
	require.NoError(t, g.AddEdge("fork", END))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)
	stream, err := r.Stream(context.Background(), 2)
	require.NoError(t, err)
	starts, ends := map[string]int{}, map[string]int{}
	finals := 0
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		if chunk.Done {
			finals++
			require.Equal(t, 6, chunk.Data)
		}
		if chunk.Event != nil {
			if chunk.Event.Type == "branch_start" {
				starts[chunk.Event.Step]++
			}
			if chunk.Event.Type == "branch_end" {
				ends[chunk.Event.Step]++
			}
		}
	}
	require.Equal(t, map[string]int{"fork/a": 1, "fork/b": 1}, starts)
	require.Equal(t, starts, ends)
	require.Equal(t, 1, finals)
}
