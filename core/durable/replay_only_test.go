package durable

import (
	"context"
	"errors"
	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReplayOnlyRequiresCompletedRecord(t *testing.T) {
	for _, status := range []string{"missing", "pending", "failed", "completed"} {
		t.Run(status, func(t *testing.T) {
			ctx := operationContext(t)
			policy := OperationPolicy{MaxAttempts: 2, RetryFailed: true, RetryInterrupted: true}
			calls := 0
			fn := func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(`"ok"`), nil }
			if status != "missing" {
				_, err := Do(ctx, "effect", json.RawMessage(`{}`), policy, fn)
				require.NoError(t, err)
				if status != "completed" {
					key := ChildKey(ExecutionKey(ctx), "effect")
					snap, err := JournalStore(ctx).Load(ctx, operationStorageID(key))
					require.NoError(t, err)
					var record OperationRecord
					require.NoError(t, json.Unmarshal(snap.Data, &record))
					record.Status = status
					record.Value = json.RawMessage(`null`)
					snap.Data, err = json.Marshal(record)
					require.NoError(t, err)
					require.NoError(t, JournalStore(ctx).Save(ctx, operationStorageID(key), snap))
				}
			}
			before := calls
			reconciled := false
			policy.Reconcile = func(context.Context) (json.RawMessage, bool, error) {
				reconciled = true
				return json.RawMessage(`"ok"`), true, nil
			}
			result, err := Do(WithReplayOnly(ctx), "effect", json.RawMessage(`{}`), policy, fn)
			if status == "completed" {
				require.NoError(t, err)
				require.True(t, result.Replayed)
			} else {
				require.ErrorIs(t, err, agentErrors.ErrReplayOnly)
				require.Nil(t, result)
			}
			require.Equal(t, before, calls)
			require.False(t, reconciled)
		})
	}
	_, err := Do(WithReplayOnly(context.Background()), "effect", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) { t.Fatal("无日志不得执行"); return nil, nil })
	require.ErrorIs(t, err, agentErrors.ErrReplayOnly)
	require.False(t, errors.Is(err, context.Canceled))
}
