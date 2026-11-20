package management

import (
	"context"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
)

// Prune 显式回收过期且无未确认依赖的管理记录；会话日志与版本仍由宿主单独保留。
// 保留期为 0 的类别永不回收；IdempotencyRetention 为 0 时写入的记录没有过期时间。
func (s *Service) Prune(ctx context.Context, a Actor) (map[string]int, error) {
	counts := map[string]int{}
	if e := s.auth(ctx, a, "management.prune", "management", ""); e != nil {
		return counts, e
	}
	e := s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
		now := s.cfg.Now()
		if e := each(ctx, tx, "idempotency", a.TenantID, Query{}, func(key Idempotency) (bool, error) {
			if key.ExpiresAt.IsZero() || now.Before(key.ExpiresAt) {
				return false, nil
			}
			if strings.HasPrefix(key.Operation, "task.") {
				var ref Meta
				if json.Unmarshal(key.Response, &ref) == nil {
					if task, e := read[Task](ctx, tx, "tasks", a.TenantID, ref.ID); e == nil && !task.State.Terminal() {
						return false, nil
					}
				}
			}
			counts["idempotency"]++
			return false, tx.Delete(ctx, "idempotency", a.TenantID, key.ID, key.Revision)
		}); e != nil {
			return e
		}
		if s.cfg.Limits.TaskRetention > 0 {
			if e := s.pruneTasks(ctx, tx, a.TenantID, now, counts); e != nil {
				return e
			}
		}
		if s.cfg.Limits.AuditRetention == 0 {
			return nil
		}
		return each(ctx, tx, "audit", a.TenantID, Query{}, func(entry Audit) (bool, error) {
			if now.Before(entry.CreatedAt.Add(s.cfg.Limits.AuditRetention)) {
				return false, nil
			}
			counts["audit"]++
			return false, tx.Delete(ctx, "audit", a.TenantID, entry.ID, entry.Revision)
		})
	})
	return counts, e
}

func (s *Service) pruneTasks(ctx context.Context, tx Tx, tenant string, now time.Time, counts map[string]int) error {
	undelivered := map[string]bool{}
	if e := each(ctx, tx, "outbox", tenant, Query{}, func(d Delivery) (bool, error) {
		if d.State != "done" {
			undelivered[d.ResourceID] = true
		}
		return false, nil
	}); e != nil {
		return e
	}
	return each(ctx, tx, "tasks", tenant, Query{}, func(task Task) (bool, error) {
		if !task.State.Terminal() || task.FinishedAt == nil || now.Before(task.FinishedAt.Add(s.cfg.Limits.TaskRetention)) || undelivered[task.ID] {
			return false, nil
		}
		safe := true
		if e := each(ctx, tx, "commands", tenant, Query{TaskID: task.ID}, func(c Command) (bool, error) {
			safe = c.State == "applied" || c.State == "conflict"
			return !safe, nil
		}); e != nil || !safe {
			return false, e
		}
		for _, kind := range []string{"approvals", "attempts", "commands"} {
			if e := each(ctx, tx, kind, tenant, Query{TaskID: task.ID}, func(m Meta) (bool, error) {
				return false, tx.Delete(ctx, kind, tenant, m.ID, m.Revision)
			}); e != nil {
				return false, e
			}
		}
		counts["tasks"]++
		return false, tx.Delete(ctx, "tasks", tenant, task.ID, task.Revision)
	})
}
