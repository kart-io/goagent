package tools

import (
	"context"

	"github.com/kart-io/goagent/core/durable"
)

// Reconciliation is a host-verified conclusion about interrupted tool calls
// whose effect was unknown. Resolution is "applied" or "not_applied" and
// applies uniformly to every call listed in Operations: a host that needs
// different conclusions for different calls must keep side-effecting tools
// serial so that at most one call is ever unresolved.
type Reconciliation struct {
	Resolution string
	Operations []durable.OperationRef
}

type reconciliationKey struct{}

// WithReconciliation returns a ctx in which the Dispatcher settles the listed
// interrupted calls with r instead of the tool's own Reconcile. Calls whose
// operation ID ("tool/"+CallID) and parent execution key do not match an entry
// in r.Operations follow their normal policy. For "applied", the output comes
// from ReconcilableTool.Reconcile when it finds a successful result, and is a
// fixed explanation otherwise; for "not_applied", the call is settled as a
// Rejected result without calling Reconcile or rerunning the tool.
func WithReconciliation(ctx context.Context, r Reconciliation) context.Context {
	return context.WithValue(ctx, reconciliationKey{}, r)
}

func reconciliationFor(ctx context.Context, parentKey, id string) (string, bool) {
	r, ok := ctx.Value(reconciliationKey{}).(Reconciliation)
	if !ok || parentKey == "" {
		return "", false
	}
	for _, ref := range r.Operations {
		if ref.ParentKey == parentKey && ref.ID == id {
			return r.Resolution, true
		}
	}
	return "", false
}

const (
	reconciledAppliedWithoutOutput = "operation was verified as applied; output unavailable"
	reconciledNotApplied           = "operation was not applied; call again if still needed"
)
