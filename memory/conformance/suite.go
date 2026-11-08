// Package conformance provides reusable behavior checks for external factual-memory providers.
package conformance

import (
	"context"
	"errors"
	"testing"

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
}
