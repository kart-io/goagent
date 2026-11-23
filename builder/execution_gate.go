package builder

import (
	"context"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/logging"
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
	a.warnUnboundedTurn(ctx)
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

// warnUnboundedTurn 在带工具的 Agent 既无迭代上限、也无截止时间和预算时警告一次：
// 限额改由调用方配置后，这种组合下模型反复调用工具不会被任何机制终止。
func (a *ConfigurableAgent[S]) warnUnboundedTurn(ctx context.Context) {
	if len(a.tools) == 0 || a.config.MaxIterations > 0 {
		return
	}
	if _, ok := ctx.Deadline(); ok {
		return
	}
	if _, ok := budget.FromContext(ctx); ok {
		return
	}
	a.unboundedWarned.Do(func() {
		logging.OrDefault(a.config.Logger).WarnContext(ctx, "Agent 回合没有任何上限：未设置 MaxIterations、Timeout，ctx 也没有截止时间或预算，模型持续调用工具时不会停止",
			"agent", a.config.AgentID)
	})
}
