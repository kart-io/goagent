// Package conformance provides reusable behavior checks for external factual-memory providers.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/memory"
)

// Fixture supplies an isolated tenant and authorization scopes for one test case.
// NewFixture must return a fresh namespace or clear all records created by the case.
type Fixture struct {
	Service  memory.MemoryService
	Write    memory.WriteScope
	Read     memory.MemoryAccess
	Isolated memory.MemoryAccess
}

// Run executes provider-neutral lifecycle and isolation checks. Each subtest gets
// a fresh fixture so it can be run by a provider's own external-package tests.
func Run(t *testing.T, newFixture func(*testing.T) Fixture) {
	t.Helper()
	if newFixture == nil {
		t.Fatal("conformance fixture factory is required")
	}
	t.Run("idempotent_save", func(t *testing.T) {
		f := newFixture(t)
		input := memory.FactInput{Content: "conformance fact"}
		first, err := f.Service.Save(context.Background(), f.Write, "conformance-save", input)
		if err != nil {
			t.Fatal(err)
		}
		second, err := f.Service.Save(context.Background(), f.Write, "conformance-save", input)
		if err != nil {
			t.Fatal(err)
		}
		if first != second {
			t.Fatalf("idempotent receipts differ: %+v != %+v", first, second)
		}
	})
	t.Run("scope_isolation", func(t *testing.T) {
		f := newFixture(t)
		receipt, err := f.Service.Save(context.Background(), f.Write, "conformance-scope", memory.FactInput{Content: "tenant private"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Service.Get(context.Background(), f.Isolated, receipt.FactID); !errors.Is(err, memory.ErrAuthorization) {
			t.Fatalf("cross-scope read must be denied, got %v", err)
		}
	})
	t.Run("revision_compare_and_swap", func(t *testing.T) {
		f := newFixture(t)
		receipt, err := f.Service.Save(context.Background(), f.Write, "conformance-cas", memory.FactInput{Content: "before"})
		if err != nil {
			t.Fatal(err)
		}
		next := "after"
		if _, err := f.Service.Update(context.Background(), f.Write, receipt.FactID, receipt.Revision, memory.FactPatch{Content: &next}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Service.Update(context.Background(), f.Write, receipt.FactID, receipt.Revision, memory.FactPatch{Content: &next}); !errors.Is(err, memory.ErrRevisionConflict) {
			t.Fatalf("stale revision must conflict, got %v", err)
		}
	})
	t.Run("delete_blocks_reads", func(t *testing.T) {
		f := newFixture(t)
		receipt, err := f.Service.Save(context.Background(), f.Write, "conformance-delete", memory.FactInput{Content: "delete me"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Service.Delete(context.Background(), f.Write, receipt.FactID, receipt.Revision); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Service.Get(context.Background(), f.Read, receipt.FactID); !errors.Is(err, memory.ErrGone) {
			t.Fatalf("deleted fact must be unavailable, got %v", err)
		}
	})
	t.Run("list_applies_tags_and_source_filters", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		for _, record := range []struct{ key, content, tag, source string }{{"match", "filtered match", "wanted", "doc-a"}, {"other", "filtered other", "different", "doc-b"}} {
			_, err := f.Service.Save(ctx, f.Write, memory.IdempotencyKey(record.key), memory.FactInput{Content: record.content, Metadata: map[string]any{"tags": []string{record.tag}}, Source: memory.FactSource{Kind: "document", SourceID: record.source}})
			if err != nil {
				t.Fatal(err)
			}
		}
		page, err := f.Service.List(ctx, f.Read, memory.FactFilter{Tags: []string{"wanted"}, SourceIDs: []string{"doc-a"}}, memory.PageRequest{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].Content != "filtered match" {
			t.Fatalf("list filters were not applied: %+v", page)
		}
	})
	t.Run("supersede_hides_old_fact", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		first, err := f.Service.Save(ctx, f.Write, "conformance-old", memory.FactInput{Content: "old fact"})
		if err != nil {
			t.Fatal(err)
		}
		next, err := f.Service.Supersede(ctx, f.Write, first.FactID, first.Revision, "conformance-new", memory.FactInput{Content: "new fact"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Service.Get(ctx, f.Read, first.FactID); !errors.Is(err, memory.ErrGone) {
			t.Fatalf("superseded fact must be hidden: %v", err)
		}
		current, err := f.Service.Get(ctx, f.Read, next.FactID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Content != "new fact" || current.SupersedesID != first.FactID {
			t.Fatalf("supersession link missing: %+v", current)
		}
	})
	t.Run("change_history_excludes_fact_bodies", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		receipt, err := f.Service.Save(ctx, f.Write, "conformance-history", memory.FactInput{Content: "sensitive historical body", Source: memory.FactSource{Kind: "operator", SourceID: "confirmation"}})
		if err != nil {
			t.Fatal(err)
		}
		page, err := f.Service.ListChanges(ctx, f.Read, receipt.FactID, memory.PageRequest{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			t.Fatal("expected a change record")
		}
		encoded, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "sensitive historical body") {
			t.Fatalf("change history leaked fact body: %s", encoded)
		}
	})
	t.Run("expired_fact_is_not_read_or_listed", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		expiresAt := time.Now().Add(100 * time.Millisecond)
		receipt, err := f.Service.Save(ctx, f.Write, "conformance-expired", memory.FactInput{Content: "expired", ExpiresAt: expiresAt})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Until(expiresAt) + 10*time.Millisecond)
		if _, err = f.Service.Get(ctx, f.Read, receipt.FactID); !errors.Is(err, memory.ErrGone) {
			t.Fatalf("expired fact remained readable: %v", err)
		}
		page, err := f.Service.List(ctx, f.Read, memory.FactFilter{}, memory.PageRequest{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		for _, fact := range page.Items {
			if fact.ID == receipt.FactID {
				t.Fatal("expired fact remained listable")
			}
		}
	})
	t.Run("scope_clear_immediately_hides_facts", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		receipt, err := f.Service.Save(ctx, f.Write, "conformance-clear", memory.FactInput{Content: "clear me"})
		if err != nil {
			t.Fatal(err)
		}
		clear := memory.ClearAccess{Principal: f.Write.Access.Principal, TargetScopes: []memory.MemoryScope{f.Write.Target}, Capabilities: []string{"memory.clear"}}
		if _, err = f.Service.DeleteScope(ctx, clear, f.Write.Target, "conformance-clear-scope"); err != nil {
			t.Fatal(err)
		}
		if _, err = f.Service.Get(ctx, f.Read, receipt.FactID); !errors.Is(err, memory.ErrGone) {
			t.Fatalf("scope clear did not immediately hide fact: %v", err)
		}
	})
}
