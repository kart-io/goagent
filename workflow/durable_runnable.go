package workflow

import (
	"context"

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
func (d *DurableGraph[T]) AsRunnable() *core.StreamRunnable[RunRequest[T], *GraphSnapshot[T]] {
	return core.NewStreamRunnable(core.StreamModeSteps, func(ctx context.Context, req RunRequest[T], emit func(core.StreamChunk[*GraphSnapshot[T]]) error) error {
		snapshot, err := d.run(ctx, req.RunID, req.Initial, req.ApprovalKey, func(event *core.StreamEvent) error {
			return emit(core.StreamChunk[*GraphSnapshot[T]]{Event: event})
		})
		// Only the actual durable wait is a normal pause. Joined unlock errors or
		// observer errors must not be converted to a successful invocation.
		if err != nil && (err != durable.ErrApprovalRequired || snapshot == nil || !snapshot.Waiting) {
			return err
		}
		return emit(core.StreamChunk[*GraphSnapshot[T]]{Done: true, Data: snapshot})
	})
}
