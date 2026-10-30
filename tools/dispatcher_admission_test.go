package tools

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func admissionRequest(name string) ToolRequest {
	return ToolRequest{Name: name, Input: &interfaces.ToolInput{Args: map[string]any{}}}
}
func admissionStart(ctx context.Context, d *Dispatcher, name string) <-chan error {
	done := make(chan error, 1)
	go func() { _, err := d.Invoke(ctx, admissionRequest(name)); done <- err }()
	return done
}
func admissionWait(t *testing.T, ctx context.Context, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatal("phase not reached before host timeout")
	}
}
func admissionQueued(t *testing.T, d *Dispatcher, n int) {
	t.Helper()
	require.Eventually(t, func() bool { d.queueMu.Lock(); defer d.queueMu.Unlock(); return d.queued == n }, time.Second, time.Millisecond)
}

func TestDispatcherApprovalDoesNotOccupyExecutionSlot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	approval := make(chan struct{})
	release := make(chan struct{})
	p := &Pipeline{Pre: []PreHook{func(_ context.Context, c *Call) (PreDecision, error) {
		if c.Tool == "approve" {
			return PreDecision{Kind: PreAsk}, nil
		}
		return PreDecision{}, nil
	}}, Approver: ApproverFunc(func(ctx context.Context, _ ApprovalRequest) (ApprovalResult, error) {
		close(approval)
		select {
		case <-release:
			return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
		case <-ctx.Done():
			return ApprovalResult{}, ctx.Err()
		}
	})}
	mk := func(name string) *BaseTool {
		return NewBaseTool(name, "", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Success: true}, nil
		})
	}
	d := argDispatcher(t, DispatchConfig{MaxConcurrency: 1, Pipeline: p}, mk("approve"), mk("read"))
	done := admissionStart(ctx, d, "approve")
	admissionWait(t, ctx, approval)
	short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	_, err := d.Invoke(short, admissionRequest("read"))
	require.NoError(t, err)
	close(release)
	require.NoError(t, <-done)
}

func TestDispatcherExecutionQueuePausesTimeoutAfterAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	holdEntered := make(chan struct{})
	releaseHold := make(chan struct{})
	approveEntered := make(chan struct{})
	releaseApprove := make(chan struct{})
	hold := &timeoutTool{BaseTool: NewBaseTool("hold", "", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		close(holdEntered)
		select {
		case <-releaseHold:
			return &interfaces.ToolOutput{Success: true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}), timeout: 2 * time.Second}
	p := &Pipeline{Pre: []PreHook{func(ctx context.Context, c *Call) (PreDecision, error) {
		if c.Tool == "quick" {
			close(approveEntered)
			select {
			case <-releaseApprove:
			case <-ctx.Done():
				return PreDecision{}, ctx.Err()
			}
		}
		return PreDecision{}, nil
	}}}
	quick := remainingTool("quick", 80*time.Millisecond, nil)
	d := argDispatcher(t, DispatchConfig{MaxConcurrency: 1, Pipeline: p}, hold, quick)
	held := admissionStart(ctx, d, "hold")
	admissionWait(t, ctx, holdEntered)
	result := make(chan *interfaces.ToolOutput, 1)
	done := make(chan error, 1)
	go func() { out, err := d.Invoke(ctx, admissionRequest("quick")); result <- out; done <- err }()
	admissionWait(t, ctx, approveEntered)
	timer := time.NewTimer(110 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(releaseApprove)
	admissionQueued(t, d, 1)
	timer.Reset(110 * time.Millisecond)
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(releaseHold)
	require.NoError(t, <-held)
	require.NoError(t, <-done)
	require.Greater(t, (<-result).Result.(time.Duration), 40*time.Millisecond)
}

func TestDispatcherBothQueuesShareLimitAndCancellationDoesNotExecute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	holdEntered := make(chan struct{})
	releaseHold := make(chan struct{})
	gateEntered := make(chan struct{})
	releaseGate := make(chan struct{})
	var calls atomic.Int32
	hold := NewBaseTool("hold", "", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		close(holdEntered)
		select {
		case <-releaseHold:
			return &interfaces.ToolOutput{Success: true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	quick := NewBaseTool("quick", "", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls.Add(1)
		return &interfaces.ToolOutput{Success: true}, nil
	})
	gate := NewBaseTool("gate", "", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls.Add(1)
		return &interfaces.ToolOutput{Success: true}, nil
	})
	p := &Pipeline{Pre: []PreHook{func(ctx context.Context, c *Call) (PreDecision, error) {
		if c.Tool == "gate" {
			close(gateEntered)
			select {
			case <-releaseGate:
			case <-ctx.Done():
				return PreDecision{}, ctx.Err()
			}
			return PreDecision{Kind: PreDeny, Feedback: "denied"}, nil
		}
		return PreDecision{}, nil
	}}}
	d := argDispatcher(t, DispatchConfig{MaxConcurrency: 1, MaxAdmissionConcurrency: 1, MaxQueued: 1, Pipeline: p}, hold, quick, gate)
	held := admissionStart(ctx, d, "hold")
	admissionWait(t, ctx, holdEntered)
	waitCtx, stopWait := context.WithCancel(ctx)
	waiting := admissionStart(waitCtx, d, "quick")
	admissionQueued(t, d, 1)
	gated := admissionStart(ctx, d, "gate")
	admissionWait(t, ctx, gateEntered)
	_, err := d.Invoke(ctx, admissionRequest("quick"))
	require.ErrorIs(t, err, ErrDispatcherBusy)
	stopWait()
	require.ErrorIs(t, <-waiting, context.Canceled)
	admissionQueued(t, d, 0)
	waitCtx2, stopWait2 := context.WithCancel(ctx)
	waiting2 := admissionStart(waitCtx2, d, "quick")
	admissionQueued(t, d, 1)
	_, err = d.Invoke(ctx, admissionRequest("quick"))
	require.ErrorIs(t, err, ErrDispatcherBusy)
	stopWait2()
	require.ErrorIs(t, <-waiting2, context.Canceled)
	close(releaseGate)
	require.NoError(t, <-gated) // 拒绝结果跳过繁忙的执行槽
	close(releaseHold)
	require.NoError(t, <-held)
	require.Zero(t, calls.Load())
}

func TestDispatcherSubstituteAuthorizationConsumesOriginalTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var calls atomic.Int32
	target := &timeoutTool{BaseTool: NewBaseTool("target", "", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls.Add(1)
		return &interfaces.ToolOutput{Success: true}, nil
	}), timeout: time.Second}
	source := NewBaseTool("source", "", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls.Add(1)
		return &interfaces.ToolOutput{Success: true}, nil
	})
	p := &Pipeline{Pre: []PreHook{func(_ context.Context, c *Call) (PreDecision, error) {
		if c.Tool == "source" {
			return PreDecision{Kind: PreSubstitute, Substitute: &Substitution{Tool: "target", Args: map[string]any{}}}, nil
		}
		return PreDecision{}, nil
	}}}
	d := argDispatcher(t, DispatchConfig{Timeout: 40 * time.Millisecond, Pipeline: p, Authorize: func(ctx context.Context, name string, _ *interfaces.ToolInput) error {
		if name != "target" {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}, source, target)
	_, err := d.Invoke(ctx, admissionRequest("source"))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, calls.Load())
}

func TestDispatcherReplaySkipsExecutionQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	journal := durable.WithJournal(durable.WithExecutionKey(ctx, "parent"), store)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	hold := NewBaseTool("hold", "", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		close(entered)
		select {
		case <-release:
			return &interfaces.ToolOutput{Success: true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	quick := NewBaseTool("quick", "", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls.Add(1)
		return &interfaces.ToolOutput{Success: true, Result: "receipt"}, nil
	})
	d := argDispatcher(t, DispatchConfig{MaxConcurrency: 1, MaxQueued: 1}, hold, quick)
	req := admissionRequest("quick")
	req.Input.CallID = "known"
	_, err = d.Invoke(journal, req)
	require.NoError(t, err)
	held := admissionStart(ctx, d, "hold")
	admissionWait(t, ctx, entered)
	short, stop := context.WithTimeout(journal, 200*time.Millisecond)
	defer stop()
	out, err := d.Invoke(short, req)
	require.NoError(t, err)
	require.Equal(t, "receipt", out.Result)
	require.EqualValues(t, 1, calls.Load())
	close(release)
	require.NoError(t, <-held)
}
