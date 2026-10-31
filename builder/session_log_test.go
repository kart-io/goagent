package builder

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

func sessionEvents(t *testing.T, store session.Store, id string) []session.Event {
	t.Helper()
	events, err := store.Read(context.Background(), id, 0)
	require.NoError(t, err)
	return events
}

func eventTypes(events []session.Event) []string {
	var out []string
	for _, ev := range events {
		if ev.Type != session.TypeBoundarySnapshot {
			out = append(out, ev.Type)
		}
	}
	return out
}

// 每次模型请求都能由日志重建：system + 日志推导的对话 + 本步的运行上下文。
func TestSessionLogRebuildsRequests(t *testing.T) {
	store := session.NewMemoryStore()
	model := newScriptModel(callTool("c1", "lookup"), callTool("c2", "lookup"), answer("答"), answer("第二轮"))
	rebuilt := 0
	agent, err := NewSimpleBuilder(model).WithSystemPrompt("你是助手").WithSessionID("s").WithSessionLog(store).
		WithTools(plainTool()).WithMaxIterations(5).
		// 第三步前压缩：只保留最近一次工具调用。
		WithPreStep(func(_ context.Context, s *Step) (PreStepResult, error) {
			if s.Index == 2 {
				n := len(s.Messages)
				s.Messages = []llm.Message{s.Messages[0], llm.UserMessage("[摘要] 已查询一次"), s.Messages[n-2], s.Messages[n-1]}
			}
			return PreStepResult{}, nil
		}).
		WithPromptContexts(PromptContext{Name: "n", Text: func(p PromptScope) string { return "步骤上下文" }}).
		WithRequestHook(func(ctx context.Context, req *ModelRequest) error {
			derived, err := session.DeriveMessages(sessionEvents(t, store, "s"))
			require.NoError(t, err)
			msgs := req.Request.Messages
			expected := append(append([]llm.Message{msgs[0]}, derived...), stepContextMessage("步骤上下文"))
			require.Equal(t, expected, msgs, "request %d", req.Step)
			rebuilt++
			return nil
		}).Build()
	require.NoError(t, err)

	out, err := agent.AsRunnable().Invoke(context.Background(), "查一下")
	require.NoError(t, err)
	require.Equal(t, "答", out.Result)
	require.Equal(t, 3, rebuilt)
	events := sessionEvents(t, store, "s")
	require.Equal(t, []string{
		session.TypeTurnStart, session.TypeSystemMessage, session.TypeToolDefinitions, session.TypeUserMessage,
		session.TypeAssistantMessage, session.TypeToolResult,
		// 第三步前的压缩连同第二次调用及其结果一起记为改写。
		session.TypeHistoryRewrite, session.TypeAssistantMessage, session.TypeTurnEnd,
	}, eventTypes(events))

	// 下一轮的历史来自日志（压缩后的对话加上答复），而不是记忆。
	out, err = agent.AsRunnable().Invoke(context.Background(), "再问")
	require.NoError(t, err)
	require.Equal(t, "第二轮", out.Result)
	derived, err := session.DeriveMessages(sessionEvents(t, store, "s"))
	require.NoError(t, err)
	require.Equal(t, llm.UserMessage("[摘要] 已查询一次"), derived[0])
	require.Equal(t, []llm.Message{{Role: "assistant", Content: "答"}, llm.UserMessage("再问"), {Role: "assistant", Content: "第二轮"}}, derived[3:])
	require.Equal(t, 4, rebuilt)

	traj, err := session.BuildTrajectory(sessionEvents(t, store, "s"))
	require.NoError(t, err)
	require.Equal(t, 2, traj.Turns)
	require.Equal(t, "你是助手", traj.System)
	require.Equal(t, []session.ToolStat{{Name: "lookup", Calls: 1}}, traj.Stats, "the compacted call is no longer in the trajectory")
}

// 非持久运行中断的轮次在下一轮记为中止，其消息不进入历史。
func TestSessionLogAbortsInterruptedTurn(t *testing.T) {
	store := session.NewMemoryStore()
	model := newScriptModel(callTool("c1", "lookup"), failing(errors.New("boom")), answer("好"))
	agent, err := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(store).WithTools(plainTool()).WithMaxIterations(4).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "第一次")
	require.Error(t, err)
	_, open := session.OpenTurn(sessionEvents(t, store, "s"))
	require.True(t, open)

	out, err := agent.AsRunnable().Invoke(context.Background(), "第二次")
	require.NoError(t, err)
	require.Equal(t, "好", out.Result)
	require.Equal(t, []llm.Message{llm.UserMessage("第二次")}, model.request(2).Messages, "the aborted turn is not history")
	derived, err := session.DeriveMessages(sessionEvents(t, store, "s"))
	require.NoError(t, err)
	require.Equal(t, []llm.Message{llm.UserMessage("第二次"), {Role: "assistant", Content: "好"}}, derived)

	// 只支持原生工具流。
	plain, err := NewSimpleBuilder(newScriptModel(answer("x"))).WithSessionLog(store).Build()
	require.NoError(t, err)
	_, err = plain.AsRunnable().Invoke(context.Background(), "x")
	require.ErrorContains(t, err, "会话日志只支持原生工具流")
}

// 持久运行以会话日志为 durable 日志：中断后以同一执行键恢复，日志与未中断的运行一致，消息不重复记录。
func TestSessionLogDurableResume(t *testing.T) {
	dir := t.TempDir()
	store, err := session.NewFileStore(dir)
	require.NoError(t, err)
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	// 第一次执行时阻塞到 ctx 取消（模拟进程中断），之后正常返回。
	tool := tools.NewBaseTool("lookup", "test", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		if calls.Add(1) == 1 {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return &interfaces.ToolOutput{Success: true, Result: "found"}, nil
	})
	build := func(model *scriptModel, st session.Store) *ConfigurableAgent[any, *core.AgentState] {
		dispatched, err := dispatcherTools(tools.DispatchConfig{}, tool)
		require.NoError(t, err)
		agent, err := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(st).WithDurableScope("resume").
			WithTools(dispatched...).WithMaxIterations(4).Build()
		require.NoError(t, err)
		return agent
	}
	runCtx := func(ctx context.Context, st session.Store) context.Context {
		return durable.WithJournal(durable.WithExecutionKey(ctx, "run-1"), session.DurableStore(st, "s"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := build(newScriptModel(callTool("c1", "lookup")), store).AsRunnable().Invoke(runCtx(ctx, store), "查")
		done <- err
	}()
	<-started
	cancel()
	require.Error(t, <-done)

	// 新进程：重新打开存储，以同一执行键恢复；模型的第一回合从日志回放，只需要最终答复。
	reopened, err := session.NewFileStore(dir)
	require.NoError(t, err)
	resume := newScriptModel(answer("答"))
	out, err := build(resume, reopened).AsRunnable().Invoke(runCtx(context.Background(), reopened), "查")
	require.NoError(t, err)
	require.Equal(t, "答", out.Result)
	require.Equal(t, 1, resume.count())
	resumed := resume.request(0).Messages
	require.Len(t, resumed, 3, "the replayed turn plus the re-run tool result")
	require.Equal(t, llm.UserMessage("查"), resumed[0])

	events := sessionEvents(t, reopened, "s")
	require.Equal(t, []string{
		session.TypeTurnStart, session.TypeToolDefinitions, session.TypeUserMessage, session.TypeAssistantMessage,
		session.TypeToolResult, session.TypeAssistantMessage, session.TypeTurnEnd,
	}, eventTypes(events), "one turn, no duplicated messages")
	require.True(t, slices.ContainsFunc(events, func(ev session.Event) bool { return ev.Type == session.TypeBoundarySnapshot }),
		"durable records live in the same log")
	derived, err := session.DeriveMessages(events)
	require.NoError(t, err)
	require.Len(t, derived, 4)
	require.Equal(t, "found", derived[2].Content)
}
