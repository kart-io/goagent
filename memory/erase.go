package memory

import (
	"context"
	"time"
)

// EraseFunc 清除依据 (ns, scope) 下记忆生成的派生副本；必须幂等。
type EraseFunc func(ctx context.Context, ns []string, scope string) error

// ErasesOnDelete 报告是否配置了 OnErase。
func (m *Memory) ErasesOnDelete() bool { return m.cfg.OnErase != nil }

// EraseWithin 返回生效的擦除时限（含默认值），供 Builder 校验 TTL。
func (m *Memory) EraseWithin() time.Duration { return m.cfg.EraseWithin }

// registerErase 登记 (ns, scope) 的擦除，Until 取 max(已登记的, 现在 + EraseWithin)：
// 窗口内的每次提交都要把重做延续到自己的时限，register 按键去重会保留旧的 Until。调用方持有该作用域的 state 锁。
func (m *Memory) registerErase(ctx context.Context, ns []string, scope string) error {
	e := queueEntry{Namespace: ns, Scope: scope, Reason: queueErase, Until: m.now().Add(m.cfg.EraseWithin)}
	return editJSON(ctx, m.cfg.Durable, queueID(ns), func(q *queueState) bool {
		if old, ok := q.Entries[e.key()]; ok && !old.Until.Before(e.Until) {
			return false
		}
		if q.Entries == nil {
			q.Entries = map[string]queueEntry{}
		}
		q.Entries[e.key()] = e
		return true
	})
}

// eraseCommitted 在提交之后擦除一次；失败不返回（已经提交），由维护按登记重做。
func (m *Memory) eraseCommitted(ctx context.Context, ns []string, scope string) {
	if m.cfg.OnErase != nil && m.cfg.OnErase(ctx, ns, scope) != nil {
		m.eraseErrors.Add(1)
	}
}

// EraseErrors 返回本实例提交之后立即擦除失败的次数；这些擦除由维护在 EraseWithin 内重做。
func (m *Memory) EraseErrors() int64 { return m.eraseErrors.Load() }

// maintainErase 处理当前作用域的擦除登记：每轮擦除一次，直到开始时刻不早于 Until 的一次成功后注销。
// 早于 Until 的擦除可能漏掉之后才写入的派生副本，所以必须重做到时限。
// 注销在 state 锁内核对：期间有新提交延长了 Until 时保留登记。
func (m *Memory) maintainErase(ctx context.Context, e queueEntry) error {
	if m.cfg.OnErase == nil {
		return m.settle(ctx, e, nil)
	}
	start := m.now()
	if err := m.cfg.OnErase(ctx, e.Namespace, e.Scope); err != nil {
		return err
	}
	if start.Before(e.Until) {
		return nil
	}
	return m.settle(ctx, e, func(*tx) bool {
		var q queueState
		if _, _, err := loadJSON(ctx, m.cfg.Durable, queueID(e.Namespace), &q); err != nil {
			return true
		}
		cur, ok := q.Entries[e.key()]
		return ok && cur.Until.After(e.Until)
	})
}
