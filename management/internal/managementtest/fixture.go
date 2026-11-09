package managementtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	mg "github.com/kart-io/goagent/management"
	"github.com/stretchr/testify/require"
)

// Factory 在启动 Worker 前配置；执行回调的跨协程状态须由测试自行同步。
type Factory struct {
	Capabilities mg.Capabilities
	ValidateErr  error
	RunFunc      func(context.Context, mg.RunSpec) (mg.RunResult, error)
}

func (f *Factory) Validate(context.Context, mg.Version) (mg.Capabilities, error) {
	return f.Capabilities, f.ValidateErr
}
func (f *Factory) Build(context.Context, mg.Version, mg.Resources) (mg.Binding, error) {
	return mg.Binding{Runtime: f}, nil
}
func (f *Factory) Inspect(_ context.Context, spec mg.RunSpec) (mg.RunEvidence, error) {
	if len(spec.Task.Waits) > 0 {
		return mg.RunEvidence{State: "waiting"}, nil
	}
	return mg.RunEvidence{State: "absent"}, nil
}
func (f *Factory) Run(ctx context.Context, spec mg.RunSpec) (mg.RunResult, error) {
	if f.RunFunc != nil {
		return f.RunFunc(ctx, spec)
	}
	return mg.RunResult{Outcome: "completed", Output: []byte(`{"ok":true}`)}, nil
}

// History 读取真实内存会话日志，并遵守业务历史隐藏内部事件的边界。
type History struct{ Sessions session.Store }

func (h History) History(ctx context.Context, id string, after int64, limit int, types []string) (mg.HistoryPage, error) {
	events, err := h.Sessions.Read(ctx, id, after)
	if err != nil {
		return mg.HistoryPage{}, err
	}
	p := mg.HistoryPage{Items: []session.Event{}, NextSeq: after}
	for _, ev := range events {
		if strings.HasPrefix(ev.Type, "management/") || ev.Type == constants.SessionEventBoundarySnapshot {
			continue
		}
		match := len(types) == 0
		for _, typ := range types {
			if typ == ev.Type {
				match = true
			}
		}
		if !match {
			continue
		}
		p.Items = append(p.Items, ev)
		p.NextSeq = ev.Seq
		if len(p.Items) == limit {
			break
		}
	}
	return p, nil
}

type Fixture struct {
	Store    *Store
	Sessions *session.MemoryStore
	Service  *mg.Service
	Actor    mg.Actor
	Factory  *Factory
}

func NewFixture(t testing.TB) *Fixture {
	t.Helper()
	f := &Fixture{Store: NewStore(), Sessions: session.NewMemoryStore(), Actor: mg.Actor{TenantID: "tenant-a", Subject: "tester"}, Factory: &Factory{Capabilities: mg.Capabilities{Approval: true, Input: true, Fork: true}}}
	var err error
	f.Service, err = mg.New(f.Config())
	require.NoError(t, err)
	return f
}
func (f *Fixture) Config() mg.Config {
	return mg.Config{Store: f.Store, Sessions: f.Sessions, HistoryReader: History{f.Sessions}, Authorizer: mg.TenantAuthorizer{}, Factories: map[string]mg.Factory{"offline": f.Factory}}
}
func (f *Fixture) Register(t testing.TB) (mg.Agent, mg.Version) {
	t.Helper()
	ctx := context.Background()
	a, err := f.Service.RegisterAgent(ctx, f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "fixture-agent", Name: "离线测试"})
	require.NoError(t, err)
	v, err := f.Service.SaveVersion(ctx, f.Actor, mg.SaveVersionRequest{IdempotencyKey: "fixture-version", AgentID: a.ID, Version: "v1", RunnerKind: "offline", ImplementationVersion: "impl-v1", Config: []byte(`{}`)})
	require.NoError(t, err)
	a, err = f.Service.PublishVersion(ctx, f.Actor, mg.PublishRequest{IdempotencyKey: "fixture-publish", AgentID: a.ID, Version: v.Version, ExpectedRevision: a.Revision})
	require.NoError(t, err)
	return a, v
}
func (f *Fixture) StartWorker(t testing.TB) *mg.Worker {
	t.Helper()
	w := mg.NewWorker(f.Service)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("工作器未及时退出")
		}
	})
	return w
}

// ReadySession 仅在准备期间启动工作器，返回前等待退出，便于调用者随后配置执行行为。
func (f *Fixture) ReadySession(t testing.TB) mg.Session {
	t.Helper()
	a, _ := f.Register(t)
	se, err := f.Service.CreateSession(context.Background(), f.Actor, mg.CreateSessionRequest{IdempotencyKey: "fixture-session", AgentID: a.ID})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mg.NewWorker(f.Service).Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("会话准备工作器未退出")
		}
	}()
	require.Eventually(t, func() bool {
		se, err = f.Service.GetSession(context.Background(), f.Actor, se.ID)
		return err == nil && se.Status == "ready"
	}, 5*time.Second, 5*time.Millisecond, "会话应经持久命令完成准备")
	return se
}
