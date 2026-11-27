package adapters

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

func TestNativeApprovalAfterRestart(t *testing.T) {
	for _, decision := range []string{"approve", "reject"} {
		t.Run(decision, func(t *testing.T) {
			dir := t.TempDir()
			p := newProbe(t, dir, 1)
			_, err := p.run(context.Background(), nil)
			waits := durable.Waits(err)
			require.Len(t, waits, 1)
			require.Equal(t, 0, p.effects)
			q := newProbe(t, dir, 1)
			key := waits[0].Path[len(waits[0].Path)-1].Key
			_, err = q.run(context.Background(), map[string]string{key: `"` + decision + `"`})
			require.NoError(t, err)
			if decision == "approve" {
				require.Equal(t, 1, q.effects)
			} else {
				require.Zero(t, q.effects)
			}
		})
	}
}

// 宿主在等待审批期间改了工具说明：恢复时已完成的模型回合无法按原意图回放，写工具不执行。
func TestNativeResumeAfterToolDefinitionChange(t *testing.T) {
	dir := t.TempDir()
	p := newProbe(t, dir, 1)
	_, err := p.run(context.Background(), nil)
	waits := durable.Waits(err)
	require.Len(t, waits, 1)
	q := newProbe(t, dir, 1)
	q.description = "改过的说明"
	key := waits[0].Path[len(waits[0].Path)-1].Key
	_, err = q.run(context.Background(), map[string]string{key: `"approve"`})
	require.ErrorIs(t, err, agentErrors.ErrOperationIntentChanged)
	require.Zero(t, q.effects)
}

func TestParallelApprovalsStayIsolated(t *testing.T) {
	p := newProbe(t, t.TempDir(), 2)
	_, err := p.run(context.Background(), nil)
	waits := durable.Waits(err)
	require.Len(t, waits, 2)
	key := waits[0].Path[len(waits[0].Path)-1].Key
	_, err = p.run(context.Background(), map[string]string{key: `"approve"`})
	left := durable.Waits(err)
	require.Len(t, left, 1)
	require.Equal(t, 1, p.effects)
	key = left[0].Path[len(left[0].Path)-1].Key
	_, err = p.run(context.Background(), map[string]string{key: `"reject"`})
	require.NoError(t, err)
	require.Equal(t, 1, p.effects)
}

func TestCompletedRunRebuildsOutcomeReadOnly(t *testing.T) {
	p := newProbe(t, t.TempDir(), 1)
	_, err := p.run(context.Background(), nil)
	w := durable.Waits(err)[0]
	_, err = p.run(context.Background(), map[string]string{w.Path[len(w.Path)-1].Key: `"approve"`})
	require.NoError(t, err)
	requests, effects := p.requests, p.effects
	_, err = p.run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, requests, p.requests)
	require.Equal(t, effects, p.effects)
}
