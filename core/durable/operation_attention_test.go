package durable

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
)

// 执行函数声明结果未知时，记录按中断处理并把未决操作交给宿主，ParentKey 指回所在执行键。
func TestEffectUnknownIsInterrupted(t *testing.T) {
	ctx := operationContext(t)
	calls := 0
	_, err := Do(ctx, "write", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: 2, RetryInterrupted: true}, func(context.Context) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("连接在响应前断开：%w", ErrEffectUnknown)
		}
		return json.RawMessage(`"ok"`), nil
	})
	var opErr *OperationError
	require.ErrorAs(t, err, &opErr)
	require.ErrorIs(t, err, agentErrors.ErrOperationAttention)
	require.Equal(t, OperationError{ID: "write", Kind: "effect_unknown", ParentKey: "node-key"}, *opErr)
	record, err := InspectOperation(ctx, "write")
	require.NoError(t, err)
	require.Equal(t, "failed", record.Status)
	require.Equal(t, "effect_unknown", record.FailureKind)
	require.Equal(t, "node-key", record.ParentKey)

	// 结果未知与崩溃一样属于中断：声明 RetryInterrupted 时在尝试次数内重跑。
	record, err = Do(ctx, "write", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: 2, RetryInterrupted: true}, func(context.Context) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`"ok"`), nil
	})
	require.NoError(t, err)
	require.Equal(t, "completed", record.Status)
	require.Equal(t, 2, calls)
}

func TestEffectUnknownConsultsReconcile(t *testing.T) {
	ctx := operationContext(t)
	_, err := Do(ctx, "write", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) {
		return nil, ErrEffectUnknown
	})
	require.ErrorIs(t, err, agentErrors.ErrOperationAttention)
	reconciled := 0
	record, err := Do(ctx, "write", json.RawMessage(`{}`), OperationPolicy{Reconcile: func(context.Context) (json.RawMessage, bool, error) {
		reconciled++
		return json.RawMessage(`"receipt"`), true, nil
	}}, func(context.Context) (json.RawMessage, error) {
		t.Fatal("核对确认已生效后不能重跑")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, reconciled)
	require.True(t, record.Reconciled)
	require.JSONEq(t, `"receipt"`, string(record.Value))
}

// 内层操作需要宿主处理时，外层记录保持 pending 且不消耗尝试次数，恢复时重新进入内层记录；
// 与外层是否声明 Resumable 无关，否则宿主对内层的核对结论送不到。
func TestNestedAttentionKeepsOuterPending(t *testing.T) {
	for _, resumable := range []bool{true, false} {
		t.Run(fmt.Sprintf("resumable=%v", resumable), func(t *testing.T) { nestedAttentionKeepsOuterPending(t, resumable) })
	}
}

func nestedAttentionKeepsOuterPending(t *testing.T, resumable bool) {
	ctx := operationContext(t)
	outer := OperationPolicy{MaxAttempts: 1, Resumable: resumable}
	innerRuns := 0
	run := func(ctx context.Context) (json.RawMessage, error) {
		_, err := Do(ctx, "inner", json.RawMessage(`{}`), OperationPolicy{}, func(context.Context) (json.RawMessage, error) {
			innerRuns++
			return nil, ErrEffectUnknown
		})
		if err != nil {
			return nil, fmt.Errorf("子流程：%w", err)
		}
		return json.RawMessage(`"done"`), nil
	}
	_, err := Do(ctx, "outer", json.RawMessage(`{}`), outer, run)
	var opErr *OperationError
	require.ErrorAs(t, err, &opErr)
	require.Equal(t, "inner", opErr.ID)
	require.Equal(t, ChildKey("node-key", "outer"), opErr.ParentKey)
	record, err := InspectOperation(ctx, "outer")
	require.NoError(t, err)
	require.Equal(t, "pending", record.Status)
	require.Equal(t, 1, record.Attempts)

	// 再次进入外层：MaxAttempts 为 1 也能重新进入，内层记录仍未决，外层尝试次数不变。
	_, err = Do(ctx, "outer", json.RawMessage(`{}`), outer, run)
	require.ErrorAs(t, err, &opErr)
	require.Equal(t, "inner", opErr.ID)
	require.Equal(t, 1, innerRuns)
	record, err = InspectOperation(ctx, "outer")
	require.NoError(t, err)
	require.Equal(t, "pending", record.Status)
	require.Equal(t, 1, record.Attempts)

	// 内层被核对为已生效后，外层照常完成。
	inner := WithExecutionKey(ctx, ChildKey("node-key", "outer"))
	_, err = Do(inner, "inner", json.RawMessage(`{}`), OperationPolicy{Reconcile: func(context.Context) (json.RawMessage, bool, error) {
		return json.RawMessage(`"receipt"`), true, nil
	}}, func(context.Context) (json.RawMessage, error) { return nil, errors.New("不应重跑") })
	require.NoError(t, err)
	record, err = Do(ctx, "outer", json.RawMessage(`{}`), outer, run)
	require.NoError(t, err)
	require.Equal(t, "completed", record.Status)
	require.Equal(t, 1, record.Attempts)
}
