package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kart-io/goagent/memory"
	pgvector "github.com/pgvector/pgvector-go"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
)

// Index is a durable pgvector-backed MemoryIndex.
type Index struct {
	pool   *pgxpool.Pool
	schema string
}

// OpenIndex creates a pool and registers pgvector codecs on each connection.
// Call Store.Migrate before opening the index.
func OpenIndex(ctx context.Context, dsn, schema string) (*Index, error) {
	if !schemaPattern.MatchString(schema) {
		return nil, fmt.Errorf("invalid PostgreSQL schema name")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error { return pgxvector.RegisterTypes(ctx, conn) }
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Index{pool: pool, schema: schema}, nil
}

// Close releases the vector index pool.
func (i *Index) Close() {
	if i != nil && i.pool != nil {
		i.pool.Close()
	}
}
func (i *Index) table() string          { return `"` + i.schema + `"."vectors"` }
func (i *Index) watermarkTable() string { return `"` + i.schema + `"."vector_watermarks"` }

// ApplyMutation is idempotent and ignores stale MutationVersion values.
func (i *Index) ApplyMutation(ctx context.Context, m memory.IndexMutation) error {
	if m.Action != memory.MutationTombstone && (m.Action != memory.MutationUpsert || len(m.Vector) == 0) {
		return &memory.MemoryError{Kind: memory.ErrInvalidFact, Op: "invalid vector mutation"}
	}
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	deleted := m.Action == memory.MutationTombstone
	var watermark int64
	err = tx.QueryRow(ctx, `INSERT INTO `+i.watermarkTable()+` AS w (fact_id,tenant_id,owner_kind,owner_id,agent_id,mutation_version,deleted) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(fact_id,tenant_id,owner_kind,owner_id,agent_id) DO UPDATE SET mutation_version=EXCLUDED.mutation_version,deleted=EXCLUDED.deleted WHERE w.mutation_version<EXCLUDED.mutation_version OR (w.mutation_version=EXCLUDED.mutation_version AND w.deleted=false AND EXCLUDED.deleted=false) RETURNING mutation_version`, string(m.FactID), m.Scope.TenantID, string(m.Scope.OwnerKind), m.Scope.OwnerID, m.Scope.AgentID, int64(m.MutationVersion), deleted).Scan(&watermark)
	if err == pgx.ErrNoRows {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if deleted {
		if _, err = tx.Exec(ctx, `UPDATE `+i.table()+` SET active=false,mutation_version=$1,revision=$2 WHERE tenant_id=$3 AND owner_kind=$4 AND owner_id=$5 AND agent_id=$6 AND fact_id=$7 AND mutation_version<$1`, int64(m.MutationVersion), int64(m.Revision), m.Scope.TenantID, string(m.Scope.OwnerKind), m.Scope.OwnerID, m.Scope.AgentID, string(m.FactID)); err != nil {
			return err
		}
	} else {
		vector := pgvector.NewVector(m.Vector)
		_, err = tx.Exec(ctx, `INSERT INTO `+i.table()+` AS v (fact_id,tenant_id,owner_kind,owner_id,agent_id,profile_id,generation,revision,mutation_version,embedding,active) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,true) ON CONFLICT(fact_id,profile_id,generation) DO UPDATE SET revision=EXCLUDED.revision,mutation_version=EXCLUDED.mutation_version,embedding=EXCLUDED.embedding,active=true WHERE v.mutation_version<EXCLUDED.mutation_version`, string(m.FactID), m.Scope.TenantID, string(m.Scope.OwnerKind), m.Scope.OwnerID, m.Scope.AgentID, string(m.ProfileID), int64(m.Generation), int64(m.Revision), int64(m.MutationVersion), vector)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Search filters by exact authorized scopes, active profile and generation before ranking.
func (i *Index) Search(ctx context.Context, q memory.IndexQuery) (memory.IndexPage, error) {
	if len(q.Vector) == 0 || q.Limit < 1 || len(q.Scopes) == 0 {
		return memory.IndexPage{}, nil
	}
	where, args := scopeWhere(q.Scopes, 4)
	args = append([]any{pgvector.NewVector(q.Vector), string(q.ProfileID), int64(q.Generation)}, args...)
	limitPosition := len(args) + 1
	args = append(args, q.Limit)
	query := `SELECT fact_id,revision,profile_id,generation,GREATEST(0,LEAST(1,1-(embedding <=> $1)/2)) AS score FROM ` + i.table() + ` WHERE profile_id=$2 AND generation=$3 AND active=true AND embedding IS NOT NULL AND ` + where + ` ORDER BY embedding <=> $1,fact_id LIMIT $` + fmt.Sprint(limitPosition)
	rows, err := i.pool.Query(ctx, query, args...)
	if err != nil {
		return memory.IndexPage{}, err
	}
	defer rows.Close()
	var result memory.IndexPage
	for rows.Next() {
		var candidate memory.IndexCandidate
		var id, profile string
		var revision, generation int64
		if err = rows.Scan(&id, &revision, &profile, &generation, &candidate.Score); err != nil {
			return result, err
		}
		candidate.FactID = memory.FactID(id)
		candidate.Revision = memory.Revision(revision)
		candidate.ProfileID = memory.EmbeddingProfileID(profile)
		candidate.Generation = memory.IndexGeneration(generation)
		result.Items = append(result.Items, candidate)
	}
	return result, rows.Err()
}

var _ memory.MemoryIndex = (*Index)(nil)
