package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	_ "github.com/mattn/go-sqlite3"
)

// refundState is the entire persisted business state. All orders are synthetic.
type refundState struct {
	OrderID      string `json:"order_id"`
	AmountCents  int64  `json:"amount_cents"`
	Currency     string `json:"currency"`
	PaidCents    int64  `json:"paid_cents"`
	DeliveryDays int    `json:"delivery_days"`
	PreparedAt   string `json:"prepared_at,omitempty"`
	ReceiptID    string `json:"receipt_id,omitempty"`
}

func newRunner(ctx context.Context, store durable.Store, ledger *refundLedger, afterRefund func()) (*durable.Runner, error) {
	g := workflow.NewGraph[refundState]()
	if err := g.AddNode("lookup_order", func(ctx context.Context, state refundState) (refundState, error) {
		// A production application obtains these facts from its order service.
		switch state.OrderID {
		case "DEMO-1001":
			state.PaidCents, state.DeliveryDays = 9900, 3
		case "DEMO-EXPIRED":
			state.PaidCents, state.DeliveryDays = 9900, 30
		default:
			return state, fmt.Errorf("unknown synthetic order %q", state.OrderID)
		}
		state.Currency = "CNY"
		return state, ctx.Err()
	}); err != nil {
		return nil, err
	}
	if err := g.AddNode("check_policy", func(ctx context.Context, state refundState) (refundState, error) {
		if state.AmountCents <= 0 || state.AmountCents > state.PaidCents || state.DeliveryDays > 7 {
			return state, fmt.Errorf("refund does not satisfy the synthetic policy")
		}
		state.PreparedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return state, ctx.Err()
	}); err != nil {
		return nil, err
	}
	for _, edge := range [][2]string{{workflow.START, "lookup_order"}, {"lookup_order", "check_policy"}, {"check_policy", workflow.END}} {
		if err := g.AddEdge(edge[0], edge[1]); err != nil {
			return nil, err
		}
	}
	prepared, err := g.Compile(ctx)
	if err != nil {
		return nil, err
	}
	return &durable.Runner{Store: store, Version: "refund-demo-v1", Steps: []durable.Step{
		{Name: "prepare_refund", Run: func(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
			var state refundState
			if err := json.Unmarshal(in.Data, &state); err != nil {
				return nil, err
			}
			state, err := prepared.Invoke(ctx, state)
			if err != nil {
				return nil, err
			}
			return json.Marshal(state)
		}},
		{Name: "issue_refund", RequireApproval: true, Run: func(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
			var state refundState
			if err := json.Unmarshal(in.Data, &state); err != nil {
				return nil, err
			}
			receipt, err := ledger.refund(ctx, in.Key, state)
			if err != nil {
				return nil, err
			}
			// The ledger has committed; the runner has not yet saved its result.
			// The CLI can exit here to exercise the ambiguous-effect crash window.
			if afterRefund != nil {
				afterRefund()
			}
			state.ReceiptID = receipt
			return json.Marshal(state)
		}},
	}}, nil
}

// refundLedger simulates an external refund service with persistent idempotency.
// Its transaction is independent of the runner's checkpoint. No money is moved.
type refundLedger struct{ db *sql.DB }

func openLedger(dir string) (*refundLedger, error) {
	path, err := filepath.Abs(filepath.Join(dir, "refunds.sqlite"))
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "_busy_timeout=5000&_journal_mode=WAL&_synchronous=FULL"}
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS refunds (
		idempotency_key TEXT PRIMARY KEY,
		order_id TEXT NOT NULL,
		amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
		currency TEXT NOT NULL,
		receipt_id TEXT NOT NULL UNIQUE
	)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &refundLedger{db: db}, nil
}

func (l *refundLedger) refund(ctx context.Context, key string, state refundState) (string, error) {
	if key == "" || state.OrderID == "" || state.AmountCents <= 0 || state.Currency != "CNY" {
		return "", fmt.Errorf("invalid refund request")
	}
	hash := sha256.Sum256([]byte(key))
	receipt := "demo-refund-" + hex.EncodeToString(hash[:])
	_, err := l.db.ExecContext(ctx, `INSERT INTO refunds (idempotency_key, order_id, amount_cents, currency, receipt_id)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(idempotency_key) DO NOTHING`, key, state.OrderID, state.AmountCents, state.Currency, receipt)
	if err != nil {
		return "", err
	}
	var order, currency, storedReceipt string
	var amount int64
	err = l.db.QueryRowContext(ctx, `SELECT order_id, amount_cents, currency, receipt_id FROM refunds WHERE idempotency_key = ?`, key).Scan(&order, &amount, &currency, &storedReceipt)
	if err != nil {
		return "", err
	}
	if order != state.OrderID || amount != state.AmountCents || currency != state.Currency {
		return "", fmt.Errorf("idempotency key already belongs to a different refund")
	}
	return storedReceipt, nil
}
