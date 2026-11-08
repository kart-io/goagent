package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
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
		if ev.Type != constants.SessionEventBoundarySnapshot {
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
		constants.SessionEventTurnStart, constants.SessionEventSystemMessage, constants.SessionEventToolDefinitions, constants.SessionEventUserMessage,
		constants.SessionEventAssistantMessage, constants.SessionEventToolResult,
		// 第三步前的压缩连同第二次调用及其结果一起记为改写。
		constants.SessionEventHistoryRewrite, constants.SessionEventAssistantMessage, constants.SessionEventTurnEnd,
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
	require.Nil(t, plain)
	require.ErrorContains(t, err, "SessionLog需要ToolStreamClient和至少一个工具")
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
		constants.SessionEventTurnStart, constants.SessionEventToolDefinitions, constants.SessionEventUserMessage, constants.SessionEventAssistantMessage,
		constants.SessionEventToolResult, constants.SessionEventAssistantMessage, constants.SessionEventTurnEnd,
	}, eventTypes(events), "one turn, no duplicated messages")
	require.True(t, slices.ContainsFunc(events, func(ev session.Event) bool { return ev.Type == constants.SessionEventBoundarySnapshot }),
		"durable records live in the same log")
	derived, err := session.DeriveMessages(events)
	require.NoError(t, err)
	require.Len(t, derived, 4)
	require.Equal(t, "found", derived[2].Content)
}

// 持久运行的会话在工具结果之后分叉：新会话以同一执行键恢复，之前的模型回合与工具回放，之后实跑；原会话不变。
func TestSessionLogForkContinuesDurableRun(t *testing.T) {
	store := session.NewMemoryStore()
	var calls atomic.Int32
	tool := tools.NewBaseTool("lookup", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls.Add(1)
		return &interfaces.ToolOutput{Success: true, Result: "found"}, nil
	})
	run := func(model *scriptModel, id string) (*AgentOutput, error) {
		dispatched, err := dispatcherTools(tools.DispatchConfig{}, tool)
		require.NoError(t, err)
		agent, err := NewSimpleBuilder(model).WithSessionID(id).WithSessionLog(store).WithDurableScope("fork").
			WithTools(dispatched...).WithMaxIterations(4).Build()
		require.NoError(t, err)
		ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "run-1"), session.DurableStore(store, id))
		return agent.AsRunnable().Invoke(ctx, "查")
	}
	out, err := run(newScriptModel(callTool("c1", "lookup"), answer("原答")), "s")
	require.NoError(t, err)
	require.Equal(t, "原答", out.Result)
	original := sessionEvents(t, store, "s")

	at := slices.IndexFunc(original, func(ev session.Event) bool { return ev.Type == constants.SessionEventToolResult })
	require.NoError(t, store.Fork(context.Background(), "s", "f", original[at].Seq))

	model := newScriptModel(answer("新答"))
	out, err = run(model, "f")
	require.NoError(t, err)
	require.Equal(t, "新答", out.Result)
	require.Equal(t, 1, model.count(), "分叉点之前的模型回合回放")
	require.EqualValues(t, 1, calls.Load(), "工具回放")
	require.Len(t, model.request(0).Messages, 3)

	forked := sessionEvents(t, store, "f")
	require.Equal(t, []string{
		constants.SessionEventTurnStart, constants.SessionEventToolDefinitions, constants.SessionEventUserMessage, constants.SessionEventAssistantMessage,
		constants.SessionEventToolResult, constants.SessionEventAssistantMessage, constants.SessionEventTurnEnd,
	}, eventTypes(forked), "续写分叉出的那一轮")
	derived, err := session.DeriveMessages(forked)
	require.NoError(t, err)
	require.Equal(t, "新答", derived[3].Content)
	require.Equal(t, original, sessionEvents(t, store, "s"))
}

// crashAt 在开始第 n 个持久操作（首次保存它的快照）时失败一次，模拟进程在该操作开始前退出。
type crashAt struct {
	durable.Store
	n    int
	seen map[string]bool
}

func (s *crashAt) Save(ctx context.Context, name string, v *durable.Snapshot) error {
	if !s.seen[name] {
		s.seen[name] = true
		if len(s.seen) == s.n {
			return errors.New("crash")
		}
	}
	return s.Store.Save(ctx, name, v)
}

// 在第二次模型请求开始前中断（工具结果已记入日志）：以同一执行键恢复时，回放的对话与已记录的一致，不重复记录。
func TestSessionLogDurableResumeAfterRecordedToolResult(t *testing.T) {
	store := session.NewMemoryStore()
	crash := &crashAt{n: 3, seen: map[string]bool{}} // 模型回合 0、工具、模型回合 1
	run := func(model *scriptModel) (*AgentOutput, error) {
		dispatched, err := dispatcherTools(tools.DispatchConfig{}, plainTool())
		require.NoError(t, err)
		agent, err := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(store).WithDurableScope("resume").
			WithTools(dispatched...).WithMaxIterations(4).Build()
		require.NoError(t, err)
		crash.Store = session.DurableStore(store, "s")
		ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "run-1"), crash)
		return agent.AsRunnable().Invoke(ctx, "查")
	}
	_, err := run(newScriptModel(callTool("c1", "lookup")))
	require.ErrorContains(t, err, "crash")
	require.Contains(t, eventTypes(sessionEvents(t, store, "s")), constants.SessionEventToolResult)

	model := newScriptModel(answer("答"))
	out, err := run(model)
	require.NoError(t, err)
	require.Equal(t, "答", out.Result)
	require.Equal(t, 1, model.count())
	require.Equal(t, []string{
		constants.SessionEventTurnStart, constants.SessionEventToolDefinitions, constants.SessionEventUserMessage, constants.SessionEventAssistantMessage,
		constants.SessionEventToolResult, constants.SessionEventAssistantMessage, constants.SessionEventTurnEnd,
	}, eventTypes(sessionEvents(t, store, "s")))
}

// 已完成持久轮次重建后仍须重放原请求，不能把其历史再次追加为新输入。
func TestSessionLogCompletedDurableTurnReplaysWithoutNewEvents(t *testing.T) {
	for _, backend := range []string{"memory", "file"} {
		t.Run(backend, func(t *testing.T) {
			var store session.Store = session.NewMemoryStore()
			if backend == "file" {
				var err error
				store, err = session.NewFileStore(t.TempDir())
				require.NoError(t, err)
			}
			var effects atomic.Int32
			model := newScriptModel(callTool("c1", "lookup"), answer("答"), answer("第二轮"))
			key := "run-1"
			invoke := func(input string, reject bool) (*AgentOutput, error) {
				tool := tools.NewBaseTool("lookup", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
					effects.Add(1)
					return &interfaces.ToolOutput{Success: true, Result: "found"}, nil
				})
				dispatched, err := dispatcherTools(tools.DispatchConfig{}, tool)
				require.NoError(t, err)
				b := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(store).WithDurableScope("completed").WithTools(dispatched...).WithMaxIterations(4)
				if reject {
					b.WithOutputValidator(func(context.Context, *AgentOutput) error { return errors.New("新的最终验收拒绝") })
				}
				agent, err := b.Build()
				require.NoError(t, err)
				ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), key), session.DurableStore(store, "s"))
				return agent.AsRunnable().Invoke(ctx, input)
			}
			out, err := invoke("查", false)
			require.NoError(t, err)
			require.Equal(t, "答", out.Result)
			before := sessionEvents(t, store, "s")
			out, err = invoke("查", false)
			require.NoError(t, err)
			require.Equal(t, "答", out.Result)
			require.Equal(t, before, sessionEvents(t, store, "s"), "完成重放不得写入重复轮次或消息")
			require.EqualValues(t, 1, effects.Load())
			require.Equal(t, 2, model.count())
			_, err = invoke("改变原始输入", false)
			require.Error(t, err)
			require.Equal(t, before, sessionEvents(t, store, "s"))
			_, err = invoke("查", true)
			require.ErrorContains(t, err, "新的最终验收拒绝")
			require.Equal(t, before, sessionEvents(t, store, "s"))
			require.Equal(t, 2, model.count())
			require.EqualValues(t, 1, effects.Load())
			key = "run-2"
			out, err = invoke("第二次问", false)
			require.NoError(t, err)
			require.Equal(t, "第二轮", out.Result)
			later := sessionEvents(t, store, "s")
			key = "run-1"
			_, err = invoke("查", false)
			require.ErrorContains(t, err, "历史轮次")
			require.Equal(t, later, sessionEvents(t, store, "s"))
			key = "run-2"
			_, err = session.Rewind(context.Background(), store, "s", 0, session.LastSeq(later))
			require.NoError(t, err)
			rewound := sessionEvents(t, store, "s")
			_, err = invoke("第二次问", false)
			require.ErrorContains(t, err, "退回")
			require.Equal(t, rewound, sessionEvents(t, store, "s"))
			require.Equal(t, 3, model.count())
		})
	}
}

func TestSessionLogCompletedReplayCannotCreateOperations(t *testing.T) {
	for _, change := range []string{"scope", "journal", "tool-record"} {
		t.Run(change, func(t *testing.T) {
			sessions := session.NewMemoryStore()
			directory := t.TempDir()
			journal, err := durable.NewFileStore(directory)
			require.NoError(t, err)
			scope := "A"
			model := newScriptModel(callTool("c1", "lookup"), answer("答"), callTool("c1", "lookup"), answer("答"))
			effects := 0
			invoke := func() error {
				tool := tools.NewBaseTool("lookup", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
					effects++
					return &interfaces.ToolOutput{Success: true, Result: "found"}, nil
				})
				dispatched, e := dispatcherTools(tools.DispatchConfig{}, tool)
				require.NoError(t, e)
				agent, e := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(sessions).WithDurableScope(scope).WithTools(dispatched...).Build()
				require.NoError(t, e)
				ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "run"), journal)
				_, e = agent.AsRunnable().Invoke(ctx, "查")
				return e
			}
			require.NoError(t, invoke())
			before := sessionEvents(t, sessions, "s")
			switch change {
			case "scope":
				scope = "B"
			case "journal":
				journal, err = durable.NewFileStore(t.TempDir())
				require.NoError(t, err)
			case "tool-record":
				files, e := os.ReadDir(directory)
				require.NoError(t, e)
				removed := 0
				for _, file := range files {
					if !strings.HasSuffix(file.Name(), ".json") {
						continue
					}
					path := filepath.Join(directory, file.Name())
					raw, e := os.ReadFile(path)
					require.NoError(t, e)
					var snap durable.Snapshot
					require.NoError(t, json.Unmarshal(raw, &snap))
					var record durable.OperationRecord
					require.NoError(t, json.Unmarshal(snap.Data, &record))
					if strings.HasPrefix(record.ID, "tool/") {
						require.NoError(t, os.Remove(path))
						removed++
					}
				}
				require.Equal(t, 1, removed)
			}
			require.Error(t, invoke(), "完成会话只能回放已有操作")
			require.Equal(t, 2, model.count(), "不能发送新的模型请求")
			require.Equal(t, 1, effects, "不能产生新的工具效果")
			require.Equal(t, before, sessionEvents(t, sessions, "s"))
		})
	}
}
