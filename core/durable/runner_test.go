package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRestartApprovalAndIdempotentRetry(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	require.NoError(t, err)
	firstCalls := 0
	keys := []string{}
	fail := true
	steps := []Step{
		{Name: "prepare", Run: func(context.Context, StepInput) (json.RawMessage, error) {
			firstCalls++
			return json.RawMessage(`{"ready":true}`), nil
		}},
		{Name: "effect", RequireApproval: true, Run: func(ctx context.Context, in StepInput) (json.RawMessage, error) {
			keys = append(keys, in.Key)
			if fail {
				return nil, errors.New("ambiguous external failure")
			}
			return json.RawMessage(`"done"`), nil
		}},
	}
	runner := &Runner{Store: store, Version: "v1", Steps: steps}
	snapshot, err := runner.Run(context.Background(), "run", nil, "")
	require.ErrorIs(t, err, ErrApprovalRequired)
	require.Equal(t, 1, firstCalls)
	require.Empty(t, keys)
	// Discard all runtime state and load the continuation through a new store.
	restarted, err := NewFileStore(dir)
	require.NoError(t, err)
	runner = &Runner{Store: restarted, Version: "v1", Steps: steps}
	_, err = runner.Run(context.Background(), "run", nil, "wrong")
	require.Error(t, err)
	require.Empty(t, keys)
	_, err = runner.Run(context.Background(), "run", nil, snapshot.Key)
	require.ErrorContains(t, err, "ambiguous")
	fail = false
	done, err := runner.Run(context.Background(), "run", nil, "")
	require.NoError(t, err)
	require.Equal(t, 2, done.Next)
	require.Equal(t, 1, firstCalls)
	require.Len(t, keys, 2)
	require.Equal(t, keys[0], keys[1])
	_, err = runner.Run(context.Background(), "run", nil, "")
	require.NoError(t, err)
	require.Len(t, keys, 2)
	runner.Version = "v2"
	_, err = runner.Run(context.Background(), "run", nil, "")
	require.Error(t, err)
}
func TestLockCancellationAndCorruption(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)
	unlock, err := store.Lock(context.Background(), "run")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = store.Lock(ctx, "run")
	require.Error(t, err)
	require.NoError(t, unlock())
	require.NoError(t, os.WriteFile(store.path("run"), []byte(`{broken`), 0600))
	runner := &Runner{Store: store, Version: "v1"}
	_, err = runner.Run(context.Background(), "run", nil, "")
	require.ErrorContains(t, err, "corrupt")
}

// The child writes a waiting checkpoint and exits; the parent resumes it in a
// distinct process with no shared Go state or channels.
func TestDurableProcessRestart(t *testing.T) {
	if dir := os.Getenv("GOAGENT_DURABLE_CHILD_DIR"); dir != "" {
		store, err := NewFileStore(dir)
		require.NoError(t, err)
		runner := &Runner{Store: store, Version: "process-v1", Steps: processSteps(dir)}
		_, err = runner.Run(context.Background(), "process", nil, "")
		require.ErrorIs(t, err, ErrApprovalRequired)
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDurableProcessRestart$")
	cmd.Env = append(os.Environ(), "GOAGENT_DURABLE_CHILD_DIR="+dir)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	store, err := NewFileStore(dir)
	require.NoError(t, err)
	snapshot, err := store.Load(context.Background(), "process")
	require.NoError(t, err)
	require.True(t, snapshot.Waiting)
	runner := &Runner{Store: store, Version: "process-v1", Steps: processSteps(dir)}
	done, err := runner.Run(context.Background(), "process", nil, snapshot.Key)
	require.NoError(t, err)
	require.Equal(t, 2, done.Next)
	data, err := os.ReadFile(filepath.Join(dir, "prepare-count"))
	require.NoError(t, err)
	require.Equal(t, "once", string(data))
}
func processSteps(dir string) []Step {
	return []Step{
		{Name: "prepare", Run: func(context.Context, StepInput) (json.RawMessage, error) {
			f, err := os.OpenFile(filepath.Join(dir, "prepare-count"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
			if err != nil {
				return nil, err
			}
			_, err = f.WriteString("once")
			_ = f.Close()
			return json.RawMessage(`null`), err
		}},
		{Name: "approved", RequireApproval: true, Run: func(context.Context, StepInput) (json.RawMessage, error) { return json.RawMessage(`"done"`), nil }},
	}
}
