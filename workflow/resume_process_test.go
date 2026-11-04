package workflow_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

// Stop after human input and approval are durable but before the node executes.
type crashOnHumanAcceptance struct{ durable.Store }

func (s crashOnHumanAcceptance) Save(ctx context.Context, id string, cp *durable.Snapshot) error {
	if err := s.Store.Save(ctx, id, cp); err != nil {
		return err
	}
	if cp.Approved {
		os.Exit(86)
	}
	return nil
}

func TestHumanResumeSurvivesProcessExitAfterAcceptance(t *testing.T) {
	if dir := os.Getenv("GOAGENT_HUMAN_RESUME_DIR"); dir != "" {
		ctx := context.Background()
		base, err := durable.NewFileStore(filepath.Join(dir, "checkpoints"))
		require.NoError(t, err)
		var store durable.Store = base
		phase := os.Getenv("GOAGENT_HUMAN_RESUME_PHASE")
		if phase == "accept" {
			store = crashOnHumanAcceptance{base}
		}
		g := oneNode(t, func(_ context.Context, n int) (int, error) {
			f, e := os.OpenFile(filepath.Join(dir, "effect"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return 0, e
			}
			return n * 2, f.Close()
		})
		run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "human-v1", ApprovalNodes: []string{"effect"}},
			workflow.WithResumeHandler(func(ctx context.Context, id string, snap *workflow.GraphSnapshot[int], input json.RawMessage) (int, error) {
				// Test instrumentation: fail if the accepted edit is ever reapplied.
				f, e := os.OpenFile(filepath.Join(dir, "accepted"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				require.NoError(t, e)
				require.NoError(t, f.Close())
				return addHumanNumber(ctx, id, snap, input)
			}))
		require.NoError(t, err)
		switch phase {
		case "start":
			wait, e := run.Run(ctx, "human", 10, "")
			require.ErrorIs(t, e, durable.ErrApprovalRequired)
			require.True(t, wait.Waiting)
		case "accept":
			wait, e := run.Snapshot(ctx, "human")
			require.NoError(t, e)
			_, _ = run.Resume(ctx, "human", workflow.ResumeRequest{Key: wait.Key, Input: json.RawMessage(`3`)})
			t.Fatal("expected process exit after durable acceptance")
		case "recover":
			snap, e := run.Snapshot(ctx, "human")
			require.NoError(t, e)
			require.False(t, snap.Waiting)
			require.False(t, snap.Done)
			require.Equal(t, 13, snap.State)
			done, e := run.Run(ctx, "human", 999, "")
			require.NoError(t, e)
			require.True(t, done.Done)
			require.Equal(t, 26, done.State)
		case "replay":
			done, e := run.Run(ctx, "human", 999, "")
			require.NoError(t, e)
			require.Equal(t, 26, done.State)
		default:
			t.Fatalf("unknown phase %q", phase)
		}
		return
	}
	dir := t.TempDir()
	for _, phase := range []string{"start", "accept", "recover", "replay"} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHumanResumeSurvivesProcessExitAfterAcceptance$")
		cmd.Env = append(os.Environ(), "GOAGENT_HUMAN_RESUME_DIR="+dir, "GOAGENT_HUMAN_RESUME_PHASE="+phase)
		cmd.WaitDelay = time.Second
		output, err := cmd.CombinedOutput()
		cancel()
		if phase == "accept" {
			var exited *exec.ExitError
			require.ErrorAs(t, err, &exited, string(output))
			require.Equal(t, 86, exited.ExitCode())
			require.NoFileExists(t, filepath.Join(dir, "effect"))
		} else {
			require.NoError(t, err, "%s: %s", phase, output)
		}
	}
	require.FileExists(t, filepath.Join(dir, "effect"))
}
