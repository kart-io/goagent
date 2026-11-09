package management

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
)

type reviewStore map[string]Document

func keyR(k, t, id string) string { return k + "/" + t + "/" + id }
func (s reviewStore) Get(_ context.Context, k, t, id string) (Document, error) {
	d, ok := s[keyR(k, t, id)]
	if !ok {
		return d, agentErrors.ErrManagementNotFound
	}
	return d, nil
}

type verifiedReader struct{ record Reconciliation }

func (r verifiedReader) Lookup(context.Context, Actor, string) (Reconciliation, error) {
	return r.record, nil
}

type proofRuntime struct {
	unknownR
	seen *Reconciliation
}

func (f *proofRuntime) Build(context.Context, Version, Resources) (Binding, error) {
	return Binding{Runtime: f}, nil
}

func (f *proofRuntime) Reconcile(_ context.Context, spec RunSpec, rec Reconciliation) (RunEvidence, error) {
	if spec.Reconciliation == nil {
		return RunEvidence{}, agentErrors.ErrManagementInvalid
	}
	f.seen = &rec
	return RunEvidence{State: "completed", Result: RunResult{Outcome: "completed", Output: []byte(`1`)}}, nil
}

func TestReconciliationEvidencePersistedAndApplied(t *testing.T) {
	w, task, se, _, at := setupR(t)
	ctx := context.Background()
	s := w.s
	s.cfg.Authorizer = TenantAuthorizer{}
	task.State = Attention
	task.Operations = []durable.OperationRef{{ParentKey: "parent", ID: "external-write"}}
	task.Revision++
	if e := update(ctx, s.cfg.Store.(Tx), "tasks", task, 1); e != nil {
		t.Fatal(e)
	}
	v := Version{Meta: Meta{ID: "@", TenantID: "tenant", Revision: 1}, RunnerKind: "proof", ConfigHash: "hash", Capabilities: Capabilities{Reconcile: true}}
	if e := insert(ctx, s.cfg.Store.(Tx), "versions", v); e != nil {
		t.Fatal(e)
	}
	rec := Reconciliation{ID: "record", TenantID: "tenant", TaskID: "task", VersionHash: "hash", EvidenceHash: "immutable-proof", VerifiedBy: "admin", VerifiedAt: time.Now(), Resolution: "applied", OperationRefs: task.Operations}
	s.cfg.Reconciliations = verifiedReader{rec}
	runtime := &proofRuntime{}
	s.cfg.Factories = map[string]Factory{"proof": runtime}
	c, e := s.ResumeTask(ctx, Actor{TenantID: "tenant", Subject: "admin"}, ResumeTaskRequest{IdempotencyKey: "proof", TaskID: "task", ExpectedRevision: 2, ReconciliationRef: "record"})
	if e != nil {
		t.Fatal(e)
	}
	saved, e := read[Command](ctx, s.cfg.Store, "commands", "tenant", c.ID)
	if e != nil || saved.ReconciliationRef != "record" || saved.ReconciliationHash != rec.EvidenceHash {
		t.Fatalf("证据未持久化：%+v %v", saved, e)
	}
	ev, _ := session.NewEvent("management/session", "session")
	_, _ = s.cfg.Sessions.Append(ctx, se.LogID, 0, ev)
	w.execute(ctx, ctx, c, task, se, v, at, nil, buildClaim{state: Queued, recoveryNeeded: task.RecoveryNeeded})
	got, e := read[Task](ctx, s.cfg.Store, "tasks", "tenant", "task")
	if e != nil || got.State != Succeeded || runtime.calls != 0 || runtime.seen == nil || runtime.seen.Resolution != "applied" {
		t.Fatalf("核对后只能补记结果，不得普通重跑：%+v %v", got, e)
	}
}

func (s reviewStore) List(_ context.Context, k string, q Selection) ([]Document, error) {
	r := []Document{}
	for _, d := range s {
		if d.Kind == k && (q.TenantID == "" || q.TenantID == d.TenantID) {
			r = append(r, d)
		}
	}
	return r, nil
}
func (s reviewStore) WithinTx(_ context.Context, fn func(Tx) error) error { return fn(s) }
func (s reviewStore) Insert(_ context.Context, d Document) error {
	s[keyR(d.Kind, d.TenantID, d.ID)] = d
	return nil
}

func (s reviewStore) CAS(c context.Context, d Document, e int64) error {
	o, x := s.Get(c, d.Kind, d.TenantID, d.ID)
	if x != nil {
		return x
	}
	if o.Revision != e {
		return agentErrors.ErrManagementConflict
	}
	return s.Insert(c, d)
}

func (s reviewStore) Delete(_ context.Context, k, t, id string, _ int64) error {
	delete(s, keyR(k, t, id))
	return nil
}

func setupR(t *testing.T) (*Worker, Task, Session, Command, Attempt) {
	st := reviewStore{}
	s := &Service{cfg: Config{Store: st, Sessions: session.NewMemoryStore(), Now: time.Now, Limits: DefaultLimits()}}
	w := NewWorker(s)
	a := Actor{TenantID: "tenant", Subject: "actor"}
	task := Task{Meta: Meta{ID: "task", TenantID: a.TenantID, Revision: 1}, State: Running, SessionID: "session", Actor: a, RunID: "task", ExecutionKey: "task", Input: []byte(`1`), Budget: budget.Config{RootKey: "task", Version: "v1", MaxModelRequests: 10}}
	se := Session{Meta: Meta{ID: "session", TenantID: a.TenantID, Revision: 1}, ActiveTaskID: "task", LogID: "log"}
	cmd := Command{Meta: Meta{ID: "command", TenantID: a.TenantID, Revision: 1}, TaskID: "task", State: "applying"}
	at := Attempt{Meta: Meta{ID: "attempt", TenantID: a.TenantID, Revision: 1}, TaskID: "task"}
	for k, v := range map[string]any{"tasks": task, "sessions": se, "commands": cmd, "attempts": at, "workers": WorkerOwner{Meta: Meta{ID: "default", TenantID: "__system", Revision: 1}, OwnerID: w.id}} {
		if e := insert(context.Background(), st, k, v); e != nil {
			t.Fatal(e)
		}
	}
	return w, task, se, cmd, at
}

func TestReviewCancelWaitingRace(t *testing.T) {
	w, task, _, cmd, at := setupR(t)
	cur := task
	cur.State = Canceling
	cur.CancelRequested = true
	cur.Revision++
	if e := update(context.Background(), w.s.cfg.Store.(Tx), "tasks", cur, 1); e != nil {
		t.Fatal(e)
	}
	e := w.finish(context.Background(), cmd, task, at, RunResult{Outcome: "waiting", Waits: []WaitPoint{{ID: "wait", Kind: "input", Info: []byte(`{}`)}}}, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	got, e := read[Task](context.Background(), w.s.cfg.Store, "tasks", "tenant", "task")
	if e != nil {
		t.Fatal(e)
	}
	if got.State != Canceled {
		t.Fatalf("cancel accepted, producer exited; want canceled got %s (cancel_requested=%v)", got.State, got.CancelRequested)
	}
}

type unknownR struct{ calls int }

func (f *unknownR) Validate(context.Context, Version) (Capabilities, error) {
	return Capabilities{}, nil
}

func (f *unknownR) Build(context.Context, Version, Resources) (Binding, error) {
	return Binding{Runtime: f}, nil
}

func (f *unknownR) Inspect(context.Context, RunSpec) (RunEvidence, error) {
	return RunEvidence{State: "unknown"}, nil
}

func (f *unknownR) Run(context.Context, RunSpec) (RunResult, error) {
	f.calls++
	return RunResult{Outcome: "completed", Output: []byte(`1`)}, nil
}

func TestReviewUnknownEvidence(t *testing.T) {
	w, task, se, cmd, at := setupR(t)
	ev, _ := session.NewEvent("management/session", "session")
	w.s.cfg.Sessions.Append(context.Background(), se.LogID, 0, ev)
	f := &unknownR{}
	w.s.cfg.Factories = map[string]Factory{"r": f}
	w.execute(context.Background(), context.Background(), cmd, task, se, Version{RunnerKind: "r"}, at, nil, buildClaim{state: Queued, recoveryNeeded: task.RecoveryNeeded})
	got, _ := read[Task](context.Background(), w.s.cfg.Store, "tasks", "tenant", "task")
	t.Logf("task=%+v", got)
	if f.calls != 0 {
		t.Fatalf("Inspect unknown still invoked Runtime.Run %d times", f.calls)
	}
}

func TestCompletedEvidenceWinsRecoveredCancel(t *testing.T) {
	w, task, se, cmd, at := setupR(t)
	ev, _ := session.NewEvent("management/session", "session")
	_, _ = w.s.cfg.Sessions.Append(context.Background(), se.LogID, 0, ev)
	task.CancelRequested = true
	task.RecoveryNeeded = true
	task.Revision++
	if e := update(context.Background(), w.s.cfg.Store.(Tx), "tasks", task, 1); e != nil {
		t.Fatal(e)
	}
	f := &completedR{}
	w.s.cfg.Factories = map[string]Factory{"r": f}
	w.execute(context.Background(), context.Background(), cmd, task, se, Version{RunnerKind: "r"}, at, nil, buildClaim{state: Queued, recoveryNeeded: task.RecoveryNeeded})
	got, _ := read[Task](context.Background(), w.s.cfg.Store, "tasks", "tenant", "task")
	if got.State != Succeeded || f.calls != 0 {
		t.Fatalf("完成证据必须优先于恢复中的取消，state=%s calls=%d", got.State, f.calls)
	}
}

type completedR struct{ calls int }

func (f *completedR) Validate(context.Context, Version) (Capabilities, error) {
	return Capabilities{}, nil
}

func (f *completedR) Build(context.Context, Version, Resources) (Binding, error) {
	return Binding{Runtime: f}, nil
}

func (f *completedR) Inspect(context.Context, RunSpec) (RunEvidence, error) {
	return RunEvidence{State: "completed", Result: RunResult{Outcome: "completed", Output: []byte(`1`)}}, nil
}

func (f *completedR) Run(context.Context, RunSpec) (RunResult, error) {
	f.calls++
	return RunResult{}, nil
}

func TestOutboxOwnerHasRecoveryProtocol(t *testing.T) {
	w, _, _, _, _ := setupR(t)
	s := w.s
	s.cfg.Authorizer = TenantAuthorizer{}
	s.cfg.ConfirmWorkerStopped = func(context.Context, string, int64) error { return nil }
	o := WorkerOwner{Meta: Meta{ID: "outbox", TenantID: "__system", Revision: 1}, OwnerID: "old", Generation: 2}
	if e := insert(context.Background(), s.cfg.Store.(Tx), "workers", o); e != nil {
		t.Fatal(e)
	}
	r, ok := any(s).(interface {
		RecoverOutboxSlot(context.Context, Actor, string, int64, string) error
	})
	if !ok {
		t.Fatal("缺少外部待办owner的受信回收接口")
	}
	if e := r.RecoverOutboxSlot(context.Background(), Actor{TenantID: "tenant", Subject: "admin"}, "old", 2, "旧进程已退出"); e != nil {
		t.Fatal(e)
	}
	got, e := read[WorkerOwner](context.Background(), s.cfg.Store, "workers", "__system", "outbox")
	if e != nil || got.OwnerID != "" {
		t.Fatalf("outbox未回收：%+v %v", got, e)
	}
}

type recReader struct{}

func (recReader) Lookup(context.Context, Actor, string) (Reconciliation, error) {
	return Reconciliation{ID: "record", TenantID: "tenant", TaskID: "task", VersionHash: "hash", EvidenceHash: "proof", VerifiedBy: "admin", Resolution: "not_applied"}, nil
}

func TestReconciliationCannotBeAcceptedWithoutProtocol(t *testing.T) {
	w, task, _, _, _ := setupR(t)
	s := w.s
	s.cfg.Authorizer = TenantAuthorizer{}
	s.cfg.Reconciliations = recReader{}
	task.State = Attention
	task.Revision++
	if e := update(context.Background(), s.cfg.Store.(Tx), "tasks", task, 1); e != nil {
		t.Fatal(e)
	}
	v := Version{Meta: Meta{ID: "@", TenantID: "tenant", Revision: 1}, ConfigHash: "hash"}
	if e := insert(context.Background(), s.cfg.Store.(Tx), "versions", v); e != nil {
		t.Fatal(e)
	}
	_, e := s.ResumeTask(context.Background(), Actor{TenantID: "tenant", Subject: "admin"}, ResumeTaskRequest{IdempotencyKey: "reconcile", TaskID: "task", ExpectedRevision: 2, ReconciliationRef: "record"})
	if e != agentErrors.ErrManagementUnsupported {
		t.Fatalf("没有适配核对协议时不可接纳attention恢复：%v", e)
	}
}
