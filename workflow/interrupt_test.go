package workflow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

var errDisk = errors.New("disk full")

// filterFailStore 让 fail 返回真的图检查点写入失败，操作记录照常保存。
type filterFailStore struct {
	durable.Store
	fail func(*durable.Snapshot) bool
}

func (s *filterFailStore) Save(ctx context.Context, id string, v *durable.Snapshot) error {
	if s.fail != nil && !strings.HasPrefix(id, "goagent/operation/") && s.fail(v) {
		return errDisk
	}
	return s.Store.Save(ctx, id, v)
}

func positiveInt(raw json.RawMessage) error {
	var n int
	if json.Unmarshal(raw, &n) != nil || n <= 0 {
		return errors.New("需要正整数")
	}
	return nil
}

// askNode 先执行一个记日志的准备步骤，再向人要一个正整数并加到状态上。
func askNode(prepared *atomic.Int32) workflow.Node[int] {
	return func(ctx context.Context, n int) (int, error) {
		if _, err := durable.Do(ctx, "prepare", json.RawMessage(`{}`), durable.OperationPolicy{}, func(context.Context) (json.RawMessage, error) {
			prepared.Add(1)
			return json.RawMessage(`true`), nil
		}); err != nil {
			return 0, err
		}
		raw, err := durable.AwaitInput(ctx, "amount", json.RawMessage(`{"question":"金额"}`), positiveInt)
		if err != nil {
			return 0, err
		}
		var amount int
		if err := json.Unmarshal(raw, &amount); err != nil {
			return 0, err
		}
		return n + amount, nil
	}
}

func compileAsk(t *testing.T, store durable.Store, prepared *atomic.Int32, opts ...workflow.DurableOption[int]) *workflow.DurableGraph[int] {
	t.Helper()
	cfg := workflow.DurableConfig{Store: store, Version: "ask-v1"}
	run, err := oneNode(t, askNode(prepared)).CompileDurable(context.Background(), cfg, opts...)
	require.NoError(t, err)
	return run
}

func requireInterrupted(t *testing.T, snap *workflow.GraphSnapshot[int], err error) workflow.InterruptPoint {
	t.Helper()
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.True(t, snap.Waiting)
	require.Len(t, snap.Interrupts, 1)
	require.Equal(t, "amount", snap.Interrupts[0].ID)
	require.JSONEq(t, `{"question":"金额"}`, string(snap.Interrupts[0].Info))
	return snap.Interrupts[0]
}

func TestNodeInterruptPausesAndResumesAfterRestart(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var prepared atomic.Int32
	snap, err := compileAsk(t, store, &prepared).Run(ctx, "ask", 10, "")
	point := requireInterrupted(t, snap, err)
	require.EqualValues(t, 1, prepared.Load())

	// 不带输入的 Run 不执行节点，原样返回等待。
	snap, err = compileAsk(t, store, &prepared).Run(ctx, "ask", 0, "")
	requireInterrupted(t, snap, err)

	// 重新编译模拟新进程。
	out, err := compileAsk(t, store, &prepared).Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`5`)})
	require.NoError(t, err)
	require.True(t, out.Done)
	require.Equal(t, 15, out.State)
	require.Empty(t, out.Interrupts)
	require.EqualValues(t, 1, prepared.Load(), "准备步骤只回放")

	_, err = compileAsk(t, store, &prepared).Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`5`)})
	require.ErrorIs(t, err, workflow.ErrApprovalMismatch)
}

func TestNodeInterruptRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var prepared atomic.Int32
	run := compileAsk(t, store, &prepared)
	snap, err := run.Run(ctx, "ask", 10, "")
	point := requireInterrupted(t, snap, err)

	var rejected *workflow.ResumeRejectedError
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`{`)})
	require.ErrorAs(t, err, &rejected)
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key})
	require.ErrorAs(t, err, &rejected)
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: "forged", Input: json.RawMessage(`1`)})
	require.ErrorIs(t, err, workflow.ErrApprovalMismatch)

	snap, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`-1`)})
	require.ErrorAs(t, err, &rejected)
	require.True(t, snap.Waiting)
	require.Len(t, snap.Interrupts, 1)
	require.True(t, snap.Interrupts[0].Rejected)
	require.Equal(t, "需要正整数", snap.Interrupts[0].Reason)

	stream, err := run.AsRunnable().Stream(ctx, workflow.RunRequest[int]{RunID: "ask",
		Resume: &workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`-2`)}})
	require.NoError(t, err)
	var streamErr error
	for chunk := range stream {
		require.False(t, chunk.Event != nil && chunk.Event.Type == "interrupt_required", "拒绝路径不发中断事件")
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
	}
	require.ErrorAs(t, streamErr, &rejected)

	out, err := run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`2`)})
	require.NoError(t, err)
	require.Equal(t, 12, out.State)
}

func TestNodeInterruptAnswerLoggedBeforeCheckpointUpdate(t *testing.T) {
	ctx := context.Background()
	base := newStore(t)
	store := &filterFailStore{Store: base}
	var prepared atomic.Int32
	run := compileAsk(t, store, &prepared)
	snap, err := run.Run(ctx, "ask", 10, "")
	point := requireInterrupted(t, snap, err)

	// 输入写入日志后，节点完成后的推进写入失败：检查点仍是中断等待。
	store.fail = func(v *durable.Snapshot) bool { return bytes.Contains(v.Data, []byte(`"routing":true`)) }
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`5`)})
	require.ErrorIs(t, err, errDisk)
	store.fail = nil
	snap, err = run.Snapshot(ctx, "ask")
	require.NoError(t, err)
	require.True(t, snap.Waiting)

	var rejected *workflow.ResumeRejectedError
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`7`)})
	require.ErrorAs(t, err, &rejected)
	require.ErrorIs(t, err, workflow.ErrInterruptAnswered)
	require.EqualValues(t, 1, prepared.Load(), "冲突输入不执行节点")

	out, err := run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(" 5 ")})
	require.NoError(t, err, "空白差异视为相同输入")
	require.Equal(t, 15, out.State)
}

func TestNodeInterruptRecoversWithoutKeyWhenAnswered(t *testing.T) {
	ctx := context.Background()
	base := newStore(t)
	store := &filterFailStore{Store: base}
	var prepared atomic.Int32
	run := compileAsk(t, store, &prepared)
	snap, err := run.Run(ctx, "ask", 10, "")
	point := requireInterrupted(t, snap, err)
	store.fail = func(v *durable.Snapshot) bool { return bytes.Contains(v.Data, []byte(`"routing":true`)) }
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`5`)})
	require.ErrorIs(t, err, errDisk)
	store.fail = nil

	out, err := run.Run(ctx, "ask", 0, "")
	require.NoError(t, err)
	require.Equal(t, 15, out.State)
}

func TestInterruptAfterApprovalDoesNotApproveAgain(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var prepared, checks atomic.Int32
	before := workflow.WithBeforeApprove(func(context.Context, string, *workflow.GraphSnapshot[int]) error { checks.Add(1); return nil })
	cfg := workflow.DurableConfig{Store: store, Version: "ask-v1", ApprovalNodes: []string{"effect"}}
	run, err := oneNode(t, askNode(&prepared)).CompileDurable(ctx, cfg, before)
	require.NoError(t, err)
	wait, err := run.Run(ctx, "ask", 10, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.Empty(t, wait.Interrupts)

	snap, err := run.Run(ctx, "ask", 0, wait.Key)
	point := requireInterrupted(t, snap, err)
	require.EqualValues(t, 1, checks.Load())

	out, err := run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`1`)})
	require.NoError(t, err)
	require.Equal(t, 11, out.State)
	require.EqualValues(t, 1, checks.Load(), "中断恢复不再走审批")
}

func TestInterruptCheckpointCorruptionIsDetected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*durable.Snapshot, map[string]any)
	}{
		{name: "审批与等待并存但没有中断", mutate: func(cp *durable.Snapshot, data map[string]any) {
			cp.Approved = true
			delete(data, "interrupts")
		}},
		{name: "路由位置带中断", mutate: func(_ *durable.Snapshot, data map[string]any) { data["routing"] = true }},
		{name: "中断缺少键", mutate: func(_ *durable.Snapshot, data map[string]any) {
			data["interrupts"] = []any{map[string]any{"id": "amount", "info": map[string]any{}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := newStore(t)
			var prepared atomic.Int32
			run := compileAsk(t, store, &prepared)
			_, err := run.Run(ctx, "ask", 10, "")
			require.ErrorIs(t, err, durable.ErrApprovalRequired)
			cp, err := store.Load(ctx, "ask")
			require.NoError(t, err)
			var data map[string]any
			require.NoError(t, json.Unmarshal(cp.Data, &data))
			tc.mutate(cp, data)
			cp.Data, err = json.Marshal(data)
			require.NoError(t, err)
			require.NoError(t, store.Save(ctx, "ask", cp))
			_, err = run.Snapshot(ctx, "ask")
			require.ErrorContains(t, err, "corrupt")
		})
	}
}

func TestInterruptThroughRunnableEmitsEvent(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var prepared atomic.Int32
	run := compileAsk(t, store, &prepared)
	stream, err := run.AsRunnable().Stream(ctx, workflow.RunRequest[int]{RunID: "ask", Initial: 10})
	require.NoError(t, err)
	var sawEvent bool
	var final *workflow.GraphSnapshot[int]
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		if chunk.Event != nil && chunk.Event.Type == "interrupt_required" {
			sawEvent = true
		}
		if chunk.Done {
			final = chunk.Data
		}
	}
	require.True(t, sawEvent)
	require.NotNil(t, final)
	require.True(t, final.Waiting)
	require.Len(t, final.Interrupts, 1)

	out, err := run.AsRunnable().Invoke(ctx, workflow.RunRequest[int]{RunID: "ask",
		Resume: &workflow.ResumeRequest{Key: final.Interrupts[0].Key, Input: json.RawMessage(`4`)}})
	require.NoError(t, err)
	require.Equal(t, 14, out.State)
}
