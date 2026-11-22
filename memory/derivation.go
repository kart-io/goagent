package memory

import (
	"context"
	"time"
)

// DerivationWork 保存一个派生类别的输入进度、技术失败和维护预算阻塞状态。
type DerivationWork struct {
	DirtySeq, DoneSeq    int64
	GeneratorVersion     string
	Attempts             int
	RetryAt              time.Time
	LastErrorCode        string
	Paused               bool
	BlockedBudgetRootKey string
}

var derivationKind = kind[DerivationWork]{
	clone:     func(w DerivationWork) DerivationWork { return w },
	normalize: func(*DerivationWork, []string) bool { return true },
	size: func(key string, w DerivationWork) int {
		return 56 + len(key) + len(w.GeneratorVersion) + len(w.LastErrorCode) + len(w.BlockedBudgetRootKey)
	},
}

func hasPendingDerivations(t *tx) bool {
	for _, w := range t.Derivations.m {
		if w.DirtySeq > w.DoneSeq {
			return true
		}
	}
	return false
}

// MarkDerivationsDirty 为当前命名空间的社区和反思登记新输入，并解除技术暂停。
func (m *Memory) MarkDerivationsDirty(ctx context.Context) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	_, err = m.mutate(ctx, ns, "", func(t *tx) error {
		for _, key := range []string{"community", "reflection"} {
			w, _ := t.Derivations.Get(key)
			w.DirtySeq++
			w.Paused = false
			w.Attempts = 0
			w.RetryAt = time.Time{}
			w.LastErrorCode = ""
			t.Derivations.Put(key, w)
		}
		return nil
	}, withBeforeSave(func(ctx context.Context, t *tx) error {
		return m.register(ctx, queueEntry{Namespace: t.ns, Scope: t.scope, Reason: queueDerivations})
	}))
	return err
}
