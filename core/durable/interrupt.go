package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ContinuationKindInterrupt 标识 WaitError.Continuation 是 InterruptContinuation。
const ContinuationKindInterrupt = "interrupt"

// ReasonInvalidJSON 是输入不是合法 JSON 时的拒绝原因。
const ReasonInvalidJSON = "invalid_json"

// ReasonValidatePanic 是 validate 发生 panic 时的拒绝原因。
const ReasonValidatePanic = "validate_panic"

// ReasonRejected 是 validate 返回空错误文本时的拒绝原因。
const ReasonRejected = "rejected"

const (
	interruptPrefix = "interrupt/"
	// interruptAttempts 允许在写 pending 与写 waiting 之间崩溃后重入；中断本身没有副作用。
	interruptAttempts = 3
	maxReasonRunes    = 200
)

// ErrInterruptNotDurable 表示 ctx 没有持久日志，暂停后无法恢复。
var ErrInterruptNotDurable = errors.New("暂停需要持久日志")

// InterruptContinuation 是 AwaitInput 等待时放进 WaitError.Continuation 的内容。
// Info 会经快照暴露给宿主，不得包含凭证。
type InterruptContinuation struct {
	Kind     string          `json:"kind"`
	ID       string          `json:"id"`
	Info     json.RawMessage `json:"info"`
	Rejected bool            `json:"rejected,omitempty"`
	Reason   string          `json:"reason,omitempty"`
}

type interruptHostContext struct{}

// AllowInterrupt 标记 ctx 可以直接承载 AwaitInput 的等待，如工作流节点。承载方必须把收到的
// 等待当作暂停，并在恢复时重新进入；Do 会为子操作清除此标记。
func AllowInterrupt(ctx context.Context) context.Context {
	return context.WithValue(ctx, interruptHostContext{}, true)
}

func interruptHost(ctx context.Context) bool {
	ok, _ := ctx.Value(interruptHostContext{}).(bool)
	return ok
}

// AwaitInput 在当前持久操作中暂停，等待宿主回应。首次成功持久化的输入就此固定，之后的重放都返回它；
// 这不保证调用前的业务副作用只执行一次。id 在同一父执行键下必须唯一，循环中须带序号。
// 输入不是合法 JSON 或 validate 失败时保持等待，并在 Continuation 中标记 Rejected。
// JSON null 是合法的显式输入；需要非空输入时由 validate 拒绝。validate 的 panic 按拒绝处理（原因 validate_panic）。
// validate 的错误文本会持久化并经 Snapshot 与 ResumeRejectedError 暴露，不得包含凭证等敏感信息。
func AwaitInput(ctx context.Context, id string, info json.RawMessage, validate func(json.RawMessage) error) (json.RawMessage, error) {
	if id == "" || !json.Valid(info) {
		return nil, fmt.Errorf("中断需要 ID 与 JSON 格式的 info")
	}
	if !HasJournal(ctx) {
		return nil, ErrInterruptNotDurable
	}
	if !CanWait(ctx) && !interruptHost(ctx) {
		return nil, fmt.Errorf("%w：中断 %q 须在可续跑的操作或工作流节点内调用", ErrWaitNotResumable, id)
	}
	policy := OperationPolicy{Resumable: true, MaxAttempts: interruptAttempts}
	record, err := Do(ctx, interruptPrefix+id, info, policy, func(ctx context.Context) (json.RawMessage, error) {
		wait := func(rejected bool, reason string) error {
			data, e := json.Marshal(InterruptContinuation{Kind: ContinuationKindInterrupt, ID: id, Info: info, Rejected: rejected, Reason: reason})
			if e != nil {
				return e
			}
			return &WaitError{Continuation: data}
		}
		raw, ok := ResumeInput(ctx)
		if !ok {
			return nil, wait(false, "")
		}
		input := json.RawMessage(raw)
		if !json.Valid(input) {
			return nil, wait(true, ReasonInvalidJSON)
		}
		if validate != nil {
			if reason, rejected := checkInput(validate, input); rejected {
				return nil, wait(true, reason)
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
func checkInput(validate func(json.RawMessage) error, input json.RawMessage) (reason string, rejected bool) {
	defer func() {
		if recover() != nil {
			reason, rejected = ReasonValidatePanic, true
		}
	}()
	if err := validate(input); err != nil {
		return rejectReason(err), true
	}
	return "", false
}

// rejectReason 截断校验错误文本；它会被持久化并对外暴露，内容由 validate 的实现者负责。
func rejectReason(err error) string {
	r := []rune(err.Error())
	if len(r) == 0 {
		return ReasonRejected
	}
	if len(r) > maxReasonRunes {
		r = r[:maxReasonRunes]
	}
	return string(r)
}

// InterruptAnswer 只读查询 key 处的中断是否已回应，已回应时返回固定的输入。
// key 是 WaitError.Path 最后一项的 Key，id 是 AwaitInput 的 id。
func InterruptAnswer(ctx context.Context, store Store, key, id string) (answer json.RawMessage, ok bool, err error) {
	if store == nil || key == "" || id == "" {
		return nil, false, fmt.Errorf("查询中断需要 Store、键和 ID")
	}
	storageID := operationStorageID(key)
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
