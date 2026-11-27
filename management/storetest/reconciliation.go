package storetest

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
)

// RunReconciliationConformance checks the ReconciliationStore contract: a
// record reads back normalized, recording the same content again is a no-op,
// different content under the same ID conflicts and leaves the stored record
// untouched, IDs are scoped by tenant, and an empty ID or tenant is invalid.
// The store may hold other rows; every case uses its own tenant.
func RunReconciliationConformance(t *testing.T, newStore func(t *testing.T) mg.ReconciliationStore) {
	ctx := context.Background()
	at := time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.FixedZone("east", 8*3600))
	record := func(tenant string) mg.Reconciliation {
		return mg.Reconciliation{
			ID: "rec-1", TenantID: tenant, TaskID: "task-1", VersionHash: "v", EvidenceHash: "e",
			Resolution: "applied", VerifiedBy: "ops", VerifiedAt: at,
			OperationRefs: []durable.OperationRef{{ParentKey: "p", ID: "op-1"}, {ParentKey: "p", ID: "op-2"}},
		}
	}
	tenant := func() string { return "storetest-" + rand.Text() }
	lookup := func(t *testing.T, st mg.ReconciliationStore, tenant, id string) (mg.Reconciliation, error) {
		t.Helper()
		return st.Lookup(ctx, mg.Actor{TenantID: tenant}, id)
	}

	t.Run("a record reads back normalized", func(t *testing.T) {
		st, r := newStore(t), record(tenant())
		require.NoError(t, st.RecordReconciliation(ctx, r))
		got, err := lookup(t, st, r.TenantID, r.ID)
		require.NoError(t, err)
		require.Equal(t, mg.NormalizeReconciliation(r), got)
		require.Equal(t, time.UTC, got.VerifiedAt.Location())
	})
	t.Run("nil operation refs read back empty", func(t *testing.T) {
		st, r := newStore(t), record(tenant())
		r.OperationRefs = nil
		require.NoError(t, st.RecordReconciliation(ctx, r))
		got, err := lookup(t, st, r.TenantID, r.ID)
		require.NoError(t, err)
		require.NotNil(t, got.OperationRefs)
		require.Empty(t, got.OperationRefs)
	})

	same := []struct {
		name   string
		change func(*mg.Reconciliation)
	}{
		{"identical", func(*mg.Reconciliation) {}},
		{"same instant in another zone", func(r *mg.Reconciliation) { r.VerifiedAt = r.VerifiedAt.UTC() }},
		{"difference below a microsecond", func(r *mg.Reconciliation) { r.VerifiedAt = r.VerifiedAt.Add(100 * time.Nanosecond) }},
	}
	for _, c := range same {
		t.Run("recording again is a no-op: "+c.name, func(t *testing.T) {
			st, r := newStore(t), record(tenant())
			require.NoError(t, st.RecordReconciliation(ctx, r))
			again := r
			c.change(&again)
			require.NoError(t, st.RecordReconciliation(ctx, again))
			got, err := lookup(t, st, r.TenantID, r.ID)
			require.NoError(t, err)
			require.Equal(t, mg.NormalizeReconciliation(r), got)
		})
	}

	different := []struct {
		name   string
		change func(*mg.Reconciliation)
	}{
		{"task", func(r *mg.Reconciliation) { r.TaskID = "task-2" }},
		{"version hash", func(r *mg.Reconciliation) { r.VersionHash = "v2" }},
		{"evidence hash", func(r *mg.Reconciliation) { r.EvidenceHash = "e2" }},
		{"resolution", func(r *mg.Reconciliation) { r.Resolution = "not_applied" }},
		{"verifier", func(r *mg.Reconciliation) { r.VerifiedBy = "someone" }},
		{"verified time", func(r *mg.Reconciliation) { r.VerifiedAt = r.VerifiedAt.Add(time.Millisecond) }},
		{"operation refs", func(r *mg.Reconciliation) { r.OperationRefs = r.OperationRefs[:1] }},
		{"operation ref order", func(r *mg.Reconciliation) {
			r.OperationRefs = []durable.OperationRef{r.OperationRefs[1], r.OperationRefs[0]}
		}},
	}
	for _, c := range different {
		t.Run("different content conflicts: "+c.name, func(t *testing.T) {
			st, r := newStore(t), record(tenant())
			require.NoError(t, st.RecordReconciliation(ctx, r))
			other := r
			c.change(&other)
			require.ErrorIs(t, st.RecordReconciliation(ctx, other), agentErrors.ErrManagementConflict)
			got, err := lookup(t, st, r.TenantID, r.ID)
			require.NoError(t, err)
			require.Equal(t, mg.NormalizeReconciliation(r), got)
		})
	}

	t.Run("IDs are scoped by tenant", func(t *testing.T) {
		st, a := newStore(t), record(tenant())
		b := record(tenant())
		b.Resolution = "not_applied"
		require.NoError(t, st.RecordReconciliation(ctx, a))
		require.NoError(t, st.RecordReconciliation(ctx, b))
		got, err := lookup(t, st, b.TenantID, b.ID)
		require.NoError(t, err)
		require.Equal(t, "not_applied", got.Resolution)
		_, err = lookup(t, st, tenant(), a.ID)
		require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
		_, err = lookup(t, st, "", a.ID)
		require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
	})
	t.Run("unknown ID is not found", func(t *testing.T) {
		st, r := newStore(t), record(tenant())
		require.NoError(t, st.RecordReconciliation(ctx, r))
		_, err := lookup(t, st, r.TenantID, "rec-unknown")
		require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
	})
	for _, c := range []struct {
		name   string
		change func(*mg.Reconciliation)
	}{
		{"empty ID", func(r *mg.Reconciliation) { r.ID = "" }},
		{"empty tenant", func(r *mg.Reconciliation) { r.TenantID = "" }},
	} {
		t.Run(c.name+" is invalid", func(t *testing.T) {
			r := record(tenant())
			c.change(&r)
			require.ErrorIs(t, newStore(t).RecordReconciliation(ctx, r), agentErrors.ErrManagementInvalid)
		})
	}
}
