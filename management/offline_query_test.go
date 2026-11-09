package management_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/core/usage"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/stretchr/testify/require"
)

type offlineQueryPolicy struct {
	visibility mg.Visibility
	scopeErr   error
	authorize  func(mg.Actor, string, mg.ResourceRef) error
}

func (p *offlineQueryPolicy) Scope(context.Context, mg.Actor, string, string) (mg.Visibility, error) {
	return p.visibility, p.scopeErr
}
func (p *offlineQueryPolicy) Authorize(_ context.Context, a mg.Actor, action string, ref mg.ResourceRef) error {
	if p.authorize != nil {
		return p.authorize(a, action, ref)
	}
	return nil
}

func offlineQueryMeta(id, tenant string) mg.Meta {
	return mg.Meta{ID: id, TenantID: tenant, Revision: 1, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func TestOfflineQueryAuthorizationRelations(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	meta := func(id string) mg.Meta { return offlineQueryMeta(id, f.Actor.TenantID) }
	for kind, value := range map[string]any{
		"agents":    mg.Agent{Meta: meta("agent")},
		"sessions":  mg.Session{Meta: meta("session"), AgentID: "agent", LogID: "log"},
		"tasks":     mg.Task{Meta: meta("task"), AgentID: "agent", SessionID: "session"},
		"approvals": mg.Approval{Meta: meta("approval"), AgentID: "agent", SessionID: "session", TaskID: "task", CommandID: "command"},
		"commands":  mg.Command{Meta: meta("command"), TaskID: "task", SessionID: "session", State: "applied"},
	} {
		require.NoError(t, f.Store.Put(ctx, kind, value))
	}
	require.NoError(t, f.Store.Put(ctx, "commands", mg.Command{Meta: meta("session-command"), SessionID: "session", State: "pending"}))
	policy := &offlineQueryPolicy{visibility: mg.Visibility{All: true, PolicyVersion: "v1"}}
	cfg := f.Config()
	cfg.Authorizer = policy
	svc, err := mg.New(cfg)
	require.NoError(t, err)
	queries := []struct {
		name, kind, id, action, sessionID string
		query                             func(mg.Actor) error
	}{
		{"代理", "agents", "agent", "agent.read", "", func(a mg.Actor) error { _, e := svc.GetAgent(ctx, a, "agent"); return e }},
		{"会话", "sessions", "session", "session.read", "session", func(a mg.Actor) error { _, e := svc.GetSession(ctx, a, "session"); return e }},
		{"任务", "tasks", "task", "task.read", "session", func(a mg.Actor) error { _, e := svc.GetTask(ctx, a, "task"); return e }},
		{"审批", "approvals", "approval", "approval.read", "session", func(a mg.Actor) error { _, e := svc.GetApproval(ctx, a, "approval"); return e }},
		{"任务命令", "tasks", "task", "task.read", "session", func(a mg.Actor) error { _, e := svc.GetCommand(ctx, a, "command"); return e }},
		{"会话命令", "sessions", "session", "session.read", "session", func(a mg.Actor) error { _, e := svc.GetCommand(ctx, a, "session-command"); return e }},
		{"运行", "tasks", "task", "run.read", "session", func(a mg.Actor) error { _, e := svc.GetRun(ctx, a, "task"); return e }},
	}
	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			policy.authorize = func(a mg.Actor, action string, ref mg.ResourceRef) error {
				calls++
				require.Equal(t, f.Actor, a)
				require.Equal(t, tc.action, action)
				require.Equal(t, mg.ResourceRef{Type: tc.kind, ID: tc.id, TenantID: a.TenantID, AgentID: "agent", SessionID: tc.sessionID}, ref)
				return nil
			}
			require.NoError(t, tc.query(f.Actor))
			require.Equal(t, 1, calls)
			require.ErrorIs(t, tc.query(mg.Actor{}), agentErrors.ErrManagementUnauthenticated)
			require.ErrorIs(t, tc.query(mg.Actor{TenantID: "other", Subject: f.Actor.Subject}), agentErrors.ErrManagementNotFound)
			require.Equal(t, 1, calls, "无身份和跨租户查询不得进入资源授权")
			policy.authorize = func(mg.Actor, string, mg.ResourceRef) error { return agentErrors.ErrManagementForbidden }
			require.ErrorIs(t, tc.query(f.Actor), agentErrors.ErrManagementNotFound, "详情不能泄露被拒绝资源是否存在")
			backendErr := errors.New("授权服务不可用")
			policy.authorize = func(mg.Actor, string, mg.ResourceRef) error { return backendErr }
			require.ErrorIs(t, tc.query(f.Actor), backendErr)
		})
	}
	policy.authorize = nil
	ap, err := svc.GetApproval(ctx, f.Actor, "approval")
	require.NoError(t, err)
	require.Equal(t, "applied", ap.CommandState)
	page, err := svc.ListApprovals(ctx, f.Actor, mg.Query{TaskID: "task"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, ap, page.Items[0])
	for _, query := range []func() error{
		func() error { _, e := svc.GetApproval(ctx, f.Actor, "approval"); return e },
		func() error { _, e := svc.ListApprovals(ctx, f.Actor, mg.Query{}); return e },
	} {
		backendErr := errors.New("命令读取失败")
		f.Store.FailNext("get", "commands", backendErr)
		require.ErrorIs(t, query(), backendErr)
	}
}

func TestOfflineQueryCursorBindsIdentityFiltersAndPolicy(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	// 隐藏记录排在可见记录前，验证权限过滤发生在分页之前；相同时间用 ID 打破平局。
	for _, row := range []struct {
		id, tenant, agent, se string
		state                 mg.TaskState
	}{
		{"00-hidden", f.Actor.TenantID, "hidden", "hidden", mg.Succeeded},
		{"01-other-tenant", "other", "allowed", "session", mg.Succeeded},
		{"02-wrong-state", f.Actor.TenantID, "allowed", "session", mg.Failed},
		{"03-other-session", f.Actor.TenantID, "allowed", "other", mg.Succeeded},
		{"a", f.Actor.TenantID, "allowed", "session", mg.Succeeded},
		{"b", f.Actor.TenantID, "allowed", "session", mg.Succeeded},
		{"c", f.Actor.TenantID, "allowed", "session", mg.Succeeded},
	} {
		require.NoError(t, f.Store.Put(ctx, "tasks", mg.Task{Meta: offlineQueryMeta(row.id, row.tenant), AgentID: row.agent, SessionID: row.se, State: row.state}))
	}
	policy := &offlineQueryPolicy{visibility: mg.Visibility{AgentIDs: []string{"allowed"}, PolicyVersion: "v1"}}
	policy.authorize = func(_ mg.Actor, action string, ref mg.ResourceRef) error {
		require.Equal(t, "task.read", action)
		require.Equal(t, "allowed", ref.AgentID)
		return nil
	}
	cfg := f.Config()
	cfg.Authorizer = policy
	cfg.Limits = mg.DefaultLimits()
	cfg.Limits.PageSize = 2
	svc, err := mg.New(cfg)
	require.NoError(t, err)
	at := offlineQueryMeta("", "").CreatedAt
	q := mg.Query{SessionID: "session", Statuses: []string{string(mg.Succeeded)}, CreatedFrom: at, CreatedTo: at, Limit: 2}
	first, err := svc.ListTasks(ctx, f.Actor, q)
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	require.Equal(t, []string{"a", "b"}, []string{first.Items[0].ID, first.Items[1].ID})
	require.NotEmpty(t, first.NextCursor)
	q.Cursor = first.NextCursor
	last, err := svc.ListTasks(ctx, f.Actor, q)
	require.NoError(t, err)
	require.Len(t, last.Items, 1)
	require.Equal(t, "c", last.Items[0].ID)
	require.Empty(t, last.NextCursor)
	for _, tc := range []struct {
		name   string
		mutate func(*mg.Actor, *mg.Query)
	}{
		{"主体", func(a *mg.Actor, _ *mg.Query) { a.Subject = "another" }},
		{"租户", func(a *mg.Actor, _ *mg.Query) { a.TenantID = "other" }},
		{"代理过滤", func(_ *mg.Actor, q *mg.Query) { q.AgentID = "allowed" }},
		{"会话过滤", func(_ *mg.Actor, q *mg.Query) { q.SessionID = "other" }},
		{"任务过滤", func(_ *mg.Actor, q *mg.Query) { q.TaskID = "c" }},
		{"状态过滤", func(_ *mg.Actor, q *mg.Query) { q.Statuses = []string{string(mg.Failed)} }},
		{"起始时间", func(_ *mg.Actor, q *mg.Query) { q.CreatedFrom = at.Add(-time.Second) }},
		{"截止时间", func(_ *mg.Actor, q *mg.Query) { q.CreatedTo = at.Add(time.Second) }},
		{"页大小", func(_ *mg.Actor, q *mg.Query) { q.Limit = 1 }},
		{"非法编码", func(_ *mg.Actor, q *mg.Query) { q.Cursor = "%%%" }},
		{"缺少绑定", func(_ *mg.Actor, q *mg.Query) { q.Cursor = base64.RawURLEncoding.EncodeToString([]byte("位置")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, changed := f.Actor, q
			tc.mutate(&a, &changed)
			_, err := svc.ListTasks(ctx, a, changed)
			require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
		})
	}
	_, err = svc.ListSessions(ctx, f.Actor, q)
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid, "游标不能跨资源种类使用")
	policy.visibility.PolicyVersion = "v2"
	_, err = svc.ListTasks(ctx, f.Actor, q)
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	q.Cursor, q.Limit = "", 0
	page, err := svc.ListTasks(ctx, f.Actor, q)
	require.NoError(t, err)
	require.Len(t, page.Items, 2, "默认页大小必须生效")
	policy.visibility = mg.Visibility{PolicyVersion: "deny-all"}
	page, err = svc.ListTasks(ctx, f.Actor, q)
	require.NoError(t, err)
	require.NotNil(t, page.Items)
	require.Empty(t, page.Items)
	require.Empty(t, page.NextCursor)
}

func TestOfflineQueryListFailuresAndPublicLists(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	a, v := f.Register(t)
	agents, err := f.Service.ListAgents(ctx, f.Actor, mg.Query{})
	require.NoError(t, err)
	require.Equal(t, []mg.Agent{a}, agents.Items)
	versions, err := f.Service.ListVersions(ctx, f.Actor, mg.Query{AgentID: a.ID})
	require.NoError(t, err)
	require.Equal(t, []mg.Version{v}, versions.Items)
	policy := &offlineQueryPolicy{visibility: mg.Visibility{All: true}}
	cfg := f.Config()
	cfg.Authorizer = policy
	svc, err := mg.New(cfg)
	require.NoError(t, err)
	_, err = svc.ListAgents(ctx, mg.Actor{}, mg.Query{})
	require.ErrorIs(t, err, agentErrors.ErrManagementUnauthenticated)
	for _, limit := range []int{-1, 201} {
		_, err = svc.ListAgents(ctx, f.Actor, mg.Query{Limit: limit})
		require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	}
	backendErr := errors.New("查询存储失败")
	policy.scopeErr = backendErr
	_, err = svc.ListAgents(ctx, f.Actor, mg.Query{})
	require.ErrorIs(t, err, backendErr)
	policy.scopeErr = nil
	f.Store.FailNext("list", "agents", backendErr)
	_, err = svc.ListAgents(ctx, f.Actor, mg.Query{})
	require.ErrorIs(t, err, backendErr)
	policy.authorize = func(mg.Actor, string, mg.ResourceRef) error { return agentErrors.ErrManagementForbidden }
	page, err := svc.ListAgents(ctx, f.Actor, mg.Query{})
	require.ErrorIs(t, err, agentErrors.ErrManagementForbidden, "范围授权不能绕过逐项授权")
	require.Empty(t, page.Items)
}

func TestOfflineQueryHistoryFiltersAndAuthorization(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	before, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	seq := session.LastSeq(before)
	for _, typ := range []string{"business/input", "management/private", constants.SessionEventBoundarySnapshot, "business/output", "business/input"} {
		ev, e := session.NewEvent(typ, map[string]string{"内容": typ})
		require.NoError(t, e)
		seq, e = f.Sessions.Append(ctx, se.LogID, seq, ev)
		require.NoError(t, e)
	}
	all, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	start := session.LastSeq(before)
	page, err := f.Service.History(ctx, f.Actor, mg.HistoryRequest{SessionID: se.ID, AfterSeq: start, Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []session.Event{all[len(before)], all[len(before)+3]}, page.Items)
	require.Equal(t, start+4, page.NextSeq)
	next, err := f.Service.History(ctx, f.Actor, mg.HistoryRequest{SessionID: se.ID, AfterSeq: page.NextSeq})
	require.NoError(t, err)
	require.Equal(t, []session.Event{all[len(before)+4]}, next.Items)
	filtered, err := f.Service.History(ctx, f.Actor, mg.HistoryRequest{SessionID: se.ID, AfterSeq: start, Types: []string{"business/input"}})
	require.NoError(t, err)
	require.Equal(t, []session.Event{all[len(before)], all[len(before)+4]}, filtered.Items)
	empty, err := f.Service.History(ctx, f.Actor, mg.HistoryRequest{SessionID: se.ID, AfterSeq: seq})
	require.NoError(t, err)
	require.Empty(t, empty.Items)
	require.Equal(t, seq, empty.NextSeq)
	for _, req := range []mg.HistoryRequest{{AfterSeq: -1}, {Limit: -1}, {Limit: 201}} {
		req.SessionID = se.ID
		_, err = f.Service.History(ctx, f.Actor, req)
		require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	}
	for _, typ := range []string{constants.SessionEventBoundarySnapshot, "management/private"} {
		_, err = f.Service.History(ctx, f.Actor, mg.HistoryRequest{SessionID: se.ID, Types: []string{typ}})
		require.ErrorIs(t, err, agentErrors.ErrManagementForbidden)
	}
	_, err = f.Service.History(ctx, mg.Actor{TenantID: "other", Subject: "reader"}, mg.HistoryRequest{SessionID: se.ID})
	require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
	cfg := f.Config()
	cfg.Authorizer = &offlineQueryPolicy{authorize: func(mg.Actor, string, mg.ResourceRef) error { return agentErrors.ErrManagementForbidden }}
	svc, err := mg.New(cfg)
	require.NoError(t, err)
	_, err = svc.History(ctx, f.Actor, mg.HistoryRequest{SessionID: se.ID})
	require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
	after, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	require.Equal(t, all, after)
}

// 查询使用真实日志读取；拦截所有写入口和锁，避免失败后被吞掉的写入尝试逃过快照比较。
type offlineQueryLog struct {
	session.Store
	writes, locks int
	readErr       error
	cancelRead    context.CancelFunc
}

func (s *offlineQueryLog) Read(ctx context.Context, id string, after int64) ([]session.Event, error) {
	if s.cancelRead != nil {
		s.cancelRead()
		return nil, ctx.Err()
	}
	if s.readErr != nil {
		return nil, s.readErr
	}
	return s.Store.Read(ctx, id, after)
}
func (s *offlineQueryLog) Append(context.Context, string, int64, ...session.Event) (int64, error) {
	s.writes++
	return 0, errors.New("查询禁止追加日志")
}
func (s *offlineQueryLog) AppendAll(context.Context, ...session.AppendBatch) ([]int64, error) {
	s.writes++
	return nil, errors.New("查询禁止批量追加日志")
}
func (s *offlineQueryLog) Fork(context.Context, string, string, int64) error {
	s.writes++
	return errors.New("查询禁止分叉日志")
}
func (s *offlineQueryLog) Lock(context.Context, string, string) (func() error, error) {
	s.locks++
	return nil, errors.New("查询禁止获取执行锁")
}

type offlineQueryStore struct {
	mg.Store
	transactions int
}

func (s *offlineQueryStore) WithinTx(context.Context, func(mg.Tx) error) error {
	s.transactions++
	return errors.New("查询禁止写事务")
}

func offlineQueryDocuments(t *testing.T, f *managementtest.Fixture) map[string][]mg.Document {
	t.Helper()
	out := map[string][]mg.Document{}
	for _, kind := range mg.Kinds {
		docs, err := f.Store.List(context.Background(), kind, mg.Selection{Visibility: mg.Visibility{All: true}})
		require.NoError(t, err)
		out[kind] = docs
	}
	return out
}

func offlineQueryTask(t *testing.T, f *managementtest.Fixture, se mg.Session, id string) mg.Task {
	t.Helper()
	task := mg.Task{Meta: offlineQueryMeta(id, f.Actor.TenantID), AgentID: se.AgentID, SessionID: se.ID, State: mg.Succeeded, Budget: budget.Config{RootKey: id, Version: "v1", MaxModelRequests: 10, MaxAgentCalls: 3}}
	end := task.CreatedAt.Add(10 * time.Second)
	task.FinishedAt = &end
	require.NoError(t, f.Store.Put(context.Background(), "tasks", task))
	return task
}

func offlineQueryEvidence(t *testing.T, f *managementtest.Fixture, se mg.Session, task mg.Task, partial bool) {
	t.Helper()
	ctx := context.Background()
	c, err := budget.NewDurable(task.Budget, session.DurableStore(f.Sessions, se.LogID))
	require.NoError(t, err)
	root := usage.Owner{RunID: task.ID, AgentID: task.AgentID, AgentCallKey: task.ID}
	for i, entry := range []struct {
		chain  []usage.Owner
		tokens int
	}{
		{[]usage.Owner{root}, 7},
		{[]usage.Owner{root, {RunID: task.ID, AgentID: "child", AgentCallKey: task.ID + "/child", ParentAgentCallKey: task.ID}}, 5},
		{nil, 3},
	} {
		if i == 2 && !partial {
			continue
		}
		key := fmt.Sprint(i)
		require.NoError(t, c.ReserveAttributed(ctx, key, 0, entry.chain))
		evidence := usage.Evidence{KnownFields: usage.BasicFields, Tokens: interfaces.TokenUsage{PromptTokens: entry.tokens - 1, CompletionTokens: 1, TotalTokens: entry.tokens}, Source: "provider"}
		if partial {
			evidence.KnownFields = usage.Total
			evidence.Tokens = interfaces.TokenUsage{TotalTokens: entry.tokens}
		}
		_, err = c.RecordEvidence(ctx, key, entry.chain, evidence)
		require.NoError(t, err)
		// 重放相同证据不应变成另一笔消耗，管理汇总也不能累计历史快照。
		_, err = c.RecordEvidence(ctx, key, entry.chain, evidence)
		require.NoError(t, err)
	}
	if partial {
		require.NoError(t, c.ReserveAttributed(ctx, "unknown", 0, []usage.Owner{root}))
	}
}

func TestOfflineQueryRunEvidenceSummaryAndReadOnly(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	full := offlineQueryTask(t, f, se, "a-full")
	partial := offlineQueryTask(t, f, se, "b-partial")
	missing := offlineQueryTask(t, f, se, "c-missing")
	offlineQueryEvidence(t, f, se, full, false)
	offlineQueryEvidence(t, f, se, partial, true)
	finish := full.CreatedAt.Add(4 * time.Second)
	require.NoError(t, f.Store.Put(ctx, "attempts", mg.Attempt{Meta: mg.Meta{ID: "attempt", TenantID: f.Actor.TenantID, Revision: 1, CreatedAt: full.CreatedAt.Add(time.Second)}, TaskID: full.ID, Number: 1, FinishedAt: &finish, Outcome: string(mg.WaitingInput), TraceID: "trace-evidence"}))
	before, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	require.Greater(t, len(before), 6, "必须存在真实预算快照证据")
	docs := offlineQueryDocuments(t, f)
	log := &offlineQueryLog{Store: f.Sessions}
	store := &offlineQueryStore{Store: f.Store}
	cfg := f.Config()
	cfg.Sessions, cfg.Store = log, store
	cfg.Now = func() time.Time { return full.CreatedAt.Add(time.Hour) }
	svc, err := mg.New(cfg)
	require.NoError(t, err)
	for range 3 {
		run, err := svc.GetRun(ctx, f.Actor, full.ID)
		require.NoError(t, err)
		require.Equal(t, "available", run.UsageStatus)
		require.True(t, run.Complete)
		require.NotNil(t, run.Usage)
		require.Equal(t, 12, run.Usage.Root.KnownTokens.TotalTokens)
		require.Equal(t, int64(1000), run.QueuedMillis)
		require.Equal(t, int64(3000), run.ExecutionMillis)
		require.Equal(t, int64(6000), run.WaitingMillis)
		require.Equal(t, int64(10000), run.ElapsedMillis)
		require.True(t, run.DurationComplete)
		require.Equal(t, "trace-evidence", run.TraceID)
		page, err := svc.ListRuns(ctx, f.Actor, mg.Query{SessionID: se.ID, Limit: 2})
		require.NoError(t, err)
		require.Len(t, page.Items, 2)
		require.Equal(t, run, page.Items[0])
		require.Equal(t, "partial", page.Items[1].UsageStatus)
		require.False(t, page.Items[1].Complete)
		require.Equal(t, 1, page.Items[1].Usage.Root.UnknownUsageEntries)
		next, err := svc.ListRuns(ctx, f.Actor, mg.Query{SessionID: se.ID, Limit: 2, Cursor: page.NextCursor})
		require.NoError(t, err)
		require.Len(t, next.Items, 1)
		require.Equal(t, missing.ID, next.Items[0].Task.ID)
		require.Equal(t, "missing", next.Items[0].UsageStatus)
		require.Nil(t, next.Items[0].Usage)
		require.False(t, next.Items[0].Complete)
		require.Empty(t, next.NextCursor)
		summary, err := svc.SummarizeRuns(ctx, f.Actor, mg.Query{SessionID: se.ID, Limit: 1})
		require.NoError(t, err)
		require.Equal(t, cfg.Now(), summary.CreatedTo)
		require.Equal(t, 3, summary.Totals.Tasks, "统计必须扫描全部页")
		require.Equal(t, 1, summary.Totals.Attempts)
		require.Equal(t, 3, summary.Totals.States[mg.Succeeded])
		require.Equal(t, 2, summary.Totals.KnownRuns)
		require.Equal(t, 1, summary.Totals.UnknownRuns)
		require.Equal(t, 1, summary.Totals.PartialRuns)
		require.False(t, summary.Totals.Complete)
		require.NotNil(t, summary.Totals.KnownTokens)
		require.Equal(t, 27, summary.Totals.KnownTokens.TotalTokens)
		require.Len(t, summary.Sessions, 1)
		require.Equal(t, summary.Totals, summary.Sessions[0].RunTotals)
		require.Len(t, summary.Agents, 1)
		require.Equal(t, 14, summary.Agents[0].KnownTokens.TotalTokens, "顶层代理只累计自身，不能重复算子代理")
		require.Len(t, summary.AgentUsage, 2)
		byAgent := map[string]mg.AgentUsageTotal{}
		for _, agent := range summary.AgentUsage {
			byAgent[agent.AgentID] = agent
		}
		require.Equal(t, 10, byAgent["child"].KnownTokens.TotalTokens)
		require.Equal(t, 1, byAgent[se.AgentID].UnknownEntries)
		require.Equal(t, 1, byAgent[se.AgentID].PartialEntries)
		require.False(t, byAgent[se.AgentID].Complete)
		require.Equal(t, 3, summary.Unattributed.TotalTokens)
		require.Equal(t, 1, summary.UnattributedKnownEntries)
		require.Equal(t, 1, summary.UnattributedPartialEntries)
		require.False(t, summary.UnattributedComplete)
	}
	after, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	require.Equal(t, before, after, "查询不得改写真实日志的序号、内容或时间")
	require.Equal(t, docs, offlineQueryDocuments(t, f), "查询不得改变任务、命令、审计或其他管理记录")
	require.Zero(t, log.writes)
	require.Zero(t, log.locks)
	require.Zero(t, store.transactions)
}

func TestOfflineQueryUsageUnavailableAndCancellation(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	task := offlineQueryTask(t, f, se, "usage-failure")
	offlineQueryEvidence(t, f, se, task, false)
	log := &offlineQueryLog{Store: f.Sessions}
	cfg := f.Config()
	cfg.Sessions = log
	svc, err := mg.New(cfg)
	require.NoError(t, err)
	for _, fault := range []string{"读取失败", "账本损坏"} {
		t.Run(fault, func(t *testing.T) {
			log.readErr = nil
			if fault == "读取失败" {
				log.readErr = errors.New("日志不可读")
			} else {
				events, err := f.Sessions.Read(ctx, se.LogID, 0)
				require.NoError(t, err)
				var boundary session.BoundaryData
				for _, ev := range events {
					if ev.Type == constants.SessionEventBoundarySnapshot {
						var b session.BoundaryData
						require.NoError(t, json.Unmarshal(ev.Data, &b))
						if strings.HasPrefix(b.ID, "goagent/budget/") {
							boundary = b
						}
					}
				}
				require.NotEmpty(t, boundary.ID)
				boundary.Snapshot = json.RawMessage(`{"version":"损坏格式","data":{}}`)
				ev, err := session.NewEvent(constants.SessionEventBoundarySnapshot, boundary)
				require.NoError(t, err)
				_, err = f.Sessions.Append(ctx, se.LogID, session.LastSeq(events), ev)
				require.NoError(t, err)
			}
			before, err := f.Sessions.Read(ctx, se.LogID, 0)
			require.NoError(t, err)
			run, err := svc.GetRun(ctx, f.Actor, task.ID)
			require.NoError(t, err)
			require.Equal(t, "unavailable", run.UsageStatus)
			require.Nil(t, run.Usage)
			require.False(t, run.Complete)
			summary, err := svc.SummarizeRuns(ctx, f.Actor, mg.Query{})
			require.NoError(t, err)
			require.Equal(t, 1, summary.Totals.UnknownRuns)
			require.Nil(t, summary.Totals.KnownTokens, "未知不能伪装成已知零消耗")
			require.False(t, summary.Totals.Complete)
			after, err := f.Sessions.Read(ctx, se.LogID, 0)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = svc.GetRun(canceled, f.Actor, task.ID)
	require.ErrorIs(t, err, context.Canceled)
	_, err = svc.ListRuns(canceled, f.Actor, mg.Query{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = svc.SummarizeRuns(canceled, f.Actor, mg.Query{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = svc.SummarizeRuns(ctx, f.Actor, mg.Query{Cursor: "不可使用分页游标"})
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	for _, failure := range []struct{ op, kind string }{{"get", "sessions"}, {"list", "attempts"}, {"get", "tasks"}} {
		backendErr := errors.New("运行查询存储失败")
		f.Store.FailNext(failure.op, failure.kind, backendErr)
		_, err = svc.GetRun(ctx, f.Actor, task.ID)
		require.ErrorIs(t, err, backendErr)
	}
	require.Zero(t, log.writes)
	require.Zero(t, log.locks)
}

func TestOfflineQuerySummaryRespectsResourceScope(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	visible := offlineQueryTask(t, f, se, "b-visible")
	hidden := offlineQueryTask(t, f, se, "a-hidden")
	offlineQueryEvidence(t, f, se, visible, false)
	offlineQueryEvidence(t, f, se, hidden, true)
	policy := &offlineQueryPolicy{visibility: mg.Visibility{ResourceIDs: []string{visible.ID}, PolicyVersion: "resource-v1"}}
	policy.authorize = func(_ mg.Actor, action string, ref mg.ResourceRef) error {
		require.Equal(t, "run.read", action)
		if ref.ID != visible.ID {
			return agentErrors.ErrManagementForbidden
		}
		return nil
	}
	cfg := f.Config()
	cfg.Authorizer = policy
	svc, err := mg.New(cfg)
	require.NoError(t, err)
	page, err := svc.ListRuns(ctx, f.Actor, mg.Query{Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, visible.ID, page.Items[0].Task.ID)
	require.Empty(t, page.NextCursor, "不可见记录不得占用分页名额或泄漏游标")
	_, err = svc.GetRun(ctx, f.Actor, hidden.ID)
	require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
	summary, err := svc.SummarizeRuns(ctx, f.Actor, mg.Query{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, 1, summary.Totals.Tasks)
	require.Equal(t, 12, summary.Totals.KnownTokens.TotalTokens)
	require.True(t, summary.Totals.Complete)
	require.Equal(t, 7, summary.Agents[0].KnownTokens.TotalTokens)
	require.Zero(t, summary.UnattributedKnownEntries)
	require.Zero(t, summary.Unattributed.TotalTokens)
	require.True(t, summary.UnattributedComplete)
	policy.visibility = mg.Visibility{PolicyVersion: "none"}
	summary, err = svc.SummarizeRuns(ctx, f.Actor, mg.Query{})
	require.NoError(t, err)
	require.Zero(t, summary.Totals.Tasks)
	require.Nil(t, summary.Totals.KnownTokens)
	require.Empty(t, summary.Agents)
	require.Empty(t, summary.Sessions)
	policy.visibility = mg.Visibility{SessionIDs: []string{se.ID}, PolicyVersion: "session-v1"}
	policy.authorize = nil
	summary, err = svc.SummarizeRuns(ctx, f.Actor, mg.Query{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, 2, summary.Totals.Tasks)
	require.Equal(t, 27, summary.Totals.KnownTokens.TotalTokens)
	backendErr := errors.New("运行详情读取失败")
	f.Store.FailNext("get", "sessions", backendErr)
	_, err = svc.ListRuns(ctx, f.Actor, mg.Query{})
	require.ErrorIs(t, err, backendErr)
	policy.scopeErr = backendErr
	_, err = svc.SummarizeRuns(ctx, f.Actor, mg.Query{})
	require.ErrorIs(t, err, backendErr)
}

func TestOfflineQueryUnknownLedgerAndMidReadCancellation(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	task := offlineQueryTask(t, f, se, "unknown-evidence")
	require.NoError(t, f.Store.Put(ctx, "attempts", mg.Attempt{Meta: offlineQueryMeta("unfinished-attempt", f.Actor.TenantID), TaskID: task.ID, Number: 1}))
	c, err := budget.NewDurable(task.Budget, session.DurableStore(f.Sessions, se.LogID))
	require.NoError(t, err)
	chain := []usage.Owner{{RunID: task.ID, AgentID: se.AgentID, AgentCallKey: task.ID}}
	require.NoError(t, c.ReserveAttributed(ctx, "not-reported", 0, chain))
	require.NoError(t, c.Reserve(ctx, "unattributed-not-reported"))
	before, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	log := &offlineQueryLog{Store: f.Sessions}
	cfg := f.Config()
	cfg.Sessions = log
	svc, err := mg.New(cfg)
	require.NoError(t, err)
	run, err := svc.GetRun(ctx, f.Actor, task.ID)
	require.NoError(t, err)
	require.Equal(t, "partial", run.UsageStatus)
	require.NotNil(t, run.Usage, "已有未回报账本与完全缺失账本必须可区分")
	require.Equal(t, 2, run.Usage.Root.UnknownUsageEntries)
	summary, err := svc.SummarizeRuns(ctx, f.Actor, mg.Query{})
	require.NoError(t, err)
	require.Equal(t, 1, summary.Totals.UnknownRuns)
	require.False(t, summary.Totals.DurationComplete)
	require.Equal(t, 1, summary.Totals.DurationIncompleteRuns)
	require.Zero(t, summary.Totals.ExecutionMillis, "终态任务中未结束的尝试不能伪造执行时长")
	require.Zero(t, summary.Totals.KnownRuns)
	require.Nil(t, summary.Totals.KnownTokens)
	require.Nil(t, summary.Agents[0].KnownTokens)
	require.Nil(t, summary.AgentUsage[0].KnownTokens)
	require.Equal(t, 1, summary.AgentUsage[0].UnknownEntries)
	require.Nil(t, summary.Unattributed)
	require.Equal(t, 1, summary.UnattributedUnknownEntries)
	require.False(t, summary.UnattributedComplete)
	for _, query := range []func(context.Context) error{
		func(ctx context.Context) error { _, e := svc.GetRun(ctx, f.Actor, task.ID); return e },
		func(ctx context.Context) error { _, e := svc.ListRuns(ctx, f.Actor, mg.Query{}); return e },
		func(ctx context.Context) error { _, e := svc.SummarizeRuns(ctx, f.Actor, mg.Query{}); return e },
	} {
		canceled, cancel := context.WithCancel(ctx)
		log.cancelRead = cancel
		err := query(canceled)
		cancel()
		require.ErrorIs(t, err, context.Canceled, "日志读取阶段取消不能降级为成功的未知结果")
	}
	after, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Zero(t, log.writes)
	require.Zero(t, log.locks)
}

func TestOfflineQuerySummaryRejectsTokenOverflow(t *testing.T) {
	ctx := context.Background()
	for _, attributed := range []bool{false, true} {
		t.Run(fmt.Sprintf("归属=%t", attributed), func(t *testing.T) {
			f := managementtest.NewFixture(t)
			se := f.ReadySession(t)
			for i := range 2 {
				task := offlineQueryTask(t, f, se, fmt.Sprintf("overflow-%d", i))
				c, err := budget.NewDurable(task.Budget, session.DurableStore(f.Sessions, se.LogID))
				require.NoError(t, err)
				var chain []usage.Owner
				if attributed {
					chain = []usage.Owner{{RunID: task.ID, AgentID: se.AgentID, AgentCallKey: task.ID}}
				}
				require.NoError(t, c.ReserveAttributed(ctx, "request", 0, chain))
				_, err = c.RecordEvidence(ctx, "request", chain, usage.Evidence{KnownFields: usage.BasicFields, Tokens: interfaces.TokenUsage{PromptTokens: math.MaxInt, TotalTokens: math.MaxInt}})
				require.NoError(t, err)
				run, err := f.Service.GetRun(ctx, f.Actor, task.ID)
				require.NoError(t, err)
				require.Equal(t, math.MaxInt, run.Usage.Root.KnownTokens.TotalTokens, "单笔证据合法，溢出只出现在跨运行累计")
			}
			_, err := f.Service.SummarizeRuns(ctx, f.Actor, mg.Query{Limit: 1})
			require.ErrorIs(t, err, agentErrors.ErrManagementInvalid, "跨页累计溢出必须拒绝，不能绕回负数")
		})
	}
}

func TestOfflineQueryRejectsMalformedRecords(t *testing.T) {
	ctx := context.Background()
	for _, field := range []string{"agent_id", "name"} {
		t.Run(field, func(t *testing.T) {
			f := managementtest.NewFixture(t)
			require.NoError(t, f.Store.Put(ctx, "agents", map[string]any{"id": "broken", "tenant_id": f.Actor.TenantID, "revision": 1, field: 42}))
			_, err := f.Service.GetAgent(ctx, f.Actor, "broken")
			require.Error(t, err, "不兼容的持久数据不得作为正常详情返回")
			page, err := f.Service.ListAgents(ctx, f.Actor, mg.Query{})
			require.Error(t, err)
			require.Empty(t, page.Items)
		})
	}
	f := managementtest.NewFixture(t)
	backendErr := errors.New("审批列表读取失败")
	f.Store.FailNext("list", "approvals", backendErr)
	_, err := f.Service.ListApprovals(ctx, f.Actor, mg.Query{})
	require.ErrorIs(t, err, backendErr)
}

func TestOfflineQueryLedgerAggregationFailureStaysUnknown(t *testing.T) {
	ctx := context.Background()
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	task := offlineQueryTask(t, f, se, "ledger-overflow")
	c, err := budget.NewDurable(task.Budget, session.DurableStore(f.Sessions, se.LogID))
	require.NoError(t, err)
	for _, key := range []string{"first", "second"} {
		require.NoError(t, c.Reserve(ctx, key))
		_, err = c.RecordEvidence(ctx, key, nil, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: math.MaxInt}})
		require.NoError(t, err)
	}
	before, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	log := &offlineQueryLog{Store: f.Sessions}
	cfg := f.Config()
	cfg.Sessions = log
	svc, err := mg.New(cfg)
	require.NoError(t, err)
	run, err := svc.GetRun(ctx, f.Actor, task.ID)
	require.NoError(t, err)
	require.Equal(t, "unavailable", run.UsageStatus)
	require.Nil(t, run.Usage)
	require.False(t, run.Complete)
	after, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Zero(t, log.writes)
	require.Zero(t, log.locks)
}
