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
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
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

// Store 在整个执行期间为一个运行加锁，并原子保存快照。实现须满足以下契约：
//   - Lock 等待到获得锁为止，ctx 结束前未获得时返回同时包装 ErrLockUnavailable 与 ctx 错误的错误。
//     同一 ID 不可重入，同一进程内的第二次 Lock 同样等待；锁之间不做死锁检测，嵌套成环时只能等到 ctx 结束。
//   - 锁可能持有很久（整张图、整个持久操作），同一进程会同时持有多个 ID；持有者进程退出后，锁须在有界时间内释放。
//   - Save 只在调用方持有该 ID 的锁时调用，也可能在 Lock 的 ctx 取消后以 WithoutCancel 的 ctx 调用。
//     Save 原子且持久，Snapshot 的全部字段原样保存。持有者已失去锁时，实现可以拒绝写入并返回 ErrLeaseLost。
//   - Load 不需要持锁，读到的是最近一次成功 Save 的完整快照（不能来自滞后的副本），成功时快照非 nil；
//     记录不存在时返回可用 errors.Is(err, os.ErrNotExist) 识别的错误。
//
// core/durable/storetest 验证其中可在通用环境检验的部分，不覆盖长时间持锁、进程退出后释放与 ErrLeaseLost。
type Store interface {
	Lock(context.Context, string) (func() error, error)
	Load(context.Context, string) (*Snapshot, error)
	Save(context.Context, string, *Snapshot) error
}

// ErrLockUnavailable 表示 ctx 结束前未能获得锁：运行锁通常是其他持有者正在执行同一运行，
// 作用域相关的操作也可能在等待作用域的删除或写入结束。
var ErrLockUnavailable = errors.New("持久运行锁不可用")

// ErrLeaseLost 表示写入时调用方已不再持有该 ID 的锁（如租约过期后被其他实例接管），写入被拒绝。
var ErrLeaseLost = errors.New("持久运行锁已失效，拒绝写入")

// ErrScopeRemoved 表示作用域已被 RemoveScope 删除，不再接受读写。
var ErrScopeRemoved = errors.New("持久作用域已删除，拒绝读写")

// Scoped 是 Store 可选的分区能力：Scope 返回独立命名空间（按需创建，只读不产生文件），
// RemoveScope 整体删除该命名空间并留下墓碑，使其拒绝读写（Lock/Save/Load 返回 ErrScopeRemoved），作用域不存在时同样生效；
// 删除中途崩溃时墓碑已生效，残留数据在再次 RemoveScope 之前同样不可读。名称删除后不可复用。
// RemoveScope 返回时不保证进行中的持锁者已经结束（FileStore 会等待其释放），只保证之后的写入被拒绝；重复调用不刷新墓碑时间。
//
// PruneRemoved 删除早于 olderThan 写下的墓碑（连同崩溃残留的数据），与写入互斥。墓碑删除后该名称的 Load 返回 os.ErrNotExist，
// 迟到的写入不再被拒绝、可能留下残留且可被读到，因此 olderThan 须大于最长的重试或恢复窗口；未过期的墓碑照常拒绝读写。
type Scoped interface {
	Scope(name string) Store
	RemoveScope(ctx context.Context, name string) error
	PruneRemoved(ctx context.Context, olderThan time.Duration) error
}
type Runner struct {
	Store   Store
	Version string
	Steps   []Step
	// BeforeApprove 在运行锁内、批准生效前调用，用于核对暂停期间外部数据是否被改动。
	// 返回错误或 panic 时拒绝本次批准且不改动检查点；snap 是独立副本。
	BeforeApprove func(ctx context.Context, id string, snap *Snapshot) error
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
				return snapshot, ErrApprovalRequired
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
