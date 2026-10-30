package agents_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/agents"
	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/testing/panictest"
	"github.com/kart-io/goagent/tools"
)

// funcAgent 的 Invoke 由 fn 实现；calls 统计调用次数。
type funcAgent struct {
	*core.BaseAgent
	fn    func(context.Context) (*core.AgentOutput, error)
	calls atomic.Int64
}

func newFuncAgent(name string, fn func(context.Context) (*core.AgentOutput, error)) *funcAgent {
	return &funcAgent{BaseAgent: core.NewBaseAgent(name, name, nil), fn: fn}
}

func (a *funcAgent) Invoke(ctx context.Context, _ *core.AgentInput) (*core.AgentOutput, error) {
	a.calls.Add(1)
	return a.fn(ctx)
}

// InvokeFast 覆盖 BaseAgent 的默认实现，TryInvokeFast 才会调用 fn。
func (a *funcAgent) InvokeFast(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	return a.Invoke(ctx, input)
}

// descRouter 按任务描述路由到同名子 Agent。
type descRouter struct{ agents.AgentRouter }

func (descRouter) Route(_ context.Context, task agents.Task, _ map[string]core.Agent) (string, error) {
	return task.Description, nil
}
func (descRouter) UpdateRouting(string, float64) {}

var errSubTask = errors.New("子任务失败")

func okAgent(name string) *funcAgent {
	return newFuncAgent(name, func(context.Context) (*core.AgentOutput, error) {
		return &core.AgentOutput{Result: name + " 完成"}, nil
	})
}

func errAgent(name string, err error) *funcAgent {
	return newFuncAgent(name, func(context.Context) (*core.AgentOutput, error) { return nil, err })
}

// newTestSupervisor 把任务拆为每个子 Agent 一个任务（描述即子 Agent 名）。
func newTestSupervisor(cfg *agents.SupervisorConfig, subs ...*funcAgent) *agents.SupervisorAgent {
	if cfg == nil {
		cfg = &agents.SupervisorConfig{MaxConcurrentAgents: 2, SubAgentTimeout: 5 * time.Second, AggregationStrategy: agents.StrategyMerge}
	}
	content := ""
	for _, sub := range subs {
		content += sub.Name() + "\n"
	}
	s := agents.NewSupervisorAgent(scriptLLM{content: content}, cfg)
	s.Router = descRouter{}
	for _, sub := range subs {
		s.AddSubAgent(sub.Name(), sub)
	}
	return s
}

// 子 Agent 直接 panic 时进程不退出，panic 以 *PanicError 由 Invoke 返回，本次执行计为失败。
func TestSupervisorSubAgentPanicIsPanicError(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		boom := newFuncAgent("boom", func(context.Context) (*core.AgentOutput, error) { panic("sub-agent panic") })
		s := newTestSupervisor(nil, boom, okAgent("ok"))

		out, err := s.Invoke(context.Background(), &core.AgentInput{Task: "任务"})
		var panicErr *agentErrors.PanicError
		require.ErrorAs(t, err, &panicErr)
		require.Equal(t, "sub-agent panic", panicErr.Value)
		require.NotNil(t, out)
		require.Equal(t, interfaces.StatusFailed, out.Status)
		require.Equal(t, int64(1), s.GetMetrics()["failed_tasks"])
	})
}

// Invoke 如实反映子任务结果：*PanicError 必须返回，全部失败返回错误，部分失败保持汇总并标记 partial。
func TestSupervisorInvokeReportsSubTaskFailures(t *testing.T) {
	panicErr := agentErrors.NewPanicError("子 Agent", "sub-agent panic")
	cases := []struct {
		name       string
		subs       func() []*funcAgent
		failOnAny  bool
		wantStatus string
		wantPanic  bool
		wantErr    error
		metric     string
	}{
		{"all_panic", func() []*funcAgent { return []*funcAgent{errAgent("a", panicErr), errAgent("b", panicErr)} }, false, interfaces.StatusFailed, true, nil, "failed_tasks"},
		{"one_panic", func() []*funcAgent { return []*funcAgent{errAgent("a", panicErr), okAgent("b")} }, false, interfaces.StatusFailed, true, nil, "failed_tasks"},
		{"all_failed", func() []*funcAgent { return []*funcAgent{errAgent("a", errSubTask), errAgent("b", errSubTask)} }, false, interfaces.StatusFailed, false, errSubTask, "failed_tasks"},
		{"partial", func() []*funcAgent { return []*funcAgent{errAgent("a", errSubTask), okAgent("b")} }, false, interfaces.StatusPartial, false, nil, "partial_tasks"},
		{"partial_fail_on_any", func() []*funcAgent { return []*funcAgent{errAgent("a", errSubTask), okAgent("b")} }, true, interfaces.StatusFailed, false, errSubTask, "failed_tasks"},
		{"all_ok", func() []*funcAgent { return []*funcAgent{okAgent("a"), okAgent("b")} }, false, interfaces.StatusSuccess, false, nil, "successful_tasks"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &agents.SupervisorConfig{MaxConcurrentAgents: 2, SubAgentTimeout: 5 * time.Second, AggregationStrategy: agents.StrategyMerge, FailOnAnyError: tc.failOnAny}
			s := newTestSupervisor(cfg, tc.subs()...)

			out, err := s.Invoke(context.Background(), &core.AgentInput{Task: "任务"})
			require.NotNil(t, out)
			require.Equal(t, tc.wantStatus, out.Status)
			switch {
			case tc.wantPanic:
				var pe *agentErrors.PanicError
				require.ErrorAs(t, err, &pe)
			case tc.wantErr != nil:
				require.ErrorIs(t, err, tc.wantErr)
			default:
				require.NoError(t, err)
			}

			metrics := s.GetMetrics()
			require.Equal(t, int64(1), metrics[tc.metric])
			wantRate := 0.0
			if tc.wantStatus == interfaces.StatusSuccess {
				wantRate = 1
			}
			require.Equal(t, wantRate, metrics["success_rate"])
		})
	}
}

// *PanicError 即使匹配 RetryableErrors 也不重试；普通可重试错误照常重试。
func TestSupervisorRetrySkipsPanicError(t *testing.T) {
	policy := &tools.RetryPolicy{MaxRetries: 3, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, Multiplier: 1, RetryableErrors: []string{"panic", "子任务失败"}}
	cases := map[string]struct {
		err       error
		wantCalls int64
	}{
		"panic_error": {agentErrors.NewPanicError("子 Agent", "sub-agent panic"), 1},
		"plain_error": {errSubTask, 4},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sub := errAgent("a", tc.err)
			cfg := &agents.SupervisorConfig{MaxConcurrentAgents: 1, SubAgentTimeout: 5 * time.Second, RetryPolicy: policy, AggregationStrategy: agents.StrategyMerge}
			_, err := newTestSupervisor(cfg, sub).Invoke(context.Background(), &core.AgentInput{Task: "任务"})
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.wantCalls, sub.calls.Load())
		})
	}
}

// 重试等待响应 ctx 取消，不阻塞到退避时长结束。
func TestSupervisorRetryDelayRespectsContext(t *testing.T) {
	policy := &tools.RetryPolicy{MaxRetries: 1, InitialDelay: time.Hour, MaxDelay: time.Hour, Multiplier: 1, RetryableErrors: []string{"子任务失败"}}
	sub := errAgent("a", errSubTask)
	cfg := &agents.SupervisorConfig{MaxConcurrentAgents: 1, SubAgentTimeout: time.Hour, RetryPolicy: policy, AggregationStrategy: agents.StrategyMerge}
	s := newTestSupervisor(cfg, sub)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := s.Invoke(ctx, &core.AgentInput{Task: "任务"})
	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second)
	require.Equal(t, int64(1), sub.calls.Load())
}
