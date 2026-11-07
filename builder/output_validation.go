package builder

import (
	"context"
	"errors"

	agentErrors "github.com/kart-io/goagent/errors"
)

// OutputValidator 验收最终业务输出；实现必须只读并可重复调用。
type OutputValidator func(context.Context, *AgentOutput) error

// WithOutputValidator 追加最终输出验收器。所有输出来源均在中间件之后、完成回调前后以及最终投递前验收。
// 验收不会追加模型请求或回滚已发生的工具、会话、状态与回调效果；错误不能被错误处理器清除。
func (b *AgentBuilder[C, S]) WithOutputValidator(validators ...OutputValidator) *AgentBuilder[C, S] {
	b.config.OutputValidators = append(b.config.OutputValidators, validators...)
	return b
}

func (a *ConfigurableAgent[C, S]) validateOutput(ctx context.Context, out *AgentOutput) (err error) {
	defer recoverPanic("最终输出验收", &err)
	if err = ctx.Err(); err != nil {
		return err
	}
	if out == nil {
		return agentErrors.Newf(agentErrors.CodeInvalidOutput, "最终输出不能为空").WithComponent("builder").WithOperation("validate_output")
	}
	for _, validate := range a.config.OutputValidators {
		if err = validate(ctx, out); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (a *ConfigurableAgent[C, S]) outputValidationError(ctx context.Context, err error) error {
	handled := a.handleError(ctx, err)
	if errors.Is(handled, err) {
		return handled
	}
	return errors.Join(err, handled)
}
