package workflow

import (
	"context"
	"errors"
	"testing"

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
