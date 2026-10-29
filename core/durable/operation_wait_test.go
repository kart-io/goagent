package durable

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func waitingRun(calls *int) func(context.Context) (json.RawMessage, error) {
	return func(context.Context) (json.RawMessage, error) {
		*calls++
		return nil, &WaitError{Continuation: json.RawMessage(`{"node":"approve"}`)}
	}
}

func TestOperationWaitIsRecordedAndReplayedWithoutRerun(t *testing.T) {
	ctx := operationContext(t)
	calls := 0
	record, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), OperationPolicy{}, waitingRun(&calls))
	waits := Waits(err)
	require.Len(t, waits, 1)
	require.Equal(t, "waiting", record.Status)
	require.Equal(t, []WaitStep{{ID: "tool/c1", Key: ChildKey("node-key", "tool/c1")}}, waits[0].Path)
	require.JSONEq(t, `{"node":"approve"}`, string(waits[0].Continuation))
	require.NotErrorIs(t, err, ErrOperationAttention)

	again, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), OperationPolicy{}, waitingRun(&calls))
	require.Len(t, Waits(err), 1)
	require.True(t, again.Replayed)
	require.Equal(t, 1, calls, "不可续跑的操作在等待时不重新进入")
}

func TestOperationResumableReentersWaitWithoutConsumingAttempts(t *testing.T) {
	ctx := operationContext(t)
	policy := OperationPolicy{MaxAttempts: 1, Resumable: true}
	calls := 0
	_, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), policy, waitingRun(&calls))
	require.Len(t, Waits(err), 1)
	_, err = Do(ctx, "tool/c1", json.RawMessage(`{}`), policy, waitingRun(&calls))
	require.Len(t, Waits(err), 1)
	record, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), policy, func(context.Context) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`"done"`), nil
	})
	require.NoError(t, err)
	require.Equal(t, "completed", record.Status)
	require.Equal(t, 1, record.Attempts)
	require.Empty(t, record.Continuation)
	require.Equal(t, 3, calls)
}

func TestOperationResumableReentersPendingWithinAttempts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		attempts  int
		reentered bool
	}{{"有剩余尝试", 2, true}, {"尝试已用完", 1, false}} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewFileStore(t.TempDir())
			require.NoError(t, err)
			flaky := &failingOperationStore{Store: store, failAt: 2}
			ctx := WithJournal(WithExecutionKey(context.Background(), "parent"), flaky)
			policy := OperationPolicy{MaxAttempts: tc.attempts, Resumable: true}
			calls := 0
			run := func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(`1`), nil }
			_, err = Do(ctx, "child", json.RawMessage(`{}`), policy, run)
			require.ErrorContains(t, err, "persistence")
			_, err = Do(ctx, "child", json.RawMessage(`{}`), policy, run)
			if tc.reentered {
				require.NoError(t, err)
				require.Equal(t, 2, calls)
				return
			}
			require.ErrorIs(t, err, ErrOperationAttention)
			require.Equal(t, 1, calls)
		})
	}
}

func pendingContext(t *testing.T) context.Context {
	t.Helper()
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := WithJournal(WithExecutionKey(context.Background(), "parent"), &failingOperationStore{Store: store, failAt: 2})
	_, err = Do(ctx, "effect", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: 2, RetryFailed: true}, func(context.Context) (json.RawMessage, error) {
		return json.RawMessage(`"first"`), nil
	})
	require.ErrorContains(t, err, "persistence")
	r, err := InspectOperation(ctx, "effect")
	require.NoError(t, err)
	require.Equal(t, "pending", r.Status)
	return ctx
}

func TestOperationReconcileResolvesAmbiguousEffect(t *testing.T) {
	policy := func(reconcile func(context.Context) (json.RawMessage, bool, error)) OperationPolicy {
		return OperationPolicy{MaxAttempts: 2, RetryFailed: true, Reconcile: reconcile}
	}
	run := func(calls *int) func(context.Context) (json.RawMessage, error) {
		return func(context.Context) (json.RawMessage, error) { *calls++; return json.RawMessage(`"rerun"`), nil }
	}

	t.Run("已生效", func(t *testing.T) {
		ctx := pendingContext(t)
		calls := 0
		var key string
		record, err := Do(ctx, "effect", json.RawMessage(`{}`), policy(func(ctx context.Context) (json.RawMessage, bool, error) {
			key = ExecutionKey(ctx)
			return json.RawMessage(`"found"`), true, nil
		}), run(&calls))
		require.NoError(t, err)
		require.Zero(t, calls)
		require.Equal(t, ChildKey("parent", "effect"), key, "核对使用与执行相同的派生键")
		require.True(t, record.Reconciled)
		require.True(t, record.Replayed)
		require.Equal(t, 1, record.Attempts)
		saved, err := InspectOperation(ctx, "effect")
		require.NoError(t, err)
		require.Equal(t, "completed", saved.Status)
		require.True(t, saved.Reconciled)
		require.JSONEq(t, `"found"`, string(saved.Value))
	})

	t.Run("未生效", func(t *testing.T) {
		ctx := pendingContext(t)
		calls := 0
		record, err := Do(ctx, "effect", json.RawMessage(`{}`), policy(func(context.Context) (json.RawMessage, bool, error) {
			return nil, false, nil
		}), run(&calls))
		require.NoError(t, err)
		require.Equal(t, 1, calls)
		require.False(t, record.Reconciled)
		require.Equal(t, 2, record.Attempts)
	})

	t.Run("核对失败与 panic", func(t *testing.T) {
		for _, reconcile := range []func(context.Context) (json.RawMessage, bool, error){
			func(context.Context) (json.RawMessage, bool, error) { return nil, false, errors.New("lookup down") },
			func(context.Context) (json.RawMessage, bool, error) { panic("boom") },
			func(context.Context) (json.RawMessage, bool, error) { return json.RawMessage(`{bad`), true, nil },
		} {
			ctx := pendingContext(t)
			calls := 0
			_, err := Do(ctx, "effect", json.RawMessage(`{}`), policy(reconcile), run(&calls))
			require.Error(t, err)
			require.Zero(t, calls)
			saved, err := InspectOperation(ctx, "effect")
			require.NoError(t, err)
			require.Equal(t, "pending", saved.Status, "核对出错时记录不变")
		}
	})
}

func TestOperationWaitJoinedWithFailureIsFailure(t *testing.T) {
	ctx := operationContext(t)
	record, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) {
		return nil, errors.Join(&WaitError{Continuation: json.RawMessage(`{}`)}, errors.New("real failure"))
	})
	require.Error(t, err)
	require.Equal(t, "failed", record.Status)
	require.Nil(t, Waits(errors.Join(&WaitError{}, errors.New("x"))))
	require.Len(t, Waits(errors.Join(&WaitError{}, &WaitError{})), 2)
}

func TestOperationNestedWaitsPrefixPath(t *testing.T) {
	ctx := operationContext(t)
	_, err := Do(ctx, "outer", json.RawMessage(`{}`), OperationPolicy{}, func(ctx context.Context) (json.RawMessage, error) {
		_, inner := Do(ctx, "inner", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) {
			return nil, &WaitError{Continuation: json.RawMessage(`"gate"`)}
		})
		return nil, inner
	})
	waits := Waits(err)
	require.Len(t, waits, 1)
	require.Len(t, waits[0].Path, 2)
	require.Equal(t, "outer", waits[0].Path[0].ID)
	require.Equal(t, "inner", waits[0].Path[1].ID)
	require.Equal(t, ChildKey(ChildKey("node-key", "outer"), "inner"), waits[0].Path[1].Key)
}

func TestOperationReadsV1Records(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := WithJournal(WithExecutionKey(context.Background(), "parent"), store)
	key := ChildKey("parent", "legacy")
	write := func(status string, extra map[string]any) {
		r := map[string]any{"id": "legacy", "key": key, "input_hash": digest(`{}`), "status": status, "attempts": 1, "max_attempts": 1, "value": `"v1"`}
		for k, v := range extra {
			r[k] = v
		}
		data, err := json.Marshal(r)
		require.NoError(t, err)
		require.NoError(t, store.Save(ctx, operationStorageID(key), &Snapshot{RunID: operationStorageID(key), Version: operationVersionV1, Data: data}))
	}
	write("completed", nil)
	_, err = Do(ctx, "legacy", json.RawMessage(`{}`), OperationPolicy{}, nil)
	require.ErrorContains(t, err, "requires ID")
	record, err := Do(ctx, "legacy", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) { panic("不应执行") })
	require.NoError(t, err)
	require.True(t, record.Replayed)

	write("waiting", map[string]any{"continuation": []any{map[string]any{}}})
	_, err = InspectOperation(ctx, "legacy")
	require.ErrorContains(t, err, "invalid", "v1 记录不能被解释成等待")
}
