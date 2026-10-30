package builder

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/core/budget"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// TokenEstimator 由宿主估计一次请求的总 Token，须包含输入、工具定义和输出预算，参数只读。
// 估计策略版本由根预算 TokenPolicyVersion 标识，不把函数写入持久配置。
type TokenEstimator func(context.Context, *llm.CompletionRequest, []interfaces.Tool) (int, error)

// WithTokenEstimator 配置原生流请求的宿主预估；仅在根预算开启 Token 上限时调用。
func (b *AgentBuilder[C, S]) WithTokenEstimator(estimate TokenEstimator) *AgentBuilder[C, S] {
	b.config.TokenEstimator = estimate
	return b
}

type tokenEstimatorKey struct{}
type tokenRequestKey struct{}
type tokenRequest struct {
	request *llm.CompletionRequest
	tools   []interfaces.Tool
}

func withTokenEstimator(ctx context.Context, estimator TokenEstimator) context.Context {
	return context.WithValue(ctx, tokenEstimatorKey{}, estimator)
}

func withTokenRequest(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) context.Context {
	return context.WithValue(ctx, tokenRequestKey{}, tokenRequest{req, tools})
}

func reserveModelBudget(ctx context.Context, controller *budget.Controller, key string) error {
	if controller.Configuration().MaxReservedTokens == 0 {
		return controller.Reserve(ctx, key)
	}
	estimator, _ := ctx.Value(tokenEstimatorKey{}).(TokenEstimator)
	req, _ := ctx.Value(tokenRequestKey{}).(tokenRequest)
	if estimator == nil || req.request == nil {
		return budget.ErrTokenEstimateRequired
	}
	estimate, err := estimator(ctx, req.request, req.tools)
	if err != nil {
		return fmt.Errorf("估计模型 Token: %w", err)
	}
	return controller.ReserveEstimated(ctx, key, estimate)
}
