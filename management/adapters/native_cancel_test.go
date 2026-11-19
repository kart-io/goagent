package adapters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/memstore"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
)

// blockingModel 先吐一个增量，随后阻塞到 ctx 取消，模拟用户中途点击停止。
type blockingModel struct {
	*mocks.MockStreamingLLMClient
	started chan struct{}
}

func (m *blockingModel) CompleteStream(ctx context.Context, _ *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	ch := make(chan *llm.StreamChunk)
	go func() {
		defer close(ch)
		select {
		case ch <- &llm.StreamChunk{Delta: "a", Content: "a"}:
		case <-ctx.Done():
			return
		}
		close(m.started)
		<-ctx.Done()
	}()
	return ch, nil
}

func (m *blockingModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, req)
}

func TestNativeCancelMarksTaskCanceled(t *testing.T) {
	model := &blockingModel{MockStreamingLLMClient: mocks.NewMockStreamingLLMClient(), started: make(chan struct{})}
	factory := NativeFactory{
		Check: func(context.Context, mg.Version) error { return nil },
		BuildAgent: func(context.Context, mg.Version, mg.Resources, tools.Approver) (*builder.SimpleAgentBuilder, func(context.Context) error, error) {
			return builder.NewSimpleBuilder(model), nil, nil
		},
	}
	sessions := session.NewMemoryStore()
	svc, err := mg.New(mg.Config{Store: memstore.New(), Sessions: sessions, HistoryReader: memstore.History{Sessions: sessions}, Authorizer: mg.TenantAuthorizer{}, Factories: map[string]mg.Factory{"native": factory}})
	require.NoError(t, err)

	ctx := context.Background()
	actor := mg.Actor{TenantID: "tenant", Subject: "user"}
	agent, err := svc.RegisterAgent(ctx, actor, mg.RegisterAgentRequest{IdempotencyKey: "agent", Name: "取消"})
	require.NoError(t, err)
	v, err := svc.SaveVersion(ctx, actor, mg.SaveVersionRequest{IdempotencyKey: "version", AgentID: agent.ID, Version: "v1", RunnerKind: "native", ImplementationVersion: "impl", Config: []byte(`{}`)})
	require.NoError(t, err)
	agent, err = svc.PublishVersion(ctx, actor, mg.PublishRequest{IdempotencyKey: "publish", AgentID: agent.ID, Version: v.Version, ExpectedRevision: agent.Revision})
	require.NoError(t, err)
	se, err := svc.CreateSession(ctx, actor, mg.CreateSessionRequest{IdempotencyKey: "session", AgentID: agent.ID})
	require.NoError(t, err)

	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- mg.NewWorker(svc).Run(workerCtx) }()
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("工作器未退出")
		}
	})
	require.Eventually(t, func() bool {
		se, err = svc.GetSession(ctx, actor, se.ID)
		return err == nil && se.Status == "ready"
	}, 5*time.Second, 5*time.Millisecond)

	task, err := svc.SubmitTask(ctx, actor, mg.SubmitTaskRequest{IdempotencyKey: "task", SessionID: se.ID, Input: []byte(`"你好"`)})
	require.NoError(t, err)
	select {
	case <-model.started:
	case <-time.After(5 * time.Second):
		t.Fatal("模型流未启动")
	}
	task, err = svc.GetTask(ctx, actor, task.ID)
	require.NoError(t, err)
	_, err = svc.CancelTask(ctx, actor, mg.CancelTaskRequest{IdempotencyKey: "cancel", TaskID: task.ID, ExpectedRevision: task.Revision, Reason: "用户停止"})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		task, err = svc.GetTask(ctx, actor, task.ID)
		return err == nil && task.State.Terminal()
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, mg.Canceled, task.State, "error_kind=%q", task.ErrorKind)
}

func TestNativeRunReturnsCtxErrWhenCanceledWithoutOutcome(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := &native{start: func(context.Context, interface{}) (<-chan builder.AgentEvent, error) {
		ch := make(chan builder.AgentEvent)
		close(ch)
		return ch, nil
	}}
	_, err := n.run(ctx, mg.RunSpec{Task: mg.Task{Input: []byte(`"x"`)}}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestNativeRunKeepsNoOutcomeErrorWithoutCancel(t *testing.T) {
	n := &native{start: func(context.Context, interface{}) (<-chan builder.AgentEvent, error) {
		ch := make(chan builder.AgentEvent)
		close(ch)
		return ch, nil
	}}
	_, err := n.run(context.Background(), mg.RunSpec{Task: mg.Task{Input: []byte(`"x"`)}}, nil)
	require.Error(t, err)
	require.False(t, errors.Is(err, context.Canceled))
	require.Contains(t, err.Error(), "生产者退出时没有确定结果")
}

func TestNativeRunJoinsCtxErrOntoEventError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	boom := errors.New("模型流中断")
	n := &native{start: func(context.Context, interface{}) (<-chan builder.AgentEvent, error) {
		ch := make(chan builder.AgentEvent, 1)
		ch <- builder.AgentEvent{Error: boom}
		close(ch)
		return ch, nil
	}}
	_, err := n.run(ctx, mg.RunSpec{Task: mg.Task{Input: []byte(`"x"`)}}, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, boom)
}
