package multiagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

func memoryBudget(t *testing.T) context.Context {
	t.Helper()
	c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 20})
	require.NoError(t, err)
	return budget.WithController(context.Background(), c)
}

func memberTool(t *testing.T, m Member) interfaces.Tool {
	t.Helper()
	list, err := SupervisorTools(m)
	require.NoError(t, err)
	return list[0]
}

func TestSupervisorToolsValidation(t *testing.T) {
	var s sessions
	factory := s.fake(func(context.Context, any) (any, error) { return "", nil })
	ok := Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: factory}
	_, err := SupervisorTools()
	require.Error(t, err)
	for name, m := range map[string]Member{
		"缺 Name":        {Description: "d", Version: "v1", Factory: factory},
		"缺 Description": {Name: "a", Version: "v1", Factory: factory},
		"缺 Version":     {Name: "a", Description: "d", Factory: factory},
		"缺 Factory":     {Name: "a", Description: "d", Version: "v1"},
		"负 Timeout":     {Name: "a", Description: "d", Version: "v1", Factory: factory, Timeout: -time.Second},
	} {
		_, err := SupervisorTools(m)
		require.Error(t, err, name)
	}
	_, err = SupervisorTools(ok, ok)
	require.ErrorContains(t, err, "重复")
}

func TestSupervisorToolsDirectInvoke(t *testing.T) {
	var s sessions
	tool := memberTool(t, Member{
		Name: "billing", Description: "账单专家", Version: "v1",
		Factory: s.fake(func(_ context.Context, in any) (any, error) { return map[string]string{"task": in.(string)}, nil }),
	})
	out, err := tool.Invoke(memoryBudget(t), &interfaces.ToolInput{Args: map[string]any{"task": "查账单"}})
	require.NoError(t, err)
	require.Equal(t, `{"task":"查账单"}`, out.Result, "非字符串结果按 JSON 编码")
	require.EqualValues(t, 1, s.closed.Load())

	_, err = tool.Invoke(memoryBudget(t), &interfaces.ToolInput{Args: map[string]any{}})
	var argErr *tools.ArgumentError
	require.ErrorAs(t, err, &argErr)

	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"task": "x"}})
	require.ErrorContains(t, err, "root budget", "没有根预算时失败")

	e := newEnv(t, "nondurable-budget")
	journaled := durable.WithJournal(durable.WithExecutionKey(memoryBudget(t), "node"), e.store)
	_, err = tool.Invoke(journaled, &interfaces.ToolInput{Args: map[string]any{"task": "x"}})
	require.ErrorContains(t, err, "durable root budget", "持久运行中使用非持久预算时失败")
}

func TestSupervisorToolsClosesSessionWhenFactoryFails(t *testing.T) {
	closed := 0
	tool := memberTool(t, Member{
		Name: "billing", Description: "账单专家", Version: "v1",
		Factory: func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{Close: func(context.Context) error { closed++; return nil }}, errors.New("会话创建失败")
		},
	})
	_, err := tool.Invoke(memoryBudget(t), &interfaces.ToolInput{Args: map[string]any{"task": "x"}})
	require.ErrorContains(t, err, "会话创建失败")
	require.Equal(t, 1, closed)
}

// blocking 返回阻塞到 ctx 结束或 d 之后才回答的会话工厂。
func blocking(s *sessions, d time.Duration) AgentFactory {
	return s.fake(func(ctx context.Context, _ any) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d):
			return "完成", nil
		}
	})
}

func dispatch(t *testing.T, cfg tools.DispatchConfig, tool interfaces.Tool) *tools.Dispatcher {
	t.Helper()
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tool))
	d, err := tools.NewDispatcher(reg, cfg)
	require.NoError(t, err)
	return d
}

func TestSupervisorToolsTimeout(t *testing.T) {
	call := tools.ToolRequest{Name: "billing", Input: &interfaces.ToolInput{Args: map[string]any{"task": "x"}}}
	var own sessions
	timed := memberTool(t, Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: blocking(&own, 10*time.Second), Timeout: 50 * time.Millisecond})
	_, err := dispatch(t, tools.DispatchConfig{}, timed).Invoke(memoryBudget(t), call)
	require.ErrorIs(t, err, context.DeadlineExceeded, "正值覆盖 Dispatcher 默认超时")
	require.EqualValues(t, 1, own.closed.Load(), "超时取消后仍调用 Close")

	var inherited sessions
	tool := memberTool(t, Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: blocking(&inherited, 10*time.Second)})
	_, err = dispatch(t, tools.DispatchConfig{Timeout: 50 * time.Millisecond}, tool).Invoke(memoryBudget(t), call)
	require.ErrorIs(t, err, context.DeadlineExceeded, "0 继承 Dispatcher 默认")

	var direct sessions
	slow := memberTool(t, Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: blocking(&direct, 150*time.Millisecond), Timeout: 50 * time.Millisecond})
	out, err := slow.Invoke(memoryBudget(t), call.Input)
	require.NoError(t, err, "直接 Invoke 不受 Timeout 约束")
	require.Equal(t, "完成", out.Result)
}

// supervisorAgent 构造带成员工具的 Supervisor 会话工厂；Supervisor 使用独立 Dispatcher。
func supervisorAgent(t *testing.T, s *sessions, client *mocks.ScriptedClient, cfg tools.DispatchConfig, members ...Member) AgentFactory {
	t.Helper()
	list, err := SupervisorTools(members...)
	require.NoError(t, err)
	return s.scripted(client, "supervisor", withTools(t, cfg, list...))
}

// runDelegation 让 Supervisor 在一个回合内委派账单与技术两个成员，再汇总回答；返回成员会话统计。
func runDelegation(t *testing.T) *sessions {
	t.Helper()
	var members, sup sessions
	billing := mocks.NewScriptedClient(mocks.ScriptedRule{Name: "账单", Match: mocks.RequestContains("核对扣款"), Reply: mocks.ScriptedReply{Text: "重复扣款 1 次"}})
	tech := mocks.NewScriptedClient(mocks.ScriptedRule{Name: "技术", Match: mocks.RequestContains("登录失败"), Reply: mocks.ScriptedReply{Text: "需重置密码"}})
	supervisor := mocks.NewScriptedClient(
		mocks.ToolCallReply("委派", mocks.ToolCall("c1", "billing", `{"task":"核对扣款"}`), mocks.ToolCall("c2", "tech", `{"task":"登录失败"}`)),
		mocks.ScriptedRule{Name: "汇总", Match: mocks.RequestContains("重复扣款 1 次", "需重置密码"), Reply: mocks.ScriptedReply{Text: "已退款并指导重置密码"}},
	)
	factory := supervisorAgent(t, &sup, supervisor, tools.DispatchConfig{},
		Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: members.scripted(billing, "billing", nil)},
		Member{Name: "tech", Description: "技术专家", Version: "v1", Factory: members.scripted(tech, "tech", nil)})
	step, err := AgentStep("supervisor", factory, askText, keepAnswer)
	require.NoError(t, err)
	g, err := Sequential(workflow.Config{}, step)
	require.NoError(t, err)
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(memoryBudget(t), ticket{Text: "扣款两次且无法登录"})
	require.NoError(t, err)
	require.Equal(t, "已退款并指导重置密码", out.Answer)
	require.Zero(t, supervisor.Remaining())
	return &members
}

func TestSupervisorToolsDelegatesAndClosesEachSession(t *testing.T) {
	members := runDelegation(t)
	require.EqualValues(t, 2, members.opened.Load(), "每次调用新建会话")
	require.EqualValues(t, 2, members.closed.Load())
}

func TestSupervisorToolsArgumentErrors(t *testing.T) {
	for _, feedback := range []bool{false, true} {
		var members, sup sessions
		billing := mocks.NewScriptedClient(mocks.TextReply("账单", "已核对"))
		rules := []mocks.ScriptedRule{mocks.ToolCallReply("缺 task", mocks.ToolCall("c1", "billing", `{}`))}
		if feedback {
			rules = append(rules,
				mocks.ScriptedRule{Name: "改正", Match: mocks.RequestContains("task"), Reply: mocks.ScriptedReply{ToolCalls: []llm.ToolCall{mocks.ToolCall("c2", "billing", `{"task":"核对"}`)}}},
				mocks.TextReply("汇总", "完成"))
		}
		supervisor := mocks.NewScriptedClient(rules...)
		factory := supervisorAgent(t, &sup, supervisor, tools.DispatchConfig{ReturnArgumentErrors: feedback},
			Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: members.scripted(billing, "billing", nil)})
		step, err := AgentStep("supervisor", factory, askText, keepAnswer)
		require.NoError(t, err)
		g, err := Sequential(workflow.Config{}, step)
		require.NoError(t, err)
		e := newEnv(t, "argument-errors")
		snap, err := compile(t, e, g, "v1").Run(e.ctx(), e.id, ticket{Text: "核对扣款"}, "")
		if !feedback {
			require.ErrorContains(t, err, "invalid arguments", "持久运行默认拒绝整个模型回合")
			require.Zero(t, billing.Calls())
			continue
		}
		require.NoError(t, err)
		require.Equal(t, "完成", snap.State.Answer)
		require.Equal(t, 1, billing.Calls())
	}
}

// interruptibleConfirm 是成员 Agent 的可中断工具：等待客户确认后返回确认内容。
func interruptibleConfirm(t *testing.T) interfaces.Tool {
	t.Helper()
	tool, err := tools.NewFunctionToolBuilder("confirm").WithDescription("向客户确认").
		WithArgsSchema(`{"type":"object","properties":{}}`).WithInterruptible("v1").
		WithFunction(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
			raw, err := durable.AwaitInput(ctx, "confirm", json.RawMessage(`{"question":"确认账单？"}`), nil)
			if err != nil {
				return nil, err
			}
			return string(raw), nil
		}).Build()
	require.NoError(t, err)
	return tool
}

// runResumableDelegation 把带 Resumable 成员的 Supervisor 放进 CompileDurable 单节点图：
// 成员经可中断工具等待，续跑后完成，且成员所在的持久操作没有新增尝试。
func runResumableDelegation(t *testing.T) {
	t.Helper()
	e := newEnv(t, "resumable-member")
	member := mocks.NewScriptedClient(
		mocks.ToolCallReply("确认", mocks.ToolCall("m1", "confirm", `{}`)),
		mocks.ScriptedRule{Name: "完成", Match: mocks.RequestContains(`"yes"`), Reply: mocks.ScriptedReply{Text: "账单已核对"}},
	)
	supervisor := mocks.NewScriptedClient(
		mocks.ToolCallReply("委派", mocks.ToolCall("c1", "billing", `{"task":"核对账单"}`)),
		mocks.ScriptedRule{Name: "汇总", Match: mocks.RequestContains("账单已核对"), Reply: mocks.ScriptedReply{Text: "已核对"}},
	)
	var members, sup sessions
	var attempts []int
	inner := members.scripted(member, "billing", withTools(t, tools.DispatchConfig{}, interruptibleConfirm(t)))
	factory := func(ctx context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		attempts = append(attempts, durable.OperationAttempt(ctx))
		return inner(ctx)
	}
	build := func() *workflow.DurableGraph[ticket] {
		step, err := AgentStep("supervisor", supervisorAgent(t, &sup, supervisor, tools.DispatchConfig{},
			Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: factory, Resumable: true}), askText, keepAnswer)
		require.NoError(t, err)
		g, err := Sequential(workflow.Config{}, step)
		require.NoError(t, err)
		return compile(t, e, g, "v1")
	}
	snap, err := build().Run(e.ctx(), e.id, ticket{Text: "核对账单"}, "")
	require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)
	require.Len(t, snap.Interrupts, 1)

	e.restart()
	snap, err = build().Resume(e.ctx(), e.id, workflow.ResumeRequest{Key: snap.Interrupts[0].Key, Input: json.RawMessage(`"yes"`)})
	require.NoError(t, err)
	require.Equal(t, "已核对", snap.State.Answer)
	require.Equal(t, []int{1, 1}, attempts, "从等待续跑不计尝试")
	require.Equal(t, 2, supervisor.Calls())
	require.Equal(t, 2, member.Calls())
}

func TestSupervisorToolsResumableMember(t *testing.T) {
	runResumableDelegation(t)

	var s sessions
	tool := memberTool(t, Member{
		Name: "billing", Description: "账单专家", Version: "v1", Resumable: true,
		Factory: s.fake(func(context.Context, any) (any, error) { return "x", nil }),
	})
	_, err := tool.Invoke(memoryBudget(t), &interfaces.ToolInput{Args: map[string]any{"task": "x"}})
	require.ErrorContains(t, err, "requires a journaled dispatcher call", "无持久日志的调用被拒绝")
	require.Zero(t, s.opened.Load())
}
