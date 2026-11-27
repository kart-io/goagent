package memstore

import (
	"context"
	"slices"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
)

// Reconciliations 是 management.ReconciliationStore 的进程内实现，零值可用。
type Reconciliations struct {
	mu   sync.Mutex
	rows map[[2]string]mg.Reconciliation
}

func (s *Reconciliations) RecordReconciliation(_ context.Context, r mg.Reconciliation) error {
	if r.ID == "" || r.TenantID == "" {
		return agentErrors.ErrManagementInvalid
	}
	r = mg.NormalizeReconciliation(r)
	r.OperationRefs = slices.Clone(r.OperationRefs)
	s.mu.Lock()
	defer s.mu.Unlock()
	k := [2]string{r.TenantID, r.ID}
	if stored, ok := s.rows[k]; ok {
		if !mg.SameReconciliation(stored, r) {
			return agentErrors.ErrManagementConflict
		}
		return nil
	}
	if s.rows == nil {
		s.rows = map[[2]string]mg.Reconciliation{}
	}
	s.rows[k] = r
	return nil
}

func (s *Reconciliations) Lookup(_ context.Context, a mg.Actor, id string) (mg.Reconciliation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[[2]string{a.TenantID, id}]
	if !ok {
		return mg.Reconciliation{}, agentErrors.ErrManagementNotFound
	}
	r.OperationRefs = slices.Clone(r.OperationRefs)
	return r, nil
}

var _ mg.ReconciliationStore = (*Reconciliations)(nil)
