// Package postgres is an optional PostgreSQL/pgvector provider for GoAgent factual memory.
package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kart-io/goagent/memory"
)

var schemaPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

// Store implements memory.FactStore using PostgreSQL transactions and durable tables.
type Store struct {
	pool                *pgxpool.Pool
	schema              string
	maxPendingPerTenant int
}

// Open creates a connection pool. It does not run migrations.
func Open(ctx context.Context, dsn, schema string) (*Store, error) {
	if !schemaPattern.MatchString(schema) {
		return nil, fmt.Errorf("invalid PostgreSQL schema name")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, schema: schema, maxPendingPerTenant: 100_000}, nil
}

// Close releases pooled connections.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// Migrate explicitly creates the provider schema and pgvector extension.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+s.qschema()); err != nil {
		return err
	}
	statements := []string{
		`CREATE SEQUENCE IF NOT EXISTS ` + s.qschema() + `."store_sequence"`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("cursor_signing_key") + ` (singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),secret bytea NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("facts") + ` (id text PRIMARY KEY,tenant_id text NOT NULL,owner_kind text NOT NULL,owner_id text NOT NULL,agent_id text NOT NULL DEFAULT '',content text NOT NULL,metadata jsonb NOT NULL,source jsonb NOT NULL,status text NOT NULL,revision bigint NOT NULL,mutation_version bigint NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,has_expiry boolean NOT NULL,supersedes_id text NOT NULL DEFAULT '',recall_class text NOT NULL,priority integer NOT NULL,index_state text NOT NULL,clear_operation text NOT NULL DEFAULT '',store_sequence bigint NOT NULL DEFAULT 0,created_sequence bigint NOT NULL DEFAULT 0)`,
		`ALTER TABLE ` + s.table("facts") + ` ADD COLUMN IF NOT EXISTS created_sequence bigint NOT NULL DEFAULT 0`,
		`UPDATE ` + s.table("facts") + ` SET created_sequence=store_sequence WHERE created_sequence=0 AND store_sequence<>0`,
		`CREATE INDEX IF NOT EXISTS facts_scope_status_idx ON ` + s.table("facts") + ` (tenant_id,owner_kind,owner_id,agent_id,status,updated_at,id)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("operations") + ` (tenant_id text NOT NULL,scope_key text NOT NULL,action text NOT NULL,idem_key text NOT NULL,digest text NOT NULL,operation_id text NOT NULL,fact_id text NOT NULL,revision bigint NOT NULL,index_state text NOT NULL,lifecycle text NOT NULL,expires_at timestamptz NOT NULL,expired boolean NOT NULL DEFAULT false,PRIMARY KEY(tenant_id,scope_key,action,idem_key))`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("changes") + ` (seq bigserial PRIMARY KEY,fact_id text NOT NULL,tenant_id text NOT NULL,owner_kind text NOT NULL,owner_id text NOT NULL,agent_id text NOT NULL DEFAULT '',revision bigint NOT NULL,action text NOT NULL,actor jsonb NOT NULL,changed_at timestamptz NOT NULL,source jsonb NOT NULL,supersedes_id text NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS changes_fact_seq_idx ON ` + s.table("changes") + ` (tenant_id,owner_kind,owner_id,agent_id,fact_id,seq)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("outbox") + ` (id text PRIMARY KEY,fact_id text NOT NULL,tenant_id text NOT NULL,owner_kind text NOT NULL,owner_id text NOT NULL,agent_id text NOT NULL DEFAULT '',revision bigint NOT NULL,mutation_version bigint NOT NULL,store_sequence bigint NOT NULL,profile_id text NOT NULL DEFAULT '',generation bigint NOT NULL DEFAULT 0,action text NOT NULL,state text NOT NULL DEFAULT 'pending',attempts integer NOT NULL DEFAULT 0,lease_token bigint NOT NULL DEFAULT 0,next_retry timestamptz NOT NULL DEFAULT now(),lease_until timestamptz NOT NULL DEFAULT 'epoch',diagnostic text NOT NULL DEFAULT '')`,
		`ALTER TABLE ` + s.table("outbox") + ` ADD COLUMN IF NOT EXISTS lease_token bigint NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS outbox_claim_idx ON ` + s.table("outbox") + ` (state,next_retry,lease_until,store_sequence)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("clears") + ` (id text PRIMARY KEY,tenant_id text NOT NULL,owner_kind text NOT NULL,owner_id text NOT NULL,agent_id text NOT NULL DEFAULT '',state text NOT NULL,cutoff bigint NOT NULL,cursor text NOT NULL DEFAULT '',created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("clear_keys") + ` (tenant_id text NOT NULL,scope_key text NOT NULL,idem_key text NOT NULL,clear_id text NOT NULL,PRIMARY KEY(tenant_id,scope_key,idem_key))`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("rebuilds") + ` (id text PRIMARY KEY,tenant_id text NOT NULL,operator_id text NOT NULL,profile jsonb NOT NULL,generation bigint NOT NULL,cutoff bigint NOT NULL,caught_up bigint NOT NULL DEFAULT 0,cursor text NOT NULL DEFAULT '',state text NOT NULL,diagnostic_code text NOT NULL DEFAULT '',created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,lease_token bigint NOT NULL DEFAULT 0,lease_until timestamptz NOT NULL DEFAULT 'epoch')`,
		`ALTER TABLE ` + s.table("rebuilds") + ` ADD COLUMN IF NOT EXISTS lease_token bigint NOT NULL DEFAULT 0`,
		`ALTER TABLE ` + s.table("rebuilds") + ` ADD COLUMN IF NOT EXISTS lease_until timestamptz NOT NULL DEFAULT 'epoch'`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("active_profile") + ` (singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),profile jsonb NOT NULL,generation bigint NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("vectors") + ` (fact_id text NOT NULL,tenant_id text NOT NULL,owner_kind text NOT NULL,owner_id text NOT NULL,agent_id text NOT NULL DEFAULT '',profile_id text NOT NULL,generation bigint NOT NULL,revision bigint NOT NULL,mutation_version bigint NOT NULL,embedding vector,active boolean NOT NULL,PRIMARY KEY(fact_id,profile_id,generation))`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("vector_watermarks") + ` (fact_id text NOT NULL,tenant_id text NOT NULL,owner_kind text NOT NULL,owner_id text NOT NULL,agent_id text NOT NULL DEFAULT '',mutation_version bigint NOT NULL,deleted boolean NOT NULL,PRIMARY KEY(fact_id,tenant_id,owner_kind,owner_id,agent_id))`,
		`CREATE INDEX IF NOT EXISTS vectors_scope_idx ON ` + s.table("vectors") + ` (tenant_id,owner_kind,owner_id,agent_id,profile_id,generation)`,
	}
	for _, statement := range statements {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("migrate factual-memory provider: %w", err)
		}
	}
	var cursorSecret []byte
	if err := s.pool.QueryRow(ctx, `SELECT secret FROM `+s.table("cursor_signing_key")+` WHERE singleton=true`).Scan(&cursorSecret); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		cursorSecret = make([]byte, 32)
		if _, err = rand.Read(cursorSecret); err != nil {
			return err
		}
		if _, err = s.pool.Exec(ctx, `INSERT INTO `+s.table("cursor_signing_key")+` (singleton,secret) VALUES(true,$1) ON CONFLICT(singleton) DO NOTHING`, cursorSecret); err != nil {
			return err
		}
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.table("active_profile")+` WHERE singleton=true)`).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		raw, _ := json.Marshal(memory.EmbeddingProfile{ID: "default", DistanceMetric: "cosine"})
		_, err = s.pool.Exec(ctx, `INSERT INTO `+s.table("active_profile")+` (singleton,profile,generation) VALUES(true,$1,1)`, raw)
		if err != nil {
			return err
		}
	}
	return nil
}

// Capabilities reports the provider guarantees and resource bounds implemented by this adapter.
func (s *Store) Capabilities() memory.ProviderCapabilities {
	return memory.ProviderCapabilities{MaxFactBytes: 1024 * 1024, MaxMetadataBytes: 256 * 1024, MaxPageSize: 1000, MaxWorkerConcurrency: 32, MaxPendingPerTenant: 100000, DurableOutbox: true, AtomicFactOutbox: true, ServerSideScopeFilter: true, DurableFacts: true, ChangeLog: true, ResumableClear: true, OnlineRebuild: true, IdempotencyTombstones: true, ReservedCleanupCapacity: true}
}

func (s *Store) qschema() string          { return `"` + s.schema + `"` }
func (s *Store) table(name string) string { return s.qschema() + `."` + name + `"` }
func (s *Store) scopeKey(scope memory.MemoryScope) string {
	data, _ := json.Marshal(scope)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func idempotencyDigest(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
func marshal(v any) ([]byte, error) { return json.Marshal(v) }
func unmarshal(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}
func scopeArgs(scope memory.MemoryScope) []any {
	return []any{scope.TenantID, string(scope.OwnerKind), scope.OwnerID, scope.AgentID}
}
func statusForFact(status string) memory.FactStatus { return memory.FactStatus(status) }
func dbError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return &memory.MemoryError{Kind: memory.ErrGone, Op: "事实不存在"}
	}
	return err
}
func normalizeTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return t.UTC()
}
func isExpired(now, expires time.Time, has bool) bool { return has && !now.Before(expires) }
func escapeLike(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `%`, `\%`)
}
