package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

type journalContext struct{}

// WithJournal binds child operations to a host-owned store. An execution key
// must also be present. Records contain outputs and hashes of inputs, not inputs.
func WithJournal(ctx context.Context, store Store) context.Context {
	return context.WithValue(ctx, journalContext{}, store)
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
}

type OperationRecord struct {
	ID          string          `json:"id"`
	Key         string          `json:"key"`
	InputHash   string          `json:"input_hash"`
	Status      string          `json:"status"` // pending, completed, failed
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	Value       json.RawMessage `json:"value"`
	FailureKind string          `json:"failure_kind,omitempty"`
	Replayed    bool            `json:"-"`
}

var ErrOperationAttention = errors.New("durable operation needs host attention")

type OperationError struct{ ID, Kind string }

func (e *OperationError) Error() string {
	return fmt.Sprintf("operation %q needs host attention (%s)", e.ID, e.Kind)
}
func (e *OperationError) Unwrap() error { return ErrOperationAttention }

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
	if snapshot == nil || snapshot.Version != "goagent-operation-v1" || snapshot.RunID != operationStorageID(key) || json.Unmarshal(snapshot.Data, &r) != nil ||
		r.ID != id || r.Key != key || r.InputHash == "" || r.Attempts < 1 || r.MaxAttempts < 1 || r.Attempts > r.MaxAttempts || !json.Valid(r.Value) ||
		(r.Status != "pending" && r.Status != "completed" && r.Status != "failed") {
		return nil, fmt.Errorf("invalid durable operation record")
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
		if !policy.RetryFailed || record.Attempts >= policy.MaxAttempts {
			record.Replayed = true
			return record, &OperationError{ID: id, Kind: record.FailureKind}
		}
	} else {
		record = &OperationRecord{ID: id, Key: key, InputHash: inputHash, MaxAttempts: policy.MaxAttempts}
	}
	record.Attempts++
	record.Status, record.FailureKind, record.Value = "pending", "ambiguous", json.RawMessage(`null`)
	save := func(saveCtx context.Context) error {
		data, e := json.Marshal(record)
		if e != nil {
			return e
		}
		return store.Save(saveCtx, storageID, &Snapshot{RunID: storageID, Version: "goagent-operation-v1", Data: data})
	}
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
	if callErr != nil {
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

func invokeOperation(ctx context.Context, run func(context.Context) (json.RawMessage, error)) (value json.RawMessage, err error) {
	defer func() {
		if v := recover(); v != nil {
			value, err = nil, agentErrors.NewPanicError("持久操作", v)
		}
	}()
	return run(ctx)
}
