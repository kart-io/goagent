package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// RunRequest binds a persisted workflow invocation to an explicit run ID.
// Initial is ignored when resuming; ApprovalKey must be authorized by the host.
type RunRequest[T any] struct {
	RunID       string
	Initial     T
	ApprovalKey string
	// Resume supplies human input and is mutually exclusive with ApprovalKey.
	Resume *ResumeRequest
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
		key := req.ApprovalKey
		var input json.RawMessage
		if req.Resume != nil {
			if key != "" || req.Resume.Key == "" {
				return &ResumeRejectedError{Err: agentErrors.Newf(agentErrors.CodeInvalidInput, "provide only Resume with a waiting checkpoint key, or ApprovalKey").WithComponent("workflow").WithOperation("as_runnable")}
			}
			key, input = req.Resume.Key, req.Resume.Input
		}
		snapshot, err := d.run(ctx, d.store, req.RunID, req.Initial, key, input, emit)
		// Only the actual durable wait is a normal pause. Joined unlock errors or
		// observer errors must not be converted to a successful invocation.
		if err != nil && (err != agentErrors.ErrApprovalRequired || snapshot == nil || !snapshot.Waiting) {
			return err
		}
		return emit(core.StreamChunk[*GraphSnapshot[T]]{Done: true, Data: snapshot})
	}).WithDescription(func() any { return describe(d.graph) })
}

// ChildContinuation 是嵌套运行的图在审批暂停或中断等待时放进 durable.WaitError 的定位信息。
// 审批暂停时用 workflow.WithResume(ctx, 等待处的执行键, ResumeRequest{Key: ApprovalKey}) 提供批准；中断等待时 ApprovalKey 为空，
// 须用 Interrupts[i].Key（最内层中断键）回应。
type ChildContinuation struct {
	RunID       string `json:"run_id"`
	Node        string `json:"node"`
	ApprovalKey string `json:"approval_key"`
	// Rejected 表示本次提供的审批未被采纳，图的状态未改变，仍在等待；Reason 为原因类别
	// constants.WorkflowRejectApprovalMismatch（含中断等待时用意图键交输入）、
	// constants.WorkflowRejectBeforeApprove 或 constants.WorkflowRejectResumeInput。
	// 存储失败、panic 等不属于拒绝，照常作为失败返回。
	Rejected bool   `json:"rejected,omitzero"`
	Reason   string `json:"reason,omitempty"`
	// Interrupts 是子图（含更深层）待回应的中断，Key 为最内层中断键。可以直接用
	// durable.WithResume(ctx, Key, input) 回应，也可以经 WithResume 封装给本子图。
	Interrupts []InterruptPoint `json:"interrupts,omitempty"`
}

// approvalWait 判断 err 是否为审批暂停或未被采纳的审批，并返回拒绝原因类别（审批暂停时为空）。
// 与别的错误并列（如解锁失败）时不算，照常作为失败。
func approvalWait(err error) (reason string, ok bool) {
	if joined, isJoined := err.(interface{ Unwrap() []error }); isJoined && len(joined.Unwrap()) > 1 {
		return "", false
	}
	var rejected *ApprovalRejectedError
	var inputRejected *ResumeRejectedError
	switch {
	case errors.Is(err, agentErrors.ErrApprovalRequired):
		return "", true
	case errors.Is(err, agentErrors.ErrApprovalMismatch):
		return constants.WorkflowRejectApprovalMismatch, true
	case errors.As(err, &rejected):
		return constants.WorkflowRejectBeforeApprove, true
	case errors.As(err, &inputRejected):
		return constants.WorkflowRejectResumeInput, true
	}
	return "", false
}

// AsChildRunnable 供图作为子流程嵌套运行，例如经 tools.NewRunnableToolFactory 包成可续跑工具。
// 检查点与节点内的持久操作写在父操作日志的 Store 中，不使用 DurableConfig.Store：子流程必须与父流程共用
// 同一 Store，否则崩溃后子图从头重跑、已执行的节点再执行一次。读取子图状态时以该 Store 编译图后调用 Snapshot。
// 运行 ID 取当前执行键，须在声明 Resumable 的持久操作内调用（见 durable.CanWait），否则在执行任何节点前
// 返回 durable.ErrWaitNotResumable：其他操作不会重新进入，审批之后无法续跑。审批暂停返回 *durable.WaitError 而不是成功，
// 等待不会被当作结果交给上层。续跑时只解析 WithResume 编码的 ResumeRequest，裸审批键会被拒绝；首次运行时续跑输入被忽略。
// 完成时返回最终状态。
func (d *DurableGraph[T]) AsChildRunnable() *core.RunnableFunc[T, T] {
	return core.NewRunnableFunc(func(ctx context.Context, initial T) (T, error) {
		var zero T
		id := durable.ExecutionKey(ctx)
		if id == "" {
			return zero, agentErrors.Newf(agentErrors.CodeInvalidConfig, "child graph requires an execution key").WithComponent("workflow").WithOperation("as_child_runnable")
		}
		if !durable.CanWait(ctx) {
			return zero, fmt.Errorf("%w：子图须在可续跑的操作内运行，如 RunnableToolConfig.Resumable", agentErrors.ErrWaitNotResumable)
		}
		// CanWait 为真时 ctx 必有日志。
		store := durable.JournalStore(ctx)
		// 续跑输入只用于仍在等待的图；已越过审批后中断再进入时，宿主多半还带着同一输入。
		payload, ok := durable.ResumeInput(ctx)
		var current *GraphSnapshot[T]
		if ok {
			var err error
			current, err = d.snapshot(ctx, store, id)
			switch {
			case errors.Is(err, os.ErrNotExist):
				ok = false
			case err != nil:
				return zero, err
			case !current.Waiting:
				ok = false
			}
		}
		var approvalKey string
		var input json.RawMessage
		if ok {
			var req ResumeRequest
			if err := json.Unmarshal([]byte(payload), &req); err != nil || req.Key == "" {
				current, e := d.snapshot(ctx, store, id)
				if e != nil {
					return zero, e
				}
				if current.Waiting && ctx.Err() == nil {
					return zero, childWait(id, current, constants.WorkflowRejectResumeInput, nil)
				}
				return zero, &ResumeRejectedError{Err: agentErrors.Newf(agentErrors.CodeInvalidInput, "invalid human resume request").WithComponent("workflow").WithOperation("as_child_runnable")}
			}
			approvalKey, input = req.Key, req.Input
		}
		// 审批后节点进入中断等待时 Key 不变；带着已消费的审批键再来不是审批不符，叶子中断输入照常生效。
		if approvalKey != "" && current != nil && len(current.Interrupts) > 0 && approvalKey == current.Key {
			if input != nil {
				// 该键是节点意图键，不能回应中断；没有中断能推进时告知宿主改用 Interrupts[i].Key，而不是静默丢弃输入。
				ready, err := interruptReady(ctx, store, current.Interrupts)
				if err != nil {
					return zero, err
				}
				if !ready && ctx.Err() == nil {
					return zero, childWait(id, current, constants.WorkflowRejectApprovalMismatch, nil)
				}
			}
			approvalKey, input = "", nil
		}
		snapshot, err := d.run(ctx, store, id, initial, approvalKey, input, func(core.StreamChunk[*GraphSnapshot[T]]) error { return nil })
		// 审批暂停与未被采纳的审批报告为等待，避免上层把这次续跑记成失败而无法再续；其他错误照常失败。
		reason, waiting := approvalWait(err)
		if waiting && snapshot != nil && snapshot.Waiting && ctx.Err() == nil {
			return zero, childWait(id, snapshot, reason, err)
		}
		if err != nil {
			return zero, err
		}
		if !snapshot.Done {
			return zero, agentErrors.Newf(agentErrors.CodeAgentExecution, "child graph stopped before completion").WithComponent("workflow").WithOperation("as_child_runnable")
		}
		return snapshot.State, nil
	})
}

// childWait 报告子图等待；拒绝时 cause 作为进程内诊断随等待上传，暂停时不携带。
func childWait[T any](id string, snapshot *GraphSnapshot[T], reason string, cause error) error {
	approvalKey := snapshot.Key
	if len(snapshot.Interrupts) > 0 {
		// 中断等待时 Key 是节点意图键，不能用来回应，避免宿主误当作审批键。
		approvalKey = ""
	}
	continuation, err := json.Marshal(ChildContinuation{RunID: id, Node: snapshot.Node, ApprovalKey: approvalKey, Rejected: reason != "", Reason: reason, Interrupts: snapshot.Interrupts})
	if err != nil {
		return err
	}
	if reason == "" {
		cause = nil
	}
	return &durable.WaitError{Continuation: continuation, Cause: cause}
}
