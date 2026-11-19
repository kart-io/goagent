// Package durable executes explicit, versioned steps with persisted continuation.
// It does not serialize Go closures or resume arbitrary Agent stack frames.
package durable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
)

// Step functions must use Key to deduplicate external side effects. A crash
// after an effect but before its checkpoint can repeat the SAME key on restart.
type Step struct {
	Name            string
	RequireApproval bool
	Run             func(context.Context, StepInput) (json.RawMessage, error)
}
type StepInput struct {
	Data json.RawMessage
	Key  string
}

// Snapshot is the persisted continuation, not an in-memory waiter/channel.
type Snapshot struct {
	Version  string          `json:"version"`
	Plan     string          `json:"plan"`
	RunID    string          `json:"run_id"`
	Next     int             `json:"next"`
	Data     json.RawMessage `json:"data"`
	Waiting  bool            `json:"waiting"`
	Approved bool            `json:"approved"`
	Key      string          `json:"key,omitempty"`
}

// Store locks a run for the whole execution and saves snapshots atomically.
// Implementations must satisfy the following contract:
//   - Lock waits until it acquires the lock; if it has not acquired it when ctx
//     ends, it returns an error that wraps both ErrLockUnavailable and the ctx
//     error. The same ID is not reentrant, and a second Lock in the same
//     process also waits; locks have no deadlock detection, so a nested cycle
//     waits until ctx ends.
//   - Locks may be held for a long time (an entire graph or durable operation),
//     and one process holds several IDs at once; after the holder process
//     exits, the lock must be released within bounded time.
//   - Save is called only while the caller holds the lock for that ID, and may
//     also be called with a WithoutCancel ctx after the Lock ctx is canceled.
//     Save is atomic and durable and stores every Snapshot field verbatim. When
//     the holder has lost the lock, the implementation may refuse the write and
//     return ErrLeaseLost.
//   - Load needs no lock and reads the complete snapshot of the most recent
//     successful Save (not from a lagging replica); on success the snapshot is
//     non-nil. When the record does not exist it returns an error recognized by
//     errors.Is(err, os.ErrNotExist).
//   - SaveAll writes snapshots for several IDs as one atomic write: either all
//     are visible or none are, including after a crash. The caller must hold
//     the lock for every ID and is responsible for acquiring multiple locks in
//     a consistent order. Duplicate IDs or nil snapshots are rejected without
//     writing; empty writes do nothing. On errors with an unknown commit
//     outcome (such as a dropped connection), the caller must Load again to
//     find out. SaveAll on a scoped view covers only IDs in that scope.
//
// core/durable/storetest verifies the parts of this contract that can be
// checked in a generic environment; it does not cover long-held locks, release
// after process exit, or ErrLeaseLost.
type Store interface {
	Lock(context.Context, string) (func() error, error)
	Load(context.Context, string) (*Snapshot, error)
	Save(context.Context, string, *Snapshot) error
	SaveAll(context.Context, ...Write) error
}

// Write is a single write in Store.SaveAll.
type Write struct {
	ID       string
	Snapshot *Snapshot
}

// ValidateWrites checks the arguments to SaveAll; Store implementations call it
// before writing.
func ValidateWrites(writes []Write) error {
	seen := make(map[string]bool, len(writes))
	for _, write := range writes {
		if write.ID == "" || write.Snapshot == nil {
			return agentErrors.Newf(agentErrors.CodeInvalidInput, "批量写入需要 ID 和快照").WithComponent("durable_runner").WithOperation("validate_writes")
		}
		if seen[write.ID] {
			return agentErrors.Newf(agentErrors.CodeInvalidInput, "批量写入的 ID 重复：%q", write.ID).WithComponent("durable_runner").WithOperation("validate_writes")
		}
		seen[write.ID] = true
	}
	return nil
}

// Scoped is an optional partitioning capability of a Store: Scope returns an
// independent namespace (created on demand; reads create no files), and
// RemoveScope deletes the whole namespace and leaves a tombstone so that it
// rejects reads and writes (Lock/Save/Load return ErrScopeRemoved), which also
// takes effect when the scope does not exist. If removal crashes midway the
// tombstone is already in effect, and leftover data stays unreadable until
// RemoveScope is called again. A name cannot be reused after removal. When
// RemoveScope returns, in-progress lock holders are not guaranteed to have
// finished (FileStore waits for them to release); it only guarantees that later
// writes are rejected. Repeated calls do not refresh the tombstone time.
//
// PruneRemoved deletes tombstones written before olderThan (along with data
// left by crashes) and is mutually exclusive with writes. After a tombstone is
// deleted, Load for that name returns os.ErrNotExist, and late writes are no
// longer rejected and may leave residue that can be read, so olderThan must
// exceed the longest retry or recovery window; unexpired tombstones reject
// reads and writes as usual.
type Scoped interface {
	Scope(name string) Store
	RemoveScope(ctx context.Context, name string) error
	PruneRemoved(ctx context.Context, olderThan time.Duration) error
}
type Runner struct {
	Store   Store
	Version string
	Steps   []Step
	// BeforeApprove is called inside the run lock before an approval takes
	// effect, to check whether external data changed during the pause.
	// Returning an error or panicking rejects this approval without modifying
	// the checkpoint; snap is an independent copy.
	BeforeApprove func(ctx context.Context, id string, snap *Snapshot) error
}

// Run starts or resumes a run. Initial data is used only for a new run. To resume
// an approval wait, pass the exact waiting Snapshot.Key after authorization by
// the host application. The key binds approval to that run, plan and step; it
// is an idempotency key, NOT an authentication secret.
func (r *Runner) Run(ctx context.Context, id string, initial json.RawMessage, approvalKey string) (snapshot *Snapshot, err error) {
	if id == "" || r.Version == "" || r.Store == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "run ID, version and store are required").WithComponent("durable_runner").WithOperation("run")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names := map[string]bool{}
	descriptor := r.Version
	for _, step := range r.Steps {
		if step.Name == "" || names[step.Name] || step.Run == nil {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "steps require unique names and functions").WithComponent("durable_runner").WithOperation("run")
		}
		names[step.Name] = true
		descriptor += fmt.Sprintf("\x00%s:%t", step.Name, step.RequireApproval)
	}
	plan := digest(descriptor)
	unlock, err := r.Store.Lock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := unlock(); e != nil {
			err = errors.Join(err, e)
		}
	}()
	snapshot, err = r.Store.Load(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		if approvalKey != "" {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "cannot approve a run that does not exist").WithComponent("durable_runner").WithOperation("run")
		}
		if len(initial) == 0 {
			initial = json.RawMessage(`null`)
		}
		if !json.Valid(initial) {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "initial data must be valid JSON").WithComponent("durable_runner").WithOperation("run")
		}
		snapshot = &Snapshot{Version: r.Version, Plan: plan, RunID: id, Data: append(json.RawMessage(nil), initial...)}
		if err = r.Store.Save(ctx, id, snapshot); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if snapshot.Plan != plan || snapshot.Version != r.Version || snapshot.RunID != id || snapshot.Next < 0 || snapshot.Next > len(r.Steps) || !json.Valid(snapshot.Data) {
		return nil, agentErrors.Newf(agentErrors.CodeStateValidation, "checkpoint does not match this versioned plan or is corrupt").WithComponent("durable_runner").WithOperation("run")
	}
	if approvalKey != "" && (!snapshot.Waiting || snapshot.Key != approvalKey) {
		return snapshot, agentErrors.Newf(agentErrors.CodeInvalidInput, "approval does not match waiting step").WithComponent("durable_runner").WithOperation("run")
	}
	if approvalKey != "" && r.BeforeApprove != nil {
		if err = r.checkBeforeApprove(ctx, id, snapshot); err != nil {
			return snapshot, err
		}
	}
	for snapshot.Next < len(r.Steps) {
		if err = ctx.Err(); err != nil {
			return snapshot, err
		}
		step := r.Steps[snapshot.Next]
		snapshot.Key = digest(id + "\x00" + plan + "\x00" + step.Name)
		if step.RequireApproval && !snapshot.Approved {
			if approvalKey == snapshot.Key {
				snapshot.Approved = true
				snapshot.Waiting = false
				approvalKey = ""
			} else {
				snapshot.Waiting = true
				if err = r.Store.Save(ctx, id, snapshot); err != nil {
					return snapshot, err
				}
				return snapshot, agentErrors.ErrApprovalRequired
			}
		}
		// Persist intent before execution. On a failed/ambiguous effect this exact
		// position, approval and key are reused, while committed steps are skipped.
		if err = r.Store.Save(ctx, id, snapshot); err != nil {
			return snapshot, err
		}
		output, e := step.Run(WithJournal(WithExecutionKey(ctx, snapshot.Key), r.Store), StepInput{Data: append(json.RawMessage(nil), snapshot.Data...), Key: snapshot.Key})
		if e != nil {
			return snapshot, e
		}
		if !json.Valid(output) {
			return snapshot, agentErrors.Newf(agentErrors.CodeInvalidOutput, "step %s returned invalid JSON", step.Name).WithComponent("durable_runner").WithOperation("run")
		}
		next := *snapshot
		next.Data = append(json.RawMessage(nil), output...)
		next.Next++
		next.Key = ""
		next.Approved = false
		next.Waiting = false
		if err = r.Store.Save(ctx, id, &next); err != nil {
			return snapshot, err
		}
		snapshot = &next
	}
	return snapshot, nil
}

func (r *Runner) checkBeforeApprove(ctx context.Context, id string, snapshot *Snapshot) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = agentErrors.NewPanicError("批准前置校验", p)
		}
	}()
	snap := *snapshot
	snap.Data = append(json.RawMessage(nil), snapshot.Data...)
	if err = r.BeforeApprove(ctx, id, &snap); err != nil {
		return fmt.Errorf("批准前置校验拒绝: %w", err)
	}
	return nil
}
func digest(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
