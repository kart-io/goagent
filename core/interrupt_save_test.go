package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/state"
	"github.com/stretchr/testify/require"
)

type failingInterruptSaver struct {
	checkpoint.Checkpointer
	err error
}

func (s *failingInterruptSaver) Save(ctx context.Context, id string, value state.State) error {
	if s.err != nil {
		return s.err
	}
	return s.Checkpointer.Save(ctx, id, value)
}

func TestInterruptSaveFailureDoesNotPublishOrExecute(t *testing.T) {
	sentinel := errors.New("checkpoint unavailable")
	saver := &failingInterruptSaver{Checkpointer: checkpoint.NewInMemorySaver(), err: sentinel}
	manager := NewInterruptManager(saver)
	created := 0
	manager.OnInterruptCreated(func(in *Interrupt) {
		created++
		require.NoError(t, manager.RespondToInterrupt(in.ID, &InterruptResponse{Approved: true}))
	})
	executor := NewInterruptableExecutor(manager, saver)
	executor.AddInterruptRule(InterruptRule{Name: "review", Condition: func(context.Context, state.State) bool { return true },
		CreateInterrupt: func(context.Context, state.State) *Interrupt { return &Interrupt{ID: "same-review"} }})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	effects := 0
	run := func(context.Context, state.State) error { effects++; return nil }
	value := state.NewAgentState()
	err := executor.ExecuteWithInterrupts(ctx, value, run)
	require.ErrorIs(t, err, sentinel)
	require.Zero(t, created)
	require.Zero(t, effects)
	require.Empty(t, manager.ListPendingInterrupts())
	require.Empty(t, manager.entries)
	// A failed write must release the ID, so recovery can create the same gate.
	saver.err = nil
	require.NoError(t, executor.ExecuteWithInterrupts(ctx, value, run))
	require.Equal(t, 1, created)
	require.Equal(t, 1, effects)
}
