package refund_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/workflow/refund"
	"github.com/stretchr/testify/require"
)

// A transactional test store copies state and commits only on success.
// Cross-process serialization is tested against the example's SQLite adapter.
type memoryStore struct {
	tx        memoryTx
	commitErr error
}
type memoryTx struct {
	order          refund.Order
	req            refund.Request
	actor, receipt string
	total          int64
}

func (s *memoryStore) Transaction(_ context.Context, f func(refund.Transaction) error) error {
	next := s.tx
	if err := f(&next); err != nil {
		return err
	}
	if s.commitErr != nil {
		return s.commitErr
	}
	s.tx = next
	return nil
}
func (t *memoryTx) Order(context.Context, string) (refund.Order, error) { return t.order, nil }
func (t *memoryTx) Request(_ context.Context, id string) (refund.Request, string, error) {
	if t.req.RequestID != id {
		return refund.Request{}, "", refund.ErrNotFound
	}
	return t.req, t.actor, nil
}
func (t *memoryTx) Receipt(context.Context, string) (string, error) {
	if t.receipt == "" {
		return "", refund.ErrNotFound
	}
	return t.receipt, nil
}
func (t *memoryTx) TotalRefunded(context.Context, string) (int64, error)    { return t.total, nil }
func (t *memoryTx) InsertRequest(_ context.Context, r refund.Request) error { t.req = r; return nil }
func (t *memoryTx) Approve(_ context.Context, _, actor string, _ time.Time) error {
	t.actor = actor
	return nil
}
func (t *memoryTx) InsertRefund(_ context.Context, _ string, r refund.Request, receipt string) error {
	t.receipt = receipt
	t.total += r.AmountCents
	return nil
}

func TestServiceRevocationCommitFailureAndRecovery(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	allowed := true
	db := &memoryStore{tx: memoryTx{order: refund.Order{PaidCents: 1000, Currency: "CNY", Version: 1, DeliveryDays: 2}}}
	svc := &refund.Service{Store: db, Now: func() time.Time { return now },
		Authorizer: refund.AuthorizeFunc(func(_ context.Context, actor string, r refund.Request) error {
			if !allowed || actor != "verified-user" || r.OrderID != "tenant-A/order-1" {
				return errors.New("permission denied")
			}
			return nil
		}), Policy: func(context.Context, refund.Request, refund.Order) error { return nil }}
	req := refund.Request{RequestID: "tenant-A/refund-1", OrderID: "tenant-A/order-1", AmountCents: 500, PaidCents: 1000, Currency: "CNY", OrderVersion: 1, DeliveryDays: 2, ValidUntil: now.Add(time.Minute).Format(time.RFC3339Nano)}
	registered, err := svc.Register(ctx, req)
	require.NoError(t, err)
	require.Equal(t, req, registered)
	// Retrying registration never refreshes reviewed facts or expiry.
	renewed := req
	renewed.ValidUntil = now.Add(time.Hour).Format(time.RFC3339Nano)
	registered, err = svc.Register(ctx, renewed)
	require.NoError(t, err)
	require.Equal(t, req, registered)
	require.NoError(t, svc.Approve(ctx, req, "verified-user"))
	allowed = false
	receipt, err := svc.Refund(ctx, "execution-1", req)
	require.ErrorContains(t, err, "permission denied")
	require.Empty(t, receipt)
	require.Zero(t, db.tx.total)
	allowed = true
	db.commitErr = errors.New("commit unavailable")
	receipt, err = svc.Refund(ctx, "execution-1", req)
	require.ErrorContains(t, err, "commit unavailable")
	require.Empty(t, receipt)
	require.Zero(t, db.tx.total)
	db.commitErr = nil
	receipt, err = svc.Refund(ctx, "execution-1", req)
	require.NoError(t, err)
	require.NotEmpty(t, receipt)
	allowed = false
	now = now.Add(time.Hour)
	db.tx.order.Version++
	retry, err := svc.Refund(ctx, "different-execution", req)
	require.NoError(t, err)
	require.Equal(t, receipt, retry)
	require.EqualValues(t, 500, db.tx.total)
	altered := req
	altered.AmountCents++
	_, err = svc.Refund(ctx, "execution-3", altered)
	require.ErrorContains(t, err, "different refund")
}

func TestServiceFailsClosed(t *testing.T) {
	ctx := context.Background()
	svc := &refund.Service{}
	_, _, err := svc.Existing(ctx, refund.Request{RequestID: "r", OrderID: "o", AmountCents: 1})
	require.ErrorContains(t, err, "required")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = svc.Existing(canceled, refund.Request{RequestID: "r", OrderID: "o", AmountCents: 1})
	require.ErrorIs(t, err, context.Canceled)
}
