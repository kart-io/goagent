package management

import (
	"context"
)

// retryUnconfirmed 处理生产者已退出但管理确认失败的记录；已落盘的完成信封先于业务重入核对。
func (w *Worker) retryUnconfirmed(ctx context.Context) error {
	commands, e := all[Command](ctx, w.s.cfg.Store, "commands", "", Query{Statuses: []string{"applying"}, Limit: 10000})
	if e != nil {
		return e
	}
	for _, c := range commands {
		w.mu.Lock()
		live := w.live[c.TaskID] != nil
		w.mu.Unlock()
		if live {
			continue
		}
		if e = w.s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
			cur, e := read[Command](ctx, tx, "commands", c.TenantID, c.ID)
			if e != nil {
				return e
			}
			if cur.State != "applying" {
				return nil
			}
			t, e := read[Task](ctx, tx, "tasks", c.TenantID, c.TaskID)
			if e != nil {
				return e
			}
			rev := cur.Revision
			cur.Revision++
			cur.State = "pending"
			if t.State.Terminal() {
				cur.State = "applied"
			} else {
				old := t.Revision
				t.Revision++
				t.State = Queued
				t.RecoveryNeeded = true
				if e = update(ctx, tx, "tasks", t, old); e != nil {
					return e
				}
			}
			return update(ctx, tx, "commands", cur, rev)
		}); e != nil {
			return e
		}
	}
	return nil
}
