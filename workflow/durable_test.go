package workflow_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

func oneNode(t *testing.T, fn workflow.Node[int]) *workflow.Graph[int] {
	t.Helper()
	g := workflow.NewGraph[int]()
	require.NoError(t, g.AddNode("effect", fn))
	require.NoError(t, g.AddEdge(workflow.START, "effect"))
	require.NoError(t, g.AddEdge("effect", workflow.END))
	return g
}
func newStore(t *testing.T) *durable.FileStore {
	t.Helper()
	s, e := durable.NewFileStore(t.TempDir())
	require.NoError(t, e)
	return s
}

func TestDurableGraphApprovalAndAmbiguousEffect(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	attempts := 0
	keys := []string{}
	effects := map[string]int{}
	fn := func(ctx context.Context, n int) (int, error) {
		key := workflow.DurableKey(ctx)
		require.NotEmpty(t, key)
		keys = append(keys, key)
		attempts++
		if _, ok := effects[key]; !ok {
			effects[key] = n + 1
		}
		if attempts == 1 {
			return 0, errors.New("lost response after effect")
		}
		return effects[key], nil
	}
	g := oneNode(t, fn)
	cfg := workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}}
	run, err := g.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	wait, err := run.Run(ctx, "run", 41, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.True(t, wait.Waiting)
	require.Zero(t, attempts)
	_, err = run.Run(ctx, "run", 999, "wrong")
	require.ErrorContains(t, err, "does not match")
	require.Zero(t, attempts)
	failed, err := run.Run(ctx, "run", 999, wait.Key)
	require.ErrorContains(t, err, "lost response")
	require.False(t, failed.Waiting)
	require.Equal(t, 41, failed.State)
	// Recompile/reload: initial input cannot replace the already approved payload.
	resumed, err := oneNode(t, fn).CompileDurable(ctx, cfg)
	require.NoError(t, err)
	done, err := resumed.Run(ctx, "run", 1000, "")
	require.NoError(t, err)
	require.True(t, done.Done)
	require.Equal(t, 42, done.State)
	require.Equal(t, 1, done.Steps)
	require.Equal(t, []string{wait.Key, wait.Key}, keys)
	require.Len(t, effects, 1)
	_, err = resumed.Run(ctx, "run", 0, "")
	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	cfg.Version = "v2"
	changed, err := g.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	_, err = changed.Run(ctx, "run", 0, "")
	require.ErrorContains(t, err, "versioned graph")
}

func TestDurableGraphRouteFailureSkipsCommittedNode(t *testing.T) {
	ctx := context.Background()
	g := workflow.NewGraph[map[string]int]()
	calls := 0
	fail := true
	require.NoError(t, g.AddNode("node", func(_ context.Context, s map[string]int) (map[string]int, error) { calls++; s["n"]++; return s, nil }))
	require.NoError(t, g.AddEdge(workflow.START, "node"))
	require.NoError(t, g.AddBranch("node", func(_ context.Context, s map[string]int) (string, error) {
		s["n"] = 999 // Router mutations must not corrupt committed node state.
		if fail {
			return "", errors.New("routing unavailable")
		}
		return workflow.END, nil
	}, workflow.END))
	cfg := workflow.DurableConfig{Store: newStore(t), Version: "v1"}
	run, err := g.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	stopped, err := run.Run(ctx, "route", map[string]int{"n": 1}, "")
	require.ErrorContains(t, err, "routing unavailable")
	require.True(t, stopped.Routing)
	require.Equal(t, 2, stopped.State["n"])
	fail = false
	run, err = g.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	done, err := run.Run(ctx, "route", nil, "")
	require.NoError(t, err)
	require.Equal(t, 2, done.State["n"])
	require.Equal(t, 1, calls)
}

func TestDurableGraphLoopBudgetSurvivesResume(t *testing.T) {
	ctx := context.Background()
	g := workflow.NewGraph[int](workflow.Config{MaxSteps: 2})
	keys := []string{}
	require.NoError(t, g.AddNode("loop", func(ctx context.Context, n int) (int, error) {
		keys = append(keys, workflow.DurableKey(ctx))
		return n + 1, nil
	}))
	require.NoError(t, g.AddEdge(workflow.START, "loop"))
	require.NoError(t, g.AddBranch("loop", func(context.Context, int) (string, error) { return "loop", nil }, "loop", workflow.END))
	cfg := workflow.DurableConfig{Store: newStore(t), Version: "v1", ApprovalNodes: []string{"loop"}}
	run, err := g.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	first, err := run.Run(ctx, "loop", 0, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	second, err := run.Run(ctx, "loop", 0, first.Key)
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.NotEqual(t, first.Key, second.Key)
	last, err := run.Run(ctx, "loop", 0, second.Key)
	require.ErrorIs(t, err, workflow.ErrMaxSteps)
	require.Equal(t, 2, last.State)
	_, err = run.Run(ctx, "loop", 0, "")
	require.ErrorIs(t, err, workflow.ErrMaxSteps)
	require.Len(t, keys, 2)
	// A topology edit is rejected even if the host forgot to change Version.
	changed := oneNode(t, func(_ context.Context, n int) (int, error) { return n, nil })
	cfg.ApprovalNodes = nil
	r2, err := changed.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	_, err = r2.Run(ctx, "loop", 0, "")
	require.ErrorContains(t, err, "versioned graph")
}

type failSaveStore struct {
	durable.Store
	fail bool
}

func (s *failSaveStore) Save(ctx context.Context, id string, cp *durable.Snapshot) error {
	if s.fail && cp.Next == 1 {
		s.fail = false
		return errors.New("checkpoint unavailable")
	}
	return s.Store.Save(ctx, id, cp)
}
func TestDurableGraphCheckpointFailureReplaysStableKey(t *testing.T) {
	ctx := context.Background()
	store := &failSaveStore{Store: newStore(t), fail: true}
	keys := []string{}
	g := oneNode(t, func(ctx context.Context, n int) (int, error) {
		keys = append(keys, workflow.DurableKey(ctx))
		return n + 1, nil
	})
	run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1"})
	require.NoError(t, err)
	failed, err := run.Run(ctx, "run", 1, "")
	require.ErrorContains(t, err, "checkpoint unavailable")
	require.Equal(t, 1, failed.State)
	require.Zero(t, failed.Steps)
	done, err := run.Run(ctx, "run", 99, "")
	require.NoError(t, err)
	require.Equal(t, 2, done.State)
	require.Len(t, keys, 2)
	require.Equal(t, keys[0], keys[1])
}

func TestDurableGraphRejectsCorruption(t *testing.T) {
	ctx := context.Background()
	cfg := workflow.DurableConfig{Store: newStore(t), Version: "v1"}
	calls := 0
	g := oneNode(t, func(_ context.Context, n int) (int, error) { calls++; return n, nil })
	cfg.ApprovalNodes = []string{"effect"}
	run, err := g.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	_, err = run.Run(ctx, "corrupt", 1, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	cp, err := cfg.Store.Load(ctx, "corrupt")
	require.NoError(t, err)
	cp.Key = "forged"
	require.NoError(t, cfg.Store.Save(ctx, "corrupt", cp))
	_, err = run.Run(ctx, "corrupt", 1, "forged")
	require.ErrorContains(t, err, "corrupt")
	require.Zero(t, calls)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = run.Run(canceled, "new", 0, "")
	require.ErrorIs(t, err, context.Canceled)
}

func TestDurableGraphRealProcessCrash(t *testing.T) {
	if dir := os.Getenv("GOAGENT_GRAPH_CRASH_DIR"); dir != "" {
		ctx := context.Background()
		store, err := durable.NewFileStore(filepath.Join(dir, "checkpoints"))
		require.NoError(t, err)
		g := oneNode(t, func(ctx context.Context, n int) (int, error) {
			path := filepath.Join(dir, workflow.DurableKey(ctx)+".effect")
			f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if errors.Is(e, os.ErrExist) {
				return n + 1, nil
			}
			if e != nil {
				return n, e
			}
			require.NoError(t, f.Sync())
			require.NoError(t, f.Close())
			os.Exit(86)
			return n, nil
		})
		run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1"})
		require.NoError(t, err)
		done, err := run.Run(ctx, "run", 41, "")
		require.NoError(t, err)
		require.True(t, done.Done)
		require.Equal(t, 42, done.State)
		return
	}
	dir := t.TempDir()
	for _, want := range []int{86, 0, 0} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDurableGraphRealProcessCrash$")
		cmd.Env = append(os.Environ(), "GOAGENT_GRAPH_CRASH_DIR="+dir)
		output, err := cmd.CombinedOutput()
		cancel()
		if want == 0 {
			require.NoError(t, err, string(output))
		} else {
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit)
			require.Equal(t, want, exit.ExitCode())
		}
	}
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	effects := 0
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".effect") {
			effects++
		}
	}
	require.Equal(t, 1, effects)
}

func TestDurableGraphRunLockCancellation(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	g := oneNode(t, func(_ context.Context, n int) (int, error) {
		calls.Add(1)
		close(started)
		<-release
		return n + 1, nil
	})
	run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: newStore(t), Version: "v1"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, e := run.Run(ctx, "same", 1, ""); done <- e }()
	<-started
	waitCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	_, err = run.Run(waitCtx, "same", 999, "")
	cancel()
	close(release)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, <-done)
	result, err := run.Run(ctx, "same", 999, "")
	require.NoError(t, err)
	require.Equal(t, 2, result.State)
	require.EqualValues(t, 1, calls.Load())
}
