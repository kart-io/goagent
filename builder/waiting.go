package builder

import (
	"errors"
	"fmt"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

// WaitingError lists all waits at the pause point. Committed model turns and
// completed tool calls are all kept in the journal. After the host provides
// resume input for each wait type, calling again with the same execution key
// continues: leaf interrupts use durable.WithResume, and subgraph approvals use
// workflow.WithResume with a structured request; the operation key is the Key
// of the last item in the wait path. It unwraps into the individual
// *durable.WaitError values and is recorded as a wait when nested within an
// outer durable operation.
type WaitingError struct {
	Waits []*durable.WaitError
}

// ErrorCode identifies the wait category; it does not change the unwrapping
// rules for wait leaves or diagnostic causes.
func (e *WaitingError) ErrorCode() agentErrors.ErrorCode {
	if e == nil {
		return agentErrors.CodeInternal
	}
	return agentErrors.CodeWaiting
}

func (e *WaitingError) Error() string {
	return fmt.Sprintf("%v: %d pending wait(s)", agentErrors.ErrAgentWaiting, len(e.Waits))
}

func (e *WaitingError) Is(target error) bool { return target == agentErrors.ErrAgentWaiting }

func (e *WaitingError) Unwrap() []error {
	errs := make([]error, len(e.Waits))
	for i, w := range e.Waits {
		errs[i] = w
	}
	return errs
}

// parallelFailure 汇总同一轮并发调用的失败，按以下顺序返回：
//   - 按模型顺序的第一个失败需要宿主处理（ErrOperationAttention）且这类失败不止一个时，返回 errors.Join
//     合并的全部这类失败（无论有没有等待），宿主一次核对覆盖整组未决操作；
//   - 没有等待时返回 nil，交由原流程处理（原流程返回第一个失败）；
//   - 有等待也有真正的失败时返回第一个失败，不让等待掩盖它；
//   - 全部是等待时返回包含全部等待的 WaitingError。
func parallelFailure(results []toolCallResult) error {
	var waits []*durable.WaitError
	var failure error
	var attention []error
	for _, r := range results {
		if r.err == nil {
			continue
		}
		if w := durable.Waits(r.err); w != nil {
			waits = append(waits, w...)
			continue
		}
		if failure == nil {
			failure = r.err
		}
		if errors.Is(r.err, agentErrors.ErrOperationAttention) {
			attention = append(attention, r.err)
		}
	}
	if len(attention) > 1 && errors.Is(failure, agentErrors.ErrOperationAttention) {
		return errors.Join(attention...)
	}
	switch {
	case len(waits) == 0:
		return nil
	case failure != nil:
		return failure
	}
	return &WaitingError{Waits: waits}
}
