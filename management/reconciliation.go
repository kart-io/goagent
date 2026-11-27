package management

import (
	"context"
	"slices"
	"time"

	"github.com/kart-io/goagent/core/durable"
)

// ReconciliationStore records the evidence a host verified for a task in
// attention and serves it back through ReconciliationReader.
//
// RecordReconciliation is idempotent by (TenantID, ID): recording a record
// that is SameReconciliation as the stored one is a no-op, and one that
// differs returns errors.ErrManagementConflict and leaves the stored record
// untouched. An empty ID or TenantID returns errors.ErrManagementInvalid.
// Lookup returns the record as NormalizeReconciliation leaves it and only
// sees records of the actor's tenant; anything else is
// errors.ErrManagementNotFound. management/storetest checks this contract.
type ReconciliationStore interface {
	ReconciliationReader
	RecordReconciliation(context.Context, Reconciliation) error
}

// NormalizeReconciliation brings a record to the form every store keeps: UTC
// at microsecond precision and a non-nil operation list.
func NormalizeReconciliation(r Reconciliation) Reconciliation {
	r.VerifiedAt = r.VerifiedAt.UTC().Truncate(time.Microsecond)
	if r.OperationRefs == nil {
		r.OperationRefs = []durable.OperationRef{}
	}
	return r
}

// SameReconciliation reports whether two records are equal once normalized.
func SameReconciliation(a, b Reconciliation) bool {
	a, b = NormalizeReconciliation(a), NormalizeReconciliation(b)
	return a.ID == b.ID && a.TenantID == b.TenantID && a.TaskID == b.TaskID && a.VersionHash == b.VersionHash &&
		a.EvidenceHash == b.EvidenceHash && a.Resolution == b.Resolution && a.VerifiedBy == b.VerifiedBy &&
		a.VerifiedAt.Equal(b.VerifiedAt) && slices.Equal(a.OperationRefs, b.OperationRefs)
}
