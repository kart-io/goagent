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

// ChildContinuation is the location information that a nested graph run puts
// into durable.WaitError when paused for approval or waiting on an interrupt.
// For an approval pause, provide the approval with workflow.WithResume(ctx,
// <execution key of the wait>, ResumeRequest{Key: ApprovalKey}); when waiting
// on an interrupt, ApprovalKey is empty and the response must use
// Interrupts[i].Key (the innermost interrupt key).
type ChildContinuation struct {
	RunID       string `json:"run_id"`
	Node        string `json:"node"`
	ApprovalKey string `json:"approval_key"`
	// Rejected reports that the supplied approval was not accepted; the graph state
	// is unchanged and still waiting. Reason is the cause category:
	// constants.WorkflowRejectApprovalMismatch (including supplying input with the
	// intent key while waiting on an interrupt),
	// constants.WorkflowRejectBeforeApprove, or
	// constants.WorkflowRejectResumeInput. Storage failures, panics, and the like
	// are not rejections and are returned as failures as usual.
	Rejected bool   `json:"rejected,omitzero"`
	Reason   string `json:"reason,omitempty"`
	// Interrupts are the pending interrupts of the subgraph (including deeper
	// levels), with Key being the innermost interrupt key. They can be answered
	// directly with durable.WithResume(ctx, Key, input), or wrapped for this
	// subgraph via WithResume.
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

// AsChildRunnable lets the graph run nested as a subflow, for example wrapped
// as a resumable tool via tools.NewRunnableToolFactory. Checkpoints and durable
// operations inside nodes are written to the Store of the parent operation
// journal, not DurableConfig.Store: the subflow must share the same Store as
// the parent, otherwise after a crash the subgraph reruns from scratch and
// already executed nodes run again. To read the subgraph state, compile the
// graph with that Store and call Snapshot. The run ID is the current execution
// key, and it must be called within a durable operation declared Resumable
// (see durable.CanWait); otherwise it returns durable.ErrWaitNotResumable
// before executing any node, because other operations are not re-entered and
// cannot resume after approval. An approval pause returns *durable.WaitError
// instead of success, so the wait is not passed upward as a result. On resume,
// only a ResumeRequest encoded by WithResume is parsed and a bare approval key
// is rejected; on the first run, resume input is ignored. On completion it
// returns the final state.
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
