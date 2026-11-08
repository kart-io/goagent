package workflow_test

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

func TestChildRunnableRejectsUnstructuredResume(t *testing.T) {
	store := newStore(t)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
	effects := 0
	g, err := oneNode(t, func(_ context.Context, n int) (int, error) { effects++; return n + 1, nil }).
		CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}})
	require.NoError(t, err)
	run := func(ctx context.Context) (json.RawMessage, error) {
		n, err := g.AsChildRunnable().Invoke(ctx, 1)
		if err != nil {
			return nil, err
		}
		return json.Marshal(n)
	}
	policy := durable.OperationPolicy{Resumable: true}
	_, err = durable.Do(ctx, "tool/c1", json.RawMessage(`{}`), policy, run)
	wait := childContinuation(t, err)
	opKey := durable.ChildKey("parent", "tool/c1")
	for _, raw := range []string{wait.ApprovalKey, "", `null`, `[]`, `"key"`, `{`, `{"key":5}`} {
		_, err = durable.Do(durable.WithResume(ctx, opKey, raw), "tool/c1", json.RawMessage(`{}`), policy, run)
		rejected := childContinuation(t, err)
		require.True(t, rejected.Rejected)
		require.Equal(t, constants.WorkflowRejectResumeInput, rejected.Reason)
		require.Equal(t, wait.ApprovalKey, rejected.ApprovalKey)
		require.Zero(t, effects, "非结构化请求不能推进审批节点")
	}
	structured, err := workflow.WithResume(ctx, opKey, workflow.ResumeRequest{Key: wait.ApprovalKey})
	require.NoError(t, err)
	result, err := durable.Do(structured, "tool/c1", json.RawMessage(`{}`), policy, run)
	require.NoError(t, err)
	require.Equal(t, "completed", result.Status)
	require.Equal(t, 1, effects)
}

func TestChildResumePreservesMissingAndNullInput(t *testing.T) {
	for _, input := range []json.RawMessage{nil, json.RawMessage(`null`)} {
		t.Run(string(input), func(t *testing.T) {
			store := newStore(t)
			ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
			var observed json.RawMessage
			handled := 0
			g, err := oneNode(t, func(_ context.Context, n int) (int, error) { return n + 1, nil }).
				CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}},
					workflow.WithResumeHandler(func(_ context.Context, _ string, snap *workflow.GraphSnapshot[int], raw json.RawMessage) (int, error) {
						handled++
						observed = append(json.RawMessage(nil), raw...)
						return snap.State, nil
					}))
			require.NoError(t, err)
			run := func(ctx context.Context) (json.RawMessage, error) {
				n, err := g.AsChildRunnable().Invoke(ctx, 1)
				if err != nil {
					return nil, err
				}
				return json.Marshal(n)
			}
			policy := durable.OperationPolicy{Resumable: true}
			_, err = durable.Do(ctx, "tool/c1", json.RawMessage(`{}`), policy, run)
			wait := childContinuation(t, err)
			structured, err := workflow.WithResume(ctx, durable.ChildKey("parent", "tool/c1"), workflow.ResumeRequest{Key: wait.ApprovalKey, Input: input})
			require.NoError(t, err)
			_, err = durable.Do(structured, "tool/c1", json.RawMessage(`{}`), policy, run)
			require.NoError(t, err)
			require.Equal(t, 1, handled)
			require.Equal(t, input, observed, "省略输入和显式null不能混同")
		})
	}
}
