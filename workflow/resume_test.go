package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

func addHumanNumber(_ context.Context, _ string, snap *workflow.GraphSnapshot[int], input json.RawMessage) (int, error) {
	var amount int
	if err := json.Unmarshal(input, &amount); err != nil || amount <= 0 {
		return 0, errors.New("positive integer required")
	}
	return snap.State + amount, nil
}

func TestResumeHumanInputUsesLatestCheckpointAndRejectsStaleKey(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var effects atomic.Int32
	graph := oneNode(t, func(_ context.Context, n int) (int, error) { effects.Add(1); return n * 2, nil })
	cfg := workflow.DurableConfig{Store: store, Version: "human-v1", ApprovalNodes: []string{"effect"}}
	run, err := graph.CompileDurable(ctx, cfg, workflow.WithResumeHandler(addHumanNumber))
	require.NoError(t, err)
	wait, err := run.Run(ctx, "human", 10, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.True(t, wait.Waiting)
	require.Zero(t, effects.Load())
	// Reconstruct the graph; the host's new initial state cannot replace saved state.
	run, err = graph.CompileDurable(ctx, cfg, workflow.WithResumeHandler(addHumanNumber))
	require.NoError(t, err)
	out, err := run.AsRunnable().Invoke(ctx, workflow.RunRequest[int]{RunID: "human", Initial: 999,
		Resume: &workflow.ResumeRequest{Key: wait.Key, Input: json.RawMessage(`3`)}})
	require.NoError(t, err)
	require.True(t, out.Done)
	require.Equal(t, 26, out.State)
	require.EqualValues(t, 1, effects.Load())
	_, err = run.Resume(ctx, "human", workflow.ResumeRequest{Key: wait.Key, Input: json.RawMessage(`100`)})
	require.ErrorIs(t, err, workflow.ErrApprovalMismatch)
	replayed, err := run.Run(ctx, "human", 999, "")
	require.NoError(t, err)
	require.Equal(t, 26, replayed.State)
	require.EqualValues(t, 1, effects.Load())
}

func TestResumeRejectsWithoutChangingCheckpoint(t *testing.T) {
	for _, kind := range []string{"missing_key", "stale_key", "invalid_json", "handler_error", "handler_panic", "missing_handler", "authorization", "authorization_canceled", "save_failure", "canceled", "conflicting_request"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &checkpointFailStore{Store: newStore(t)}
			effects := 0
			g := workflow.NewGraph[map[string]int]()
			require.NoError(t, g.AddNode("effect", func(_ context.Context, in map[string]int) (map[string]int, error) { effects++; return in, nil }))
			require.NoError(t, g.AddEdge(workflow.START, "effect"))
			require.NoError(t, g.AddEdge("effect", workflow.END))
			var opts []workflow.DurableOption[map[string]int]
			if kind != "missing_handler" {
				opts = append(opts, workflow.WithResumeHandler(func(_ context.Context, _ string, snap *workflow.GraphSnapshot[map[string]int], _ json.RawMessage) (map[string]int, error) {
					snap.State["n"] = 99 // mutation must not leak on rejection
					if kind == "handler_error" {
						return nil, errors.New("invalid decision")
					}
					if kind == "handler_panic" {
						panic("boom")
					}
					if kind == "canceled" {
						cancel()
					}
					return snap.State, nil
				}))
			}
			if kind == "authorization" || kind == "authorization_canceled" {
				opts = append(opts, workflow.WithBeforeApprove(func(_ context.Context, _ string, snap *workflow.GraphSnapshot[map[string]int]) error {
					require.Equal(t, 99, snap.State["n"], "authorization sees proposed state")
					if kind == "authorization_canceled" {
						cancel()
						return nil
					}
					return errors.New("not authorized")
				}))
			}
			run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}}, opts...)
			require.NoError(t, err)
			wait, err := run.Run(ctx, "human", map[string]int{"n": 1}, "")
			require.ErrorIs(t, err, durable.ErrApprovalRequired)
			before, err := store.Load(ctx, "human")
			require.NoError(t, err)
			req := workflow.ResumeRequest{Key: wait.Key, Input: json.RawMessage(`{}`)}
			switch kind {
			case "missing_key":
				req.Key = ""
			case "stale_key":
				req.Key = "forged"
			case "invalid_json":
				req.Input = json.RawMessage(`{`)
			case "save_failure":
				store.fail = errors.New("disk full")
			}
			if kind == "conflicting_request" {
				_, err = run.AsRunnable().Invoke(ctx, workflow.RunRequest[map[string]int]{RunID: "human", ApprovalKey: wait.Key, Resume: &req})
			} else {
				_, err = run.Resume(ctx, "human", req)
			}
			require.Error(t, err)
			if kind == "handler_panic" {
				var panicErr *agentErrors.PanicError
				require.ErrorAs(t, err, &panicErr)
			}
			if kind == "canceled" || kind == "authorization_canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			after, err := store.Load(context.Background(), "human")
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Zero(t, effects)
			require.Equal(t, 1, wait.State["n"])
		})
	}
}

func TestResumeAcceptanceSurvivesNodeFailureWithoutReapplyingInput(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	fail := true
	handlers := 0
	g := oneNode(t, func(_ context.Context, n int) (int, error) {
		if fail {
			return 0, errors.New("downstream unavailable")
		}
		return n, nil
	})
	run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}},
		workflow.WithResumeHandler(func(ctx context.Context, id string, snap *workflow.GraphSnapshot[int], input json.RawMessage) (int, error) {
			handlers++
			return addHumanNumber(ctx, id, snap, input)
		}))
	require.NoError(t, err)
	wait, err := run.Run(ctx, "human", 10, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	_, err = run.Resume(ctx, "human", workflow.ResumeRequest{Key: wait.Key, Input: json.RawMessage(`3`)})
	require.ErrorContains(t, err, "downstream unavailable")
	snap, err := run.Snapshot(ctx, "human")
	require.NoError(t, err)
	require.False(t, snap.Waiting)
	require.Equal(t, 13, snap.State)
	fail = false
	done, err := run.Run(ctx, "human", 0, "")
	require.NoError(t, err)
	require.Equal(t, 13, done.State)
	require.Equal(t, 1, handlers)
}

func TestConcurrentResumeAcceptsOnlyOneDecision(t *testing.T) {
	ctx := context.Background()
	var effects atomic.Int32
	g := oneNode(t, func(_ context.Context, n int) (int, error) { effects.Add(1); return n, nil })
	cfg := workflow.DurableConfig{Store: newStore(t), Version: "v1", ApprovalNodes: []string{"effect"}}
	run, err := g.CompileDurable(ctx, cfg, workflow.WithResumeHandler(addHumanNumber))
	require.NoError(t, err)
	other, err := g.CompileDurable(ctx, cfg, workflow.WithResumeHandler(addHumanNumber))
	require.NoError(t, err)
	wait, err := run.Run(ctx, "human", 10, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, r := range []*workflow.DurableGraph[int]{run, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Resume(ctx, "human", workflow.ResumeRequest{Key: wait.Key, Input: json.RawMessage(`3`)})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			require.ErrorIs(t, err, workflow.ErrApprovalMismatch)
		}
	}
	require.Equal(t, 1, accepted)
	require.EqualValues(t, 1, effects.Load())
}

func TestChildResumeInputRejectionRemainsResumable(t *testing.T) {
	store := newStore(t)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
	g, err := oneNode(t, func(_ context.Context, n int) (int, error) { return n, nil }).CompileDurable(ctx,
		workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}}, workflow.WithResumeHandler(addHumanNumber))
	require.NoError(t, err)
	_, err = runChild(ctx, g, "")
	waits := durable.Waits(err)
	require.Len(t, waits, 1)
	c := childContinuation(t, err)
	key := waits[0].Path[len(waits[0].Path)-1].Key
	for _, input := range []string{`-1`, `3`} {
		resumed, e := workflow.WithResume(ctx, key, workflow.ResumeRequest{Key: c.ApprovalKey, Input: json.RawMessage(input)})
		require.NoError(t, e)
		record, err := runChild(resumed, g, "")
		if input == `-1` {
			require.Equal(t, "waiting", record.Status)
			rejected := childContinuation(t, err)
			require.Equal(t, workflow.RejectResumeInput, rejected.Reason)
		} else {
			require.NoError(t, err)
			require.Equal(t, "completed", record.Status)
			require.JSONEq(t, `4`, string(record.Value))
		}
	}
}

func TestResumeHandlerCannotBeBypassedWithApprovalOnly(t *testing.T) {
	ctx := context.Background()
	run, err := oneNode(t, func(_ context.Context, n int) (int, error) { return n, nil }).CompileDurable(ctx,
		workflow.DurableConfig{Store: newStore(t), Version: "v1", ApprovalNodes: []string{"effect"}}, workflow.WithResumeHandler(addHumanNumber))
	require.NoError(t, err)
	wait, err := run.Run(ctx, "human", 10, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	_, err = run.Run(ctx, "human", 0, wait.Key)
	var rejected *workflow.ResumeRejectedError
	require.ErrorAs(t, err, &rejected)
	_, err = run.Resume(ctx, "human", workflow.ResumeRequest{Key: wait.Key})
	require.ErrorAs(t, err, &rejected)
	_, err = run.Resume(ctx, "human", workflow.ResumeRequest{Key: wait.Key, Input: json.RawMessage(`null`)})
	require.ErrorAs(t, err, &rejected)
	snap, err := run.Snapshot(ctx, "human")
	require.NoError(t, err)
	require.True(t, snap.Waiting)
	require.Equal(t, 10, snap.State)
}

func TestResumeStreamStopsAtNextGateWithDistinctKey(t *testing.T) {
	ctx := context.Background()
	g := workflow.NewGraph[int]()
	require.NoError(t, g.AddNode("loop", func(_ context.Context, n int) (int, error) { return n, nil }))
	require.NoError(t, g.AddEdge(workflow.START, "loop"))
	require.NoError(t, g.AddBranch("loop", func(_ context.Context, n int) (string, error) {
		if n >= 2 {
			return workflow.END, nil
		}
		return "loop", nil
	}, "loop", workflow.END))
	run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: newStore(t), Version: "v1", ApprovalNodes: []string{"loop"}}, workflow.WithResumeHandler(addHumanNumber))
	require.NoError(t, err)
	wait, err := run.Run(ctx, "human", 0, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	chunks, err := run.AsRunnable().Stream(ctx, workflow.RunRequest[int]{RunID: "human", Resume: &workflow.ResumeRequest{Key: wait.Key, Input: json.RawMessage(`1`)}})
	require.NoError(t, err)
	var next *workflow.GraphSnapshot[int]
	finals := 0
	for chunk := range chunks {
		require.NoError(t, chunk.Error)
		if chunk.Done {
			finals++
			next = chunk.Data
		}
	}
	require.Equal(t, 1, finals)
	require.True(t, next.Waiting)
	require.False(t, next.Done)
	require.Equal(t, 1, next.State)
	require.NotEqual(t, wait.Key, next.Key)
	_, err = run.Resume(ctx, "human", workflow.ResumeRequest{Key: wait.Key, Input: json.RawMessage(`9`)})
	require.ErrorIs(t, err, workflow.ErrApprovalMismatch)
	done, err := run.Resume(ctx, "human", workflow.ResumeRequest{Key: next.Key, Input: json.RawMessage(`1`)})
	require.NoError(t, err)
	require.True(t, done.Done)
	require.Equal(t, 2, done.State)
}
