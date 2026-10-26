package react_test

import (
	"context"
	"github.com/kart-io/goagent/agents/react"
	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFormatRepairUsesStepBudget(t *testing.T) {
	for _, tt := range []struct {
		steps, retries, calls int
		success               bool
	}{{3, 1, 2, true}, {1, 1, 1, false}, {3, 0, 1, false}} {
		client := NewMockLLMClient([]string{"Malformed response", "ANSWER: 9"})
		agent := react.NewReActAgent(react.ReActConfig{LLM: client, MaxSteps: tt.steps, MaxParseRetries: tt.retries})
		out, err := agent.Invoke(context.Background(), &core.AgentInput{Task: "q"})
		if tt.success {
			require.NoError(t, err)
			require.Equal(t, "9", out.Result)
			require.Equal(t, 1, out.Metadata["parse_retries"])
		} else {
			require.Error(t, err)
		}
		require.Equal(t, tt.calls, client.callCount)
		require.Empty(t, out.ToolCalls)
	}
}
func TestFormatRepairExhaustionStops(t *testing.T) {
	client := NewMockLLMClient([]string{"invalid", "still invalid", "ANSWER: 9"})
	agent := react.NewReActAgent(react.ReActConfig{LLM: client, MaxSteps: 10, MaxParseRetries: 1})
	out, err := agent.InvokeFast(context.Background(), &core.AgentInput{Task: "q"})
	require.Error(t, err)
	require.Equal(t, "failed", out.Status)
	require.Equal(t, 2, client.callCount)
}
