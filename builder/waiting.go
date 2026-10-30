package builder

import (
	"errors"
	"fmt"

	"github.com/kart-io/goagent/core/durable"
)

// ErrWaiting 标识本次执行因子流程等待宿主续跑而暂停；它既不是失败，也没有答案。
var ErrWaiting = errors.New("agent is waiting for host continuation")

// WaitingError 列出暂停处的全部等待。已提交的模型回合与已完成的工具调用都保留在日志中；
// 宿主提供续跑输入（durable.WithResume，键取等待路径最后一项的 Key）后，用同一执行键再次调用即可继续。
// 它展开为各个 *durable.WaitError，嵌套在上层持久操作中时会被记录为等待。
type WaitingError struct {
	Waits []*durable.WaitError
}

func (e *WaitingError) Error() string {
	return fmt.Sprintf("%v: %d pending wait(s)", ErrWaiting, len(e.Waits))
}

func (e *WaitingError) Is(target error) bool { return target == ErrWaiting }

func (e *WaitingError) Unwrap() []error {
	errs := make([]error, len(e.Waits))
	for i, w := range e.Waits {
		errs[i] = w
	}
	return errs
}

// parallelFailure 只处理同一轮并发调用中出现等待的情况：同时有真正的失败时按模型顺序返回第一个失败，
// 不让等待掩盖它；全部是等待时返回包含全部等待的 WaitingError。没有等待时返回 nil，交由原流程处理。
func parallelFailure(results []toolCallResult) error {
	var waits []*durable.WaitError
	var failure error
	for _, r := range results {
		if r.err == nil {
			continue
		}
		if w := durable.Waits(r.err); w != nil {
			waits = append(waits, w...)
		} else if failure == nil {
			failure = r.err
		}
	}
	switch {
	case len(waits) == 0:
		return nil
	case failure != nil:
		return failure
	}
	return &WaitingError{Waits: waits}
}
