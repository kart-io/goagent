package multiagent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

type ticket struct {
	Text   string
	Answer string
}

func askText(t ticket) (any, error) { return t.Text, nil }

func keepAnswer(t ticket, o *builder.AgentOutput) (ticket, error) {
	t.Answer, _ = o.Result.(string)
	return t, nil
}

// runOne 把单个 Step 放进 START → s → END 的图并执行。
func runOne(t *testing.T, s Step[ticket], in ticket) (ticket, error) {
	t.Helper()
	g := workflow.NewGraph[ticket]()
	require.NoError(t, g.AddRunnable(s.Name, s.Run))
	require.NoError(t, g.AddEdge(constants.WorkflowStart, s.Name))
	require.NoError(t, g.AddEdge(s.Name, constants.WorkflowEnd))
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	return run.Invoke(context.Background(), in)
}

func TestAgentStepOpensAndClosesSessionPerExecution(t *testing.T) {
	var s sessions
	step, err := AgentStep("answer", s.fake(func(_ context.Context, in any) (any, error) { return "答：" + in.(string), nil }), askText, keepAnswer)
	require.NoError(t, err)
	out, err := runOne(t, step, ticket{Text: "退款"})
	require.NoError(t, err)
	require.Equal(t, "答：退款", out.Answer)
	_, err = runOne(t, step, ticket{Text: "物流"})
	require.NoError(t, err)
	require.EqualValues(t, 2, s.opened.Load())
	require.EqualValues(t, 2, s.closed.Load())
}

func TestAgentStepValidation(t *testing.T) {
	var s sessions
	agent := s.fake(func(context.Context, any) (any, error) { return "", nil })
	for name, build := range map[string]func() (Step[ticket], error){
		"空名称":      func() (Step[ticket], error) { return AgentStep("", agent, askText, keepAnswer) },
		"首尾空白":     func() (Step[ticket], error) { return AgentStep(" a", agent, askText, keepAnswer) },
		"保留名":      func() (Step[ticket], error) { return AgentStep(constants.WorkflowEnd, agent, askText, keepAnswer) },
		"agent 为空": func() (Step[ticket], error) { return AgentStep("a", nil, askText, keepAnswer) },
		"in 为空":    func() (Step[ticket], error) { return AgentStep[ticket]("a", agent, nil, keepAnswer) },
		"out 为空":   func() (Step[ticket], error) { return AgentStep("a", agent, askText, nil) },
	} {
		_, err := build()
		require.Error(t, err, name)
	}
}

func TestAgentStepFactoryErrorStillCloses(t *testing.T) {
	closed := 0
	factory := func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		return tools.OwnedRunnable[any, *builder.AgentOutput]{Close: func(context.Context) error { closed++; return nil }}, errors.New("会话创建失败")
	}
	step, err := AgentStep("answer", factory, askText, keepAnswer)
	require.NoError(t, err)
	_, err = runOne(t, step, ticket{Text: "q"})
	require.ErrorContains(t, err, "会话创建失败")
	require.Equal(t, 1, closed)
}

func TestAgentStepRejectsIncompleteSessions(t *testing.T) {
	var typedNil *core.RunnableFunc[any, *builder.AgentOutput]
	nilAnswer := core.NewRunnableFunc(func(context.Context, any) (*builder.AgentOutput, error) { return nil, nil })
	closeOK := func(context.Context) error { return nil }
	for name, owned := range map[string]tools.OwnedRunnable[any, *builder.AgentOutput]{
		"Close 为空":    {Runnable: nilAnswer},
		"typed nil":   {Runnable: typedNil, Close: closeOK},
		"返回 nil, nil": {Runnable: nilAnswer, Close: closeOK},
	} {
		factory := func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) { return owned, nil }
		step, err := AgentStep("answer", factory, askText, keepAnswer)
		require.NoError(t, err)
		_, err = runOne(t, step, ticket{Text: "q"})
		require.Error(t, err, name)
		require.ErrorContains(t, err, `node "answer"`, name)
	}
}

func TestAgentStepJoinsExecutionAndCloseErrors(t *testing.T) {
	factory := func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		return tools.OwnedRunnable[any, *builder.AgentOutput]{
			Runnable: core.NewRunnableFunc(func(context.Context, any) (*builder.AgentOutput, error) { return nil, errors.New("模型失败") }),
			Close:    func(context.Context) error { return errors.New("关闭失败") },
		}, nil
	}
	step, err := AgentStep("answer", factory, askText, keepAnswer)
	require.NoError(t, err)
	_, err = runOne(t, step, ticket{Text: "q"})
	require.ErrorContains(t, err, "模型失败")
	require.ErrorContains(t, err, "关闭失败")
}

func TestAgentStepOutErrorNamesNode(t *testing.T) {
	var s sessions
	reject := func(ticket, *builder.AgentOutput) (ticket, error) {
		return ticket{}, errors.New("合规检查不通过")
	}
	step, err := AgentStep("compliance", s.fake(func(context.Context, any) (any, error) { return "含敏感词", nil }), askText, reject)
	require.NoError(t, err)
	_, err = runOne(t, step, ticket{Text: "q"})
	require.ErrorContains(t, err, `node "compliance"`)
	require.ErrorContains(t, err, "合规检查不通过")
}

func TestAgentStepInErrorSendsNoModelRequest(t *testing.T) {
	client := mocks.NewScriptedClient(mocks.TextReply("不应发出", "x"))
	var s sessions
	needOrder := func(t ticket) (any, error) {
		if t.Text == "" {
			return nil, errors.New("缺少工单内容")
		}
		return t.Text, nil
	}
	step, err := AgentStep("extract", s.scripted(client, "", nil), needOrder, keepAnswer)
	require.NoError(t, err)
	_, err = runOne(t, step, ticket{})
	require.ErrorContains(t, err, "缺少工单内容")
	require.Zero(t, client.Calls())
	require.Zero(t, s.opened.Load(), "映射失败时不创建会话")
}

func TestAgentStepWithRealBuilder(t *testing.T) {
	client := mocks.NewScriptedClient(mocks.ScriptedRule{Name: "回答", Match: mocks.RequestContains("订单 A1"), Reply: mocks.ScriptedReply{Text: "已查询"}})
	var s sessions
	step, err := AgentStep("answer", s.scripted(client, "", nil), askText, keepAnswer)
	require.NoError(t, err)
	out, err := runOne(t, step, ticket{Text: "订单 A1"})
	require.NoError(t, err)
	require.Equal(t, "已查询", out.Answer)
	require.Equal(t, 1, client.Calls())
	require.EqualValues(t, 1, s.closed.Load())
}
