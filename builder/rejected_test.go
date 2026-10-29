package builder

import (
	"context"
	"strings"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func rejectingDispatcher(t *testing.T, calls *int) *tools.Dispatcher {
	t.Helper()
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tools.NewBaseTool("refund", "退款", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		*calls++
		return tools.Rejected("订单不存在", nil), nil
	})))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	return d
}

func rejectionTurns() [][]*llm.StreamChunk {
	return [][]*llm.StreamChunk{
		{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "refund", `{}`)}}},
		{{Delta: "未找到该订单"}, {Done: true, FinishReason: "stop"}},
	}
}

func TestRejectedToolResultReachesModelAndReplays(t *testing.T) {
	ctx := journalContextForTest(t)
	calls := 0
	model := nativeModel(rejectionTurns()...)
	agent, err := NewSimpleBuilder(model).WithDurableScope("refund").WithTools(rejectingDispatcher(t, &calls).Tools()...).WithMaxIterations(2).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(ctx, "退订单 A1")
	require.NoError(t, err)
	require.Equal(t, "未找到该订单", out.Result)
	require.Len(t, model.requests, 2)
	last := model.requests[1].Messages[len(model.requests[1].Messages)-1]
	require.Equal(t, "tool", last.Role)
	require.True(t, strings.Contains(last.Content, "订单不存在"), last.Content)

	model.requests, model.turns = nil, nil
	again, err := agent.AsRunnable().Invoke(ctx, "退订单 A1")
	require.NoError(t, err)
	require.Equal(t, out.Result, again.Result)
	require.Equal(t, 1, calls, "恢复时只回放")
	require.Empty(t, model.requests)
}

func TestRejectedDirectToolContinuesToModel(t *testing.T) {
	calls := 0
	model := nativeModel(rejectionTurns()...)
	agent, err := NewSimpleBuilder(model).WithTools(rejectingDispatcher(t, &calls).Tools()...).WithReturnDirectTools("refund").WithMaxIterations(2).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "退订单 A1")
	require.NoError(t, err)
	require.Equal(t, "未找到该订单", out.Result, "业务拒绝不能作为直接答案")
	require.Len(t, model.requests, 2)
}
