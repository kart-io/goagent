package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

func parallelGraph(t *testing.T, limit int, branches []workflow.Branch[int], merge func(context.Context, int, []int) (int, error)) *workflow.Graph[int] {
	t.Helper()
	g := workflow.NewGraph[int](workflow.Config{MaxConcurrency: limit})
	require.NoError(t, g.AddParallel("fork", workflow.ParallelConfig[int]{Branches: branches, Clone: func(n int) int { return n }, Merge: merge}))
	require.NoError(t, g.AddEdge(workflow.START, "fork"))
	require.NoError(t, g.AddEdge("fork", workflow.END))
	return g
}
func sumBranches(_ context.Context, _ int, out []int) (int, error) {
	n := 0
	for _, v := range out {
		n += v
	}
	return n, nil
}

func TestDurableParallelResumeAndMergeFailure(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	calls := []int{0, 0}
	keys := []string{}
	failBranch, failMerge := true, true
	branches := []workflow.Branch[int]{
		{Name: "first", Run: func(ctx context.Context, n int) (int, error) {
			calls[0]++
			keys = append(keys, workflow.DurableKey(ctx))
			return n + 1, nil
		}},
		{Name: "second", Run: func(ctx context.Context, n int) (int, error) {
			calls[1]++
			keys = append(keys, workflow.DurableKey(ctx))
			if failBranch {
				return n, errors.New("branch unavailable")
			}
			return n + 2, nil
		}},
	}
	merge := func(ctx context.Context, n int, out []int) (int, error) {
		if failMerge {
			return n, errors.New("merge unavailable")
		}
		return sumBranches(ctx, n, out)
	}
	cfg := workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"fork"}}
	g := parallelGraph(t, 1, branches, merge)
	run, err := g.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	wait, err := run.Run(ctx, "parallel", 10, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	failed, err := run.Run(ctx, "parallel", 999, wait.Key)
	require.ErrorContains(t, err, "branch unavailable")
	require.Equal(t, 10, failed.State)
	require.Equal(t, []int{1, 1}, calls)
	failBranch = false
	// Reconstruction emulates a new executor instance with the same version/plan.
	run, err = g.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	_, err = run.Run(ctx, "parallel", 999, "")
	require.ErrorContains(t, err, "merge unavailable")
	require.Equal(t, []int{1, 2}, calls)
	failMerge = false
	done, err := run.Run(ctx, "parallel", 999, "")
	require.NoError(t, err)
	require.True(t, done.Done)
	require.Equal(t, 23, done.State)
	require.Equal(t, []int{1, 2}, calls)
	require.NotEqual(t, keys[0], keys[1])
	require.Equal(t, keys[1], keys[2])
	require.NotEqual(t, wait.Key, keys[0])
	// Branch order is part of the plan because Merge receives registration order.
	reversed := parallelGraph(t, 1, []workflow.Branch[int]{branches[1], branches[0]}, merge)
	changed, err := reversed.CompileDurable(ctx, cfg)
	require.NoError(t, err)
	_, err = changed.Run(ctx, "parallel", 0, "")
	require.ErrorContains(t, err, "versioned graph")
}

func TestDurableParallelConcurrencyAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered := make(chan struct{}, 4)
	var active, peak, exited atomic.Int32
	branch := func(ctx context.Context, n int) (int, error) {
		count := active.Add(1)
		defer active.Add(-1)
		defer exited.Add(1)
		for old := peak.Load(); count > old && !peak.CompareAndSwap(old, count); old = peak.Load() {
		}
		entered <- struct{}{}
		<-ctx.Done()
		return n, ctx.Err()
	}
	g := parallelGraph(t, 2, []workflow.Branch[int]{{Name: "a", Run: branch}, {Name: "b", Run: branch}, {Name: "c", Run: branch}}, sumBranches)
	run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: newStore(t), Version: "v1"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, e := run.Run(ctx, "cancel", 1, ""); done <- e }()
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("workers did not start")
		}
	}
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.EqualValues(t, 2, peak.Load())
	require.EqualValues(t, 2, exited.Load())
	require.Zero(t, active.Load())
}

type failBranchCheckpoint struct {
	durable.Store
	fail bool
}

func (s *failBranchCheckpoint) Save(ctx context.Context, id string, cp *durable.Snapshot) error {
	var state struct {
		Parallel *struct {
			Outputs map[string]json.RawMessage `json:"outputs"`
		} `json:"parallel"`
	}
	if err := json.Unmarshal(cp.Data, &state); err != nil {
		return err
	}
	if s.fail && state.Parallel != nil && len(state.Parallel.Outputs) > 0 {
		s.fail = false
		return errors.New("branch save failed")
	}
	return s.Store.Save(ctx, id, cp)
}
func TestDurableParallelSaveFailureRetainsEffectKey(t *testing.T) {
	ctx := context.Background()
	store := &failBranchCheckpoint{Store: newStore(t), fail: true}
	keys := []string{}
	g := parallelGraph(t, 1, []workflow.Branch[int]{{Name: "effect", Run: func(ctx context.Context, n int) (int, error) {
		keys = append(keys, workflow.DurableKey(ctx))
		return n + 1, nil
	}}}, sumBranches)
	run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1"})
	require.NoError(t, err)
	_, err = run.Run(ctx, "save", 1, "")
	require.ErrorContains(t, err, "branch save failed")
	done, err := run.Run(ctx, "save", 100, "")
	require.NoError(t, err)
	require.Equal(t, 2, done.State)
	require.Len(t, keys, 2)
	require.Equal(t, keys[0], keys[1])
}

func TestDurableParallelProcessCrash(t *testing.T) {
	if dir := os.Getenv("GOAGENT_PARALLEL_CRASH_DIR"); dir != "" {
		ctx := context.Background()
		store, err := durable.NewFileStore(filepath.Join(dir, "checkpoints"))
		require.NoError(t, err)
		g := parallelGraph(t, 1, []workflow.Branch[int]{
			{Name: "saved", Run: func(_ context.Context, n int) (int, error) {
				f, e := os.OpenFile(filepath.Join(dir, "saved-once"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if e != nil {
					return n, errors.New("committed sibling reran")
				}
				return n + 1, f.Close()
			}},
			{Name: "ambiguous", Run: func(ctx context.Context, n int) (int, error) {
				f, e := os.OpenFile(filepath.Join(dir, workflow.DurableKey(ctx)+".effect"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if errors.Is(e, os.ErrExist) {
					return n + 2, nil
				}
				if e != nil {
					return n, e
				}
				if e = f.Sync(); e != nil {
					_ = f.Close()
					return n, e
				}
				if e = f.Close(); e != nil {
					return n, e
				}
				os.Exit(86)
				return n, nil
			}},
		}, sumBranches)
		run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1"})
		require.NoError(t, err)
		done, err := run.Run(ctx, "crash", 10, "")
		require.NoError(t, err)
		require.True(t, done.Done)
		require.Equal(t, 23, done.State)
		return
	}
	dir := t.TempDir()
	for _, want := range []int{86, 0, 0} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDurableParallelProcessCrash$")
		cmd.Env = append(os.Environ(), "GOAGENT_PARALLEL_CRASH_DIR="+dir)
		cmd.WaitDelay = time.Second
		output, err := cmd.CombinedOutput()
		cancel()
		if want == 0 {
			require.NoError(t, err, string(output))
		} else {
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, string(output))
			require.Equal(t, want, exit.ExitCode())
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.effect"))
	require.NoError(t, err)
	require.Len(t, files, 1)
}

func TestDurableRunnablePauseEventsAndResume(t *testing.T) {
	ctx := context.Background()
	g := oneNode(t, func(_ context.Context, n int) (int, error) { return n + 1, nil })
	run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: newStore(t), Version: "v1", ApprovalNodes: []string{"effect"}})
	require.NoError(t, err)
	r := run.AsRunnable()
	paused, err := r.Invoke(ctx, workflow.RunRequest[int]{RunID: "r", Initial: 1})
	require.NoError(t, err)
	require.True(t, paused.Waiting)
	require.False(t, paused.Done)
	stream, err := r.Stream(ctx, workflow.RunRequest[int]{RunID: "r", ApprovalKey: paused.Key})
	require.NoError(t, err)
	events := []string{}
	finals := 0
	for c := range stream {
		require.NoError(t, c.Error)
		if c.Event != nil {
			events = append(events, c.Event.Type)
		}
		if c.Done {
			finals++
			require.True(t, c.Data.Done)
			require.Equal(t, 2, c.Data.State)
		}
	}
	require.Equal(t, []string{"node_start", "node_end"}, events)
	require.Equal(t, 1, finals)
}

func TestDurableStreamCancellationAfterCommit(t *testing.T) {
	ctx := context.Background()
	calls := 0
	g := oneNode(t, func(_ context.Context, n int) (int, error) { calls++; return n + 1, nil })
	run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: newStore(t), Version: "v1"})
	require.NoError(t, err)
	// Cancel the event stream after node_end; context cancellation is observable
	// and a later Run can finish the same persisted invocation.
	cancelCtx, cancel := context.WithCancel(ctx)
	stream, err := run.AsRunnable().Stream(cancelCtx, workflow.RunRequest[int]{RunID: "r", Initial: 1})
	require.NoError(t, err)
	for c := range stream {
		if c.Event != nil && c.Event.Type == "node_end" {
			cancel()
		}
	}
	cancel()
	done, err := run.Run(ctx, "r", 999, "")
	require.NoError(t, err)
	require.Equal(t, 2, done.State)
	require.Equal(t, 1, calls)
}

func TestDurableSerialV1CheckpointCompatibility(t *testing.T) {
	data, err := os.ReadFile("testdata/durable-v1-waiting.json")
	require.NoError(t, err)
	var cp durable.Snapshot
	require.NoError(t, json.Unmarshal(data, &cp))
	store := newStore(t)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, "legacy", &cp))
	g := oneNode(t, func(_ context.Context, n int) (int, error) { return n + 1, nil })
	run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}})
	require.NoError(t, err)
	status, err := run.Snapshot(ctx, "legacy")
	require.NoError(t, err)
	require.True(t, status.Waiting)
	done, err := run.Run(ctx, "legacy", 999, cp.Key)
	require.NoError(t, err)
	require.Equal(t, 42, done.State)
}

type panicJSONState struct{ Panic bool }

func (p panicJSONState) MarshalJSON() ([]byte, error) {
	if p.Panic {
		panic("marshal panic")
	}
	return []byte(`{"Panic":false}`), nil
}
func TestDurableParallelSerializationPanicIsContained(t *testing.T) {
	g := workflow.NewGraph[panicJSONState]()
	require.NoError(t, g.AddParallel("fork", workflow.ParallelConfig[panicJSONState]{
		Branches: []workflow.Branch[panicJSONState]{{Name: "panic", Run: func(context.Context, panicJSONState) (panicJSONState, error) { return panicJSONState{Panic: true}, nil }}},
		Clone:    func(s panicJSONState) panicJSONState { return s }, Merge: func(_ context.Context, s panicJSONState, _ []panicJSONState) (panicJSONState, error) { return s, nil },
	}))
	require.NoError(t, g.AddEdge(workflow.START, "fork"))
	require.NoError(t, g.AddEdge("fork", workflow.END))
	run, err := g.CompileDurable(context.Background(), workflow.DurableConfig{Store: newStore(t), Version: "v1"})
	require.NoError(t, err)
	_, err = run.Run(context.Background(), "panic", panicJSONState{}, "")
	require.Error(t, err)
	// Lock was released; the failed branch can be retried rather than hanging.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = run.Snapshot(ctx, "panic")
	require.NoError(t, err)
}
