package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

var amountInfo = json.RawMessage(`{"question":"金额"}`)

func positive(raw json.RawMessage) error {
	var n int
	if json.Unmarshal(raw, &n) != nil || n <= 0 {
		return errors.New("需要正整数")
	}
	return nil
}

func interruptContext(t *testing.T) context.Context {
	t.Helper()
	return AllowInterrupt(operationContext(t))
}

func interruptKey(id string) string { return ChildKey("node-key", "interrupt/"+id) }

func requireInterruptWait(t *testing.T, err error) InterruptContinuation {
	t.Helper()
	waits := Waits(err)
	require.Len(t, waits, 1, "%v", err)
	require.Equal(t, interruptKey("amount"), waits[0].Path[len(waits[0].Path)-1].Key)
	var c InterruptContinuation
	require.NoError(t, json.Unmarshal(waits[0].Continuation, &c))
	require.Equal(t, ContinuationKindInterrupt, c.Kind)
	return c
}

func TestAwaitInputWaitsThenFixesFirstAnswer(t *testing.T) {
	ctx := interruptContext(t)
	_, err := AwaitInput(ctx, "amount", amountInfo, positive)
	c := requireInterruptWait(t, err)
	require.Equal(t, "amount", c.ID)
	require.JSONEq(t, string(amountInfo), string(c.Info))
	require.False(t, c.Rejected)

	got, err := AwaitInput(WithResume(ctx, interruptKey("amount"), `5`), "amount", amountInfo, positive)
	require.NoError(t, err)
	require.JSONEq(t, `5`, string(got))

	// 已固定的输入只回放：不带输入、或带不同输入都返回第一次的值。
	got, err = AwaitInput(ctx, "amount", amountInfo, positive)
	require.NoError(t, err)
	require.JSONEq(t, `5`, string(got))
	got, err = AwaitInput(WithResume(ctx, interruptKey("amount"), `9`), "amount", amountInfo, positive)
	require.NoError(t, err)
	require.JSONEq(t, `5`, string(got))

	answer, ok, err := InterruptAnswer(ctx, JournalStore(ctx), interruptKey("amount"), "amount")
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `5`, string(answer))
}

func TestAwaitInputRejectionsStayWaiting(t *testing.T) {
	for _, tc := range []struct {
		name, input, reason string
		validate            func(json.RawMessage) error
	}{
		{name: "空字符串", input: "", reason: ReasonInvalidJSON, validate: positive},
		{name: "非法 JSON", input: "{", reason: ReasonInvalidJSON, validate: positive},
		{name: "校验失败", input: "-1", reason: "需要正整数", validate: positive},
		{name: "null 被校验拒绝", input: "null", reason: "需要正整数", validate: positive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := interruptContext(t)
			_, err := AwaitInput(WithResume(ctx, interruptKey("amount"), tc.input), "amount", amountInfo, tc.validate)
			c := requireInterruptWait(t, err)
			require.True(t, c.Rejected)
			require.Equal(t, tc.reason, c.Reason)
			_, ok, err := InterruptAnswer(ctx, JournalStore(ctx), interruptKey("amount"), "amount")
			require.NoError(t, err)
			require.False(t, ok)
			got, err := AwaitInput(WithResume(ctx, interruptKey("amount"), `2`), "amount", amountInfo, tc.validate)
			require.NoError(t, err)
			require.JSONEq(t, `2`, string(got))
		})
	}
}

func TestAwaitInputAcceptsNullWithoutValidator(t *testing.T) {
	ctx := interruptContext(t)
	got, err := AwaitInput(WithResume(ctx, interruptKey("amount"), `null`), "amount", amountInfo, nil)
	require.NoError(t, err)
	require.Equal(t, "null", string(got))
}

func TestAwaitInputRequiresJournalAndWaitableContext(t *testing.T) {
	_, err := AwaitInput(AllowInterrupt(context.Background()), "amount", amountInfo, nil)
	require.ErrorIs(t, err, ErrInterruptNotDurable)

	ctx := operationContext(t)
	_, err = AwaitInput(ctx, "amount", amountInfo, nil)
	require.ErrorIs(t, err, ErrWaitNotResumable)
	_, err = InspectOperation(ctx, "interrupt/amount")
	require.ErrorIs(t, err, os.ErrNotExist, "不可等待时不能写记录")

	// Do 为子操作清除 AllowInterrupt 标记：不可续跑的操作里不能暂停。
	host := AllowInterrupt(ctx)
	_, err = Do(host, "plain", json.RawMessage(`{}`), OperationPolicy{}, func(ctx context.Context) (json.RawMessage, error) {
		_, e := AwaitInput(ctx, "amount", amountInfo, nil)
		return nil, e
	})
	require.ErrorIs(t, err, ErrWaitNotResumable)
	_, err = InspectOperation(WithExecutionKey(host, ChildKey("node-key", "plain")), "interrupt/amount")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAwaitInputInsideResumableOperation(t *testing.T) {
	ctx := operationContext(t)
	run := func(ctx context.Context) (json.RawMessage, error) {
		return AwaitInput(ctx, "amount", amountInfo, positive)
	}
	policy := OperationPolicy{Resumable: true, MaxAttempts: 1}
	_, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), policy, run)
	waits := Waits(err)
	require.Len(t, waits, 1)
	key := waits[0].Path[len(waits[0].Path)-1].Key
	require.Equal(t, ChildKey(ChildKey("node-key", "tool/c1"), "interrupt/amount"), key)
	record, err := Do(WithResume(ctx, key, `3`), "tool/c1", json.RawMessage(`{}`), policy, run)
	require.NoError(t, err)
	require.JSONEq(t, `3`, string(record.Value))
}

func TestAwaitInputReentersAfterCrashBeforeWaitingSaved(t *testing.T) {
	ctx := interruptContext(t)
	key := interruptKey("amount")
	pending := &OperationRecord{ID: "interrupt/amount", Key: key, InputHash: digest(string(amountInfo)), Status: "pending",
		Attempts: 1, MaxAttempts: interruptAttempts, Value: json.RawMessage(`null`), FailureKind: "ambiguous"}
	require.NoError(t, saveOperation(ctx, JournalStore(ctx), operationStorageID(key), pending))
	got, err := AwaitInput(WithResume(ctx, key, `7`), "amount", amountInfo, positive)
	require.NoError(t, err)
	require.JSONEq(t, `7`, string(got))
}

func TestAwaitInputInfoChangeIsIntentChange(t *testing.T) {
	ctx := interruptContext(t)
	_, err := AwaitInput(ctx, "amount", amountInfo, nil)
	requireInterruptWait(t, err)
	_, err = AwaitInput(ctx, "amount", json.RawMessage(`{"question":"其他"}`), nil)
	require.ErrorContains(t, err, "intent")
}

// 只改 Resumable 不会被 Do 检测：记录只比较输入哈希与 MaxAttempts。迁移须提升 intent 版本。
func TestDoDoesNotDetectResumableChange(t *testing.T) {
	ctx := operationContext(t)
	calls := 0
	run := func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(`1`), nil }
	_, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: 1}, run)
	require.NoError(t, err)
	record, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: 1, Resumable: true}, run)
	require.NoError(t, err)
	require.True(t, record.Replayed)
	require.Equal(t, 1, calls)
}
