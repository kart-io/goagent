package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// OperationRef 定位宿主已知的操作日志，不要求 Store 支持列举。
// ParentKey 是传入 Do 的父执行键，不是操作派生的子键；诊断不会自动发现嵌套操作。
type OperationRef struct {
	ParentKey string `json:"parent_key"`
	ID        string `json:"id"`
}

// DiagnosticReport 仅包含元数据，不含业务状态、输出、审批键、续跑载荷或原始错误。
// Actions 是宿主可参考的建议，不代表执行授权。未完成检查点不能区分运行中、崩溃或失败；
// 分别加载检查点和操作记录也不构成事务一致的视图。
type DiagnosticReport struct {
	RunID      string                `json:"run_id"`
	Status     string                `json:"status"`
	Position   int                   `json:"position,omitempty"`
	Blockers   []string              `json:"blockers,omitempty"`
	Actions    []string              `json:"actions,omitempty"`
	Operations []OperationDiagnostic `json:"operations,omitempty"`
}

type OperationDiagnostic struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	Attempts    int      `json:"attempts,omitempty"`
	MaxAttempts int      `json:"max_attempts,omitempty"`
	FailureKind string   `json:"failure_kind,omitempty"`
	Actions     []string `json:"actions,omitempty"`
}

// DiagnosticError 保留 errors.Is/As，同时避免打印可能包含凭据或原始载荷的 Store 错误。
// 宿主不应把解包后的错误序列化给不可信受众。
type DiagnosticError struct {
	Cause error `json:"-"`
}

func (e *DiagnosticError) Error() string { return "诊断读取失败" }
func (e *DiagnosticError) Unwrap() error { return e.Cause }

// DiagnoseOperations 只读显式指定的操作记录。使用 Load 而非 InspectOperation，
// 不取得执行锁、不写入，也不调用核对函数。pending 仅表示结果未知，不证明正在执行。
func DiagnoseOperations(ctx context.Context, store Store, refs ...OperationRef) ([]OperationDiagnostic, error) {
	if store == nil {
		return nil, fmt.Errorf("诊断需要 Store")
	}
	result := make([]OperationDiagnostic, 0, len(refs))
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, &DiagnosticError{Cause: err}
		}
		if ref.ParentKey == "" || ref.ID == "" {
			return nil, fmt.Errorf("操作父执行键和 ID 不能为空")
		}
		item := OperationDiagnostic{ID: ref.ID}
		record, err := loadOperation(ctx, store, ChildKey(ref.ParentKey, ref.ID), ref.ID)
		switch {
		case errors.Is(err, os.ErrNotExist):
			item.Status = "not_found"
			item.Actions = []string{"verify_operation_reference"}
		case err != nil:
			return nil, &DiagnosticError{Cause: err}
		default:
			item.Status = record.Status
			item.Attempts = record.Attempts
			item.MaxAttempts = record.MaxAttempts
			switch record.Status {
			case "pending":
				item.Actions = []string{"reconcile_effects_before_resume"}
			case "failed":
				item.Actions = []string{"review_failure_and_retry_policy"}
				switch record.FailureKind {
				case "execution", "canceled", "deadline", "not_resumable":
					item.FailureKind = record.FailureKind
				default:
					item.FailureKind = "unknown"
				}
			case "waiting":
				item.Actions = []string{"review_continuation_and_authorize"}
			case "completed":
				item.Actions = []string{"replay_completed_operation"}
			}
		}
		result = append(result, item)
	}
	return result, nil
}

// Diagnose 不取得执行锁，只读取最近保存的检查点。宿主采取动作前须与活跃执行者协调；
// 单凭检查点无法证明重试外部效果是安全的。
func (r *Runner) Diagnose(ctx context.Context, id string, refs ...OperationRef) (*DiagnosticReport, error) {
	if r == nil || r.Store == nil || r.Version == "" || id == "" {
		return nil, fmt.Errorf("运行 ID、版本和 Store 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, &DiagnosticError{Cause: err}
	}
	names := map[string]bool{}
	descriptor := r.Version
	for _, step := range r.Steps {
		if step.Name == "" || names[step.Name] || step.Run == nil {
			return nil, fmt.Errorf("步骤需要唯一名称和执行函数")
		}
		names[step.Name] = true
		descriptor += fmt.Sprintf("\x00%s:%t", step.Name, step.RequireApproval)
	}
	report := &DiagnosticReport{RunID: id}
	cp, err := r.Store.Load(ctx, id)
	switch {
	case errors.Is(err, os.ErrNotExist):
		report.Status = "not_found"
		report.Actions = []string{"verify_run_id"}
	case err != nil:
		return nil, &DiagnosticError{Cause: err}
	case cp == nil:
		report.Blockers = []string{"invalid_checkpoint"}
	default:
		if cp.Version != r.Version {
			report.Blockers = append(report.Blockers, "version_mismatch")
		}
		if cp.Plan != digest(descriptor) {
			report.Blockers = append(report.Blockers, "plan_mismatch")
		}
		if cp.RunID != id || cp.Next < 0 || cp.Next > len(r.Steps) || !json.Valid(cp.Data) {
			report.Blockers = append(report.Blockers, "invalid_checkpoint")
		}
		if len(report.Blockers) == 0 {
			report.Position = cp.Next
			var valid bool
			if cp.Next == len(r.Steps) {
				valid = cp.Key == "" && !cp.Waiting && !cp.Approved
			} else {
				step := r.Steps[cp.Next]
				expected := digest(id + "\x00" + cp.Plan + "\x00" + step.Name)
				valid = (cp.Key == "" || cp.Key == expected) && (!cp.Waiting || !cp.Approved) && ((!cp.Waiting && !cp.Approved) || (step.RequireApproval && cp.Key == expected))
			}
			if !valid {
				report.Blockers = append(report.Blockers, "invalid_checkpoint")
			} else if cp.Next == len(r.Steps) {
				report.Status = "completed"
			} else if cp.Waiting {
				report.Status = "approval_waiting"
				report.Actions = []string{"review_and_authorize_approval"}
			} else {
				report.Status = "outcome_unknown"
				report.Actions = []string{"reconcile_effects_before_resume"}
			}
		}
	}
	if len(report.Blockers) > 0 {
		report.Status = "blocked"
		report.Actions = []string{"restore_matching_plan_or_migrate_checkpoint"}
	}
	report.Operations, err = DiagnoseOperations(ctx, r.Store, refs...)
	if err != nil {
		return nil, err
	}
	return report, nil
}
