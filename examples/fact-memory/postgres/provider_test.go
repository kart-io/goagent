package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/memory/conformance"
)

func TestPostgresFactPersistsAcrossStoreRestart(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL persistence tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_test_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
	})
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-restart", OwnerKind: memory.OwnerAgent, OwnerID: "user", AgentID: "agent-restart"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: "user", AgentID: scope.AgentID}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	write := memory.WriteScope{Access: access, Target: scope}
	input := memory.FactInput{Content: "prefers Mandarin", RecallClass: memory.RecallAlways, Metadata: map[string]any{"kind": "preference"}}
	first, err := service.Save(ctx, write, "restart-save", input)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	process := exec.Command(os.Args[0], "-test.run=^TestPostgresProcessRestartHelper$")
	process.Env = append(os.Environ(), "GOAGENT_FACT_MEMORY_HELPER=1", "GOAGENT_FACT_MEMORY_SCHEMA="+schema, "GOAGENT_FACT_MEMORY_ID="+string(first.FactID), "GOAGENT_FACT_MEMORY_TENANT="+scope.TenantID)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("fresh process could not read persisted fact: %v\n%s", err, output)
	}
	store, err = Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err = newService(store)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := service.Get(ctx, access, first.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if fact.Content != input.Content || fact.Revision != first.Revision {
		t.Fatalf("reopened Store returned wrong fact: %+v", fact)
	}
	replayed, err := service.Save(ctx, write, "restart-save", input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed != first {
		t.Fatalf("restart replay changed receipt: %+v != %+v", replayed, first)
	}
	if _, err = service.Delete(ctx, write, first.FactID, first.Revision); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err = newService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Get(ctx, access, first.FactID); !is(err, memory.ErrGone) {
		t.Fatalf("deleted fact survived Store restart: %v", err)
	}
	if _, err = service.Save(ctx, write, "restart-save", input); !is(err, memory.ErrGone) {
		t.Fatalf("deleted idempotent replay leaked or resurrected fact: %v", err)
	}
	var content string
	if err = store.pool.QueryRow(ctx, `SELECT content FROM `+store.table("facts")+` WHERE id=$1`, string(first.FactID)).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "" {
		t.Fatal("logical delete retained fact body")
	}
}

func TestPostgresIdempotencyExpiresToDigestOnlyTombstone(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL idempotency tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_idem_tombstone_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		store.Close()
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
		store.Close()
	})
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-idempotency", OwnerKind: memory.OwnerUser, OwnerID: "user-idempotency"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: "agent"}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	write := memory.WriteScope{Access: access, Target: scope}
	const key = "sensitive-stable-operation-key"
	if _, err = service.Save(ctx, write, key, memory.FactInput{Content: "fact"}); err != nil {
		t.Fatal(err)
	}
	hash := idempotencyDigest(key)
	var storedKey string
	if err = store.pool.QueryRow(ctx, `SELECT idem_key FROM `+store.table("operations")+` WHERE tenant_id=$1`, scope.TenantID).Scan(&storedKey); err != nil {
		t.Fatal(err)
	}
	if storedKey != hash || storedKey == key {
		t.Fatalf("raw idempotency key was retained: %q", storedKey)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE `+store.table("operations")+` SET expires_at=now()-interval '1 second' WHERE tenant_id=$1`, scope.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ClaimIndexMutations(ctx, 10); err != nil {
		t.Fatalf("maintenance poll did not prune expired operation receipts: %v", err)
	}
	if _, err = service.Save(ctx, write, key, memory.FactInput{Content: "fact"}); !errors.Is(err, memory.ErrIdempotencyExpired) {
		t.Fatalf("expired operation replay did not reject permanently: %v", err)
	}
	var operationID, factID, lifecycle string
	var revision int64
	var expired bool
	if err = store.pool.QueryRow(ctx, `SELECT operation_id,fact_id,revision,lifecycle,expired FROM `+store.table("operations")+` WHERE tenant_id=$1 AND idem_key=$2`, scope.TenantID, hash).Scan(&operationID, &factID, &revision, &lifecycle, &expired); err != nil {
		t.Fatal(err)
	}
	if operationID != "" || factID != "" || revision != 0 || lifecycle != "expired" || !expired {
		t.Fatalf("expired key retained its receipt instead of a digest tombstone: op=%q fact=%q rev=%d lifecycle=%q expired=%v", operationID, factID, revision, lifecycle, expired)
	}
}

func TestPostgresProcessRestartHelper(t *testing.T) {
	if os.Getenv("GOAGENT_FACT_MEMORY_HELPER") != "1" {
		return
	}
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	schema := os.Getenv("GOAGENT_FACT_MEMORY_SCHEMA")
	store, err := Open(context.Background(), dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: os.Getenv("GOAGENT_FACT_MEMORY_TENANT"), OwnerKind: memory.OwnerAgent, OwnerID: "user", AgentID: "agent-restart"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: "user", AgentID: scope.AgentID}, ReadScopes: []memory.MemoryScope{scope}}
	fact, err := service.Get(context.Background(), access, memory.FactID(os.Getenv("GOAGENT_FACT_MEMORY_ID")))
	if err != nil {
		t.Fatal(err)
	}
	if fact.Content != "prefers Mandarin" {
		t.Fatalf("fresh process read wrong value: %q", fact.Content)
	}
}

func TestPostgresRecallAfterIndependentProcessRestart(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL recall tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_recall_restart_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
		store.Close()
	}()
	index, err := OpenIndex(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-recall-restart", OwnerKind: memory.OwnerAgent, OwnerID: "user", AgentID: "agent-recall-restart"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: "user", AgentID: scope.AgentID}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	service, err := memory.NewDefaultMemoryService(memory.MemoryServiceConfig{Store: store, Capabilities: store.Capabilities(), Limits: memory.DefaultFactLimits(), Index: index, Embedders: providerEmbedderResolver{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Save(ctx, memory.WriteScope{Access: access, Target: scope}, "restart-recall", memory.FactInput{Content: "prefers quiet workspaces", Metadata: map[string]any{"tags": []string{"preference"}}}); err != nil {
		t.Fatal(err)
	}
	worker, err := memory.NewFactWorker(memory.FactWorkerConfig{Store: store, Index: index, Embedders: providerEmbedderResolver{}, BatchSize: 10, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	store.Close()
	index.Close()
	process := exec.Command(os.Args[0], "-test.run=^TestPostgresRecallRestartHelper$")
	process.Env = append(os.Environ(), "GOAGENT_FACT_RECALL_HELPER=1", "GOAGENT_FACT_MEMORY_SCHEMA="+schema, "GOAGENT_FACT_MEMORY_TENANT="+scope.TenantID, "GOAGENT_FACT_MEMORY_AGENT="+scope.AgentID)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("fresh process could not recall persisted fact: %v\n%s", err, output)
	}
}

func TestPostgresRecallRestartHelper(t *testing.T) {
	if os.Getenv("GOAGENT_FACT_RECALL_HELPER") != "1" {
		return
	}
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	schema := os.Getenv("GOAGENT_FACT_MEMORY_SCHEMA")
	store, err := Open(context.Background(), dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	index, err := OpenIndex(context.Background(), dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	service, err := memory.NewDefaultMemoryService(memory.MemoryServiceConfig{Store: store, Capabilities: store.Capabilities(), Limits: memory.DefaultFactLimits(), Index: index, Embedders: providerEmbedderResolver{}})
	if err != nil {
		t.Fatal(err)
	}
	tenant, agent := os.Getenv("GOAGENT_FACT_MEMORY_TENANT"), os.Getenv("GOAGENT_FACT_MEMORY_AGENT")
	scope := memory.MemoryScope{TenantID: tenant, OwnerKind: memory.OwnerAgent, OwnerID: "user", AgentID: agent}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: tenant, UserID: "user", AgentID: agent}, ReadScopes: []memory.MemoryScope{scope}}
	result, err := service.Recall(context.Background(), access, "quiet workspace", memory.RecallOptions{Limit: 5, MinScore: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Fact.Content != "prefers quiet workspaces" {
		t.Fatalf("fresh process recalled wrong facts: %+v", result)
	}
}

func TestPostgresProviderConformance(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL conformance tests must not skip")
	}
	conformance.Run(t, func(t *testing.T) conformance.Fixture {
		ctx := context.Background()
		schema, err := randomID("fact_suite_")
		if err != nil {
			t.Fatal(err)
		}
		schema = schema[:min(len(schema), 60)]
		store, err := Open(ctx, dsn, schema)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = store.pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
			store.Close()
		})
		service, err := newService(store)
		if err != nil {
			t.Fatal(err)
		}
		scope := memory.MemoryScope{TenantID: "suite-" + schema, OwnerKind: memory.OwnerAgent, OwnerID: "user", AgentID: "agent"}
		writeAccess := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: "user", AgentID: "agent"}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
		isolated := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: "other", AgentID: "other-agent"}, ReadScopes: []memory.MemoryScope{{TenantID: scope.TenantID, OwnerKind: memory.OwnerAgent, OwnerID: "other-agent", AgentID: "other-agent"}}}
		return conformance.Fixture{Service: service, Write: memory.WriteScope{Access: writeAccess, Target: scope}, Read: writeAccess, Isolated: isolated}
	})
}

func TestPostgresAgentPrivateScopeSeparatesUsersOfSameAgent(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL scope tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_user_private_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		store.Close()
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
		store.Close()
	})
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	const tenant, agentID = "tenant-shared-agent", "same-agent"
	scopeA := memory.MemoryScope{TenantID: tenant, OwnerKind: memory.OwnerAgent, OwnerID: "user-a", AgentID: agentID}
	accessA := memory.MemoryAccess{Principal: memory.Principal{TenantID: tenant, UserID: "user-a", AgentID: agentID}, ReadScopes: []memory.MemoryScope{scopeA}, WriteScopes: []memory.MemoryScope{scopeA}}
	if _, err = service.Save(ctx, memory.WriteScope{Access: accessA, Target: scopeA}, "private-a", memory.FactInput{Content: "user A secret"}); err != nil {
		t.Fatal(err)
	}
	accessB := memory.MemoryAccess{Principal: memory.Principal{TenantID: tenant, UserID: "user-b", AgentID: agentID}, ReadScopes: []memory.MemoryScope{scopeA}, WriteScopes: []memory.MemoryScope{scopeA}}
	if _, err = service.List(ctx, accessB, memory.FactFilter{}, memory.PageRequest{Limit: 10}); !errors.Is(err, memory.ErrAuthorization) {
		t.Fatalf("same AgentID allowed another user to list private facts: %v", err)
	}
	if _, err = service.Save(ctx, memory.WriteScope{Access: accessB, Target: scopeA}, "private-b", memory.FactInput{Content: "user B"}); !errors.Is(err, memory.ErrAuthorization) {
		t.Fatalf("same AgentID allowed another user to write private facts: %v", err)
	}
}

func TestPostgresScopeClearBlocksWritesAndResumesWorker(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL clear tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_clear_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		store.Close()
	}()
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-clear", OwnerKind: memory.OwnerAgent, OwnerID: "user", AgentID: "agent-clear"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: "user", AgentID: scope.AgentID}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	write := memory.WriteScope{Access: access, Target: scope}
	if _, err = service.Save(ctx, write, "pre-clear", memory.FactInput{Content: "old content"}); err != nil {
		t.Fatal(err)
	}
	clearAccess := memory.ClearAccess{Principal: access.Principal, TargetScopes: []memory.MemoryScope{scope}, Capabilities: []string{"memory.clear"}}
	operation, err := service.DeleteScope(ctx, clearAccess, scope, "clear-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Save(ctx, write, "during-clear", memory.FactInput{Content: "must block"}); !errors.Is(err, memory.ErrCapacityExceeded) {
		t.Fatalf("write during clear was not rejected: %v", err)
	}
	index, err := OpenIndex(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	worker, err := memory.NewFactWorker(memory.FactWorkerConfig{Store: store, Index: index, Embedders: providerEmbedderResolver{}, BatchSize: 20, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := service.List(ctx, access, memory.FactFilter{}, memory.PageRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Items) != 0 {
		t.Fatalf("cleared scope still has visible facts: %+v", current.Items)
	}
	clearStatus, err := store.GetClearOperation(ctx, access, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if clearStatus.State != "complete" {
		t.Fatalf("clear worker did not complete: %+v", clearStatus)
	}
	if _, err = service.Save(ctx, write, "after-clear", memory.FactInput{Content: "new content"}); err != nil {
		t.Fatalf("write after completed clear failed: %v", err)
	}
}

func TestPostgresScopeClearResumesAfterStoreRestart(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL clear recovery tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_clear_restart_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
		store.Close()
	})
	scope := memory.MemoryScope{TenantID: "tenant-clear-restart", OwnerKind: memory.OwnerAgent, OwnerID: "user", AgentID: "agent-clear-restart"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: scope.AgentID}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	write := memory.WriteScope{Access: access, Target: scope}
	if _, err = service.Save(ctx, write, "before-restart-clear", memory.FactInput{Content: "must remain hidden"}); err != nil {
		t.Fatal(err)
	}
	clearAccess := memory.ClearAccess{Principal: access.Principal, TargetScopes: []memory.MemoryScope{scope}, Capabilities: []string{"memory.clear"}}
	operation, err := service.DeleteScope(ctx, clearAccess, scope, "restart-clear-key")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate process loss after the clear transaction commits but before an
	// index worker claims any cleanup work.
	store.Close()
	store, err = Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service, err = newService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Save(ctx, write, "during-restarted-clear", memory.FactInput{Content: "still blocked"}); !errors.Is(err, memory.ErrCapacityExceeded) {
		t.Fatalf("clear state did not survive Store restart: %v", err)
	}
	index, err := OpenIndex(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	worker, err := memory.NewFactWorker(memory.FactWorkerConfig{Store: store, Index: index, Embedders: providerEmbedderResolver{}, BatchSize: 20, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := store.GetClearOperation(ctx, access, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "complete" {
		t.Fatalf("restarted Worker did not complete persisted clear: %+v", status)
	}
	if _, err = service.Save(ctx, write, "after-restarted-clear", memory.FactInput{Content: "new fact"}); err != nil {
		t.Fatalf("write remained blocked after restarted cleanup completed: %v", err)
	}
}

func TestPostgresConcurrentClearKeysRemainBoundToSameOperation(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL clear idempotency tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_clear_keys_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		store.Close()
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
		store.Close()
	})
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-clear-keys", OwnerKind: memory.OwnerUser, OwnerID: "user-clear-keys"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: "agent"}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	write := memory.WriteScope{Access: access, Target: scope}
	if _, err = service.Save(ctx, write, "before-clear", memory.FactInput{Content: "old"}); err != nil {
		t.Fatal(err)
	}
	clearAccess := memory.ClearAccess{Principal: access.Principal, TargetScopes: []memory.MemoryScope{scope}, Capabilities: []string{"memory.clear"}}
	first, err := service.DeleteScope(ctx, clearAccess, scope, "clear-key-a")
	if err != nil {
		t.Fatal(err)
	}
	coalesced, err := service.DeleteScope(ctx, clearAccess, scope, "clear-key-b")
	if err != nil {
		t.Fatal(err)
	}
	if coalesced.ID != first.ID {
		t.Fatalf("overlapping clears did not coalesce: %s != %s", coalesced.ID, first.ID)
	}
	index, err := OpenIndex(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	worker, err := memory.NewFactWorker(memory.FactWorkerConfig{Store: store, Index: index, Embedders: providerEmbedderResolver{}, BatchSize: 20, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Save(ctx, write, "after-clear", memory.FactInput{Content: "new fact"}); err != nil {
		t.Fatal(err)
	}
	replayed, err := service.DeleteScope(ctx, clearAccess, scope, "clear-key-b")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != first.ID || replayed.State != "complete" {
		t.Fatalf("retry of coalesced key did not return the original completed operation: %+v", replayed)
	}
	page, err := service.List(ctx, access, memory.FactFilter{}, memory.PageRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Content != "new fact" {
		t.Fatalf("retry of coalesced clear deleted post-clear facts: %+v", page.Items)
	}
}

func TestPostgresConcurrentRevisionAndBodyFreeChanges(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL lifecycle tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_cas_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		store.Close()
	}()
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-cas", OwnerKind: memory.OwnerAgent, OwnerID: "user", AgentID: "agent-cas"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: "user", AgentID: scope.AgentID}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	write := memory.WriteScope{Access: access, Target: scope}
	receipt, err := service.Save(ctx, write, "cas-create", memory.FactInput{Content: "old secret body"})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, content := range []string{"new-a", "new-b"} {
		content := content
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := service.Update(ctx, write, receipt.FactID, receipt.Revision, memory.FactPatch{Content: &content})
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for e := range errs {
		if e == nil {
			success++
		} else if errors.Is(e, memory.ErrRevisionConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("CAS winners success=%d conflict=%d", success, conflict)
	}
	page, err := service.ListChanges(ctx, access, receipt.FactID, memory.PageRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) < 2 {
		t.Fatalf("change chain incomplete: %+v", page)
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "old secret body") || strings.Contains(string(raw), "new-a") || strings.Contains(string(raw), "new-b") {
		t.Fatalf("change chain leaked historical body: %s", raw)
	}
}

type providerEmbedderResolver struct{}

func (providerEmbedderResolver) Resolve(context.Context, memory.EmbeddingProfileID) (memory.Embedder, error) {
	return providerEmbedder{}, nil
}

type providerEmbedder struct{}

func (providerEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0}, nil
}
func (providerEmbedder) Profile() memory.EmbeddingProfile {
	return memory.EmbeddingProfile{ID: "default", Dimension: 2}
}

type rebuildEmbedderResolver struct{}

func (rebuildEmbedderResolver) Resolve(_ context.Context, id memory.EmbeddingProfileID) (memory.Embedder, error) {
	return rebuildEmbedder{profile: memory.EmbeddingProfile{ID: id, Dimension: 2}}, nil
}

type rebuildEmbedder struct{ profile memory.EmbeddingProfile }

func (e rebuildEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{0, 1}, nil
}
func (e rebuildEmbedder) Profile() memory.EmbeddingProfile { return e.profile }

func TestPostgresOnlineRebuildSwitchesGeneration(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL rebuild tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_rebuild_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		store.Close()
	}()
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-rebuild", OwnerKind: memory.OwnerUser, OwnerID: "user-rebuild"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: "agent-rebuild"}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	receipt, err := service.Save(ctx, memory.WriteScope{Access: access, Target: scope}, "rebuild-save", memory.FactInput{Content: "likes tea"})
	if err != nil {
		t.Fatal(err)
	}
	index, err := OpenIndex(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	worker, err := memory.NewFactWorker(memory.FactWorkerConfig{Store: store, Index: index, Embedders: rebuildEmbedderResolver{}, BatchSize: 20, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	maintenance := memory.MaintenanceAccess{TenantID: scope.TenantID, OperatorID: "operator", Capabilities: []string{"memory.rebuild"}}
	rebuildID, err := worker.StartRebuild(ctx, maintenance, memory.EmbeddingProfile{ID: "new-profile", Dimension: 2, DistanceMetric: "cosine"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = worker.StartRebuild(ctx, maintenance, memory.EmbeddingProfile{ID: "overlapping-profile", Dimension: 2, DistanceMetric: "cosine"}); !errors.Is(err, memory.ErrCapacityExceeded) {
		t.Fatalf("overlapping rebuild allocated a duplicate generation: %v", err)
	}
	if err = worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := store.ActiveIndexProfile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if active.Profile.ID != "new-profile" || active.Generation != 2 {
		t.Fatalf("generation was not switched: %+v", active)
	}
	page, err := index.Search(ctx, memory.IndexQuery{Scopes: []memory.MemoryScope{scope}, ProfileID: active.Profile.ID, Generation: active.Generation, Vector: []float32{0, 1}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].FactID != receipt.FactID {
		t.Fatalf("rebuilt generation missing fact: %+v", page)
	}
	status, err := worker.RebuildStatus(ctx, maintenance, rebuildID)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "active" {
		t.Fatalf("rebuild did not reach active state: %+v", status)
	}
	if err = store.AdvanceRebuild(ctx, rebuildID, memory.RebuildProgress{Cursor: "", Done: false}); !errors.Is(err, memory.ErrRevisionConflict) {
		t.Fatalf("stale worker revived a completed rebuild: %v", err)
	}
}

func TestPostgresRebuildClaimFencesExpiredWorker(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL rebuild lease tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_rebuild_lease_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		store.Close()
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
		store.Close()
	})
	profile := memory.EmbeddingProfile{ID: "leased-profile", Dimension: 2, DistanceMetric: "cosine"}
	rebuild, err := store.CreateRebuild(ctx, memory.MaintenancePrincipal{TenantID: "tenant-rebuild-lease", OperatorID: "operator"}, profile)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ClaimRebuilds(ctx, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim failed: %+v %v", first, err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE `+store.table("rebuilds")+` SET lease_until='epoch' WHERE id=$1`, string(rebuild.ID)); err != nil {
		t.Fatal(err)
	}
	second, err := store.ClaimRebuilds(ctx, 1)
	if err != nil || len(second) != 1 {
		t.Fatalf("replacement claim failed: %+v %v", second, err)
	}
	if second[0].LeaseToken <= first[0].LeaseToken {
		t.Fatalf("claim token did not advance: first=%d second=%d", first[0].LeaseToken, second[0].LeaseToken)
	}
	if err = store.AdvanceRebuild(ctx, rebuild.ID, memory.RebuildProgress{LeaseToken: first[0].LeaseToken, Cursor: "old-worker", CaughtUp: rebuild.Cutoff}); !errors.Is(err, memory.ErrRevisionConflict) {
		t.Fatalf("expired worker advanced the rebuild: %v", err)
	}
	status, err := store.GetRebuild(ctx, memory.MaintenanceAccess{TenantID: "tenant-rebuild-lease", OperatorID: "operator", Capabilities: []string{"memory.rebuild"}}, rebuild.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Cursor != "" || status.LeaseToken != second[0].LeaseToken {
		t.Fatalf("stale worker changed fenced progress: %+v", status)
	}
}

type blockingRebuildResolver struct {
	started, release chan struct{}
	once             sync.Once
}

func (r *blockingRebuildResolver) Resolve(_ context.Context, id memory.EmbeddingProfileID) (memory.Embedder, error) {
	if id == "new-profile" {
		return blockingRebuildEmbedder{resolver: r}, nil
	}
	return providerEmbedder{}, nil
}

type blockingRebuildEmbedder struct{ resolver *blockingRebuildResolver }

func (e blockingRebuildEmbedder) Profile() memory.EmbeddingProfile {
	return memory.EmbeddingProfile{ID: "new-profile", Dimension: 2}
}
func (e blockingRebuildEmbedder) Embed(ctx context.Context, _ string) ([]float32, error) {
	e.resolver.once.Do(func() {
		close(e.resolver.started)
		select {
		case <-e.resolver.release:
		case <-ctx.Done():
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []float32{0, 1}, nil
}

func TestPostgresOnlineRebuildCatchesConcurrentWrite(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL rebuild concurrency tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_rebuild_race_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		store.Close()
	}()
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-rebuild-race", OwnerKind: memory.OwnerUser, OwnerID: "user-rebuild-race"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: "agent-rebuild-race"}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	write := memory.WriteScope{Access: access, Target: scope}
	first, err := service.Save(ctx, write, "before-rebuild", memory.FactInput{Content: "preexisting fact"})
	if err != nil {
		t.Fatal(err)
	}
	index, err := OpenIndex(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	resolver := &blockingRebuildResolver{started: make(chan struct{}), release: make(chan struct{})}
	worker, err := memory.NewFactWorker(memory.FactWorkerConfig{Store: store, Index: index, Embedders: resolver, BatchSize: 1, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	maintenance := memory.MaintenanceAccess{TenantID: scope.TenantID, OperatorID: "operator", Capabilities: []string{"memory.rebuild"}}
	if _, err = worker.StartRebuild(ctx, maintenance, memory.EmbeddingProfile{ID: "new-profile", Dimension: 2, DistanceMetric: "cosine"}); err != nil {
		t.Fatal(err)
	}
	runResult := make(chan error, 1)
	go func() { runResult <- worker.Run(ctx) }()
	select {
	case <-resolver.started:
	case <-time.After(5 * time.Second):
		t.Fatal("rebuild did not begin embedding the existing fact")
	}
	second, err := service.Save(ctx, write, "during-rebuild", memory.FactInput{Content: "concurrent fact"})
	if err != nil {
		close(resolver.release)
		t.Fatal(err)
	}
	close(resolver.release)
	if err = <-runResult; err != nil {
		t.Fatal(err)
	}
	if err = worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := store.ActiveIndexProfile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if active.Profile.ID != "new-profile" || active.Generation != 2 {
		t.Fatalf("concurrent write prevented generation catch-up: %+v", active)
	}
	page, err := index.Search(ctx, memory.IndexQuery{Scopes: []memory.MemoryScope{scope}, ProfileID: active.Profile.ID, Generation: active.Generation, Vector: []float32{0, 1}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	got := map[memory.FactID]bool{}
	for _, item := range page.Items {
		got[item.FactID] = true
	}
	if len(got) != 2 || !got[first.FactID] || !got[second.FactID] {
		t.Fatalf("new generation omitted cutoff/concurrent facts: %+v", page)
	}
}

func TestPostgresListCursorPersistsAndBindsFilters(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL cursor tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_cursor_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
		store.Close()
	}()
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-cursor", OwnerKind: memory.OwnerUser, OwnerID: "user-cursor"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: "agent-cursor"}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	write := memory.WriteScope{Access: access, Target: scope}
	filter := memory.FactFilter{Scopes: []memory.MemoryScope{scope}, Tags: []string{"wanted"}, SourceIDs: []string{"doc-1"}}
	for n := 0; n < 4; n++ {
		tags, sourceID := []string{"wanted"}, "doc-1"
		if n == 3 {
			tags, sourceID = []string{"other"}, "doc-2"
		}
		_, err = service.Save(ctx, write, memory.IdempotencyKey(fmt.Sprintf("cursor-%d", n)), memory.FactInput{Content: fmt.Sprintf("fact-%d", n), Metadata: map[string]any{"tags": tags}, Source: memory.FactSource{Kind: "document", SourceID: sourceID}})
		if err != nil {
			t.Fatal(err)
		}
		if n < 3 {
			time.Sleep(time.Millisecond)
		}
	}
	first, err := service.List(ctx, access, filter, memory.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("expected first filtered page with continuation: %+v", first)
	}
	if _, err = service.List(ctx, access, memory.FactFilter{Scopes: filter.Scopes, Tags: []string{"other"}, SourceIDs: filter.SourceIDs}, memory.PageRequest{Limit: 1, Cursor: first.NextCursor}); !errors.Is(err, memory.ErrInvalidCursor) {
		t.Fatalf("cursor reused with different filter: %v", err)
	}
	if _, err = service.List(ctx, access, filter, memory.PageRequest{Limit: 1, Cursor: first.NextCursor + "tampered"}); !errors.Is(err, memory.ErrInvalidCursor) {
		t.Fatalf("tampered cursor accepted: %v", err)
	}
	store.Close()
	store, err = Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	service, err = newService(store)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[memory.FactID]bool{first.Items[0].ID: true}
	listedIDs := []memory.FactID{first.Items[0].ID}
	cursor := first.NextCursor
	for cursor != "" {
		page, e := service.List(ctx, access, filter, memory.PageRequest{Limit: 1, Cursor: cursor})
		if e != nil {
			t.Fatal(e)
		}
		for _, fact := range page.Items {
			if fact.Metadata["tags"] == nil || fact.Source.SourceID != "doc-1" {
				t.Fatalf("filter leaked unrelated fact: %+v", fact)
			}
			if seen[fact.ID] {
				t.Fatalf("duplicate page member %s", fact.ID)
			}
			seen[fact.ID] = true
			listedIDs = append(listedIDs, fact.ID)
		}
		cursor = page.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("cursor lost filtered members across restart: %d", len(seen))
	}
	current, err := service.Get(ctx, access, listedIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"updated-1", "updated-2"} {
		current, err = service.Update(ctx, write, current.ID, current.Revision, memory.FactPatch{Content: &content})
		if err != nil {
			t.Fatal(err)
		}
	}
	changePage, err := service.ListChanges(ctx, access, current.ID, memory.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(changePage.Items) != 1 || changePage.NextCursor == "" {
		t.Fatalf("expected first change page and cursor: %+v", changePage)
	}
	if _, err = service.ListChanges(ctx, access, listedIDs[1], memory.PageRequest{Limit: 1, Cursor: changePage.NextCursor}); !errors.Is(err, memory.ErrInvalidCursor) {
		t.Fatalf("change cursor crossed fact boundary: %v", err)
	}
	changeCount := len(changePage.Items)
	for cursor := changePage.NextCursor; cursor != ""; {
		page, e := service.ListChanges(ctx, access, current.ID, memory.PageRequest{Limit: 1, Cursor: cursor})
		if e != nil {
			t.Fatal(e)
		}
		changeCount += len(page.Items)
		cursor = page.NextCursor
	}
	if changeCount != 3 {
		t.Fatalf("change cursor did not return all revisions: %d", changeCount)
	}
}

func TestPostgresListCursorKeepsUnseenFactAfterUpdate(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL pagination tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_list_update_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		store.Close()
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
		store.Close()
	})
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-list-update", OwnerKind: memory.OwnerUser, OwnerID: "user-list-update"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: "agent"}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	write := memory.WriteScope{Access: access, Target: scope}
	a, err := service.Save(ctx, write, "list-a", memory.FactInput{Content: "first"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := service.Save(ctx, write, "list-b", memory.FactInput{Content: "second"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.List(ctx, access, memory.FactFilter{}, memory.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor == "" || len(first.Items) != 1 {
		t.Fatalf("expected first page cursor: %+v", first)
	}
	var unseen memory.SaveResult
	if first.Items[0].ID == a.FactID {
		unseen = b
	} else if first.Items[0].ID == b.FactID {
		unseen = a
	} else {
		t.Fatalf("first page returned unknown fact: %+v", first.Items)
	}
	newContent := "updated unseen fact"
	updated, err := service.Update(ctx, write, unseen.FactID, unseen.Revision, memory.FactPatch{Content: &newContent})
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.List(ctx, access, memory.FactFilter{}, memory.PageRequest{Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].ID != updated.ID || next.Items[0].Content != "updated unseen fact" || next.Items[0].Revision != updated.Revision {
		t.Fatalf("unseen member was lost or stale after in-place update: %+v", next.Items)
	}
}

func TestPostgresOutboxFailureBackoffStopsAndCanRetry(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL retry tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_retry_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		store.Close()
	}()
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-retry", OwnerKind: memory.OwnerUser, OwnerID: "user-retry"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: "agent-retry"}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	if _, err = service.Save(ctx, memory.WriteScope{Access: access, Target: scope}, "retry-save", memory.FactInput{Content: "never in diagnostics"}); err != nil {
		t.Fatal(err)
	}
	var last memory.MutationID
	for n := 0; n < 10; n++ {
		batch, e := store.ClaimIndexMutations(ctx, 1)
		if e != nil {
			t.Fatal(e)
		}
		if len(batch) != 1 {
			t.Fatalf("attempt %d was not claimable: %d", n+1, len(batch))
		}
		last = batch[0].ID
		if e = store.AckIndexMutation(ctx, batch[0], memory.MutationFailed); e != nil {
			t.Fatal(e)
		}
		if n < 9 {
			if _, e = store.pool.Exec(ctx, `UPDATE `+store.table("outbox")+` SET next_retry=now() WHERE id=$1`, string(last)); e != nil {
				t.Fatal(e)
			}
		}
	}
	if batch, err := store.ClaimIndexMutations(ctx, 1); err != nil || len(batch) != 0 {
		t.Fatalf("terminal failure was reclaimed: batch=%d err=%v", len(batch), err)
	}
	maintenance := memory.MaintenanceAccess{TenantID: scope.TenantID, OperatorID: "operator", AuthorizedScopes: []memory.MemoryScope{scope}, Capabilities: []string{"memory.maintenance"}}
	status, err := store.ListMaintenance(ctx, maintenance, memory.MaintenanceFilter{}, memory.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Items) != 1 || status.Items[0].Attempts != 10 || status.Items[0].Diagnostic == "" {
		t.Fatalf("missing safe terminal diagnostics: %+v", status)
	}
	encoded, _ := json.Marshal(status.Items[0])
	if strings.Contains(string(encoded), "never in diagnostics") {
		t.Fatalf("fact body leaked into diagnostics: %s", encoded)
	}
	unauthorized := maintenance
	unauthorized.AuthorizedScopes = []memory.MemoryScope{{TenantID: scope.TenantID, OwnerKind: memory.OwnerUser, OwnerID: "someone-else"}}
	if err = store.RetryMutation(ctx, unauthorized, last); !errors.Is(err, memory.ErrAuthorization) {
		t.Fatalf("retry accepted a task outside authorized scopes: %v", err)
	}
	if err = store.RetryMutation(ctx, maintenance, last); err != nil {
		t.Fatal(err)
	}
	batch, err := store.ClaimIndexMutations(ctx, 1)
	if err != nil || len(batch) != 1 {
		t.Fatalf("explicit retry did not restore claim: batch=%d err=%v", len(batch), err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE `+store.table("outbox")+` SET lease_until=now()-interval '1 second' WHERE id=$1`, string(batch[0].ID)); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := store.ClaimIndexMutations(ctx, 1)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].ID != batch[0].ID {
		t.Fatalf("expired lease was not recovered after process restart: %+v err=%v", reclaimed, err)
	}
	for attempt := 2; attempt < 10; attempt++ {
		if _, err = store.pool.Exec(ctx, `UPDATE `+store.table("outbox")+` SET lease_until=now()-interval '1 second' WHERE id=$1`, string(last)); err != nil {
			t.Fatal(err)
		}
		reclaimed, err = store.ClaimIndexMutations(ctx, 1)
		if err != nil || len(reclaimed) != 1 {
			t.Fatalf("crash lease attempt %d was not recovered: %+v err=%v", attempt+1, reclaimed, err)
		}
	}
	if _, err = store.pool.Exec(ctx, `UPDATE `+store.table("outbox")+` SET lease_until=now()-interval '1 second' WHERE id=$1`, string(last)); err != nil {
		t.Fatal(err)
	}
	if reclaimed, err = store.ClaimIndexMutations(ctx, 1); err != nil || len(reclaimed) != 0 {
		t.Fatalf("repeated crash beyond retry limit was not terminated: %+v err=%v", reclaimed, err)
	}
	status, err = store.ListMaintenance(ctx, maintenance, memory.MaintenanceFilter{}, memory.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	foundExhausted := false
	for _, item := range status.Items {
		if item.MutationID == last {
			foundExhausted = item.Attempts == 10 && item.Diagnostic == "index_attempts_exhausted"
		}
	}
	if !foundExhausted {
		t.Fatalf("exhausted lease lost terminal state: %+v", status)
	}
}

func TestPostgresMaintenanceCursorBindsScopeAndState(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL maintenance tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_maint_page_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		store.Close()
	}()
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-maint-page", OwnerKind: memory.OwnerAgent, OwnerID: "user", AgentID: "agent-maint-page"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: "user", AgentID: scope.AgentID}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	for n := 0; n < 3; n++ {
		if _, err = service.Save(ctx, memory.WriteScope{Access: access, Target: scope}, memory.IdempotencyKey(fmt.Sprintf("maint-%d", n)), memory.FactInput{Content: fmt.Sprintf("maint fact %d", n)}); err != nil {
			t.Fatal(err)
		}
	}
	maintenance := memory.MaintenanceAccess{TenantID: scope.TenantID, OperatorID: "operator", AuthorizedScopes: []memory.MemoryScope{scope}, Capabilities: []string{"memory.maintenance"}}
	filter := memory.MaintenanceFilter{Scopes: []memory.MemoryScope{scope}, States: []string{"pending"}}
	first, err := store.ListMaintenance(ctx, maintenance, filter, memory.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("expected maintenance continuation cursor: %+v", first)
	}
	if _, err = store.ListMaintenance(ctx, maintenance, memory.MaintenanceFilter{Scopes: filter.Scopes, States: []string{"failed"}}, memory.PageRequest{Limit: 1, Cursor: first.NextCursor}); !errors.Is(err, memory.ErrInvalidCursor) {
		t.Fatalf("maintenance cursor reused with different state filter: %v", err)
	}
	seen := map[memory.MutationID]bool{first.Items[0].MutationID: true}
	cursor := first.NextCursor
	for cursor != "" {
		page, e := store.ListMaintenance(ctx, maintenance, filter, memory.PageRequest{Limit: 1, Cursor: cursor})
		if e != nil {
			t.Fatal(e)
		}
		for _, item := range page.Items {
			if seen[item.MutationID] {
				t.Fatalf("maintenance cursor duplicated %s", item.MutationID)
			}
			if item.Scope != scope {
				t.Fatalf("maintenance scope leaked: %+v", item.Scope)
			}
			seen[item.MutationID] = true
		}
		cursor = page.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("maintenance pages returned %d tasks", len(seen))
	}
	unauthorized := memory.MaintenanceFilter{Scopes: []memory.MemoryScope{{TenantID: scope.TenantID, OwnerKind: memory.OwnerAgent, OwnerID: "other", AgentID: "other"}}}
	if _, err = store.ListMaintenance(ctx, maintenance, unauthorized, memory.PageRequest{Limit: 1}); !errors.Is(err, memory.ErrAuthorization) {
		t.Fatalf("maintenance list accepted unauthorized scope: %v", err)
	}
}

func TestPostgresOutboxBackpressureIsAtomicAndReservesDeleteCapacity(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL backpressure tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_backpressure_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		store.Close()
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
		store.Close()
	})
	store.maxPendingPerTenant = 1
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-backpressure", OwnerKind: memory.OwnerAgent, OwnerID: "user", AgentID: "agent-backpressure"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: "user", AgentID: scope.AgentID}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	write := memory.WriteScope{Access: access, Target: scope}
	first, err := service.Save(ctx, write, "first-at-capacity", memory.FactInput{Content: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Save(ctx, write, "rejected-at-capacity", memory.FactInput{Content: "must roll back"}); !errors.Is(err, memory.ErrCapacityExceeded) {
		t.Fatalf("outbox capacity was not enforced: %v", err)
	}
	var facts, operations, pending int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM `+store.table("facts")+` WHERE tenant_id=$1`, scope.TenantID).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM `+store.table("operations")+` WHERE tenant_id=$1`, scope.TenantID).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM `+store.table("outbox")+` WHERE tenant_id=$1 AND state IN('pending','leased','failed')`, scope.TenantID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if facts != 1 || operations != 1 || pending != 1 {
		t.Fatalf("rejected Save was partially committed: facts=%d operations=%d pending=%d", facts, operations, pending)
	}
	if _, err = service.Delete(ctx, write, first.FactID, first.Revision); err != nil {
		t.Fatalf("Delete did not use reserved cleanup capacity: %v", err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM `+store.table("outbox")+` WHERE tenant_id=$1 AND state IN('pending','leased','failed')`, scope.TenantID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 2 {
		t.Fatalf("expected both original upsert and reserved tombstone, got %d pending rows", pending)
	}
	index, err := OpenIndex(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	worker, err := memory.NewFactWorker(memory.FactWorkerConfig{Store: store, Index: index, Embedders: providerEmbedderResolver{}, BatchSize: 10, Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Save(ctx, write, "recovered-capacity", memory.FactInput{Content: "accepted after drain"}); err != nil {
		t.Fatalf("capacity did not recover after Worker drained the outbox: %v", err)
	}
}

func TestPostgresOutboxClaimsAreDisjointAcrossWorkers(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL worker concurrency tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_claims_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		store.Close()
	}()
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-claims", OwnerKind: memory.OwnerUser, OwnerID: "user-claims"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: "agent-claims"}, ReadScopes: []memory.MemoryScope{scope}, WriteScopes: []memory.MemoryScope{scope}}
	for n := 0; n < 20; n++ {
		if _, err = service.Save(ctx, memory.WriteScope{Access: access, Target: scope}, memory.IdempotencyKey(fmt.Sprintf("claim-%d", n)), memory.FactInput{Content: fmt.Sprintf("claim fact %d", n)}); err != nil {
			t.Fatal(err)
		}
	}
	type claimResult struct {
		items []memory.IndexMutation
		err   error
	}
	results := make(chan claimResult, 2)
	for n := 0; n < 2; n++ {
		go func() { items, e := store.ClaimIndexMutations(ctx, 10); results <- claimResult{items, e} }()
	}
	seen := map[memory.MutationID]bool{}
	total := 0
	for n := 0; n < 2; n++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		total += len(result.items)
		for _, item := range result.items {
			if seen[item.ID] {
				t.Fatalf("two workers claimed mutation %s", item.ID)
			}
			seen[item.ID] = true
		}
	}
	if total != 20 {
		t.Fatalf("workers claimed %d of 20 pending mutations", total)
	}
}

func TestPostgresExpiredWorkerCannotOverwriteNewerAcknowledgement(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL lease fencing tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_lease_fence_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		store.Close()
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		admin.Close()
		store.Close()
	})
	service, err := newService(store)
	if err != nil {
		t.Fatal(err)
	}
	scope := memory.MemoryScope{TenantID: "tenant-lease-fence", OwnerKind: memory.OwnerUser, OwnerID: "user-lease-fence"}
	access := memory.MemoryAccess{Principal: memory.Principal{TenantID: scope.TenantID, UserID: scope.OwnerID, AgentID: "agent"}, WriteScopes: []memory.MemoryScope{scope}}
	if _, err = service.Save(ctx, memory.WriteScope{Access: access, Target: scope}, "lease-fence", memory.FactInput{Content: "fact"}); err != nil {
		t.Fatal(err)
	}
	first, err := store.ClaimIndexMutations(ctx, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first lease failed: batch=%+v err=%v", first, err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE `+store.table("outbox")+` SET lease_until='epoch' WHERE id=$1`, string(first[0].ID)); err != nil {
		t.Fatal(err)
	}
	second, err := store.ClaimIndexMutations(ctx, 1)
	if err != nil || len(second) != 1 {
		t.Fatalf("replacement lease failed: batch=%+v err=%v", second, err)
	}
	if second[0].LeaseToken == first[0].LeaseToken {
		t.Fatalf("lease token did not advance: %d", second[0].LeaseToken)
	}
	if err = store.AckIndexMutation(ctx, second[0], memory.MutationApplied); err != nil {
		t.Fatalf("current worker acknowledgement failed: %v", err)
	}
	if err = store.AckIndexMutation(ctx, first[0], memory.MutationFailed); !errors.Is(err, memory.ErrRevisionConflict) {
		t.Fatalf("stale worker overwrote terminal acknowledgement: %v", err)
	}
	var state string
	if err = store.pool.QueryRow(ctx, `SELECT state FROM `+store.table("outbox")+` WHERE id=$1`, string(first[0].ID)).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != string(memory.MutationApplied) {
		t.Fatalf("stale failure changed applied mutation state to %q", state)
	}
}

func TestPostgresVectorIndexPersistsAndFiltersScope(t *testing.T) {
	dsn := os.Getenv("GOAGENT_PGSTORE_DSN")
	if dsn == "" {
		t.Fatal("GOAGENT_PGSTORE_DSN is required; PostgreSQL persistence tests must not skip")
	}
	ctx := context.Background()
	schema, err := randomID("fact_vec_")
	if err != nil {
		t.Fatal(err)
	}
	schema = schema[:min(len(schema), 60)]
	store, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		store.pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		store.Close()
	}()
	index, err := OpenIndex(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	scope := memory.MemoryScope{TenantID: "tenant-vector", OwnerKind: memory.OwnerUser, OwnerID: "user-vector"}
	mutation := memory.IndexMutation{ID: "m1", FactID: "f1", Scope: scope, Revision: 1, MutationVersion: 1, ProfileID: "default", Generation: 1, Action: memory.MutationUpsert, Vector: []float32{1, 0}}
	if err = index.ApplyMutation(ctx, mutation); err != nil {
		t.Fatal(err)
	}
	query, err := index.Search(ctx, memory.IndexQuery{Scopes: []memory.MemoryScope{scope}, ProfileID: "default", Generation: 1, Vector: []float32{1, 0}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Items) != 1 || query.Items[0].FactID != "f1" || query.Items[0].Score < 0.99 {
		t.Fatalf("unexpected vector search: %+v", query)
	}
	other := memory.MemoryScope{TenantID: "other-tenant", OwnerKind: memory.OwnerUser, OwnerID: "user-vector"}
	query, err = index.Search(ctx, memory.IndexQuery{Scopes: []memory.MemoryScope{other}, ProfileID: "default", Generation: 1, Vector: []float32{1, 0}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Items) != 0 {
		t.Fatalf("cross-scope vector candidate leaked: %+v", query)
	}
	if err = index.ApplyMutation(ctx, memory.IndexMutation{FactID: "f1", Scope: scope, Revision: 2, MutationVersion: 2, Action: memory.MutationTombstone}); err != nil {
		t.Fatal(err)
	}
	if err = index.ApplyMutation(ctx, mutation); err != nil {
		t.Fatal(err)
	}
	query, err = index.Search(ctx, memory.IndexQuery{Scopes: []memory.MemoryScope{scope}, ProfileID: "default", Generation: 1, Vector: []float32{1, 0}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Items) != 0 {
		t.Fatalf("tombstoned vector remained searchable: %+v", query)
	}
	if err = index.ApplyMutation(ctx, memory.IndexMutation{FactID: "f2", Scope: scope, Revision: 2, MutationVersion: 2, Action: memory.MutationTombstone}); err != nil {
		t.Fatal(err)
	}
	if err = index.ApplyMutation(ctx, memory.IndexMutation{FactID: "f2", Scope: scope, Revision: 1, MutationVersion: 1, ProfileID: "default", Generation: 1, Action: memory.MutationUpsert, Vector: []float32{1, 0}}); err != nil {
		t.Fatal(err)
	}
	query, err = index.Search(ctx, memory.IndexQuery{Scopes: []memory.MemoryScope{scope}, ProfileID: "default", Generation: 1, Vector: []float32{1, 0}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Items) != 0 {
		t.Fatalf("late stale upsert resurrected a tombstone: %+v", query)
	}
}

func newService(store *Store) (*memory.DefaultMemoryService, error) {
	limits := memory.DefaultFactLimits()
	return memory.NewDefaultMemoryService(memory.MemoryServiceConfig{Store: store, Capabilities: store.Capabilities(), Limits: limits, Now: time.Now})
}
func is(err, target error) bool { return errors.Is(err, target) }
