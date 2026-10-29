package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/stretchr/testify/require"
)

// Each invocation has its own process, heap, file locks and SQLite connection.
func TestRefundRecoveryAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	invoke := func(code int, extra ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		args := append([]string{"-test.run=^TestRefundChildProcess$", "--", "-dir", dir, "-run", "request-1"}, extra...)
		cmd := exec.CommandContext(ctx, os.Args[0], args...)
		cmd.Env = append(os.Environ(), "GOAGENT_REFUND_CHILD=1")
		cmd.WaitDelay = time.Second
		output, err := cmd.CombinedOutput()
		if code == 0 {
			require.NoError(t, err, string(output))
		} else {
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, string(output))
			require.Equal(t, code, exit.ExitCode(), string(output))
		}
		return output
	}
	var waiting struct {
		Status   string           `json:"status"`
		Snapshot durable.Snapshot `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(invoke(0), &waiting))
	require.Equal(t, "approval_required", waiting.Status)
	require.Equal(t, 1, waiting.Snapshot.Next)
	require.True(t, waiting.Snapshot.Waiting)
	ledger, err := openLedger(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ledger.db.Close()) })
	count := func(want int) {
		t.Helper()
		var got int
		require.NoError(t, ledger.db.QueryRow("SELECT count(*) FROM refunds").Scan(&got))
		require.Equal(t, want, got)
	}
	count(0)
	// No implicit approval, and another run/step's key cannot approve this one.
	invoke(0)
	require.Contains(t, string(invoke(1, "-approve", "wrong-key")), "approval does not match")
	count(0)
	require.Contains(t, string(invoke(1, "-approve", waiting.Snapshot.Key, "-approver", "unauthorized")), "not authorized")
	count(0)
	invoke(crashExitCode, "-approve", waiting.Snapshot.Key, "-approver", "demo-reviewer", "-crash-after-refund")
	count(1)
	store, err := durable.NewFileStore(filepath.Join(dir, "checkpoints"))
	require.NoError(t, err)
	crashed, err := store.Load(context.Background(), "request-1")
	require.NoError(t, err)
	require.Equal(t, 1, crashed.Next, "refund result is not checkpointed yet")
	require.True(t, crashed.Approved)
	require.Equal(t, waiting.Snapshot.Key, crashed.Key)
	require.JSONEq(t, string(waiting.Snapshot.Data), string(crashed.Data))
	// New input cannot change the amount/order already reviewed and approved.
	var completed struct {
		Status   string           `json:"status"`
		Snapshot durable.Snapshot `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(invoke(0, "-order", "DEMO-EXPIRED", "-amount-cents", "1"), &completed))
	require.Equal(t, "completed", completed.Status)
	require.Equal(t, 2, completed.Snapshot.Next)
	var before, after refundState
	require.NoError(t, json.Unmarshal(waiting.Snapshot.Data, &before))
	require.NoError(t, json.Unmarshal(completed.Snapshot.Data, &after))
	require.NotEmpty(t, after.ReceiptID)
	var receipt string
	require.NoError(t, ledger.db.QueryRow("SELECT receipt_id FROM refunds").Scan(&receipt))
	require.Equal(t, receipt, after.ReceiptID)
	after.ReceiptID = ""
	require.Equal(t, before, after, "the committed preparation graph must not rerun")
	count(1)
	repeated := invoke(0)
	require.Contains(t, string(repeated), receipt)
	count(1)
	// A different execution ID cannot create a second refund for the same
	// business request, or silently change its approved payload.
	var secondRun struct {
		Snapshot durable.Snapshot `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(invoke(0, "-run", "execution-2"), &secondRun))
	secondDone := invoke(0, "-run", "execution-2", "-approve", secondRun.Snapshot.Key, "-approver", "demo-reviewer")
	require.Contains(t, string(secondDone), receipt)
	require.Contains(t, string(invoke(1, "-run", "execution-3", "-amount-cents", "1")), "different refund")
	count(1)
}

func TestRefundChildProcess(t *testing.T) {
	if os.Getenv("GOAGENT_REFUND_CHILD") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			err := runCLI(context.Background(), os.Args[i+1:], os.Stdout, func() { os.Exit(crashExitCode) })
			if err != nil {
				_, _ = io.WriteString(os.Stderr, err.Error())
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	t.Fatal("missing child arguments")
}

func TestRejectedRefundHasNoEffect(t *testing.T) {
	for _, args := range [][]string{{"-order", "DEMO-EXPIRED"}, {"-order", "missing"}, {"-amount-cents", "10000"}} {
		t.Run(args[1], func(t *testing.T) {
			dir := t.TempDir()
			var output bytes.Buffer
			err := runCLI(context.Background(), append([]string{"-dir", dir}, args...), &output, nil)
			require.Error(t, err)
			require.Empty(t, output.String())
			ledger, err := openLedger(dir)
			require.NoError(t, err)
			defer func() { require.NoError(t, ledger.db.Close()) }()
			var count int
			require.NoError(t, ledger.db.QueryRow("SELECT count(*) FROM refunds").Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestLedgerDeduplicatesAndRejectsChangedPayload(t *testing.T) {
	ledger, err := openLedger(t.TempDir())
	require.NoError(t, err)
	defer func() { require.NoError(t, ledger.db.Close()) }()
	state := preparedRequest(t, ledger, "business-1", 9900)
	require.NoError(t, ledger.approve(context.Background(), state, "demo-reviewer"))
	const callers = 8
	var wg sync.WaitGroup
	errors := make([]error, callers)
	receipts := make([]string, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipts[i], errors[i] = ledger.refund(context.Background(), "stable-key", state)
		}()
	}
	wg.Wait()
	for i := range callers {
		require.NoError(t, errors[i])
		require.NotEmpty(t, receipts[i])
		require.Equal(t, receipts[0], receipts[i])
	}
	state.AmountCents = 1
	_, err = ledger.refund(context.Background(), "stable-key", state)
	require.ErrorContains(t, err, "different refund")
	var count int
	require.NoError(t, ledger.db.QueryRow("SELECT count(*) FROM refunds").Scan(&count))
	require.Equal(t, 1, count)
}
