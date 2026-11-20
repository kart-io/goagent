package durable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
)

type journalContext struct{}

type replayOnlyContext struct{}

// WithReplayOnly restricts Do in the derived context to replaying completed
// records, without calling run or Reconcile. Host callbacks, authorization
// logic, and code that accesses storage directly must still ensure on their own
// that they are read-only.
func WithReplayOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, replayOnlyContext{}, true)
}

// WithJournal binds child operations to a host-owned store. An execution key
// must also be present. Records contain outputs and hashes of inputs, not inputs.
func WithJournal(ctx context.Context, store Store) context.Context {
	return context.WithValue(ctx, journalContext{}, store)
}

// JournalStore returns the journal Store bound to ctx, or nil if there is none.
// Nested subflows use it to write their state into the same Store as the parent
// journal.
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
	// It applies only to ordinary failures with a known outcome; interrupted
	// records (crash, cancellation, timeout) are rerun only after Reconcile
	// confirms the effect did not take place or RetryInterrupted is declared.
	RetryFailed bool `json:"retry_failed"`
	// Reconcile is called before a rerun when a record was interrupted (stuck
	// in pending, failed due to cancellation or timeout, or failed with
	// ErrEffectUnknown) and checks by derived execution key whether the effect has taken
	// place: if found is true the record is completed with value without
	// calling run or counting an attempt; if it returns an error the record is
	// unchanged.
	Reconcile func(ctx context.Context) (value json.RawMessage, found bool, err error) `json:"-"`
	// Resumable declares that all effects of run are recorded under derived
	// execution keys, so re-entering only replays completed steps: waiting
	// records are always re-entered without counting an attempt, and pending
	// records are re-entered within MaxAttempts. Only operations that declare
	// it can record a *WaitError as waiting. Independently of Resumable, a run
	// that stops because an inner operation needs host attention keeps the
	// record pending without consuming an attempt and is always re-entered, so
	// that the host's conclusion reaches the inner record; the inner records
	// are journaled under derived keys and replay instead of rerunning.
	Resumable bool `json:"resumable"`
	// RetryInterrupted allows rerunning an interrupted operation within
	// MaxAttempts: one whose record is stuck in pending (process crash), failed
	// due to cancellation or timeout, or failed with ErrEffectUnknown. The previous execution may already
	// have taken effect, so the caller must accept duplicate effects; ordinary
	// failures are not interruptions.
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
	// Continuation is the set of waits saved by a waiting record (JSON of
	// []*WaitError) and contains only location information.
	Continuation json.RawMessage `json:"continuation,omitempty"`
	// Reconciled reports that the completed result came from Reconcile rather
	// than from this execution.
	Reconciled bool `json:"reconciled,omitzero"`
	// ParentKey is the execution key the operation ran under (ExecutionKey of
	// the ctx passed to Do); together with ID it forms the OperationRef a host
	// reconciles. Records written before it existed leave it empty.
	ParentKey string `json:",omitempty"`
	Replayed  bool   `json:"-"`
}

const operationVersion = "goagent-operation-v2"

type waitableContext struct{}

// CanWait reports whether returning *WaitError in ctx can be resumed: the
// innermost durable operation containing ctx has a journal and declares
// Resumable. Subflows that may wait (such as DurableGraph.AsChildRunnable) use
// it to fail before running any step.
func CanWait(ctx context.Context) bool {
	ok, _ := ctx.Value(waitableContext{}).(bool)
	return ok
}

// ErrEffectUnknown is returned (wrapped) by an operation's run function when
// it cannot tell whether its effect took place, for example when a connection
// drops after the request was sent. Do records it as failed with FailureKind
// "effect_unknown", treats it like a crash (an interrupted record) and returns
// an *OperationError so the host decides what happened.
var ErrEffectUnknown = errors.New("durable operation effect is unknown")

// OperationError reports an operation whose outcome needs host attention.
// ParentKey and ID identify it as a durable.OperationRef.
type OperationError struct {
	ID, Kind  string
	ParentKey string `json:",omitempty"`
}

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
// Only operations that declare Resumable can host a wait; other operations
// record it as a failure and return ErrWaitNotResumable.
type WaitError struct {
	Path         []WaitStep      `json:"path,omitempty"`
	Continuation json.RawMessage `json:"continuation,omitempty"`
	// Cause is the in-process diagnostic for this wait, such as a panic in
	// validate; it is not persisted and not exposed through Unwrap, so the wait
	// is not treated as a failure.
	Cause error `json:"-"`
}

// ErrorCode identifies the wait category without changing how wait leaves or
// diagnostic causes are unwrapped.
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

// OperationStorageID returns the storage identity of an operation in the
// current journal Store; it neither accesses storage nor changes execution
// state.
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

// DecodeOperationRecord only decodes and validates an operation snapshot of the
// current version; it does not access the Store, run business logic, or modify
// its inputs. key and id are the operation identity the caller expects;
// migration tools and online execution share the same data contract.
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
	parentKey := ExecutionKey(ctx)
	if err == nil {
		record.ParentKey = parentKey
		if record.InputHash != inputHash || record.MaxAttempts != policy.MaxAttempts {
			return record, agentErrors.Newf(agentErrors.CodeConflict, "operation intent or attempt policy changed").WithComponent("durable_operation").WithOperation("do")
		}
		if record.Status == "completed" {
			record.Replayed = true
			return record, nil
		}
		absent := false
		// nested 的外层没有中断：它停下是因为内层操作需要宿主处理，再次进入只回放内层记录。
		nested := record.Status == "pending" && record.FailureKind == nestedAttentionKind
		interrupted := !nested && (record.Status == "pending" || (record.Status == "failed" && (record.FailureKind == "canceled" || record.FailureKind == "deadline" || record.FailureKind == "effect_unknown")))
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
		// 内层记录按子键写入日志，重新进入只回放它们，不依赖外层是否声明 Resumable；
		// 否则非 Resumable 的外层挡在前面，宿主对内层的核对结论永远送不到。
		resume := nested || (policy.Resumable && (record.Status == "waiting" || (record.Status == "pending" && record.Attempts < policy.MaxAttempts)))
		if record.Status == "waiting" && !resume {
			record.Replayed = true
			return record, &OperationError{ID: id, Kind: "not_resumable", ParentKey: parentKey}
		}
		resume = resume || (policy.RetryInterrupted && interrupted && record.Attempts < policy.MaxAttempts)
		retryFailed := policy.RetryFailed && (!interrupted || absent)
		if !resume && (!retryFailed || record.Attempts >= policy.MaxAttempts) {
			record.Replayed = true
			return record, &OperationError{ID: id, Kind: record.FailureKind, ParentKey: parentKey}
		}
		// 从等待或内层待处理中续跑不是新的尝试。
		if record.Status != "waiting" && !nested {
			record.Attempts++
		}
	} else {
		record = &OperationRecord{ID: id, Key: key, InputHash: inputHash, MaxAttempts: policy.MaxAttempts, ParentKey: parentKey}
		record.Attempts++
	}
	record.Status, record.FailureKind, record.Value, record.Continuation = "pending", "ambiguous", json.RawMessage(`null`), nil
	save := func(saveCtx context.Context) error { return saveOperation(saveCtx, store, storageID, record) }
	if err = save(ctx); err != nil {
		return record, err
	}
	value, callErr := invokeOperation(context.WithValue(childCtx, attemptContext{}, record.Attempts), run)
	// run 已成功返回时效果已经生效：照常记为 completed 以便恢复时回放，同时把取消告知调用方。
	late := callErr == nil && ctx.Err() != nil
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
	} else if errors.Is(callErr, agentErrors.ErrOperationAttention) && !errors.Is(callErr, ErrEffectUnknown) {
		// 内层操作需要宿主处理：外层没有失败也不能算一次尝试，保持 pending 以便恢复时重新进入内层记录。
		record.Status, record.FailureKind = "pending", nestedAttentionKind
	} else if callErr != nil {
		record.Status, record.FailureKind = "failed", "execution"
		// 取消后供应方可能只返回“连接已关闭”，错误链里没有 context.Canceled。
		if errors.Is(callErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			record.FailureKind = "canceled"
		}
		if errors.Is(callErr, context.DeadlineExceeded) {
			record.FailureKind = "deadline"
		}
		if errors.Is(callErr, ErrEffectUnknown) {
			record.FailureKind = "effect_unknown"
			// 只返回分类错误：外层据此判断是内层待处理，而不是把结果未知记到自己身上。
			callErr = &OperationError{ID: id, Kind: "effect_unknown", ParentKey: parentKey}
		}
	}
	// Persist cancellation/failure evidence with a bounded independent context.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.DurableCompletionSaveTimeout)
	defer cancel()
	if late {
		callErr = errors.Join(callErr, ctx.Err())
	}
	return record, errors.Join(callErr, save(saveCtx))
}

// nestedAttentionKind marks a pending record whose run stopped because an
// inner operation needs host attention.
const nestedAttentionKind = "nested_attention"

type attemptContext struct{}

// OperationAttempt returns the current attempt number (starting at 1) of the
// durable operation containing ctx; it returns 0 outside an operation with a
// journal.
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
