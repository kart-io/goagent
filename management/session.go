package management

import (
	"context"
	"errors"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
)

func (s *Service) CreateSession(ctx context.Context, a Actor, r CreateSessionRequest) (Session, error) {
	if err := s.auth(ctx, a, "session.write", "agents", r.AgentID); err != nil {
		return Session{}, err
	}
	return mutate(s, ctx, a, r.IdempotencyKey, "session.create", r, func(tx Tx) (Session, error) {
		ag, err := read[Agent](ctx, tx, "agents", a.TenantID, r.AgentID)
		if err != nil {
			return Session{}, err
		}
		if !ag.Enabled {
			return Session{}, agentErrors.ErrManagementConflict
		}
		version := r.Version
		if version == "" {
			version = ag.PublishedVersion
		}
		if version == "" {
			return Session{}, agentErrors.ErrManagementInvalid
		}
		if _, err = read[Version](ctx, tx, "versions", a.TenantID, versionID(ag.ID, version)); err != nil {
			return Session{}, err
		}
		se := Session{Meta: s.meta(a), AgentID: ag.ID, Version: version, Status: "preparing"}
		se.LogID = "management/session/" + se.ID
		if err = insert(ctx, tx, "sessions", se); err != nil {
			return se, err
		}
		if _, err = s.command(ctx, tx, a, "create_session", "", se.ID); err != nil {
			return se, err
		}
		return se, s.audit(ctx, tx, a, "sessions", se.ID, "create", "")
	})
}
func (s *Service) ForkSession(ctx context.Context, a Actor, r ForkSessionRequest) (Session, error) {
	if err := s.auth(ctx, a, "session.fork", "sessions", r.SessionID); err != nil {
		return Session{}, err
	}
	return mutate(s, ctx, a, r.IdempotencyKey, "session.fork/"+r.SessionID, r, func(tx Tx) (Session, error) {
		source, err := read[Session](ctx, tx, "sessions", a.TenantID, r.SessionID)
		if err != nil {
			return Session{}, err
		}
		if source.Revision != r.ExpectedRevision || source.Status != "ready" || source.ActiveTaskID != "" || r.UpTo < 0 {
			return Session{}, agentErrors.ErrManagementConflict
		}
		agent, err := read[Agent](ctx, tx, "agents", a.TenantID, source.AgentID)
		if err != nil {
			return Session{}, err
		}
		if !agent.Enabled {
			return Session{}, agentErrors.ErrManagementConflict
		}
		tasks, err := all[Task](ctx, tx, "tasks", a.TenantID, Query{SessionID: source.ID, Limit: s.cfg.Limits.MaxPending + 1})
		if err != nil {
			return Session{}, err
		}
		for _, t := range tasks {
			if !t.State.Terminal() {
				return Session{}, agentErrors.ErrManagementConflict
			}
		}
		v, err := read[Version](ctx, tx, "versions", a.TenantID, versionID(source.AgentID, source.Version))
		if err != nil {
			return Session{}, err
		}
		if !v.Capabilities.Fork {
			return Session{}, agentErrors.ErrManagementUnsupported
		}
		events, err := s.cfg.Sessions.Read(ctx, source.LogID, 0)
		if err != nil {
			return Session{}, err
		}
		seq := r.UpTo
		valid := false
		for _, ev := range events {
			if ev.Type == constants.SessionEventTurnEnd {
				var end session.TurnEnd
				if json.Unmarshal(ev.Data, &end) == nil && !end.Aborted && (r.UpTo == 0 || ev.Seq == r.UpTo) {
					seq = ev.Seq
					valid = true
				}
			}
		}
		if !valid {
			return Session{}, agentErrors.ErrManagementInvalid
		}
		prefix := prefixEvents(events, seq)
		raw, err := encode(prefix)
		if err != nil {
			return Session{}, err
		}
		se := Session{Meta: s.meta(a), AgentID: source.AgentID, Version: source.Version, Status: "preparing", ParentID: source.ID, ForkSeq: seq, SourceHash: digest(raw)}
		se.LogID = "management/session/" + se.ID
		if err = insert(ctx, tx, "sessions", se); err != nil {
			return se, err
		}
		if _, err = s.command(ctx, tx, a, "fork", "", se.ID); err != nil {
			return se, err
		}
		return se, s.audit(ctx, tx, a, "sessions", se.ID, "fork", source.ID)
	})
}
func prefixEvents(events []session.Event, seq int64) []session.Event {
	out := []session.Event{}
	for _, ev := range events {
		if ev.Seq <= seq {
			out = append(out, ev)
		}
	}
	return out
}
func (s *Service) applySession(ctx context.Context, c Command) (err error) {
	se, err := read[Session](ctx, s.cfg.Store, "sessions", c.TenantID, c.SessionID)
	if err != nil {
		return err
	}
	if se.Status == "ready" {
		return nil
	}
	if se.ParentID == "" {
		events, e := s.cfg.Sessions.Read(ctx, se.LogID, 0)
		if e == nil {
			if len(events) == 0 || events[0].Type != "management/session" || string(events[0].Data) != `"`+se.ID+`"` {
				return agentErrors.ErrManagementConflict
			}
		} else {
			if !errors.Is(e, agentErrors.ErrSessionNotFound) {
				return e
			}
			ev, e := session.NewEvent("management/session", se.ID)
			if e != nil {
				return e
			}
			if _, e = s.cfg.Sessions.Append(ctx, se.LogID, 0, ev); e != nil {
				return e
			}
		}
	} else {
		source, e := read[Session](ctx, s.cfg.Store, "sessions", c.TenantID, se.ParentID)
		if e != nil {
			return e
		}
		unlock, e := s.cfg.Sessions.Lock(ctx, source.LogID, "management/execution")
		if e != nil {
			return e
		}
		defer func() { err = errors.Join(err, unlock()) }()
		source, e = read[Session](ctx, s.cfg.Store, "sessions", c.TenantID, se.ParentID)
		if e != nil {
			return e
		}
		if source.ActiveTaskID != "" {
			return agentErrors.ErrManagementConflict
		}
		events, e := s.cfg.Sessions.Read(ctx, source.LogID, 0)
		if e != nil {
			return e
		}
		raw, _ := encode(prefixEvents(events, se.ForkSeq))
		if digest(raw) != se.SourceHash {
			return agentErrors.ErrManagementConflict
		}
		target, e := s.cfg.Sessions.Read(ctx, se.LogID, 0)
		if e != nil {
			if e = s.cfg.Sessions.Fork(ctx, source.LogID, se.LogID, se.ForkSeq); e != nil {
				return e
			}
		} else {
			raw, _ = encode(target)
			if digest(raw) != se.SourceHash {
				return agentErrors.ErrManagementConflict
			}
		}
	}
	return s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		cur, e := read[Session](ctx, tx, "sessions", c.TenantID, se.ID)
		if e != nil {
			return e
		}
		old := cur.Revision
		cur.Status = "ready"
		cur.Revision++
		return update(ctx, tx, "sessions", cur, old)
	})
}
