package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/require"
)

type adaptedState struct {
	Question string
	Answer   int
}

func TestAdaptMapsDifferentRunnableTypesAndPreservesEvents(t *testing.T) {
	source := core.NewStreamRunnable(core.StreamModeTokens, func(ctx context.Context, in string, emit func(core.StreamChunk[int]) error) error {
		if err := emit(core.StreamChunk[int]{Event: &core.StreamEvent{Type: "text", Delta: "answer"}}); err != nil {
			return err
		}
		return emit(core.StreamChunk[int]{Done: true, Data: len(in)})
	})
	adapted, err := Adapt(source, func(state adaptedState) (string, error) { return state.Question, nil }, func(state adaptedState, answer int) (adaptedState, error) { state.Answer = answer; return state, nil })
	require.NoError(t, err)
	g := NewGraph[adaptedState]()
	require.NoError(t, g.AddRunnable("agent", adapted))
	require.NoError(t, g.AddEdge(START, "agent"))
	require.NoError(t, g.AddEdge("agent", END))
	compiled, err := g.Compile(context.Background())
	require.NoError(t, err)
	stream, err := compiled.Stream(context.Background(), adaptedState{Question: "hello"})
	require.NoError(t, err)
	text := ""
	var final adaptedState
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		if chunk.Event != nil {
			text += chunk.Event.Delta
		}
		if chunk.Done {
			final = chunk.Data
		}
	}
	require.Equal(t, "answer", text)
	require.Equal(t, adaptedState{Question: "hello", Answer: 5}, final)
}

func TestAdaptPropagatesMappingErrorsAndValidatesBeforeMappingOutput(t *testing.T) {
	failure := errors.New("mapping failed")
	for _, scenario := range []string{"input", "output", "invalid stream"} {
		called := false
		var source core.Runnable[int, int] = core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, in int, emit func(core.StreamChunk[int]) error) error {
			return emit(core.StreamChunk[int]{Done: true, Data: in})
		})
		if scenario == "invalid stream" {
			source = &rawRunnable{chunks: []core.StreamChunk[int]{{Done: true, Data: 1}, {Error: failure}}}
		}
		adapted, err := Adapt(source, func(state int) (int, error) {
			if scenario == "input" {
				return 0, failure
			}
			return state, nil
		}, func(state, answer int) (int, error) { called = true; return 0, failure })
		require.NoError(t, err)
		_, err = adapted.Invoke(context.Background(), 1)
		require.ErrorIs(t, err, failure)
		require.Equal(t, scenario == "output", called)
	}
}

func TestAdaptRejectsMissingComponents(t *testing.T) {
	input := func(in int) (int, error) { return in, nil }
	output := func(_, out int) (int, error) { return out, nil }
	_, err := Adapt((*core.StreamRunnable[int, int])(nil), input, output)
	require.Error(t, err)
	node := compileLinear(t)
	_, err = Adapt(node, (func(int) (int, error))(nil), output)
	require.Error(t, err)
	_, err = Adapt(node, input, (func(int, int) (int, error))(nil))
	require.Error(t, err)
}

func TestAdaptStreamTypedPreviewsDoNotAdvanceState(t *testing.T) {
	source := core.NewStreamRunnable(core.StreamModeTokens, func(ctx context.Context, in string, emit func(core.StreamChunk[int]) error) error {
		if err := emit(core.StreamChunk[int]{Data: 2, Event: &core.StreamEvent{Type: "preview"}}); err != nil {
			return err
		}
		return emit(core.StreamChunk[int]{Done: true, Data: 5})
	})
	adapted, err := AdaptStream(source, func(s adaptedState) (string, error) { return s.Question, nil }, func(s adaptedState, n int) (adaptedState, error) { s.Answer = n; return s, nil }, func(s adaptedState, c core.StreamChunk[int]) (core.StreamChunk[adaptedState], error) {
		s.Answer = c.Data
		return core.StreamChunk[adaptedState]{Data: s, Event: c.Event}, nil
	})
	require.NoError(t, err)
	require.Equal(t, core.StreamModeTokens, adapted.StreamMode())
	graph := NewGraph[adaptedState]()
	require.NoError(t, graph.AddRunnable("model", adapted))
	require.NoError(t, graph.AddNode("consumer", func(_ context.Context, s adaptedState) (adaptedState, error) {
		require.Equal(t, 5, s.Answer)
		s.Answer++
		return s, nil
	}))
	require.NoError(t, graph.AddEdge(START, "model"))
	require.NoError(t, graph.AddEdge("model", "consumer"))
	require.NoError(t, graph.AddEdge("consumer", END))
	run, err := graph.Compile(context.Background())
	require.NoError(t, err)
	stream, err := run.Stream(context.Background(), adaptedState{Question: "test"})
	require.NoError(t, err)
	preview, final := 0, 0
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		if chunk.Event != nil && chunk.Event.Type == "preview" {
			preview = chunk.Data.Answer
		}
		if chunk.Done {
			final = chunk.Data.Answer
		}
	}
	require.Equal(t, 2, preview)
	require.Equal(t, 6, final)
}

func TestAdaptStreamMappingFailureCancelsUpstream(t *testing.T) {
	stopped := make(chan struct{})
	source := core.NewStreamRunnable(core.StreamModeTokens, func(ctx context.Context, in int, emit func(core.StreamChunk[int]) error) error {
		defer close(stopped)
		for {
			if err := emit(core.StreamChunk[int]{Data: in}); err != nil {
				return err
			}
		}
	})
	failure := errors.New("preview mapping failure")
	adapted, err := AdaptStream(source, func(n int) (int, error) { return n, nil }, func(_, n int) (int, error) { t.Error("finalized failed stream"); return n, nil }, func(_ int, c core.StreamChunk[int]) (core.StreamChunk[int], error) { return c, failure })
	require.NoError(t, err)
	_, err = adapted.Invoke(context.Background(), 1)
	require.ErrorIs(t, err, failure)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("upstream was not canceled")
	}
}
