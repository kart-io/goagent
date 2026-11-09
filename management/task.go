package management

import (
	"context"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

func (s *Service) SubmitTask(ctx context.Context, a Actor, r SubmitTaskRequest) (Task, error) {
	if err := s.auth(ctx, a, "task.submit", "sessions", r.SessionID); err != nil {
		return Task{}, err
	}
	raw, err := CanonicalJSON(r.Input)
	if err != nil || len(raw) > s.cfg.Limits.MaxInputBytes {
		return Task{}, agentErrors.ErrManagementInvalid
	}
	r.Input = raw
	return mutate(s, ctx, a, r.IdempotencyKey, "task.submit", r, func(tx Tx) (Task, error) {
		se, e := read[Session](ctx, tx, "sessions", a.TenantID, r.SessionID)
		if e != nil {
			return Task{}, e
		}
		ag, e := read[Agent](ctx, tx, "agents", a.TenantID, se.AgentID)
		if e != nil {
			return Task{}, e
		}
		if !ag.Enabled || se.Status != "ready" {
			return Task{}, agentErrors.ErrManagementConflict
		}
		pending, e := all[Task](ctx, tx, "tasks", a.TenantID, Query{Statuses: []string{string(Queued), string(Running), string(WaitingApproval), string(WaitingInput), string(Canceling), string(Attention)}, Limit: s.cfg.Limits.MaxPending + 1})
		if e != nil {
			return Task{}, e
		}
		if len(pending) >= s.cfg.Limits.MaxPending {
			return Task{}, agentErrors.ErrManagementBusy
		}
		t := Task{Meta: s.meta(a), SessionID: se.ID, AgentID: se.AgentID, Version: se.Version, RevisionID: se.RevisionID, Input: r.Input, InputHash: digest(r.Input), State: Queued, Actor: a}
		t.RunID = t.ID
		t.ExecutionKey = t.ID
		t.Budget = budget.Config{RootKey: t.ID, Version: "management/v1", MaxModelRequests: 100, MaxToolCalls: 1000, DeadlineUnixMilli: s.cfg.Now().Add(24 * time.Hour).UnixMilli()}
		if e = insert(ctx, tx, "tasks", t); e != nil {
			return t, e
		}
		if _, e = s.command(ctx, tx, a, "start", t.ID, se.ID); e != nil {
			return t, e
		}
		return t, s.audit(ctx, tx, a, "tasks", t.ID, "submit", "")
	})
}

func (s *Service) CancelTask(ctx context.Context, a Actor, r CancelTaskRequest) (Task, error) {
	if err := s.auth(ctx, a, "task.cancel", "tasks", r.TaskID); err != nil {
		return Task{}, err
	}
	if len([]rune(r.Reason)) > 1024 {
		return Task{}, agentErrors.ErrManagementInvalid
	}
	return mutate(s, ctx, a, r.IdempotencyKey, "task.cancel/"+r.TaskID, r, func(tx Tx) (Task, error) {
		t, e := read[Task](ctx, tx, "tasks", a.TenantID, r.TaskID)
		if e != nil {
			return t, e
		}
		if t.State.Terminal() {
			return t, nil
		}
		if t.Revision != r.ExpectedRevision {
			return t, agentErrors.ErrManagementConflict
		}
		if t.State != Running && t.State != Canceling && !canCancelWithoutInspection(t.State, t.RecoveryNeeded) {
			// 未知结果只能由已接纳的恢复命令核对；没有命令时拒绝取消，
			// 否则 CancelRequested 会封死公开 ResumeTask 入口。
			commands, err := all[Command](ctx, tx, "commands", t.TenantID, Query{TaskID: t.ID, Statuses: []string{"pending", "blocked", "applying"}, Limit: 10000})
			if err != nil {
				return t, err
			}
			continuing := false
			for _, command := range commands {
				if command.Kind == "start" || command.Kind == "resume" {
					continuing = true
					break
				}
			}
			if !continuing {
				return t, agentErrors.ErrManagementConflict
			}
		}
		old := t.Revision
		t.Revision++
		t.CancelRequested = true
		if t.State == Running || t.State == Canceling {
			t.State = Canceling
		} else if canCancelWithoutInspection(t.State, t.RecoveryNeeded) {
			t.State = Canceled
			now := s.cfg.Now().UTC()
			t.FinishedAt = &now
			if e = s.releaseSession(ctx, tx, t); e != nil {
				return t, e
			}
			if e = s.supersedeApprovals(ctx, tx, t); e != nil {
				return t, e
			}
		}
		// 待恢复或 Attention 保留原状态，由已接纳的恢复命令先核对。
		if e = update(ctx, tx, "tasks", t, old); e != nil {
			return t, e
		}
		if _, e = s.command(ctx, tx, a, "cancel", t.ID, t.SessionID); e != nil {
			return t, e
		}
		return t, s.audit(ctx, tx, a, "tasks", t.ID, "cancel", r.Reason)
	})
}

func (s *Service) releaseSession(ctx context.Context, tx Tx, t Task) error {
	se, e := read[Session](ctx, tx, "sessions", t.TenantID, t.SessionID)
	if e != nil {
		return e
	}
	if se.ActiveTaskID == t.ID {
		old := se.Revision
		se.ActiveTaskID = ""
		se.Revision++
		return update(ctx, tx, "sessions", se, old)
	}
	return nil
}

func (s *Service) supersedeApprovals(ctx context.Context, tx Tx, t Task) error {
	aps, e := all[Approval](ctx, tx, "approvals", t.TenantID, Query{TaskID: t.ID, Limit: 10000})
	if e != nil {
		return e
	}
	for _, ap := range aps {
		if ap.State == "pending" {
			old := ap.Revision
			ap.State = "superseded"
			ap.Revision++
			if e = update(ctx, tx, "approvals", ap, old); e != nil {
				return e
			}
		}
	}
	return nil
}

type ApprovalAnswer struct {
	ApprovalID string `json:"approval_id"`
	Decision   string `json:"decision"`
	IntentHash string `json:"intent_hash"`
}

func (s *Service) DecideApproval(ctx context.Context, a Actor, r DecisionRequest) (Approval, error) {
	if err := s.auth(ctx, a, "approval.decide", "approvals", r.ApprovalID); err != nil {
		return Approval{}, err
	}
	if (r.Decision != "approve" && r.Decision != "reject") || len([]rune(r.Reason)) > 1024 {
		return Approval{}, agentErrors.ErrManagementInvalid
	}
	return mutate(s, ctx, a, r.IdempotencyKey, "approval.decide/"+r.ApprovalID, r, func(tx Tx) (Approval, error) {
		ap, e := read[Approval](ctx, tx, "approvals", a.TenantID, r.ApprovalID)
		if e != nil {
			return ap, e
		}
		if ap.State != "pending" || ap.Revision != r.ExpectedRevision {
			return ap, agentErrors.ErrManagementConflict
		}
		t, e := read[Task](ctx, tx, "tasks", a.TenantID, ap.TaskID)
		if e != nil {
			return ap, e
		}
		if t.State.Terminal() || t.CancelRequested {
			return ap, agentErrors.ErrManagementConflict
		}
		found := false
		for _, p := range t.Waits {
			if p.ID == ap.Wait.ID && p.Epoch == ap.Wait.Epoch {
				found = true
			}
		}
		if !found {
			return ap, agentErrors.ErrManagementConflict
		}
		ap.Decision = r.Decision
		ap.State = "approved"
		if r.Decision == "reject" {
			ap.State = "rejected"
		}
		ap.Actor = a
		ap.Reason = r.Reason
		now := s.cfg.Now().UTC()
		ap.DecidedAt = &now
		ap.Revision++
		c := Command{Meta: s.meta(a), TaskID: t.ID, SessionID: t.SessionID, Kind: "resume", State: "pending", Actor: a, WaitID: ap.Wait.ID, ApprovalID: ap.ID, AvailableAt: now}
		ap.CommandID = c.ID
		ap.CommandState = c.State
		if e = update(ctx, tx, "approvals", ap, r.ExpectedRevision); e != nil {
			return ap, e
		}
		c.Input, _ = json.Marshal(ApprovalAnswer{ap.ID, ap.Decision, ap.Wait.IntentHash})
		if e = insert(ctx, tx, "commands", c); e != nil {
			return ap, e
		}
		return ap, s.audit(ctx, tx, a, "approvals", ap.ID, "decide", r.Reason)
	})
}

func (s *Service) ResumeTask(ctx context.Context, a Actor, r ResumeTaskRequest) (Command, error) {
	if err := s.auth(ctx, a, "task.resume", "tasks", r.TaskID); err != nil {
		return Command{}, err
	}
	return mutate(s, ctx, a, r.IdempotencyKey, "task.resume/"+r.TaskID, r, func(tx Tx) (Command, error) {
		var reconciliationHash string
		t, e := read[Task](ctx, tx, "tasks", a.TenantID, r.TaskID)
		if e != nil {
			return Command{}, e
		}
		if t.State.Terminal() || t.CancelRequested || t.Revision != r.ExpectedRevision {
			return Command{}, agentErrors.ErrManagementConflict
		}
		if r.ReconciliationRef != "" {
			if r.WaitID != "" || len(r.Input) > 0 || t.State != Attention {
				return Command{}, agentErrors.ErrManagementInvalid
			}
			if s.cfg.Reconciliations == nil {
				return Command{}, agentErrors.ErrManagementUnsupported
			}
			rec, e := s.cfg.Reconciliations.Lookup(ctx, a, r.ReconciliationRef)
			if e != nil {
				return Command{}, e
			}
			v, e := read[Version](ctx, tx, "versions", a.TenantID, versionID(t.AgentID, t.Version))
			if e != nil {
				return Command{}, e
			}
			if !v.Capabilities.Reconcile {
				return Command{}, agentErrors.ErrManagementUnsupported
			}
			if rec.TenantID != a.TenantID || rec.TaskID != t.ID || rec.VersionHash != v.ConfigHash || rec.EvidenceHash == "" || rec.VerifiedBy == "" || (rec.Resolution != "applied" && rec.Resolution != "not_applied") {
				return Command{}, agentErrors.ErrManagementConflict
			}
			if !sameOperations(t.Operations, rec.OperationRefs) || rec.VerifiedAt.IsZero() {
				return Command{}, agentErrors.ErrManagementConflict
			}
			reconciliationHash = rec.EvidenceHash
		} else {
			found := false
			for _, w := range t.Waits {
				if w.ID == r.WaitID {
					if w.Kind == "approval" {
						return Command{}, agentErrors.ErrManagementApprovalRequired
					}
					found = true
				}
			}
			if !found || !json.Valid(r.Input) || len(r.Input) > s.cfg.Limits.MaxInputBytes {
				return Command{}, agentErrors.ErrManagementInvalid
			}
		}
		c := Command{Meta: s.meta(a), TaskID: t.ID, SessionID: t.SessionID, Kind: "resume", State: "pending", Actor: a, WaitID: r.WaitID, Input: r.Input, AvailableAt: s.cfg.Now().UTC()}
		c.ReconciliationRef = r.ReconciliationRef
		c.ReconciliationHash = reconciliationHash
		if e = insert(ctx, tx, "commands", c); e != nil {
			return c, e
		}
		return c, s.audit(ctx, tx, a, "tasks", t.ID, "resume", r.ReconciliationRef)
	})
}

func sameOperations(a, b []durable.OperationRef) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	set := map[durable.OperationRef]int{}
	for _, ref := range a {
		if ref.ParentKey == "" || ref.ID == "" {
			return false
		}
		set[ref]++
	}
	for _, ref := range b {
		if set[ref] == 0 {
			return false
		}
		set[ref]--
	}
	return true
}
