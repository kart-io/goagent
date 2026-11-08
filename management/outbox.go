package management

import (
	"context"
	agentErrors "github.com/kart-io/goagent/errors"
	"time"
)

// OutboxDispatcher 至少一次投递稳定 Delivery.ID；接收端必须据此去重。
type OutboxDispatcher struct {
	s     *Service
	owner *Worker
}

func NewOutboxDispatcher(s *Service) *OutboxDispatcher {
	w := NewWorker(s)
	w.slot = "outbox"
	return &OutboxDispatcher{s, w}
}
func (d *OutboxDispatcher) Run(ctx context.Context) (err error) {
	if d.s.cfg.Delivery == nil {
		return agentErrors.ErrManagementUnsupported
	}
	if err = d.owner.owner(ctx, false); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if e := d.owner.owner(cleanup, true); err == nil {
			err = e
		}
	}()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		items, e := all[Delivery](ctx, d.s.cfg.Store, "outbox", "", Query{Statuses: []string{"pending", "applying"}, Limit: 10000})
		if e != nil && ctx.Err() == nil {
			return e
		}
		for _, item := range items {
			if ctx.Err() != nil {
				return nil
			}
			if item.AvailableAt.After(d.s.cfg.Now()) {
				continue
			}
			err = d.s.cfg.Store.WithinTx(ctx, func(tx Tx) error {
				cur, e := read[Delivery](ctx, tx, "outbox", item.TenantID, item.ID)
				if e != nil {
					return e
				}
				if cur.State == "done" {
					return agentErrors.ErrManagementConflict
				}
				old := cur.Revision
				cur.Revision++
				cur.State = "applying"
				cur.Attempts++
				item = cur
				return update(ctx, tx, "outbox", cur, old)
			})
			if err != nil {
				continue
			}
			call, cancel := context.WithTimeout(ctx, 5*time.Second)
			deliveryErr := d.s.cfg.Delivery.Accept(call, item)
			cancel()
			save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			err = d.s.cfg.Store.WithinTx(save, func(tx Tx) error {
				cur, e := read[Delivery](save, tx, "outbox", item.TenantID, item.ID)
				if e != nil {
					return e
				}
				old := cur.Revision
				cur.Revision++
				if deliveryErr == nil {
					cur.State = "done"
				} else {
					cur.State = "pending"
					delay := time.Second * time.Duration(1<<min(cur.Attempts, 8))
					cur.AvailableAt = d.s.cfg.Now().Add(delay)
				}
				return update(save, tx, "outbox", cur, old)
			})
			cancel()
			if err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
