package builder

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// resendFixture 用保存失败模拟进程在回合落盘前崩溃。
type resendFixture struct {
	ctx   context.Context
	crash *bool
	c     *budget.Controller
}

func newResendFixture(t *testing.T, maxRequests int) resendFixture {
	t.Helper()
	inner, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	crash := new(bool)
	store := failOnSave{Store: inner, fail: func(v *durable.Snapshot) bool {
		return *crash && v.Version == "goagent-operation-v2" && !bytesContains(v.Data, `"pending"`)
	}}
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
	c, err := budget.NewDurable(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: maxRequests}, inner)
	require.NoError(t, err)
	return resendFixture{ctx: budget.WithController(ctx, c), crash: crash, c: c}
}

func resendAgent(t *testing.T, model llm.Client) *core.StreamRunnable[any, *AgentOutput] {
	t.Helper()
	agent, err := NewSimpleBuilder(model).WithDurableScope("resend").WithInterruptedModelResend().Build()
	require.NoError(t, err)
	return agent.AsRunnable()
}

func TestModelResendAfterCrash(t *testing.T) {
	f := newResendFixture(t, 5)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("丢失", 7), answerTurn("好的", 7))}
	agent := resendAgent(t, model)

	*f.crash = true
	_, err := agent.Invoke(f.ctx, "q")
	require.ErrorContains(t, err, "crash")
	*f.crash = false

	out, err := agent.Invoke(f.ctx, "q")
	require.NoError(t, err)
	require.Equal(t, "好的", out.Result)
	require.Equal(t, 2, model.calls)

	replayed, err := agent.Invoke(f.ctx, "q")
	require.NoError(t, err)
	require.Equal(t, "好的", replayed.Result)
	require.Equal(t, 2, model.calls, "已提交的回合不再发请求")

	snap, err := f.c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, snap.ReservedRequests, "重发计入预算")
	require.Equal(t, 14, snap.KnownTokens.TotalTokens, "回放按重发那次的键登记，不冲突、不重复")
}

// stuckModel 的第 stuckAt 次请求一直挂起，直到 ctx 被取消，模拟进程停止时正在等模型。
type stuckModel struct {
	*nativeEventModel
	calls   int
	stuckAt int
	started chan struct{}
}

func (m *stuckModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	m.calls++
	if m.calls == m.stuckAt {
		close(m.started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return m.nativeEventModel.CompleteToolsStream(ctx, req, tools)
}

func (m *stuckModel) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return m.CompleteToolsStream(ctx, req, nil)
}

func TestModelResendAfterStop(t *testing.T) {
	f := newResendFixture(t, 5)
	model := &stuckModel{stuckAt: 1, nativeEventModel: nativeModel(answerTurn("好的", 7)), started: make(chan struct{})}
	agent := resendAgent(t, model)

	stopping, stop := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() {
		_, err := agent.Invoke(stopping, "q")
		done <- err
	}()
	<-model.started
	stop()
	require.ErrorIs(t, <-done, context.Canceled)

	out, err := agent.Invoke(f.ctx, "q")
	require.NoError(t, err)
	require.Equal(t, "好的", out.Result)
	require.Equal(t, 2, model.calls)

	// 回放登记在重发那次的键下；被打断的第一次没有用量，不能算到它头上。
	_, err = agent.Invoke(f.ctx, "q")
	require.NoError(t, err)
	snap, err := f.c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 7, snap.KnownTokens.TotalTokens)
}

func TestModelResendStopsAtAttemptLimit(t *testing.T) {
	f := newResendFixture(t, 10)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("a", 1), answerTurn("b", 1), answerTurn("c", 1), answerTurn("d", 1))}
	agent := resendAgent(t, model)

	*f.crash = true
	for range modelResendAttempts {
		_, err := agent.Invoke(f.ctx, "q")
		require.ErrorContains(t, err, "crash")
	}
	*f.crash = false
	_, err := agent.Invoke(f.ctx, "q")
	require.ErrorIs(t, err, agentErrors.ErrOperationAttention)
	require.Equal(t, modelResendAttempts, model.calls)
}

func TestModelResendRejectedByBudgetBeforeSend(t *testing.T) {
	f := newResendFixture(t, 1)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("丢失", 7), answerTurn("不应发出", 7))}
	agent := resendAgent(t, model)

	*f.crash = true
	_, err := agent.Invoke(f.ctx, "q")
	require.ErrorContains(t, err, "crash")
	*f.crash = false

	_, err = agent.Invoke(f.ctx, "q")
	require.ErrorIs(t, err, agentErrors.ErrModelBudgetExceeded)
	require.Equal(t, 1, model.calls, "额度不足时不发请求")
}

func TestModelResendSkipsOrdinaryFailure(t *testing.T) {
	f := newResendFixture(t, 5)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("不应发出", 7)), steps: []error{unavailable(400, 0)}}
	agent := resendAgent(t, model)

	_, err := agent.Invoke(f.ctx, "q")
	require.Error(t, err)
	_, err = agent.Invoke(f.ctx, "q")
	require.ErrorIs(t, err, agentErrors.ErrOperationAttention)
	require.Equal(t, 1, model.calls, "模型明确拒绝的请求不是中断")
}

// 请求内重试和重启后重发都消耗预算，两者的键不能撞车。
func TestModelResendWithRequestRetries(t *testing.T) {
	f := newResendFixture(t, 5)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("丢失", 7), answerTurn("好的", 7)), steps: []error{unavailable(503, 0), nil, unavailable(503, 0)}}
	agent, err := NewSimpleBuilder(model).WithDurableScope("resend").WithInterruptedModelResend().WithRequestError(2, RetryTransient(2, noWait)).Build()
	require.NoError(t, err)

	*f.crash = true
	_, err = agent.AsRunnable().Invoke(f.ctx, "q")
	require.ErrorContains(t, err, "crash")
	*f.crash = false

	out, err := agent.AsRunnable().Invoke(f.ctx, "q")
	require.NoError(t, err)
	require.Equal(t, "好的", out.Result)
	require.Equal(t, 4, model.calls)

	_, err = agent.AsRunnable().Invoke(f.ctx, "q")
	require.NoError(t, err)
	snap, err := f.c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 4, snap.ReservedRequests)
	require.Equal(t, 14, snap.KnownTokens.TotalTokens)
}

func TestModelResendIsNotInheritedByChildAgent(t *testing.T) {
	f := newResendFixture(t, 5)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("丢失", 7), answerTurn("不应发出", 7))}
	child, err := NewSimpleBuilder(model).WithDurableScope("resend").Build()
	require.NoError(t, err)
	ctx := withInterruptedModelResend(f.ctx, true)

	*f.crash = true
	_, err = child.AsRunnable().Invoke(ctx, "q")
	require.ErrorContains(t, err, "crash")
	*f.crash = false
	_, err = child.AsRunnable().Invoke(ctx, "q")
	require.ErrorIs(t, err, agentErrors.ErrOperationAttention)
	require.Equal(t, 1, model.calls)
}

// 工具循环和纯问答走不同的请求路径，开关要在两边都生效：已完成的工具不重跑，只重发被打断的回合。
func TestModelResendInDurableToolLoop(t *testing.T) {
	for _, resend := range []bool{true, false} {
		f := newResendFixture(t, 5)
		model := &stuckModel{stuckAt: 2, started: make(chan struct{}), nativeEventModel: nativeModel(
			[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "lookup", `{}`)}}},
			answerTurn("好的", 7),
		)}
		calls := 0
		reg := tools.NewRegistry()
		require.NoError(t, reg.Register(tools.NewBaseTool("lookup", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			calls++
			return &interfaces.ToolOutput{Success: true, Result: "found"}, nil
		})))
		dispatch, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
		require.NoError(t, err)
		b := NewSimpleBuilder(model).WithDurableScope("resend").WithTools(dispatch.Tools()...)
		if resend {
			b = b.WithInterruptedModelResend()
		}
		agent, err := b.Build()
		require.NoError(t, err)

		stopping, stop := context.WithCancel(f.ctx)
		done := make(chan error, 1)
		go func() {
			_, err := agent.AsRunnable().Invoke(stopping, "q")
			done <- err
		}()
		<-model.started
		stop()
		require.ErrorIs(t, <-done, context.Canceled)

		out, err := agent.AsRunnable().Invoke(f.ctx, "q")
		if resend {
			require.NoError(t, err)
			require.Equal(t, "好的", out.Result)
			require.Equal(t, 3, model.calls)
		} else {
			require.ErrorIs(t, err, agentErrors.ErrOperationAttention)
			require.Equal(t, 2, model.calls)
		}
		require.Equal(t, 1, calls, "已完成的工具不重跑")
	}
}
