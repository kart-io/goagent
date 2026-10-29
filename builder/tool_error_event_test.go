package builder

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// collectErrorEvent 读完事件流并返回唯一的 error 事件。
func collectErrorEvent(t *testing.T, events <-chan AgentEvent) AgentEvent {
	t.Helper()
	var failures []AgentEvent
	for e := range events {
		require.NotEqual(t, "done", e.Type)
		if e.Type == "error" {
			failures = append(failures, e)
		}
	}
	require.Len(t, failures, 1)
	return failures[0]
}

// 工具执行失败时，终态 error 事件带上失败的工具名。
func TestStreamToolFailureEventCarriesTool(t *testing.T) {
	t.Run("工具选择路径", func(t *testing.T) {
		failing := tools.NewBaseTool("lookup", "lookup", argSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return nil, errors.New("执行失败")
		})
		model := &eventModel{MockLLMClient: NewMockLLMClient()}
		a, err := NewSimpleBuilder(model).WithTools(failing).Build()
		require.NoError(t, err)
		events, err := a.ExecuteStream(context.Background(), "q")
		require.NoError(t, err)
		e := collectErrorEvent(t, events)
		require.ErrorContains(t, e.Error, "执行失败")
		require.Equal(t, "lookup", e.Tool)
	})
	for _, tc := range []struct {
		name  string
		limit int
	}{{"原生顺序执行", 1}, {"原生并发执行", 4}} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &concurrencyProbe{}
			model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: probeCalls("1", "bad", "2")}})
			a, err := NewSimpleBuilder(model).WithTools(probeTools(t, probe, 4)...).WithParallelToolCalls(tc.limit).WithMaxIterations(3).Build()
			require.NoError(t, err)
			events, err := a.ExecuteStream(context.Background(), "q")
			require.NoError(t, err)
			e := collectErrorEvent(t, events)
			require.ErrorContains(t, e.Error, "执行失败")
			require.Equal(t, "probe", e.Tool)
		})
	}
	t.Run("非工具失败不带工具名", func(t *testing.T) {
		model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "partial"}}}
		a, err := NewSimpleBuilder(model).WithTools(NewMockTool("lookup", "ok")).Build()
		require.NoError(t, err)
		events, err := a.ExecuteStream(context.Background(), "q")
		require.NoError(t, err)
		require.Empty(t, collectErrorEvent(t, events).Tool)
	})
}
