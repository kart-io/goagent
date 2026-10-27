// Package refund provides transaction-bound refund guards, independent of storage
// and identity providers. It records local refund effects, not remote payments.
package refund

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned by a Transaction for an absent record.
var ErrNotFound = errors.New("refund record not found")

// Request binds a business idempotency key to reviewed order facts. IDs must be
// namespaced by the host for its tenant; this package does not infer tenancy.
type Request struct {
	RequestID    string `json:"request_id"`
	OrderVersion int64  `json:"order_version"`
	ValidUntil   string `json:"valid_until"`
	OrderID      string `json:"order_id"`
	AmountCents  int64  `json:"amount_cents"`
	Currency     string `json:"currency"`
	PaidCents    int64  `json:"paid_cents"`
	DeliveryDays int    `json:"delivery_days"`
	PreparedAt   string `json:"prepared_at,omitempty"`
	ReceiptID    string `json:"receipt_id,omitempty"`
}

type Order struct {
	PaidCents    int64
	Currency     string
	DeliveryDays int
	Version      int64
}

// Store must serialize all transactions touching the same order, even across
// different request IDs and processes. On callback error it must roll back;
// on success it must atomically commit before returning nil. Unique request and
// receipt constraints are required. Do not implement this as read-then-write RPCs.
type Store interface {
	Transaction(context.Context, func(Transaction) error) error
}

// Transaction is valid only within the Store callback. Receipt returns
// ErrNotFound if no effect exists; TotalRefunded includes all committed requests.
type Transaction interface {
	Order(context.Context, string) (Order, error)
	Request(context.Context, string) (Request, string, error)
	Receipt(context.Context, string) (string, error)
	TotalRefunded(context.Context, string) (int64, error)
	InsertRequest(context.Context, Request) error
	Approve(context.Context, string, string, time.Time) error
	InsertRefund(context.Context, string, Request, string) error
}

// Authorizer must resolve an authenticated principal supplied by the host and
// enforce its permissions for this request. A caller-provided name is not proof
// of identity. Authorization is checked at approval and before a new effect.
type Authorizer interface {
	Authorize(context.Context, string, Request) error
}

type AuthorizeFunc func(context.Context, string, Request) error

func (f AuthorizeFunc) Authorize(ctx context.Context, actor string, req Request) error {
	if f == nil {
		return fmt.Errorf("authorizer is required")
	}
	return f(ctx, actor, req)
}

// Policy is the host's refund eligibility rule, evaluated against current facts.
// Positive amounts, currency, expiry and exact fact/version matching are always
// enforced by Service independently of this policy.
type Policy func(context.Context, Request, Order) error

type Service struct {
	Store      Store
	Authorizer Authorizer
	Policy     Policy
	Now        func() time.Time // nil uses time.Now; do not mutate configuration during use.
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s *Service) transact(ctx context.Context, f func(Transaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.Store == nil || s.Authorizer == nil || s.Policy == nil {
		return fmt.Errorf("store, authorizer and policy are required")
	}
	return s.Store.Transaction(ctx, f)
}
func sameIntent(a, b Request) bool {
	return a.RequestID == b.RequestID && a.OrderID == b.OrderID && a.AmountCents == b.AmountCents
}
func validIntent(req Request) error {
	if req.RequestID == "" || req.OrderID == "" || req.AmountCents <= 0 {
		return fmt.Errorf("business request ID, order ID and positive amount are required")
	}
	return nil
}

// Existing returns immutable reviewed facts for a previously registered intent.
// It never extends an approval deadline on retry.
func (s *Service) Existing(ctx context.Context, req Request) (out Request, found bool, err error) {
	if err = validIntent(req); err != nil {
		return
	}
	err = s.transact(ctx, func(tx Transaction) error {
		stored, _, e := tx.Request(ctx, req.RequestID)
		if errors.Is(e, ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		if !sameIntent(stored, req) {
			return fmt.Errorf("business request ID belongs to a different refund")
		}
		out, found = stored, true
		return nil
	})
	return
}

func (s *Service) checkCurrent(ctx context.Context, tx Transaction, req Request) error {
	deadline, err := time.Parse(time.RFC3339Nano, req.ValidUntil)
	if err != nil || !s.now().Before(deadline) {
		return fmt.Errorf("approval expired; create a new reviewed business request")
	}
	order, err := tx.Order(ctx, req.OrderID)
	if err != nil {
		return err
	}
	if order.Version != req.OrderVersion || order.PaidCents != req.PaidCents || order.Currency != req.Currency || order.DeliveryDays != req.DeliveryDays {
		return fmt.Errorf("order changed; a new review is required")
	}
	if req.AmountCents <= 0 || req.AmountCents > order.PaidCents || order.Currency == "" || order.Version <= 0 {
		return fmt.Errorf("invalid refund amount, currency or order version")
	}
	return s.Policy(ctx, req, order)
}

func (s *Service) Register(ctx context.Context, req Request) (out Request, err error) {
	if err = validIntent(req); err != nil {
		return
	}
	if req.ReceiptID != "" {
		return out, fmt.Errorf("new request cannot contain a receipt")
	}
	err = s.transact(ctx, func(tx Transaction) error {
		stored, _, e := tx.Request(ctx, req.RequestID)
		if e == nil {
			if !sameIntent(stored, req) {
				return fmt.Errorf("business request ID belongs to a different refund")
			}
			out = stored
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if e = s.checkCurrent(ctx, tx, req); e != nil {
			return e
		}
		if e = tx.InsertRequest(ctx, req); e != nil {
			return e
		}
		out = req
		return nil
	})
	return
}

func (s *Service) Approve(ctx context.Context, req Request, actor string) error {
	if err := validIntent(req); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("authenticated approver is required")
	}
	return s.transact(ctx, func(tx Transaction) error {
		stored, _, err := tx.Request(ctx, req.RequestID)
		if err != nil {
			return err
		}
		if stored != req {
			return fmt.Errorf("approval payload differs from registered request")
		}
		if err = s.Authorizer.Authorize(ctx, actor, stored); err != nil {
			return err
		}
		if err = s.checkCurrent(ctx, tx, stored); err != nil {
			return err
		}
		return tx.Approve(ctx, req.RequestID, actor, s.now().UTC())
	})
}

// Refund records the effect atomically with the cumulative balance check. Key is
// an execution audit key; RequestID deduplicates retries across workflow runs.
// A committed receipt is returned before expiry/authorization checks so recovery
// can acknowledge it after an approval expires or permissions are revoked.
func (s *Service) Refund(ctx context.Context, key string, req Request) (receipt string, err error) {
	if err = validIntent(req); err != nil {
		return
	}
	if key == "" {
		return "", fmt.Errorf("execution key is required")
	}
	err = s.transact(ctx, func(tx Transaction) error {
		stored, actor, e := tx.Request(ctx, req.RequestID)
		if e != nil {
			return e
		}
		if stored != req {
			return fmt.Errorf("business request ID belongs to a different refund")
		}
		receipt, e = tx.Receipt(ctx, req.RequestID)
		if e == nil {
			if receipt == "" {
				return fmt.Errorf("empty committed receipt")
			}
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if actor == "" {
			return fmt.Errorf("refund requires an authorized approval")
		}
		if e = s.Authorizer.Authorize(ctx, actor, stored); e != nil {
			return e
		}
		if e = s.checkCurrent(ctx, tx, stored); e != nil {
			return e
		}
		refunded, e := tx.TotalRefunded(ctx, req.OrderID)
		if e != nil {
			return e
		}
		if refunded < 0 || refunded > req.PaidCents || req.AmountCents > req.PaidCents-refunded {
			return fmt.Errorf("cumulative refund exceeds paid amount")
		}
		hash := sha256.Sum256([]byte(req.RequestID))
		receipt = "demo-refund-" + hex.EncodeToString(hash[:])
		return tx.InsertRefund(ctx, key, req, receipt)
	})
	if err != nil {
		receipt = ""
	}
	return
}
