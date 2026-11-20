package management

import (
	"context"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
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
		cleanup, cancel := d.owner.persistCtx(ctx)
		defer cancel()
		if e := d.owner.owner(cleanup, true); err == nil {
			err = e
		}
	}()
	tick := time.NewTicker(d.s.cfg.Limits.OutboxPollInterval)
	defer tick.Stop()
	for {
		items, e := all[Delivery](ctx, d.s.cfg.Store, "outbox", "", Query{Statuses: []string{"pending", "applying"}})
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
			call, cancel := ctx, context.CancelFunc(func() {})
			if d.s.cfg.Limits.DeliveryTimeout > 0 {
				call, cancel = context.WithTimeout(ctx, d.s.cfg.Limits.DeliveryTimeout)
			}
			deliveryErr := d.s.cfg.Delivery.Accept(call, item)
			cancel()
			save, cancel := d.owner.persistCtx(ctx)
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
					cur.AvailableAt = d.s.cfg.Now().Add(redeliveryDelay(cur.Attempts, d.s.cfg.Limits.DeliveryBackoffMax))
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

// redeliveryDelay doubles from one second per attempt and stops at ceiling.
func redeliveryDelay(attempts int, ceiling time.Duration) time.Duration {
	delay := time.Second
	for i := 0; i < attempts && delay < ceiling; i++ {
		delay *= 2
	}
	return min(delay, ceiling)
}
