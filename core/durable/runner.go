// Package durable executes explicit, versioned steps with persisted continuation.
// It does not serialize Go closures or resume arbitrary Agent stack frames.
package durable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

var ErrApprovalRequired = errors.New("durable step requires approval")

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

// Store locks a run across the entire execution and atomically saves snapshots.
// Unlock must also occur after process exit (as OS-backed file locks do).
type Store interface {
	Lock(context.Context, string) (func() error, error)
	Load(context.Context, string) (*Snapshot, error)
	Save(context.Context, string, *Snapshot) error
}
type Runner struct {
	Store   Store
	Version string
	Steps   []Step
}

// Run starts or resumes a run. Initial data is used only for a new run. To resume
// an approval wait, pass the exact waiting Snapshot.Key after authorization by
// the host application. The key binds approval to that run, plan and step; it
// is an idempotency key, NOT an authentication secret.
func (r *Runner) Run(ctx context.Context, id string, initial json.RawMessage, approvalKey string) (snapshot *Snapshot, err error) {
	if id == "" || r.Version == "" || r.Store == nil {
		return nil, fmt.Errorf("run ID, version and store are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names := map[string]bool{}
	descriptor := r.Version
	for _, step := range r.Steps {
		if step.Name == "" || names[step.Name] || step.Run == nil {
			return nil, fmt.Errorf("steps require unique names and functions")
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
			return nil, fmt.Errorf("cannot approve a run that does not exist")
		}
		if len(initial) == 0 {
			initial = json.RawMessage(`null`)
		}
		if !json.Valid(initial) {
			return nil, fmt.Errorf("initial data must be valid JSON")
		}
		snapshot = &Snapshot{Version: r.Version, Plan: plan, RunID: id, Data: append(json.RawMessage(nil), initial...)}
		if err = r.Store.Save(ctx, id, snapshot); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if snapshot.Plan != plan || snapshot.Version != r.Version || snapshot.RunID != id || snapshot.Next < 0 || snapshot.Next > len(r.Steps) || !json.Valid(snapshot.Data) {
		return nil, fmt.Errorf("checkpoint does not match this versioned plan or is corrupt")
	}
	if approvalKey != "" && (!snapshot.Waiting || snapshot.Key != approvalKey) {
		return snapshot, fmt.Errorf("approval does not match waiting step")
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
				return snapshot, ErrApprovalRequired
			}
		}
		// Persist intent before execution. On a failed/ambiguous effect this exact
		// position, approval and key are reused, while committed steps are skipped.
		if err = r.Store.Save(ctx, id, snapshot); err != nil {
			return snapshot, err
		}
		output, e := step.Run(ctx, StepInput{Data: append(json.RawMessage(nil), snapshot.Data...), Key: snapshot.Key})
		if e != nil {
			return snapshot, e
		}
		if !json.Valid(output) {
			return snapshot, fmt.Errorf("step %s returned invalid JSON", step.Name)
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
func digest(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
