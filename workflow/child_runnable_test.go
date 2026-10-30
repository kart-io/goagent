package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

// checkpointFailStore 在 fail 非空时让图检查点的写入失败，操作记录照常保存。
type checkpointFailStore struct {
	durable.Store
	fail error
}

func (s *checkpointFailStore) Save(ctx context.Context, id string, v *durable.Snapshot) error {
	if s.fail != nil && !strings.HasPrefix(id, "goagent/operation/") {
		return s.fail
	}
	return s.Store.Save(ctx, id, v)
}

// runChild 在可续跑的持久操作内运行子图；approval 非空时作为续跑输入提供给等待处。
func runChild(ctx context.Context, g *workflow.DurableGraph[int], approval string) (*durable.OperationRecord, error) {
	return durable.Do(ctx, "tool/c1", json.RawMessage(`{}`), durable.OperationPolicy{MaxAttempts: 3, Resumable: true}, func(ctx context.Context) (json.RawMessage, error) {
		if approval != "" {
			ctx = durable.WithResume(ctx, durable.ExecutionKey(ctx), approval)
		}
		out, err := g.AsChildRunnable().Invoke(ctx, 1)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	})
}

func childContinuation(t *testing.T, err error) workflow.ChildContinuation {
	t.Helper()
	waits := durable.Waits(err)
	require.Len(t, waits, 1, "%v", err)
	var c workflow.ChildContinuation
	require.NoError(t, json.Unmarshal(waits[0].Continuation, &c))
	return c
}

func TestChildRunnableClassifiesApprovalOutcome(t *testing.T) {
	refuse := errors.New("金额已变化")
	for _, tc := range []struct {
		name     string
		approval func(key string) string
		before   func() error
		fail     error
		reason   string
		failure  func(*testing.T, error)
	}{
		{name: "审批键不符", approval: func(string) string { return "forged" }, reason: workflow.RejectApprovalMismatch},
		{name: "前置校验拒绝", approval: func(k string) string { return k }, before: func() error { return refuse }, reason: workflow.RejectBeforeApprove},
		{name: "前置校验 panic", approval: func(k string) string { return k }, before: func() error { panic("boom") }, failure: func(t *testing.T, err error) {
			var p *agentErrors.PanicError
			require.ErrorAs(t, err, &p)
		}},
		{name: "检查点写入失败", approval: func(k string) string { return k }, fail: errors.New("disk full"), failure: func(t *testing.T, err error) {
			require.ErrorContains(t, err, "disk full")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := newStore(t)
			store := &checkpointFailStore{Store: base}
			ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
			effects := 0
			g, err := oneNode(t, func(_ context.Context, n int) (int, error) { effects++; return n + 1, nil }).
				CompileDurable(ctx, workflow.DurableConfig{Store: base, Version: "v1", ApprovalNodes: []string{"effect"}},
					workflow.WithBeforeApprove(func(context.Context, string, *workflow.GraphSnapshot[int]) error {
						if tc.before != nil {
							return tc.before()
						}
						return nil
					}))
			require.NoError(t, err)

			_, err = runChild(ctx, g, "")
			first := childContinuation(t, err)
			require.False(t, first.Rejected)
			require.Empty(t, first.Reason)

			store.fail = tc.fail
			record, err := runChild(ctx, g, tc.approval(first.ApprovalKey))
			require.Zero(t, effects)
			if tc.failure != nil {
				require.Nil(t, durable.Waits(err), "失败不能被报告为等待：%v", err)
				tc.failure(t, err)
				require.Equal(t, "failed", record.Status)
				return
			}
			c := childContinuation(t, err)
			require.True(t, c.Rejected)
			require.Equal(t, tc.reason, c.Reason)
			require.Equal(t, "waiting", record.Status)

			// 拒绝后状态不变，仍可用正确条件批准。
			tc.before = nil
			record, err = runChild(ctx, g, first.ApprovalKey)
			require.NoError(t, err)
			require.Equal(t, "completed", record.Status)
			require.Equal(t, 1, effects)
		})
	}
}
