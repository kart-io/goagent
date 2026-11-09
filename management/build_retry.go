package management

import (
	"context"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// RetryableBuildError 由 Factory 显式声明：本次 Build 未执行任务、未产生业务副作用，
// 且本次分配的资源已释放，可以安全重试构建。Err 保留实际失败原因。
// 此声明不代表此前 attempt 没有副作用，不能据此跳过已有恢复核对或取消核对。
// 只适用于 Build 返回错误且没有 Runtime 的情形，不适用于 Inspect/Run 错误。
// Worker 还会检查返回 Binding 的 Cleanup 成功，之后才恢复原命令。
type RetryableBuildError struct {
	Err error
}

func (e *RetryableBuildError) Error() string {
	if e == nil || e.Err == nil {
		return "本次构建未执行任务，可安全重试"
	}
	return "本次构建未执行任务，可安全重试：" + e.Err.Error()
}

func (e *RetryableBuildError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// buildClaim 只保存本次 claim 改动前的执行状态，不回写旧 Task 覆盖并发取消或修订号。
type buildClaim struct {
	state          TaskState
	recoveryNeeded bool
}

// canCancelWithoutInspection 只对尚未开始或停在已知等待点、且无旧执行待核对的任务成立。
// Attention 即使没有 RecoveryNeeded，也可能持有未知业务结果，必须先核对。
func canCancelWithoutInspection(state TaskState, recoveryNeeded bool) bool {
	return !recoveryNeeded && (state == Queued || state == WaitingInput || state == WaitingApproval)
}

// nextAttemptNumber 按 Store 的时间/ID 游标分页，历史序号有空洞或超过一页也不重复。
func nextAttemptNumber(ctx context.Context, reader Reader, task Task) (int, error) {
	query := Query{TaskID: task.ID, Limit: 200}
	maximum := 0
	for {
		attempts, err := all[Attempt](ctx, reader, "attempts", task.TenantID, query)
		if err != nil {
			return 0, err
		}
		for _, attempt := range attempts {
			maximum = max(maximum, attempt.Number)
		}
		if len(attempts) < query.Limit {
			return maximum + 1, nil
		}
		last := attempts[len(attempts)-1]
		query.Cursor = last.CreatedAt.Format(time.RFC3339Nano) + "|" + last.ID
	}
}

func (w *Worker) blockBuild(ctx context.Context, c Command, original Task, attempt Attempt, claim buildClaim) error {
	return w.s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		owner, err := read[WorkerOwner](ctx, tx, "workers", "__system", "default")
		if err != nil || owner.OwnerID != w.id {
			return agentErrors.ErrManagementOwner
		}
		t, err := read[Task](ctx, tx, "tasks", original.TenantID, original.ID)
		if err != nil {
			return err
		}
		cmd, err := read[Command](ctx, tx, "commands", c.TenantID, c.ID)
		if err != nil {
			return err
		}
		if t.State.Terminal() {
			return nil
		}
		if (t.State != Running && t.State != Canceling) || cmd.State != "applying" {
			return agentErrors.ErrManagementConflict
		}
		old := t.Revision
		t.State = claim.state
		t.RecoveryNeeded = claim.recoveryNeeded
		t.Revision++
		canceled := t.CancelRequested && canCancelWithoutInspection(claim.state, claim.recoveryNeeded)
		if canceled {
			// 已知等待点或未开始的新任务可完成取消；旧 attempt 待核对时绝不能走此分支。
			t.State = Canceled
			t.Waits = nil
			now := w.s.cfg.Now().UTC()
			t.FinishedAt = &now
			if err = w.s.releaseSession(ctx, tx, t); err != nil {
				return err
			}
			if err = w.s.supersedeApprovals(ctx, tx, t); err != nil {
				return err
			}
			raw, err := encode(map[string]any{"task_id": t.ID, "state": t.State})
			if err != nil {
				return err
			}
			if err = insert(ctx, tx, "outbox", Delivery{Meta: w.s.meta(t.Actor), Kind: "task." + string(t.State), ResourceID: t.ID, Payload: raw, State: "pending", AvailableAt: now}); err != nil {
				return err
			}
		}
		// CancelRequested、等待点、身份与其他并发字段均来自最新记录。
		// 非终态时保留 Session 的 ActiveTaskID，其他任务不能越过本次未恢复的任务。
		if err = update(ctx, tx, "tasks", t, old); err != nil {
			return err
		}
		old = cmd.Revision
		cmd.State = "blocked"
		cmd.ErrorKind = "build_unavailable"
		// 持续的构建故障不能在每次轮询中产生一个新 attempt。
		cmd.AvailableAt = w.s.cfg.Now().Add(time.Second).UTC()
		if canceled {
			cmd.State = "applied"
			cmd.ErrorKind = ""
		}
		cmd.Revision++
		if err = update(ctx, tx, "commands", cmd, old); err != nil {
			return err
		}
		now := w.s.cfg.Now().UTC()
		attempt.FinishedAt = &now
		attempt.Outcome = "blocked"
		attempt.Revision++
		if err = update(ctx, tx, "attempts", attempt, attempt.Revision-1); err != nil {
			return err
		}
		return w.s.audit(ctx, tx, t.Actor, "tasks", t.ID, "build_blocked", "build_unavailable")
	})
}
