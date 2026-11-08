package postgres

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kart-io/goagent/memory"
)

const maxMutationAttempts = 10

func (s *Store) ClaimIndexMutations(ctx context.Context, limit int) ([]memory.IndexMutation, error) {
	if limit < 1 {
		return nil, nil
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `WITH expired AS (SELECT ctid FROM `+s.table("operations")+` WHERE expires_at<=now() AND expired=false ORDER BY expires_at LIMIT 1000 FOR UPDATE SKIP LOCKED) UPDATE `+s.table("operations")+` o SET expired=true,operation_id='',fact_id='',revision=0,index_state='',lifecycle='expired' FROM expired WHERE o.ctid=expired.ctid`); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE `+s.table("outbox")+` SET state='failed',next_retry=$1,lease_until='epoch',diagnostic='index_attempts_exhausted' WHERE state='leased' AND lease_until<=now() AND attempts >= $2`, time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC), maxMutationAttempts); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,fact_id,tenant_id,owner_kind,owner_id,agent_id,revision,mutation_version,store_sequence,profile_id,generation,action,lease_token FROM `+s.table("outbox")+` WHERE state IN('pending','failed','leased') AND attempts < $2 AND next_retry<=now() AND lease_until<=now() ORDER BY store_sequence,id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit, maxMutationAttempts)
	if err != nil {
		return nil, err
	}
	var out []memory.IndexMutation
	for rows.Next() {
		var m memory.IndexMutation
		var id, fact, owner, action, profile string
		var rev, mv, seq, gen, leaseToken int64
		if err = rows.Scan(&id, &fact, &m.Scope.TenantID, &owner, &m.Scope.OwnerID, &m.Scope.AgentID, &rev, &mv, &seq, &profile, &gen, &action, &leaseToken); err != nil {
			rows.Close()
			return nil, err
		}
		m.ID = memory.MutationID(id)
		m.LeaseToken = uint64(leaseToken + 1)
		m.FactID = memory.FactID(fact)
		m.Scope.OwnerKind = memory.OwnerKind(owner)
		m.Revision = memory.Revision(rev)
		m.MutationVersion = memory.MutationVersion(mv)
		m.StoreSequence = memory.StoreSequence(seq)
		m.ProfileID = memory.EmbeddingProfileID(profile)
		m.Generation = memory.IndexGeneration(gen)
		m.Action = memory.IndexMutationAction(action)
		out = append(out, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, m := range out {
		if _, err = tx.Exec(ctx, `UPDATE `+s.table("outbox")+` SET state='leased',lease_until=now()+interval '30 seconds',attempts=attempts+1,lease_token=lease_token+1 WHERE id=$1`, string(m.ID)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Store) AckIndexMutation(ctx context.Context, m memory.IndexMutation, result memory.IndexMutationResult) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var attempts int64
	var leaseToken int64
	var leaseUntil time.Time
	var leaseState string
	if err = tx.QueryRow(ctx, `SELECT attempts,lease_token,lease_until,state FROM `+s.table("outbox")+` WHERE id=$1 FOR UPDATE`, string(m.ID)).Scan(&attempts, &leaseToken, &leaseUntil, &leaseState); err != nil {
		return dbError(err)
	}
	if leaseState != "leased" || uint64(leaseToken) != m.LeaseToken || !time.Now().Before(leaseUntil) {
		return &memory.MemoryError{Kind: memory.ErrRevisionConflict, Op: "索引任务租约已失效"}
	}
	var current int64
	var status string
	err = tx.QueryRow(ctx, `SELECT mutation_version,status FROM `+s.table("facts")+` WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, m.Scope.TenantID, string(m.FactID)).Scan(&current, &status)
	if err != nil && !isNoRows(err) {
		return err
	}
	if result == memory.MutationApplied && err == nil && memory.MutationVersion(current) != m.MutationVersion {
		result = memory.MutationObsolete
	}
	state := string(result)
	diagnostic := ""
	retry := time.Now().UTC()
	if result == memory.MutationFailed {
		state = "failed"
		diagnostic = "index_apply_failed"
		if attempts >= maxMutationAttempts {
			retry = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
		} else {
			delay := time.Duration(1<<min(max(0, attempts-1), 8)) * time.Second
			if delay > 5*time.Minute {
				delay = 5 * time.Minute
			}
			retry = retry.Add(delay)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE `+s.table("outbox")+` SET state=$1,lease_until='epoch',next_retry=$2,diagnostic=$3 WHERE id=$4`, state, retry, diagnostic, string(m.ID)); err != nil {
		return err
	}
	if result == memory.MutationApplied && err == nil {
		indexState := memory.IndexReady
		if status != string(memory.FactActive) {
			indexState = memory.IndexPending
		}
		_, err = tx.Exec(ctx, `UPDATE `+s.table("facts")+` SET index_state=$1 WHERE tenant_id=$2 AND id=$3 AND mutation_version=$4`, string(indexState), m.Scope.TenantID, string(m.FactID), int64(m.MutationVersion))
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE `+s.table("clears")+` c SET state='complete',updated_at=now() WHERE state IN('pending','running') AND NOT EXISTS(SELECT 1 FROM `+s.table("outbox")+` o WHERE o.tenant_id=c.tenant_id AND o.owner_kind=c.owner_kind AND o.owner_id=c.owner_id AND o.agent_id=c.agent_id AND o.action='tombstone' AND o.state IN('pending','leased','failed'))`)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func isNoRows(err error) bool { return err == pgx.ErrNoRows }

func (s *Store) BeginScopeClear(ctx context.Context, access memory.ClearAccess, target memory.MemoryScope, key memory.IdempotencyKey) (memory.ClearOperation, error) {
	if access.Principal.TenantID == "" || access.Principal.TenantID != target.TenantID || !authorized(access.TargetScopes, target) || !hasCap(access.Capabilities, "memory.clear") {
		return memory.ClearOperation{}, &memory.MemoryError{Kind: memory.ErrAuthorization}
	}
	if key == "" {
		return memory.ClearOperation{}, &memory.MemoryError{Kind: memory.ErrIdempotencyConflict}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return memory.ClearOperation{}, err
	}
	defer tx.Rollback(ctx)
	scopeKey := s.scopeKey(target)
	keyHash := idempotencyDigest(string(key))
	if err = lockMemoryScope(ctx, tx, target, scopeKey); err != nil {
		return memory.ClearOperation{}, err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT clear_id FROM `+s.table("clear_keys")+` WHERE tenant_id=$1 AND scope_key=$2 AND idem_key=$3`, target.TenantID, scopeKey, keyHash).Scan(&id)
	if err == nil {
		op, e := s.getClearTx(ctx, tx, id)
		if e != nil {
			return op, e
		}
		if err = tx.Commit(ctx); err != nil {
			return op, err
		}
		return op, nil
	}
	if !isNoRows(err) {
		return memory.ClearOperation{}, err
	}
	err = tx.QueryRow(ctx, `SELECT id FROM `+s.table("clears")+` WHERE tenant_id=$1 AND owner_kind=$2 AND owner_id=$3 AND agent_id=$4 AND state<>'complete' LIMIT 1`, target.TenantID, string(target.OwnerKind), target.OwnerID, target.AgentID).Scan(&id)
	if err == nil {
		op, e := s.getClearTx(ctx, tx, id)
		if e != nil {
			return op, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO `+s.table("clear_keys")+` (tenant_id,scope_key,idem_key,clear_id) VALUES($1,$2,$3,$4)`, target.TenantID, scopeKey, keyHash, id); e != nil {
			return memory.ClearOperation{}, e
		}
		return op, tx.Commit(ctx)
	}
	if !isNoRows(err) {
		return memory.ClearOperation{}, err
	}
	var cutoff int64
	if err = tx.QueryRow(ctx, `SELECT nextval('`+s.schema+`.store_sequence')`).Scan(&cutoff); err != nil {
		return memory.ClearOperation{}, err
	}
	id, err = randomID("clear_")
	if err != nil {
		return memory.ClearOperation{}, err
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO `+s.table("clears")+` (id,tenant_id,owner_kind,owner_id,agent_id,state,cutoff,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'pending',$6,$7,$7)`, id, target.TenantID, string(target.OwnerKind), target.OwnerID, target.AgentID, cutoff, now)
	if err != nil {
		return memory.ClearOperation{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO `+s.table("clear_keys")+` (tenant_id,scope_key,idem_key,clear_id) VALUES($1,$2,$3,$4)`, target.TenantID, scopeKey, keyHash, id)
	if err != nil {
		return memory.ClearOperation{}, err
	}
	rows, err := tx.Query(ctx, `SELECT id,revision,mutation_version,source,supersedes_id,recall_class,priority,metadata,created_at,updated_at,expires_at,has_expiry FROM `+s.table("facts")+` WHERE tenant_id=$1 AND owner_kind=$2 AND owner_id=$3 AND agent_id=$4 AND status='active' FOR UPDATE`, target.TenantID, string(target.OwnerKind), target.OwnerID, target.AgentID)
	if err != nil {
		return memory.ClearOperation{}, err
	}
	type item struct {
		id                        string
		rev, mv                   int64
		source                    []byte
		supersedes, class         string
		priority                  int
		metadata                  []byte
		created, updated, expires time.Time
		has                       bool
	}
	var facts []item
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.id, &v.rev, &v.mv, &v.source, &v.supersedes, &v.class, &v.priority, &v.metadata, &v.created, &v.updated, &v.expires, &v.has); err != nil {
			rows.Close()
			return memory.ClearOperation{}, err
		}
		facts = append(facts, v)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return memory.ClearOperation{}, err
	}
	for _, v := range facts {
		f := memory.Fact{ID: memory.FactID(v.id), Scope: target, Revision: memory.Revision(v.rev + 1), MutationVersion: memory.MutationVersion(v.mv + 1), Source: memory.FactSource{}, SupersedesID: memory.FactID(v.supersedes), RecallClass: memory.RecallClass(v.class), Priority: v.priority, CreatedAt: v.created, UpdatedAt: now}
		_ = json.Unmarshal(v.source, &f.Source)
		_, err = tx.Exec(ctx, `UPDATE `+s.table("facts")+` SET status='deleted',revision=$1,mutation_version=$2,updated_at=$3,content='',metadata='{}'::jsonb,clear_operation=$4 WHERE id=$5`, v.rev+1, v.mv+1, now, id, v.id)
		if err != nil {
			return memory.ClearOperation{}, err
		}
		if err = s.insertChangeTx(ctx, tx, f, memory.ChangeDeleted, access.Principal, now, ""); err != nil {
			return memory.ClearOperation{}, err
		}
		if err = s.insertOutboxTx(ctx, tx, f.ID, target, f.Revision, f.MutationVersion, memory.StoreSequence(cutoff), memory.MutationTombstone); err != nil {
			return memory.ClearOperation{}, err
		}
		_, err = tx.Exec(ctx, `UPDATE `+s.table("operations")+` SET lifecycle='deleted' WHERE tenant_id=$1 AND fact_id=$2`, target.TenantID, v.id)
		if err != nil {
			return memory.ClearOperation{}, err
		}
	}
	op := memory.ClearOperation{ID: memory.ClearOperationID(id), Scope: target, State: "pending", Cutoff: memory.StoreSequence(cutoff), CreatedAt: now, UpdatedAt: now}
	if err = tx.Commit(ctx); err != nil {
		return memory.ClearOperation{}, err
	}
	return op, nil
}

func lockMemoryScope(ctx context.Context, tx pgx.Tx, scope memory.MemoryScope, key string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,1))`, `scope|`+scope.TenantID+"|"+key)
	return err
}
func hasCap(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}
func (s *Store) getClearTx(ctx context.Context, tx rowQuerier, id string) (memory.ClearOperation, error) {
	var o memory.ClearOperation
	var tenant, ownerKind string
	var cutoff int64
	err := tx.QueryRow(ctx, `SELECT id,tenant_id,owner_kind,owner_id,agent_id,state,cutoff,created_at,updated_at FROM `+s.table("clears")+` WHERE id=$1`, id).Scan((*string)(&id), &tenant, &ownerKind, &o.Scope.OwnerID, &o.Scope.AgentID, &o.State, &cutoff, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return o, err
	}
	o.ID = memory.ClearOperationID(id)
	o.Scope.TenantID = tenant
	o.Scope.OwnerKind = memory.OwnerKind(ownerKind)
	o.Cutoff = memory.StoreSequence(cutoff)
	return o, nil
}
func (s *Store) GetClearOperation(ctx context.Context, access memory.MemoryAccess, id memory.ClearOperationID) (memory.ClearOperation, error) {
	o, err := s.getClearTx(ctx, s.pool, string(id))
	if err != nil {
		return o, dbError(err)
	}
	if !authorized(access.ReadScopes, o.Scope) {
		return memory.ClearOperation{}, &memory.MemoryError{Kind: memory.ErrAuthorization}
	}
	return o, nil
}
func (s *Store) ClaimClearOperations(ctx context.Context, limit int) ([]memory.ClearOperation, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,tenant_id,owner_kind,owner_id,agent_id,state,cutoff,created_at,updated_at FROM `+s.table("clears")+` WHERE state IN('pending','running') ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memory.ClearOperation
	for rows.Next() {
		var o memory.ClearOperation
		var id, tenant, owner string
		var cutoff int64
		if err = rows.Scan(&id, &tenant, &owner, &o.Scope.OwnerID, &o.Scope.AgentID, &o.State, &cutoff, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		o.ID = memory.ClearOperationID(id)
		o.Scope.TenantID = tenant
		o.Scope.OwnerKind = memory.OwnerKind(owner)
		o.Cutoff = memory.StoreSequence(cutoff)
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *Store) AckClearOperation(ctx context.Context, id memory.ClearOperationID, p memory.ClearProgress) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var op memory.ClearOperation
	var tenant, owner string
	var cutoff int64
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,owner_kind,owner_id,agent_id,cutoff FROM `+s.table("clears")+` WHERE id=$1 FOR UPDATE`, string(id)).Scan((*string)(&id), &tenant, &owner, &op.Scope.OwnerID, &op.Scope.AgentID, &cutoff)
	if err != nil {
		return dbError(err)
	}
	state := "running"
	if p.Done {
		var pending int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM `+s.table("outbox")+` WHERE tenant_id=$1 AND owner_kind=$2 AND owner_id=$3 AND agent_id=$4 AND action='tombstone' AND store_sequence<=$5 AND state IN('pending','leased','failed')`, tenant, owner, op.Scope.OwnerID, op.Scope.AgentID, cutoff).Scan(&pending); err != nil {
			return err
		}
		if pending == 0 {
			state = "complete"
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE `+s.table("clears")+` SET state=$1,cursor=$2,updated_at=now() WHERE id=$3`, state, p.Cursor, string(id)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListMaintenance(ctx context.Context, access memory.MaintenanceAccess, filter memory.MaintenanceFilter, page memory.PageRequest) (memory.MaintenancePage, error) {
	if access.TenantID == "" || access.OperatorID == "" || !hasCap(access.Capabilities, "memory.maintenance") {
		return memory.MaintenancePage{}, &memory.MemoryError{Kind: memory.ErrAuthorization}
	}
	if page.Limit < 1 || page.Limit > 1000 {
		return memory.MaintenancePage{}, &memory.MemoryError{Kind: memory.ErrInvalidCursor, Op: "维护分页大小超出范围"}
	}
	scopes := filter.Scopes
	if len(scopes) == 0 {
		scopes = access.AuthorizedScopes
	}
	for _, scope := range scopes {
		if scope.TenantID != access.TenantID || !authorized(access.AuthorizedScopes, scope) {
			return memory.MaintenancePage{}, &memory.MemoryError{Kind: memory.ErrAuthorization}
		}
	}
	filter.Scopes = append([]memory.MemoryScope(nil), scopes...)
	states := sortedUnique(filter.States)
	for _, state := range states {
		if state != "failed" && state != "pending" && state != "leased" {
			return memory.MaintenancePage{}, &memory.MemoryError{Kind: memory.ErrInvalidFact, Op: "维护状态筛选无效"}
		}
	}
	binding, err := maintenanceCursorBinding(access, filter)
	if err != nil {
		return memory.MaintenancePage{}, err
	}
	var secret []byte
	if err = s.pool.QueryRow(ctx, `SELECT secret FROM `+s.table("cursor_signing_key")+` WHERE singleton=true`).Scan(&secret); err != nil {
		return memory.MaintenancePage{}, err
	}
	var cursor listCursor
	if page.Cursor == "" {
		if err = s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(store_sequence),0) FROM `+s.table("outbox")+` WHERE tenant_id=$1`, access.TenantID).Scan(&cursor.Cutoff); err != nil {
			return memory.MaintenancePage{}, err
		}
	} else {
		cursor, err = decodeCursor(secret, page.Cursor, binding, time.Now())
		if err != nil {
			return memory.MaintenancePage{}, err
		}
		if cursor.Kind != "maintenance" || cursor.ID == "" {
			return memory.MaintenancePage{}, &memory.MemoryError{Kind: memory.ErrInvalidCursor}
		}
	}
	where, args := scopeWhere(scopes, 1)
	appendArg := func(value any) string { args = append(args, value); return "$" + strconv.Itoa(len(args)) }
	predicates := []string{where, "tenant_id=" + appendArg(access.TenantID), "store_sequence<=" + appendArg(cursor.Cutoff), "state IN('failed','pending','leased')"}
	if len(states) > 0 {
		predicates = append(predicates, "state=ANY("+appendArg(states)+"::text[])")
	}
	if cursor.ID != "" {
		predicates = append(predicates, "(store_sequence > "+appendArg(cursor.Sequence)+" OR (store_sequence = "+appendArg(cursor.Sequence)+" AND id > "+appendArg(cursor.ID)+"))")
	}
	limitArg := appendArg(page.Limit + 1)
	rows, err := s.pool.Query(ctx, `SELECT id,tenant_id,owner_kind,owner_id,agent_id,state,attempts,diagnostic,next_retry,store_sequence FROM `+s.table("outbox")+` WHERE `+strings.Join(predicates, " AND ")+` ORDER BY store_sequence,id LIMIT `+limitArg, args...)
	if err != nil {
		return memory.MaintenancePage{}, err
	}
	defer rows.Close()
	var result memory.MaintenancePage
	var sequences []int64
	for rows.Next() {
		var item memory.MaintenanceItem
		var scope memory.MemoryScope
		var id, owner, state string
		var sequence int64
		if err = rows.Scan(&id, &scope.TenantID, &owner, &scope.OwnerID, &scope.AgentID, &state, &item.Attempts, &item.Diagnostic, &item.NextRetry, &sequence); err != nil {
			return result, err
		}
		scope.OwnerKind = memory.OwnerKind(owner)
		sequences = append(sequences, sequence)
		item.MutationID = memory.MutationID(id)
		item.Scope = scope
		item.State = state
		result.Items = append(result.Items, item)
	}
	if len(result.Items) > page.Limit {
		result.Items = result.Items[:page.Limit]
		last := result.Items[len(result.Items)-1]
		next := listCursor{Version: cursorVersion, Kind: "maintenance", Binding: binding, Cutoff: cursor.Cutoff, Sequence: sequences[page.Limit-1], ID: string(last.MutationID), Expires: time.Now().Add(cursorTTL).Unix()}
		result.NextCursor, err = encodeCursor(secret, next)
		if err != nil {
			return memory.MaintenancePage{}, err
		}
	}
	return result, rows.Err()
}
func (s *Store) RetryMutation(ctx context.Context, access memory.MaintenanceAccess, id memory.MutationID) error {
	if access.TenantID == "" || access.OperatorID == "" || !hasCap(access.Capabilities, "memory.maintenance") {
		return &memory.MemoryError{Kind: memory.ErrAuthorization}
	}
	var scope memory.MemoryScope
	var owner string
	err := s.pool.QueryRow(ctx, `SELECT tenant_id,owner_kind,owner_id,agent_id FROM `+s.table("outbox")+` WHERE id=$1 AND tenant_id=$2`, string(id), access.TenantID).Scan(&scope.TenantID, &owner, &scope.OwnerID, &scope.AgentID)
	if err != nil {
		if isNoRows(err) {
			return &memory.MemoryError{Kind: memory.ErrMaintenanceNotFound}
		}
		return err
	}
	scope.OwnerKind = memory.OwnerKind(owner)
	if !authorized(access.AuthorizedScopes, scope) {
		return &memory.MemoryError{Kind: memory.ErrAuthorization}
	}
	tag, err := s.pool.Exec(ctx, `UPDATE `+s.table("outbox")+` SET state='pending',attempts=0,next_retry=now(),diagnostic='' WHERE id=$1 AND tenant_id=$2 AND state='failed' AND EXISTS(SELECT 1 FROM `+s.table("facts")+` f WHERE f.id=`+s.table("outbox")+`.fact_id AND f.tenant_id=$2)`, string(id), access.TenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &memory.MemoryError{Kind: memory.ErrMaintenanceNotFound}
	}
	return nil
}

func (s *Store) CreateRebuild(ctx context.Context, principal memory.MaintenancePrincipal, profile memory.EmbeddingProfile) (memory.Rebuild, error) {
	if principal.TenantID == "" || principal.OperatorID == "" || profile.ID == "" || profile.Dimension < 1 {
		return memory.Rebuild{}, &memory.MemoryError{Kind: memory.ErrInvalidFact}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return memory.Rebuild{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,3))`, `index_generation_switch`); err != nil {
		return memory.Rebuild{}, err
	}
	var active int64
	if err = tx.QueryRow(ctx, `SELECT generation FROM `+s.table("active_profile")+` WHERE singleton=true FOR UPDATE`).Scan(&active); err != nil {
		return memory.Rebuild{}, err
	}
	var rebuilding bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.table("rebuilds")+` WHERE state IN('building','ready'))`).Scan(&rebuilding); err != nil {
		return memory.Rebuild{}, err
	}
	if rebuilding {
		return memory.Rebuild{}, &memory.MemoryError{Kind: memory.ErrCapacityExceeded, Op: "已有索引重建正在进行"}
	}
	var cutoff int64
	if err = tx.QueryRow(ctx, `SELECT last_value FROM `+s.qschema()+`."store_sequence"`).Scan(&cutoff); err != nil {
		return memory.Rebuild{}, err
	}
	id, err := randomID("rebuild_")
	if err != nil {
		return memory.Rebuild{}, err
	}
	now := time.Now().UTC()
	raw, _ := json.Marshal(profile)
	generation := active + 1
	_, err = tx.Exec(ctx, `INSERT INTO `+s.table("rebuilds")+` (id,tenant_id,operator_id,profile,generation,cutoff,state,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'building',$7,$7)`, id, principal.TenantID, principal.OperatorID, raw, generation, cutoff, now)
	if err != nil {
		return memory.Rebuild{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return memory.Rebuild{}, err
	}
	return memory.Rebuild{ID: memory.RebuildID(id), Profile: profile, Generation: memory.IndexGeneration(generation), Cutoff: memory.StoreSequence(cutoff), State: "building", CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) ClaimRebuilds(ctx context.Context, limit int) ([]memory.Rebuild, error) {
	if limit < 1 {
		return nil, nil
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,profile,generation,cutoff,caught_up,cursor,state,diagnostic_code,created_at,updated_at,lease_token FROM `+s.table("rebuilds")+` WHERE state IN('building','ready') AND lease_until<=now() ORDER BY created_at LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memory.Rebuild
	for rows.Next() {
		var r memory.Rebuild
		var id string
		var raw []byte
		var gen, cutoff, caught, leaseToken int64
		if err = rows.Scan(&id, &raw, &gen, &cutoff, &caught, &r.Cursor, &r.State, &r.DiagnosticCode, &r.CreatedAt, &r.UpdatedAt, &leaseToken); err != nil {
			return nil, err
		}
		r.ID = memory.RebuildID(id)
		r.LeaseToken = uint64(leaseToken + 1)
		r.Generation = memory.IndexGeneration(gen)
		r.Cutoff = memory.StoreSequence(cutoff)
		r.CaughtUp = memory.StoreSequence(caught)
		if err = json.Unmarshal(raw, &r.Profile); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, rebuild := range out {
		if _, err = tx.Exec(ctx, `UPDATE `+s.table("rebuilds")+` SET lease_token=lease_token+1,lease_until=now()+interval '5 minutes',updated_at=now() WHERE id=$1`, string(rebuild.ID)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ScanRebuildFacts(ctx context.Context, id memory.RebuildID, cursor string, limit int) (memory.RebuildPage, error) {
	if limit < 1 {
		return memory.RebuildPage{}, nil
	}
	var cutoff int64
	if err := s.pool.QueryRow(ctx, `SELECT cutoff FROM `+s.table("rebuilds")+` WHERE id=$1 AND state IN('building','ready')`, string(id)).Scan(&cutoff); err != nil {
		return memory.RebuildPage{}, dbError(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT id,tenant_id,owner_kind,owner_id,agent_id,content,metadata,source,status,revision,mutation_version,created_at,updated_at,expires_at,has_expiry,supersedes_id,recall_class,priority,index_state,store_sequence FROM `+s.table("facts")+` WHERE status='active' AND store_sequence<=$1 AND (has_expiry=false OR expires_at>now()) AND id>$2 ORDER BY id LIMIT $3`, cutoff, cursor, limit+1)
	if err != nil {
		return memory.RebuildPage{}, err
	}
	defer rows.Close()
	var page memory.RebuildPage
	for rows.Next() {
		fact, err := scanFact(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, fact)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = string(page.Items[len(page.Items)-1].ID)
	} else {
		page.Done = true
		if len(page.Items) > 0 {
			page.NextCursor = string(page.Items[len(page.Items)-1].ID)
		}
	}
	var current int64
	if err = s.pool.QueryRow(ctx, `SELECT last_value FROM `+s.qschema()+`."store_sequence"`).Scan(&current); err != nil {
		return page, err
	}
	page.CaughtUp = memory.StoreSequence(current)
	return page, nil
}
func (s *Store) GetRebuild(ctx context.Context, access memory.MaintenanceAccess, id memory.RebuildID) (memory.Rebuild, error) {
	var r memory.Rebuild
	var raw []byte
	var gen, cutoff, caught, leaseToken int64
	err := s.pool.QueryRow(ctx, `SELECT id,profile,generation,cutoff,caught_up,cursor,state,diagnostic_code,created_at,updated_at,lease_token FROM `+s.table("rebuilds")+` WHERE id=$1 AND tenant_id=$2`, string(id), access.TenantID).Scan((*string)(&id), &raw, &gen, &cutoff, &caught, &r.Cursor, &r.State, &r.DiagnosticCode, &r.CreatedAt, &r.UpdatedAt, &leaseToken)
	if err != nil {
		return r, dbError(err)
	}
	if !hasCap(access.Capabilities, "memory.rebuild") {
		return r, &memory.MemoryError{Kind: memory.ErrAuthorization}
	}
	r.ID = id
	r.LeaseToken = uint64(leaseToken)
	r.Generation = memory.IndexGeneration(gen)
	r.Cutoff = memory.StoreSequence(cutoff)
	r.CaughtUp = memory.StoreSequence(caught)
	_ = json.Unmarshal(raw, &r.Profile)
	return r, nil
}
func (s *Store) AdvanceRebuild(ctx context.Context, id memory.RebuildID, p memory.RebuildProgress) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var leaseToken int64
	var leaseUntil time.Time
	var currentState, cursor string
	if err = tx.QueryRow(ctx, `SELECT lease_token,lease_until,state,cursor FROM `+s.table("rebuilds")+` WHERE id=$1 FOR UPDATE`, string(id)).Scan(&leaseToken, &leaseUntil, &currentState, &cursor); err != nil {
		return dbError(err)
	}
	if currentState != "building" && currentState != "ready" || uint64(leaseToken) != p.LeaseToken || !time.Now().Before(leaseUntil) {
		return &memory.MemoryError{Kind: memory.ErrRevisionConflict, Op: "重建已完成或进度已推进"}
	}
	state := currentState
	if currentState == "building" && p.Done {
		var profile []byte
		var generation int64
		if err = tx.QueryRow(ctx, `SELECT profile,generation FROM `+s.table("rebuilds")+` WHERE id=$1`, string(id)).Scan(&profile, &generation); err != nil {
			return err
		}
		var embed memory.EmbeddingProfile
		if err = json.Unmarshal(profile, &embed); err != nil {
			return err
		}
		var pending int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM `+s.table("outbox")+` WHERE profile_id=$1 AND generation=$2 AND state IN('pending','leased','failed')`, string(embed.ID), generation).Scan(&pending); err != nil {
			return err
		}
		if pending == 0 {
			state = "ready"
		}
	}
	if p.Cursor < cursor {
		p.Cursor = cursor
	}
	if _, err = tx.Exec(ctx, `UPDATE `+s.table("rebuilds")+` SET caught_up=GREATEST(caught_up,$1),cursor=$2,state=$3,lease_until=CASE WHEN $3='ready' THEN now()+interval '5 minutes' ELSE 'epoch' END,updated_at=now() WHERE id=$4`, int64(p.CaughtUp), p.Cursor, state, string(id)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) ActiveIndexProfile(ctx context.Context) (memory.IndexProfileState, error) {
	var raw []byte
	var generation int64
	err := s.pool.QueryRow(ctx, `SELECT profile,generation FROM `+s.table("active_profile")+` WHERE singleton=true`).Scan(&raw, &generation)
	if err != nil {
		return memory.IndexProfileState{}, err
	}
	var profile memory.EmbeddingProfile
	if err = json.Unmarshal(raw, &profile); err != nil {
		return memory.IndexProfileState{}, err
	}
	return memory.IndexProfileState{Profile: profile, Generation: memory.IndexGeneration(generation)}, nil
}
func (s *Store) SwitchActiveGeneration(ctx context.Context, expected memory.IndexGeneration, id memory.RebuildID, caught memory.StoreSequence, claimToken uint64) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,3))`, `index_generation_switch`); err != nil {
		return err
	}
	var r memory.Rebuild
	var raw []byte
	var gen, cutoff, current int64
	var leaseToken int64
	var leaseUntil time.Time
	err = tx.QueryRow(ctx, `SELECT profile,generation,cutoff,caught_up,lease_token,lease_until FROM `+s.table("rebuilds")+` WHERE id=$1 AND state='ready' FOR UPDATE`, string(id)).Scan(&raw, &gen, &cutoff, &current, &leaseToken, &leaseUntil)
	if err != nil {
		if isNoRows(err) {
			return &memory.MemoryError{Kind: memory.ErrRevisionConflict, Op: "重建尚未就绪"}
		}
		return err
	}
	if uint64(leaseToken) != claimToken || !time.Now().Before(leaseUntil) {
		return &memory.MemoryError{Kind: memory.ErrRevisionConflict, Op: "索引重建租约已失效"}
	}
	var latest int64
	if err = tx.QueryRow(ctx, `SELECT last_value FROM `+s.qschema()+`."store_sequence"`).Scan(&latest); err != nil {
		return err
	}
	var pending int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM `+s.table("outbox")+` WHERE profile_id=(SELECT profile->>'ID' FROM `+s.table("rebuilds")+` WHERE id=$1) AND generation=$2 AND state IN('pending','leased','failed')`, string(id), gen).Scan(&pending); err != nil {
		return err
	}
	if memory.StoreSequence(current) < caught || caught < memory.StoreSequence(cutoff) || caught < memory.StoreSequence(latest) || pending > 0 {
		return &memory.MemoryError{Kind: memory.ErrRevisionConflict, Op: "重建未追平"}
	}
	if err = tx.QueryRow(ctx, `SELECT generation FROM `+s.table("active_profile")+` WHERE singleton=true FOR UPDATE`).Scan(&current); err != nil {
		return err
	}
	if memory.IndexGeneration(current) != expected {
		return &memory.MemoryError{Kind: memory.ErrRevisionConflict}
	}
	if gen <= current {
		return &memory.MemoryError{Kind: memory.ErrRevisionConflict, Op: "拒绝回退索引代际"}
	}
	if err = json.Unmarshal(raw, &r.Profile); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE `+s.table("active_profile")+` SET profile=$1,generation=$2 WHERE singleton=true`, raw, gen); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE `+s.table("rebuilds")+` SET state='active',lease_until='epoch',updated_at=now() WHERE id=$1`, string(id)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ memory.FactStore = (*Store)(nil)
