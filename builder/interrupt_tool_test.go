package builder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func confirmRefundTool(t *testing.T, calls *int) interfaces.Tool {
	t.Helper()
	tool, err := tools.NewFunctionToolBuilder("confirm_refund").WithDescription("向用户确认退款").
		WithArgsSchema(`{"type":"object","properties":{}}`).WithInterruptible("v1").
		WithFunction(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
			*calls++
			raw, err := durable.AwaitInput(ctx, "confirm", json.RawMessage(`{"question":"确认退款？"}`), nil)
			if err != nil {
				return nil, err
			}
			return string(raw), nil
		}).Build()
	require.NoError(t, err)
	return tool
}

func interruptAgentContext(t *testing.T, root string) context.Context {
	t.Helper()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), root), store)
	controller, err := budget.NewDurable(budget.Config{RootKey: root, Version: "v1", MaxModelRequests: 3}, store)
	require.NoError(t, err)
	return budget.WithController(ctx, controller)
}

func interruptModel() *nativeEventModel {
	return nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "confirm_refund", `{}`)}, Usage: &llm.Usage{TotalTokens: 3}}},
		answerTurn("已确认退款", 4),
	)
}

func interruptKeyFrom(t *testing.T, err error) string {
	t.Helper()
	require.ErrorIs(t, err, ErrWaiting)
	var waiting *WaitingError
	require.True(t, errors.As(err, &waiting))
	require.Len(t, waiting.Waits, 1)
	var c durable.InterruptContinuation
	require.NoError(t, json.Unmarshal(waiting.Waits[0].Continuation, &c))
	require.Equal(t, "confirm", c.ID)
	path := waiting.Waits[0].Path
	return path[len(path)-1].Key
}

func TestInterruptibleToolPausesAgentAndResumesWithInvoke(t *testing.T) {
	ctx := interruptAgentContext(t, "ticket-int-1")
	calls := 0
	model := interruptModel()
	agent, err := NewSimpleBuilder(model).WithDurableScope("support").
		WithTools(dispatcherFor(t, confirmRefundTool(t, &calls)).Tools()...).WithMaxIterations(2).Build()
	require.NoError(t, err)

	_, err = agent.AsRunnable().Invoke(ctx, "退款 A1")
	key := interruptKeyFrom(t, err)
	require.Len(t, model.requests, 1)

	out, err := agent.AsRunnable().Invoke(durable.WithResume(ctx, key, `"yes"`), "退款 A1")
	require.NoError(t, err)
	require.Equal(t, "已确认退款", out.Result)
	require.Len(t, model.requests, 2)
	require.Equal(t, 2, calls)
}

func TestInterruptibleToolResumesWithStart(t *testing.T) {
	ctx := interruptAgentContext(t, "ticket-int-2")
	calls := 0
	model := interruptModel()
	agent, err := NewSimpleBuilder(model).WithDurableScope("support").
		WithTools(dispatcherFor(t, confirmRefundTool(t, &calls)).Tools()...).WithMaxIterations(2).Build()
	require.NoError(t, err)
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = agent.Start(ctx, "退款 A1").Wait(waitCtx)
	key := interruptKeyFrom(t, err)
	out, err := agent.Start(durable.WithResume(ctx, key, `"yes"`), "退款 A1").Wait(waitCtx)
	require.NoError(t, err)
	require.Equal(t, "已确认退款", out.Result)
}

func TestParallelToolCallsOneInterruptsOthersComplete(t *testing.T) {
	ctx := interruptAgentContext(t, "ticket-int-3")
	calls, lookups := 0, 0
	lookup, err := tools.NewFunctionToolBuilder("lookup").WithArgsSchema(`{"type":"object","properties":{}}`).
		WithFunction(func(context.Context, map[string]interface{}) (interface{}, error) {
			lookups++
			return "订单存在", nil
		}).Build()
	require.NoError(t, err)
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{
			nativeCall("c1", "confirm_refund", `{}`), nativeCall("c2", "lookup", `{}`)}, Usage: &llm.Usage{TotalTokens: 3}}},
		answerTurn("完成", 4),
	)
	agent, err := NewSimpleBuilder(model).WithDurableScope("support").WithParallelToolCalls(2).
		WithTools(dispatcherFor(t, confirmRefundTool(t, &calls), lookup).Tools()...).WithMaxIterations(2).Build()
	require.NoError(t, err)

	_, err = agent.AsRunnable().Invoke(ctx, "退款 A1")
	key := interruptKeyFrom(t, err)
	require.Equal(t, 1, lookups)

	out, err := agent.AsRunnable().Invoke(durable.WithResume(ctx, key, `"yes"`), "退款 A1")
	require.NoError(t, err)
	require.Equal(t, "完成", out.Result)
	require.Equal(t, 1, lookups, "已完成的兄弟调用只回放")
}
