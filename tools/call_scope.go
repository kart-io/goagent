package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
)

// ErrNestedExecutionLimit rejects recursion before starting another child.
var ErrNestedExecutionLimit = errors.New("nested runnable depth exceeded")

// ErrDispatcherReentry rejects waiting for a slot held by an ancestor invocation.
var ErrDispatcherReentry = errors.New("dispatcher reentry requires an independent child dispatcher")

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
		return nil, ErrNestedExecutionLimit
	}
	if modelInvoking {
		c, ok := budget.FromContext(ctx)
		if !ok {
			return nil, fmt.Errorf("model tool requires root budget")
		}
		if durable.HasJournal(ctx) && !c.IsDurable() {
			return nil, fmt.Errorf("model tool requires durable root budget")
		}
	}
	return context.WithValue(ctx, depthKey{}, scope), nil
}

type dispatcherKey struct{}

func enterDispatcher(ctx context.Context, d *Dispatcher) (context.Context, error) {
	stack, _ := ctx.Value(dispatcherKey{}).([]*Dispatcher)
	for _, active := range stack {
		if active == d {
			return nil, ErrDispatcherReentry
		}
	}
	next := append(append([]*Dispatcher(nil), stack...), d)
	return context.WithValue(ctx, dispatcherKey{}, next), nil
}
