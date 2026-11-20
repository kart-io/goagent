package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
)

const emptySchema = `{"type":"object","properties":{},"additionalProperties":false}`

func TestDispatcherZeroConcurrencyAndTimeoutUnlimited(t *testing.T) {
	const parallel = 12
	var running, peak atomic.Int32
	release := make(chan struct{})
	var deadlines sync.Map
	tool := NewBaseTool("slow", "test", emptySchema, func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		_, has := ctx.Deadline()
		deadlines.Store(in.CallID, has)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		running.Add(-1)
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	})
	d := dispatcherWith(t, DispatchConfig{}, tool)
	var wg sync.WaitGroup
	errs := make(chan error, parallel)
	for i := range parallel {
		wg.Go(func() {
			_, err := d.Invoke(context.Background(), ToolRequest{Name: "slow", Input: &interfaces.ToolInput{CallID: fmt.Sprintf("c%d", i), Args: map[string]any{}}})
			errs <- err
		})
	}
	require.Eventually(t, func() bool { return running.Load() == parallel }, 5*time.Second, time.Millisecond, "旧默认并发 4 会卡住其余调用")
	// 超时为 0 时调用不得立即到期：等待一段时间后仍在运行。
	time.Sleep(50 * time.Millisecond)
	require.EqualValues(t, parallel, running.Load())
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, parallel, peak.Load())
	deadlines.Range(func(_, has any) bool {
		require.False(t, has.(bool), "Timeout 为 0 时不应设置调用截止时间")
		return true
	})

	_, err := NewDispatcher(NewRegistry(), DispatchConfig{Timeout: -time.Second})
	require.Error(t, err)
	_, err = NewDispatcher(NewRegistry(), DispatchConfig{MaxConcurrency: -1})
	require.Error(t, err)
}

func TestTimeoutProviderZeroUsesDispatcherDefault(t *testing.T) {
	for _, tc := range []struct {
		name              string
		dispatch, perTool time.Duration
		want              time.Duration // 0 表示不应有截止时间
	}{
		{"工具返回 0 沿用 Dispatcher 超时", time.Hour, 0, time.Hour},
		{"工具返回正值覆盖", time.Hour, time.Minute, time.Minute},
		{"两者都为 0 时不限", 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var left time.Duration
			var has bool
			tool := &timeoutTool{BaseTool: NewBaseTool("t", "test", emptySchema, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				var dl time.Time
				dl, has = ctx.Deadline()
				left = time.Until(dl)
				return &interfaces.ToolOutput{Success: true}, nil
			}), timeout: tc.perTool}
			d := dispatcherWith(t, DispatchConfig{Timeout: tc.dispatch}, tool)
			_, err := d.Invoke(context.Background(), ToolRequest{Name: "t", Input: &interfaces.ToolInput{Args: map[string]any{}}})
			require.NoError(t, err)
			require.Equal(t, tc.want > 0, has)
			if tc.want > 0 {
				require.InDelta(t, float64(tc.want), float64(left), float64(time.Second))
			}
		})
	}
}

func TestDispatchConfigDefaultMaxAttemptsStillThree(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
	var calls atomic.Int32
	tool := NewBaseTool("flaky", "test", emptySchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls.Add(1)
		return nil, errors.New("boom")
	})
	d := dispatcherWith(t, DispatchConfig{}, tool)
	req := ToolRequest{Name: "flaky", Input: &interfaces.ToolInput{CallID: "c1", Args: map[string]any{}}}
	for range 4 {
		_, err := d.Invoke(ctx, req)
		require.Error(t, err)
	}
	require.EqualValues(t, 3, calls.Load(), "持久化的默认尝试次数仍为 3")
}

func TestRunnableToolZeroAttemptsNoRetry(t *testing.T) {
	cfg := func(attempts int) RunnableToolConfig[string, string] {
		return RunnableToolConfig[string, string]{
			Name: "child", Version: "v1", ArgsSchema: emptySchema, Resumable: true, MaxAttempts: attempts,
			Decode: func(map[string]any) (string, error) { return "", nil },
			Encode: func(s string) (any, error) { return s, nil },
		}
	}
	run := core.NewRunnableFunc(func(context.Context, string) (string, error) { return "ok", nil })
	for _, tc := range []struct {
		attempts, want int
	}{
		{0, 1},
		{1, 1},
		{3, 3},
	} {
		tool, err := NewRunnableTool(run, cfg(tc.attempts))
		require.NoError(t, err)
		policy := tool.(DurablePolicyProvider).DurableOperationPolicy()
		require.Equal(t, durable.OperationPolicy{MaxAttempts: tc.want, Resumable: true}, policy, "MaxAttempts=%d", tc.attempts)
	}
	_, err := NewRunnableTool(run, cfg(-1))
	require.Error(t, err)
}

func TestRunnableDepthAntiAbuseCap(t *testing.T) {
	depthOf := func(maxDepth, nest int) error {
		ctx := context.Background()
		var err error
		for range nest {
			if ctx, err = enterRunnable(ctx, maxDepth, false); err != nil {
				return err
			}
		}
		return nil
	}
	require.NoError(t, depthOf(0, DefaultRunnableMaxDepth))
	require.Error(t, depthOf(DefaultRunnableMaxDepth, DefaultRunnableMaxDepth+1), "0 仍是默认深度 8")
	require.NoError(t, depthOf(UnlimitedRunnableDepth, 50))
	_, err := NewRunnableTool(core.NewRunnableFunc(func(context.Context, string) (string, error) { return "", nil }), RunnableToolConfig[string, string]{
		Name: "x", Version: "v1", ArgsSchema: emptySchema, MaxDepth: -2,
		Decode: func(map[string]any) (string, error) { return "", nil },
		Encode: func(s string) (any, error) { return s, nil },
	})
	require.Error(t, err)
}

func TestApprovalTimeoutZeroWaitsForContext(t *testing.T) {
	var hasDeadline atomic.Bool
	p := &Pipeline{Pre: []PreHook{decide(PreAsk, "")}, Approver: ApproverFunc(func(ctx context.Context, _ ApprovalRequest) (ApprovalResult, error) {
		_, has := ctx.Deadline()
		hasDeadline.Store(has)
		return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
	})}
	f := newPipelineFixture(t, p, DispatchConfig{})
	out, err := f.call(context.Background(), "ls")
	require.NoError(t, err)
	require.Equal(t, "ran ls", out.Result)
	require.False(t, hasDeadline.Load(), "旧默认 10 分钟")

	_, err = NewDispatcher(NewRegistry(), DispatchConfig{Pipeline: &Pipeline{ApprovalTimeout: -time.Second}})
	require.Error(t, err)
}
