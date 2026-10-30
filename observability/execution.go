package observability

import (
	"context"
	"github.com/kart-io/goagent/core/observe"
)

// ExecutionContext reuses this tracer in the optional execution instrumentation.
// It does not install a global provider or enable capture of prompt/tool bodies.
func (t *AgentTracer) ExecutionContext(ctx context.Context) context.Context {
	return observe.WithTracer(ctx, t.tracer)
}
