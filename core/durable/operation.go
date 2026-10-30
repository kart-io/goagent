package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

type journalContext struct{}

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
	MaxAttempts int
	// RetryFailed permits another attempt on a subsequent explicit invocation.
	// Do never loops or retries within a single invocation.
	RetryFailed bool
	// Reconcile 在记录停在 pending（上次执行中断、结果未知）时先于重跑调用，按派生执行键查询效果是否已生效：
	// found 为真时用 value 补记为 completed，不执行 run、不计尝试次数；返回错误时记录不变。
	Reconcile func(ctx context.Context) (value json.RawMessage, found bool, err error)
	// Resumable 声明 run 的全部效果都记在派生执行键下，重新进入只会回放已完成的步骤：
	// waiting 记录总是重新进入且不计尝试次数，pending 记录在 MaxAttempts 内重新进入。
	// 只有声明它的操作能把 *WaitError 记为等待。
	Resumable bool
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
	Reconciled bool `json:"reconciled,omitempty"`
	Replayed   bool `json:"-"`
}

const (
	operationVersionV1 = "goagent-operation-v1"
	operationVersion   = "goagent-operation-v2"
)

var ErrOperationAttention = errors.New("durable operation needs host attention")

// ErrWaitNotResumable 表示没有声明 Resumable 的持久操作收到了等待：它以后不会重新进入，等待无法续跑，
// 因此记为失败（FailureKind 为 not_resumable）。承载等待的操作（如包住子图的工具）须声明 Resumable。
var ErrWaitNotResumable = errors.New("等待只能由声明 Resumable 的持久操作承载")

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
func (e *OperationError) Unwrap() error { return ErrOperationAttention }

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
		return nil, fmt.Errorf("invalid durable operation record")
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

func operationStorageID(key string) string { return "goagent/operation/" + key }

// InspectOperation reads a child record under the same host store/parent key.
func InspectOperation(ctx context.Context, id string) (record *OperationRecord, err error) {
	store, _ := ctx.Value(journalContext{}).(Store)
	key := ChildKey(ExecutionKey(ctx), id)
	if store == nil || key == "" {
		return nil, fmt.Errorf("operation journal, parent key and ID required")
	}
	unlock, err := store.Lock(ctx, operationStorageID(key))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	return loadOperation(ctx, store, key, id)
}

func loadOperation(ctx context.Context, store Store, key, id string) (*OperationRecord, error) {
	snapshot, err := store.Load(ctx, operationStorageID(key))
	if err != nil {
		return nil, err
	}
	var r OperationRecord
	if snapshot == nil || (snapshot.Version != operationVersion && snapshot.Version != operationVersionV1) || snapshot.RunID != operationStorageID(key) || json.Unmarshal(snapshot.Data, &r) != nil ||
		r.ID != id || r.Key != key || r.InputHash == "" || r.Attempts < 1 || r.MaxAttempts < 1 || r.Attempts > r.MaxAttempts || !json.Valid(r.Value) ||
		(r.Status != "pending" && r.Status != "completed" && r.Status != "failed" && r.Status != "waiting") {
		return nil, fmt.Errorf("invalid durable operation record")
	}
	// v1 记录没有等待与核对语义，不能被重新解释。
	if (r.Status == "waiting") != (len(r.Continuation) > 0) || (r.Reconciled && r.Status != "completed") ||
		(snapshot.Version == operationVersionV1 && (r.Status == "waiting" || r.Reconciled)) {
		return nil, fmt.Errorf("invalid durable operation record")
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
		return nil, fmt.Errorf("operation requires ID, JSON intent and function")
	}
	if policy.MaxAttempts == 0 {
		policy.MaxAttempts = 1
	}
	if policy.MaxAttempts < 1 {
		return nil, fmt.Errorf("positive operation attempt budget required")
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
	childCtx = context.WithValue(childCtx, waitableContext{}, policy.Resumable && store != nil && key != "")
	if store == nil || key == "" {
		value, e := run(childCtx)
		return &OperationRecord{ID: id, Key: key, Value: value, Attempts: 1}, e
	}
	storageID := operationStorageID(key)
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
	if err == nil {
		if record.InputHash != inputHash || record.MaxAttempts != policy.MaxAttempts {
			return record, fmt.Errorf("operation intent or attempt policy changed")
		}
		if record.Status == "completed" {
			record.Replayed = true
			return record, nil
		}
		if record.Status == "pending" && policy.Reconcile != nil {
			value, found, e := reconcileOperation(childCtx, policy.Reconcile)
			if e != nil {
				return record, e
			}
			if found {
				record.Status, record.FailureKind, record.Value, record.Reconciled, record.Replayed = "completed", "", value, true, true
				return record, saveOperation(ctx, store, storageID, record)
			}
		}
		resume := policy.Resumable && (record.Status == "waiting" || (record.Status == "pending" && record.Attempts < policy.MaxAttempts))
		if record.Status == "waiting" && !resume {
			record.Replayed = true
			return record, &OperationError{ID: id, Kind: "not_resumable"}
		}
		if !resume && (!policy.RetryFailed || record.Attempts >= policy.MaxAttempts) {
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
	value, callErr := invokeOperation(childCtx, run)
	if callErr == nil {
		callErr = ctx.Err()
	}
	if len(value) == 0 {
		value = json.RawMessage(`null`)
	}
	if !json.Valid(value) {
		value = json.RawMessage(`null`)
		callErr = errors.Join(callErr, fmt.Errorf("operation output is not JSON"))
	}
	record.Value, record.Status, record.FailureKind = value, "completed", ""
	if waits := Waits(callErr); waits != nil && !policy.Resumable {
		record.Status, record.FailureKind = "failed", "not_resumable"
		callErr = fmt.Errorf("%w：操作 %q 收到 %v", ErrWaitNotResumable, id, callErr)
	} else if waits != nil {
		record.Status = "waiting"
		if record.Continuation, err = json.Marshal(waits); err != nil {
			record.Status, record.FailureKind, record.Continuation = "failed", "execution", nil
			callErr = errors.Join(callErr, err)
		} else {
			callErr = storedWaits(record)
		}
	} else if callErr != nil {
		record.Status, record.FailureKind = "failed", "execution"
		if errors.Is(callErr, context.Canceled) {
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
			return nil, false, fmt.Errorf("reconciled output is not JSON")
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
