package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// ResumeRequest responds to one persisted approval/input gate. Key is the
// waiting GraphSnapshot.Key, not an authorization credential. A nil Input means
// no payload; JSON null is an explicit payload. A configured resume handler
// receives either and must reject missing input when the gate requires it.
type ResumeRequest struct {
	Key   string          `json:"key"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ResumeRejectedError means human input was not accepted and the node did not
// advance. The host may correct the input and retry the same waiting key. When a
// dynamic interrupt's validator rejects the input, only that interrupt's
// Rejected/Reason in the checkpoint are updated.
// Storage errors, cancellation and panics are not input rejection.
type ResumeRejectedError struct{ Err error }

func (e *ResumeRejectedError) Error() string { return "resume input rejected: " + e.Err.Error() }
func (e *ResumeRejectedError) Unwrap() error { return e.Err }

// WithResumeHandler validates human input and produces the next business state
// at an ApprovalNodes gate. It runs under the run lock on an isolated snapshot;
// the returned state and approval are saved atomically before executing the node.
// The handler must be side-effect-free: failed saves can cause it to run again.
// It must validate allowed fields and decisions; the host authenticates callers.
// It runs for every approval, including legacy approval-only calls with nil input.
// WithBeforeApprove then checks the proposed state before it is saved.
// Bump DurableConfig.Version when changing the handler's semantics.
func WithResumeHandler[T any](fn func(context.Context, string, *GraphSnapshot[T], json.RawMessage) (T, error)) DurableOption[T] {
	return func(d *DurableGraph[T]) { d.resumeHandler = fn }
}

// Resume applies an authorized response to a waiting checkpoint and continues
// execution. Like Run, reaching another gate returns durable.ErrApprovalRequired.
// Once accepted, the old key is no longer valid. After an uncertain result, read
// Snapshot: if it is no longer waiting at that key, use Run without an approval
// key to recover; do not apply human edits again. External effects still require
// business idempotency. Resume cannot create a new run or edit an executing node.
func (d *DurableGraph[T]) Resume(ctx context.Context, id string, req ResumeRequest) (*GraphSnapshot[T], error) {
	if d == nil {
		return nil, fmt.Errorf("compiled durable graph is required")
	}
	if req.Key == "" {
		return nil, &ResumeRejectedError{Err: fmt.Errorf("waiting checkpoint key is required")}
	}
	var initial T
	return d.run(ctx, d.store, id, initial, req.Key, req.Input, func(core.StreamChunk[*GraphSnapshot[T]]) error { return nil })
}

// WithResume supplies structured human input to an AsChildRunnable wait inside
// a resumable tool/operation. operationKey is the last WaitError.Path key;
// req.Key is ChildContinuation.ApprovalKey. They identify different boundaries.
// req.Key 也可以是 ChildContinuation.Interrupts 中的中断键。
// Legacy approval-only durable.WithResume calls remain supported.
func WithResume(ctx context.Context, operationKey string, req ResumeRequest) (context.Context, error) {
	if operationKey == "" || req.Key == "" {
		return nil, &ResumeRejectedError{Err: fmt.Errorf("operation and checkpoint keys are required")}
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, &ResumeRejectedError{Err: err}
	}
	return durable.WithResume(ctx, operationKey, string(data)), nil
}

func (d *DurableGraph[T]) applyResume(ctx context.Context, id string, cp *durable.Snapshot, input json.RawMessage) (next durable.Snapshot, state graphState[T], err error) {
	defer func() {
		if r := recover(); r != nil {
			err = agentErrors.NewPanicError("workflow resume handler", r)
		}
	}()
	if d.resumeHandler == nil {
		return next, state, &ResumeRejectedError{Err: fmt.Errorf("human input requires WithResumeHandler")}
	}
	state, err = d.decodeCheckpoint(id, cp)
	if err != nil {
		return next, state, err
	}
	out, err := d.resumeHandler(ctx, id, publicGraphSnapshot(cp, state), append(json.RawMessage(nil), input...))
	if ctx.Err() != nil {
		return next, state, ctx.Err()
	}
	if err != nil {
		return next, state, &ResumeRejectedError{Err: err}
	}
	state.State = out
	next = *cp
	next.Data, err = json.Marshal(state)
	return next, state, err
}
