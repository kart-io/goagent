package builder

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func TestJournaledTurnRetainsFailedUsageAndDoesNotRetry(t *testing.T) {
	for _, mode := range []string{"length", "invalid_plan", "eof"} {
		t.Run(mode, func(t *testing.T) {
			store, err := durable.NewFileStore(t.TempDir())
			require.NoError(t, err)
			ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
			chunk := &llm.StreamChunk{Done: true, FinishReason: mode, Usage: &llm.Usage{TotalTokens: 19}}
			if mode == "invalid_plan" {
				chunk.FinishReason = "tool_calls"
				chunk.ToolCalls = []llm.ToolCall{nativeCall("x", "unknown", "{}")}
			}
			if mode == "eof" {
				chunk.Done = false
			}
			model := nativeModel([]*llm.StreamChunk{chunk})
			emit := func(AgentEvent) error { return nil }
			_, err = readJournaledTurn(ctx, 0, model, &llm.CompletionRequest{}, nil, emit)
			require.Error(t, err)
			_, err = readJournaledTurn(ctx, 0, model, &llm.CompletionRequest{}, nil, emit)
			require.ErrorIs(t, err, durable.ErrOperationAttention)
			require.Len(t, model.requests, 1)
			record, err := durable.InspectOperation(ctx, "model/turn/0")
			require.NoError(t, err)
			var saved llm.StreamChunk
			require.NoError(t, json.Unmarshal(record.Value, &saved))
			require.Equal(t, 19, saved.Usage.TotalTokens)
			require.Equal(t, "failed", record.Status)
		})
	}
}

func TestInvalidSecondToolPlanBlocksWholeRound(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
	calls := 0
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(tools.NewBaseTool("write", "write", `{"type":"object","properties":{"stage":{"type":"string"}},"required":["stage"],"additionalProperties":false}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		return &interfaces.ToolOutput{Success: true, Result: "receipt"}, nil
	})))
	dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{})
	require.NoError(t, err)
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("a", "write", `{"stage":"A"}`), nativeCall("b", "write", `{"key":"unexpected"}`)}, Usage: &llm.Usage{TotalTokens: 23}}})
	agent, err := NewSimpleBuilder(model).WithTools(dispatcher.Tools()...).WithMaxIterations(2).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "run")
	require.ErrorContains(t, err, "invalid arguments")
	require.Zero(t, calls, "first tool must not run when second plan arguments are invalid")
	_, err = agent.AsRunnable().Invoke(ctx, "run")
	require.ErrorIs(t, err, durable.ErrOperationAttention)
	require.Len(t, model.requests, 1)
}

func TestDurableLoopRejectsUnjournaledToolsBeforeModelRequest(t *testing.T) {
	model := nativeModel()
	agent, err := NewSimpleBuilder(model).WithTools(NewMockTool("write", "receipt")).Build()
	require.NoError(t, err)
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
	_, err = agent.AsRunnable().Invoke(ctx, "run")
	require.ErrorContains(t, err, "journaled adapter")
	require.Empty(t, model.requests)
}
