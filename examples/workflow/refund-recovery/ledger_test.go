package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func preparedRequest(t *testing.T, ledger *refundLedger, id string, amount int64) refundState {
	t.Helper()
	order, err := ledger.order(context.Background(), "DEMO-1001")
	require.NoError(t, err)
	state, err := ledger.register(context.Background(), refundState{RequestID: id, OrderID: "DEMO-1001", AmountCents: amount,
		PaidCents: order.PaidCents, Currency: order.Currency, DeliveryDays: order.DeliveryDays, OrderVersion: order.Version,
		PreparedAt: ledger.now().UTC().Format(time.RFC3339Nano), ValidUntil: ledger.now().Add(time.Minute).UTC().Format(time.RFC3339Nano)})
	require.NoError(t, err)
	return state
}

func TestRefundBusinessGuards(t *testing.T) {
	for _, scenario := range []string{"missing-approval", "expired-approval", "changed-order", "business-dedup", "changed-payload"} {
		t.Run(scenario, func(t *testing.T) {
			l, err := openLedger(t.TempDir())
			require.NoError(t, err)
			defer func() { require.NoError(t, l.db.Close()) }()
			clock := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)
			l.now = func() time.Time { return clock }
			state := preparedRequest(t, l, "refund-1", 5000)
			ctx := context.Background()
			if scenario != "missing-approval" {
				require.NoError(t, l.approve(ctx, state, "demo-reviewer"))
			}
			switch scenario {
			case "expired-approval":
				clock = clock.Add(time.Minute)
			case "changed-order":
				_, err = l.db.Exec("UPDATE orders SET version = version + 1 WHERE order_id = 'DEMO-1001'")
				require.NoError(t, err)
			case "business-dedup":
				first, err := l.refund(ctx, "run-A", state)
				require.NoError(t, err)
				// A committed refund can still be acknowledged after approval expires.
				clock = clock.Add(time.Hour)
				second, err := l.refund(ctx, "run-B", state)
				require.NoError(t, err)
				require.Equal(t, first, second)
			case "changed-payload":
				state.AmountCents++
			}
			_, err = l.refund(ctx, "retry-run", state)
			if scenario == "business-dedup" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			var count int
			require.NoError(t, l.db.QueryRow("SELECT count(*) FROM refunds").Scan(&count))
			if scenario == "business-dedup" {
				require.Equal(t, 1, count)
			} else {
				require.Zero(t, count)
			}
		})
	}
}

func TestConcurrentRequestsCannotOverRefund(t *testing.T) {
	dir := t.TempDir()
	var ledgers []*refundLedger
	var states []refundState
	for i := range 2 {
		l, err := openLedger(dir)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, l.db.Close()) })
		ledgers = append(ledgers, l)
		state := preparedRequest(t, l, fmt.Sprintf("request-%d", i), 6000)
		require.NoError(t, l.approve(context.Background(), state, "demo-reviewer"))
		states = append(states, state)
	}
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = ledgers[i].refund(context.Background(), fmt.Sprintf("run-%d", i), states[i])
		}()
	}
	wg.Wait()
	passed := 0
	for _, err := range results {
		if err == nil {
			passed++
		} else {
			require.ErrorContains(t, err, "cumulative refund")
		}
	}
	require.Equal(t, 1, passed)
	var amount int64
	require.NoError(t, ledgers[0].db.QueryRow("SELECT SUM(amount_cents) FROM refunds").Scan(&amount))
	require.EqualValues(t, 6000, amount)
}

func TestApprovalRejectsExpiredChangedAndUnauthorizedRequests(t *testing.T) {
	l, err := openLedger(t.TempDir())
	require.NoError(t, err)
	defer func() { require.NoError(t, l.db.Close()) }()
	state := preparedRequest(t, l, "request-1", 100)
	require.ErrorContains(t, l.approve(context.Background(), state, "untrusted"), "not authorized")
	changed := state
	changed.AmountCents++
	require.ErrorContains(t, l.approve(context.Background(), changed, "demo-reviewer"), "payload differs")
	l.now = func() time.Time { deadline, _ := time.Parse(time.RFC3339Nano, state.ValidUntil); return deadline }
	require.ErrorContains(t, l.approve(context.Background(), state, "demo-reviewer"), "expired")
	var actor string
	require.NoError(t, l.db.QueryRow("SELECT approved_by FROM refund_requests").Scan(&actor))
	require.Empty(t, actor)
}

func TestLegacyLedgerIsNotSilentlyReinitialized(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "refunds.sqlite"))
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE refunds(idempotency_key TEXT PRIMARY KEY)")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	_, err = openLedger(dir)
	require.ErrorContains(t, err, "incompatible ledger schema")
}
