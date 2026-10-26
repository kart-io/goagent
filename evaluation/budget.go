package evaluation

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

var ErrBudgetExceeded = errors.New("evaluation budget exhausted")

// Budget fixes the model, temperature, call ceiling and per-call output ceiling.
// ObservedTokens stops subsequent calls after the reported usage reaches it.
// Input tokenization and provider-hidden tokens are unknown before a request,
// so ObservedTokens is NOT a hard billing cap; the last call may overshoot it.
type Budget struct {
	Model           string  `json:"model"`
	Temperature     float64 `json:"temperature"`
	MaxCalls        int     `json:"max_calls"`
	MaxOutputTokens int     `json:"max_output_tokens_per_call"`
	ObservedTokens  int     `json:"observed_token_stop"`
}
type Measurements struct {
	Calls        int                   `json:"calls"`
	Usage        interfaces.TokenUsage `json:"usage"`
	UsageReports int                   `json:"usage_reports"`
}

// BudgetClient serializes calls to prevent simultaneous budget reservations from
// passing the same limit. Embedded Client exposes only the basic llm contract.
type BudgetClient struct {
	llm.Client
	Budget  Budget
	mu      sync.Mutex
	metrics Measurements
}

func (c *BudgetClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil || c.Client == nil || c.Budget.MaxCalls <= 0 || c.Budget.MaxOutputTokens <= 0 {
		return nil, fmt.Errorf("client, request and positive call/output budgets required")
	}
	if c.metrics.Calls >= c.Budget.MaxCalls || (c.Budget.ObservedTokens > 0 && c.metrics.Usage.TotalTokens >= c.Budget.ObservedTokens) {
		return nil, ErrBudgetExceeded
	}
	request := *req
	request.Model = c.Budget.Model
	request.Temperature = c.Budget.Temperature
	request.MaxTokens = c.Budget.MaxOutputTokens
	c.metrics.Calls++
	response, err := c.Client.Complete(ctx, &request)
	if response != nil {
		if response.Usage != nil {
			c.metrics.Usage.Add(response.Usage)
			c.metrics.UsageReports++
		} else {
			c.metrics.Usage.TotalTokens += response.TokensUsed
		}
	}
	return response, err
}
func (c *BudgetClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return c.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}
func (c *BudgetClient) Measurements() Measurements {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.metrics
}

// EstimatedCost uses caller-supplied prices per million tokens. No provider
// price assumptions are embedded. This simple estimate does not discount cache.
func EstimatedCost(usage interfaces.TokenUsage, inputPrice, outputPrice float64) float64 {
	return (float64(usage.PromptTokens)*inputPrice + float64(usage.CompletionTokens)*outputPrice) / 1e6
}
