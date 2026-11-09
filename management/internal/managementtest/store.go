// Package managementtest 提供管理子树的离线事务替身，不证明 PostgreSQL 隔离、锁或持久性。
package managementtest

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
)

type docKey struct{ kind, tenant, id string }
type documents map[docKey]mg.Document

// Store 用串行事务和深拷贝保证失败写入不可见，读取结果不会泄漏可变引用。
type Store struct {
	mu      sync.Mutex
	docs    documents
	failure *failure
}
type failure struct {
	operation, kind string
	err             error
}
type transaction struct {
	docs  documents
	store *Store
}

func NewStore() *Store                { return &Store{docs: documents{}} }
func clone(d mg.Document) mg.Document { d.Data = slices.Clone(d.Data); return d }
func key(d mg.Document) docKey        { return docKey{d.Kind, d.TenantID, d.ID} }

func validKind(kind string) error {
	if !slices.Contains(mg.Kinds, kind) {
		return agentErrors.ErrManagementInvalid
	}
	return nil
}
func validDocument(d mg.Document) error {
	if err := validKind(d.Kind); err != nil {
		return err
	}
	var m mg.Meta
	if json.Unmarshal(d.Data, &m) != nil || d.Revision < 1 || m.ID != d.ID || m.TenantID != d.TenantID || m.Revision != d.Revision {
		return agentErrors.ErrManagementInvalid
	}
	return nil
}

// FailNext 在下一次匹配的存储操作失败，用于检验事务内的多记录原子性。
func (s *Store) FailNext(operation, kind string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failure = &failure{operation, kind, err}
}
func (s *Store) fail(operation, kind string) error {
	if f := s.failure; f != nil && f.operation == operation && f.kind == kind {
		s.failure = nil
		return f.err
	}
	return nil
}
func (s *Store) WithinTx(ctx context.Context, fn func(mg.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	copy := documents{}
	for k, d := range s.docs {
		copy[k] = clone(d)
	}
	if err := fn(&transaction{copy, s}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.docs = copy
	return nil
}
func (s *Store) Get(ctx context.Context, kind, tenant, id string) (mg.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&transaction{s.docs, s}).Get(ctx, kind, tenant, id)
}
func (s *Store) List(ctx context.Context, kind string, selection mg.Selection) ([]mg.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&transaction{s.docs, s}).List(ctx, kind, selection)
}
func (tx *transaction) Get(ctx context.Context, kind, tenant, id string) (mg.Document, error) {
	if err := validKind(kind); err != nil {
		return mg.Document{}, err
	}
	if err := ctx.Err(); err != nil {
		return mg.Document{}, err
	}
	if err := tx.store.fail("get", kind); err != nil {
		return mg.Document{}, err
	}
	d, ok := tx.docs[docKey{kind, tenant, id}]
	if !ok {
		return mg.Document{}, agentErrors.ErrManagementNotFound
	}
	return clone(d), nil
}
func (tx *transaction) Insert(ctx context.Context, d mg.Document) error {
	if err := validDocument(d); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.store.fail("insert", d.Kind); err != nil {
		return err
	}
	if _, ok := tx.docs[key(d)]; ok {
		return agentErrors.ErrManagementConflict
	}
	tx.docs[key(d)] = clone(d)
	return nil
}
func (tx *transaction) CAS(ctx context.Context, d mg.Document, expected int64) error {
	if err := validDocument(d); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.store.fail("cas", d.Kind); err != nil {
		return err
	}
	old, ok := tx.docs[key(d)]
	if !ok || old.Revision != expected {
		return agentErrors.ErrManagementConflict
	}
	d.CreatedAt = old.CreatedAt
	tx.docs[key(d)] = clone(d)
	return nil
}
func (tx *transaction) Delete(ctx context.Context, kind, tenant, id string, expected int64) error {
	if err := validKind(kind); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.store.fail("delete", kind); err != nil {
		return err
	}
	k := docKey{kind, tenant, id}
	old, ok := tx.docs[k]
	if !ok || old.Revision != expected {
		return agentErrors.ErrManagementConflict
	}
	delete(tx.docs, k)
	return nil
}
func (tx *transaction) List(ctx context.Context, kind string, selection mg.Selection) ([]mg.Document, error) {
	if err := validKind(kind); err != nil {
		return nil, err
	}
	if selection.TenantID == "" && !selection.Visibility.All {
		return nil, agentErrors.ErrManagementForbidden
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := tx.store.fail("list", kind); err != nil {
		return nil, err
	}
	q, v := selection.Query, selection.Visibility
	if q.Limit > 10001 {
		return nil, agentErrors.ErrManagementInvalid
	}
	if q.Limit <= 0 {
		q.Limit = 201
	}
	var cursorTime time.Time
	var cursorID string
	if q.Cursor != "" {
		parts := strings.SplitN(q.Cursor, "|", 2)
		if len(parts) != 2 {
			return nil, agentErrors.ErrManagementInvalid
		}
		var err error
		cursorTime, err = time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return nil, agentErrors.ErrManagementInvalid
		}
		cursorID = parts[1]
	}
	out := []mg.Document{}
	for _, d := range tx.docs {
		if d.Kind != kind || (selection.TenantID != "" && d.TenantID != selection.TenantID) {
			continue
		}
		var fields struct {
			AgentID   string `json:"agent_id"`
			SessionID string `json:"session_id"`
			TaskID    string `json:"task_id"`
			State     string `json:"state"`
			Status    string `json:"status"`
		}
		if err := json.Unmarshal(d.Data, &fields); err != nil {
			return nil, err
		}
		if q.AgentID != "" && fields.AgentID != q.AgentID || q.SessionID != "" && fields.SessionID != q.SessionID || q.TaskID != "" && fields.TaskID != q.TaskID {
			continue
		}
		state := fields.State
		if state == "" {
			state = fields.Status
		}
		if len(q.Statuses) > 0 && !slices.Contains(q.Statuses, state) {
			continue
		}
		if !q.CreatedFrom.IsZero() && d.CreatedAt.Before(q.CreatedFrom) || !q.CreatedTo.IsZero() && d.CreatedAt.After(q.CreatedTo) {
			continue
		}
		// 自身 ID 只用于可见范围；查询字段仍按 JSON 中的父资源关系匹配。
		if kind == "agents" {
			fields.AgentID = d.ID
		}
		if kind == "sessions" {
			fields.SessionID = d.ID
		}
		if !v.All && !slices.Contains(v.ResourceIDs, d.ID) && !slices.Contains(v.AgentIDs, fields.AgentID) && !slices.Contains(v.SessionIDs, fields.SessionID) {
			continue
		}
		if q.Cursor != "" && (d.CreatedAt.Before(cursorTime) || d.CreatedAt.Equal(cursorTime) && d.ID <= cursorID) {
			continue
		}
		out = append(out, clone(d))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// Put 显式布置测试前置状态；已有记录也必须通过当前修订号的 CAS。
func (s *Store) Put(ctx context.Context, kind string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var meta mg.Meta
	if err = json.Unmarshal(raw, &meta); err != nil {
		return err
	}
	d := mg.Document{Kind: kind, TenantID: meta.TenantID, ID: meta.ID, Revision: meta.Revision, CreatedAt: meta.CreatedAt, Data: raw}
	return s.WithinTx(ctx, func(tx mg.Tx) error {
		old, err := tx.Get(ctx, kind, meta.TenantID, meta.ID)
		if err == agentErrors.ErrManagementNotFound {
			return tx.Insert(ctx, d)
		}
		if err != nil {
			return err
		}
		return tx.CAS(ctx, d, old.Revision)
	})
}

var _ mg.Store = (*Store)(nil)
