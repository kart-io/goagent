package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kart-io/goagent/memory"
)

func randomID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

func (s *Store) ApplyFact(ctx context.Context, scope memory.WriteScope, cmd memory.FactMutation) (memory.FactMutationResult, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return memory.FactMutationResult{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockMemoryScope(ctx, tx, scope.Target, s.scopeKey(scope.Target)); err != nil {
		return memory.FactMutationResult{}, err
	}
	if cmd.IdempotencyKey != "" {
		keyHash := idempotencyDigest(string(cmd.IdempotencyKey))
		key := scope.Target.TenantID + "|" + s.scopeKey(scope.Target) + "|" + string(cmd.Action) + "|" + keyHash
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
			return memory.FactMutationResult{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE `+s.table("operations")+` SET expired=true,operation_id='',fact_id='',revision=0,index_state='',lifecycle='expired' WHERE tenant_id=$1 AND expires_at<=now() AND expired=false`, scope.Target.TenantID); err != nil {
			return memory.FactMutationResult{}, err
		}
		var storedDigest, opID, factID, indexState, lifecycle string
		var revision int64
		var expired bool
		err = tx.QueryRow(ctx, `SELECT o.digest,o.operation_id,o.fact_id,o.revision,o.index_state,COALESCE(CASE WHEN f.status='deleted' THEN 'deleted' WHEN f.status='superseded' THEN 'superseded' WHEN f.has_expiry AND f.expires_at<=now() THEN 'expired' ELSE f.status END,'expired'),(o.expired OR o.expires_at<=now()) FROM `+s.table("operations")+` o LEFT JOIN `+s.table("facts")+` f ON f.tenant_id=o.tenant_id AND f.id=o.fact_id WHERE o.tenant_id=$1 AND o.scope_key=$2 AND o.action=$3 AND o.idem_key=$4 FOR UPDATE OF o`, scope.Target.TenantID, s.scopeKey(scope.Target), string(cmd.Action), keyHash).Scan(&storedDigest, &opID, &factID, &revision, &indexState, &lifecycle, &expired)
		if err == nil {
			if expired {
				if _, e := tx.Exec(ctx, `UPDATE `+s.table("operations")+` SET expired=true,operation_id='',fact_id='',revision=0,index_state='',lifecycle='expired' WHERE tenant_id=$1 AND scope_key=$2 AND action=$3 AND idem_key=$4`, scope.Target.TenantID, s.scopeKey(scope.Target), string(cmd.Action), keyHash); e != nil {
					return memory.FactMutationResult{}, e
				}
				if e := tx.Commit(ctx); e != nil {
					return memory.FactMutationResult{}, e
				}
				return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrIdempotencyExpired}
			}
			if storedDigest != cmd.RequestDigest {
				return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrIdempotencyConflict}
			}
			if err = tx.Commit(ctx); err != nil {
				return memory.FactMutationResult{}, err
			}
			return memory.FactMutationResult{Receipt: memory.MutationReceipt{OperationID: opID, FactID: memory.FactID(factID), Revision: memory.Revision(revision), IndexState: memory.IndexState(indexState)}, Lifecycle: memory.FactStatus(lifecycle)}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return memory.FactMutationResult{}, err
		}
	}
	if (cmd.Action == memory.ChangeCreated || cmd.Action == memory.ChangeSuperseded) && !cmd.Input.ExpiresAt.IsZero() && !time.Now().UTC().Before(cmd.Input.ExpiresAt) {
		return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrInvalidFact, Op: "事实有效期必须晚于当前时间"}
	}
	if cmd.Action != memory.ChangeDeleted {
		var clearing bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.table("clears")+` WHERE tenant_id=$1 AND owner_kind=$2 AND owner_id=$3 AND agent_id=$4 AND state IN('pending','running'))`, scope.Target.TenantID, string(scope.Target.OwnerKind), scope.Target.OwnerID, scope.Target.AgentID).Scan(&clearing); err != nil {
			return memory.FactMutationResult{}, err
		}
		if clearing {
			return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrCapacityExceeded, Op: "作用域正在清理"}
		}
	}
	now := time.Now().UTC()
	var fact memory.Fact
	var old memory.Fact
	if cmd.Action == memory.ChangeCreated {
		id := cmd.NewFactID
		if id == "" {
			id = cmd.FactID
		}
		if id == "" {
			return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrInvalidFact}
		}
		class := cmd.Input.RecallClass
		if class == "" {
			class = memory.RecallNormal
		}
		metadata, _ := json.Marshal(cmd.Input.Metadata)
		source, _ := json.Marshal(cmd.Input.Source)
		hasExpiry := !cmd.Input.ExpiresAt.IsZero()
		_, err = tx.Exec(ctx, `INSERT INTO `+s.table("facts")+` (id,tenant_id,owner_kind,owner_id,agent_id,content,metadata,source,status,revision,mutation_version,created_at,updated_at,expires_at,has_expiry,recall_class,priority,index_state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'active',1,1,$9,$9,$10,$11,$12,$13,'pending')`, string(id), scope.Target.TenantID, string(scope.Target.OwnerKind), scope.Target.OwnerID, scope.Target.AgentID, cmd.Input.Content, metadata, source, now, normalizeTime(cmd.Input.ExpiresAt), hasExpiry, string(class), cmd.Input.Priority)
		if err != nil {
			return memory.FactMutationResult{}, err
		}
		fact = memory.Fact{ID: id, Scope: scope.Target, Content: cmd.Input.Content, Metadata: cmd.Input.Metadata, Source: cmd.Input.Source, Status: memory.FactActive, Revision: 1, MutationVersion: 1, CreatedAt: now, UpdatedAt: now, ExpiresAt: cmd.Input.ExpiresAt, RecallClass: class, Priority: cmd.Input.Priority, IndexState: memory.IndexPending}
	} else {
		old, err = s.getFactTx(ctx, tx, scope.Target.TenantID, cmd.FactID, true)
		if err != nil {
			return memory.FactMutationResult{}, err
		}
		if old.Scope != scope.Target {
			return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrAuthorization}
		}
		if old.Status != memory.FactActive || isExpired(now, old.ExpiresAt, !old.ExpiresAt.IsZero()) {
			return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrGone}
		}
		if old.Revision != cmd.ExpectedRevision {
			return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrRevisionConflict}
		}
		switch cmd.Action {
		case memory.ChangeUpdated:
			if cmd.Patch == nil {
				return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrInvalidFact}
			}
			fact = old
			patch := cmd.Patch
			if patch.Content != nil {
				fact.Content = *patch.Content
			}
			if patch.Metadata != nil {
				fact.Metadata = patch.Metadata
			}
			if patch.Source != nil {
				fact.Source = *patch.Source
			}
			if patch.ExpiresAt != nil {
				fact.ExpiresAt = *patch.ExpiresAt
			}
			if patch.RecallClass != nil {
				fact.RecallClass = *patch.RecallClass
			}
			if patch.Priority != nil {
				fact.Priority = *patch.Priority
			}
			fact.Revision++
			fact.MutationVersion++
			fact.UpdatedAt = now
			if err = s.updateFactTx(ctx, tx, fact, cmd.ExpectedRevision); err != nil {
				return memory.FactMutationResult{}, err
			}
		case memory.ChangeDeleted:
			fact = old
			fact.Status = memory.FactDeleted
			fact.Revision++
			fact.MutationVersion++
			fact.UpdatedAt = now
			if err = s.updateFactTx(ctx, tx, fact, cmd.ExpectedRevision); err != nil {
				return memory.FactMutationResult{}, err
			}
		case memory.ChangeSuperseded:
			fact = old
			fact.Status = memory.FactSuperseded
			fact.Revision++
			fact.MutationVersion++
			fact.UpdatedAt = now
			if err = s.updateFactTx(ctx, tx, fact, cmd.ExpectedRevision); err != nil {
				return memory.FactMutationResult{}, err
			}
			newID := cmd.NewFactID
			if newID == "" {
				return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrInvalidFact}
			}
			class := cmd.Input.RecallClass
			if class == "" {
				class = memory.RecallNormal
			}
			metadata, _ := json.Marshal(cmd.Input.Metadata)
			source, _ := json.Marshal(cmd.Input.Source)
			hasExpiry := !cmd.Input.ExpiresAt.IsZero()
			_, err = tx.Exec(ctx, `INSERT INTO `+s.table("facts")+` (id,tenant_id,owner_kind,owner_id,agent_id,content,metadata,source,status,revision,mutation_version,created_at,updated_at,expires_at,has_expiry,supersedes_id,recall_class,priority,index_state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'active',1,1,$9,$9,$10,$11,$12,$13,$14,'pending')`, string(newID), scope.Target.TenantID, string(scope.Target.OwnerKind), scope.Target.OwnerID, scope.Target.AgentID, cmd.Input.Content, metadata, source, now, normalizeTime(cmd.Input.ExpiresAt), hasExpiry, string(old.ID), string(class), cmd.Input.Priority)
			if err != nil {
				return memory.FactMutationResult{}, err
			}
			fact = memory.Fact{ID: newID, Scope: scope.Target, Content: cmd.Input.Content, Metadata: cmd.Input.Metadata, Source: cmd.Input.Source, Status: memory.FactActive, Revision: 1, MutationVersion: 1, CreatedAt: now, UpdatedAt: now, ExpiresAt: cmd.Input.ExpiresAt, SupersedesID: old.ID, RecallClass: class, Priority: cmd.Input.Priority, IndexState: memory.IndexPending}
		default:
			return memory.FactMutationResult{}, &memory.MemoryError{Kind: memory.ErrInvalidFact}
		}
	}
	var seq int64
	if err = tx.QueryRow(ctx, `SELECT nextval('`+s.schema+`.store_sequence')`).Scan(&seq); err != nil {
		return memory.FactMutationResult{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE `+s.table("facts")+` SET store_sequence=$1 WHERE tenant_id=$2 AND id=$3`, seq, fact.Scope.TenantID, string(fact.ID)); err != nil {
		return memory.FactMutationResult{}, err
	}
	if cmd.Action == memory.ChangeCreated || cmd.Action == memory.ChangeSuperseded {
		if _, err = tx.Exec(ctx, `UPDATE `+s.table("facts")+` SET created_sequence=$1 WHERE tenant_id=$2 AND id=$3`, seq, fact.Scope.TenantID, string(fact.ID)); err != nil {
			return memory.FactMutationResult{}, err
		}
	}
	if cmd.Action == memory.ChangeSuperseded {
		if _, err = tx.Exec(ctx, `UPDATE `+s.table("facts")+` SET store_sequence=$1 WHERE tenant_id=$2 AND id=$3`, seq, old.Scope.TenantID, string(old.ID)); err != nil {
			return memory.FactMutationResult{}, err
		}
	}
	if cmd.Action == memory.ChangeSuperseded {
		if err = s.insertChangeTx(ctx, tx, old, cmd.Action, cmd.Actor, now, fact.ID); err != nil {
			return memory.FactMutationResult{}, err
		}
		if err = s.insertOutboxTx(ctx, tx, old.ID, old.Scope, old.Revision+1, old.MutationVersion+1, memory.StoreSequence(seq), memory.MutationTombstone); err != nil {
			return memory.FactMutationResult{}, err
		}
	}
	if err = s.insertChangeTx(ctx, tx, fact, cmd.Action, cmd.Actor, now, fact.SupersedesID); err != nil {
		return memory.FactMutationResult{}, err
	}
	action := memory.MutationUpsert
	if fact.Status != memory.FactActive {
		action = memory.MutationTombstone
	}
	if err = s.insertOutboxTx(ctx, tx, fact.ID, fact.Scope, fact.Revision, fact.MutationVersion, memory.StoreSequence(seq), action); err != nil {
		return memory.FactMutationResult{}, err
	}
	opID, err := randomID("op_")
	if err != nil {
		return memory.FactMutationResult{}, err
	}
	receipt := memory.MutationReceipt{OperationID: opID, FactID: fact.ID, Revision: fact.Revision, IndexState: memory.IndexPending}
	if cmd.IdempotencyKey != "" {
		idempotencyExpiry := cmd.IdempotencyExpiresAt
		if idempotencyExpiry.IsZero() {
			idempotencyExpiry = now.Add(30 * 24 * time.Hour)
		}
		_, err = tx.Exec(ctx, `INSERT INTO `+s.table("operations")+` (tenant_id,scope_key,action,idem_key,digest,operation_id,fact_id,revision,index_state,lifecycle,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, scope.Target.TenantID, s.scopeKey(scope.Target), string(cmd.Action), idempotencyDigest(string(cmd.IdempotencyKey)), cmd.RequestDigest, opID, string(fact.ID), int64(fact.Revision), string(receipt.IndexState), string(fact.Status), idempotencyExpiry)
		if err != nil {
			return memory.FactMutationResult{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return memory.FactMutationResult{}, err
	}
	return memory.FactMutationResult{Receipt: receipt, Lifecycle: fact.Status}, nil
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Store) getFactTx(ctx context.Context, tx rowQuerier, tenant string, id memory.FactID, lock bool) (memory.Fact, error) {
	query := `SELECT id,tenant_id,owner_kind,owner_id,agent_id,content,metadata,source,status,revision,mutation_version,created_at,updated_at,expires_at,has_expiry,supersedes_id,recall_class,priority,index_state,store_sequence FROM ` + s.table("facts") + ` WHERE tenant_id=$1 AND id=$2`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanFact(tx.QueryRow(ctx, query, tenant, string(id)))
}
func scanFact(row pgx.Row) (memory.Fact, error) {
	var f memory.Fact
	var id string
	var ownerKind, status, class, indexState string
	var metadata, source []byte
	var rev, mv int64
	var expires time.Time
	var hasExpiry bool
	var supersedes string
	var sequence int64
	err := row.Scan(&id, &f.Scope.TenantID, &ownerKind, &f.Scope.OwnerID, &f.Scope.AgentID, &f.Content, &metadata, &source, &status, &rev, &mv, &f.CreatedAt, &f.UpdatedAt, &expires, &hasExpiry, &supersedes, &class, &f.Priority, &indexState, &sequence)
	if err != nil {
		return memory.Fact{}, dbError(err)
	}
	f.ID = memory.FactID(id)
	f.Scope.OwnerKind = memory.OwnerKind(ownerKind)
	f.Status = memory.FactStatus(status)
	f.Revision = memory.Revision(rev)
	f.MutationVersion = memory.MutationVersion(mv)
	f.StoreSequence = memory.StoreSequence(sequence)
	f.RecallClass = memory.RecallClass(class)
	f.IndexState = memory.IndexState(indexState)
	f.SupersedesID = memory.FactID(supersedes)
	if hasExpiry {
		f.ExpiresAt = expires
	}
	_ = unmarshal(metadata, &f.Metadata)
	_ = unmarshal(source, &f.Source)
	return f, nil
}
func (s *Store) updateFactTx(ctx context.Context, tx pgx.Tx, f memory.Fact, expected memory.Revision) error {
	metadata, _ := marshal(f.Metadata)
	source, _ := marshal(f.Source)
	content := f.Content
	if f.Status == memory.FactDeleted {
		content = ""
		metadata = []byte(`{}`)
	}
	tag, err := tx.Exec(ctx, `UPDATE `+s.table("facts")+` SET content=$1,metadata=$2,source=$3,status=$4,revision=$5,mutation_version=$6,updated_at=$7,expires_at=$8,has_expiry=$9,supersedes_id=$10,recall_class=$11,priority=$12,index_state=$13,store_sequence=$14 WHERE tenant_id=$15 AND id=$16 AND revision=$17`, content, metadata, source, string(f.Status), int64(f.Revision), int64(f.MutationVersion), f.UpdatedAt, normalizeTime(f.ExpiresAt), !f.ExpiresAt.IsZero(), string(f.SupersedesID), string(f.RecallClass), f.Priority, string(memory.IndexPending), int64(f.StoreSequence), f.Scope.TenantID, string(f.ID), int64(expected))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return &memory.MemoryError{Kind: memory.ErrRevisionConflict}
	}
	_, err = tx.Exec(ctx, `UPDATE `+s.table("operations")+` SET lifecycle=$1 WHERE tenant_id=$2 AND fact_id=$3`, string(f.Status), f.Scope.TenantID, string(f.ID))
	return err
}
func (s *Store) insertChangeTx(ctx context.Context, tx pgx.Tx, f memory.Fact, action memory.ChangeAction, actor memory.Principal, at time.Time, supersedes memory.FactID) error {
	actorJSON, _ := marshal(actor)
	source, _ := marshal(f.Source)
	_, err := tx.Exec(ctx, `INSERT INTO `+s.table("changes")+` (fact_id,tenant_id,owner_kind,owner_id,agent_id,revision,action,actor,changed_at,source,supersedes_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, string(f.ID), f.Scope.TenantID, string(f.Scope.OwnerKind), f.Scope.OwnerID, f.Scope.AgentID, int64(f.Revision), string(action), actorJSON, at, source, string(supersedes))
	return err
}
func (s *Store) insertOutboxTx(ctx context.Context, tx pgx.Tx, id memory.FactID, scope memory.MemoryScope, rev memory.Revision, mv memory.MutationVersion, seq memory.StoreSequence, action memory.IndexMutationAction) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,3))`, `index_generation_switch`); err != nil {
		return err
	}
	var activeRaw []byte
	var activeGeneration int64
	if err := tx.QueryRow(ctx, `SELECT profile,generation FROM `+s.table("active_profile")+` WHERE singleton=true`).Scan(&activeRaw, &activeGeneration); err != nil {
		return err
	}
	var activeProfile memory.EmbeddingProfile
	if err := json.Unmarshal(activeRaw, &activeProfile); err != nil {
		return err
	}
	targets := []memory.IndexProfileState{{Profile: activeProfile, Generation: memory.IndexGeneration(activeGeneration)}}
	if action == memory.MutationUpsert {
		rows, err := tx.Query(ctx, `SELECT profile,generation FROM `+s.table("rebuilds")+` WHERE state IN('building','ready')`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var raw []byte
			var generation int64
			if err = rows.Scan(&raw, &generation); err != nil {
				rows.Close()
				return err
			}
			var profile memory.EmbeddingProfile
			if err = json.Unmarshal(raw, &profile); err != nil {
				rows.Close()
				return err
			}
			targets = append(targets, memory.IndexProfileState{Profile: profile, Generation: memory.IndexGeneration(generation)})
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
	}
	for _, target := range targets {
		if action == memory.MutationUpsert {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,2))`, `outbox|`+scope.TenantID); err != nil {
				return err
			}
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+s.table("outbox")+` WHERE tenant_id=$1 AND state IN('pending','leased','failed')`, scope.TenantID).Scan(&count); err != nil {
				return err
			}
			if count >= s.maxPendingPerTenant {
				return &memory.MemoryError{Kind: memory.ErrCapacityExceeded, Op: "索引 outbox 背压"}
			}
		}
		idText, err := randomID("mut_")
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO `+s.table("outbox")+` (id,fact_id,tenant_id,owner_kind,owner_id,agent_id,revision,mutation_version,store_sequence,profile_id,generation,action,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'pending')`, idText, string(id), scope.TenantID, string(scope.OwnerKind), scope.OwnerID, scope.AgentID, int64(rev), int64(mv), int64(seq), string(target.Profile.ID), int64(target.Generation), string(action))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Get(ctx context.Context, access memory.MemoryAccess, id memory.FactID, revision memory.Revision) (memory.Fact, error) {
	f, err := s.getFactTx(ctx, s.pool, access.Principal.TenantID, id, false)
	if err != nil {
		return f, err
	}
	if !authorized(access.ReadScopes, f.Scope) {
		return memory.Fact{}, &memory.MemoryError{Kind: memory.ErrAuthorization}
	}
	if revision != 0 && f.Revision != revision {
		return memory.Fact{}, &memory.MemoryError{Kind: memory.ErrRevisionConflict}
	}
	if f.Status != memory.FactActive || isExpired(time.Now(), f.ExpiresAt, !f.ExpiresAt.IsZero()) {
		return memory.Fact{}, &memory.MemoryError{Kind: memory.ErrGone}
	}
	return f, nil
}
func authorized(scopes []memory.MemoryScope, target memory.MemoryScope) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}
func (s *Store) LoadIndexInput(ctx context.Context, m memory.IndexMutation) (memory.IndexInput, error) {
	f, err := s.getFactTx(ctx, s.pool, m.Scope.TenantID, m.FactID, false)
	if err != nil {
		return memory.IndexInput{}, err
	}
	return memory.IndexInput{FactID: f.ID, Content: f.Content, MutationVersion: f.MutationVersion, Active: f.Status == memory.FactActive && !isExpired(time.Now(), f.ExpiresAt, !f.ExpiresAt.IsZero())}, nil
}
func (s *Store) List(ctx context.Context, access memory.MemoryAccess, filter memory.FactFilter, page memory.PageRequest) (memory.FactPage, error) {
	if page.Limit < 1 || page.Limit > 1000 {
		return memory.FactPage{}, &memory.MemoryError{Kind: memory.ErrInvalidCursor, Op: "分页大小超出范围"}
	}
	scopes := filter.Scopes
	if len(scopes) == 0 {
		scopes = access.ReadScopes
	}
	for _, scope := range scopes {
		if scope.TenantID != access.Principal.TenantID || !authorized(access.ReadScopes, scope) {
			return memory.FactPage{}, &memory.MemoryError{Kind: memory.ErrAuthorization}
		}
	}
	filter.Scopes = append([]memory.MemoryScope(nil), scopes...)
	binding, err := cursorBinding(access, filter)
	if err != nil {
		return memory.FactPage{}, err
	}
	var secret []byte
	if err = s.pool.QueryRow(ctx, `SELECT secret FROM `+s.table("cursor_signing_key")+` WHERE singleton=true`).Scan(&secret); err != nil {
		return memory.FactPage{}, err
	}
	var cursor listCursor
	if page.Cursor == "" {
		if err = s.pool.QueryRow(ctx, `SELECT last_value FROM `+s.qschema()+`."store_sequence"`).Scan(&cursor.Cutoff); err != nil {
			return memory.FactPage{}, err
		}
	} else {
		cursor, err = decodeCursor(secret, page.Cursor, binding, time.Now())
		if err != nil {
			return memory.FactPage{}, err
		}
		if cursor.Kind != "facts" || cursor.ID == "" {
			return memory.FactPage{}, &memory.MemoryError{Kind: memory.ErrInvalidCursor}
		}
	}
	where, args := scopeWhere(scopes, 1)
	appendArg := func(value any) string { args = append(args, value); return "$" + fmt.Sprint(len(args)) }
	predicates := []string{where, "tenant_id=" + appendArg(access.Principal.TenantID), "created_sequence<=" + appendArg(cursor.Cutoff), "status='active'", "(has_expiry=false OR expires_at>now())"}
	if len(filter.Statuses) > 0 {
		statusValues := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			statusValues = append(statusValues, string(status))
		}
		predicates = append(predicates, "status=ANY("+appendArg(sortedUnique(statusValues))+"::text[])")
	}
	if len(filter.RecallClasses) > 0 {
		classes := make([]string, 0, len(filter.RecallClasses))
		for _, class := range filter.RecallClasses {
			classes = append(classes, string(class))
		}
		predicates = append(predicates, "recall_class=ANY("+appendArg(sortedUnique(classes))+"::text[])")
	}
	for _, tag := range sortedUnique(filter.Tags) {
		predicates = append(predicates, "COALESCE(metadata->'tags','[]'::jsonb) ? "+appendArg(tag))
	}
	if len(filter.SourceIDs) > 0 {
		predicates = append(predicates, "source->>'SourceID'=ANY("+appendArg(sortedUnique(filter.SourceIDs))+"::text[])")
	}
	if !filter.UpdatedAfter.IsZero() {
		predicates = append(predicates, "updated_at >= "+appendArg(filter.UpdatedAfter))
	}
	if !filter.UpdatedBefore.IsZero() {
		predicates = append(predicates, "updated_at <= "+appendArg(filter.UpdatedBefore))
	}
	if cursor.ID != "" {
		predicates = append(predicates, "id > "+appendArg(cursor.ID))
	}
	limitArg := appendArg(page.Limit + 1)
	query := `SELECT id,tenant_id,owner_kind,owner_id,agent_id,content,metadata,source,status,revision,mutation_version,created_at,updated_at,expires_at,has_expiry,supersedes_id,recall_class,priority,index_state,store_sequence FROM ` + s.table("facts") + ` WHERE ` + strings.Join(predicates, " AND ") + ` ORDER BY id ASC LIMIT ` + limitArg
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return memory.FactPage{}, err
	}
	defer rows.Close()
	var result memory.FactPage
	for rows.Next() {
		f, err := scanFact(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, f)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > page.Limit {
		result.Items = result.Items[:page.Limit]
		last := result.Items[len(result.Items)-1]
		next := listCursor{Version: cursorVersion, Kind: "facts", Binding: binding, Cutoff: cursor.Cutoff, ID: string(last.ID), Expires: time.Now().Add(cursorTTL).Unix()}
		result.NextCursor, err = encodeCursor(secret, next)
		if err != nil {
			return memory.FactPage{}, err
		}
	}
	return result, nil
}
func scopeWhere(scopes []memory.MemoryScope, start int) (string, []any) {
	if len(scopes) == 0 {
		return "false", nil
	}
	parts := make([]string, 0, len(scopes))
	var args []any
	for _, scope := range scopes {
		n := start + len(args)
		parts = append(parts, fmt.Sprintf("(tenant_id=$%d AND owner_kind=$%d AND owner_id=$%d AND agent_id=$%d)", n, n+1, n+2, n+3))
		args = append(args, scope.TenantID, string(scope.OwnerKind), scope.OwnerID, scope.AgentID)
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func (s *Store) ListChanges(ctx context.Context, access memory.MemoryAccess, id memory.FactID, page memory.PageRequest) (memory.ChangePage, error) {
	if page.Limit < 1 || page.Limit > 1000 {
		return memory.ChangePage{}, &memory.MemoryError{Kind: memory.ErrInvalidCursor, Op: "变更分页大小超出范围"}
	}
	f, err := s.getFactTx(ctx, s.pool, access.Principal.TenantID, id, false)
	if err != nil {
		return memory.ChangePage{}, err
	}
	if !authorized(access.ReadScopes, f.Scope) {
		return memory.ChangePage{}, &memory.MemoryError{Kind: memory.ErrAuthorization}
	}
	binding, err := changeCursorBinding(access, f.Scope, id)
	if err != nil {
		return memory.ChangePage{}, err
	}
	var secret []byte
	if err = s.pool.QueryRow(ctx, `SELECT secret FROM `+s.table("cursor_signing_key")+` WHERE singleton=true`).Scan(&secret); err != nil {
		return memory.ChangePage{}, err
	}
	var cursor listCursor
	if page.Cursor == "" {
		if err = s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(seq),0) FROM `+s.table("changes")+` WHERE tenant_id=$1 AND fact_id=$2`, access.Principal.TenantID, string(id)).Scan(&cursor.Cutoff); err != nil {
			return memory.ChangePage{}, err
		}
	} else {
		cursor, err = decodeCursor(secret, page.Cursor, binding, time.Now())
		if err != nil {
			return memory.ChangePage{}, err
		}
		if cursor.Kind != "changes" || cursor.Sequence < 0 {
			return memory.ChangePage{}, &memory.MemoryError{Kind: memory.ErrInvalidCursor}
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT fact_id,tenant_id,owner_kind,owner_id,agent_id,revision,action,actor,changed_at,source,supersedes_id,seq FROM `+s.table("changes")+` WHERE tenant_id=$1 AND fact_id=$2 AND seq>$3 AND seq<=$4 ORDER BY seq LIMIT $5`, access.Principal.TenantID, string(id), cursor.Sequence, cursor.Cutoff, page.Limit+1)
	if err != nil {
		return memory.ChangePage{}, err
	}
	defer rows.Close()
	result := memory.ChangePage{}
	var sequences []int64
	for rows.Next() {
		var c memory.ChangeRecord
		var id, owner, action string
		var rev int64
		var actor, source []byte
		var supersedes string
		var seq int64
		if err := rows.Scan(&id, &c.Scope.TenantID, &owner, &c.Scope.OwnerID, &c.Scope.AgentID, &rev, &action, &actor, &c.At, &source, &supersedes, &seq); err != nil {
			return result, err
		}
		sequences = append(sequences, seq)
		c.Scope.OwnerKind = memory.OwnerKind(owner)
		c.FactID = memory.FactID(id)
		c.Revision = memory.Revision(rev)
		c.Action = memory.ChangeAction(action)
		c.SupersedesID = memory.FactID(supersedes)
		_ = unmarshal(actor, &c.Actor)
		_ = unmarshal(source, &c.Source)
		result.Items = append(result.Items, c)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > page.Limit {
		result.Items = result.Items[:page.Limit]
		nextSequence := sequences[page.Limit-1]
		next := listCursor{Version: cursorVersion, Kind: "changes", Binding: binding, Cutoff: cursor.Cutoff, Sequence: nextSequence, Expires: time.Now().Add(cursorTTL).Unix()}
		result.NextCursor, err = encodeCursor(secret, next)
		if err != nil {
			return memory.ChangePage{}, err
		}
	}
	return result, nil
}
