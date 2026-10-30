package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/kart-io/goagent/core/durable"
)

// Diagnose 只读取保存的续跑元数据，不执行节点、路由、审批钩子或 Store 写入与加锁。
// 报告不能证明是否有其他执行者活跃。显式操作引用可使用嵌套父键，不列举 Store 或自动发现操作。
// 检查点与操作读取不构成事务；恢复未知执行前，宿主须协调活跃执行者并核对外部效果。
func (d *DurableGraph[T]) Diagnose(ctx context.Context, id string, refs ...durable.OperationRef) (*durable.DiagnosticReport, error) {
	if d == nil || d.store == nil || d.graph == nil || id == "" {
		return nil, fmt.Errorf("诊断需要已编译的持久图和运行 ID")
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
				case state.Node == END:
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
		return meta, fmt.Errorf("检查点无效")
	}
	if meta.Node != END && d.graph.nodes[meta.Node] == nil {
		return meta, fmt.Errorf("未知节点")
	}
	if err := d.validateContinuation(id, cp, graphState[T]{Node: meta.Node, Routing: meta.Routing}); err != nil {
		return meta, err
	}
	if meta.Parallel != nil {
		cfg, ok := d.graph.parallel[meta.Node]
		if !ok || meta.Routing || cp.Key == "" || cp.Waiting || (d.approvals[meta.Node] && !cp.Approved) {
			return meta, fmt.Errorf("并行检查点无效")
		}
		if len(meta.Parallel.Inputs) != len(cfg.Branches) || meta.Parallel.Outputs == nil {
			return meta, fmt.Errorf("并行检查点无效")
		}
		allowed := map[string]bool{}
		for _, b := range cfg.Branches {
			allowed[b.Name] = true
			if !json.Valid(meta.Parallel.Inputs[b.Name]) {
				return meta, fmt.Errorf("并行输入无效")
			}
		}
		for name, data := range meta.Parallel.Outputs {
			if !allowed[name] || !json.Valid(data) {
				return meta, fmt.Errorf("并行输出无效")
			}
		}
	}
	return meta, nil
}
