package builder

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

const argSchema = `{"type":"object","properties":{"id":{"type":"string","minLength":1}},"required":["id"],"additionalProperties":false}`

func argumentTools(t *testing.T, cfg tools.DispatchConfig, calls map[string]int) []interfaces.Tool {
	t.Helper()
	reg := tools.NewRegistry()
	for _, name := range []string{"lookup", "quote"} {
		require.NoError(t, reg.Register(tools.NewBaseTool(name, name, argSchema, func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			calls[name]++
			return &interfaces.ToolOutput{Success: true, Result: name + ":" + in.Args["id"].(string)}, nil
		})))
	}
	d, err := tools.NewDispatcher(reg, cfg)
	require.NoError(t, err)
	return d.Tools()
}

// 同一轮一个调用参数不合法、一个合法：合法调用照常执行，错误作为该调用的结果交给模型改正，
// 整个过程写入日志，第二次执行完全回放。
func TestArgumentErrorReturnedToModelWithinTurn(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "arg-turn"), store)
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{
			nativeCall("a", "lookup", `{"id":""}`), nativeCall("b", "quote", `{"id":"q1"}`)}}},
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c", "lookup", `{"id":"k1"}`)}}},
		[]*llm.StreamChunk{{Delta: "answer"}, {Done: true, FinishReason: "stop"}},
	)
	calls := map[string]int{}
	business := argumentTools(t, tools.DispatchConfig{ReturnArgumentErrors: true}, calls)
	for range 2 {
		agent, err := NewSimpleBuilder(model).WithDurableScope("arg").WithTools(business...).WithMaxIterations(4).Build()
		require.NoError(t, err)
		out, err := agent.AsRunnable().Invoke(ctx, "question")
		require.NoError(t, err)
		require.Equal(t, "answer", out.Result)
	}
	require.Len(t, model.requests, 3, "第二次执行全部回放")
	require.Equal(t, map[string]int{"lookup": 1, "quote": 1}, calls)
	second := model.requests[1].Messages
	bad, good := second[len(second)-2], second[len(second)-1]
	require.Equal(t, "a", bad.ToolCallID)
	require.Contains(t, bad.Content, `"error":"invalid_arguments"`)
	require.Contains(t, bad.Content, `tool \"lookup\" arguments:`)
	require.Equal(t, "b", good.ToolCallID)
	require.Equal(t, "quote:q1", good.Content)
	third := model.requests[2].Messages
	require.Equal(t, "lookup:k1", third[len(third)-1].Content)
}

// 未开启时行为不变：参数错误中止执行，不再请求模型。
func TestArgumentErrorAbortsWhenDisabled(t *testing.T) {
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("a", "lookup", `{"id":""}`)}}})
	calls := map[string]int{}
	agent, err := NewSimpleBuilder(model).WithTools(argumentTools(t, tools.DispatchConfig{}, calls)...).WithMaxIterations(4).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "question")
	require.ErrorContains(t, err, `tool "lookup" arguments:`)
	require.Len(t, model.requests, 1)
	require.Empty(t, calls)
}

// 直接返回工具不能把参数错误当作最终答案。
func TestArgumentErrorNeverBecomesDirectAnswer(t *testing.T) {
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("a", "lookup", `{"id":""}`)}}})
	agent, err := NewSimpleBuilder(model).WithTools(argumentTools(t, tools.DispatchConfig{ReturnArgumentErrors: true}, map[string]int{})...).
		WithReturnDirectTools("lookup").WithMaxIterations(4).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "question")
	require.ErrorContains(t, err, "未成功完成")
	require.Len(t, model.requests, 1)
}
