package evaluation

import (
	"context"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

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

// CompleteStream 在整条流结束前持有锁，按终止块的用量计账；调用方放弃读取时必须取消 ctx。
func (c *BudgetClient) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if req == nil || c.Client == nil || c.Budget.MaxCalls <= 0 || c.Budget.MaxOutputTokens <= 0 {
		c.mu.Unlock()
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "client, request and positive call/output budgets required").WithComponent("evaluation").WithOperation("complete_stream")
	}
	if c.metrics.Calls >= c.Budget.MaxCalls || (c.Budget.ObservedTokens > 0 && c.metrics.Usage.TotalTokens >= c.Budget.ObservedTokens) {
		c.mu.Unlock()
		return nil, agentErrors.ErrEvaluationBudgetExceeded
	}
	request := *req
	request.Model = c.Budget.Model
	request.Temperature = c.Budget.Temperature
	request.MaxTokens = c.Budget.MaxOutputTokens
	c.metrics.Calls++
	upstream, err := c.Client.CompleteStream(ctx, &request)
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	out := make(chan *llm.StreamChunk)
	go func() {
		defer c.mu.Unlock()
		defer close(out)
		for chunk := range upstream {
			if chunk != nil && chunk.Done && chunk.Usage != nil {
				tokens := chunk.Usage.Tokens()
				c.metrics.Usage.Add(&tokens)
				c.metrics.UsageReports++
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
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
