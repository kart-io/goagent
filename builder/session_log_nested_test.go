package builder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func TestNestedAgentSessionLogOwnership(t *testing.T) {
	for _, mode := range []string{"普通Client", "无工具原生Client", "工具子Agent", "Handle子Agent", "独立日志", "Handle独立日志"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			store := session.NewMemoryStore()
			var client llm.Client = mocks.NewScriptedClient(mocks.TextReply("子任务", "子回答"))
			if mode == "普通Client" {
				client = plainSessionClient{client}
			}
			childBuilder := NewSimpleBuilder(client).WithSystemPrompt("子系统")
			handle := mode == "Handle子Agent" || mode == "Handle独立日志"
			if handle || mode == "工具子Agent" {
				childBuilder.WithTools(plainTool())
			}
			ownLog := mode == "独立日志" || mode == "Handle独立日志"
			if ownLog {
				childBuilder.WithSessionLog(store)
			}
			child, err := childBuilder.Build()
			require.NoError(t, err)
			var before, after []session.Event
			delegate := tools.NewBaseTool("delegate", "调用子Agent", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				var err error
				before, err = store.Read(ctx, "parent", 0)
				if err != nil {
					return nil, err
				}
				if ownLog {
					// 共享存储时，宿主显式为子 Agent 绑定自己的会话身份。
					ctx = tools.WithSessionID(ctx, "child")
				}
				var out *AgentOutput
				if handle {
					out, err = child.Start(ctx, "子任务").Wait(ctx)
				} else {
					out, err = child.AsRunnable().Invoke(ctx, "子任务")
				}
				if err != nil {
					return nil, err
				}
				after, err = store.Read(ctx, "parent", 0)
				return &interfaces.ToolOutput{Success: err == nil, Result: out.Result}, err
			})
			parent, err := NewSimpleBuilder(newScriptModel(callTool("c1", "delegate"), answer("父答案"))).
				WithTools(delegate).WithSessionID("parent").WithSessionLog(store).WithSystemPrompt("父系统").Build()
			require.NoError(t, err)
			out, err := parent.AsRunnable().Invoke(ctx, "父任务")
			require.NoError(t, err)
			require.Equal(t, "父答案", out.Result)
			require.Equal(t, len(before), len(after), "子 Agent 不能向父轮次追加事件")
			require.Equal(t, before, after, "子 Agent 不能修改父轮次的消息、系统提示或工具定义")
			_, open := session.OpenTurn(after)
			require.True(t, open, "子 Agent 返回时父轮次尚未结束")
			trajectory, err := session.BuildTrajectory(sessionEvents(t, store, "parent"))
			require.NoError(t, err)
			require.Equal(t, 1, trajectory.Turns)
			require.Equal(t, "父系统", trajectory.System)
			if ownLog {
				childTrajectory, err := session.BuildTrajectory(sessionEvents(t, store, "child"))
				require.NoError(t, err)
				require.Equal(t, 1, childTrajectory.Turns)
				require.Equal(t, "子系统", childTrajectory.System)
			}
		})
	}
}

func TestNestedPlainAgentAllowsParentDurableResume(t *testing.T) {
	store := session.NewMemoryStore()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = durable.WithJournal(durable.WithExecutionKey(ctx, "run"), session.DurableStore(store, "parent"))
	child, err := NewSimpleBuilder(mocks.NewScriptedClient(mocks.TextReply("子任务", "子回答"))).WithDurableScope("child").Build()
	require.NoError(t, err)
	delegate := tools.NewBaseTool("delegate", "调用子Agent", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		out, err := child.AsRunnable().Invoke(ctx, "子任务")
		if err != nil {
			return nil, err
		}
		return &interfaces.ToolOutput{Success: true, Result: out.Result}, nil
	})
	dispatched, err := dispatcherTools(tools.DispatchConfig{}, delegate)
	require.NoError(t, err)
	stopped := false
	model := newScriptModel(callTool("c1", "delegate"), answer("父答案"))
	build := func() *SimpleAgent {
		t.Helper()
		a, err := NewSimpleBuilder(model).WithTools(dispatched...).WithSessionID("parent").WithSessionLog(store).WithDurableScope("parent").
			WithRequestHook(func(_ context.Context, req *ModelRequest) error {
				if req.Step == 1 && !stopped {
					stopped = true
					return errors.New("注入中断")
				}
				return nil
			}).Build()
		require.NoError(t, err)
		return a
	}
	_, err = build().AsRunnable().Invoke(ctx, "父任务")
	require.ErrorContains(t, err, "注入中断")
	out, err := build().AsRunnable().Invoke(ctx, "父任务")
	require.NoError(t, err, "父轮次未完成，恢复时必须允许执行缺失的最终模型操作")
	require.Equal(t, "父答案", out.Result)
}

func TestNestedPlainAgentDoesNotRetainFailedParentHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store := session.NewMemoryStore()
	child, err := NewSimpleBuilder(mocks.NewScriptedClient(mocks.TextReply("子任务", "子回答"))).Build()
	require.NoError(t, err)
	delegate := tools.NewBaseTool("delegate", "调用子Agent", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		_, err := child.AsRunnable().Invoke(ctx, "子任务")
		return &interfaces.ToolOutput{Success: err == nil, Result: "子结果"}, err
	})
	model := newScriptModel(callTool("c1", "delegate"), failing(errors.New("父模型中断")), answer("下一轮"))
	parent, err := NewSimpleBuilder(model).WithTools(delegate).WithSessionID("parent").WithSessionLog(store).Build()
	require.NoError(t, err)
	_, err = parent.AsRunnable().Invoke(ctx, "失败的父任务")
	require.ErrorContains(t, err, "父模型中断")
	_, err = parent.AsRunnable().Invoke(ctx, "新输入")
	require.NoError(t, err)
	require.Equal(t, []llm.Message{llm.UserMessage("新输入")}, model.request(2).Messages)
}
