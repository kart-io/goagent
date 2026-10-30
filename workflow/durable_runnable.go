package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
)

// RunRequest binds a persisted workflow invocation to an explicit run ID.
// Initial is ignored when resuming; ApprovalKey must be authorized by the host.
type RunRequest[T any] struct {
	RunID       string
	Initial     T
	ApprovalKey string
}

// AsRunnable exposes Invoke/Stream/Batch through the same durable execution
// source as Run. An approval pause returns a successful invocation with
// Snapshot.Waiting=true and Snapshot.Done=false. Stream Done terminates this
// invocation, not necessarily the business process. Run retains ErrApprovalRequired
// for compatibility. Events are live observations, not a persisted event log.
// Intermediate node chunks carry Data.Preview=true and an uncommitted State.
// Only a non-preview final snapshot represents the saved continuation.
func (d *DurableGraph[T]) AsRunnable() *core.StreamRunnable[RunRequest[T], *GraphSnapshot[T]] {
	return core.NewStreamRunnable(core.StreamModeSteps, func(ctx context.Context, req RunRequest[T], emit func(core.StreamChunk[*GraphSnapshot[T]]) error) error {
		snapshot, err := d.run(ctx, d.store, req.RunID, req.Initial, req.ApprovalKey, emit)
		// Only the actual durable wait is a normal pause. Joined unlock errors or
		// observer errors must not be converted to a successful invocation.
		if err != nil && (err != durable.ErrApprovalRequired || snapshot == nil || !snapshot.Waiting) {
			return err
		}
		return emit(core.StreamChunk[*GraphSnapshot[T]]{Done: true, Data: snapshot})
	}).WithDescription(func() any { return describe(d.graph) })
}

// ChildContinuation 是嵌套运行的图在审批暂停时放进 durable.WaitError 的定位信息。
// 宿主续跑时用 durable.WithResume(ctx, 等待处的执行键, ApprovalKey) 提供批准。
type ChildContinuation struct {
	RunID       string `json:"run_id"`
	Node        string `json:"node"`
	ApprovalKey string `json:"approval_key"`
	// Rejected 表示本次提供的审批未被采纳，图的状态未改变，仍在等待；Reason 为原因类别
	// RejectApprovalMismatch 或 RejectBeforeApprove。存储失败、panic 等不属于拒绝，照常作为失败返回。
	Rejected bool   `json:"rejected,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// ChildContinuation.Reason 的取值。
const (
	RejectApprovalMismatch = "approval_mismatch" // 审批键与等待的节点不符
	RejectBeforeApprove    = "before_approve"    // WithBeforeApprove 的钩子拒绝
)

// approvalWait 判断 err 是否为审批暂停或未被采纳的审批，并返回拒绝原因类别（审批暂停时为空）。
// 与别的错误并列（如解锁失败）时不算，照常作为失败。
func approvalWait(err error) (reason string, ok bool) {
	if joined, isJoined := err.(interface{ Unwrap() []error }); isJoined && len(joined.Unwrap()) > 1 {
		return "", false
	}
	var rejected *ApprovalRejectedError
	switch {
	case errors.Is(err, durable.ErrApprovalRequired):
		return "", true
	case errors.Is(err, ErrApprovalMismatch):
		return RejectApprovalMismatch, true
	case errors.As(err, &rejected):
		return RejectBeforeApprove, true
	}
	return "", false
}

// AsChildRunnable 供图作为子流程嵌套运行，例如经 tools.NewRunnableToolFactory 包成可续跑工具。
// 检查点与节点内的持久操作写在父操作日志的 Store 中，不使用 DurableConfig.Store：子流程必须与父流程共用
// 同一 Store，否则崩溃后子图从头重跑、已执行的节点再执行一次。读取子图状态时以该 Store 编译图后调用 Snapshot。
// 运行 ID 取当前执行键，须在声明 Resumable 的持久操作内调用（见 durable.CanWait），否则在执行任何节点前
// 返回 durable.ErrWaitNotResumable：其他操作不会重新进入，审批之后无法续跑。审批暂停返回 *durable.WaitError 而不是成功，
// 等待不会被当作结果交给上层。续跑时从 durable.ResumeInput 读取审批键，首次运行时它被忽略。
// 完成时返回最终状态。
func (d *DurableGraph[T]) AsChildRunnable() *core.RunnableFunc[T, T] {
	return core.NewRunnableFunc(func(ctx context.Context, initial T) (T, error) {
		var zero T
		id := durable.ExecutionKey(ctx)
		if id == "" {
			return zero, fmt.Errorf("child graph requires an execution key")
		}
		if !durable.CanWait(ctx) {
			return zero, fmt.Errorf("%w：子图须在可续跑的操作内运行，如 RunnableToolConfig.Resumable", durable.ErrWaitNotResumable)
		}
		// CanWait 为真时 ctx 必有日志。
		store := durable.JournalStore(ctx)
		// 续跑输入只用于仍在等待的图；已越过审批后中断再进入时，宿主多半还带着同一输入。
		approvalKey, ok := durable.ResumeInput(ctx)
		if ok {
			current, err := d.snapshot(ctx, store, id)
			switch {
			case errors.Is(err, os.ErrNotExist):
				approvalKey = ""
			case err != nil:
				return zero, err
			case !current.Waiting:
				approvalKey = ""
			}
		}
		snapshot, err := d.run(ctx, store, id, initial, approvalKey, func(core.StreamChunk[*GraphSnapshot[T]]) error { return nil })
		// 审批暂停与未被采纳的审批报告为等待，避免上层把这次续跑记成失败而无法再续；其他错误照常失败。
		reason, waiting := approvalWait(err)
		if waiting && snapshot != nil && snapshot.Waiting && ctx.Err() == nil {
			continuation, e := json.Marshal(ChildContinuation{RunID: id, Node: snapshot.Node, ApprovalKey: snapshot.Key, Rejected: reason != "", Reason: reason})
			if e != nil {
				return zero, e
			}
			return zero, &durable.WaitError{Continuation: continuation}
		}
		if err != nil {
			return zero, err
		}
		if !snapshot.Done {
			return zero, fmt.Errorf("child graph stopped before completion")
		}
		return snapshot.State, nil
	})
}
