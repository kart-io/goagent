package builder

import (
	"context"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/budget"
)

type persistTimeoutKey struct{}

// persistTimeoutFrom 返回本次执行的落盘时限；子 Agent 自己的 beginExecution 会覆盖它。
func persistTimeoutFrom(ctx context.Context) time.Duration {
	if d, ok := ctx.Value(persistTimeoutKey{}).(time.Duration); ok && d > 0 {
		return d
	}
	return constants.DefaultInboxWriteTimeout
}

// beginExecution protects a whole session turn, including middleware and
// callbacks. Queueing consumes the same timeout budget as execution.
func (a *ConfigurableAgent[S]) beginExecution(ctx context.Context) (context.Context, func(), error) {
	var cancel context.CancelFunc
	if a.config.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, a.config.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	ctx = context.WithValue(ctx, persistTimeoutKey{}, a.config.persistTimeout())
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
