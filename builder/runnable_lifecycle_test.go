package builder

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	"github.com/stretchr/testify/require"
)

// Gating the real journal write distinguishes cancellation notification from
// completion of the producer which still owns the store and execution gate.
type runnableCompletionGate struct {
	durable.Store
	entered chan struct{}
	release chan struct{}
}

func (s *runnableCompletionGate) Save(ctx context.Context, id string, snap *durable.Snapshot) error {
	var record durable.OperationRecord
	if json.Unmarshal(snap.Data, &record) == nil && record.FailureKind == "canceled" {
		close(s.entered)
		<-s.release
	}
	return s.Store.Save(ctx, id, snap)
}

func TestBuilderRunnableCancellationJoinsJournalCompletion(t *testing.T) {
	for _, entry := range []string{"invoke", "stream"} {
		t.Run(entry, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inner, err := durable.NewFileStore(t.TempDir())
				require.NoError(t, err)
				store := &runnableCompletionGate{Store: inner, entered: make(chan struct{}), release: make(chan struct{})}
				base := durable.WithJournal(durable.WithExecutionKey(t.Context(), "root"), store)
				model := &eventModel{MockLLMClient: NewMockLLMClient(), stall: true, started: make(chan struct{})}
				agent, err := NewSimpleBuilder(model).WithDurableScope("member").Build()
				require.NoError(t, err)
				ctx, cancel := context.WithCancel(base)
				defer cancel()
				returned := make(chan error, 1)
				go func() {
					if entry == "invoke" {
						_, err := agent.AsRunnable().Invoke(ctx, "question")
						returned <- err
						return
					}
					stream, err := agent.AsRunnable().Stream(ctx, "question")
					if err != nil {
						returned <- err
						return
					}
					for range stream {
					}
					returned <- ctx.Err() // cancellation errors on public streams are best effort
				}()
				<-model.started
				cancel()
				<-store.entered
				// All goroutines are now durably blocked or finished. No timing
				// window or sleep is needed to detect premature completion.
				synctest.Wait()
				early := false
				var result error
				select {
				case result = <-returned:
					early = true
				default:
				}
				close(store.release)
				if !early {
					result = <-returned
				}
				synctest.Wait() // all producer writes finish before checking the store
				require.False(t, early, "Runnable finished while cancellation evidence was still being saved")
				require.ErrorIs(t, result, context.Canceled)
				require.Empty(t, agent.executionGate, "Runnable completion releases session ownership")
				opCtx := durable.WithExecutionKey(base, durable.ChildKey("root", "agent/member"))
				record, err := durable.InspectOperation(opCtx, "model/answer/0")
				require.NoError(t, err)
				require.Equal(t, "failed", record.Status)
				require.Equal(t, "canceled", record.FailureKind)
			})
		})
	}
}
