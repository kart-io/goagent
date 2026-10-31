package builder

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// flakyModel 的 steps[i] 非 nil 时第 i 次请求在建流时返回该错误，否则按 turns 回放下一个回合。
type flakyModel struct {
	*nativeEventModel
	steps []error
	calls int
}

func (m *flakyModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	step := m.calls
	m.calls++
	if step < len(m.steps) && m.steps[step] != nil {
		return nil, m.steps[step]
	}
	return m.nativeEventModel.CompleteToolsStream(ctx, req, tools)
}

func (m *flakyModel) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return m.CompleteToolsStream(ctx, req, nil)
}

func unavailable(status int, after time.Duration) error {
	return &llm.ProviderError{StatusCode: status, RetryAfter: after, Err: errors.New("upstream unavailable")}
}

func answerTurn(text string, tokens int) []*llm.StreamChunk {
	return []*llm.StreamChunk{{Delta: text}, {Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: tokens}}}
}

func noWait(int) time.Duration { return time.Millisecond }

func TestModelRetryRecoversTransientFailure(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 3})
	require.NoError(t, err)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("好的", 5)), steps: []error{unavailable(503, 0)}}
	agent, err := NewSimpleBuilder(model).WithRequestError(2, RetryTransient(2, noWait)).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(budget.WithController(context.Background(), c), "q")
	require.NoError(t, err)
	require.Equal(t, "好的", out.Result)
	require.EqualValues(t, 2, out.Metadata["new_model_invocations"], "重试计入实际请求数")
	snap, err := c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, snap.ReservedRequests, "每次尝试各占一份额度")
	require.Equal(t, 5, snap.KnownTokens.TotalTokens)
}

func TestModelRetryStopsWhenNotRetryable(t *testing.T) {
	for name, tc := range map[string]struct {
		model    *flakyModel
		retries  int
		requests int
		ctx      func() (context.Context, context.CancelFunc)
	}{
		"未开启":           {&flakyModel{nativeEventModel: nativeModel(answerTurn("x", 1)), steps: []error{unavailable(503, 0)}}, 0, 1, nil},
		"400":           {&flakyModel{nativeEventModel: nativeModel(answerTurn("x", 1)), steps: []error{unavailable(400, 0)}}, 2, 1, nil},
		"非 Provider 错误": {&flakyModel{nativeEventModel: nativeModel(answerTurn("x", 1)), steps: []error{errors.New("protocol")}}, 2, 1, nil},
		"次数用完":          {&flakyModel{nativeEventModel: nativeModel(answerTurn("x", 1)), steps: []error{unavailable(503, 0), unavailable(503, 0), unavailable(503, 0)}}, 2, 3, nil},
		"等待越过截止时间": {&flakyModel{nativeEventModel: nativeModel(answerTurn("x", 1)), steps: []error{unavailable(429, time.Hour)}}, 2, 1, func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 2*time.Second)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.Background(), func() {}
			if tc.ctx != nil {
				ctx, cancel = tc.ctx()
			}
			defer cancel()
			agent, err := NewSimpleBuilder(tc.model).WithRequestError(tc.retries, RetryTransient(tc.retries, noWait)).Build()
			require.NoError(t, err)
			started := time.Now()
			_, err = agent.AsRunnable().Invoke(ctx, "q")
			require.Error(t, err)
			require.Equal(t, tc.requests, tc.model.calls)
			require.Less(t, time.Since(started), time.Second)
		})
	}
}

func TestModelRetryDoesNotRepeatEmittedText(t *testing.T) {
	model := &flakyModel{nativeEventModel: nativeModel([]*llm.StreamChunk{{Delta: "半句"}, {Done: true, Error: unavailable(0, 0)}}, answerTurn("完整", 1))}
	agent, err := NewSimpleBuilder(model).WithRequestError(2, RetryTransient(2, noWait)).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "q")
	var pe *llm.ProviderError
	require.True(t, errors.As(err, &pe))
	require.Equal(t, 1, model.calls)
}

func TestModelRetryHonorsRetryAfter(t *testing.T) {
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("ok", 1)), steps: []error{unavailable(429, 60*time.Millisecond)}}
	agent, err := NewSimpleBuilder(model).WithRequestError(1, RetryTransient(1, func(int) time.Duration { return time.Hour })).Build()
	require.NoError(t, err)
	started := time.Now()
	out, err := agent.AsRunnable().Invoke(context.Background(), "q")
	require.NoError(t, err)
	require.Equal(t, "ok", out.Result)
	require.GreaterOrEqual(t, time.Since(started), 60*time.Millisecond)
}

func TestModelRetryDoesNotWaitBeyondRetryAfterCap(t *testing.T) {
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("ok", 1)), steps: []error{unavailable(429, 2*maxRetryAfter)}}
	agent, err := NewSimpleBuilder(model).WithRequestError(1, RetryTransient(1, noWait)).WithTimeout(time.Hour).Build()
	require.NoError(t, err)
	started := time.Now()
	_, err = agent.AsRunnable().Invoke(context.Background(), "q")
	var pe *llm.ProviderError
	require.True(t, errors.As(err, &pe), "%v", err)
	require.Equal(t, 1, model.calls, "超过上限的 Retry-After 不等待、不重试")
	require.Less(t, time.Since(started), time.Second)
}

func TestModelRetryRejectedByBudgetBeforeSend(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1})
	require.NoError(t, err)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("ok", 1)), steps: []error{unavailable(503, 0)}}
	agent, err := NewSimpleBuilder(model).WithRequestError(3, RetryTransient(3, noWait)).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(budget.WithController(context.Background(), c), "q")
	require.ErrorIs(t, err, budget.ErrBudgetExceeded)
	var pe *llm.ProviderError
	require.True(t, errors.As(err, &pe), "原始失败仍可识别")
	require.Equal(t, 1, model.calls)
}

func TestModelRetryIsNotInheritedByChildAgent(t *testing.T) {
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("ok", 1)), steps: []error{unavailable(503, 0)}}
	child, err := NewSimpleBuilder(model).Build()
	require.NoError(t, err)
	_, err = child.AsRunnable().Invoke(withRequestRecovery(context.Background(), func(context.Context, bool, error) (*RequestErrorDecision, error) {
		return &RequestErrorDecision{Action: RequestRetry}, nil
	}), "q")
	require.Error(t, err)
	require.Equal(t, 1, model.calls)
}

func TestModelRetryDurableReplayUsesSuccessfulAttemptKey(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
	c, err := budget.NewDurable(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 3}, store)
	require.NoError(t, err)
	ctx = budget.WithController(ctx, c)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("好的", 7)), steps: []error{unavailable(503, 0)}}
	agent, err := NewSimpleBuilder(model).WithDurableScope("retry").WithRequestError(2, RetryTransient(2, noWait)).Build()
	require.NoError(t, err)
	first, err := agent.AsRunnable().Invoke(ctx, "q")
	require.NoError(t, err)

	// 修改重试配置后恢复：不因意图变化被拒绝，也不发新请求。
	resumed, err := NewSimpleBuilder(model).WithDurableScope("retry").WithRequestError(5, RetryTransient(5, noWait)).Build()
	require.NoError(t, err)
	model.turns = nil
	second, err := resumed.AsRunnable().Invoke(ctx, "q")
	require.NoError(t, err)
	require.Equal(t, first.Result, second.Result)
	require.Equal(t, 2, model.calls)
	snap, err := c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, snap.ReservedRequests)
	require.Equal(t, 7, snap.KnownTokens.TotalTokens, "回放按成功那次尝试的键登记，不冲突、不重复")
}

type failOnSave struct {
	durable.Store
	fail func(*durable.Snapshot) bool
}

func (s failOnSave) Save(ctx context.Context, id string, v *durable.Snapshot) error {
	if s.fail(v) {
		return errors.New("crash while saving turn")
	}
	return s.Store.Save(ctx, id, v)
}

func TestModelRetryCrashDuringRetryNeedsHostAttention(t *testing.T) {
	inner, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	crash := true
	store := failOnSave{Store: inner, fail: func(v *durable.Snapshot) bool {
		return crash && v.Version == "goagent-operation-v2" && !bytesContains(v.Data, `"pending"`)
	}}
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
	c, err := budget.NewDurable(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 3}, inner)
	require.NoError(t, err)
	ctx = budget.WithController(ctx, c)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("好的", 7)), steps: []error{unavailable(503, 0)}}
	agent, err := NewSimpleBuilder(model).WithDurableScope("retry").WithRequestError(2, RetryTransient(2, noWait)).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "q")
	require.ErrorContains(t, err, "crash")
	crash = false
	_, err = agent.AsRunnable().Invoke(ctx, "q")
	require.ErrorIs(t, err, durable.ErrOperationAttention)
	require.Equal(t, 2, model.calls, "恢复时不再发请求")
	snap, err := c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, snap.ReservedRequests, "已预留的额度不退回")
}

func bytesContains(data []byte, s string) bool { return strings.Contains(string(data), s) }

func TestModelRetryInToolLoop(t *testing.T) {
	model := &flakyModel{nativeEventModel: nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "lookup", `{}`)}}},
		answerTurn("三十天", 1),
	), steps: []error{nil, unavailable(502, 0)}}
	lookup := &countingEventTool{MockTool: NewMockTool("lookup", "30")}
	agent, err := NewSimpleBuilder(model).WithTools(lookup).WithMaxIterations(2).WithRequestError(1, RetryTransient(1, noWait)).Build()
	require.NoError(t, err)
	c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 3})
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(budget.WithController(context.Background(), c), "q")
	require.NoError(t, err)
	require.Equal(t, "三十天", out.Result)
	require.Equal(t, 1, lookup.calls)
	require.Equal(t, 2, out.Metadata["model_calls"], "重试不占回合")
	require.EqualValues(t, 3, out.Metadata["new_model_invocations"])
}

// 持久的原生工具流中，原样重试在同一条回合记录内进行；替换消息的决定被拒绝。
func TestRequestErrorInDurableToolLoop(t *testing.T) {
	run := func(hook RequestErrorHook) (*flakyModel, error) {
		store, err := durable.NewFileStore(t.TempDir())
		require.NoError(t, err)
		ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
		model := &flakyModel{nativeEventModel: nativeModel(answerTurn("好的", 1)), steps: []error{unavailable(503, 0)}}
		reg := tools.NewRegistry()
		require.NoError(t, reg.Register(plainTool()))
		dispatch, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
		require.NoError(t, err)
		agent, err := NewSimpleBuilder(model).WithDurableScope("retry").WithTools(dispatch.Tools()...).WithRequestError(1, hook).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(ctx, "q")
		return model, err
	}
	var emitted []bool
	model, err := run(func(ctx context.Context, f *RequestFailure) (RequestErrorDecision, error) {
		emitted = append(emitted, f.Emitted)
		return RetryTransient(1, noWait)(ctx, f)
	})
	require.NoError(t, err)
	require.Equal(t, 2, model.calls)
	require.Equal(t, []bool{false}, emitted)

	model, err = run(func(_ context.Context, f *RequestFailure) (RequestErrorDecision, error) {
		return RequestErrorDecision{Action: RequestRetry, Messages: f.Messages[len(f.Messages)-1:]}, nil
	})
	require.ErrorContains(t, err, "只能原样重试")
	var pe *llm.ProviderError
	require.True(t, errors.As(err, &pe), "原始失败仍可识别")
	require.Equal(t, 1, model.calls)
}
