package multiagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/testing/panictest"
)

// panicAgent 的 Collaborate、Vote、ReceiveMessage 都 panic，模拟宿主实现的 Agent。
type panicAgent struct{ *BaseCollaborativeAgent }

func (panicAgent) Collaborate(context.Context, *CollaborativeTask) (*Assignment, error) {
	panic("collaborate boom")
}
func (panicAgent) Vote(context.Context, interface{}) (bool, error) { panic("vote boom") }
func (panicAgent) ReceiveMessage(context.Context, Message) error   { panic("receive boom") }

// planLeader 把任务分配给 boom 工作者。
type planLeader struct{ *BaseCollaborativeAgent }

func (l planLeader) Collaborate(_ context.Context, task *CollaborativeTask) (*Assignment, error) {
	plan := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"worker_id": "boom", "task": "x"}}}
	return &Assignment{AgentID: l.Name(), Result: plan, Status: TaskStatusCompleted}, nil
}

func newTask(kind CollaborationType) *CollaborativeTask {
	return &CollaborativeTask{ID: string(kind), Type: kind, Input: "x", Assignments: map[string]Assignment{}}
}

// 系统在 goroutine 中调用宿主 Agent（并行协作、层级工作者、共识投票、消息路由）：panic 时进程不得退出，
// 并行协作返回可识别的 PanicError，其余场景记为该 Agent 失败，消息路由继续投递后续消息。
func TestMultiAgentGoroutinePanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		recorder := &panictest.Recorder{}
		core.SetGlobalMetricsCollector(recorder)
		system := NewMultiAgentSystem(&MockLogger{})
		defer system.Close()
		ok := NewBaseCollaborativeAgent("ok", "", RoleWorker, system)
		require.NoError(t, system.RegisterAgent("ok", ok))
		require.NoError(t, system.RegisterAgent("boom", panicAgent{NewBaseCollaborativeAgent("boom", "", RoleWorker, system)}))

		_, err := system.ExecuteTask(context.Background(), newTask(CollaborationTypeParallel))
		var p *agentErrors.PanicError
		require.True(t, errors.As(err, &p), "并行协作应返回 PanicError：%v", err)
		require.Equal(t, "collaborate boom", p.Value)

		require.NoError(t, system.RegisterAgent("third", NewBaseCollaborativeAgent("third", "", RoleWorker, system)))
		_, _ = system.ExecuteTask(context.Background(), newTask(CollaborationTypeConsensus))

		require.NoError(t, system.RegisterAgent("leader", planLeader{NewBaseCollaborativeAgent("leader", "", RoleLeader, system)}))
		_, _ = system.ExecuteTask(context.Background(), newTask(CollaborationTypeHierarchical))

		require.NoError(t, system.SendMessage(Message{From: "ok", To: "boom"}))
		require.NoError(t, system.SendMessage(Message{From: "boom", To: "ok"}))
		select {
		case <-ok.messageBox:
		case <-time.After(2 * time.Second):
			t.Fatal("消息路由在接收方 panic 后应继续投递")
		}

		counts := map[any]int{}
		for _, e := range recorder.Wait(4, 2*time.Second) {
			counts[e.Value]++
		}
		// 并行协作与层级工作者各一次 collaborate
		require.Equal(t, map[any]int{"collaborate boom": 2, "vote boom": 1, "receive boom": 1}, counts)
	})
}
