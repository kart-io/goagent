package management

import (
	"context"
	"github.com/kart-io/goagent/codec/json"
	"strings"
)

// Prune 显式回收过期且无未确认依赖的管理记录；会话日志与版本仍由宿主单独保留。
func (s *Service) Prune(ctx context.Context, a Actor) (map[string]int, error) {
	counts := map[string]int{}
	if e := s.auth(ctx, a, "management.prune", "management", ""); e != nil {
		return counts, e
	}
	e := s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		now := s.cfg.Now()
		keys, e := all[Idempotency](ctx, tx, "idempotency", a.TenantID, Query{Limit: 10000})
		if e != nil {
			return e
		}
		for _, key := range keys {
			if now.Before(key.ExpiresAt) {
				continue
			}
			if strings.HasPrefix(key.Operation, "task.") {
				var ref Meta
				if json.Unmarshal(key.Response, &ref) == nil {
					if task, e := read[Task](ctx, tx, "tasks", a.TenantID, ref.ID); e == nil && !task.State.Terminal() {
						continue
					}
				}
			}
			if e = tx.Delete(ctx, "idempotency", a.TenantID, key.ID, key.Revision); e != nil {
				return e
			}
			counts["idempotency"]++
		}
		tasks, e := all[Task](ctx, tx, "tasks", a.TenantID, Query{Limit: 10000})
		if e != nil {
			return e
		}
		for _, task := range tasks {
			if !task.State.Terminal() || task.FinishedAt == nil || now.Before(task.FinishedAt.Add(s.cfg.Limits.TaskRetention)) {
				continue
			}
			commands, e := all[Command](ctx, tx, "commands", a.TenantID, Query{TaskID: task.ID, Limit: 10000})
			if e != nil {
				return e
			}
			safe := true
			for _, c := range commands {
				if c.State != "applied" && c.State != "conflict" {
					safe = false
				}
			}
			outbox, e := all[Delivery](ctx, tx, "outbox", a.TenantID, Query{Limit: 10000})
			if e != nil {
				return e
			}
			for _, d := range outbox {
				if d.ResourceID == task.ID && d.State != "done" {
					safe = false
				}
			}
			if !safe {
				continue
			}
			for _, kind := range []string{"approvals", "attempts", "commands"} {
				docs, e := tx.List(ctx, kind, Selection{TenantID: a.TenantID, Query: Query{TaskID: task.ID, Limit: 10000}, Visibility: Visibility{All: true}})
				if e != nil {
					return e
				}
				for _, d := range docs {
					if e = tx.Delete(ctx, kind, a.TenantID, d.ID, d.Revision); e != nil {
						return e
					}
				}
			}
			if e = tx.Delete(ctx, "tasks", a.TenantID, task.ID, task.Revision); e != nil {
				return e
			}
			counts["tasks"]++
		}
		audit, e := all[Audit](ctx, tx, "audit", a.TenantID, Query{Limit: 10000})
		if e != nil {
			return e
		}
		for _, entry := range audit {
			if now.Before(entry.CreatedAt.Add(s.cfg.Limits.AuditRetention)) {
				continue
			}
			if e = tx.Delete(ctx, "audit", a.TenantID, entry.ID, entry.Revision); e != nil {
				return e
			}
			counts["audit"]++
		}
		return nil
	})
	return counts, e
}
