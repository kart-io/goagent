package management

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"strings"
	"time"
)

func getAuthorized[T any](s *Service, ctx context.Context, a Actor, kind, action, id string) (v T, err error) {
	if err = ValidateActor(a); err != nil {
		return
	}
	d, err := s.cfg.Store.Get(ctx, kind, a.TenantID, id)
	if err != nil {
		return v, err
	}
	ref, err := resourceReference(kind, a.TenantID, id, d.Data)
	if err != nil {
		return v, err
	}
	if err = s.cfg.Authorizer.Authorize(ctx, a, action, ref); err != nil {
		if errors.Is(err, agentErrors.ErrManagementForbidden) {
			return v, agentErrors.ErrManagementNotFound
		}
		return v, err
	}
	err = json.Unmarshal(d.Data, &v)
	return
}
func (s *Service) GetAgent(c context.Context, a Actor, id string) (Agent, error) {
	return getAuthorized[Agent](s, c, a, "agents", "agent.read", id)
}
func (s *Service) GetSession(c context.Context, a Actor, id string) (Session, error) {
	return getAuthorized[Session](s, c, a, "sessions", "session.read", id)
}
func (s *Service) GetTask(c context.Context, a Actor, id string) (Task, error) {
	return getAuthorized[Task](s, c, a, "tasks", "task.read", id)
}
func (s *Service) GetApproval(c context.Context, a Actor, id string) (Approval, error) {
	ap, err := getAuthorized[Approval](s, c, a, "approvals", "approval.read", id)
	if err != nil {
		return ap, err
	}
	if ap.CommandID != "" {
		command, e := read[Command](c, s.cfg.Store, "commands", a.TenantID, ap.CommandID)
		if e != nil {
			return ap, e
		}
		ap.CommandState = command.State
	}
	return ap, nil
}

func (s *Service) GetCommand(ctx context.Context, a Actor, id string) (Command, error) {
	if err := ValidateActor(a); err != nil {
		return Command{}, err
	}
	c, err := read[Command](ctx, s.cfg.Store, "commands", a.TenantID, id)
	if err != nil {
		return c, err
	}
	if c.TaskID != "" {
		_, err = s.GetTask(ctx, a, c.TaskID)
	} else {
		_, err = s.GetSession(ctx, a, c.SessionID)
	}
	if err != nil {
		return Command{}, err
	}
	return c, nil
}
func listAuthorized[T any](s *Service, ctx context.Context, a Actor, kind, action string, q Query) (out Page[T], err error) {
	out.Items = []T{}
	if err = ValidateActor(a); err != nil {
		return
	}
	v, err := s.cfg.Authorizer.Scope(ctx, a, action, kind)
	if err != nil {
		return out, err
	}
	if q.Limit == 0 {
		q.Limit = s.cfg.Limits.PageSize
	}
	if q.Limit < 1 || q.Limit > s.cfg.Limits.MaxPageSize {
		return out, agentErrors.ErrManagementInvalid
	}
	cursor := q.Cursor
	q.Cursor = ""
	raw, _ := encode(struct {
		Actor  Actor
		Kind   string
		Query  Query
		Policy string
	}{a, kind, q, v.PolicyVersion})
	hash := digest(raw)
	if cursor != "" {
		decoded, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return out, agentErrors.ErrManagementInvalid
		}
		parts := strings.SplitN(string(decoded), "#", 2)
		if len(parts) != 2 || parts[0] != hash {
			return out, agentErrors.ErrManagementInvalid
		}
		q.Cursor = parts[1]
	}
	limit := q.Limit
	q.Limit++
	ds, err := s.cfg.Store.List(ctx, kind, Selection{TenantID: a.TenantID, Query: q, Visibility: v})
	if err != nil {
		return out, err
	}
	if len(ds) > limit {
		last := ds[limit-1]
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(hash + "#" + last.CreatedAt.Format(time.RFC3339Nano) + "|" + last.ID))
		ds = ds[:limit]
	}
	for _, d := range ds {
		var value T
		if err = json.Unmarshal(d.Data, &value); err != nil {
			return out, err
		}
		ref, e := resourceReference(kind, a.TenantID, d.ID, d.Data)
		if e != nil {
			return out, e
		}
		if err = s.cfg.Authorizer.Authorize(ctx, a, action, ref); err != nil {
			return out, agentErrors.ErrManagementForbidden
		}
		out.Items = append(out.Items, value)
	}
	return out, nil
}
func (s *Service) ListAgents(c context.Context, a Actor, q Query) (Page[Agent], error) {
	return listAuthorized[Agent](s, c, a, "agents", "agent.read", q)
}
func (s *Service) ListVersions(c context.Context, a Actor, q Query) (Page[Version], error) {
	return listAuthorized[Version](s, c, a, "versions", "agent.read", q)
}
func (s *Service) ListSessions(c context.Context, a Actor, q Query) (Page[Session], error) {
	return listAuthorized[Session](s, c, a, "sessions", "session.read", q)
}
func (s *Service) ListTasks(c context.Context, a Actor, q Query) (Page[Task], error) {
	return listAuthorized[Task](s, c, a, "tasks", "task.read", q)
}
func (s *Service) ListApprovals(c context.Context, a Actor, q Query) (Page[Approval], error) {
	page, err := listAuthorized[Approval](s, c, a, "approvals", "approval.read", q)
	if err != nil {
		return page, err
	}
	for i := range page.Items {
		ap := &page.Items[i]
		if ap.CommandID != "" {
			command, e := read[Command](c, s.cfg.Store, "commands", a.TenantID, ap.CommandID)
			if e != nil {
				return page, e
			}
			ap.CommandState = command.State
		}
	}
	return page, nil
}
func (s *Service) History(ctx context.Context, a Actor, r HistoryRequest) (HistoryPage, error) {
	v, err := s.GetSession(ctx, a, r.SessionID)
	if err != nil {
		return HistoryPage{}, err
	}
	if r.Limit == 0 {
		r.Limit = s.cfg.Limits.PageSize
	}
	if r.AfterSeq < 0 || r.Limit < 1 || r.Limit > s.cfg.Limits.MaxPageSize {
		return HistoryPage{}, agentErrors.ErrManagementInvalid
	}
	for _, typ := range r.Types {
		if typ == constants.SessionEventBoundarySnapshot || strings.HasPrefix(typ, "management/") {
			return HistoryPage{}, agentErrors.ErrManagementForbidden
		}
	}
	return s.cfg.HistoryReader.History(ctx, v.LogID, r.AfterSeq, r.Limit, r.Types)
}

type Run struct {
	DurationComplete   bool                      `json:"duration_complete"`
	UnfinishedAttempts int                       `json:"unfinished_attempts"`
	QueuedMillis       int64                     `json:"queued_millis"`
	ExecutionMillis    int64                     `json:"execution_millis"`
	WaitingMillis      int64                     `json:"waiting_millis"`
	ElapsedMillis      int64                     `json:"elapsed_millis"`
	TraceID            string                    `json:"trace_id,omitempty"`
	Task               Task                      `json:"task"`
	Attempts           []Attempt                 `json:"attempts"`
	Usage              *budget.AttributionReport `json:"usage"`
	Complete           bool                      `json:"complete"`
	UsageStatus        string                    `json:"usage_status"`
}

func (s *Service) GetRun(ctx context.Context, a Actor, id string) (Run, error) {
	t, err := getAuthorized[Task](s, ctx, a, "tasks", "run.read", id)
	if err != nil {
		return Run{}, err
	}
	se, err := read[Session](ctx, s.cfg.Store, "sessions", a.TenantID, t.SessionID)
	if err != nil {
		return Run{}, err
	}
	attempts, err := all[Attempt](ctx, s.cfg.Store, "attempts", a.TenantID, Query{TaskID: t.ID, Limit: 10000})
	if err != nil {
		return Run{}, err
	}
	out := Run{Task: t, Attempts: attempts}
	projectDurations(&out, s.cfg.Now())
	out.Usage, out.UsageStatus, err = readUsage(ctx, t.Budget, session.DurableStore(s.cfg.Sessions, se.LogID))
	if err != nil {
		return out, err
	}
	out.Complete = out.Usage != nil && out.Usage.Complete
	return out, nil
}
func (s *Service) ListRuns(ctx context.Context, a Actor, q Query) (Page[Run], error) {
	ts, err := listAuthorized[Task](s, ctx, a, "tasks", "run.read", q)
	out := Page[Run]{Items: []Run{}, NextCursor: ts.NextCursor}
	if err != nil {
		return out, err
	}
	for _, t := range ts.Items {
		r, e := s.GetRun(ctx, a, t.ID)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, r)
	}
	return out, nil
}
