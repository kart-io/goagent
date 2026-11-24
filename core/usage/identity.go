// Package usage defines value-only agent identities and model usage evidence.
package usage

import "context"

type (
	// Owner identifies one agent call within a run and links it to its parent
	// call; it is the unit of budget attribution.
	Owner struct{ RunID, AgentID, AgentCallKey, ParentAgentCallKey string }
	// Invocation is an Owner plus the visit IDs of the current execution step.
	Invocation struct {
		Owner
		VisitID, ParentVisitID string
	}
)
type (
	invocationKey     struct{}
	invocationContext struct {
		invocation Invocation
		lineage    []Owner
	}
)

// WithInvocation appends an invocation while retaining a private ancestor chain.
func WithInvocation(ctx context.Context, in Invocation) context.Context {
	chain := Lineage(ctx)
	chain = append(chain, in.Owner)
	return context.WithValue(ctx, invocationKey{}, invocationContext{in, chain})
}

// FromContext returns the innermost invocation set by WithInvocation and
// whether one was set.
func FromContext(ctx context.Context) (Invocation, bool) {
	v, ok := ctx.Value(invocationKey{}).(invocationContext)
	return v.invocation, ok
}

// Lineage returns an independent root-to-current owner slice.
func Lineage(ctx context.Context) []Owner {
	v, _ := ctx.Value(invocationKey{}).(invocationContext)
	return append([]Owner(nil), v.lineage...)
}
