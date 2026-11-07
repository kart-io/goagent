package builder

import (
	"context"
	"errors"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func failingTool(err error, calls *int) interfaces.Tool {
	return tools.NewBaseTool("flaky", "可能失败", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		*calls++
		if *calls == 1 {
			return nil, err
		}
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	})
}

func feedbackTurns(call llm.ToolCall) [][]*llm.StreamChunk {
	return [][]*llm.StreamChunk{
		{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{call}}},
		{{Delta: "已处理"}, {Done: true, FinishReason: "stop"}},
	}
}

func lastToolMessage(t *testing.T, model *nativeEventModel) string {
	t.Helper()
	require.Len(t, model.requests, 2)
	msgs := model.requests[1].Messages
	last := msgs[len(msgs)-1]
	require.Equal(t, "tool", last.Role)
	return last.Content
}

func TestToolErrorFeedbackDisabledByDefault(t *testing.T) {
	calls := 0
	model := nativeModel(feedbackTurns(nativeCall("c1", "flaky", `{}`))...)
	agent, err := NewSimpleBuilder(model).WithTools(failingTool(errors.New("磁盘已满"), &calls)).WithMaxIterations(3).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "go")
	require.ErrorContains(t, err, "磁盘已满")
}

func TestToolErrorFeedbackReturnsExecutionError(t *testing.T) {
	calls := 0
	model := nativeModel(feedbackTurns(nativeCall("c1", "flaky", `{}`))...)
	agent, err := NewSimpleBuilder(model).WithTools(failingTool(errors.New("磁盘已满"), &calls)).WithToolErrorFeedback().WithMaxIterations(3).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "go")
	require.NoError(t, err)
	require.Equal(t, "已处理", out.Result)
	require.Contains(t, lastToolMessage(t, model), "磁盘已满")
}

func TestToolErrorFeedbackUnofferedTool(t *testing.T) {
	calls := 0
	model := nativeModel(feedbackTurns(nativeCall("c1", "nope", `{}`))...)
	agent, err := NewSimpleBuilder(model).WithTools(failingTool(nil, &calls)).WithToolErrorFeedback().WithMaxIterations(3).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "go")
	require.NoError(t, err)
	msg := lastToolMessage(t, model)
	require.Contains(t, msg, "nope")
	require.Contains(t, msg, "flaky", "列出可用工具")
	require.Zero(t, calls)
}

func TestToolErrorFeedbackInvalidArguments(t *testing.T) {
	calls := 0
	model := nativeModel(feedbackTurns(nativeCall("c1", "flaky", `{"a":`))...)
	agent, err := NewSimpleBuilder(model).WithTools(failingTool(nil, &calls)).WithToolErrorFeedback().WithMaxIterations(3).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "go")
	require.NoError(t, err)
	require.Contains(t, lastToolMessage(t, model), "JSON")
	require.Zero(t, calls)
}

func TestToolErrorFeedbackKeepsFatalErrors(t *testing.T) {
	for name, fatal := range map[string]error{
		"budget": agentErrors.ErrModelBudgetExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			model := nativeModel(feedbackTurns(nativeCall("c1", "flaky", `{}`))...)
			agent, err := NewSimpleBuilder(model).WithTools(failingTool(fatal, &calls)).WithToolErrorFeedback().WithMaxIterations(3).Build()
			require.NoError(t, err)
			_, err = agent.AsRunnable().Invoke(context.Background(), "go")
			require.ErrorIs(t, err, fatal)
		})
	}
}

func TestToolErrorFeedbackKeepsPanic(t *testing.T) {
	model := nativeModel(feedbackTurns(nativeCall("c1", "boom", `{}`))...)
	boom := tools.NewBaseTool("boom", "panic", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		panic("bug")
	})
	agent, err := NewSimpleBuilder(model).WithTools(boom).WithToolErrorFeedback().WithMaxIterations(3).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "go")
	require.Error(t, err)
}

func TestToolErrorFeedbackDurableExecutionErrorStillFails(t *testing.T) {
	ctx := journalContextForTest(t)
	calls := 0
	model := nativeModel(feedbackTurns(nativeCall("c1", "flaky", `{}`))...)
	agent, err := NewSimpleBuilder(model).WithDurableScope("fb").WithTools(dispatched(t, failingTool(errors.New("磁盘已满"), &calls))...).WithToolErrorFeedback().WithMaxIterations(3).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "go")
	require.ErrorContains(t, err, "磁盘已满", "持久运行无法记录失败结果，保持中止")
}

func TestToolErrorFeedbackDurableUnofferedToolReplays(t *testing.T) {
	ctx := journalContextForTest(t)
	calls := 0
	model := nativeModel(feedbackTurns(nativeCall("c1", "nope", `{}`))...)
	agent, err := NewSimpleBuilder(model).WithDurableScope("fb").WithTools(dispatched(t, failingTool(nil, &calls))...).WithToolErrorFeedback().WithMaxIterations(3).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(ctx, "go")
	require.NoError(t, err)

	model.requests, model.turns = nil, nil
	again, err := agent.AsRunnable().Invoke(ctx, "go")
	require.NoError(t, err)
	require.Equal(t, out.Result, again.Result)
	require.Empty(t, model.requests)
}

func TestToolErrorFeedbackParallel(t *testing.T) {
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tools.NewBaseTool("bad", "失败", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, errors.New("连接被拒绝")
	})))
	require.NoError(t, reg.Register(tools.NewBaseTool("good", "成功", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "fine"}, nil
	})))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "bad", `{}`), nativeCall("c2", "good", `{}`)}}},
		[]*llm.StreamChunk{{Delta: "完成"}, {Done: true, FinishReason: "stop"}},
	)
	agent, err := NewSimpleBuilder(model).WithTools(d.Tools()...).WithParallelToolCalls(2).WithToolErrorFeedback().WithMaxIterations(3).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "go")
	require.NoError(t, err)
	msgs := model.requests[1].Messages
	require.Contains(t, msgs[len(msgs)-2].Content, "连接被拒绝")
	require.Contains(t, msgs[len(msgs)-1].Content, "fine")
}
