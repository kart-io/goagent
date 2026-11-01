package builder

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/testing/panictest"
	"github.com/stretchr/testify/require"
)

type exitBeforeEvidenceStore struct{ durable.Store }

func (s exitBeforeEvidenceStore) Save(ctx context.Context, id string, snapshot *durable.Snapshot) error {
	if strings.HasPrefix(id, "goagent/budget/") {
		var data struct{ Evidence map[string]json.RawMessage }
		if json.Unmarshal(snapshot.Data, &data) == nil && len(data.Evidence) > 0 {
			os.Exit(86)
		}
	}
	return s.Store.Save(ctx, id, snapshot)
}

func TestAgentUsageProcessExitAfterModelBeforeEvidence(t *testing.T) {
	dir := os.Getenv("GOAGENT_USAGE_CRASH_DIR")
	child := dir != ""
	if !child {
		dir = t.TempDir()
		cmd := exec.Command(os.Args[0], "-test.run=^TestAgentUsageProcessExitAfterModelBeforeEvidence$")
		cmd.Env = append(os.Environ(), "GOAGENT_USAGE_CRASH_DIR="+dir)
		output, err := panictest.CombinedOutput(cmd, panictest.ChildTimeout(t, time.Minute))
		var exited *exec.ExitError
		require.ErrorAs(t, err, &exited, string(output))
		require.Equal(t, 86, exited.ExitCode(), string(output))
	}
	var store durable.Store
	store, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	if child {
		store = exitBeforeEvidenceStore{store}
	}
	c, err := budget.NewDurable(budget.Config{RootKey: "process-usage", Version: "v1", MaxModelRequests: 1, AttributionVersion: "v1", MaxAgentCalls: 2}, store)
	require.NoError(t, err)
	ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "process-usage"), store), c)
	model := nativeModel()
	if child {
		model = nativeModel(answerTurn("saved", 8))
	}
	a, err := NewSimpleBuilder(model).WithAgentID("root").WithDurableScope("root").WithTools(dispatcherFor(t, plainTool()).Tools()...).Build()
	require.NoError(t, err)
	out, err := a.AsRunnable().Invoke(ctx, "task")
	require.NoError(t, err)
	if child {
		t.Fatal("expected process exit before evidence commit")
	}
	require.Equal(t, "saved", out.Result)
	require.Empty(t, model.requests, "restarted process must not send saved model request again")
	r, err := c.Attribution(ctx)
	require.NoError(t, err)
	require.Equal(t, 8, r.Root.KnownTokens.TotalTokens)
	require.Equal(t, 1, r.Root.ReservedRequests)
	require.Zero(t, r.UnknownUsageEntries)
}
