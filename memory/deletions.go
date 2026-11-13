package memory

import (
	"context"
	"slices"
	"time"

	"github.com/kart-io/goagent/store"
)

func deletionsID(ns []string) string { return "memory/deletions/" + store.NamespaceKey(ns) }

// Deletion 是删除日志的一条：被遗忘的记录 ID，或被 Purge 的作用域，以及时间。
type Deletion struct {
	ID string    `json:"id"`
	At time.Time `json:"at"`
}

// DeletionLog 是一个命名空间在 RollbackWindow 内的遗忘与 Purge。数据回滚到旧存储之后，宿主据此重放，使被要求删除的内容不会复活。
type DeletionLog struct {
	Forgotten []Deletion `json:"forgotten"`
	Purges    []Deletion `json:"purges"`
}

// Deletions 返回当前命名空间的删除日志；RollbackWindow 为 0 时不写日志，返回空。
func (m *Memory) Deletions(ctx context.Context) (DeletionLog, error) {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return DeletionLog{}, err
	}
	var l DeletionLog
	_, _, err = loadJSON(ctx, m.cfg.Durable, deletionsID(ns), &l)
	return l, err
}

// logDeletion 在删除日志的锁内修改并保存日志；位于根作用域，调用方不得持有 state 的锁（加锁顺序见设计 4.2）。
func (m *Memory) logDeletion(ctx context.Context, ns []string, fn func(*DeletionLog)) error {
	return editJSON(ctx, m.cfg.Durable, deletionsID(ns), func(l *DeletionLog) bool { fn(l); return true })
}

// pruneDeletions 删除早于 RollbackWindow 的日志条目；日志不存在或没有过期条目时不写入。
func (m *Memory) pruneDeletions(ctx context.Context, ns []string, now time.Time) error {
	id := deletionsID(ns)
	var l DeletionLog
	ok, _, err := loadJSON(ctx, m.cfg.Durable, id, &l)
	if err != nil || !ok {
		return err
	}
	old := func(d Deletion) bool { return now.Sub(d.At) > m.cfg.RollbackWindow }
	if !slices.ContainsFunc(l.Forgotten, old) && !slices.ContainsFunc(l.Purges, old) {
		return nil
	}
	return editJSON(ctx, m.cfg.Durable, id, func(l *DeletionLog) bool {
		n := len(l.Forgotten) + len(l.Purges)
		l.Forgotten, l.Purges = slices.DeleteFunc(l.Forgotten, old), slices.DeleteFunc(l.Purges, old)
		return len(l.Forgotten)+len(l.Purges) != n
	})
}
