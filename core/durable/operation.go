package durable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
)

type journalContext struct{}

type replayOnlyContext struct{}

// WithReplayOnly 限制派生上下文中的 Do 只能回放已完成记录，不调用 run 或 Reconcile。
// 宿主回调、授权逻辑及直接访问存储的代码仍须自行保证只读。
func WithReplayOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, replayOnlyContext{}, true)
}

// WithJournal binds child operations to a host-owned store. An execution key
// must also be present. Records contain outputs and hashes of inputs, not inputs.
func WithJournal(ctx context.Context, store Store) context.Context {
	return context.WithValue(ctx, journalContext{}, store)
}

// JournalStore 返回 ctx 绑定的日志 Store，没有时为 nil。嵌套的子流程用它把状态写在父日志的同一 Store 中。
func JournalStore(ctx context.Context) Store {
	s, _ := ctx.Value(journalContext{}).(Store)
	return s
}

func HasJournal(ctx context.Context) bool {
	s, _ := ctx.Value(journalContext{}).(Store)
	return s != nil && ExecutionKey(ctx) != ""
}

// ChildKey derives an unambiguous key for a named logical operation. IDs must
// be stable across restarts; a fresh model-generated ID is not sufficient.
func ChildKey(parent, id string) string {
	if parent == "" || id == "" {
		return ""
	}
	b, _ := json.Marshal([]string{parent, id})
	return digest(string(b))
}

type OperationPolicy struct {
	MaxAttempts int `json:"max_attempts"`
	// RetryFailed permits another attempt on a subsequent explicit invocation.
	// Do never loops or retries within a single invocation.
	// 只作用于结果已知的失败（普通失败、超时）；中断的记录须由 Reconcile 核对未生效或声明 RetryInterrupted 才重跑。
	RetryFailed bool `json:"retry_failed"`
	// Reconcile 在记录被中断（停在 pending 或因取消而失败，结果未知）时先于重跑调用，按派生执行键查询效果是否已生效：
	// found 为真时用 value 补记为 completed，不执行 run、不计尝试次数；返回错误时记录不变。
	Reconcile func(ctx context.Context) (value json.RawMessage, found bool, err error) `json:"-"`
	// Resumable 声明 run 的全部效果都记在派生执行键下，重新进入只会回放已完成的步骤：
	// waiting 记录总是重新进入且不计尝试次数，pending 记录在 MaxAttempts 内重新进入。
	// 只有声明它的操作能把 *WaitError 记为等待。
	Resumable bool `json:"resumable"`
	// RetryInterrupted 允许在 MaxAttempts 内重跑被中断的操作：记录停在 pending（进程崩溃）或因取消而失败（进程停止）。
	// 上次执行可能已经生效，调用方须接受重复的效果；普通失败和超时不算中断。
	RetryInterrupted bool `json:"retry_interrupted"`
}

type OperationRecord struct {
	ID          string          `json:"id"`
	Key         string          `json:"key"`
	InputHash   string          `json:"input_hash"`
	Status      string          `json:"status"` // pending, completed, failed, waiting
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	Value       json.RawMessage `json:"value"`
	FailureKind string          `json:"failure_kind,omitempty"`
	// Continuation 是 waiting 记录保存的等待项（[]*WaitError 的 JSON），只含定位信息。
	Continuation json.RawMessage `json:"continuation,omitempty"`
	// Reconciled 表示 completed 结果来自 Reconcile 核对，而非本次执行。
	Reconciled bool `json:"reconciled,omitzero"`
	Replayed   bool `json:"-"`
}

const operationVersion = "goagent-operation-v2"

type waitableContext struct{}

// CanWait 报告在 ctx 中返回 *WaitError 能否被续跑：ctx 所在的最内层持久操作带日志且声明了 Resumable。
// 会产生等待的子流程（如 DurableGraph.AsChildRunnable）据此在执行任何步骤之前失败。
func CanWait(ctx context.Context) bool {
	ok, _ := ctx.Value(waitableContext{}).(bool)
	return ok
}

type OperationError struct{ ID, Kind string }

func (e *OperationError) Error() string {
	return fmt.Sprintf("operation %q needs host attention (%s)", e.ID, e.Kind)
}
func (e *OperationError) Unwrap() error { return agentErrors.ErrOperationAttention }

// WaitStep locates one journaled operation on the path to a wait, outermost first.
type WaitStep struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// WaitError is a normal pause awaiting host continuation (e.g. an approval).
// It is neither a failure nor a result. Do records it as waiting and prefixes
// its own step, so the path leads from the outermost operation to the wait.
// 只有声明 Resumable 的操作能承载等待，其余操作把它记为失败并返回 ErrWaitNotResumable。
type WaitError struct {
	Path         []WaitStep      `json:"path,omitempty"`
	Continuation json.RawMessage `json:"continuation,omitempty"`
	// Cause 是本次等待的进程内诊断，如 validate 的 panic；不持久化，也不经 Unwrap 暴露，以免等待被当作失败。
	Cause error `json:"-"`
}

// ErrorCode 标识等待分类，不改变等待叶子或诊断原因的展开规则。
func (e *WaitError) ErrorCode() agentErrors.ErrorCode {
	if e == nil {
		return agentErrors.CodeInternal
	}
	return agentErrors.CodeWaiting
}

func (e *WaitError) Error() string {
	ids := make([]string, len(e.Path))
	for i, step := range e.Path {
		ids[i] = step.ID
	}
	return fmt.Sprintf("operation waiting for host continuation at %q", strings.Join(ids, " > "))
}

// Waits returns the waits in err when every failure in its tree is a wait;
// otherwise nil, because a joined real failure must not be recorded as a pause.
func Waits(err error) []*WaitError {
	var waits []*WaitError
	var walk func(error) bool
	walk = func(e error) bool {
		if w, ok := e.(*WaitError); ok {
			waits = append(waits, w)
			return true
		}
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			branches := u.Unwrap()
			for _, b := range branches {
				if b != nil && !walk(b) {
					return false
				}
			}
			return len(branches) > 0
		case interface{ Unwrap() error }:
			if inner := u.Unwrap(); inner != nil {
				return walk(inner)
			}
		}
		return false
	}
	if err == nil || !walk(err) {
		return nil
	}
	return waits
}

func decodeWaits(r *OperationRecord) ([]*WaitError, error) {
	var waits []*WaitError
	if json.Unmarshal(r.Continuation, &waits) != nil || len(waits) == 0 || slices.Contains(waits, nil) {
		return nil, agentErrors.Newf(agentErrors.CodeStateValidation, "invalid durable operation record").WithComponent("durable_operation").WithOperation("decode_waits")
	}
	return waits, nil
}

// storedWaits rebuilds the waits of a waiting record, prefixed with its own step.
func storedWaits(r *OperationRecord) error {
	waits, err := decodeWaits(r)
	if err != nil {
		return err
	}
	errs := make([]error, len(waits))
	for i, w := range waits {
		w.Path = append([]WaitStep{{ID: r.ID, Key: r.Key}}, w.Path...)
		errs[i] = w
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return errors.Join(errs...)
}

// OperationStorageID 返回操作在当前日志 Store 中的存储身份；不访问存储或改变执行状态。
func OperationStorageID(key string) string { return "goagent/operation/" + key }

// InspectOperation reads a child record under the same host store/parent key.
func InspectOperation(ctx context.Context, id string) (record *OperationRecord, err error) {
	store, _ := ctx.Value(journalContext{}).(Store)
	key := ChildKey(ExecutionKey(ctx), id)
	if store == nil || key == "" {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "operation journal, parent key and ID required").WithComponent("durable_operation").WithOperation("inspect_operation")
	}
	unlock, err := store.Lock(ctx, OperationStorageID(key))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	return loadOperation(ctx, store, key, id)
}

func loadOperation(ctx context.Context, store Store, key, id string) (*OperationRecord, error) {
	snapshot, err := store.Load(ctx, OperationStorageID(key))
	if err != nil {
		return nil, err
	}
	return DecodeOperationRecord(snapshot, key, id)
}

// DecodeOperationRecord 只解码与验证当前版本的操作快照，不访问 Store、不执行业务、不修改输入。
// key 和 id 是调用方预期的操作身份；迁移工具与在线执行共用同一数据契约。
func DecodeOperationRecord(snapshot *Snapshot, key, id string) (*OperationRecord, error) {
	var r OperationRecord
	if key == "" || id == "" || snapshot == nil || snapshot.Version != operationVersion || snapshot.RunID != OperationStorageID(key) || json.Unmarshal(snapshot.Data, &r) != nil ||
		r.ID != id || r.Key != key || r.InputHash == "" || r.Attempts < 1 || r.MaxAttempts < 1 || r.Attempts > r.MaxAttempts || !json.Valid(r.Value) ||
		(r.Status != "pending" && r.Status != "completed" && r.Status != "failed" && r.Status != "waiting") {
		return nil, agentErrors.Newf(agentErrors.CodeStateValidation, "invalid durable operation record").WithComponent("durable_operation").WithOperation("load_operation")
	}
	// 等待状态与续跑定位信息必须一致，核对结果只能用于已完成记录。
	if (r.Status == "waiting") != (len(r.Continuation) > 0) || (r.Reconciled && r.Status != "completed") {
		return nil, agentErrors.Newf(agentErrors.CodeStateValidation, "invalid durable operation record").WithComponent("durable_operation").WithOperation("load_operation")
	}
	if r.Status == "waiting" {
		if _, err := decodeWaits(&r); err != nil {
			return nil, err
		}
	}
	return &r, nil
}

// Do journals a named child operation, caching successes and retaining failed
// outputs (e.g. usage on invalid model JSON). Stored errors contain categories
// only, to avoid persisting credentials from third-party error strings.
// Effects around a process crash still need target-side idempotency by child key.
func Do(ctx context.Context, id string, input json.RawMessage, policy OperationPolicy, run func(context.Context) (json.RawMessage, error)) (record *OperationRecord, err error) {
	if id == "" || !json.Valid(input) || run == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "operation requires ID, JSON intent and function").WithComponent("durable_operation").WithOperation("do")
	}
	if policy.MaxAttempts == 0 {
		policy.MaxAttempts = 1
	}
	if policy.MaxAttempts < 1 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "positive operation attempt budget required").WithComponent("durable_operation").WithOperation("do")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	key := ChildKey(ExecutionKey(ctx), id)
	childCtx := ctx
	if key != "" {
		childCtx = WithExecutionKey(ctx, key)
	}
	store, _ := ctx.Value(journalContext{}).(Store)
	replayOnly, _ := ctx.Value(replayOnlyContext{}).(bool)
	childCtx = context.WithValue(childCtx, waitableContext{}, policy.Resumable && store != nil && key != "")
	childCtx = context.WithValue(childCtx, interruptHostContext{}, false)
	if store == nil || key == "" {
		if replayOnly {
			return nil, agentErrors.ErrReplayOnly
		}
		value, e := run(childCtx)
		return &OperationRecord{ID: id, Key: key, Value: value, Attempts: 1}, e
	}
	storageID := OperationStorageID(key)
	unlock, err := store.Lock(ctx, storageID)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	inputHash := digest(string(input))
	record, err = loadOperation(ctx, store, key, id)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if replayOnly && (err != nil || record.Status != "completed") {
		return nil, agentErrors.ErrReplayOnly
	}
	if err == nil {
		if record.InputHash != inputHash || record.MaxAttempts != policy.MaxAttempts {
			return record, agentErrors.Newf(agentErrors.CodeConflict, "operation intent or attempt policy changed").WithComponent("durable_operation").WithOperation("do")
		}
		if record.Status == "completed" {
			record.Replayed = true
			return record, nil
		}
		absent := false
		interrupted := record.Status == "pending" || (record.Status == "failed" && record.FailureKind == "canceled")
		if interrupted && policy.Reconcile != nil {
			value, found, e := reconcileOperation(childCtx, policy.Reconcile)
			if e != nil {
				return record, e
			}
			if found {
				record.Status, record.FailureKind, record.Value, record.Reconciled, record.Replayed = "completed", "", value, true, true
				return record, saveOperation(ctx, store, storageID, record)
			}
			absent = true
		}
		resume := policy.Resumable && (record.Status == "waiting" || (record.Status == "pending" && record.Attempts < policy.MaxAttempts))
		if record.Status == "waiting" && !resume {
			record.Replayed = true
			return record, &OperationError{ID: id, Kind: "not_resumable"}
		}
		resume = resume || (policy.RetryInterrupted && interrupted && record.Attempts < policy.MaxAttempts)
		retryFailed := policy.RetryFailed && (!interrupted || absent)
		if !resume && (!retryFailed || record.Attempts >= policy.MaxAttempts) {
			record.Replayed = true
			return record, &OperationError{ID: id, Kind: record.FailureKind}
		}
	} else {
		record = &OperationRecord{ID: id, Key: key, InputHash: inputHash, MaxAttempts: policy.MaxAttempts}
	}
	// 从等待中续跑不是新的尝试。
	if record.Status != "waiting" {
		record.Attempts++
	}
	record.Status, record.FailureKind, record.Value, record.Continuation = "pending", "ambiguous", json.RawMessage(`null`), nil
	save := func(saveCtx context.Context) error { return saveOperation(saveCtx, store, storageID, record) }
	if err = save(ctx); err != nil {
		return record, err
	}
	value, callErr := invokeOperation(context.WithValue(childCtx, attemptContext{}, record.Attempts), run)
	if callErr == nil {
		callErr = ctx.Err()
	}
	if len(value) == 0 {
		value = json.RawMessage(`null`)
	}
	if !json.Valid(value) {
		value = json.RawMessage(`null`)
		callErr = errors.Join(callErr, agentErrors.Newf(agentErrors.CodeInvalidOutput, "operation output is not JSON").WithComponent("durable_operation").WithOperation("do"))
	}
	record.Value, record.Status, record.FailureKind = value, "completed", ""
	if waits := Waits(callErr); waits != nil && !policy.Resumable {
		record.Status, record.FailureKind = "failed", "not_resumable"
		callErr = fmt.Errorf("%w：操作 %q 收到 %v", agentErrors.ErrWaitNotResumable, id, callErr)
	} else if waits != nil {
		record.Status = "waiting"
		if record.Continuation, err = json.Marshal(waits); err != nil {
			record.Status, record.FailureKind, record.Continuation = "failed", "execution", nil
			callErr = errors.Join(callErr, err)
		} else {
			callErr = storedWaits(record)
			// Cause 不持久化，重建的等待与 waits 同序，从本次执行带回。
			for i, w := range Waits(callErr) {
				if i < len(waits) {
					w.Cause = waits[i].Cause
				}
			}
		}
	} else if callErr != nil {
		record.Status, record.FailureKind = "failed", "execution"
		// 取消后供应方可能只返回“连接已关闭”，错误链里没有 context.Canceled。
		if errors.Is(callErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			record.FailureKind = "canceled"
		}
		if errors.Is(callErr, context.DeadlineExceeded) {
			record.FailureKind = "deadline"
		}
	}
	// Persist cancellation/failure evidence with a bounded independent context.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return record, errors.Join(callErr, save(saveCtx))
}

type attemptContext struct{}

// OperationAttempt 返回 ctx 所在持久操作当前是第几次尝试（从 1 起）；不在带日志的操作内时返回 0。
func OperationAttempt(ctx context.Context) int {
	n, _ := ctx.Value(attemptContext{}).(int)
	return n
}

func saveOperation(ctx context.Context, store Store, storageID string, record *OperationRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return store.Save(ctx, storageID, &Snapshot{RunID: storageID, Version: operationVersion, Data: data})
}

func reconcileOperation(ctx context.Context, reconcile func(context.Context) (json.RawMessage, bool, error)) (value json.RawMessage, found bool, err error) {
	defer func() {
		if v := recover(); v != nil {
			value, found, err = nil, false, agentErrors.NewPanicError("持久操作的核对", v)
		}
	}()
	value, found, err = reconcile(ctx)
	if err == nil && found {
		if len(value) == 0 {
			value = json.RawMessage(`null`)
		}
		if !json.Valid(value) {
			return nil, false, agentErrors.Newf(agentErrors.CodeInvalidOutput, "reconciled output is not JSON").WithComponent("durable_operation").WithOperation("reconcile_operation")
		}
	}
	return value, found, err
}

func invokeOperation(ctx context.Context, run func(context.Context) (json.RawMessage, error)) (value json.RawMessage, err error) {
	defer func() {
		if v := recover(); v != nil {
			value, err = nil, agentErrors.NewPanicError("持久操作", v)
		}
	}()
	return run(ctx)
}
