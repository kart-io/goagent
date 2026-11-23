package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/execution"
)

type Service struct{ cfg Config }

func New(c Config) (*Service, error) {
	if c.Store == nil || c.Sessions == nil || c.HistoryReader == nil || c.Authorizer == nil || len(c.Factories) == 0 {
		return nil, agentErrors.ErrManagementInvalid
	}
	limits, err := c.Limits.normalize()
	if err != nil {
		return nil, err
	}
	c.Limits = limits
	if err := c.DefaultTaskBudget.validate(); err != nil {
		return nil, err
	}
	if _, err := execution.NewScope(execution.Config{RunID: "management/config-validation", Tracer: c.Tracer}); err != nil {
		return nil, agentErrors.ErrManagementInvalid
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	for name, f := range c.Factories {
		if name == "" || f == nil {
			return nil, agentErrors.ErrManagementInvalid
		}
	}
	copyFactories := make(map[string]Factory, len(c.Factories))
	for k, v := range c.Factories {
		copyFactories[k] = v
	}
	c.Factories = copyFactories
	return &Service{c}, nil
}

// ValidateRemoteTaskBudget reports whether remotely submitted tasks are
// bounded: it fails when DefaultTaskBudget is zero and
// UnlimitedRemoteTaskBudget is not set. Remote entry points call it at
// construction; hosts with their own remote entry point should do the same.
func (s *Service) ValidateRemoteTaskBudget() error {
	if s == nil {
		return agentErrors.ErrManagementInvalid
	}
	if s.cfg.DefaultTaskBudget == (TaskBudget{}) && !s.cfg.UnlimitedRemoteTaskBudget {
		return fmt.Errorf("%w：远程入口要求配置 DefaultTaskBudget，或显式设置 UnlimitedRemoteTaskBudget", agentErrors.ErrManagementInvalid)
	}
	return nil
}

func (s *Service) meta(a Actor) Meta { return Meta{uuid.NewString(), a.TenantID, 1, s.cfg.Now().UTC()} }

func digest(raw []byte) string              { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func encode(v any) (json.RawMessage, error) { return json.Marshal(v) }
func document(kind string, v any) (Document, error) {
	raw, err := encode(v)
	if err != nil {
		return Document{}, err
	}
	var m Meta
	if err = json.Unmarshal(raw, &m); err != nil {
		return Document{}, err
	}
	return Document{kind, m.TenantID, m.ID, m.Revision, m.CreatedAt, raw}, nil
}

func insert(ctx context.Context, tx Tx, kind string, v any) error {
	d, err := document(kind, v)
	if err != nil {
		return err
	}
	return tx.Insert(ctx, d)
}

func update(ctx context.Context, tx Tx, kind string, v any, expected int64) error {
	d, err := document(kind, v)
	if err != nil {
		return err
	}
	return tx.CAS(ctx, d, expected)
}

func read[T any](ctx context.Context, r Reader, kind, tenant, id string) (v T, err error) {
	d, err := r.Get(ctx, kind, tenant, id)
	if err == nil {
		err = json.Unmarshal(d.Data, &v)
	}
	return
}

// page reads one Store page as given. Callers that modify the rows they read
// so that they leave the query set may loop on it from the head.
func page[T any](ctx context.Context, r Reader, kind, tenant string, q Query) ([]T, error) {
	ds, err := r.List(ctx, kind, Selection{TenantID: tenant, Query: q, Visibility: Visibility{All: true}})
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(ds))
	for _, d := range ds {
		var v T
		if err = json.Unmarshal(d.Data, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// scanPage bounds one internal Store read; internal scans follow the cursor
// until the query set is exhausted, so it is not a limit on the result.
const scanPage = 500

// each walks every matching row with the Store's CreatedAt|ID cursor and stops
// early when fn returns true. q.Limit and q.Cursor are owned by the walk.
func each[T any](ctx context.Context, r Reader, kind, tenant string, q Query, fn func(T) (bool, error)) error {
	q.Limit, q.Cursor = scanPage, ""
	for {
		ds, err := r.List(ctx, kind, Selection{TenantID: tenant, Query: q, Visibility: Visibility{All: true}})
		if err != nil {
			return err
		}
		for _, d := range ds {
			var v T
			if err = json.Unmarshal(d.Data, &v); err != nil {
				return err
			}
			stop, err := fn(v)
			if err != nil || stop {
				return err
			}
		}
		if len(ds) < q.Limit {
			return nil
		}
		last := ds[len(ds)-1]
		q.Cursor = last.CreatedAt.Format(time.RFC3339Nano) + "|" + last.ID
	}
}

// all returns every matching row; it never truncates at a fixed scan size.
func all[T any](ctx context.Context, r Reader, kind, tenant string, q Query) ([]T, error) {
	out := []T{}
	err := each(ctx, r, kind, tenant, q, func(v T) (bool, error) { out = append(out, v); return false, nil })
	return out, err
}

func (s *Service) auth(ctx context.Context, a Actor, action, kind, id string) error {
	if err := ValidateActor(a); err != nil {
		return err
	}
	ref := ResourceRef{Type: kind, ID: id, TenantID: a.TenantID}
	if id != "" && kind != "workers" {
		d, err := s.cfg.Store.Get(ctx, kind, a.TenantID, id)
		if err != nil {
			return err
		}
		ref, err = resourceReference(kind, a.TenantID, id, d.Data)
		if err != nil {
			return err
		}
	}
	return s.cfg.Authorizer.Authorize(ctx, a, action, ref)
}

func (s *Service) audit(ctx context.Context, tx Tx, a Actor, kind, id, op, reason string) error {
	return insert(ctx, tx, "audit", Audit{Meta: s.meta(a), ResourceType: kind, ResourceID: id, Operation: op, Actor: a, Reason: reason})
}

func mutate[T any](s *Service, ctx context.Context, a Actor, key, op string, request any, fn func(Tx) (T, error)) (out T, err error) {
	if ValidateActor(a) != nil {
		return out, agentErrors.ErrManagementUnauthenticated
	}
	if key == "" || len(key) > 256 {
		return out, agentErrors.ErrManagementInvalid
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return out, agentErrors.ErrManagementInvalid
	}
	raw, err = CanonicalJSON(raw)
	if err != nil {
		return out, err
	}
	hash := digest(raw)
	id := digest([]byte(op + "\x00" + key))
	err = s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		old, e := read[Idempotency](ctx, tx, "idempotency", a.TenantID, id)
		if e == nil && (old.ExpiresAt.IsZero() || old.ExpiresAt.After(s.cfg.Now())) {
			if old.Hash != hash {
				return agentErrors.ErrManagementConflict
			}
			return json.Unmarshal(old.Response, &out)
		}
		if e != nil && !errors.Is(e, agentErrors.ErrManagementNotFound) {
			return e
		}
		if e == nil {
			if e = tx.Delete(ctx, "idempotency", a.TenantID, id, old.Revision); e != nil {
				return e
			}
		}
		out, e = fn(tx)
		if e != nil {
			return e
		}
		response, e := encode(out)
		if e != nil {
			return e
		}
		m := s.meta(a)
		m.ID = id
		var expires time.Time
		if s.cfg.Limits.IdempotencyRetention > 0 {
			expires = s.cfg.Now().Add(s.cfg.Limits.IdempotencyRetention)
		}
		return insert(ctx, tx, "idempotency", Idempotency{Meta: m, Operation: op, Key: key, Hash: hash, Response: response, ExpiresAt: expires})
	})
	return
}

// CanonicalJSON 使用精确数值并拒绝重复键和多份 JSON，作为幂等比较的输入。
func CanonicalJSON(raw []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(raw, &value, json.Strict, json.UseNumber); err != nil {
		return nil, agentErrors.ErrManagementInvalid
	}
	return json.Marshal(value)
}

func validText(s string, max int) bool {
	return s != "" && strings.TrimSpace(s) == s && len([]rune(s)) <= max
}
func versionID(agent, version string) string { return agent + "@" + version }
func (s *Service) command(ctx context.Context, tx Tx, a Actor, kind, task, session string) (Command, error) {
	c := Command{Meta: s.meta(a), Kind: kind, TaskID: task, SessionID: session, State: "pending", Actor: a, AvailableAt: s.cfg.Now().UTC()}
	return c, insert(ctx, tx, "commands", c)
}

func wrapInvalid(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w：%v", agentErrors.ErrManagementInvalid, err)
}
