package builder

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

type refundState struct {
	OrderID  string `json:"order_id"`
	Receipt  string `json:"receipt"`
	Approved bool   `json:"approved"`
}

// refundGraph 在 refund 节点前等待审批；effects 按 DurableKey 计数，用来证明退款只执行一次。
func refundGraph(t *testing.T, store durable.Store, effects map[string]int) *workflow.DurableGraph[refundState] {
	t.Helper()
	g := workflow.NewGraph[refundState]()
	require.NoError(t, g.AddNode("check", func(_ context.Context, s refundState) (refundState, error) { return s, nil }))
	require.NoError(t, g.AddNode("refund", func(ctx context.Context, s refundState) (refundState, error) {
		effects[workflow.DurableKey(ctx)]++
		s.Receipt, s.Approved = "R-"+s.OrderID, true
		return s, nil
	}))
	require.NoError(t, g.AddEdge(workflow.START, "check"))
	require.NoError(t, g.AddEdge("check", "refund"))
	require.NoError(t, g.AddEdge("refund", workflow.END))
	run, err := g.CompileDurable(context.Background(), workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"refund"}})
	require.NoError(t, err)
	return run
}

func refundTool(t *testing.T, graph *workflow.DurableGraph[refundState], name string) interfaces.Tool {
	t.Helper()
	return refundToolWith(t, graph, name, true)
}

func refundToolWith(t *testing.T, graph *workflow.DurableGraph[refundState], name string, resumable bool) interfaces.Tool {
	t.Helper()
	tool, err := tools.NewRunnableToolFactory(func(context.Context) (tools.OwnedRunnable[refundState, refundState], error) {
		return tools.OwnedRunnable[refundState, refundState]{Runnable: graph.AsChildRunnable(), Close: func(context.Context) error { return nil }}, nil
	}, tools.RunnableToolConfig[refundState, refundState]{
		Name: name, Description: "退款流程", Version: "v1", Resumable: resumable,
		ArgsSchema: `{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"]}`,
		Decode: func(args map[string]any) (refundState, error) {
			return refundState{OrderID: args["order_id"].(string)}, nil
		},
		Encode: func(s refundState) (any, error) { return s.Receipt, nil },
	})
	require.NoError(t, err)
	return tool
}

func dispatcherFor(t *testing.T, ts ...interfaces.Tool) *tools.Dispatcher {
	t.Helper()
	reg := tools.NewRegistry()
	for _, tool := range ts {
		require.NoError(t, reg.Register(tool))
	}
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	return d
}

func requireWaiting(t *testing.T, err error) (*durable.WaitError, workflow.ChildContinuation) {
	t.Helper()
	require.ErrorIs(t, err, ErrWaiting)
	var waiting *WaitingError
	require.True(t, errors.As(err, &waiting))
	require.Len(t, waiting.Waits, 1)
	var c workflow.ChildContinuation
	require.NoError(t, json.Unmarshal(waiting.Waits[0].Continuation, &c))
	return waiting.Waits[0], c
}

func TestNestedApprovalPausesAndResumesParentAgent(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "ticket-1"), store)
	controller, err := budget.NewDurable(budget.Config{RootKey: "ticket-1", Version: "v1", MaxModelRequests: 3}, store)
	require.NoError(t, err)
	ctx = budget.WithController(ctx, controller)
	effects := map[string]int{}
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "refund_workflow", `{"order_id":"A1"}`)}, Usage: &llm.Usage{TotalTokens: 3}}},
		answerTurn("已退款 R-A1", 4),
	)
	agent, err := NewSimpleBuilder(model).WithDurableScope("support").
		WithTools(dispatcherFor(t, refundTool(t, refundGraph(t, store, effects), "refund_workflow")).Tools()...).
		WithMaxIterations(2).Build()
	require.NoError(t, err)

	_, err = agent.AsRunnable().Invoke(ctx, "退款 A1")
	wait, c := requireWaiting(t, err)
	require.Len(t, model.requests, 1, "等待结果没有交给模型")
	require.Empty(t, effects)
	require.Equal(t, "refund", c.Node)
	require.NotEmpty(t, c.ApprovalKey)
	require.False(t, c.Rejected)
	waitKey := wait.Path[len(wait.Path)-1].Key

	// 没有提供审批：重新进入后仍在等待，已完成的模型回合只回放。
	_, err = agent.AsRunnable().Invoke(ctx, "退款 A1")
	requireWaiting(t, err)
	require.Len(t, model.requests, 1)

	// 错误的审批键不被采纳，状态不变，仍可续跑。
	_, err = agent.AsRunnable().Invoke(durable.WithResume(ctx, waitKey, "forged"), "退款 A1")
	_, rejected := requireWaiting(t, err)
	require.True(t, rejected.Rejected)
	require.Empty(t, effects)

	out, err := agent.AsRunnable().Invoke(durable.WithResume(ctx, waitKey, c.ApprovalKey), "退款 A1")
	require.NoError(t, err)
	require.Equal(t, "已退款 R-A1", out.Result)
	require.Len(t, model.requests, 2, "续跑只新增最终回答这一次请求")
	require.Len(t, effects, 1)
	for _, n := range effects {
		require.Equal(t, 1, n, "退款只执行一次")
	}
	snap, err := controller.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, snap.ReservedRequests)
	require.Equal(t, 7, snap.KnownTokens.TotalTokens)

	// 完成后再次调用只回放。
	again, err := agent.AsRunnable().Invoke(ctx, "退款 A1")
	require.NoError(t, err)
	require.Equal(t, out.Result, again.Result)
	require.Len(t, model.requests, 2)
}

func TestNestedWaitThroughChildAgent(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "ticket-2"), store)
	effects := map[string]int{}
	graph := refundGraph(t, store, effects)
	childModel := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("g1", "refund_workflow", `{"order_id":"B2"}`)}}},
		answerTurn("子流程完成", 1),
	)
	childFactory := func(context.Context) (tools.OwnedRunnable[any, *AgentOutput], error) {
		child, err := NewSimpleBuilder(childModel).WithDurableScope("refund-agent").
			WithTools(dispatcherFor(t, refundTool(t, graph, "refund_workflow")).Tools()...).WithMaxIterations(2).Build()
		if err != nil {
			return tools.OwnedRunnable[any, *AgentOutput]{}, err
		}
		return tools.OwnedRunnable[any, *AgentOutput]{Runnable: child.AsRunnable(), Close: func(context.Context) error { return nil }}, nil
	}
	childTool, err := tools.NewRunnableToolFactory(childFactory, tools.RunnableToolConfig[any, *AgentOutput]{
		Name: "refund_agent", Description: "退款助手", Version: "v1", Resumable: true, ModelInvoking: true,
		ArgsSchema: `{"type":"object","properties":{"task":{"type":"string"}},"required":["task"]}`,
		Decode:     func(args map[string]any) (any, error) { return args["task"].(string), nil },
		Encode:     func(o *AgentOutput) (any, error) { return o.Result, nil },
	})
	require.NoError(t, err)
	parentModel := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("p1", "refund_agent", `{"task":"退款 B2"}`)}}},
		answerTurn("已处理", 1),
	)
	parent, err := NewSimpleBuilder(parentModel).WithDurableScope("support").
		WithTools(dispatcherFor(t, childTool).Tools()...).WithMaxIterations(2).Build()
	require.NoError(t, err)
	controller, err := budget.NewDurable(budget.Config{RootKey: "ticket-2", Version: "v1", MaxModelRequests: 4}, store)
	require.NoError(t, err)
	ctx = budget.WithController(ctx, controller)

	_, err = parent.AsRunnable().Invoke(ctx, "退款 B2")
	wait, c := requireWaiting(t, err)
	require.GreaterOrEqual(t, len(wait.Path), 2, "等待路径从父工具一直通到孙图")
	require.Len(t, parentModel.requests, 1)
	require.Len(t, childModel.requests, 1)

	out, err := parent.AsRunnable().Invoke(durable.WithResume(ctx, wait.Path[len(wait.Path)-1].Key, c.ApprovalKey), "退款 B2")
	require.NoError(t, err)
	require.Equal(t, "已处理", out.Result)
	require.Len(t, parentModel.requests, 2)
	require.Len(t, childModel.requests, 2, "子 Agent 已完成的回合只回放")
	require.Len(t, effects, 1)
}

func TestResumableToolRequiresJournal(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	tool := refundTool(t, refundGraph(t, store, map[string]int{}), "refund_workflow")
	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"order_id": "A1"}})
	require.ErrorContains(t, err, "journaled")
}

func TestParallelWaitAndFailureReportsFailure(t *testing.T) {
	wait := &durable.WaitError{Continuation: json.RawMessage(`{}`)}
	failure := errors.New("real failure")
	require.Same(t, failure, parallelFailure([]toolCallResult{{err: wait}, {err: failure}}))
	all := parallelFailure([]toolCallResult{{err: wait}, {}, {err: wait}})
	var w *WaitingError
	require.True(t, errors.As(all, &w))
	require.Len(t, w.Waits, 2)
	require.Nil(t, parallelFailure([]toolCallResult{{err: failure}}))
}

func TestNestedResumeAfterCrashDoesNotRepeatEffect(t *testing.T) {
	inner, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	crash := false
	store := failOnSave{Store: inner, fail: func(v *durable.Snapshot) bool {
		// 子图完成并执行退款后、父工具记录保存为完成之前崩溃。
		return crash && strings.Contains(string(v.Data), `"status":"completed"`) && strings.Contains(string(v.Data), `"tool/turn/0/call/c1"`)
	}}
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "ticket-3"), store)
	effects := map[string]int{}
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "refund_workflow", `{"order_id":"C3"}`)}}},
		answerTurn("已退款", 1),
	)
	agent, err := NewSimpleBuilder(model).WithDurableScope("support").
		WithTools(dispatcherFor(t, refundTool(t, refundGraph(t, inner, effects), "refund_workflow")).Tools()...).
		WithMaxIterations(2).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "退款 C3")
	wait, c := requireWaiting(t, err)
	resumeCtx := durable.WithResume(ctx, wait.Path[len(wait.Path)-1].Key, c.ApprovalKey)

	crash = true
	_, err = agent.AsRunnable().Invoke(resumeCtx, "退款 C3")
	require.ErrorContains(t, err, "crash")
	require.Len(t, effects, 1)

	crash = false
	out, err := agent.AsRunnable().Invoke(resumeCtx, "退款 C3")
	require.NoError(t, err, "带着同一审批输入重新进入也能完成")
	require.Equal(t, "已退款", out.Result)
	for _, n := range effects {
		require.Equal(t, 1, n, "子图已完成的节点不重跑")
	}
	require.Len(t, model.requests, 2)
}

// 没有声明 Resumable 的工具无法续跑：子图在执行任何节点之前失败并说明原因，不留下永远续不上的等待。
func TestChildGraphInNonResumableToolFailsBeforeRunning(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "ticket-4"), store)
	checked := 0
	g := workflow.NewGraph[refundState]()
	require.NoError(t, g.AddNode("check", func(_ context.Context, s refundState) (refundState, error) { checked++; return s, nil }))
	require.NoError(t, g.AddEdge(workflow.START, "check"))
	require.NoError(t, g.AddEdge("check", workflow.END))
	graph, err := g.CompileDurable(context.Background(), workflow.DurableConfig{Store: store, Version: "v1"})
	require.NoError(t, err)
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "refund_workflow", `{"order_id":"D4"}`)}}},
		answerTurn("不应到达", 1),
	)
	agent, err := NewSimpleBuilder(model).WithDurableScope("support").
		WithTools(dispatcherFor(t, refundToolWith(t, graph, "refund_workflow", false)).Tools()...).
		WithMaxIterations(2).Build()
	require.NoError(t, err)

	_, err = agent.AsRunnable().Invoke(ctx, "退款 D4")
	require.ErrorIs(t, err, durable.ErrWaitNotResumable)
	require.NotErrorIs(t, err, ErrWaiting)
	require.Zero(t, checked, "子图一个节点都没有执行")

	_, err = agent.AsRunnable().Invoke(durable.WithResume(ctx, "any", "key"), "退款 D4")
	require.ErrorIs(t, err, durable.ErrOperationAttention, "失败记录交给宿主处理，而不是一直返回等待")
	require.Len(t, model.requests, 1)
}

// 子图配置了另一个（如进程内存）Store 时，崩溃后它的检查点随之丢失；子图检查点必须写在父日志的 Store 里，
// 否则续跑会从头重跑，已执行的退款再执行一次。
func TestChildGraphCheckpointsLiveInParentJournalStore(t *testing.T) {
	journal, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	crash := false
	store := failOnSave{Store: journal, fail: func(v *durable.Snapshot) bool {
		return crash && strings.Contains(string(v.Data), `"status":"completed"`) && strings.Contains(string(v.Data), `"tool/turn/0/call/c1"`)
	}}
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "ticket-5"), store)
	effects := map[string]int{}
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "refund_workflow", `{"order_id":"E5"}`)}}},
		answerTurn("已退款", 1),
	)
	// 每次“进程启动”都换一个新的子图 Store，模拟进程内存中的检查点在崩溃后丢失。
	newAgent := func() *ConfigurableAgent[any, *core.AgentState] {
		volatile, err := durable.NewFileStore(t.TempDir())
		require.NoError(t, err)
		agent, err := NewSimpleBuilder(model).WithDurableScope("support").
			WithTools(dispatcherFor(t, refundTool(t, refundGraph(t, volatile, effects), "refund_workflow")).Tools()...).
			WithMaxIterations(2).Build()
		require.NoError(t, err)
		return agent
	}
	_, err = newAgent().AsRunnable().Invoke(ctx, "退款 E5")
	wait, c := requireWaiting(t, err)
	resumeCtx := durable.WithResume(ctx, wait.Path[len(wait.Path)-1].Key, c.ApprovalKey)

	crash = true
	_, err = newAgent().AsRunnable().Invoke(resumeCtx, "退款 E5")
	require.ErrorContains(t, err, "crash")
	require.Len(t, effects, 1)

	crash = false
	out, err := newAgent().AsRunnable().Invoke(resumeCtx, "退款 E5")
	require.NoError(t, err)
	require.Equal(t, "已退款", out.Result)
	for _, n := range effects {
		require.Equal(t, 1, n, "崩溃后续跑不重复退款")
	}
}
