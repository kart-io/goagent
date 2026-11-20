package memstore_test

import (
	"testing"

	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/memstore"
	"github.com/kart-io/goagent/management/storetest"
)

func TestListConformance(t *testing.T) {
	storetest.RunListConformance(t, func(*testing.T) mg.Store { return memstore.New() })
}
