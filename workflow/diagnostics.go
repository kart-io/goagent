package workflow

import (
	"context"
	"errors"
	"os"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// Diagnose only reads the saved resume metadata; it does not execute nodes,
// routes, or approval hooks, nor write to or lock the Store. The report cannot
// prove whether other executors are active. Explicit operation references may
// use nested parent keys; it does not enumerate the Store or discover
// operations automatically. Checkpoint and operation reads are not
// transactional; before resuming an unknown execution, the host must
// coordinate active executors and reconcile external effects.
func (d *DurableGraph[T]) Diagnose(ctx context.Context, id string, refs ...durable.OperationRef) (*durable.DiagnosticReport, error) {
	if d == nil || d.store == nil || d.graph == nil || id == "" {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "诊断需要已编译的持久图和运行 ID").WithComponent("workflow").WithOperation("diagnose")
	}
	if err := ctx.Err(); err != nil {
		return nil, &durable.DiagnosticError{Cause: err}
	}
	report := &durable.DiagnosticReport{RunID: id}
	cp, err := d.store.Load(ctx, id)
	switch {
	case errors.Is(err, os.ErrNotExist):
		report.Status = "not_found"
		report.Actions = []string{"verify_run_id"}
	case err != nil:
		return nil, &durable.DiagnosticError{Cause: err}
	case cp == nil:
		report.Blockers = []string{"invalid_checkpoint"}
	default:
		if cp.Version != d.version {
			report.Blockers = append(report.Blockers, "version_mismatch")
		}
		if cp.Plan != d.plan {
			report.Blockers = append(report.Blockers, "plan_mismatch")
		}
		if len(report.Blockers) == 0 {
			// 仅解码原始 JSON 元数据，避免调用宿主的 T.UnmarshalJSON 或暴露业务状态。
			state, e := d.decodeDiagnosticCheckpoint(id, cp)
			if e != nil {
				report.Blockers = append(report.Blockers, "invalid_checkpoint")
			} else {
				report.Position = cp.Next
				switch {
				case state.Node == constants.WorkflowEnd:
					report.Status = "completed"
				case cp.Waiting:
					report.Status = "approval_waiting"
					report.Actions = []string{"review_and_authorize_approval"}
				default:
					report.Status = "outcome_unknown"
					report.Actions = []string{"reconcile_effects_before_resume"}
				}
			}
		}
	}
	if len(report.Blockers) > 0 {
		report.Status = "blocked"
		report.Actions = []string{"restore_matching_plan_or_migrate_checkpoint"}
	}
	report.Operations, err = durable.DiagnoseOperations(ctx, d.store, refs...)
	if err != nil {
		return nil, err
	}
	return report, nil
}

// 仅解码结构元数据，不验证业务状态类型兼容性，避免执行宿主自定义的反序列化代码。
func (d *DurableGraph[T]) decodeDiagnosticCheckpoint(id string, cp *durable.Snapshot) (graphState[json.RawMessage], error) {
	var meta graphState[json.RawMessage]
	if cp.RunID != id || cp.Next < 0 || cp.Next > d.graph.config.MaxSteps || json.Unmarshal(cp.Data, &meta) != nil {
		return meta, agentErrors.Newf(agentErrors.CodeStateValidation, "检查点无效").WithComponent("workflow").WithOperation("decode_diagnostic_checkpoint")
	}
	if meta.Node != constants.WorkflowEnd && d.graph.nodes[meta.Node] == nil {
		return meta, agentErrors.Newf(agentErrors.CodeStateValidation, "未知节点").WithComponent("workflow").WithOperation("decode_diagnostic_checkpoint")
	}
	if err := d.validateContinuationMetadata(id, cp, meta); err != nil {
		return meta, err
	}
	return meta, nil
}
