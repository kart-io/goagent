package adapters

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/stretchr/testify/require"
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
