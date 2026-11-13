package memory

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/core/durable"
)

// Purge 删除当前命名空间的全部数据（如用户要求删除个人数据）：把命名空间切换到一个新的空作用域，旧作用域进入 retiring，
// 随即清理一次。提交点是 meta、retiring 与删除日志的同一次 SaveAll，返回之后不再有读写落在旧作用域上。
// 清理失败时 Purge 已经生效（旧数据不可读），返回清理的错误；之后的 Purge 或 Worker 轮次会补完物理删除。
// 迁移进行中时返回 CodeConflict。
func (m *Memory) Purge(ctx context.Context) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	if err := m.switchScope(ctx, ns, m.now()); err != nil {
		return err
	}
	return m.cleanupRetiring(ctx)
}

// switchScope 按 meta → retiring → deletions 的顺序加锁（设计 4.1），用一次 SaveAll 切换作用域。
func (m *Memory) switchScope(ctx context.Context, ns []string, at time.Time) error {
	st := m.cfg.Durable
	ids := []string{metaID(ns), retiringID}
	if m.cfg.RollbackWindow > 0 {
		ids = append(ids, deletionsID(ns))
	}
	for _, id := range ids {
		unlock, err := st.Lock(ctx, id)
		if err != nil {
			return err
		}
		defer func() { _ = unlock() }()
	}
	meta, err := m.loadMeta(ctx, ns)
	if err != nil {
		return err
	}
	if meta.Migrating != "" {
		return conflict("命名空间正在迁移（%s），不能 Purge", meta.Migrating)
	}
	var ret retiringState
	if _, _, err := loadJSON(ctx, st, retiringID, &ret); err != nil {
		return err
	}
	if ret.Scopes == nil {
		ret.Scopes = map[string]retiringEntry{}
	}
	old := meta.Current
	ret.Scopes[scopeName(ns, old)] = retiringEntry{Namespace: ns, Scope: old, Since: at}
	meta.Current, meta.Purges = uuid.NewString(), meta.Purges+1
	values := []any{meta, ret}
	if m.cfg.RollbackWindow > 0 {
		var l DeletionLog
		if _, _, err := loadJSON(ctx, st, deletionsID(ns), &l); err != nil {
			return err
		}
		l.Purges = append(l.Purges, Deletion{ID: old, At: at})
		values = append(values, l)
	}
	writes := make([]durable.Write, len(ids))
	for i, id := range ids {
		snap, err := encodeSnapshot(id, values[i])
		if err != nil {
			return err
		}
		writes[i] = durable.Write{ID: id, Snapshot: snap}
	}
	return st.SaveAll(context.WithoutCancel(ctx), writes...)
}

// cleanupRetiring 物理删除全部 retiring 中的作用域：不持锁删除作用域并清空它的索引，加入超过 RetireGrace 的条目
// 再锁 retiring 移出（设计 4.3）。宽限期内每次清理都重做一遍，清掉提交后才写入旧作用域的迟到向量。
// 中途失败或崩溃时条目留在 retiring 中，下一次重做；每一步都可以重复执行。
func (m *Memory) cleanupRetiring(ctx context.Context) error {
	var ret retiringState
	if _, _, err := loadJSON(ctx, m.cfg.Durable, retiringID, &ret); err != nil {
		return err
	}
	var done []string
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(ret.Scopes)) {
		e := ret.Scopes[name]
		if err := m.retire(ctx, name, e); err != nil {
			errs = append(errs, err)
			continue
		}
		if m.now().Sub(e.Since) >= m.cfg.RetireGrace {
			done = append(done, name)
		}
	}
	if len(done) > 0 {
		errs = append(errs, editJSON(ctx, m.cfg.Durable, retiringID, func(r *retiringState) bool {
			for _, name := range done {
				delete(r.Scopes, name)
			}
			return true
		}))
	}
	return errors.Join(errs...)
}

// retire 删除一个旧作用域（含 state 与访问记录），再清空它的记录与实体索引命名空间。
func (m *Memory) retire(ctx context.Context, name string, e retiringEntry) error {
	if err := m.cfg.Durable.(durable.Scoped).RemoveScope(ctx, name); err != nil {
		return err
	}
	if m.cfg.Index == nil {
		return nil
	}
	var errs []error
	for _, sp := range indexSpaces {
		errs = append(errs, m.cfg.Index.Clear(ctx, sp.namespace(e.Namespace, e.Scope)))
	}
	return errors.Join(errs...)
}
