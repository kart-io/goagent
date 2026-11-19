package builder

import (
	"context"

	"github.com/kart-io/goagent/core/logging"
	"github.com/kart-io/goagent/core/observe"
)

// WithLogger writes the Agent's run lifecycle to l: every observe event of a
// run (agent, model call, model retry, tool call, cache, usage) becomes one
// structured record, see observe.NewLogObserver; an observer already bound to
// the context still receives every event. The Builder's own warnings and
// callback failures also go to l. Records carry metadata only, never inputs,
// outputs or provider error text.
//
// l is typically a *slog.Logger or a *slogx.Logger. Without WithLogger, run
// lifecycle is not logged and warnings go to slog.Default(). In a nested
// Agent the outermost Agent with a Logger records the whole run, so nested
// runs are not logged twice.
//
// [Core] Observability option.
func (b *AgentBuilder[S]) WithLogger(l logging.Logger) *AgentBuilder[S] {
	b.config.Logger = l
	return b
}

type loggerBoundKey struct{}

// bindLogger 在本次执行的 ctx 上追加日志观察者；外层已绑定时不重复，避免嵌套 Agent 的事件记两遍。
func (a *ConfigurableAgent[S]) bindLogger(ctx context.Context) context.Context {
	if a.config.Logger == nil || ctx.Value(loggerBoundKey{}) != nil {
		return ctx
	}
	ctx = observe.AddObserver(ctx, observe.NewLogObserver(a.config.Logger))
	return context.WithValue(ctx, loggerBoundKey{}, true)
}

// verboseLog 让 WithVerbose 的请求/响应日志写入 Builder 的 Logger。
func verboseLog(l logging.Logger) func(string) {
	l = logging.OrDefault(l)
	return func(msg string) { l.InfoContext(context.Background(), msg) }
}
