package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/kart-io/goagent/workflow/refund"
	_ "github.com/mattn/go-sqlite3"
)

// This is a local test backend, not a payment service. Every balance check and
// insert is serialized in one SQLite write transaction, across processes too.
type refundLedger struct {
	db  *sql.DB
	now func() time.Time
}

type orderRecord = refund.Order

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func openLedger(dir string) (*refundLedger, error) {
	path, err := filepath.Abs(filepath.Join(dir, "refunds.sqlite"))
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "_busy_timeout=5000&_journal_mode=WAL&_synchronous=FULL&_txlock=immediate"}
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	l := &refundLedger{db: db, now: time.Now}
	if err = l.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return l, nil
}

func (l *refundLedger) initialize() error {
	tx, err := l.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var version, tables int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == 2 {
		return tx.Commit()
	}
	if err = tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table'").Scan(&tables); err != nil {
		return err
	}
	if version != 0 || tables != 0 {
		return fmt.Errorf("incompatible ledger schema; preserve old data and use a new demo directory")
	}
	_, err = tx.Exec(`
		CREATE TABLE orders (order_id TEXT PRIMARY KEY, paid_cents INTEGER NOT NULL, currency TEXT NOT NULL,
		 delivery_days INTEGER NOT NULL, version INTEGER NOT NULL);
		INSERT INTO orders VALUES ('DEMO-1001',9900,'CNY',3,1),('DEMO-EXPIRED',9900,'CNY',30,1);
		CREATE TABLE refund_requests (request_id TEXT PRIMARY KEY, state_json BLOB NOT NULL,
		 approved_by TEXT NOT NULL DEFAULT '', approved_at TEXT NOT NULL DEFAULT '');
		CREATE TABLE refunds (request_id TEXT PRIMARY KEY, execution_key TEXT NOT NULL,
		 order_id TEXT NOT NULL, amount_cents INTEGER NOT NULL CHECK(amount_cents > 0),
		 currency TEXT NOT NULL, receipt_id TEXT NOT NULL UNIQUE);
		PRAGMA user_version = 2;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func readOrder(ctx context.Context, q rowQuerier, id string) (orderRecord, error) {
	var order orderRecord
	err := q.QueryRowContext(ctx, "SELECT paid_cents, currency, delivery_days, version FROM orders WHERE order_id = ?", id).
		Scan(&order.PaidCents, &order.Currency, &order.DeliveryDays, &order.Version)
	return order, err
}

func (l *refundLedger) order(ctx context.Context, id string) (orderRecord, error) {
	return readOrder(ctx, l.db, id)
}

func readRequest(ctx context.Context, q rowQuerier, id string) (refundState, string, error) {
	var data []byte
	var actor string
	var state refundState
	err := q.QueryRowContext(ctx, "SELECT state_json, approved_by FROM refund_requests WHERE request_id = ?", id).Scan(&data, &actor)
	if err == nil {
		err = json.Unmarshal(data, &state)
	}
	return state, actor, err
}

// Transaction adapts the local SQLite backend to the reusable guard service.
func (l *refundLedger) Transaction(ctx context.Context, f func(refund.Transaction) error) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = f(ledgerTx{tx}); err != nil {
		return err
	}
	return tx.Commit()
}

type ledgerTx struct{ *sql.Tx }

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return refund.ErrNotFound
	}
	return err
}
func (t ledgerTx) Order(ctx context.Context, id string) (refund.Order, error) {
	order, err := readOrder(ctx, t.Tx, id)
	return order, notFound(err)
}
func (t ledgerTx) Request(ctx context.Context, id string) (refund.Request, string, error) {
	req, actor, err := readRequest(ctx, t.Tx, id)
	return req, actor, notFound(err)
}
func (t ledgerTx) Receipt(ctx context.Context, id string) (string, error) {
	var receipt string
	err := t.QueryRowContext(ctx, "SELECT receipt_id FROM refunds WHERE request_id = ?", id).Scan(&receipt)
	return receipt, notFound(err)
}
func (t ledgerTx) TotalRefunded(ctx context.Context, id string) (int64, error) {
	var amount int64
	err := t.QueryRowContext(ctx, "SELECT COALESCE(SUM(amount_cents),0) FROM refunds WHERE order_id = ?", id).Scan(&amount)
	return amount, err
}
func (t ledgerTx) InsertRequest(ctx context.Context, req refund.Request) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = t.ExecContext(ctx, "INSERT INTO refund_requests(request_id,state_json) VALUES (?,?)", req.RequestID, data)
	return err
}
func (t ledgerTx) Approve(ctx context.Context, id, actor string, at time.Time) error {
	_, err := t.ExecContext(ctx, "UPDATE refund_requests SET approved_by = ?, approved_at = ? WHERE request_id = ?", actor, at.Format(time.RFC3339Nano), id)
	return err
}
func (t ledgerTx) InsertRefund(ctx context.Context, key string, req refund.Request, receipt string) error {
	_, err := t.ExecContext(ctx, "INSERT INTO refunds VALUES (?,?,?,?,?,?)", req.RequestID, key, req.OrderID, req.AmountCents, req.Currency, receipt)
	return err
}
func (l *refundLedger) service() *refund.Service {
	return &refund.Service{Store: l, Now: l.now,
		Authorizer: refund.AuthorizeFunc(func(_ context.Context, actor string, _ refund.Request) error {
			if actor != "demo-reviewer" {
				return fmt.Errorf("approver is not authorized by the demo policy")
			}
			return nil
		}),
		Policy: func(_ context.Context, _ refund.Request, order refund.Order) error {
			if order.DeliveryDays < 0 || order.DeliveryDays > 7 {
				return fmt.Errorf("refund does not satisfy the synthetic policy")
			}
			return nil
		},
	}
}
func (l *refundLedger) existingRequest(ctx context.Context, state refundState) (refundState, bool, error) {
	return l.service().Existing(ctx, state)
}
func (l *refundLedger) register(ctx context.Context, state refundState) (refundState, error) {
	return l.service().Register(ctx, state)
}
func (l *refundLedger) approve(ctx context.Context, state refundState, actor string) error {
	return l.service().Approve(ctx, state, actor)
}
func (l *refundLedger) refund(ctx context.Context, key string, state refundState) (string, error) {
	return l.service().Refund(ctx, key, state)
}
