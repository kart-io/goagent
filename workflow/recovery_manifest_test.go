package workflow_test

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

func TestRecoveryManifestLegacyPlan(t *testing.T) {
	store := newStore(t)
	g := oneNode(t, func(_ context.Context, v int) (int, error) { return v + 1, nil })
	d, err := g.CompileDurable(context.Background(), workflow.DurableConfig{Store: store, Version: "v1"})
	require.NoError(t, err)
	_, err = d.Run(context.Background(), "legacy", 0, "")
	require.NoError(t, err)
	cp, err := store.Load(context.Background(), "legacy")
	require.NoError(t, err)
	require.Equal(t, "0954e6cf27acda9bc4ca3bcb9e7fdb0aac2545492139b70fa2826e3c2b55ffd1", cp.Plan)
	for _, components := range []map[string]string{nil, {}} {
		current, e := g.CompileDurable(context.Background(), workflow.DurableConfig{Store: store, Version: "v1", Components: components})
		require.NoError(t, e)
		require.Equal(t, cp.Plan, current.Manifest().Plan)
		report, e := current.CheckCompatibility(context.Background(), "legacy")
		require.NoError(t, e)
		require.True(t, report.Runs[0].Compatible)
	}
}

func TestRecoveryManifestDeclarationsAndDrift(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	g := oneNode(t, func(_ context.Context, v int) (int, error) { return v, nil })
	cfg := workflow.DurableConfig{Store: store, Version: "v1", StateVersion: "state-v1", Components: map[string]string{"agent": "a1", "tool": "t1"}}
	d, err := g.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	manifest := d.Manifest()
	require.Equal(t, "state-v1", manifest.StateVersion)
	require.Equal(t, "a1", manifest.Components["agent"])
	cfg.Components["agent"] = "被外部改写"
	manifest.Components["tool"] = "被报告改写"
	require.Equal(t, "a1", d.Manifest().Components["agent"])
	require.Equal(t, "t1", d.Manifest().Components["tool"])
	_, err = d.Run(ctx, "run", 7, "")
	require.NoError(t, err)
	for _, change := range []string{"顺序", "组件", "状态", "流程"} {
		t.Run(change, func(t *testing.T) {
			next := workflow.DurableConfig{Store: graphDiagnosticReadOnly{store}, Version: "v1", StateVersion: "state-v1", Components: map[string]string{"tool": "t1", "agent": "a1"}}
			if change == "组件" {
				next.Components["agent"] = "a2"
			}
			if change == "状态" {
				next.StateVersion = "state-v2"
			}
			if change == "流程" {
				next.Version = "v2"
			}
			current, e := g.CompileDurable(ctx, next)
			require.NoError(t, e)
			report, e := current.CheckCompatibility(ctx, "run", "missing")
			require.NoError(t, e)
			require.Len(t, report.Runs, 2)
			require.Equal(t, change == "顺序", report.Runs[0].Compatible)
			require.False(t, report.Runs[1].Compatible)
			require.Equal(t, "not_found", report.Runs[1].Status)
			if change == "顺序" {
				require.Equal(t, d.Manifest().Plan, current.Manifest().Plan)
			} else {
				require.Contains(t, report.Runs[0].Blockers, "plan_mismatch")
			}
		})
	}
}

type manifestPanicState int

func (*manifestPanicState) UnmarshalJSON([]byte) error { panic("兼容检查不得解码业务状态") }

func TestRecoveryCompatibilityIsReadOnly(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	initial := oneNode(t, func(_ context.Context, v int) (int, error) { return v, nil })
	d, err := initial.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1"})
	require.NoError(t, err)
	_, err = d.Run(ctx, "run", 7, "")
	require.NoError(t, err)
	g := workflow.NewGraph[manifestPanicState]()
	require.NoError(t, g.AddNode("effect", func(context.Context, manifestPanicState) (manifestPanicState, error) {
		t.Fatal("不能执行节点")
		return 0, nil
	}))
	require.NoError(t, g.AddEdge(workflow.START, "effect"))
	require.NoError(t, g.AddEdge("effect", workflow.END))
	readOnly, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: graphDiagnosticReadOnly{store}, Version: "v1"})
	require.NoError(t, err)
	report, err := readOnly.CheckCompatibility(ctx, "run")
	require.NoError(t, err)
	require.True(t, report.Runs[0].Compatible)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = readOnly.CheckCompatibility(canceled, "run")
	require.ErrorIs(t, err, context.Canceled)
	for _, ids := range [][]string{nil, {""}, {"run", "run"}} {
		_, err = readOnly.CheckCompatibility(ctx, ids...)
		require.Error(t, err)
	}
}

func TestRecoveryManifestRejectsEmptyComponentVersions(t *testing.T) {
	g := oneNode(t, func(_ context.Context, v int) (int, error) { return v, nil })
	for _, cfg := range []workflow.DurableConfig{
		{Version: "v1", StateVersion: " "},
		{Version: "v1", Components: map[string]string{"agent": ""}},
		{Version: "v1", Components: map[string]string{" agent": "v1"}},
	} {
		cfg.Store = newStore(t)
		_, err := g.CompileDurable(context.Background(), cfg)
		require.Error(t, err)
	}
}
