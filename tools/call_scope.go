package tools

import (
	"context"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

type depthKey struct{}
type depthScope struct{ depth, limit int }

func enterRunnable(ctx context.Context, maxDepth int, modelInvoking bool) (context.Context, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, _ := ctx.Value(depthKey{}).(depthScope)
	if scope.limit == 0 || maxDepth < scope.limit {
		scope.limit = maxDepth
	}
	scope.depth++
	if scope.depth > scope.limit {
		return nil, agentErrors.ErrNestedExecutionLimit
	}
	if modelInvoking {
		c, ok := budget.FromContext(ctx)
		if !ok {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "model tool requires root budget").
				WithComponent("runnable_tool").
				WithOperation("enter_scope")
		}
		if durable.HasJournal(ctx) && !c.IsDurable() {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "model tool requires durable root budget").
				WithComponent("runnable_tool").
				WithOperation("enter_scope")
		}
	}
	return context.WithValue(ctx, depthKey{}, scope), nil
}

type dispatcherKey struct{}

func enterDispatcher(ctx context.Context, d *Dispatcher) (context.Context, error) {
	stack, _ := ctx.Value(dispatcherKey{}).([]*Dispatcher)
	for _, active := range stack {
		if active == d {
			return nil, agentErrors.ErrDispatcherReentry
		}
	}
	next := append(append([]*Dispatcher(nil), stack...), d)
	return context.WithValue(ctx, dispatcherKey{}, next), nil
}
