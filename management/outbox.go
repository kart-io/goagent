package management

import (
	"context"
	"fmt"
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
	// saveFailures 统计连续的结果保存失败：单条失败不让投递器退出，按退避等待后整轮重试。
	saveFailures := 0
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
			deliveryErr := d.accept(call, item)
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
				if ctx.Err() != nil {
					return nil
				}
				d.owner.report("outbox_save", err)
				saveFailures++
				break
			}
			saveFailures = 0
		}
		wait, stop := tick.C, func() bool { return false }
		if saveFailures > 0 {
			timer := time.NewTimer(redeliveryDelay(saveFailures-1, d.s.cfg.Limits.DeliveryBackoffMax))
			wait, stop = timer.C, timer.Stop
		}
		select {
		case <-ctx.Done():
			stop()
			return nil
		case <-wait:
		}
	}
}

// accept 把接收端的 panic 计为一次投递失败，投递器继续运行。
func (d *OutboxDispatcher) accept(ctx context.Context, item Delivery) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("delivery sink panic: %v", r)
		}
	}()
	return d.s.cfg.Delivery.Accept(ctx, item)
}

// redeliveryDelay doubles from one second per attempt and stops at ceiling.
func redeliveryDelay(attempts int, ceiling time.Duration) time.Duration {
	delay := time.Second
	for i := 0; i < attempts && delay < ceiling; i++ {
		delay *= 2
	}
	return min(delay, ceiling)
}
