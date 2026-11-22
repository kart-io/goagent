package memory

import (
	"context"
	"errors"
	"time"
)

type derivationKey struct{}
type derivationUnit struct {
	kind string
	seq  int64
}

const errDerivationChanged = sentinel("派生输入已变更")
const errPassLimit = sentinel("本轮派生单元已达上限")

func currentDerivation(ctx context.Context, t *tx) error {
	if u, ok := ctx.Value(derivationKey{}).(derivationUnit); ok && t.Derivations.m[u.kind].DirtySeq != u.seq {
		return errDerivationChanged
	}
	return nil
}

func pendingEpisode(r Record) bool { return r.Kind == KindEpisode && r.Valid() && !r.Reflected }

// 只检查本次写入的对象；请求序号、访问统计和派生输出都不是新输入。
func (m *Memory) dirtyInputs(ctx context.Context, old state, t *tx) error {
	episode := false
	u, _ := ctx.Value(derivationKey{}).(derivationUnit)
	if u.kind != "reflection" {
		for id := range t.Records.changes {
			a, b := old.Records[id], t.Records.m[id]
			if pendingEpisode(a) != pendingEpisode(b) || pendingEpisode(a) && (a.Content != b.Content || a.Importance != b.Importance || refOf(a) != refOf(b)) {
				episode = true
				break
			}
		}
	}
	graph := false
	for id := range t.Edges.changes {
		a, b := old.Edges[id], t.Edges.m[id]
		if a.Valid() != b.Valid() || a.Valid() && (a.Subject != b.Subject || a.Object != b.Object || a.Fact != b.Fact || !a.ValidFrom.Equal(b.ValidFrom) || a.Incarnation != b.Incarnation || a.ContentRev != b.ContentRev) {
			graph = true
			break
		}
	}
	_, maintaining := ctx.Value(maintenanceKey{}).(string)
	repair := false
	if maintaining && m.cfg.Index != nil {
		for id := range t.Records.changes {
			if activeRecordKey(old.Records[id]) != activeRecordKey(t.Records.m[id]) {
				repair = true
				break
			}
		}
	}
	for id := range t.Entities.changes {
		a, b := old.Entities[id], t.Entities.m[id]
		if maintaining && m.cfg.Index != nil && entityKey(a) != entityKey(b) {
			repair = true
		}
		if a.Name != b.Name || a.Incarnation != b.Incarnation || a.ContentRev != b.ContentRev {
			graph = true
		}
	}
	if repair {
		if err := m.register(ctx, queueEntry{Namespace: t.ns, Scope: t.scope, Reason: queueIndex}); err != nil {
			return err
		}
	}
	if !episode && !graph {
		return nil
	}
	if err := m.register(ctx, queueEntry{Namespace: t.ns, Scope: t.scope, Reason: queueDerivations}); err != nil {
		return err
	}
	bump := func(kind string) {
		w, _ := t.Derivations.Get(kind)
		w.DirtySeq++
		t.Derivations.Put(kind, w)
	}
	if graph {
		bump("community")
	}
	if episode {
		bump("reflection")
	}
	return nil
}

func (m *Memory) runDerivations(ctx context.Context, e queueEntry) error {
	return m.runDerivationKinds(ctx, e, []string{"community", "reflection"})
}
func (m *Memory) runDerivationKinds(ctx context.Context, e queueEntry, kinds []string) error {
	ctx = m.maintenanceContext(ctx, e.Namespace, e.Scope)
	var errs []error
	for _, kind := range kinds {
		scope, s, err := m.read(ctx, e.Namespace)
		if err != nil {
			return err
		}
		if scope != e.Scope {
			return m.unregister(ctx, e)
		}
		w := s.Derivations[kind]
		root := m.cfg.Background.Budget.Configuration().RootKey
		if w.BlockedBudgetRootKey != "" && w.BlockedBudgetRootKey != root {
			_, err = m.mutate(ctx, e.Namespace, scope, func(t *tx) error {
				cur, _ := t.Derivations.Get(kind)
				if cur.BlockedBudgetRootKey != w.BlockedBudgetRootKey {
					return errNoChange
				}
				cur.BlockedBudgetRootKey = ""
				if cur.LastErrorCode == "budget_exhausted" {
					cur.LastErrorCode = ""
				}
				t.Derivations.Put(kind, cur)
				return nil
			})
			if err != nil {
				return err
			}
			w.BlockedBudgetRootKey = ""
		}
		if w.DirtySeq <= w.DoneSeq || w.Paused || w.RetryAt.After(m.now()) || w.BlockedBudgetRootKey == root {
			continue
		}
		unit := context.WithValue(ctx, derivationKey{}, derivationUnit{kind, w.DirtySeq})
		if kind == "community" {
			err = m.syncCommunities(unit, e.Namespace)
		} else {
			err = m.reflect(unit)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errDerivationChanged) {
			continue
		}
		result := err
		_, err = m.mutate(ctx, e.Namespace, scope, func(t *tx) error {
			cur, _ := t.Derivations.Get(kind)
			if cur.DirtySeq != w.DirtySeq {
				return errNoChange
			}
			cur.finish(result, root, m.now())
			t.Derivations.Put(kind, cur)
			return nil
		})
		if !errors.Is(result, errPassLimit) && !errors.Is(result, errBackgroundBudget) {
			errs = append(errs, result)
		}
		errs = append(errs, err)
	}
	return errors.Join(append(errs, m.settle(ctx, e, hasPendingDerivations))...)
}

func (cur *DerivationWork) finish(result error, root string, now time.Time) {
	cur.GeneratorVersion = "1"
	switch {
	case result == nil:
		cur.DoneSeq = cur.DirtySeq
		cur.Attempts = 0
		cur.LastErrorCode = ""
		cur.RetryAt = time.Time{}
	case errors.Is(result, errBackgroundBudget):
		cur.BlockedBudgetRootKey = root
		cur.LastErrorCode = "budget_exhausted"
	case errors.Is(result, errPassLimit):
		cur.LastErrorCode = "deferred_pass_limit"
	case errors.Is(result, errInputOversized):
		cur.LastErrorCode = "input_oversized"
		cur.Paused = true
	default:
		cur.Attempts++
		cur.LastErrorCode = "generation_failed"
		cur.Paused = cur.Attempts >= 5
		cur.RetryAt = now.Add(min(5*time.Minute, time.Second<<min(cur.Attempts-1, 9)))
	}

}
