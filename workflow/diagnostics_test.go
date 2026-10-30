package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

func TestGraphDiagnoseAmbiguityAndCompletion(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	fail := true
	g := oneNode(t, func(context.Context, int) (int, error) {
		if fail {
			return 0, errors.New("SECRET raw failure")
		}
		return 42, nil
	})
	cfg := workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}}
	d, err := g.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	cp, err := d.Run(ctx, "run", 99, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	readonly, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: graphDiagnosticReadOnly{store}, Version: "v1", ApprovalNodes: cfg.ApprovalNodes})
	require.NoError(t, err)
	report, err := readonly.Diagnose(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, "approval_waiting", report.Status)
	_, err = d.Run(ctx, "run", 0, cp.Key)
	require.Error(t, err)
	report, err = readonly.Diagnose(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, "outcome_unknown", report.Status)
	require.Contains(t, report.Actions, "reconcile_effects_before_resume")
	raw, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "SECRET")
	require.NotContains(t, string(raw), "99")
	changed, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v2"})
	require.NoError(t, err)
	report, err = changed.Diagnose(ctx, "run")
	require.NoError(t, err)
	require.Contains(t, report.Blockers, "version_mismatch")
	fail = false
	_, err = d.Run(ctx, "run", 0, "")
	require.NoError(t, err)
	report, err = readonly.Diagnose(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, "completed", report.Status)
}

type graphDiagnosticReadOnly struct{ durable.Store }

func (graphDiagnosticReadOnly) Lock(context.Context, string) (func() error, error) {
	panic("diagnostic lock")
}
func (graphDiagnosticReadOnly) Save(context.Context, string, *durable.Snapshot) error {
	panic("diagnostic write")
}
