package workflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/flow"
	"github.com/kart-io/goagent/workflow"
)

func TestFlowLateFailureDoesNotScheduleDownstream(t *testing.T) {
	late := errors.New("late failure")
	run, err := flow.AsRunnable(func(context.Context, int) (flow.Source[int], error) {
		return func(_ context.Context, y func(int) error) error { _ = y(0); return late }, nil
	}, flow.Fold[int, int]{Init: func() int { return 0 }, Add: func(_ context.Context, a, b int) (int, error) { return a + b, nil }}, nil, flow.Limits[int]{})
	require.NoError(t, err)
	g := workflow.NewGraph[int]()
	require.NoError(t, g.AddRunnable("flow", run))
	calls := 0
	require.NoError(t, g.AddNode("after", func(_ context.Context, n int) (int, error) { calls++; return n, nil }))
	require.NoError(t, g.AddEdge(constants.WorkflowStart, "flow"))
	require.NoError(t, g.AddEdge("flow", "after"))
	require.NoError(t, g.AddEdge("after", constants.WorkflowEnd))
	compiled, err := g.Compile(context.Background())
	require.NoError(t, err)
	_, err = compiled.Invoke(context.Background(), 0)
	require.ErrorIs(t, err, late)
	require.Zero(t, calls)
}
