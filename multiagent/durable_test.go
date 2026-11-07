package multiagent

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

func agentGraph(t *testing.T, factory AgentFactory) *workflow.Graph[ticket] {
	t.Helper()
	step, err := AgentStep("answer", factory, askText, keepAnswer)
	require.NoError(t, err)
	g, err := Sequential(workflow.Config{}, step)
	require.NoError(t, err)
	return g
}

// crashBeforeNodeCommit 让第一个节点输出的检查点保存失败一次：节点内的模型操作已提交，节点输出未提交。
func crashBeforeNodeCommit(e *env) {
	e.fault.arm(false, func(_ string, s *durable.Snapshot) bool {
		v, ok := checkpoint(s)
		return ok && v.Routing && s.Next == 1
	})
}

func operationFails(id, status string) func(string, *durable.Snapshot) bool {
	return func(_ string, s *durable.Snapshot) bool {
		r, ok := operation(s)
		return ok && r.ID == id && r.Status == status
	}
}

func TestDurableModelReplay(t *testing.T) {
	e := newEnv(t, "model-replay")
	client := mocks.NewScriptedClient(mocks.TextReply("回答", "已处理"))
	var s sessions
	crashBeforeNodeCommit(e)
	_, err := compile(t, e, agentGraph(t, s.scripted(client, "answer", nil)), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.ErrorIs(t, err, errInjected)
	require.Equal(t, 1, e.fault.Hits())

	e.restart()
	snap, err := compile(t, e, agentGraph(t, s.scripted(client, "answer", nil)), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.NoError(t, err)
	require.Equal(t, "已处理", snap.State.Answer)
	require.Equal(t, 1, client.Calls(), "恢复后从日志回放模型请求")
	used, err := e.controller.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, used.ReservedRequests, "恢复后根预算不重复扣减")
}

func TestDurableCompletedToolReplay(t *testing.T) {
	e := newEnv(t, "tool-replay")
	var lookups atomic.Int32
	client := mocks.NewScriptedClient(mocks.ToolCallReply("查询", mocks.ToolCall("c1", "lookup", `{}`)), mocks.TextReply("回答", "订单已发货"))
	var s sessions
	build := func() *workflow.DurableGraph[ticket] {
		return compile(t, e, agentGraph(t, s.scripted(client, "answer", withTools(t, tools.DispatchConfig{}, countingTool(t, "lookup", "已发货", &lookups)))), "v1")
	}
	e.fault.arm(false, operationFails("model/turn/1", "pending"))
	_, err := build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.ErrorIs(t, err, errInjected)
	require.Equal(t, 1, e.fault.Hits())

	e.restart()
	snap, err := build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.NoError(t, err)
	require.Equal(t, "订单已发货", snap.State.Answer)
	require.EqualValues(t, 1, lookups.Load(), "已完成的工具不再执行")
	require.Equal(t, 2, client.Calls())
}

func runnableLookup(t *testing.T, resumable bool, calls *atomic.Int32) interfaces.Tool {
	t.Helper()
	tool, err := tools.NewRunnableTool(core.NewRunnableFunc(func(context.Context, string) (string, error) {
		calls.Add(1)
		return "已发货", nil
	}), tools.RunnableToolConfig[string, string]{
		Name: "lookup", Description: "查询订单", Version: "v1", Resumable: resumable,
		ArgsSchema: `{"type":"object","properties":{}}`,
		Decode:     func(map[string]any) (string, error) { return "", nil },
		Encode:     func(s string) (any, error) { return s, nil },
	})
	require.NoError(t, err)
	return tool
}

func TestDurablePendingTool(t *testing.T) {
	t.Run("默认工具拒绝", func(t *testing.T) {
		e := newEnv(t, "tool-pending")
		var calls atomic.Int32
		client := mocks.NewScriptedClient(mocks.ToolCallReply("查询", mocks.ToolCall("c1", "lookup", `{}`)), mocks.TextReply("回答", "x"))
		var s sessions
		build := func() *workflow.DurableGraph[ticket] {
			return compile(t, e, agentGraph(t, s.scripted(client, "answer", withTools(t, tools.DispatchConfig{}, runnableLookup(t, false, &calls)))), "v1")
		}
		e.fault.arm(false, operationFails("tool/turn/0/call/c1", "completed"))
		_, err := build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
		require.ErrorIs(t, err, errInjected)
		e.restart()
		_, err = build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
		var opErr *durable.OperationError
		require.ErrorAs(t, err, &opErr)
		require.Equal(t, "ambiguous", opErr.Kind)
		require.EqualValues(t, 1, calls.Load())
	})
	t.Run("Resumable 工具重入三次", func(t *testing.T) {
		e := newEnv(t, "tool-pending-resumable")
		var calls atomic.Int32
		client := mocks.NewScriptedClient(mocks.ToolCallReply("查询", mocks.ToolCall("c1", "lookup", `{}`)), mocks.TextReply("回答", "x"))
		var s sessions
		build := func() *workflow.DurableGraph[ticket] {
			return compile(t, e, agentGraph(t, s.scripted(client, "answer", withTools(t, tools.DispatchConfig{}, runnableLookup(t, true, &calls)))), "v1")
		}
		e.fault.arm(true, operationFails("tool/turn/0/call/c1", "completed"))
		for attempt := 1; attempt <= 3; attempt++ {
			_, err := build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
			require.ErrorIs(t, err, errInjected, "第 %d 次尝试", attempt)
			e.restart()
		}
		_, err := build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
		var opErr *durable.OperationError
		require.ErrorAs(t, err, &opErr, "第 4 次尝试时尝试耗尽")
		require.EqualValues(t, 3, calls.Load())
		require.Equal(t, 1, client.Calls(), "模型回合每次都从日志回放")
	})
}

// interruptedModel 构造模型请求中断测试的 Agent：配置一个经 Dispatcher 包装的工具，使请求走 model/turn/0。
func interruptedModel(t *testing.T, e *env, client *mocks.ScriptedClient, resend bool, key *atomic.Value) *workflow.DurableGraph[ticket] {
	t.Helper()
	var lookups atomic.Int32
	var s sessions
	tool := withTools(t, tools.DispatchConfig{}, countingTool(t, "lookup", "x", &lookups))
	factory := s.scripted(client, "answer", func(b *builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder {
		b = tool(b)
		if resend {
			b = b.WithInterruptedModelResend()
		}
		return b
	})
	return compile(t, e, agentGraph(t, keyed(factory, key)), "v1")
}

func TestDurableModelRequestInterrupted(t *testing.T) {
	for _, resend := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending/resend=%v", resend), func(t *testing.T) {
			e := newEnv(t, "model-pending")
			client := mocks.NewScriptedClient(mocks.TextReply("首次", "丢失"), mocks.TextReply("重发", "已处理"))
			var key atomic.Value
			e.fault.arm(false, operationFails("model/turn/0", "completed"))
			_, err := interruptedModel(t, e, client, resend, &key).Run(e.ctx(), e.id, ticket{Text: "q"}, "")
			require.ErrorIs(t, err, errInjected)
			require.Equal(t, 1, e.fault.Hits())
			// Do 返回的内存记录已是 completed，必须重新读取存储确认落盘的是 pending。
			r := inspect(t, e, key.Load().(string), "answer", "model/turn/0")
			require.Equal(t, "pending", r.Status)
			require.Equal(t, "ambiguous", r.FailureKind)
			require.Equal(t, 1, r.Attempts)
			require.Equal(t, 1, client.Calls())

			e.restart()
			snap, err := interruptedModel(t, e, client, resend, &key).Run(e.ctx(), e.id, ticket{Text: "q"}, "")
			if !resend {
				var opErr *durable.OperationError
				require.ErrorAs(t, err, &opErr)
				require.Equal(t, "ambiguous", opErr.Kind)
				require.Equal(t, 1, client.Calls())
				return
			}
			require.NoError(t, err)
			require.Equal(t, "已处理", snap.State.Answer)
			require.Equal(t, 2, client.Calls())
			require.Equal(t, 2, inspect(t, e, key.Load().(string), "answer", "model/turn/0").Attempts)
		})
		t.Run(fmt.Sprintf("canceled/resend=%v", resend), func(t *testing.T) {
			e := newEnv(t, "model-canceled")
			started := make(chan struct{})
			client := mocks.NewScriptedClient(
				mocks.ScriptedRule{Name: "挂起", Respond: func(ctx context.Context, _ *llm.CompletionRequest) (mocks.ScriptedReply, error) {
					close(started)
					<-ctx.Done()
					return mocks.ScriptedReply{}, ctx.Err()
				}},
				mocks.TextReply("重发", "已处理"),
			)
			var key atomic.Value
			ctx, stop := context.WithCancel(e.ctx())
			done := make(chan error, 1)
			d := interruptedModel(t, e, client, resend, &key)
			go func() {
				_, err := d.Run(ctx, e.id, ticket{Text: "q"}, "")
				done <- err
			}()
			<-started
			stop()
			require.ErrorIs(t, <-done, context.Canceled)
			// Agent 的流在取消时立即返回，内部可能尚未保存终态；InspectOperation 先取操作锁再读。
			r := inspect(t, e, key.Load().(string), "answer", "model/turn/0")
			require.Equal(t, "failed", r.Status)
			require.Equal(t, "canceled", r.FailureKind)

			e.restart()
			snap, err := interruptedModel(t, e, client, resend, &key).Run(e.ctx(), e.id, ticket{Text: "q"}, "")
			if !resend {
				var opErr *durable.OperationError
				require.ErrorAs(t, err, &opErr)
				require.Equal(t, "canceled", opErr.Kind)
				require.Equal(t, 1, client.Calls())
				return
			}
			require.NoError(t, err)
			require.Equal(t, "已处理", snap.State.Answer)
			require.Equal(t, 2, client.Calls())
		})
	}
}

func TestDurableLoopRoundsUseDistinctKeys(t *testing.T) {
	e := newEnv(t, "loop-keys")
	client := mocks.NewScriptedClient(mocks.TextReply("第一稿", "稿一"), mocks.TextReply("第二稿", "稿二"))
	var s sessions
	var keys []string
	inner := s.scripted(client, "writer", nil)
	factory := func(ctx context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		keys = append(keys, workflow.DurableKey(ctx))
		return inner(ctx)
	}
	write, err := TextStep("write", factory)
	require.NoError(t, err)
	g, err := Loop(workflow.Config{}, LoopConfig[Conversation]{
		Steps: []Step[Conversation]{write}, MaxIterations: 2,
		Until: func(_ context.Context, c Conversation) (bool, error) { return len(c.Turns) >= 2, nil },
	})
	require.NoError(t, err)
	snap, err := compile(t, e, g, "v1").Run(e.ctx(), e.id, Conversation{Input: "写回复"}, "")
	require.NoError(t, err)
	require.Len(t, snap.State.Turns, 2)
	require.Len(t, keys, 2)
	require.NotEqual(t, keys[0], keys[1], "执行键包含步数")
	first := inspect(t, e, keys[0], "writer", "model/answer/0")
	second := inspect(t, e, keys[1], "writer", "model/answer/0")
	require.Equal(t, "completed", first.Status)
	require.Equal(t, "completed", second.Status)
	require.NotEqual(t, string(first.Value), string(second.Value), "两轮日志互不覆盖")
}

func TestDurableBudgetConfiguration(t *testing.T) {
	crashed := func(t *testing.T) (*env, *mocks.ScriptedClient) {
		e := newEnv(t, "budget-config")
		client := mocks.NewScriptedClient(mocks.TextReply("回答", "已处理"))
		var s sessions
		crashBeforeNodeCommit(e)
		_, err := compile(t, e, agentGraph(t, s.scripted(client, "answer", nil)), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
		require.ErrorIs(t, err, errInjected)
		return e, client
	}
	resume := func(t *testing.T, e *env, client *mocks.ScriptedClient) error {
		var s sessions
		e.restart()
		_, err := compile(t, e, agentGraph(t, s.scripted(client, "answer", nil)), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
		return err
	}
	t.Run("复用预算存储但配置变化", func(t *testing.T) {
		e, _ := crashed(t)
		budgetStore, err := durable.NewFileStore(e.budgetDir)
		require.NoError(t, err)
		changed := e.budget
		changed.DeadlineUnixMilli = 4102444800000 // 重启时重新计算截止时间
		_, err = budget.NewDurable(changed, budgetStore)
		require.ErrorContains(t, err, "budget configuration or record changed")
	})
	t.Run("新预算存储且配置变化", func(t *testing.T) {
		e, client := crashed(t)
		e.budgetDir = t.TempDir()
		e.budget.MaxModelRequests++
		require.ErrorContains(t, resume(t, e, client), "operation intent or attempt policy changed")
		require.Equal(t, 1, client.Calls())
	})
	t.Run("新预算存储且配置相同", func(t *testing.T) {
		e, client := crashed(t)
		e.budgetDir = t.TempDir()
		require.ErrorContains(t, resume(t, e, client), "usage has no reservation")
		require.Equal(t, 1, client.Calls())
	})
}

// triageGraph 构造文本分流：triage 的输出首行决定成员；fallback 为真时配置转人工兜底。
func triageGraph(t *testing.T, triage, refund *mocks.ScriptedClient, fallback bool, human *[]Conversation) *workflow.Graph[Conversation] {
	t.Helper()
	var s sessions
	sup, err := TextStep("triage", s.scripted(triage, "triage", nil))
	require.NoError(t, err)
	member, err := TextStep("refund", s.scripted(refund, "refund", nil))
	require.NoError(t, err)
	next, err := TextNext(member)
	require.NoError(t, err)
	cfg := RouterConfig[Conversation]{Supervisor: sup, Members: []Step[Conversation]{member}, Next: next, MaxRounds: 3}
	if fallback {
		cfg.Fallback = &Step[Conversation]{Name: "human", Run: core.NewRunnableFunc(func(_ context.Context, c Conversation) (Conversation, error) {
			*human = append(*human, c)
			c.Turns = append(c.Turns, Turn{Agent: "human", Output: "已转人工"})
			return c, nil
		})}
	}
	g, err := RouterSupervisor(workflow.Config{}, cfg)
	require.NoError(t, err)
	return g
}

func TestDurableFallback(t *testing.T) {
	e := newEnv(t, "fallback")
	triage := mocks.NewScriptedClient(mocks.TextReply("无效分流", "退款（原因：重复扣款）"))
	var human []Conversation
	snap, err := compile(t, e, triageGraph(t, triage, mocks.NewScriptedClient(), true, &human), "v1").Run(e.ctx(), e.id, Conversation{Input: "重复扣款"}, "")
	require.NoError(t, err)
	require.Len(t, human, 1)
	require.Equal(t, "已转人工", snap.State.Turns[1].Output)

	e = newEnv(t, "no-fallback")
	triage = mocks.NewScriptedClient(mocks.TextReply("无效分流", "退款（原因：重复扣款）"))
	d := compile(t, e, triageGraph(t, triage, mocks.NewScriptedClient(), false, nil), "v1")
	_, err = d.Run(e.ctx(), e.id, Conversation{Input: "重复扣款"}, "")
	require.ErrorIs(t, err, agentErrors.ErrInvalidRoute)
	_, err = d.Run(e.ctx(), e.id, Conversation{Input: "重复扣款"}, "")
	require.ErrorIs(t, err, agentErrors.ErrInvalidRoute, "无兜底时对该运行 ID 是终态")
	require.Equal(t, 1, triage.Calls())
}

// routedTo 匹配“路由结果为 node”的那次检查点保存：非操作记录、Key 为空、Next > 0、Routing 为假。
func routedTo(node string) func(string, *durable.Snapshot) bool {
	return func(_ string, s *durable.Snapshot) bool {
		v, ok := checkpoint(s)
		return ok && s.Key == "" && s.Next > 0 && !v.Routing && v.Node == node
	}
}

func TestDurableFallbackRerouteWindow(t *testing.T) {
	t.Run("Router", func(t *testing.T) {
		e := newEnv(t, "reroute-router")
		triage := mocks.NewScriptedClient(mocks.TextReply("无效分流", "不知道"))
		var human []Conversation
		e.fault.arm(false, routedTo("human"))
		d := compile(t, e, triageGraph(t, triage, mocks.NewScriptedClient(), true, &human), "v1")
		_, err := d.Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.ErrorIs(t, err, errInjected)
		require.Equal(t, 1, e.fault.Hits())
		snap, err := d.Snapshot(context.Background(), e.id)
		require.NoError(t, err)
		require.Equal(t, "triage", snap.Node)
		require.True(t, snap.Routing)

		e.restart()
		_, err = compile(t, e, triageGraph(t, triage, mocks.NewScriptedClient(), true, &human), "v1").Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.NoError(t, err)
		require.Equal(t, 1, triage.Calls(), "重新路由不重新请求分流模型")
		require.Len(t, human, 1)
		require.Equal(t, "不知道", human[0].Turns[0].Output, "兜底收到无效的那次输出")
	})
	t.Run("Loop", func(t *testing.T) {
		e := newEnv(t, "reroute-loop")
		var fallbacks atomic.Int32
		build := func() *workflow.DurableGraph[record] {
			fb := Step[record]{Name: "human", Run: core.NewRunnableFunc(func(_ context.Context, r record) (record, error) {
				fallbacks.Add(1)
				return r, nil
			})}
			g, err := Loop(workflow.Config{}, LoopConfig[record]{
				Steps: []Step[record]{mark("review")}, MaxIterations: 2, Fallback: &fb,
				Until: func(context.Context, record) (bool, error) { return false, agentErrors.ErrInvalidRoute },
			})
			require.NoError(t, err)
			return compile(t, e, g, "v1")
		}
		e.fault.arm(false, routedTo("human"))
		_, err := build().Run(e.ctx(), e.id, record{}, "")
		require.ErrorIs(t, err, errInjected)
		e.restart()
		snap, err := build().Run(e.ctx(), e.id, record{}, "")
		require.NoError(t, err)
		require.Equal(t, []string{"review"}, snap.State.Log)
		require.EqualValues(t, 1, fallbacks.Load())
	})
}

func TestDurableRenderDeterminism(t *testing.T) {
	graph := func(t *testing.T, client *mocks.ScriptedClient, render func(Conversation) any) *workflow.Graph[Conversation] {
		var s sessions
		step, err := TextStep("answer", s.scripted(client, "answer", nil), WithRender(render))
		require.NoError(t, err)
		g, err := Sequential(workflow.Config{}, step)
		require.NoError(t, err)
		return g
	}
	upper := func(c Conversation) any { return strings.ToUpper(c.Input) }
	for name, after := range map[string]func(Conversation) any{
		"相同 render 回放": upper,
		"替换 render 报错": func(c Conversation) any { return "新格式：" + c.Input },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, "render")
			client := mocks.NewScriptedClient(mocks.TextReply("回答", "ok"))
			crashBeforeNodeCommit(e)
			_, err := compile(t, e, graph(t, client, upper), "v1").Run(e.ctx(), e.id, Conversation{Input: "refund"}, "")
			require.ErrorIs(t, err, errInjected)
			e.restart()
			_, err = compile(t, e, graph(t, client, after), "v1").Run(e.ctx(), e.id, Conversation{Input: "refund"}, "")
			if name == "相同 render 回放" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "operation intent or attempt policy changed")
			}
			require.Equal(t, 1, client.Calls())
		})
	}
}

func TestDurableContractPreconditions(t *testing.T) {
	e := newEnv(t, "contract")
	client := mocks.NewScriptedClient(mocks.TextReply("不应发出", "x"))
	var s sessions
	_, err := compile(t, e, agentGraph(t, s.scripted(client, "", nil)), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.ErrorContains(t, err, "WithDurableScope")

	e = newEnv(t, "contract-tool")
	var calls atomic.Int32
	raw := countingTool(t, "lookup", "x", &calls)
	_, err = compile(t, e, agentGraph(t, s.scripted(client, "answer", func(b *builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder {
		return b.WithTools(raw)
	})), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.ErrorContains(t, err, "Dispatcher.Tools")
	require.Zero(t, client.Calls(), "在模型调用前失败")
}

func TestDurableVersionMigration(t *testing.T) {
	e := newEnv(t, "version")
	loop := func(iterations int) *workflow.Graph[record] {
		g, err := Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("write"), mark("review")}, Until: untilRounds("review", 2), MaxIterations: iterations})
		require.NoError(t, err)
		return g
	}
	old := compile(t, e, loop(2), "v1", "review")
	snap, err := old.Run(context.Background(), e.id, record{}, "")
	require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)

	_, err = compile(t, e, loop(3), "v1", "review").Run(context.Background(), e.id, record{}, snap.Key)
	require.ErrorContains(t, err, "checkpoint does not match versioned graph")
	_, err = compile(t, e, loop(3), "v2", "review").Run(context.Background(), e.id, record{}, snap.Key)
	require.ErrorContains(t, err, "checkpoint does not match versioned graph", "提升 Version 也不能恢复旧运行")

	stored, err := e.store.Load(context.Background(), e.id)
	require.NoError(t, err)
	require.Equal(t, "v1", stored.Version, "检查点自带版本，宿主据此选择旧图")
	snap, err = compile(t, e, loop(2), "v1", "review").Run(context.Background(), e.id, record{}, snap.Key)
	require.ErrorIs(t, err, agentErrors.ErrApprovalRequired, "第二轮评审仍需审批")
	snap, err = compile(t, e, loop(2), "v1", "review").Run(context.Background(), e.id, record{}, snap.Key)
	require.NoError(t, err)
	require.Equal(t, []string{"write", "review", "write", "review"}, snap.State.Log)
}
