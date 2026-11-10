package management_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	mt "github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/stretchr/testify/require"
)

func offlineCount(t *testing.T, f *mt.Fixture, kind string) int {
	t.Helper()
	docs, err := f.Store.List(context.Background(), kind, mg.Selection{TenantID: f.Actor.TenantID, Visibility: mg.Visibility{All: true}, Query: mg.Query{Limit: 10000}})
	require.NoError(t, err)
	return len(docs)
}

func TestOfflineRegistrationTransactionsAndIdempotency(t *testing.T) {
	ctx := context.Background()
	f := mt.NewFixture(t)
	r := mg.RegisterAgentRequest{IdempotencyKey: "register", Name: "注册"}
	for _, kind := range []string{"agents", "audit", "idempotency"} {
		t.Run("回滚_"+kind, func(t *testing.T) {
			failure := errors.New("写入失败")
			f.Store.FailNext("insert", kind, failure)
			_, err := f.Service.RegisterAgent(ctx, f.Actor, r)
			require.ErrorIs(t, err, failure)
			for _, k := range []string{"agents", "audit", "idempotency"} {
				require.Zero(t, offlineCount(t, f, k))
			}
		})
	}
	a, err := f.Service.RegisterAgent(ctx, f.Actor, r)
	require.NoError(t, err)
	require.True(t, a.Enabled)
	require.Equal(t, int64(1), a.Revision)
	again, err := f.Service.RegisterAgent(ctx, f.Actor, r)
	require.NoError(t, err)
	require.Equal(t, a, again)
	for _, k := range []string{"agents", "audit", "idempotency"} {
		require.Equal(t, 1, offlineCount(t, f, k))
	}
	r.Name = "不同内容"
	_, err = f.Service.RegisterAgent(ctx, f.Actor, r)
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
	other := f.Actor
	other.TenantID = "tenant-b"
	b, err := f.Service.RegisterAgent(ctx, other, r)
	require.NoError(t, err)
	require.NotEqual(t, a.ID, b.ID, "幂等键必须按租户隔离")
	_, err = f.Service.GetAgent(ctx, other, a.ID)
	require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
	for _, name := range []string{"", " 前空格", strings.Repeat("字", 257)} {
		_, err = f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "invalid", Name: name})
		require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	}
	for _, key := range []string{"", strings.Repeat("x", 257)} {
		_, err = f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: key, Name: "有效名称"})
		require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	}
}

func TestOfflineExpiredIdempotencyIsReplacedAtomically(t *testing.T) {
	f := mt.NewFixture(t)
	now := time.Now().UTC()
	cfg := f.Config()
	cfg.Now = func() time.Time { return now }
	cfg.Limits = mg.DefaultLimits()
	cfg.Limits.IdempotencyRetention = time.Minute
	s, err := mg.New(cfg)
	require.NoError(t, err)
	r := mg.RegisterAgentRequest{IdempotencyKey: "expires", Name: "过期前"}
	first, err := s.RegisterAgent(context.Background(), f.Actor, r)
	require.NoError(t, err)
	now = now.Add(2 * time.Minute)
	r.Name = "过期后"
	failure := errors.New("替换失败")
	f.Store.FailNext("insert", "audit", failure)
	_, err = s.RegisterAgent(context.Background(), f.Actor, r)
	require.ErrorIs(t, err, failure)
	require.Equal(t, 1, offlineCount(t, f, "idempotency"), "失败时旧幂等记录删除必须回滚")
	second, err := s.RegisterAgent(context.Background(), f.Actor, r)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, 1, offlineCount(t, f, "idempotency"))
	require.Equal(t, 2, offlineCount(t, f, "agents"))
}

func TestOfflineVersionsPublicationAndSessionBinding(t *testing.T) {
	ctx := context.Background()
	f := mt.NewFixture(t)
	a, v1 := f.Register(t)
	r := mg.SaveVersionRequest{IdempotencyKey: "v2", AgentID: a.ID, Version: "v2", RunnerKind: "offline", ImplementationVersion: "impl-v2", Config: []byte(`{"b":2,"a":9007199254740993}`)}
	v2, err := f.Service.SaveVersion(ctx, f.Actor, r)
	require.NoError(t, err)
	require.NotEqual(t, v1.ConfigHash, v2.ConfigHash)
	require.Equal(t, `{"a":9007199254740993,"b":2}`, string(v2.Config))
	r.Config = []byte(`{"a":9007199254740993,"b":2}`)
	again, err := f.Service.SaveVersion(ctx, f.Actor, r)
	require.NoError(t, err)
	require.Equal(t, v2.ID, again.ID)
	r.IdempotencyKey = "duplicate-version"
	_, err = f.Service.SaveVersion(ctx, f.Actor, r)
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
	before, err := f.Service.CreateSession(ctx, f.Actor, mg.CreateSessionRequest{IdempotencyKey: "before-publish", AgentID: a.ID})
	require.NoError(t, err)
	require.Equal(t, "v1", before.Version)
	_, err = f.Service.PublishVersion(ctx, f.Actor, mg.PublishRequest{IdempotencyKey: "stale", AgentID: a.ID, Version: "v2", ExpectedRevision: 1})
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
	publish := mg.PublishRequest{IdempotencyKey: "publish-v2", AgentID: a.ID, Version: "v2", ExpectedRevision: a.Revision}
	failure := errors.New("审计失败")
	f.Store.FailNext("insert", "audit", failure)
	_, err = f.Service.PublishVersion(ctx, f.Actor, publish)
	require.ErrorIs(t, err, failure)
	current, err := f.Service.GetAgent(ctx, f.Actor, a.ID)
	require.NoError(t, err)
	require.Equal(t, a, current)
	a, err = f.Service.PublishVersion(ctx, f.Actor, publish)
	require.NoError(t, err)
	after, err := f.Service.CreateSession(ctx, f.Actor, mg.CreateSessionRequest{IdempotencyKey: "after-publish", AgentID: a.ID})
	require.NoError(t, err)
	require.Equal(t, "v2", after.Version)
	old, err := f.Service.GetSession(ctx, f.Actor, before.ID)
	require.NoError(t, err)
	require.Equal(t, "v1", old.Version)
	a, err = f.Service.SetAgentEnabled(ctx, f.Actor, mg.SetEnabledRequest{IdempotencyKey: "disable", AgentID: a.ID, ExpectedRevision: a.Revision})
	require.NoError(t, err)
	create := mg.CreateSessionRequest{IdempotencyKey: "retry-create", AgentID: a.ID}
	_, err = f.Service.CreateSession(ctx, f.Actor, create)
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
	_, err = f.Service.SetAgentEnabled(ctx, f.Actor, mg.SetEnabledRequest{IdempotencyKey: "stale-enable", AgentID: a.ID, ExpectedRevision: a.Revision - 1, Enabled: true})
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
	_, err = f.Service.SetAgentEnabled(ctx, f.Actor, mg.SetEnabledRequest{IdempotencyKey: "enable", AgentID: a.ID, ExpectedRevision: a.Revision, Enabled: true})
	require.NoError(t, err)
	_, err = f.Service.CreateSession(ctx, f.Actor, create)
	require.NoError(t, err, "被拒绝的请求不应占用幂等键")
}

func TestOfflineSessionPreparationAndForkPrefix(t *testing.T) {
	ctx := context.Background()
	f := mt.NewFixture(t)
	se := f.ReadySession(t)
	events, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "management/session", events[0].Type)
	_, err = f.Service.ForkSession(ctx, f.Actor, mg.ForkSessionRequest{IdempotencyKey: "no-boundary", SessionID: se.ID, ExpectedRevision: se.Revision})
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	end, err := session.NewEvent(constants.SessionEventTurnEnd, session.TurnEnd{})
	require.NoError(t, err)
	aborted, err := session.NewEvent(constants.SessionEventTurnEnd, session.TurnEnd{Aborted: true})
	require.NoError(t, err)
	_, err = f.Sessions.Append(ctx, se.LogID, 1, end, aborted)
	require.NoError(t, err)
	_, err = f.Service.ForkSession(ctx, f.Actor, mg.ForkSessionRequest{IdempotencyKey: "aborted", SessionID: se.ID, UpTo: 3, ExpectedRevision: se.Revision})
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	r := mg.ForkSessionRequest{IdempotencyKey: "fork", SessionID: se.ID, ExpectedRevision: se.Revision}
	child, err := f.Service.ForkSession(ctx, f.Actor, r)
	require.NoError(t, err)
	require.Equal(t, "preparing", child.Status)
	require.Equal(t, int64(2), child.ForkSeq)
	require.Equal(t, se.ID, child.ParentID)
	require.Equal(t, se.Version, child.Version)
	again, err := f.Service.ForkSession(ctx, f.Actor, r)
	require.NoError(t, err)
	require.Equal(t, child, again)
	f.StartWorker(t)
	require.Eventually(t, func() bool {
		cur, e := f.Service.GetSession(ctx, f.Actor, child.ID)
		return e == nil && cur.Status == "ready"
	}, 5*time.Second, 5*time.Millisecond)
	source, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	branch, err := f.Sessions.Read(ctx, child.LogID, 0)
	require.NoError(t, err)
	require.Equal(t, source[:2], branch)
	extra, err := session.NewEvent("business/message", "独立分支")
	require.NoError(t, err)
	_, err = f.Sessions.Append(ctx, child.LogID, 2, extra)
	require.NoError(t, err)
	sourceAfter, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	require.Equal(t, source, sourceAfter)
}

func TestOfflineAdmissionFailureRollsBackTaskAndCommand(t *testing.T) {
	f := mt.NewFixture(t)
	se := f.ReadySession(t)
	ctx := context.Background()
	r := mg.SubmitTaskRequest{IdempotencyKey: "task", SessionID: se.ID, Input: []byte(`{"x":1}`)}
	for _, kind := range []string{"commands", "audit", "idempotency"} {
		before := map[string]int{}
		for _, k := range []string{"tasks", "commands", "audit", "idempotency"} {
			before[k] = offlineCount(t, f, k)
		}
		failure := fmt.Errorf("%s写入失败", kind)
		f.Store.FailNext("insert", kind, failure)
		_, err := f.Service.SubmitTask(ctx, f.Actor, r)
		require.ErrorIs(t, err, failure)
		for k, count := range before {
			require.Equal(t, count, offlineCount(t, f, k), k)
		}
	}
	task, err := f.Service.SubmitTask(ctx, f.Actor, r)
	require.NoError(t, err)
	require.Equal(t, mg.Queued, task.State)
	again, err := f.Service.SubmitTask(ctx, f.Actor, r)
	require.NoError(t, err)
	require.Equal(t, task.ID, again.ID)
	r.Input = []byte(`{"x":2}`)
	_, err = f.Service.SubmitTask(ctx, f.Actor, r)
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
	cfg := f.Config()
	cfg.Limits = mg.DefaultLimits()
	cfg.Limits.MaxPending = 1
	s, err := mg.New(cfg)
	require.NoError(t, err)
	r.IdempotencyKey = "capacity"
	_, err = s.SubmitTask(ctx, f.Actor, r)
	require.ErrorIs(t, err, agentErrors.ErrManagementBusy)
	_, err = s.CancelTask(ctx, f.Actor, mg.CancelTaskRequest{IdempotencyKey: "cancel", TaskID: task.ID, ExpectedRevision: task.Revision})
	require.NoError(t, err)
	_, err = s.SubmitTask(ctx, f.Actor, r)
	require.NoError(t, err, "终态任务不可占用接纳容量")
}

func TestOfflineConfigurationRejectsUnsafeLimitsAndCopiesFactories(t *testing.T) {
	f := mt.NewFixture(t)
	for _, tc := range []struct {
		name   string
		change func(*mg.Config)
	}{
		{"缺少存储", func(c *mg.Config) { c.Store = nil }},
		{"缺少会话存储", func(c *mg.Config) { c.Sessions = nil }},
		{"缺少历史读取器", func(c *mg.Config) { c.HistoryReader = nil }},
		{"缺少授权器", func(c *mg.Config) { c.Authorizer = nil }},
		{"缺少工厂", func(c *mg.Config) { c.Factories = nil }},
		{"工厂名称为空", func(c *mg.Config) { c.Factories = map[string]mg.Factory{"": f.Factory} }},
		{"工厂为空", func(c *mg.Config) { c.Factories = map[string]mg.Factory{"offline": nil} }},
		{"无工作器", func(c *mg.Config) { c.Limits.Workers = 0 }},
		{"无接纳容量", func(c *mg.Config) { c.Limits.MaxPending = 0 }},
		{"无默认分页", func(c *mg.Config) { c.Limits.PageSize = 0 }},
		{"分页上限过小", func(c *mg.Config) { c.Limits.MaxPageSize = 1 }},
		{"分页上限过大", func(c *mg.Config) { c.Limits.MaxPageSize = 201 }},
		{"无输入上限", func(c *mg.Config) { c.Limits.MaxInputBytes = 0 }},
		{"无输出上限", func(c *mg.Config) { c.Limits.MaxOutputBytes = 0 }},
		{"无幂等保留期", func(c *mg.Config) { c.Limits.IdempotencyRetention = 0 }},
		{"无任务保留期", func(c *mg.Config) { c.Limits.TaskRetention = 0 }},
		{"无审计保留期", func(c *mg.Config) { c.Limits.AuditRetention = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := f.Config()
			cfg.Limits = mg.DefaultLimits()
			tc.change(&cfg)
			s, err := mg.New(cfg)
			require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
			require.Nil(t, s)
		})
	}
	cfg := f.Config()
	s, err := mg.New(cfg)
	require.NoError(t, err)
	delete(cfg.Factories, "offline")
	f.Service = s
	_, v := f.Register(t)
	require.Equal(t, "offline", v.RunnerKind, "宿主修改原始注册表不得改变服务绑定")
}

func TestOfflineVersionValidationAndMissingPublication(t *testing.T) {
	f := mt.NewFixture(t)
	ctx := context.Background()
	a, err := f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "agent", Name: "校验"})
	require.NoError(t, err)
	_, err = f.Service.CreateSession(ctx, f.Actor, mg.CreateSessionRequest{IdempotencyKey: "unpublished", AgentID: a.ID})
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	_, err = f.Service.PublishVersion(ctx, f.Actor, mg.PublishRequest{IdempotencyKey: "missing", AgentID: a.ID, Version: "missing", ExpectedRevision: a.Revision})
	require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
	r := mg.SaveVersionRequest{IdempotencyKey: "version", AgentID: a.ID, RunnerKind: "offline", Version: "v1", ImplementationVersion: "impl", Config: []byte(`{}`)}
	for _, change := range []func(*mg.SaveVersionRequest){
		func(v *mg.SaveVersionRequest) { v.RunnerKind = "missing" },
		func(v *mg.SaveVersionRequest) { v.Version = "" },
		func(v *mg.SaveVersionRequest) { v.ImplementationVersion = "" },
		func(v *mg.SaveVersionRequest) { v.Config = []byte(`{"x":1,"x":2}`) },
	} {
		bad := r
		change(&bad)
		_, err = f.Service.SaveVersion(ctx, f.Actor, bad)
		require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	}
	f.Factory.ValidateErr = errors.New("不可执行配置")
	_, err = f.Service.SaveVersion(ctx, f.Actor, r)
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	require.Zero(t, offlineCount(t, f, "versions"))
	f.Factory.ValidateErr = nil
	v, err := f.Service.SaveVersion(ctx, f.Actor, r)
	require.NoError(t, err)
	f.Factory.ValidateErr = errors.New("实现已不可用")
	pub := mg.PublishRequest{IdempotencyKey: "publish", AgentID: a.ID, Version: v.Version, ExpectedRevision: a.Revision}
	_, err = f.Service.PublishVersion(ctx, f.Actor, pub)
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	cur, err := f.Service.GetAgent(ctx, f.Actor, a.ID)
	require.NoError(t, err)
	require.Equal(t, a, cur)
	f.Factory.ValidateErr = nil
	cfg := f.Config()
	cfg.Factories = map[string]mg.Factory{"replacement": f.Factory}
	s, err := mg.New(cfg)
	require.NoError(t, err)
	_, err = s.PublishVersion(ctx, f.Actor, pub)
	require.ErrorIs(t, err, agentErrors.ErrManagementUnsupported)
	_, err = f.Service.PublishVersion(ctx, f.Actor, pub)
	require.NoError(t, err)
}

func TestOfflineForkRejectsUnsafeSource(t *testing.T) {
	for _, mode := range []string{"修订冲突", "会话未准备", "活跃任务", "负序号", "代理停用", "不支持分支", "队列未排空"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := mt.NewFixture(t)
			se := f.ReadySession(t)
			end, err := session.NewEvent(constants.SessionEventTurnEnd, session.TurnEnd{})
			require.NoError(t, err)
			_, err = f.Sessions.Append(ctx, se.LogID, 1, end)
			require.NoError(t, err)
			r := mg.ForkSessionRequest{IdempotencyKey: "fork", SessionID: se.ID, ExpectedRevision: se.Revision}
			want := agentErrors.ErrManagementConflict
			switch mode {
			case "修订冲突":
				r.ExpectedRevision--
			case "负序号":
				r.UpTo = -1
			case "会话未准备":
				se.Status = "preparing"
				require.NoError(t, f.Store.Put(ctx, "sessions", se))
			case "活跃任务":
				se.ActiveTaskID = "running"
				require.NoError(t, f.Store.Put(ctx, "sessions", se))
			case "代理停用":
				a, e := f.Service.GetAgent(ctx, f.Actor, se.AgentID)
				require.NoError(t, e)
				_, e = f.Service.SetAgentEnabled(ctx, f.Actor, mg.SetEnabledRequest{IdempotencyKey: "disable", AgentID: a.ID, ExpectedRevision: a.Revision})
				require.NoError(t, e)
			case "不支持分支":
				page, e := f.Service.ListVersions(ctx, f.Actor, mg.Query{AgentID: se.AgentID})
				require.NoError(t, e)
				require.Len(t, page.Items, 1)
				v := page.Items[0]
				v.Capabilities.Fork = false
				require.NoError(t, f.Store.Put(ctx, "versions", v))
				want = agentErrors.ErrManagementUnsupported
			case "队列未排空":
				_, e := f.Service.SubmitTask(ctx, f.Actor, mg.SubmitTaskRequest{IdempotencyKey: "pending", SessionID: se.ID, Input: []byte(`1`)})
				require.NoError(t, e)
			}
			before := offlineCount(t, f, "sessions")
			_, err = f.Service.ForkSession(ctx, f.Actor, r)
			require.ErrorIs(t, err, want)
			require.Equal(t, before, offlineCount(t, f, "sessions"), "拒绝时不可留下分支会话")
		})
	}
}

func TestOfflineSessionPreparationDetectsForeignLog(t *testing.T) {
	f := mt.NewFixture(t)
	ctx := context.Background()
	a, _ := f.Register(t)
	se, err := f.Service.CreateSession(ctx, f.Actor, mg.CreateSessionRequest{IdempotencyKey: "collision", AgentID: a.ID})
	require.NoError(t, err)
	ev, err := session.NewEvent("management/session", "其他会话")
	require.NoError(t, err)
	_, err = f.Sessions.Append(ctx, se.LogID, 0, ev)
	require.NoError(t, err)
	f.StartWorker(t)
	require.Eventually(t, func() bool {
		current, e := f.Service.GetSession(ctx, f.Actor, se.ID)
		return e == nil && current.Status == "attention"
	}, 5*time.Second, 5*time.Millisecond)
	events, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, ev.Data, events[0].Data, "会话冲突不得覆盖原日志")
	_, err = f.Service.SubmitTask(ctx, f.Actor, mg.SubmitTaskRequest{IdempotencyKey: "unsafe", SessionID: se.ID, Input: []byte(`1`)})
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict)
}

// publishPolicy 只允许 publisher 执行 agent.publish。
type publishPolicy struct{ mg.TenantAuthorizer }

func (p publishPolicy) Authorize(ctx context.Context, actor mg.Actor, action string, ref mg.ResourceRef) error {
	if action == "agent.publish" && actor.Subject != "publisher" {
		return agentErrors.ErrManagementForbidden
	}
	return p.TenantAuthorizer.Authorize(ctx, actor, action, ref)
}

// 只有会话权限时只能使用已发布的版本；指定未发布的版本须具备发布权限。
func TestCreateSessionWithUnpublishedVersionRequiresPublish(t *testing.T) {
	ctx := context.Background()
	f := mt.NewFixture(t)
	cfg := f.Config()
	cfg.Authorizer = publishPolicy{}
	var err error
	f.Service, err = mg.New(cfg)
	require.NoError(t, err)
	publisher := f.Actor
	publisher.Subject = "publisher"
	a, err := f.Service.RegisterAgent(ctx, publisher, mg.RegisterAgentRequest{IdempotencyKey: "agent", Name: "草稿"})
	require.NoError(t, err)
	for _, v := range []string{"v1", "v2"} {
		_, err = f.Service.SaveVersion(ctx, publisher, mg.SaveVersionRequest{IdempotencyKey: v, AgentID: a.ID, Version: v, RunnerKind: "offline", ImplementationVersion: "impl", Config: []byte(`{}`)})
		require.NoError(t, err)
	}
	_, err = f.Service.PublishVersion(ctx, publisher, mg.PublishRequest{IdempotencyKey: "publish", AgentID: a.ID, Version: "v1", ExpectedRevision: a.Revision})
	require.NoError(t, err)

	tests := []struct {
		name    string
		actor   mg.Actor
		version string
		err     error
	}{
		{"默认使用发布版本", f.Actor, "", nil},
		{"显式指定发布版本", f.Actor, "v1", nil},
		{"未发布版本需要发布权限", f.Actor, "v2", agentErrors.ErrManagementForbidden},
		{"有发布权限可使用未发布版本", publisher, "v2", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			se, err := f.Service.CreateSession(ctx, tt.actor, mg.CreateSessionRequest{IdempotencyKey: tt.name, AgentID: a.ID, Version: tt.version})
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, cmp.Or(tt.version, "v1"), se.Version)
		})
	}
}
