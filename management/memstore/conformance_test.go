package memstore_test

import (
	"testing"

	"github.com/kart-io/goagent/core/session"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/memstore"
	"github.com/kart-io/goagent/management/storetest"
)

func TestListConformance(t *testing.T) {
	storetest.RunListConformance(t, func(*testing.T) mg.Store { return memstore.New() })
}

func TestHistoryConformance(t *testing.T) {
	storetest.RunHistoryConformance(t, func(*testing.T) (mg.HistoryReader, session.Store) {
		sessions := session.NewMemoryStore()
		return mg.SessionHistory{Sessions: sessions}, sessions
	})
}

func TestReconciliationConformance(t *testing.T) {
	storetest.RunReconciliationConformance(t, func(*testing.T) mg.ReconciliationStore { return &memstore.Reconciliations{} })
}
