package tot

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/evaluation"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

type contractModel struct {
	llm.Client
	responses []string
	calls     int
	failAt    int
	err       error
}

func (m *contractModel) Chat(ctx context.Context, msg []llm.Message) (*llm.CompletionResponse, error) {
	m.calls++
	if m.calls == m.failAt {
		return nil, m.err
	}
	if m.calls > len(m.responses) {
		return nil, errors.New("unexpected extra model call")
	}
	return &llm.CompletionResponse{Content: m.responses[m.calls-1], Usage: &interfaces.TokenUsage{TotalTokens: 10}}, nil
}
func (m *contractModel) Complete(ctx context.Context, r *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return m.Chat(ctx, r.Messages)
}
func runContract(t *testing.T, entry string, model llm.Client) (*core.AgentOutput, error) {
	t.Helper()
	a := NewToTAgent(ToTConfig{LLM: model, MaxDepth: 1, BranchingFactor: 1, BeamWidth: 1})
	input := &core.AgentInput{Task: "Compute 4+5. End with ANSWER: 9"}
	switch entry {
	case "invoke":
		return a.Invoke(context.Background(), input)
	case "fast":
		return a.InvokeFast(context.Background(), input)
	case "stream":
		stream, err := a.Stream(context.Background(), input)
		if err != nil {
			return nil, err
		}
		chunk := <-stream
		return chunk.Data, chunk.Error
	default:
		var final *core.AgentOutput
		var runErr error
		for out, err := range a.RunGenerator(context.Background(), input) {
			final = out
			if err != nil {
				runErr = err
			}
		}
		return final, runErr
	}
}
func TestToTFinalAnswerContractAcrossEntrypoints(t *testing.T) {
	for _, entry := range []string{"invoke", "fast", "generator", "stream"} {
		t.Run(entry, func(t *testing.T) {
			for _, tt := range []struct {
				answer, status string
				wantErr        bool
			}{
				{`{"solved":true,"answer":"ANSWER: 9"}`, "success", false},
				{`{"solved":false,"answer":""}`, "partial", false},
				{`{"solved":true,"answer":""}`, "failed", true},
				{`{}`, "failed", true},
			} {
				model := &contractModel{responses: []string{"Step 1: Adding four and five gives nine.", "0.9", tt.answer}}
				out, err := runContract(t, entry, model)
				if tt.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, tt.status, out.Status)
				require.Equal(t, 3, model.calls)
				require.Equal(t, 30, out.TokenUsage.TotalTokens)
				if tt.status == "success" {
					require.Equal(t, "ANSWER: 9", out.Result)
				} else {
					require.Nil(t, out.Result)
				}
			}
		})
	}
}
func TestToTPropagatesGenerationScoringAndFinalErrors(t *testing.T) {
	for _, entry := range []string{"invoke", "fast", "generator", "stream"} {
		for failAt := 1; failAt <= 3; failAt++ {
			sentinel := errors.New("upstream failed")
			model := &contractModel{responses: []string{"Step 1: Complete calculation", "0.9", `{"solved":true,"answer":"9"}`}, failAt: failAt, err: sentinel}
			out, err := runContract(t, entry, model)
			require.ErrorIs(t, err, sentinel)
			require.Equal(t, "failed", out.Status)
			require.Equal(t, failAt, model.calls)
		}
	}
}
func TestToTBudgetStopsBeforeFinalGeneration(t *testing.T) {
	model := &contractModel{responses: []string{"Step 1: Complete calculation", "0.9"}}
	budget := &evaluation.BudgetClient{Client: model, Budget: evaluation.Budget{MaxCalls: 2, MaxOutputTokens: 100}}
	out, err := runContract(t, "invoke", budget)
	require.ErrorIs(t, err, evaluation.ErrBudgetExceeded)
	require.Equal(t, "failed", out.Status)
	require.Equal(t, 2, model.calls)
	require.Equal(t, 20, out.TokenUsage.TotalTokens)
}
func TestToTRejectsMalformedScore(t *testing.T) {
	for _, score := range []string{"NaN", "Inf", "2", "0.9 because", "yes"} {
		model := &contractModel{responses: []string{"Step 1: Complete calculation", score}}
		out, err := runContract(t, "invoke", model)
		require.Error(t, err)
		require.Equal(t, "failed", out.Status)
		require.Equal(t, 2, model.calls)
	}
}

func TestAllSearchStrategiesPropagateSolutionCheckError(t *testing.T) {
	for _, strategy := range []interfaces.ReasoningStrategy{interfaces.StrategyBeamSearch, interfaces.StrategyBreadthFirst, interfaces.StrategyDepthFirst, interfaces.StrategyMonteCarlo} {
		sentinel := errors.New("solution check unavailable")
		model := &contractModel{responses: []string{"Step 1: Complete calculation", "0.9"}, failAt: 3, err: sentinel}
		a := NewToTAgent(ToTConfig{LLM: model, SearchStrategy: strategy, MaxDepth: 2, BranchingFactor: 1, BeamWidth: 1})
		out, err := a.Invoke(context.Background(), &core.AgentInput{Task: "q"})
		require.ErrorIs(t, err, sentinel, string(strategy))
		require.Equal(t, "failed", out.Status)
		require.Equal(t, 3, model.calls)
	}
}
func TestToTCancellationAndEarlyConsumerStop(t *testing.T) {
	model := &contractModel{}
	a := NewToTAgent(ToTConfig{LLM: model})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Invoke(ctx, &core.AgentInput{})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, model.calls)
	for range a.RunGenerator(context.Background(), &core.AgentInput{Task: "q"}) {
		break
	}
	require.Zero(t, model.calls, "stopping before expansion must not call the model")
}
