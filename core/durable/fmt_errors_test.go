package durable

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
)

func TestFormattedOperationErrorClassification(t *testing.T) {
	t.Run("无效输入不执行回调", func(t *testing.T) {
		calls := 0
		run := func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(`null`), nil }
		_, err := Do(context.Background(), "", json.RawMessage(`{}`), OperationPolicy{}, run)
		require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
		require.Zero(t, calls)
		_, err = Do(context.Background(), "op", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: -1}, run)
		require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
		require.Zero(t, calls)
	})
	t.Run("损坏记录不能续跑", func(t *testing.T) {
		ctx := operationContext(t)
		key := ChildKey(ExecutionKey(ctx), "op")
		id := OperationStorageID(key)
		require.NoError(t, JournalStore(ctx).Save(ctx, id, &Snapshot{RunID: id, Version: operationVersion, Data: json.RawMessage(`{}`)}))
		calls := 0
		_, err := Do(ctx, "op", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(`null`), nil })
		require.Equal(t, agentErrors.CodeStateValidation, agentErrors.GetCode(err))
		require.ErrorContains(t, err, "invalid durable operation record")
		require.Zero(t, calls)
	})
	t.Run("输出无效记为失败", func(t *testing.T) {
		ctx := operationContext(t)
		record, err := Do(ctx, "op", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) { return json.RawMessage(`broken`), nil })
		require.Equal(t, agentErrors.CodeInvalidOutput, agentErrors.GetCode(err))
		require.Equal(t, "failed", record.Status)
		require.Nil(t, Waits(err))
		saved, err := InspectOperation(ctx, "op")
		require.NoError(t, err)
		require.Equal(t, "execution", saved.FailureKind)
	})
}
