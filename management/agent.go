package management

import (
	"context"
	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
)

func (s *Service) RegisterAgent(ctx context.Context, a Actor, r RegisterAgentRequest) (Agent, error) {
	if err := s.auth(ctx, a, "agent.write", "agents", ""); err != nil {
		return Agent{}, err
	}
	if !validText(r.Name, 256) {
		return Agent{}, agentErrors.ErrManagementInvalid
	}
	return mutate(s, ctx, a, r.IdempotencyKey, "agent.register", r, func(tx Tx) (Agent, error) {
		v := Agent{Meta: s.meta(a), Name: r.Name, Enabled: true}
		if err := insert(ctx, tx, "agents", v); err != nil {
			return v, err
		}
		return v, s.audit(ctx, tx, a, "agents", v.ID, "register", "")
	})
}
func (s *Service) SaveVersion(ctx context.Context, a Actor, r SaveVersionRequest) (Version, error) {
	if err := s.auth(ctx, a, "agent.write", "agents", r.AgentID); err != nil {
		return Version{}, err
	}
	f := s.cfg.Factories[r.RunnerKind]
	if f == nil || !validText(r.Version, 128) || !validText(r.ImplementationVersion, 128) {
		return Version{}, agentErrors.ErrManagementInvalid
	}
	raw, err := CanonicalJSON(r.Config)
	if err != nil || len(raw) > s.cfg.Limits.MaxInputBytes {
		return Version{}, agentErrors.ErrManagementInvalid
	}
	r.Config = raw
	return mutate(s, ctx, a, r.IdempotencyKey, "agent.version/"+r.AgentID, r, func(tx Tx) (Version, error) {
		if _, e := read[Agent](ctx, tx, "agents", a.TenantID, r.AgentID); e != nil {
			return Version{}, e
		}
		v := Version{Meta: s.meta(a), AgentID: r.AgentID, Version: r.Version, RunnerKind: r.RunnerKind, ImplementationVersion: r.ImplementationVersion, Config: json.RawMessage(raw), ConfigHash: digest(append([]byte(r.RunnerKind+"\x00"+r.ImplementationVersion+"\x00"), raw...)), CreatedBy: a.Subject}
		v.ID = versionID(r.AgentID, r.Version)
		caps, e := f.Validate(ctx, v)
		if e != nil {
			return v, wrapInvalid(e)
		}
		v.Capabilities = caps
		if e = insert(ctx, tx, "versions", v); e != nil {
			return v, e
		}
		return v, s.audit(ctx, tx, a, "agents", r.AgentID, "save_version", r.Version)
	})
}
func (s *Service) PublishVersion(ctx context.Context, a Actor, r PublishRequest) (Agent, error) {
	if err := s.auth(ctx, a, "agent.publish", "agents", r.AgentID); err != nil {
		return Agent{}, err
	}
	return mutate(s, ctx, a, r.IdempotencyKey, "agent.publish/"+r.AgentID, r, func(tx Tx) (Agent, error) {
		v, e := read[Agent](ctx, tx, "agents", a.TenantID, r.AgentID)
		if e != nil {
			return v, e
		}
		if v.Revision != r.ExpectedRevision {
			return v, agentErrors.ErrManagementConflict
		}
		version, e := read[Version](ctx, tx, "versions", a.TenantID, versionID(v.ID, r.Version))
		if e != nil {
			return v, e
		}
		f := s.cfg.Factories[version.RunnerKind]
		if f == nil {
			return v, agentErrors.ErrManagementUnsupported
		}
		if _, e = f.Validate(ctx, version); e != nil {
			return v, wrapInvalid(e)
		}
		v.PublishedVersion = r.Version
		v.Revision++
		if e = update(ctx, tx, "agents", v, r.ExpectedRevision); e != nil {
			return v, e
		}
		return v, s.audit(ctx, tx, a, "agents", v.ID, "publish", r.Version)
	})
}
func (s *Service) SetAgentEnabled(ctx context.Context, a Actor, r SetEnabledRequest) (Agent, error) {
	if err := s.auth(ctx, a, "agent.enable", "agents", r.AgentID); err != nil {
		return Agent{}, err
	}
	return mutate(s, ctx, a, r.IdempotencyKey, "agent.enable/"+r.AgentID, r, func(tx Tx) (Agent, error) {
		v, e := read[Agent](ctx, tx, "agents", a.TenantID, r.AgentID)
		if e != nil {
			return v, e
		}
		if v.Revision != r.ExpectedRevision {
			return v, agentErrors.ErrManagementConflict
		}
		v.Enabled = r.Enabled
		v.Revision++
		if e = update(ctx, tx, "agents", v, r.ExpectedRevision); e != nil {
			return v, e
		}
		return v, s.audit(ctx, tx, a, "agents", v.ID, "enable", "")
	})
}
