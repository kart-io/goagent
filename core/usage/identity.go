// Package usage defines value-only agent identities and model usage evidence.
package usage

import "context"

type Owner struct{ RunID, AgentID, AgentCallKey, ParentAgentCallKey string }
type Invocation struct {
	Owner
	VisitID, ParentVisitID string
}
type invocationKey struct{}
type invocationContext struct {
	invocation Invocation
	lineage    []Owner
}

// WithInvocation appends an invocation while retaining a private ancestor chain.
func WithInvocation(ctx context.Context, in Invocation) context.Context {
	chain := Lineage(ctx)
	chain = append(chain, in.Owner)
	return context.WithValue(ctx, invocationKey{}, invocationContext{in, chain})
}
func FromContext(ctx context.Context) (Invocation, bool) {
	v, ok := ctx.Value(invocationKey{}).(invocationContext)
	return v.invocation, ok
}

// Lineage returns an independent root-to-current owner slice.
func Lineage(ctx context.Context) []Owner {
	v, _ := ctx.Value(invocationKey{}).(invocationContext)
	return append([]Owner(nil), v.lineage...)
}
