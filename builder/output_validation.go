package builder

import (
	"context"
	"errors"

	agentErrors "github.com/kart-io/goagent/errors"
)

// OutputValidator validates the final business output; implementations must be
// read-only and safe to call repeatedly.
type OutputValidator func(context.Context, *AgentOutput) error

// WithOutputValidator appends final output validators. Output from every source
// is validated after middleware, before and after completion callbacks, and
// before final delivery. Validation does not add model requests or roll back
// effects already made by tools, sessions, state, or callbacks; its errors
// cannot be cleared by the error handler.
func (b *AgentBuilder[S]) WithOutputValidator(validators ...OutputValidator) *AgentBuilder[S] {
	b.config.OutputValidators = append(b.config.OutputValidators, validators...)
	return b
}

func (a *ConfigurableAgent[S]) validateOutput(ctx context.Context, out *AgentOutput) (err error) {
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

func (a *ConfigurableAgent[S]) outputValidationError(ctx context.Context, err error) error {
	handled := a.handleError(ctx, err)
	if errors.Is(handled, err) {
		return handled
	}
	return errors.Join(err, handled)
}
