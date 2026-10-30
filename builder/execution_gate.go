package builder

import (
	"context"
	"github.com/kart-io/goagent/core/budget"
)

// beginExecution protects a whole session turn, including middleware and
// callbacks. Queueing consumes the same timeout budget as execution.
func (a *ConfigurableAgent[C, S]) beginExecution(ctx context.Context) (context.Context, func(), error) {
	var cancel context.CancelFunc
	if a.config.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, a.config.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	if controller, ok := budget.FromContext(ctx); ok {
		parentCancel := cancel
		var rootCancel context.CancelFunc
		ctx, rootCancel = budget.Bind(ctx, controller)
		cancel = func() { rootCancel(); parentCancel() }
	}
	if err := ctx.Err(); err != nil {
		cancel()
		return ctx, nil, err
	}
	select {
	case a.executionGate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-a.executionGate
			cancel()
			return ctx, nil, err
		}
		return ctx, func() { cancel(); <-a.executionGate }, nil
	case <-ctx.Done():
		cancel()
		return ctx, nil, ctx.Err()
	}
}
