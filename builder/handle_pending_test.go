package builder

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

// A failed turn must not discard input which was accepted for a later turn.
func TestHandleFailedTurnRetainsFollowup(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	boom := errors.New("provider unavailable")
	model := newScriptModel(answer("draft"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithMaxIterations(3).
		WithTurnStopping(func(context.Context, *Stopping) (string, error) {
			close(started)
			<-release
			return "", boom
		}).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "first")
	<-started
	require.NoError(t, h.Followup("second"))
	close(release)
	_, err = h.Wait(context.Background())
	require.ErrorIs(t, err, boom)
	require.Equal(t, []PendingInput{{Kind: "followup", Text: "second"}}, h.PendingInputs())
	require.Equal(t, 1, model.count())
}

func TestHandlePendingInputsAtStoppingBoundary(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	model := newScriptModel(answer("finished"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithMaxIterations(3).
		WithTurnStopping(func(context.Context, *Stopping) (string, error) {
			close(started) // The loop has already checked whether Steer should wake it.
			<-release
			return "", nil
		}).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "first")
	<-started
	require.NoError(t, h.Steer("late correction"))
	require.NoError(t, h.Inject("reference"))
	require.NoError(t, h.Redirect("late redirect")) // No model request: becomes Steer.
	close(release)
	require.Equal(t, "finished", waitAnswer(t, h).Result)
	require.Equal(t, []PendingInput{
		{Kind: "steer", Text: "late correction"},
		{Kind: "inject", Text: "reference"},
		{Kind: "steer", Text: "late redirect"},
	}, h.PendingInputs())
	require.Equal(t, 1, model.count(), "snapshot does not create another turn or bypass limits")
	require.ErrorIs(t, h.Followup("too late"), agentErrors.ErrAgentHandleDone)
}

func TestHandlePendingInputsAtIterationLimit(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	model := newScriptModel(gated(started, release, answer("finished")))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).WithMaxIterations(1).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "first")
	<-started
	require.NoError(t, h.Steer("correction"))
	close(release)
	require.Equal(t, "finished", waitAnswer(t, h).Result)
	require.Equal(t, []PendingInput{{Kind: "steer", Text: "correction"}}, h.PendingInputs())
	require.Equal(t, 1, model.count())
}

func TestHandlePendingInputsAfterInterrupt(t *testing.T) {
	started := make(chan struct{})
	model := newScriptModel(gated(started, make(chan struct{}), answer("unused")))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "first")
	<-started
	require.NoError(t, h.Inject("reference"))
	require.NoError(t, h.Followup("second"))
	// Timing out one waiter must not stop the running Handle.
	waitCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = h.Wait(waitCtx)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, h.Followup("third"))
	boom := errors.New("user canceled")
	h.Interrupt(boom)
	_, err = h.Wait(context.Background())
	require.ErrorIs(t, err, boom)
	require.Equal(t, []PendingInput{
		{Kind: "inject", Text: "reference"},
		{Kind: "followup", Text: "second"},
		{Kind: "followup", Text: "third"},
	}, h.PendingInputs())
}

func TestHandlePendingInputsSnapshotAndConsumption(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	model := newScriptModel(gated(started, release, answer("one")), answer("two"))
	agent, err := NewSimpleBuilder(model).WithTools(plainTool()).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "first")
	<-started
	require.NoError(t, h.Followup("second"))
	snapshot := h.PendingInputs()
	require.Equal(t, []PendingInput{{Kind: "followup", Text: "second"}}, snapshot)
	snapshot[0].Text = "changed by caller"
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for range 100 {
				_ = h.PendingInputs()
			}
		})
	}
	close(release)
	require.Equal(t, "two", waitAnswer(t, h).Result)
	readers.Wait()
	require.Empty(t, h.PendingInputs())
	require.Equal(t, "second", model.request(1).Messages[len(model.request(1).Messages)-1].Content)
	require.Equal(t, 2, model.count())
}
