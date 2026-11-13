package memorytest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/memory/memorytest"
)

func TestFileStore(t *testing.T) {
	memorytest.Run(t, func(t *testing.T) (durable.Store, durable.Store) {
		dir := t.TempDir()
		a, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		b, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		return a, b
	})
}
