package durable

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func operationContext(t *testing.T) context.Context {
	t.Helper()
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)
	return WithJournal(WithExecutionKey(context.Background(), "node-key"), store)
}

func TestOperationsDistinctKeysReplayAndIntent(t *testing.T) {
	ctx := operationContext(t)
	keys := map[string]bool{}
	for _, id := range []string{"write-A", "write-B"} {
		calls := 0
		fn := func(ctx context.Context) (json.RawMessage, error) {
			calls++
			keys[ExecutionKey(ctx)] = true
			return json.RawMessage(`{"receipt":"ok"}`), nil
		}
		first, err := Do(ctx, id, json.RawMessage(`{"amount":10}`), OperationPolicy{}, fn)
		require.NoError(t, err)
		second, err := Do(ctx, id, json.RawMessage(`{"amount":10}`), OperationPolicy{}, fn)
		require.NoError(t, err)
		require.True(t, second.Replayed)
		require.Equal(t, first.Value, second.Value)
		require.Equal(t, 1, calls)
		_, err = Do(ctx, id, json.RawMessage(`{"amount":20}`), OperationPolicy{}, fn)
		require.ErrorContains(t, err, "intent")
	}
	require.Len(t, keys, 2)
}

func TestOperationFailureRetainsUsageAndNeedsAttention(t *testing.T) {
	ctx := operationContext(t)
	calls := 0
	fn := func(context.Context) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"usage":91,"failure":"invalid_json"}`), errors.New("sensitive-provider-error")
	}
	_, err := Do(ctx, "model", json.RawMessage(`{}`), OperationPolicy{}, fn)
	require.ErrorContains(t, err, "sensitive-provider-error")
	record, err := InspectOperation(ctx, "model")
	require.NoError(t, err)
	require.Equal(t, "failed", record.Status)
	require.JSONEq(t, `{"usage":91,"failure":"invalid_json"}`, string(record.Value))
	second, err := Do(ctx, "model", json.RawMessage(`{}`), OperationPolicy{}, fn)
	require.ErrorIs(t, err, ErrOperationAttention)
	require.NotContains(t, err.Error(), "sensitive-provider-error")
	require.True(t, second.Replayed)
	require.Equal(t, 1, calls)
}

func TestOperationExplicitResumeBudgetAndCancellation(t *testing.T) {
	ctx := operationContext(t)
	calls := 0
	fn := func(context.Context) (json.RawMessage, error) { calls++; return nil, errors.New("retryable") }
	for range 3 {
		_, _ = Do(ctx, "tool", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: 2, RetryFailed: true}, fn)
	}
	require.Equal(t, 2, calls)
	cancelCtx, cancel := context.WithCancel(ctx)
	_, err := Do(cancelCtx, "canceled", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) {
		cancel()
		return json.RawMessage(`{"usage":9}`), context.Canceled
	})
	require.ErrorIs(t, err, context.Canceled)
	r, err := InspectOperation(ctx, "canceled")
	require.NoError(t, err)
	require.Equal(t, "canceled", r.FailureKind)
	require.Contains(t, string(r.Value), "9")
}

func TestOperationConcurrentDuplicateOnlyRunsOnce(t *testing.T) {
	ctx := operationContext(t)
	var calls atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Do(ctx, "same", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) { calls.Add(1); return json.RawMessage(`"result"`), nil })
			errs <- err
		}()
	}
	wg.Wait()
	for range 2 {
		require.NoError(t, <-errs)
	}
	require.EqualValues(t, 1, calls.Load())
}

type failingOperationStore struct {
	Store
	saves  int
	failAt int
}

func (s *failingOperationStore) Save(ctx context.Context, id string, value *Snapshot) error {
	s.saves++
	if s.saves == s.failAt {
		return errors.New("injected persistence failure")
	}
	return s.Store.Save(ctx, id, value)
}

func TestOperationSaveFailureDoesNotDuplicatePaidRequest(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		store, err := NewFileStore(t.TempDir())
		require.NoError(t, err)
		flaky := &failingOperationStore{Store: store, failAt: failAt}
		ctx := WithJournal(WithExecutionKey(context.Background(), "parent"), flaky)
		calls := 0
		fn := func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(`{"usage":11}`), nil }
		_, err = Do(ctx, "paid", json.RawMessage(`{}`), OperationPolicy{}, fn)
		require.ErrorContains(t, err, "persistence")
		if failAt == 1 {
			require.Zero(t, calls, "no call without persisted intent")
			_, err = Do(ctx, "paid", json.RawMessage(`{}`), OperationPolicy{}, fn)
			require.NoError(t, err)
		} else {
			_, err = Do(ctx, "paid", json.RawMessage(`{}`), OperationPolicy{}, fn)
			require.ErrorIs(t, err, ErrOperationAttention)
			saved, err := InspectOperation(ctx, "paid")
			require.NoError(t, err)
			require.Equal(t, "pending", saved.Status)
			require.Equal(t, "ambiguous", saved.FailureKind)
		}
		require.Equal(t, 1, calls)
	}
}
