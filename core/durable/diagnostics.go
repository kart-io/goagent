package durable

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
)

// OperationRef locates an operation journal known to the host and does not
// require the Store to support listing. ParentKey is the parent execution key
// passed to Do, not the child key derived by the operation; diagnostics do not
// discover nested operations automatically.
type OperationRef struct {
	ParentKey string `json:"parent_key"`
	ID        string `json:"id"`
}

// DiagnosticReport contains metadata only: no business state, outputs, approval
// keys, resume payloads, or raw errors. Actions are suggestions the host may
// consult, not authorization to act. An incomplete checkpoint cannot
// distinguish running, crashed, or failed, and loading the checkpoint and
// operation records separately does not form a transactionally consistent view.
type DiagnosticReport struct {
	RunID      string                `json:"run_id"`
	Status     string                `json:"status"`
	Position   int                   `json:"position,omitzero"`
	Blockers   []string              `json:"blockers,omitempty"`
	Actions    []string              `json:"actions,omitempty"`
	Operations []OperationDiagnostic `json:"operations,omitempty"`
}

// OperationDiagnostic summarizes one operation record for DiagnoseOperations:
// Status is the record status or "not_found", FailureKind is a coarse category
// ("unknown" when unrecognized), and Actions suggests host next steps.
type OperationDiagnostic struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	Attempts    int      `json:"attempts,omitzero"`
	MaxAttempts int      `json:"max_attempts,omitzero"`
	FailureKind string   `json:"failure_kind,omitempty"`
	Actions     []string `json:"actions,omitempty"`
}

// DiagnosticError preserves errors.Is/As while avoiding printing Store errors
// that may contain credentials or raw payloads. Hosts should not serialize the
// unwrapped error for untrusted audiences.
type DiagnosticError struct {
	Cause error `json:"-"`
}

func (e *DiagnosticError) Error() string { return "诊断读取失败" }
func (e *DiagnosticError) Unwrap() error { return e.Cause }

// DiagnoseOperations only reads the explicitly specified operation records. It
// uses Load rather than InspectOperation: it takes no execution lock, writes
// nothing, and calls no reconcile function. pending only means the result is
// unknown; it does not prove the operation is running.
func DiagnoseOperations(ctx context.Context, store Store, refs ...OperationRef) ([]OperationDiagnostic, error) {
	if store == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "诊断需要 Store").WithComponent("durable_diagnostics").WithOperation("diagnose_operations")
	}
	result := make([]OperationDiagnostic, 0, len(refs))
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, &DiagnosticError{Cause: err}
		}
		if ref.ParentKey == "" || ref.ID == "" {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "操作父执行键和 ID 不能为空").WithComponent("durable_diagnostics").WithOperation("diagnose_operations")
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

// Diagnose takes no execution lock and only reads the most recently saved
// checkpoint. Hosts must coordinate with any active executor before acting; the
// checkpoint alone cannot prove that retrying external effects is safe.
func (r *Runner) Diagnose(ctx context.Context, id string, refs ...OperationRef) (*DiagnosticReport, error) {
	if r == nil || r.Store == nil || r.Version == "" || id == "" {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "运行 ID、版本和 Store 不能为空").WithComponent("durable_diagnostics").WithOperation("diagnose")
	}
	if err := ctx.Err(); err != nil {
		return nil, &DiagnosticError{Cause: err}
	}
	names := map[string]bool{}
	descriptor := r.Version
	for _, step := range r.Steps {
		if step.Name == "" || names[step.Name] || step.Run == nil {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "步骤需要唯一名称和执行函数").WithComponent("durable_diagnostics").WithOperation("diagnose")
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
