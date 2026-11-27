// Package storetest provides conformance checks shared by the implementations
// of management's storage ports (Store, HistoryReader, ReconciliationStore),
// so every backend honours the same contracts.
package storetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
)

// RunListConformance checks the List contract documented on management.Reader:
// Limit 0 returns every row without a Store-imposed cap, a positive Limit is
// honoured exactly, rows are ordered by CreatedAt then ID, the cursor is
// exclusive, negative limits are rejected, and filters combine with paging.
// newStore must return an empty Store for each call.
func RunListConformance(t *testing.T, newStore func(t *testing.T) mg.Store) {
	const tenant = "storetest-tenant"
	// More rows than any former default page (201) or scan cap would return.
	const rows = 450
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	seed := func(t *testing.T) mg.Store {
		t.Helper()
		st := newStore(t)
		ctx := context.Background()
		require.NoError(t, st.WithinTx(ctx, func(tx mg.Tx) error {
			for i := 0; i < rows; i++ {
				// Pairs share a CreatedAt so the ID tie-break is exercised.
				created := base.Add(time.Duration(i/2) * time.Millisecond)
				state := "pending"
				if i%3 == 0 {
					state = "done"
				}
				id := fmt.Sprintf("row-%04d", rows-i)
				raw, err := json.Marshal(mg.Delivery{Meta: mg.Meta{ID: id, TenantID: tenant, Revision: 1, CreatedAt: created}, Kind: "storetest", ResourceID: "r", State: state})
				if err != nil {
					return err
				}
				if err = tx.Insert(ctx, mg.Document{Kind: "outbox", TenantID: tenant, ID: id, Revision: 1, CreatedAt: created, Data: raw}); err != nil {
					return err
				}
			}
			return nil
		}))
		return st
	}
	list := func(t *testing.T, st mg.Store, q mg.Query) ([]mg.Document, error) {
		t.Helper()
		return st.List(context.Background(), "outbox", mg.Selection{TenantID: tenant, Query: q, Visibility: mg.Visibility{All: true}})
	}
	ordered := func(t *testing.T, docs []mg.Document) {
		t.Helper()
		for i := 1; i < len(docs); i++ {
			a, b := docs[i-1], docs[i]
			require.True(t, a.CreatedAt.Before(b.CreatedAt) || a.CreatedAt.Equal(b.CreatedAt) && a.ID < b.ID, "rows must be ordered by CreatedAt then ID: %s before %s", a.ID, b.ID)
		}
	}
	cursor := func(d mg.Document) string { return mg.FormatCursor(d.CreatedAt, d.ID) }

	t.Run("zero limit returns every row", func(t *testing.T) {
		docs, err := list(t, seed(t), mg.Query{})
		require.NoError(t, err)
		require.Len(t, docs, rows)
		ordered(t, docs)
	})
	t.Run("positive limit is exact", func(t *testing.T) {
		st := seed(t)
		for _, limit := range []int{1, 7, rows, rows + 1} {
			docs, err := list(t, st, mg.Query{Limit: limit})
			require.NoError(t, err)
			require.Len(t, docs, min(limit, rows))
		}
	})
	t.Run("negative limit is invalid", func(t *testing.T) {
		_, err := list(t, seed(t), mg.Query{Limit: -1})
		require.Error(t, err)
	})
	t.Run("cursor pages are exclusive and complete", func(t *testing.T) {
		st := seed(t)
		whole, err := list(t, st, mg.Query{})
		require.NoError(t, err)
		var paged []mg.Document
		q := mg.Query{Limit: 64}
		for {
			docs, err := list(t, st, q)
			require.NoError(t, err)
			paged = append(paged, docs...)
			if len(docs) < q.Limit {
				break
			}
			q.Cursor = cursor(docs[len(docs)-1])
		}
		require.Equal(t, ids(whole), ids(paged))
	})
	t.Run("malformed cursor is invalid", func(t *testing.T) {
		st := seed(t)
		for _, c := range []string{"no-separator", "not-a-time|row-0001"} {
			_, err := list(t, st, mg.Query{Cursor: c})
			require.ErrorIs(t, err, agentErrors.ErrManagementInvalid, c)
		}
	})
	t.Run("filters combine with paging", func(t *testing.T) {
		st := seed(t)
		whole, err := list(t, st, mg.Query{Statuses: []string{"pending"}})
		require.NoError(t, err)
		require.Len(t, whole, rows-rows/3)
		first, err := list(t, st, mg.Query{Statuses: []string{"pending"}, Limit: 10})
		require.NoError(t, err)
		rest, err := list(t, st, mg.Query{Statuses: []string{"pending"}, Cursor: cursor(first[len(first)-1])})
		require.NoError(t, err)
		require.Equal(t, ids(whole), append(ids(first), ids(rest)...))
	})
}

func ids(docs []mg.Document) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.ID
	}
	return out
}
