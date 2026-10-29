package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

func TestRefundGraphRecoveryAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	invoke := func(code int, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		base := []string{"-test.run=^TestRefundChildProcess$", "--", "-engine", "graph", "-dir", dir, "-run", "native-1"}
		cmd := exec.CommandContext(ctx, os.Args[0], append(base, args...)...)
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
	var wait struct {
		Status   string
		Snapshot workflow.GraphSnapshot[refundState]
	}
	require.NoError(t, json.Unmarshal(invoke(0), &wait))
	require.Equal(t, "approval_required", wait.Status)
	require.True(t, wait.Snapshot.Waiting)
	require.Equal(t, "issue_refund", wait.Snapshot.Node)
	ledger, err := openLedger(dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, ledger.db.Close()) }()
	count := func(want int) {
		t.Helper()
		var n int
		require.NoError(t, ledger.db.QueryRow("SELECT count(*) FROM refunds").Scan(&n))
		require.Equal(t, want, n)
	}
	count(0)
	require.Contains(t, string(invoke(1, "-approve", wait.Snapshot.Key, "-approver", "attacker")), "not authorized")
	count(0)
	invoke(crashExitCode, "-approve", wait.Snapshot.Key, "-approver", "demo-reviewer", "-crash-after-refund")
	count(1)
	// The tool receives a durable key and the completed effect is acknowledged
	// even if input changes after the ambiguous-effect crash.
	var completed struct {
		Status   string
		Snapshot workflow.GraphSnapshot[refundState]
	}
	require.NoError(t, json.Unmarshal(invoke(0, "-amount-cents", "1", "-order", "DEMO-EXPIRED"), &completed))
	require.Equal(t, "completed", completed.Status)
	require.True(t, completed.Snapshot.Done)
	require.EqualValues(t, 9900, completed.Snapshot.State.AmountCents)
	require.NotEmpty(t, completed.Snapshot.State.ReceiptID)
	var key, receipt string
	require.NoError(t, ledger.db.QueryRow("SELECT execution_key,receipt_id FROM refunds").Scan(&key, &receipt))
	require.Equal(t, wait.Snapshot.Key, key)
	require.Equal(t, receipt, completed.Snapshot.State.ReceiptID)
	invoke(0)
	count(1)
}

func TestRefundGraphRechecksReviewedOrder(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var out bytes.Buffer
	require.NoError(t, runCLI(ctx, []string{"-engine", "graph", "-dir", dir}, &out, nil))
	var wait struct {
		Snapshot workflow.GraphSnapshot[refundState]
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &wait))
	ledger, err := openLedger(dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, ledger.db.Close()) }()
	_, err = ledger.db.Exec("UPDATE orders SET version=version+1 WHERE order_id='DEMO-1001'")
	require.NoError(t, err)
	out.Reset()
	err = runCLI(ctx, []string{"-engine", "graph", "-dir", dir, "-approve", wait.Snapshot.Key, "-approver", "demo-reviewer"}, &out, nil)
	require.ErrorContains(t, err, "order changed")
	require.Empty(t, out.String())
	var count int
	require.NoError(t, ledger.db.QueryRow("SELECT count(*) FROM refunds").Scan(&count))
	require.Zero(t, count)
}
