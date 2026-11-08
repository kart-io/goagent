package builder

import (
	"context"
	"errors"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReviewRT09BuilderConsumesSelectedTools(t *testing.T) {
	for _, query := range []string{"search", "你好"} {
		t.Run(query, func(t *testing.T) {
			model := nativeModel([]*llm.StreamChunk{{Delta: "答案"}, {Done: true, FinishReason: "stop"}})
			agent, err := NewSimpleBuilder(model).WithTools(NewMockTool("search", ""), NewMockTool("calculator", "")).WithMiddleware(middleware.NewToolSelectorMiddleware([]string{"search", "calculator"}, 2)).Build()
			require.NoError(t, err)
			_, err = agent.AsRunnable().Invoke(context.Background(), query)
			require.NoError(t, err)
			want := 0
			if query == "search" {
				want = 1
			}
			require.Len(t, model.toolSets[0], want)
		})
	}
}

func TestReviewRT09SelectorCannotBypassHostAuthorization(t *testing.T) {
	denied := errors.New("宿主拒绝授权")
	executed := false
	authorized := false
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tools.NewBaseTool("search", "查询", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		executed = true
		return &interfaces.ToolOutput{Success: true, Result: "结果"}, nil
	})))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{Authorize: func(context.Context, string, *interfaces.ToolInput) error { authorized = true; return denied }})
	require.NoError(t, err)
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("1", "search", "{}")}}})
	agent, err := NewSimpleBuilder(model).WithDispatcher(d).WithMiddleware(middleware.NewToolSelectorMiddleware([]string{"search"}, 1)).WithMaxIterations(2).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "search")
	require.ErrorIs(t, err, denied)
	require.True(t, authorized)
	require.False(t, executed)
}
