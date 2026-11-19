package builder

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/core/budget"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// TokenEstimator is the host's estimate of a request's total tokens, which must
// include input, tool definitions, and the output budget; its arguments are
// read-only. The estimation policy version is identified by the root budget's
// TokenPolicyVersion; the function is not written to the durable configuration.
type TokenEstimator func(context.Context, *llm.CompletionRequest, []interfaces.Tool) (int, error)

// WithTokenEstimator configures the host's estimate for native stream requests;
// it is called only when the root budget enables a token limit.
func (b *AgentBuilder[S]) WithTokenEstimator(estimate TokenEstimator) *AgentBuilder[S] {
	b.config.TokenEstimator = estimate
	return b
}

type (
	tokenEstimatorKey struct{}
	tokenRequestKey   struct{}
	tokenRequest      struct {
		request *llm.CompletionRequest
		tools   []interfaces.Tool
	}
)

func withTokenEstimator(ctx context.Context, estimator TokenEstimator) context.Context {
	return context.WithValue(ctx, tokenEstimatorKey{}, estimator)
}

func withTokenRequest(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) context.Context {
	return context.WithValue(ctx, tokenRequestKey{}, tokenRequest{req, tools})
}

func reserveModelBudget(ctx context.Context, controller *budget.Controller, key string) error {
	if controller.Configuration().MaxReservedTokens == 0 {
		return controller.ReserveEstimated(ctx, key, 0)
	}
	estimator, _ := ctx.Value(tokenEstimatorKey{}).(TokenEstimator)
	req, _ := ctx.Value(tokenRequestKey{}).(tokenRequest)
	if estimator == nil || req.request == nil {
		return agentErrors.ErrTokenEstimateRequired
	}
	estimate, err := estimator(ctx, req.request, req.tools)
	if err != nil {
		return fmt.Errorf("估计模型 Token: %w", err)
	}
	return controller.ReserveEstimated(ctx, key, estimate)
}
