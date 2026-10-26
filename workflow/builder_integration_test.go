package workflow_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

type answerState struct {
	Question  string
	Answer    string
	Validated bool
}

func TestBuilderAgentRunsInsideTypedWorkflow(t *testing.T) {
	agent, err := builder.NewSimpleBuilder(llm.NewMockStreamClient()).Build()
	require.NoError(t, err)
	node, err := workflow.Adapt(agent.AsRunnable(), func(state answerState) (any, error) { return state.Question, nil }, func(state answerState, out *builder.AgentOutput) (answerState, error) {
		state.Answer = out.Result.(string)
		return state, nil
	})
	require.NoError(t, err)
	graph := workflow.NewGraph[answerState]()
	require.NoError(t, graph.AddRunnable("answer", node))
	require.NoError(t, graph.AddNode("validate", func(_ context.Context, state answerState) (answerState, error) {
		state.Validated = state.Answer != ""
		return state, nil
	}))
	require.NoError(t, graph.AddEdge(workflow.START, "answer"))
	require.NoError(t, graph.AddEdge("answer", "validate"))
	require.NoError(t, graph.AddEdge("validate", workflow.END))
	run, err := graph.Compile(context.Background())
	require.NoError(t, err)
	events, err := run.Stream(context.Background(), answerState{Question: "question"})
	require.NoError(t, err)
	var text strings.Builder
	var final answerState
	done := 0
	for chunk := range events {
		require.NoError(t, chunk.Error)
		if chunk.Event != nil {
			text.WriteString(chunk.Event.Delta)
		}
		if chunk.Done {
			done++
			final = chunk.Data
		}
	}
	require.Equal(t, 1, done)
	require.Equal(t, "question", final.Question)
	require.True(t, final.Validated)
	require.Equal(t, "This is a mock streaming response from the LLM.", final.Answer)
	require.Equal(t, final.Answer, text.String())
}
