package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

func journalContextForTest(t *testing.T) context.Context {
	t.Helper()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	return durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
}

func TestDurableAgentRequiresExplicitScope(t *testing.T) {
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "stop", Content: "answer"}})
	agent, err := NewSimpleBuilder(model).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(journalContextForTest(t), "question")
	require.ErrorContains(t, err, "DurableScope")
	require.Empty(t, model.requests)
}

func TestDurableTextOnlyReplaysWithoutModelRequest(t *testing.T) {
	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "answer"}, {Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 11}}}}
	agent, err := NewSimpleBuilder(model).WithDurableScope("answer").Build()
	require.NoError(t, err)
	ctx := journalContextForTest(t)
	first, err := agent.AsRunnable().Invoke(ctx, "question")
	require.NoError(t, err)
	// An upstream that can no longer answer proves recovery must use the journal.
	model.chunks = nil
	second, err := agent.AsRunnable().Invoke(ctx, "question")
	require.NoError(t, err)
	require.Equal(t, first.Result, second.Result)
	require.Equal(t, 11, second.TokenUsage.TotalTokens)
}

func TestDurableLegacyToolsRejectedBeforeModel(t *testing.T) {
	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "answer"}, {Done: true, FinishReason: "stop"}}}
	agent, err := NewSimpleBuilder(model).WithDurableScope("legacy").WithTools(NewMockTool("lookup", "value")).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(journalContextForTest(t), "question")
	require.ErrorContains(t, err, "ToolStreamClient")
	require.Zero(t, model.selections)
	require.Nil(t, model.request)
}

func TestDurableScopesIsolateAgentResultsToolsAndRecovery(t *testing.T) {
	ctx := context.Background()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	effectKeys := map[string]bool{}
	crash := errors.New("after both agents, before node checkpoint")
	for _, resuming := range []bool{false, true} {
		controller, err := budget.NewDurable(budget.Config{RootKey: "multi-agent", Version: "v1", MaxModelRequests: 2}, store)
		require.NoError(t, err)
		ctx = budget.WithController(ctx, controller)
		calls := 0
		models := []*nativeEventModel{}
		agents := []*ConfigurableAgent[any, *core.AgentState]{}
		for _, label := range []string{"A", "B"} {
			reg := tools.NewRegistry()
			require.NoError(t, reg.Register(tools.NewBaseTool("write", "write", `{"type":"object"}`, func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				calls++
				require.False(t, resuming, "completed tool should not execute after restart")
				require.Equal(t, durable.ExecutionKey(ctx), in.ExecutionKey)
				effectKeys[in.ExecutionKey] = true
				return &interfaces.ToolOutput{Success: true, Result: label}, nil
			})))
			dispatch, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
			require.NoError(t, err)
			model := nativeModel()
			if !resuming {
				model = nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("same-call-id", "write", `{}`)}, Usage: &llm.Usage{TotalTokens: 5}}})
			}
			agent, err := NewSimpleBuilder(model).WithDurableScope(label).WithTools(dispatch.Tools()...).WithReturnDirectTools("write").WithMaxIterations(1).Build()
			require.NoError(t, err)
			models = append(models, model)
			agents = append(agents, agent)
		}
		graph := workflow.NewGraph[string]()
		require.NoError(t, graph.AddNode("both", func(ctx context.Context, in string) (string, error) {
			a, err := agents[0].AsRunnable().Invoke(ctx, in)
			if err != nil {
				return "", err
			}
			b, err := agents[1].AsRunnable().Invoke(ctx, in)
			if err != nil {
				return "", err
			}
			require.Equal(t, "A", a.Result)
			require.Equal(t, "B", b.Result)
			if !resuming {
				return "", crash
			}
			return fmt.Sprint(a.Result) + fmt.Sprint(b.Result), nil
		}))
		require.NoError(t, graph.AddEdge(workflow.START, "both"))
		require.NoError(t, graph.AddEdge("both", workflow.END))
		run, err := graph.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1"})
		require.NoError(t, err)
		snapshot, err := run.Run(ctx, "run", "same question", "")
		ledger, ledgerErr := controller.Snapshot(ctx)
		require.NoError(t, ledgerErr)
		require.Equal(t, 2, ledger.ReservedRequests, "scope isolation and replay must preserve the shared budget")
		require.Equal(t, 10, ledger.KnownTokens.TotalTokens, "replay must not count usage twice")
		if resuming {
			require.NoError(t, err)
			require.True(t, snapshot.Done)
			require.Equal(t, "AB", snapshot.State)
			require.Zero(t, calls)
			require.Empty(t, models[0].requests)
			require.Empty(t, models[1].requests)
		} else {
			require.ErrorIs(t, err, crash)
			require.Equal(t, 2, calls)
			require.Len(t, models[0].requests, 1)
			require.Len(t, models[1].requests, 1)
		}
	}
	require.Len(t, effectKeys, 2, "same tool name and call ID must still get separate effect keys")
}

func TestDurableTextFailureRetainsUsageAndStopsRetry(t *testing.T) {
	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Done: true, FinishReason: "length", Usage: &llm.Usage{TotalTokens: 17}}}}
	agent, err := NewSimpleBuilder(model).WithDurableScope("answer").Build()
	require.NoError(t, err)
	ctx := journalContextForTest(t)
	_, err = agent.AsRunnable().Invoke(ctx, "question")
	require.Error(t, err)
	require.NotNil(t, model.request)
	model.request = nil
	_, err = agent.AsRunnable().Invoke(ctx, "question")
	require.ErrorIs(t, err, durable.ErrOperationAttention)
	require.Nil(t, model.request, "no new request for persisted failure")
	scoped, err := agent.scopeDurableExecution(ctx)
	require.NoError(t, err)
	record, err := durable.InspectOperation(scoped, "model/answer/0")
	require.NoError(t, err)
	var saved llm.StreamChunk
	require.NoError(t, json.Unmarshal(record.Value, &saved))
	require.Equal(t, 17, saved.Usage.TotalTokens)
}

func TestDurableSynchronousEntrypointsRejectBeforeRequest(t *testing.T) {
	model := NewMockLLMClient()
	agent, err := NewSimpleBuilder(model).WithDurableScope("answer").Build()
	require.NoError(t, err)
	ctx := journalContextForTest(t)
	_, err = agent.Execute(ctx, "question")
	require.ErrorContains(t, err, "AsRunnable")
	_, err = agent.ExecuteWithTools(ctx, "question")
	require.ErrorContains(t, err, "AsRunnable")
}

func TestDurableScopeRejectsLegacyUnscopedCheckpoint(t *testing.T) {
	for _, id := range []string{"model/turn/0", "model/answer/0"} {
		t.Run(id, func(t *testing.T) {
			ctx := journalContextForTest(t)
			_, err := durable.Do(ctx, id, json.RawMessage(`{}`), durable.OperationPolicy{}, func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil })
			require.NoError(t, err)
			model := nativeModel()
			agent, err := NewSimpleBuilder(model).WithDurableScope("answer").Build()
			require.NoError(t, err)
			_, err = agent.AsRunnable().Invoke(ctx, "question")
			require.ErrorContains(t, err, "unscoped checkpoint")
			require.Empty(t, model.requests)
		})
	}
}

type blockedScopeStore struct{ durable.Store }

func (s blockedScopeStore) Lock(ctx context.Context, _ string) (func() error, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDurableScopeInspectionUsesAgentTimeout(t *testing.T) {
	model := nativeModel()
	agent, err := NewSimpleBuilder(model).WithDurableScope("answer").WithTimeout(20 * time.Millisecond).Build()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = durable.WithJournal(durable.WithExecutionKey(ctx, "node"), blockedScopeStore{})
	start := time.Now()
	_, err = agent.AsRunnable().Invoke(ctx, "question")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 500*time.Millisecond)
	require.Empty(t, model.requests)
}
