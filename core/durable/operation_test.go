package durable

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/codec/json"

	agentErrors "github.com/kart-io/goagent/errors"
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
	require.ErrorIs(t, err, agentErrors.ErrOperationAttention)
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
			_, err := Do(ctx, "same", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) {
				calls.Add(1)
				return json.RawMessage(`"result"`), nil
			})
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
			require.ErrorIs(t, err, agentErrors.ErrOperationAttention)
			saved, err := InspectOperation(ctx, "paid")
			require.NoError(t, err)
			require.Equal(t, "pending", saved.Status)
			require.Equal(t, "ambiguous", saved.FailureKind)
		}
		require.Equal(t, 1, calls)
	}
}

func TestRetryInterruptedOnlyRerunsInterruptedRecords(t *testing.T) {
	policy := OperationPolicy{MaxAttempts: 2, RetryInterrupted: true}
	for _, test := range []struct {
		name         string
		status, kind string
		attempts     int
		rerun        bool
	}{
		{"崩溃后停在 pending", "pending", "ambiguous", 1, true},
		{"进程停止时被取消", "failed", "canceled", 1, true},
		{"普通失败", "failed", "execution", 1, false},
		{"超时", "failed", "deadline", 1, false},
		{"尝试次数已用完", "pending", "ambiguous", 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := operationContext(t)
			input := json.RawMessage(`{}`)
			key := ChildKey(ExecutionKey(ctx), "effect")
			store := ctx.Value(journalContext{}).(Store)
			require.NoError(t, saveOperation(ctx, store, OperationStorageID(key), &OperationRecord{
				ID: "effect", Key: key, InputHash: digest(string(input)), Status: test.status, FailureKind: test.kind,
				Attempts: test.attempts, MaxAttempts: policy.MaxAttempts, Value: json.RawMessage(`null`),
			}))
			attempt := 0
			_, err := Do(ctx, "effect", input, policy, func(ctx context.Context) (json.RawMessage, error) {
				attempt = OperationAttempt(ctx)
				return json.RawMessage(`1`), nil
			})
			if test.rerun {
				require.NoError(t, err)
				require.Equal(t, test.attempts+1, attempt, "run 能看到这是第几次尝试")
				return
			}
			require.ErrorIs(t, err, agentErrors.ErrOperationAttention)
			require.Zero(t, attempt)
		})
	}
}

// 没有声明时行为不变：中断的操作不重跑。
func TestInterruptedOperationStaysStoppedByDefault(t *testing.T) {
	ctx := operationContext(t)
	stopping, stop := context.WithCancel(ctx)
	_, err := Do(stopping, "effect", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: 2}, func(context.Context) (json.RawMessage, error) {
		stop()
		return nil, context.Canceled
	})
	require.ErrorIs(t, err, context.Canceled)
	_, err = Do(ctx, "effect", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: 2}, func(context.Context) (json.RawMessage, error) {
		t.Fatal("不应重跑")
		return nil, nil
	})
	require.ErrorIs(t, err, agentErrors.ErrOperationAttention)
}

// 取消后供应方可能返回不带 context.Canceled 的错误（如连接被关闭），仍然算中断。
func TestCancelledOperationIsRecordedAsCanceledWhateverItReturned(t *testing.T) {
	ctx := operationContext(t)
	stopping, stop := context.WithCancel(ctx)
	record, err := Do(stopping, "effect", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) {
		stop()
		return nil, errors.New("connection closed")
	})
	require.Error(t, err)
	require.Equal(t, "canceled", record.FailureKind)
}
