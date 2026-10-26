package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoreCanceledContextDoesNotRunQueries(t *testing.T) {
	persistent, mock, db := setupTestStore(t)
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	namespace := []string{"canceled"}
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"put", func() error { return persistent.Put(ctx, namespace, "key", "value") }},
		{"get", func() error { _, err := persistent.Get(ctx, namespace, "key"); return err }},
		{"delete", func() error { return persistent.Delete(ctx, namespace, "key") }},
		{"search", func() error { _, err := persistent.Search(ctx, namespace, nil); return err }},
		{"list", func() error { _, err := persistent.List(ctx, namespace); return err }},
		{"clear", func() error { return persistent.Clear(ctx, namespace) }},
		{"size", func() error { _, err := persistent.Size(ctx); return err }},
	} {
		t.Run(operation.name, func(t *testing.T) { require.ErrorIs(t, operation.run(), context.Canceled) })
	}
	require.NoError(t, mock.ExpectationsWereMet())
}
