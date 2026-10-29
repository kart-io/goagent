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

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/testing/panictest"
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
	output, err := panictest.CombinedOutput(cmd, panictest.ChildTimeout(t, time.Minute))
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

func TestBeforeApproveRunsUnderLock(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)
	effects := 0
	runner := &Runner{Store: store, Version: "v1", Steps: []Step{
		{Name: "effect", RequireApproval: true, Run: func(context.Context, StepInput) (json.RawMessage, error) {
			effects++
			return json.RawMessage(`"done"`), nil
		}},
	}}
	wait, err := runner.Run(ctx, "run", json.RawMessage(`{"ticket":"h1"}`), "")
	require.ErrorIs(t, err, ErrApprovalRequired)
	var seen *Snapshot
	reject := errors.New("工单已变更")
	runner.BeforeApprove = func(_ context.Context, id string, snap *Snapshot) error {
		require.Equal(t, "run", id)
		seen = snap
		snap.Data = json.RawMessage(`"tampered"`)
		snap.Approved = true
		return reject
	}
	_, err = runner.Run(ctx, "run", nil, wait.Key)
	require.ErrorIs(t, err, reject)
	require.True(t, seen.Waiting)
	require.Zero(t, effects)
	after, err := store.Load(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, wait, after)
	runner.BeforeApprove = func(context.Context, string, *Snapshot) error { panic("boom") }
	_, err = runner.Run(ctx, "run", nil, wait.Key)
	require.ErrorContains(t, err, "批准前置校验 发生 panic")
	require.NotContains(t, err.Error(), "boom")
	var panicErr *agentErrors.PanicError
	require.ErrorAs(t, err, &panicErr)
	require.Equal(t, "boom", panicErr.Value)
	require.Zero(t, effects)
	runner.BeforeApprove = func(_ context.Context, _ string, snap *Snapshot) error {
		require.JSONEq(t, `{"ticket":"h1"}`, string(snap.Data))
		return nil
	}
	done, err := runner.Run(ctx, "run", nil, wait.Key)
	require.NoError(t, err)
	require.Equal(t, 1, effects)
	require.Equal(t, 1, done.Next)
	// 非批准恢复不调用钩子。
	runner.BeforeApprove = func(context.Context, string, *Snapshot) error { panic("不应调用") }
	_, err = runner.Run(ctx, "run", nil, "")
	require.NoError(t, err)
}
