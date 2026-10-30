package builder

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/stretchr/testify/require"
)

func TestLegacyEntriesRejectBoundRootBudgetBeforeRequest(t *testing.T) {
	for _, entry := range []string{"Execute", "ExecuteWithTools"} {
		t.Run(entry, func(t *testing.T) {
			model := NewMockLLMClient()
			agent, err := NewSimpleBuilder(model).Build()
			require.NoError(t, err)
			controller, err := budget.New(budget.Config{RootKey: "legacy", Version: "v1", MaxModelRequests: 1})
			require.NoError(t, err)
			ctx := budget.WithController(context.Background(), controller)
			for range 2 {
				if entry == "Execute" {
					_, err = agent.Execute(ctx, "question")
				} else {
					_, err = agent.ExecuteWithTools(ctx, "question")
				}
				require.ErrorContains(t, err, "root budget")
			}
			require.Zero(t, model.index)
			snapshot, err := controller.Snapshot(ctx)
			require.NoError(t, err)
			require.Zero(t, snapshot.ReservedRequests)
			// The old, unconfigured synchronous entry is still supported.
			_, err = agent.Execute(context.Background(), "question")
			require.NoError(t, err)
			require.Equal(t, 1, model.index)
		})
	}
}

func TestIdentityPolicyPreservesOpaqueProviderBytes(t *testing.T) {
	policy := &testPolicy{change: func(turn contextpolicy.Turn) contextpolicy.Turn { return turn }}
	runtime := &policyRuntime{policies: []contextpolicy.Policy{policy}, descriptors: []contextpolicy.Descriptor{policy.Descriptor()}}
	input := contextpolicy.Turn{Messages: []llm.Message{{Role: "assistant", Content: "signed text", ToolCalls: []llm.ToolCall{nativeCall("id", "lookup", "{}")}, ProviderState: &llm.ProviderState{Provider: constants.ProviderGemini, Data: json.RawMessage(`{ "signature": "opaque", "escaped": "\u0061" }`)}}}}
	output, err := runtime.apply(context.Background(), input, false)
	require.NoError(t, err)
	require.Equal(t, input, output)
	require.NotSame(t, input.Messages[0].ProviderState, output.Messages[0].ProviderState)
	output.Messages[0].ProviderState.Data[0] = 'X'
	output.Messages[0].ToolCalls[0].ID = "changed"
	require.Equal(t, byte('{'), input.Messages[0].ProviderState.Data[0])
	require.Equal(t, "id", input.Messages[0].ToolCalls[0].ID)
}

func TestPolicyRejectsBoundAssistantContentBeforeAdmission(t *testing.T) {
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", Content: "signed text", ToolCalls: []llm.ToolCall{nativeCall("id", "lookup", "{}")}, ProviderState: &llm.ProviderState{Provider: constants.ProviderGemini, Data: json.RawMessage(`{"signature":"opaque"}`)}}}, []*llm.StreamChunk{{Done: true, FinishReason: "stop", Content: "answer"}})
	agent, err := NewSimpleBuilder(model).WithTools(NewMockTool("lookup", "evidence")).WithMaxIterations(3).WithContextPolicies(func() contextpolicy.Policy {
		return &testPolicy{change: func(turn contextpolicy.Turn) contextpolicy.Turn {
			for i := range turn.Messages {
				if turn.Messages[i].Role == "assistant" {
					turn.Messages[i].Content = "changed"
				}
			}
			return turn
		}}
	}).Build()
	require.NoError(t, err)
	controller, err := budget.New(budget.Config{RootKey: "signed", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, err)
	ctx := budget.WithController(context.Background(), controller)
	_, err = agent.AsRunnable().Invoke(ctx, "question")
	require.ErrorContains(t, err, "protected message protocol")
	require.Len(t, model.requests, 1)
	snapshot, err := controller.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, snapshot.ReservedRequests)
}
