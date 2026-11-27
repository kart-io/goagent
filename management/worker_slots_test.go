package management_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	mg "github.com/kart-io/goagent/management"
	mt "github.com/kart-io/goagent/management/internal/managementtest"
)

// slotBench 让每个任务阻塞到测试放行，便于观察哪些任务拿到了槽位。
type slotBench struct {
	f       *mt.Fixture
	mu      sync.Mutex
	release map[string]chan struct{}
	agents  map[string]mg.Agent
}

func (b *slotBench) gate(name string) chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.release[name] == nil {
		b.release[name] = make(chan struct{})
	}
	return b.release[name]
}

func newSlotBench(t *testing.T, limits mg.Limits, kinds ...string) *slotBench {
	t.Helper()
	b := &slotBench{f: mt.NewFixture(t), release: map[string]chan struct{}{}, agents: map[string]mg.Agent{}}
	b.f.Factory.RunFunc = func(ctx context.Context, spec mg.RunSpec) (mg.RunResult, error) {
		var in struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(spec.Task.Input, &in); err != nil {
			return mg.RunResult{}, err
		}
		select {
		case <-b.gate(in.Name):
			return mg.RunResult{Outcome: "completed", Output: []byte(`{}`)}, nil
		case <-ctx.Done():
			return mg.RunResult{}, ctx.Err()
		}
	}
	withConfig(t, b.f, func(c *mg.Config) {
		c.Limits = limits
		for _, kind := range kinds {
			c.Factories[kind] = b.f.Factory
		}
	})
	ctx := context.Background()
	for _, kind := range kinds {
		a, err := b.f.Service.RegisterAgent(ctx, b.f.Actor, mg.RegisterAgentRequest{IdempotencyKey: "agent-" + kind, Name: kind})
		require.NoError(t, err)
		v, err := b.f.Service.SaveVersion(ctx, b.f.Actor, mg.SaveVersionRequest{IdempotencyKey: "version-" + kind, AgentID: a.ID, Version: "v1", RunnerKind: kind, ImplementationVersion: "impl-v1", Config: []byte(`{}`)})
		require.NoError(t, err)
		a, err = b.f.Service.PublishVersion(ctx, b.f.Actor, mg.PublishRequest{IdempotencyKey: "publish-" + kind, AgentID: a.ID, Version: v.Version, ExpectedRevision: a.Revision})
		require.NoError(t, err)
		b.agents[kind] = a
	}
	b.f.StartWorker(t)
	return b
}

// submit 为每个任务单开会话：同一会话同一时刻只跑一个任务，会掩盖槽位判定。
func (b *slotBench) submit(t *testing.T, actor mg.Actor, kind, name string) string {
	t.Helper()
	ctx := context.Background()
	se, err := b.f.Service.CreateSession(ctx, actor, mg.CreateSessionRequest{IdempotencyKey: "session-" + name, AgentID: b.agents[kind].ID})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		got, e := b.f.Service.GetSession(ctx, actor, se.ID)
		return e == nil && got.Status == "ready"
	}, 5*time.Second, 5*time.Millisecond, "会话应完成准备")
	task, err := b.f.Service.SubmitTask(ctx, actor, mg.SubmitTaskRequest{IdempotencyKey: "task-" + name, SessionID: se.ID, Input: []byte(`{"name":"` + name + `"}`)})
	require.NoError(t, err)
	return task.ID
}

func (b *slotBench) state(t *testing.T, id string) mg.TaskState {
	t.Helper()
	task, err := b.f.Service.GetTask(context.Background(), b.f.Actor, id)
	require.NoError(t, err)
	return task.State
}

func (b *slotBench) wait(t *testing.T, id string, want mg.TaskState) {
	t.Helper()
	require.Eventually(t, func() bool { return b.state(t, id) == want }, 5*time.Second, 5*time.Millisecond, "任务应进入状态 %s", want)
}

// stays 断言任务在若干轮询周期后仍停在原状态。
func (b *slotBench) stays(t *testing.T, id string, want mg.TaskState) {
	t.Helper()
	require.Never(t, func() bool { return b.state(t, id) != want }, 300*time.Millisecond, 10*time.Millisecond, "任务应停在状态 %s", want)
}

func TestKindWorkersReserveSlotsForARunnerKind(t *testing.T) {
	b := newSlotBench(t, mg.Limits{Workers: 1, KindWorkers: map[string]int{"text": 1}}, "chat", "text")
	chat1 := b.submit(t, b.f.Actor, "chat", "chat1")
	b.wait(t, chat1, mg.Running)

	text1 := b.submit(t, b.f.Actor, "text", "text1")
	b.wait(t, text1, mg.Running)

	chat2 := b.submit(t, b.f.Actor, "chat", "chat2")
	b.stays(t, chat2, mg.Queued)
	close(b.gate("chat1"))
	b.wait(t, chat1, mg.Succeeded)
	b.wait(t, chat2, mg.Running)

	text2 := b.submit(t, b.f.Actor, "text", "text2")
	b.stays(t, text2, mg.Queued)
	close(b.gate("chat2"))
	b.wait(t, chat2, mg.Succeeded)
	b.wait(t, text2, mg.Running)
	require.Equal(t, mg.Running, b.state(t, text1), "专用槽占满后同类任务可溢出到共享槽")
	close(b.gate("text1"))
	close(b.gate("text2"))
	b.wait(t, text2, mg.Succeeded)
}

func TestActorWorkersCapOneActorsSharedSlots(t *testing.T) {
	b := newSlotBench(t, mg.Limits{Workers: 3, ActorWorkers: 1, KindWorkers: map[string]int{"text": 1}}, "chat", "text")
	other := mg.Actor{TenantID: b.f.Actor.TenantID, Subject: "other"}

	first := b.submit(t, b.f.Actor, "chat", "first")
	b.wait(t, first, mg.Running)
	second := b.submit(t, b.f.Actor, "chat", "second")
	b.stays(t, second, mg.Queued)

	theirs := b.submit(t, other, "chat", "theirs")
	b.wait(t, theirs, mg.Running)
	require.Equal(t, mg.Queued, b.state(t, second), "其他 Actor 的任务不受该 Actor 上限影响")

	text := b.submit(t, b.f.Actor, "text", "text")
	b.wait(t, text, mg.Running)
	require.Equal(t, mg.Queued, b.state(t, second), "专用槽不计入 Actor 上限")

	close(b.gate("first"))
	b.wait(t, second, mg.Running)
	for _, name := range []string{"second", "theirs", "text"} {
		close(b.gate(name))
	}
	b.wait(t, second, mg.Succeeded)
}
