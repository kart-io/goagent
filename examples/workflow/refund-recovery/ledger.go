package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// This is a local test backend, not a payment service. Every balance check and
// insert is serialized in one SQLite write transaction, across processes too.
type refundLedger struct {
	db  *sql.DB
	now func() time.Time
}

type orderRecord struct {
	PaidCents    int64
	Currency     string
	DeliveryDays int
	Version      int64
}

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

func sameRequest(a, b refundState) bool {
	return a.RequestID == b.RequestID && a.OrderID == b.OrderID && a.AmountCents == b.AmountCents
}

func (l *refundLedger) existingRequest(ctx context.Context, state refundState) (refundState, bool, error) {
	if state.RequestID == "" {
		return state, false, fmt.Errorf("business request ID is required")
	}
	existing, _, err := readRequest(ctx, l.db, state.RequestID)
	if errors.Is(err, sql.ErrNoRows) {
		return state, false, nil
	}
	if err != nil {
		return state, false, err
	}
	if !sameRequest(existing, state) {
		return state, false, fmt.Errorf("business request ID belongs to a different refund")
	}
	return existing, true, nil
}

// checkCurrent also runs inside the refund transaction immediately before money
// would move. An approval is bound to the exact observed order revision.
func (l *refundLedger) checkCurrent(ctx context.Context, q rowQuerier, state refundState) error {
	deadline, err := time.Parse(time.RFC3339Nano, state.ValidUntil)
	if err != nil || !l.now().Before(deadline) {
		return fmt.Errorf("approval expired; create a new reviewed business request")
	}
	order, err := readOrder(ctx, q, state.OrderID)
	if err != nil {
		return err
	}
	if order.Version != state.OrderVersion || order.PaidCents != state.PaidCents || order.Currency != state.Currency || order.DeliveryDays != state.DeliveryDays {
		return fmt.Errorf("order changed; a new review is required")
	}
	if state.AmountCents <= 0 || state.AmountCents > order.PaidCents || order.DeliveryDays < 0 || order.DeliveryDays > 7 {
		return fmt.Errorf("refund does not satisfy the synthetic policy")
	}
	return nil
}

func (l *refundLedger) register(ctx context.Context, state refundState) (refundState, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return state, err
	}
	defer func() { _ = tx.Rollback() }()
	existing, _, err := readRequest(ctx, tx, state.RequestID)
	if err == nil {
		if !sameRequest(existing, state) {
			return state, fmt.Errorf("business request ID belongs to a different refund")
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}
	if state.RequestID == "" {
		return state, fmt.Errorf("business request ID is required")
	}
	if err = l.checkCurrent(ctx, tx, state); err != nil {
		return state, err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return state, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO refund_requests(request_id,state_json) VALUES (?,?)", state.RequestID, data); err != nil {
		return state, err
	}
	return state, tx.Commit()
}

// The CLI supplies a demo identity. A production host must authenticate it
// before calling this boundary; a caller-provided name is not authentication.
func (l *refundLedger) approve(ctx context.Context, state refundState, actor string) error {
	if actor != "demo-reviewer" {
		return fmt.Errorf("approver is not authorized by the demo policy")
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stored, _, err := readRequest(ctx, tx, state.RequestID)
	if err != nil {
		return err
	}
	if stored != state {
		return fmt.Errorf("approval payload differs from registered request")
	}
	if err = l.checkCurrent(ctx, tx, stored); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE refund_requests SET approved_by = ?, approved_at = ? WHERE request_id = ?", actor, l.now().UTC().Format(time.RFC3339Nano), state.RequestID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (l *refundLedger) refund(ctx context.Context, key string, state refundState) (string, error) {
	if key == "" {
		return "", fmt.Errorf("execution key is required")
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	stored, actor, err := readRequest(ctx, tx, state.RequestID)
	if err != nil {
		return "", err
	}
	if stored != state {
		return "", fmt.Errorf("business request ID belongs to a different refund")
	}
	// A committed effect is returned even if the order/approval later changes.
	// Rejecting it would leave recovery unable to acknowledge a real refund.
	var receipt string
	err = tx.QueryRowContext(ctx, "SELECT receipt_id FROM refunds WHERE request_id = ?", state.RequestID).Scan(&receipt)
	if err == nil {
		return receipt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if actor == "" {
		return "", fmt.Errorf("refund requires an authorized approval")
	}
	if err = l.checkCurrent(ctx, tx, state); err != nil {
		return "", err
	}
	var refunded int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(amount_cents),0) FROM refunds WHERE order_id = ?", state.OrderID).Scan(&refunded); err != nil {
		return "", err
	}
	if state.AmountCents > state.PaidCents-refunded {
		return "", fmt.Errorf("cumulative refund exceeds paid amount")
	}
	hash := sha256.Sum256([]byte(state.RequestID))
	receipt = "demo-refund-" + hex.EncodeToString(hash[:])
	_, err = tx.ExecContext(ctx, "INSERT INTO refunds VALUES (?,?,?,?,?,?)", state.RequestID, key, state.OrderID, state.AmountCents, state.Currency, receipt)
	if err != nil {
		return "", err
	}
	return receipt, tx.Commit()
}
