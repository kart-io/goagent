package builder

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/stretchr/testify/require"
)

func TestDirectToolSavesFinalRequestAndConversation(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "默认两次", true: "直接一次"}[direct], func(t *testing.T) {
			ctx := context.Background()
			mem := memory.NewInMemoryManager(nil)
			model := nativeModel(
				[]*llm.StreamChunk{{Delta: "正在查询"}, {Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("id", "status", "{}")}, Usage: &llm.Usage{TotalTokens: 10}}},
				[]*llm.StreamChunk{{Delta: "已发货"}, {Done: true, Usage: &llm.Usage{TotalTokens: 20}}},
			)
			tool := &countingEventTool{MockTool: NewMockTool("status", "已发货")}
			b := NewSimpleBuilder(model).WithTools(tool).WithMemory(mem).WithSessionID("direct").WithMaxIterations(2)
			if direct {
				b.WithReturnDirectTools("status").WithMaxIterations(1)
			}
			agent, err := b.Build()
			require.NoError(t, err)
			agent.chain.Use(endingMiddleware{middleware.NewBaseMiddleware("direct-completion")})
			out, err := agent.AsRunnable().Invoke(ctx, "订单状态")
			require.NoError(t, err)
			require.Equal(t, "已发货", out.Result)
			require.Equal(t, 1, tool.calls)
			require.Equal(t, true, out.Metadata["usage_complete"])
			require.Equal(t, true, out.Metadata["after"])
			if direct {
				require.Len(t, model.requests, 1)
				require.Equal(t, 10, out.TokenUsage.TotalTokens)
				require.Equal(t, "tool", out.Metadata["answer_source"])
				require.Equal(t, "status", out.Metadata["return_direct_tool"])
				require.Equal(t, false, out.Metadata["answer_usage_reported"])
			} else {
				require.Len(t, model.requests, 2)
				require.Equal(t, 30, out.TokenUsage.TotalTokens)
			}
			history, err := mem.GetConversationHistory(ctx, "direct", 10)
			require.NoError(t, err)
			require.Len(t, history, 2)
			require.Equal(t, "已发货", history[1].Content)
			last, ok := agent.GetState().Get("last_response")
			require.True(t, ok)
			require.Equal(t, "已发货", last)
		})
	}
}

type directResultTool struct {
	*MockTool
	output *interfaces.ToolOutput
	err    error
	cancel context.CancelFunc
}

func (t *directResultTool) Invoke(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if t.cancel != nil {
		t.cancel()
	}
	return t.output, t.err
}

func TestDirectToolResultValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output *interfaces.ToolOutput
		err    error
		cancel bool
		want   string
	}{
		{name: "结构化", output: &interfaces.ToolOutput{Success: true, Result: map[string]any{"days": 30}}, want: `{"days":30}`},
		{name: "失败状态", output: &interfaces.ToolOutput{Result: "失败"}},
		{name: "工具错误", err: errors.New("执行失败")},
		{name: "错误字段", output: &interfaces.ToolOutput{Success: true, Result: "无效", Error: "失败"}},
		{name: "空输出"},
		{name: "空结果", output: &interfaces.ToolOutput{Success: true}},
		{name: "类型化空值", output: &interfaces.ToolOutput{Success: true, Result: map[string]any(nil)}},
		{name: "空文本", output: &interfaces.ToolOutput{Success: true, Result: "  "}},
		{name: "编码失败", output: &interfaces.ToolOutput{Success: true, Result: make(chan int)}},
		{name: "取消", output: &interfaces.ToolOutput{Success: true, Result: "无效"}, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			model := nativeModel([]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("id", "status", "{}")}}})
			tool := &directResultTool{MockTool: NewMockTool("status", ""), output: tc.output, err: tc.err}
			if tc.cancel {
				tool.cancel = cancel
			}
			agent, err := NewSimpleBuilder(model).WithTools(tool).WithReturnDirectTools("status").WithMaxIterations(1).Build()
			require.NoError(t, err)
			out, err := agent.AsRunnable().Invoke(ctx, "状态")
			if tc.want == "" {
				require.Error(t, err)
				require.Nil(t, out)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, out.Result)
				require.Equal(t, false, out.Metadata["usage_complete"])
			}
			require.Len(t, model.requests, 1)
		})
	}
}

func TestDirectToolMultipleCallsRequireModelSynthesis(t *testing.T) {
	for _, budget := range []int{1, 2} {
		model := nativeModel([]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("a", "status", "{}"), nativeCall("b", "status", "{}")}}}, []*llm.StreamChunk{{Delta: "合并答案"}, {Done: true}})
		tool := &countingEventTool{MockTool: NewMockTool("status", "单个结果")}
		agent, err := NewSimpleBuilder(model).WithTools(tool).WithReturnDirectTools("status").WithMaxIterations(budget).Build()
		require.NoError(t, err)
		out, err := agent.AsRunnable().Invoke(context.Background(), "汇总")
		if budget == 1 {
			require.Error(t, err)
			require.Zero(t, tool.calls)
		} else {
			require.NoError(t, err)
			require.Equal(t, "合并答案", out.Result)
			require.Equal(t, 2, tool.calls)
			require.Len(t, model.requests, 2)
		}
	}
}

func TestDirectToolsConfiguration(t *testing.T) {
	tool := NewMockTool("status", "结果")
	_, err := NewSimpleBuilder(&eventModel{MockLLMClient: NewMockLLMClient()}).WithTools(tool).WithReturnDirectTools("status").Build()
	require.Error(t, err)
	for _, name := range []string{"", "unknown"} {
		_, err = NewSimpleBuilder(nativeModel()).WithTools(tool).WithReturnDirectTools(name).Build()
		require.Error(t, err)
	}
	_, err = NewSimpleBuilder(nativeModel()).WithTools(tool, tool).WithReturnDirectTools("status").Build()
	require.Error(t, err)
}

func TestDirectToolRejectsInvalidCallBeforeExecution(t *testing.T) {
	for _, call := range []llm.ToolCall{nativeCall("", "status", "{}"), nativeCall("id", "status", "null"), nativeCall("id", "status", `{"id":`)} {
		model := nativeModel([]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{call}}})
		tool := &countingEventTool{MockTool: NewMockTool("status", "结果")}
		agent, err := NewSimpleBuilder(model).WithTools(tool).WithReturnDirectTools("status").WithMaxIterations(1).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(context.Background(), "状态")
		require.Error(t, err)
		require.Zero(t, tool.calls)
	}
}

func TestNonDirectToolContinuesModelLoop(t *testing.T) {
	model := nativeModel([]*llm.StreamChunk{{Done: true, ToolCalls: []llm.ToolCall{nativeCall("id", "evidence", "{}")}}}, []*llm.StreamChunk{{Delta: "根据证据的答案"}, {Done: true}})
	agent, err := NewSimpleBuilder(model).WithTools(NewMockTool("status", "状态"), NewMockTool("evidence", "证据")).WithReturnDirectTools("status").WithMaxIterations(2).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "推理")
	require.NoError(t, err)
	require.Equal(t, "根据证据的答案", out.Result)
	require.Len(t, model.requests, 2)
	require.NotContains(t, out.Metadata, "return_direct_tool")
}
