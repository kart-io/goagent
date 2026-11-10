package builder

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// Handle must not claim a new turn after cancellation has already won at the
// invocation boundary, even if the completed invocation returned no error.
func TestHandleCancellationBetweenTurnsRetainsFollowup(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	boom := errors.New("canceled between turns")
	h := &Handle{inbox: &inbox{}, cancel: cancel, done: make(chan struct{})}
	require.NoError(t, h.Followup("second"))
	calls := 0
	h.run(ctx, func(context.Context, string) (*AgentOutput, error) {
		calls++
		cancel(boom)
		return &AgentOutput{Result: "first"}, nil
	}, "first")
	_, err := h.Wait(context.Background())
	require.ErrorIs(t, err, boom)
	require.Equal(t, 1, calls)
	require.Equal(t, []PendingInput{{Kind: "followup", Text: "second"}}, h.PendingInputs())
}

// 恢复时按记录的批次继续 Followup 轮次：宿主只需重建 Start，子流程从已保存的审批续跑，不重复之前的模型请求。
func TestHandleFollowupApprovalRecoveryUsesRecordedBatch(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "followup-run"), store)
	effects := map[string]int{}
	build := func(model *scriptModel, gate func()) *ConfigurableAgent[*core.AgentState] {
		var first sync.Once
		agent, err := NewSimpleBuilder(model).WithDurableScope("followup").WithMaxIterations(2).
			WithTools(dispatcherFor(t, refundTool(t, refundGraph(t, store, effects), "refund_workflow")).Tools()...).
			WithRequestHook(func(context.Context, *ModelRequest) error {
				if gate != nil {
					first.Do(gate)
				}
				return nil
			}).Build()
		require.NoError(t, err)
		return agent
	}
	started, release := make(chan struct{}), make(chan struct{})
	model := newScriptModel(answer("first answer"), step{run: func(_ context.Context, send func(*llm.StreamChunk) bool) {
		send(&llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("refund-1", "refund_workflow", `{"order_id":"A1"}`)}})
	}})
	h := build(model, func() { close(started); <-release }).Start(ctx, "first")
	<-started
	require.NoError(t, h.Followup("second"))
	close(release)
	_, err = h.Wait(context.Background())
	wait, continuation := requireWaiting(t, err)
	require.Empty(t, h.PendingInputs(), "the waiting Followup has already been claimed")
	require.Empty(t, effects)
	resumeCtx := nestedApprovalContext(t, ctx, wait.Path[len(wait.Path)-1].Key, continuation.ApprovalKey)

	resumed := newScriptModel(answer("refund completed"))
	require.Equal(t, "refund completed", waitAnswer(t, build(resumed, nil).Start(resumeCtx, "first")).Result)
	require.Equal(t, 1, resumed.count())
	require.Len(t, effects, 1)
	for _, count := range effects {
		require.Equal(t, 1, count)
	}
}

// A custom cancellation cause must not erase a known execution/cleanup error.
func TestHandleCancellationPreservesExecutionError(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	boom, cleanupErr := errors.New("canceled"), errors.New("cleanup failed")
	h := &Handle{inbox: &inbox{}, cancel: cancel, done: make(chan struct{})}
	h.run(ctx, func(context.Context, string) (*AgentOutput, error) {
		cancel(boom)
		return nil, errors.Join(context.Canceled, cleanupErr)
	}, "first")
	_, err := h.Wait(context.Background())
	require.ErrorIs(t, err, boom)
	require.ErrorIs(t, err, cleanupErr)
}

// The owned Runnable's cleanup is deliberately paused. Wait may time out, but
// must not report the Handle itself finished while its execution still owns it.
func TestHandleInterruptWaitsForOwnedCleanup(t *testing.T) {
	started, cleanupStarted := make(chan struct{}), make(chan struct{})
	release, cleaned := make(chan struct{}), make(chan struct{})
	cleanupErr := errors.New("owned cleanup failed")
	var releaseOnce sync.Once
	releaseCleanup := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseCleanup()
	child, err := tools.NewRunnableToolFactory(func(context.Context) (tools.OwnedRunnable[string, string], error) {
		return tools.OwnedRunnable[string, string]{
			Runnable: core.NewRunnableFunc(func(ctx context.Context, _ string) (string, error) {
				close(started)
				<-ctx.Done()
				return "", ctx.Err()
			}),
			Close: func(ctx context.Context) error {
				close(cleanupStarted)
				select {
				case <-release:
					close(cleaned)
					return cleanupErr
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		}, nil
	}, tools.RunnableToolConfig[string, string]{
		Name: "lookup", Description: "child", Version: "v1", ArgsSchema: `{"type":"object"}`,
		Decode: func(map[string]any) (string, error) { return "task", nil },
		Encode: func(result string) (any, error) { return result, nil },
	})
	require.NoError(t, err)
	model := newScriptModel(callTool("child-1", "lookup"))
	agent, err := NewSimpleBuilder(model).WithTools(child).WithMaxIterations(2).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "first")
	<-started
	require.NoError(t, h.Followup("second"))
	boom := errors.New("user canceled")
	h.Interrupt(boom)
	<-cleanupStarted
	waitCtx, stopWaiting := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stopWaiting()
	_, err = h.Wait(waitCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	releaseCleanup()
	settleCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = h.Wait(settleCtx)
	require.ErrorIs(t, err, boom)
	require.ErrorIs(t, err, cleanupErr)
	select {
	case <-cleaned:
	default:
		t.Fatal("Handle finished before owned cleanup")
	}
	require.Empty(t, agent.executionGate, "execution scope released before Handle completion")
	require.Equal(t, []PendingInput{{Kind: "followup", Text: "second"}}, h.PendingInputs())
	require.Equal(t, 1, model.count())
}
