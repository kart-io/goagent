package management

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/execution"
	"go.opentelemetry.io/otel/trace"
)

type Worker struct {
	s       *Service
	id      string
	slot    string
	mu      sync.Mutex
	live    map[string]context.CancelFunc
	closing bool
	cancel  context.CancelFunc
	done    chan struct{}
	wg      sync.WaitGroup
}

func NewWorker(s *Service) *Worker {
	return &Worker{s: s, id: uuid.NewString(), slot: "default", live: map[string]context.CancelFunc{}, done: make(chan struct{})}
}

func (w *Worker) BeginShutdown() {
	w.mu.Lock()
	w.closing = true
	if w.cancel != nil {
		w.cancel()
	}
	for _, cancel := range w.live {
		cancel()
	}
	w.mu.Unlock()
}

func (w *Worker) Shutdown(ctx context.Context) error {
	w.BeginShutdown()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) report(kind string, err error) {
	if err != nil && w.s.cfg.Report != nil {
		w.s.cfg.Report(kind, err)
	}
}

func (w *Worker) owner(ctx context.Context, release bool) error {
	return w.s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		o, e := read[WorkerOwner](ctx, tx, "workers", "__system", w.slot)
		if errors.Is(e, agentErrors.ErrManagementNotFound) && !release {
			o = WorkerOwner{Meta: Meta{ID: w.slot, TenantID: "__system", Revision: 1, CreatedAt: w.s.cfg.Now().UTC()}, OwnerID: w.id, Generation: 1, LastSeen: w.s.cfg.Now().UTC()}
			return insert(ctx, tx, "workers", o)
		}
		if e != nil {
			return e
		}
		if release {
			if o.OwnerID != w.id {
				return agentErrors.ErrManagementOwner
			}
			o.OwnerID = ""
		} else {
			if o.OwnerID != "" && o.OwnerID != w.id {
				return agentErrors.ErrManagementOwner
			}
			o.OwnerID = w.id
			o.Generation++
		}
		old := o.Revision
		o.Revision++
		o.LastSeen = w.s.cfg.Now().UTC()
		return update(ctx, tx, "workers", o, old)
	})
}

func (s *Service) RecoverWorkerSlot(ctx context.Context, a Actor, owner string, generation int64, reason string) error {
	return s.recoverSlot(ctx, a, "default", owner, generation, reason)
}

func (s *Service) RecoverOutboxSlot(ctx context.Context, a Actor, owner string, generation int64, reason string) error {
	return s.recoverSlot(ctx, a, "outbox", owner, generation, reason)
}

func (s *Service) recoverSlot(ctx context.Context, a Actor, slot, owner string, generation int64, reason string) error {
	if err := s.auth(ctx, a, "worker.recover", "workers", slot); err != nil {
		return err
	}
	if !validText(reason, 1024) || owner == "" {
		return agentErrors.ErrManagementInvalid
	}
	if s.cfg.ConfirmWorkerStopped == nil {
		return agentErrors.ErrManagementUnsupported
	}
	if err := s.cfg.ConfirmWorkerStopped(ctx, owner, generation); err != nil {
		return err
	}
	return s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		o, e := read[WorkerOwner](ctx, tx, "workers", "__system", slot)
		if e != nil {
			return e
		}
		if o.OwnerID != owner || o.Generation != generation {
			return agentErrors.ErrManagementConflict
		}
		old := o.Revision
		o.OwnerID = ""
		o.Revision++
		if e = update(ctx, tx, "workers", o, old); e != nil {
			return e
		}
		return s.audit(ctx, tx, a, "workers", slot, "recover", reason)
	})
}

func (w *Worker) Run(parent context.Context) (runErr error) {
	w.mu.Lock()
	if w.cancel != nil || w.closing {
		w.mu.Unlock()
		return agentErrors.ErrManagementConflict
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.mu.Unlock()
	defer close(w.done)
	defer cancel()
	if err := w.owner(ctx, false); err != nil {
		return err
	}
	defer func() {
		cancel()
		w.mu.Lock()
		w.closing = true
		for _, c := range w.live {
			c()
		}
		w.mu.Unlock()
		w.wg.Wait()
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer cancel()
		runErr = errors.Join(runErr, w.owner(cleanup, true))
	}()
	if err := w.recover(ctx); err != nil {
		// 恢复扫描收到关闭信号时按主循环正常退出，真实恢复错误仍交给宿主。
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return nil
		}
		return err
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := w.poll(ctx); err != nil && !errors.Is(err, context.Canceled) {
			w.report("poll", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func (w *Worker) recover(ctx context.Context) error {
	return w.s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		// 每批修改后记录即离开查询集合；从集合头继续读取，不使用会因修改而跳项的偏移分页。
		// 两类记录仍在同一事务中恢复，任一批失败均由 Store 回滚。
		const batchSize = 200
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			tasks, e := all[Task](ctx, tx, "tasks", "", Query{Statuses: []string{string(Running), string(Canceling)}, Limit: batchSize})
			if e != nil {
				return e
			}
			for _, t := range tasks {
				old := t.Revision
				t.State = Queued
				t.RecoveryNeeded = true
				t.Revision++
				if e = update(ctx, tx, "tasks", t, old); e != nil {
					return e
				}
			}
			if len(tasks) < batchSize {
				break
			}
		}
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			cs, e := all[Command](ctx, tx, "commands", "", Query{Statuses: []string{"applying"}, Limit: batchSize})
			if e != nil {
				return e
			}
			for _, c := range cs {
				old := c.Revision
				c.State = "pending"
				c.Revision++
				if e = update(ctx, tx, "commands", c, old); e != nil {
					return e
				}
			}
			if len(cs) < batchSize {
				break
			}
		}
		return nil
	})
}

func (w *Worker) poll(ctx context.Context) error {
	owner, err := read[WorkerOwner](ctx, w.s.cfg.Store, "workers", "__system", w.slot)
	if err != nil {
		return err
	}
	if owner.OwnerID != w.id {
		w.BeginShutdown()
		return agentErrors.ErrManagementOwner
	}
	if err = w.retryUnconfirmed(ctx); err != nil {
		return err
	}
	cs, err := all[Command](ctx, w.s.cfg.Store, "commands", "", Query{Statuses: []string{"pending", "blocked"}, Limit: 10000})
	if err != nil {
		return err
	}
	for _, c := range cs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.AvailableAt.After(w.s.cfg.Now()) {
			continue
		}
		if c.Kind == "create_session" || c.Kind == "fork" {
			err = w.s.applySession(ctx, c)
			state := "applied"
			if err != nil {
				state = "attention"
			}
			if e := w.commandState(ctx, c, state, err); e != nil {
				return e
			}
			continue
		}
		if c.Kind == "cancel" {
			w.mu.Lock()
			cancel := w.live[c.TaskID]
			w.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			if err = w.commandState(ctx, c, "applied", nil); err != nil {
				return err
			}
			continue
		}
		if c.Kind == "start" || c.Kind == "resume" {
			if err = w.start(ctx, c); err != nil && !errors.Is(err, agentErrors.ErrManagementBusy) && !errors.Is(err, agentErrors.ErrManagementConflict) {
				w.report("start", err)
			}
		}
	}
	return nil
}

func (w *Worker) commandState(ctx context.Context, c Command, state string, cause error) error {
	return w.s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		cur, e := read[Command](ctx, tx, "commands", c.TenantID, c.ID)
		if e != nil {
			return e
		}
		kind := ""
		if cause != nil {
			kind = "application_failed"
			switch {
			case errors.Is(cause, agentErrors.ErrManagementAgentDisabled):
				kind = "agent_disabled"
			case errors.Is(cause, agentErrors.ErrManagementForbidden) || errors.Is(cause, agentErrors.ErrManagementUnauthenticated):
				kind = "permission_denied"
			case errors.Is(cause, agentErrors.ErrManagementUnsupported):
				kind = "implementation_missing"
			}
		}
		if cur.State == state && cur.ErrorKind == kind {
			return nil
		}
		old := cur.Revision
		cur.State = state
		cur.ErrorKind = kind
		cur.Revision++
		if cause != nil {
			if c.SessionID != "" && (c.Kind == "create_session" || c.Kind == "fork") {
				se, err := read[Session](ctx, tx, "sessions", c.TenantID, c.SessionID)
				if err != nil {
					return err
				}
				rev := se.Revision
				se.Revision++
				se.Status = "attention"
				if err = update(ctx, tx, "sessions", se, rev); err != nil {
					return err
				}
			}
		}
		return update(ctx, tx, "commands", cur, old)
	})
}

func (w *Worker) blockStart(ctx context.Context, c Command, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, agentErrors.ErrManagementAgentDisabled) || errors.Is(err, agentErrors.ErrManagementForbidden) || errors.Is(err, agentErrors.ErrManagementUnauthenticated) || errors.Is(err, agentErrors.ErrManagementUnsupported) {
		if e := w.commandState(ctx, c, "blocked", err); e != nil {
			return e
		}
		return agentErrors.ErrManagementBusy
	}
	return err
}

func (w *Worker) start(ctx context.Context, c Command) error {
	w.mu.Lock()
	if w.closing || len(w.live) >= w.s.cfg.Limits.Workers || w.live[c.TaskID] != nil {
		w.mu.Unlock()
		return agentErrors.ErrManagementBusy
	}
	// 构造前预留槽位；生命周期锁不包数据库或宿主校验。
	// 即使校验不响应取消，Shutdown也能按期限返回，并保留owner等待它实际退出。
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	w.live[c.TaskID] = cancel
	w.wg.Add(1)
	w.mu.Unlock()
	launched := false
	defer func() {
		if !launched {
			cancel()
			w.mu.Lock()
			delete(w.live, c.TaskID)
			w.mu.Unlock()
			w.wg.Done()
		}
	}()
	preview, err := read[Task](ctx, w.s.cfg.Store, "tasks", c.TenantID, c.TaskID)
	if err != nil {
		return err
	}
	if !preview.State.Terminal() {
		version, e := read[Version](ctx, w.s.cfg.Store, "versions", c.TenantID, versionID(preview.AgentID, preview.Version))
		if e != nil {
			return e
		}
		factory := w.s.cfg.Factories[version.RunnerKind]
		if factory == nil {
			return w.blockStart(ctx, c, agentErrors.ErrManagementUnsupported)
		}
		if _, e = validateFactory(ctx, factory, version); e != nil {
			return w.blockStart(ctx, c, agentErrors.ErrManagementUnsupported)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var task Task
	var se Session
	var v Version
	var attempt Attempt
	var claim buildClaim
	var inputs map[string]string
	err = w.s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		cur, e := read[Command](ctx, tx, "commands", c.TenantID, c.ID)
		if e != nil {
			return e
		}
		if cur.State != "pending" && cur.State != "blocked" {
			return agentErrors.ErrManagementConflict
		}
		task, e = read[Task](ctx, tx, "tasks", c.TenantID, c.TaskID)
		if e != nil {
			return e
		}
		if task.State.Terminal() {
			cur.State = "applied"
			cur.ErrorKind = ""
			old := cur.Revision
			cur.Revision++
			return update(ctx, tx, "commands", cur, old)
		}
		se, e = read[Session](ctx, tx, "sessions", c.TenantID, task.SessionID)
		if e != nil {
			return e
		}
		if se.Status != "ready" || (se.ActiveTaskID != "" && se.ActiveTaskID != task.ID) {
			return agentErrors.ErrManagementBusy
		}
		ag, e := read[Agent](ctx, tx, "agents", c.TenantID, task.AgentID)
		if e != nil {
			return e
		}
		if !ag.Enabled {
			return agentErrors.ErrManagementAgentDisabled
		}
		v, e = read[Version](ctx, tx, "versions", c.TenantID, versionID(task.AgentID, task.Version))
		if e != nil {
			return e
		}
		if task.AgentID != se.AgentID || task.Version != se.Version || task.RevisionID != se.RevisionID || task.RevisionID != v.RevisionID {
			return agentErrors.ErrManagementConflict
		}
		if w.s.cfg.Factories[v.RunnerKind] == nil {
			return agentErrors.ErrManagementUnsupported
		}
		if v.RevisionID != "" {
			if _, ok := w.s.cfg.Factories[v.RunnerKind].(RevisionFactory); !ok {
				return agentErrors.ErrManagementUnsupported
			}
		}
		if task.State != Queued && c.Kind != "resume" {
			return agentErrors.ErrManagementBusy
		}
		if task.State == Running || task.State == Canceling {
			return agentErrors.ErrManagementBusy
		}
		inputs = map[string]string{}
		if c.Kind == "resume" {
			if c.ApprovalID == "" {
				if e = w.s.cfg.Authorizer.Authorize(ctx, c.Actor, "task.resume", ResourceRef{Type: "tasks", ID: task.ID, AgentID: task.AgentID, SessionID: task.SessionID, TenantID: task.TenantID}); e != nil {
					return e
				}
			}
			found := c.WaitID == "" && c.ReconciliationRef != "" && (task.State == Attention || task.RecoveryNeeded)
			for _, point := range task.Waits {
				if point.ID != c.WaitID {
					continue
				}
				found = true
				if point.Kind == "approval" {
					ap, e := read[Approval](ctx, tx, "approvals", c.TenantID, c.ApprovalID)
					if e != nil {
						return e
					}
					if ap.Wait.ID != point.ID || ap.Wait.Epoch != point.Epoch || ap.Wait.IntentHash != point.IntentHash || (ap.State != "approved" && ap.State != "rejected") {
						return agentErrors.ErrManagementConflict
					}
					if e = w.s.cfg.Authorizer.Authorize(ctx, ap.Actor, "approval.decide", ResourceRef{Type: "approvals", ID: ap.ID, TenantID: ap.TenantID, AgentID: task.AgentID, SessionID: task.SessionID}); e != nil {
						return e
					}
					c.Input, _ = json.Marshal(ApprovalAnswer{ap.ID, ap.Decision, point.IntentHash})
				}
				inputs[point.Key] = string(c.Input)
			}
			if !found {
				old := cur.Revision
				cur.State = "conflict"
				cur.Revision++
				return update(ctx, tx, "commands", cur, old)
			}
		}
		if task.CancelRequested && canCancelWithoutInspection(task.State, task.RecoveryNeeded) {
			old := task.Revision
			task.State = Canceled
			task.Revision++
			now := w.s.cfg.Now().UTC()
			task.FinishedAt = &now
			if e = update(ctx, tx, "tasks", task, old); e != nil {
				return e
			}
			if e = w.s.releaseSession(ctx, tx, task); e != nil {
				return e
			}
			old = cur.Revision
			cur.State = "applied"
			cur.Revision++
			return update(ctx, tx, "commands", cur, old)
		}
		old := task.Revision
		claim = buildClaim{state: task.State, recoveryNeeded: task.RecoveryNeeded}
		task.State = Running
		task.RecoveryNeeded = false
		task.Revision++
		if e = update(ctx, tx, "tasks", task, old); e != nil {
			return e
		}
		old = se.Revision
		se.ActiveTaskID = task.ID
		se.Revision++
		if e = update(ctx, tx, "sessions", se, old); e != nil {
			return e
		}
		number, e := nextAttemptNumber(ctx, tx, task)
		if e != nil {
			return e
		}
		attempt = Attempt{Meta: w.s.meta(task.Actor), TaskID: task.ID, Number: number, WorkerID: w.id}
		if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
			attempt.TraceID = sc.TraceID().String()
		}
		if e = insert(ctx, tx, "attempts", attempt); e != nil {
			return e
		}
		old = cur.Revision
		cur.State = "applying"
		cur.ErrorKind = ""
		cur.Revision++
		return update(ctx, tx, "commands", cur, old)
	})
	if err != nil {
		return w.blockStart(ctx, c, err)
	}
	if task.State != Running || attempt.ID == "" {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	runCtx := ctx
	launched = true
	go func() {
		defer w.wg.Done()
		defer cancel()
		defer func() { w.mu.Lock(); delete(w.live, task.ID); w.mu.Unlock() }()
		tracer := w.s.cfg.Tracer
		if tracer == nil {
			tracer = observe.TracerFromContext(runCtx)
		}
		traced, span := tracer.Start(runCtx, "management.task")
		defer span.End()
		if sc := span.SpanContext(); sc.IsValid() && attempt.TraceID != sc.TraceID().String() {
			attempt.TraceID = sc.TraceID().String()
			next := attempt
			next.Revision++
			if e := w.s.cfg.Store.WithinTx(traced, func(tx Tx) error { return update(traced, tx, "attempts", next, attempt.Revision) }); e == nil {
				attempt = next
			} else {
				w.report("trace", e)
			}
		}
		w.execute(traced, parent, c, task, se, v, attempt, inputs, claim)
	}()
	return nil
}
func outcomeID(t Task) string { return durable.ChildKey(t.ExecutionKey, "management/outcome/v1") }
func loadOutcome(ctx context.Context, j durable.Store, t Task, versionHash string) (RunResult, error) {
	snap, e := j.Load(ctx, outcomeID(t))
	if e != nil {
		return RunResult{}, e
	}
	var envelope struct {
		TaskID, VersionHash, InputHash string
		Result                         RunResult
	}
	if e = json.Unmarshal(snap.Data, &envelope); e != nil {
		return RunResult{}, e
	}
	if envelope.TaskID != t.ID || envelope.InputHash != t.InputHash || envelope.VersionHash != versionHash {
		return RunResult{}, agentErrors.ErrManagementConflict
	}
	return envelope.Result, nil
}

func saveOutcome(ctx context.Context, j durable.Store, t Task, v Version, result RunResult) (err error) {
	id := outcomeID(t)
	unlock, e := j.Lock(ctx, id)
	if e != nil {
		return e
	}
	defer func() { err = errors.Join(err, unlock()) }()
	raw, e := encode(struct {
		TaskID, VersionHash, InputHash string
		Result                         RunResult
	}{t.ID, v.ConfigHash, t.InputHash, result})
	if e != nil {
		return e
	}
	return j.Save(ctx, id, &durable.Snapshot{Version: "management/outcome/v1", RunID: t.ID, Data: raw})
}

func (w *Worker) execute(ctx, parent context.Context, c Command, t Task, se Session, v Version, attempt Attempt, inputs map[string]string, claim buildClaim) {
	result := RunResult{}
	var runErr error
	buildBlocked := false
	j := session.DurableStore(w.s.cfg.Sessions, se.LogID)
	defer func() {
		if p := recover(); p != nil {
			buildBlocked = false
			runErr = agentErrors.Newf(agentErrors.CodeInternal, "运行回调发生 panic").WithComponent("management")
			result = RunResult{Outcome: "attention", ErrorKind: "panic"}
		}
		doneCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if buildBlocked {
			if e := w.blockBuild(doneCtx, c, t, attempt, claim); e != nil {
				w.report("build_blocked", e)
			}
			return
		}
		if e := w.finish(doneCtx, c, t, attempt, result, runErr, parent.Err() != nil); e != nil {
			w.report("finish", e)
		}
	}()
	inspectionCtx, inspectionCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer inspectionCancel()
	unlock, err := w.s.cfg.Sessions.Lock(inspectionCtx, se.LogID, "management/execution")
	if err != nil {
		runErr = err
		return
	}
	defer func() { w.report("session_unlock", unlock()) }()
	cached, err := loadOutcome(inspectionCtx, j, t, v.ConfigHash)
	if err == nil && (cached.Outcome == "completed" || cached.Outcome == "failed" || cached.Outcome == "canceled") {
		result = cached
		return
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		runErr = err
		result.Outcome = "attention"
		return
	}
	controller, err := budget.NewDurable(t.Budget, j)
	if err != nil {
		runErr = err
		return
	}
	scope, err := execution.NewScope(execution.Config{RunID: t.RunID, SessionID: se.LogID, Journal: j, Budget: controller, Tracer: w.s.cfg.Tracer})
	if err != nil {
		runErr = err
		return
	}
	readScope, err := execution.NewScope(execution.Config{RunID: t.RunID, SessionID: se.LogID, Journal: j})
	if err != nil {
		runErr = err
		return
	}
	inspectBound, inspectCancel, err := readScope.Bind(inspectionCtx)
	if err != nil {
		runErr = err
		return
	}
	defer inspectCancel()
	resources := Resources{Task: t, Session: se, Scope: scope, Journal: j, Budget: controller, Sessions: w.s.cfg.Sessions, ApprovalReader: func(ctx context.Context, id string) (Approval, error) {
		return read[Approval](ctx, w.s.cfg.Store, "approvals", t.TenantID, id)
	}}
	binding, err := w.s.cfg.Factories[v.RunnerKind].Build(inspectBound, v, resources)
	if err != nil {
		var cleanupErr error
		if binding.Cleanup != nil {
			cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			cleanupErr = binding.Cleanup(cleanCtx)
			cancel()
			w.report("cleanup", cleanupErr)
		}
		runErr = errors.Join(err, cleanupErr)
		var retryable *RetryableBuildError
		buildBlocked = binding.Runtime == nil && cleanupErr == nil && errors.As(err, &retryable) && retryable != nil
		return
	}
	if binding.Cleanup != nil {
		defer func() {
			cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			w.report("cleanup", binding.Cleanup(cleanCtx))
		}()
	}
	if binding.Runtime == nil {
		runErr = agentErrors.ErrManagementInvalid
		return
	}
	spec := RunSpec{Task: t, ResumeInputs: inputs}
	evidence, err := binding.Runtime.Inspect(inspectBound, spec)
	if err != nil {
		runErr = err
		result.Outcome = "attention"
		return
	}
	if evidence.State == "completed" {
		result = evidence.Result
		runErr = w.confirmOutcome(ctx, j, t, v, &result)
		return
	}
	if evidence.State != "absent" && evidence.State != "waiting" && (c.ReconciliationRef == "" || t.CancelRequested) {
		result = evidence.Result
		result.Outcome = "attention"
		if result.ErrorKind == "" {
			result.ErrorKind = "unknown_evidence"
		}
		return
	}
	if t.CancelRequested {
		result.Outcome = "interrupted"
		runErr = context.Canceled
		return
	}
	bound, cancel, err := scope.Bind(ctx)
	if err != nil {
		runErr = err
		return
	}
	defer cancel()
	if c.ReconciliationRef != "" {
		if w.s.cfg.Reconciliations == nil || !v.Capabilities.Reconcile {
			result = RunResult{Outcome: "attention", ErrorKind: "reconciliation_unsupported"}
			return
		}
		rec, e := w.s.cfg.Reconciliations.Lookup(bound, c.Actor, c.ReconciliationRef)
		if e != nil || rec.TenantID != t.TenantID || rec.TaskID != t.ID || rec.VersionHash != v.ConfigHash || rec.EvidenceHash != c.ReconciliationHash || !sameOperations(t.Operations, rec.OperationRefs) {
			result = RunResult{Outcome: "attention", ErrorKind: "reconciliation_mismatch"}
			runErr = e
			return
		}
		reconciler, ok := binding.Runtime.(RuntimeReconciler)
		if !ok {
			result = RunResult{Outcome: "attention", ErrorKind: "reconciliation_unsupported"}
			return
		}
		spec.Reconciliation = &rec
		evidence, err = reconciler.Reconcile(bound, spec, rec)
		if err != nil {
			result = RunResult{Outcome: "attention", ErrorKind: "reconciliation_failed"}
			runErr = err
			return
		}
		if evidence.State == "completed" {
			result = evidence.Result
			runErr = w.confirmOutcome(ctx, j, t, v, &result)
			return
		}
	}
	if evidence.State != "absent" && evidence.State != "waiting" {
		result = evidence.Result
		result.Outcome = "attention"
		if result.ErrorKind == "" {
			result.ErrorKind = "unknown_evidence"
		}
		return
	}
	result, runErr = binding.Runtime.Run(bound, spec)
	if runErr != nil {
		if waits := durable.Waits(runErr); waits != nil {
			result.Outcome = "waiting"
			if len(result.Waits) == 0 {
				result.Waits = WaitPoints(waits)
			}
		} else if errors.Is(runErr, agentErrors.ErrOperationAttention) {
			result.Outcome = "attention"
			result.ErrorKind = "unknown_effect"
		} else if errors.Is(runErr, context.Canceled) {
			result.Outcome = "interrupted"
		} else {
			result.Outcome = "failed"
			result.ErrorKind = "execution"
		}
	}
	if result.Outcome == "" {
		result.Outcome = "failed"
		result.ErrorKind = "missing_outcome"
	}
	if len(result.Output) > w.s.cfg.Limits.MaxOutputBytes {
		result.Output = nil
		result.Outcome = "failed"
		result.ErrorKind = "output_limit"
	}
	if result.FinishedAt.IsZero() {
		result.FinishedAt = w.s.cfg.Now().UTC()
	}
	if result.Outcome != "interrupted" {
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if e := saveOutcome(persist, j, t, v, result); e != nil {
			result.Outcome = "attention"
			result.ErrorKind = "outcome_unconfirmed"
			runErr = e
		}
	}
}

func (w *Worker) confirmOutcome(ctx context.Context, j durable.Store, t Task, v Version, result *RunResult) error {
	if result.Outcome != "completed" || !json.Valid(result.Output) {
		result.Outcome = "attention"
		result.ErrorKind = "invalid_completion_evidence"
		return agentErrors.ErrManagementConflict
	}
	if len(result.Output) > w.s.cfg.Limits.MaxOutputBytes {
		result.Outcome = "failed"
		result.ErrorKind = "output_limit"
		result.Output = nil
	}
	if result.FinishedAt.IsZero() {
		result.FinishedAt = w.s.cfg.Now().UTC()
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if e := saveOutcome(persist, j, t, v, *result); e != nil {
		result.Outcome = "attention"
		result.ErrorKind = "outcome_unconfirmed"
		return e
	}
	return nil
}

func WaitPoints(waits []*durable.WaitError) []WaitPoint {
	out := []WaitPoint{}
	seen := map[string]bool{}
	for _, wait := range waits {
		if len(wait.Path) == 0 {
			continue
		}
		key := wait.Path[len(wait.Path)-1].Key
		var c durable.InterruptContinuation
		if json.Unmarshal(wait.Continuation, &c) != nil || c.ID == "" {
			continue
		}
		var info struct {
			Kind       string `json:"kind"`
			IntentHash string `json:"intent_hash"`
		}
		_ = json.Unmarshal(c.Info, &info)
		kind := "input"
		if info.Kind == "approval" {
			kind = "approval"
		}
		hash := digest(c.Info)
		if info.IntentHash != "" {
			hash = info.IntentHash
		}
		id := digest([]byte(key + "\x00" + c.ID + "\x00" + hash))
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, WaitPoint{ID: id, Kind: kind, Key: key, InterruptID: c.ID, IntentHash: hash, Info: c.Info, Rejected: c.Rejected, Reason: c.Reason})
	}
	return out
}

func (w *Worker) finish(ctx context.Context, c Command, original Task, attempt Attempt, result RunResult, runErr error, stopping bool) error {
	if result.FinishedAt.IsZero() {
		result.FinishedAt = w.s.cfg.Now().UTC()
	}
	return w.s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		owner, e := read[WorkerOwner](ctx, tx, "workers", "__system", "default")
		if e != nil || owner.OwnerID != w.id {
			return agentErrors.ErrManagementOwner
		}
		t, e := read[Task](ctx, tx, "tasks", original.TenantID, original.ID)
		if e != nil {
			return e
		}
		if t.State.Terminal() {
			return nil
		}
		old := t.Revision
		t.Revision++
		t.ErrorKind = result.ErrorKind
		if result.Operations != nil {
			t.Operations = result.Operations
		}
		if t.CancelRequested && result.Outcome == "waiting" {
			result.Outcome = "canceled"
		}
		switch result.Outcome {
		case "canceled":
			t.State = Canceled
			t.Waits = nil
		case "completed":
			t.State = Succeeded
			t.Result = result.Output
			t.ResultRef = outcomeID(t)
			t.Waits = nil
		case "waiting":
			if len(result.Waits) == 0 {
				t.State = Attention
				t.ErrorKind = "unlocated_wait"
			} else {
				t.State = WaitingInput
				t.Waits = result.Waits
				for i := range t.Waits {
					point := &t.Waits[i]
					for _, prev := range original.Waits {
						if prev.ID == point.ID {
							point.Epoch = prev.Epoch
						}
					}
					if point.Epoch == 0 {
						t.WaitEpoch++
						point.Epoch = t.WaitEpoch
					}
					if point.Kind == "approval" {
						t.State = WaitingApproval
						apID := digest([]byte(t.ID + "/" + point.ID))
						ap, e := read[Approval](ctx, tx, "approvals", t.TenantID, apID)
						if errors.Is(e, agentErrors.ErrManagementNotFound) {
							ap = Approval{Meta: w.s.meta(t.Actor), TaskID: t.ID, AgentID: t.AgentID, SessionID: t.SessionID, Wait: *point, State: "pending"}
							ap.ID = apID
							if e = insert(ctx, tx, "approvals", ap); e != nil {
								return e
							}
						} else if e != nil {
							return e
						}
					}
				}
			}
		case "attention":
			t.State = Attention
		case "interrupted":
			if stopping {
				t.State = Queued
				t.RecoveryNeeded = true
			} else if t.CancelRequested {
				t.State = Canceled
			} else {
				t.State = Attention
				t.ErrorKind = "interrupted"
			}
		default:
			if stopping && (errors.Is(runErr, context.Canceled) || result.Outcome == "") {
				t.State = Queued
				t.RecoveryNeeded = true
			} else if t.CancelRequested && errors.Is(runErr, context.Canceled) {
				t.State = Canceled
			} else {
				t.State = Failed
				if t.ErrorKind == "" {
					t.ErrorKind = "execution"
				}
			}
		}
		if t.State.Terminal() {
			now := w.s.cfg.Now().UTC()
			t.FinishedAt = &now
			if e = w.s.releaseSession(ctx, tx, t); e != nil {
				return e
			}
			if e = w.s.supersedeApprovals(ctx, tx, t); e != nil {
				return e
			}
			raw, _ := encode(map[string]any{"task_id": t.ID, "state": t.State})
			if e = insert(ctx, tx, "outbox", Delivery{Meta: w.s.meta(t.Actor), Kind: "task." + string(t.State), ResourceID: t.ID, Payload: raw, State: "pending", AvailableAt: now}); e != nil {
				return e
			}
		}
		if e = update(ctx, tx, "tasks", t, old); e != nil {
			return e
		}
		cmd, e := read[Command](ctx, tx, "commands", c.TenantID, c.ID)
		if e != nil {
			return e
		}
		rev := cmd.Revision
		cmd.Revision++
		cmd.State = "applied"
		for _, point := range result.Waits {
			if point.ID == c.WaitID && point.Rejected {
				cmd.State = "conflict"
				cmd.ErrorKind = "validation_rejected"
			}
		}
		if t.RecoveryNeeded {
			cmd.State = "pending"
		}
		if e = update(ctx, tx, "commands", cmd, rev); e != nil {
			return e
		}
		attempt.FinishedAt = &result.FinishedAt
		attempt.Outcome = string(t.State)
		attempt.Revision++
		if e = update(ctx, tx, "attempts", attempt, attempt.Revision-1); e != nil {
			return e
		}
		return w.s.audit(ctx, tx, t.Actor, "tasks", t.ID, "state", string(t.State))
	})
}
