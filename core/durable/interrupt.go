package durable

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
)

const (
	interruptPrefix = "interrupt/"
)

// InterruptContinuation is what AwaitInput places in WaitError.Continuation
// while waiting. Info is exposed to the host through snapshots and must not
// contain credentials.
type InterruptContinuation struct {
	Kind     string          `json:"kind"`
	ID       string          `json:"id"`
	Info     json.RawMessage `json:"info"`
	Rejected bool            `json:"rejected,omitzero"`
	Reason   string          `json:"reason,omitempty"`
}

type interruptHostContext struct{}

// AllowInterrupt marks ctx as able to host an AwaitInput wait directly, such as
// a workflow node. The host must treat a received wait as a pause and re-enter
// on resume; Do clears this mark for child operations.
func AllowInterrupt(ctx context.Context) context.Context {
	return context.WithValue(ctx, interruptHostContext{}, true)
}

func interruptHost(ctx context.Context) bool {
	ok, _ := ctx.Value(interruptHostContext{}).(bool)
	return ok
}

// AwaitInput pauses the current durable operation and waits for the host to
// respond. The first successfully persisted input is fixed, and every later
// replay returns it; this does not guarantee that business side effects before
// the call run only once. id must be unique under the same parent execution
// key; include a sequence number in loops. If the input is not valid JSON or
// validate fails, it keeps waiting and marks Rejected in the Continuation. JSON
// null is a valid explicit input; use validate to reject it when a non-null
// input is required. A panic in validate is treated as a rejection (reason
// validate_panic), with the panic value and stack trace in WaitError.Cause. The
// error text from validate is persisted and exposed through Snapshot and
// ResumeRejectedError, so it must not contain credentials or other sensitive
// information.
func AwaitInput(ctx context.Context, id string, info json.RawMessage, validate func(json.RawMessage) error) (json.RawMessage, error) {
	if id == "" || !json.Valid(info) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "中断需要 ID 与 JSON 格式的 info").WithComponent("durable_interrupt").WithOperation("await_input")
	}
	if !HasJournal(ctx) {
		return nil, agentErrors.ErrInterruptNotDurable
	}
	if !CanWait(ctx) && !interruptHost(ctx) {
		return nil, fmt.Errorf("%w：中断 %q 须在可续跑的操作或工作流节点内调用", agentErrors.ErrWaitNotResumable, id)
	}
	policy := OperationPolicy{Resumable: true, MaxAttempts: constants.DefaultInterruptAttempts}
	record, err := Do(ctx, interruptPrefix+id, info, policy, func(ctx context.Context) (json.RawMessage, error) {
		wait := func(rejected bool, reason string, cause error) error {
			data, e := json.Marshal(InterruptContinuation{Kind: constants.ContinuationKindInterrupt, ID: id, Info: info, Rejected: rejected, Reason: reason})
			if e != nil {
				return e
			}
			return &WaitError{Continuation: data, Cause: cause}
		}
		raw, ok := ResumeInput(ctx)
		if !ok {
			return nil, wait(false, "", nil)
		}
		input := json.RawMessage(raw)
		if !json.Valid(input) {
			return nil, wait(true, constants.InterruptReasonInvalidJSON, nil)
		}
		if validate != nil {
			if reason, rejected, cause := checkInput(validate, input); rejected {
				return nil, wait(true, reason, cause)
			}
		}
		return append(json.RawMessage(nil), input...), nil
	})
	if err != nil {
		return nil, err
	}
	return record.Value, nil
}

// checkInput 调用 validate；panic 也算拒绝，否则 Do 会把中断记成失败，再也无法恢复。
// panic 的值与调用栈只作为 cause 留在进程内，不进检查点。
func checkInput(validate func(json.RawMessage) error, input json.RawMessage) (reason string, rejected bool, cause error) {
	defer func() {
		if v := recover(); v != nil {
			reason, rejected, cause = constants.InterruptReasonValidatePanic, true, agentErrors.NewPanicError("中断输入校验", v)
		}
	}()
	if err := validate(input); err != nil {
		return rejectReason(err), true, nil
	}
	return "", false, nil
}

// rejectReason 截断校验错误文本；它会被持久化并对外暴露，内容由 validate 的实现者负责。
func rejectReason(err error) string {
	r := []rune(err.Error())
	if len(r) == 0 {
		return constants.InterruptReasonRejected
	}
	if len(r) > constants.InterruptMaxReasonRunes {
		r = r[:constants.InterruptMaxReasonRunes]
	}
	return string(r)
}

// InterruptAnswer checks, read-only, whether the interrupt at key has been
// answered and returns the fixed input if so. key is the Key of the last
// element of WaitError.Path, and id is the AwaitInput id.
func InterruptAnswer(ctx context.Context, store Store, key, id string) (answer json.RawMessage, ok bool, err error) {
	if store == nil || key == "" || id == "" {
		return nil, false, agentErrors.Newf(agentErrors.CodeInvalidInput, "查询中断需要 Store、键和 ID").WithComponent("durable_interrupt").WithOperation("interrupt_answer")
	}
	storageID := OperationStorageID(key)
	unlock, err := store.Lock(ctx, storageID)
	if err != nil {
		return nil, false, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	record, err := loadOperation(ctx, store, key, interruptPrefix+id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || record.Status != "completed" {
		return nil, false, err
	}
	return record.Value, true, nil
}
