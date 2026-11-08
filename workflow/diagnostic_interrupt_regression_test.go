package workflow_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
)

func TestDiagnosticInterruptMatrix(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"普通", "审批后", "并行", "并行审批后", "嵌套"} {
		t.Run(kind, func(t *testing.T) {
			store := newStore(t)
			var calls atomic.Int32
			compile := func(store durable.Store) *workflow.DurableGraph[int] {
				if kind == "嵌套" {
					return nestedGraphs(t, store, &calls)
				}
				g := oneNode(t, askNode(&calls))
				cfg := workflow.DurableConfig{Store: store, Version: "diagnostic-v1"}
				if kind == "并行" || kind == "并行审批后" {
					g = parallelGraph(t, 2, []workflow.Branch[int]{askBranch("a", &calls), askBranch("b", &calls)}, sumBranches)
				}
				if kind == "审批后" {
					cfg.ApprovalNodes = []string{"effect"}
				}
				if kind == "并行审批后" {
					cfg.ApprovalNodes = []string{"fork"}
				}
				d, err := g.CompileDurable(ctx, cfg)
				require.NoError(t, err)
				return d
			}
			d := compile(store)
			wait, err := d.Run(ctx, "diagnose", 10, "")
			require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)
			if len(wait.Interrupts) == 0 {
				wait, err = d.Run(ctx, "diagnose", 0, wait.Key)
				require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)
			}
			require.NotEmpty(t, wait.Interrupts)
			reader := compile(graphDiagnosticReadOnly{store})
			for {
				report, err := reader.Diagnose(ctx, "diagnose")
				require.NoError(t, err)
				require.Empty(t, report.Blockers)
				require.Equal(t, "approval_waiting", report.Status)
				compat, err := reader.CheckCompatibility(ctx, "diagnose")
				require.NoError(t, err)
				require.True(t, compat.Runs[0].Compatible)
				wait, err = d.Resume(ctx, "diagnose", workflow.ResumeRequest{Key: wait.Interrupts[0].Key, Input: json.RawMessage(`2`)})
				if err == nil {
					require.True(t, wait.Done)
					break
				}
				require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)
				require.NotEmpty(t, wait.Interrupts)
			}
		})
	}
}

func TestDiagnosticInterruptDoesNotDecodeBusinessState(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var calls atomic.Int32
	d := compileAsk(t, store, &calls)
	_, err := d.Run(ctx, "ask", 10, "")
	require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)
	g := workflow.NewGraph[manifestPanicState]()
	require.NoError(t, g.AddNode("effect", func(context.Context, manifestPanicState) (manifestPanicState, error) {
		panic("诊断不得执行节点")
	}))
	require.NoError(t, g.AddEdge(constants.WorkflowStart, "effect"))
	require.NoError(t, g.AddEdge("effect", constants.WorkflowEnd))
	reader, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: graphDiagnosticReadOnly{store}, Version: "ask-v1"})
	require.NoError(t, err)
	report, err := reader.CheckCompatibility(ctx, "ask")
	require.NoError(t, err)
	require.True(t, report.Runs[0].Compatible)
}

func TestDiagnosticParallelInterruptDoesNotDecodeBusinessState(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var calls atomic.Int32
	d, err := parallelGraph(t, 2, []workflow.Branch[int]{askBranch("a", &calls), askBranch("b", &calls)}, sumBranches).
		CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "par-v1"})
	require.NoError(t, err)
	_, err = d.Run(ctx, "par", 10, "")
	require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)
	g := workflow.NewGraph[manifestPanicState](workflow.Config{MaxConcurrency: 2})
	branches := []workflow.Branch[manifestPanicState]{}
	for _, name := range []string{"a", "b"} {
		branches = append(branches, workflow.Branch[manifestPanicState]{Name: name, Run: func(context.Context, manifestPanicState) (manifestPanicState, error) { panic("不得执行分支") }})
	}
	require.NoError(t, g.AddParallel("fork", workflow.ParallelConfig[manifestPanicState]{Branches: branches,
		Clone: func(manifestPanicState) manifestPanicState { panic("不得复制业务状态") },
		Merge: func(context.Context, manifestPanicState, []manifestPanicState) (manifestPanicState, error) {
			panic("不得合并")
		}}))
	require.NoError(t, g.AddEdge(constants.WorkflowStart, "fork"))
	require.NoError(t, g.AddEdge("fork", constants.WorkflowEnd))
	reader, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: graphDiagnosticReadOnly{store}, Version: "par-v1"})
	require.NoError(t, err)
	report, err := reader.Diagnose(ctx, "par")
	require.NoError(t, err)
	require.Empty(t, report.Blockers)
	compat, err := reader.CheckCompatibility(ctx, "par")
	require.NoError(t, err)
	require.True(t, compat.Runs[0].Compatible)
}

func TestDiagnosticInterruptRejectsCorruptMetadata(t *testing.T) {
	for _, field := range []string{"key", "id", "info", "waiting", "routing", "approved"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			store := newStore(t)
			var calls atomic.Int32
			g := oneNode(t, askNode(&calls))
			cfg := workflow.DurableConfig{Store: store, Version: "ask-v1", ApprovalNodes: []string{"effect"}}
			d, err := g.CompileDurable(ctx, cfg)
			require.NoError(t, err)
			wait, err := d.Run(ctx, "ask", 10, "")
			require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)
			_, err = d.Run(ctx, "ask", 0, wait.Key)
			require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)
			cp, err := store.Load(ctx, "ask")
			require.NoError(t, err)
			var meta map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(cp.Data, &meta))
			switch field {
			case "waiting":
				cp.Waiting = false
			case "approved":
				cp.Approved = false
			case "routing":
				meta["routing"] = json.RawMessage(`true`)
			default:
				var interrupts []map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(meta["interrupts"], &interrupts))
				delete(interrupts[0], field)
				meta["interrupts"], err = json.Marshal(interrupts)
				require.NoError(t, err)
			}
			cp.Data, err = json.Marshal(meta)
			require.NoError(t, err)
			require.NoError(t, store.Save(ctx, "ask", cp))
			cfg.Store = graphDiagnosticReadOnly{store}
			reader, err := g.CompileDurable(ctx, cfg)
			require.NoError(t, err)
			report, err := reader.Diagnose(ctx, "ask")
			require.NoError(t, err)
			require.Contains(t, report.Blockers, "invalid_checkpoint")
			compat, err := reader.CheckCompatibility(ctx, "ask")
			require.NoError(t, err)
			require.False(t, compat.Runs[0].Compatible)
		})
	}
}
