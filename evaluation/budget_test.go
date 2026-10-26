package evaluation

import (
	"context"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
	"testing"
)

type budgetMock struct {
	llm.Client
	requests []*llm.CompletionRequest
}

func (m *budgetMock) Complete(ctx context.Context, r *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.requests = append(m.requests, r)
	return &llm.CompletionResponse{Usage: &interfaces.TokenUsage{PromptTokens: 8, CompletionTokens: 2, TotalTokens: 10}}, nil
}
func TestBudgetStopsCallsAndMeasuresActualUsage(t *testing.T) {
	m := &budgetMock{}
	c := &BudgetClient{Client: m, Budget: Budget{Model: "same-model", MaxCalls: 2, MaxOutputTokens: 50, ObservedTokens: 10}}
	_, err := c.Chat(context.Background(), nil)
	require.NoError(t, err)
	_, err = c.Chat(context.Background(), nil)
	require.ErrorIs(t, err, ErrBudgetExceeded)
	require.Len(t, m.requests, 1)
	require.Equal(t, 50, m.requests[0].MaxTokens)
	require.Equal(t, "same-model", m.requests[0].Model)
	require.Equal(t, 10, c.Measurements().Usage.TotalTokens)
	require.Equal(t, 0.000012, EstimatedCost(c.Measurements().Usage, 1, 2))
}
