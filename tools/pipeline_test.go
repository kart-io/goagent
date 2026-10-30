package tools

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

const cmdSchema = `{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"],"additionalProperties":false}`

type pipelineFixture struct {
	d     *Dispatcher
	calls atomic.Int32
	ran   sync.Map // cmd -> true
}

func newPipelineFixture(t *testing.T, p *Pipeline, cfg DispatchConfig) *pipelineFixture {
	t.Helper()
	f := &pipelineFixture{}
	run := NewBaseTool("run", "test", cmdSchema, func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		f.calls.Add(1)
		f.ran.Store(in.Args["cmd"], true)
		return &interfaces.ToolOutput{Success: true, Result: "ran " + in.Args["cmd"].(string)}, nil
	})
	safe := NewBaseTool("safe_run", "test", cmdSchema, func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "safely ran " + in.Args["cmd"].(string)}, nil
	})
	cfg.Pipeline = p
	cfg.ReturnArgumentErrors = true
	f.d = argDispatcher(t, cfg, run, safe)
	return f
}

func (f *pipelineFixture) call(ctx context.Context, cmd string) (*interfaces.ToolOutput, error) {
	return modelCall(ctx, f.d, "run", &interfaces.ToolInput{Args: map[string]any{"cmd": cmd}})
}

func requireRejected(t *testing.T, out *interfaces.ToolOutput, err error, contains string) {
	t.Helper()
	require.NoError(t, err)
	msg, ok := RejectedResult(out)
	require.True(t, ok, "expected rejection, got %+v", out)
	require.Contains(t, msg, contains)
}

func decide(kind PreKind, feedback string) PreHook {
	return func(context.Context, *Call) (PreDecision, error) {
		return PreDecision{Kind: kind, Feedback: feedback}, nil
	}
}

type prefixGrant string

func (g prefixGrant) Describe() string { return "run commands starting with " + string(g) }
func (g prefixGrant) Matches(c *Call) bool {
	cmd, _ := c.Args["cmd"].(string)
	return c.Tool == "run" && strings.HasPrefix(cmd, string(g))
}

func askWithGrant(context.Context, *Call) (PreDecision, error) {
	return PreDecision{Kind: PreAsk, Feedback: "needs approval", Grant: prefixGrant("go ")}, nil
}

func TestPipelinePreDecisions(t *testing.T) {
	ctx := context.Background()
	var seenPost []string
	p := &Pipeline{
		Pre: []PreHook{
			decide(PreAllow, ""),
			func(_ context.Context, c *Call) (PreDecision, error) {
				switch c.Args["cmd"] {
				case "rm":
					return PreDecision{Kind: PreDeny, Feedback: "use trash instead"}, nil
				case "halt":
					return PreDecision{Kind: PreStop, Feedback: "budget exhausted"}, nil
				}
				return PreDecision{}, nil
			},
			// 全部钩子都执行，后面钩子的 Deny 与前面的 Deny 合并。
			func(_ context.Context, c *Call) (PreDecision, error) {
				if c.Args["cmd"] == "rm" {
					return PreDecision{Kind: PreDeny, Feedback: "rm is blocked"}, nil
				}
				return PreDecision{}, nil
			},
		},
		Post: []PostHook{func(_ context.Context, c *Call, out *interfaces.ToolOutput) (PostDecision, error) {
			seenPost = append(seenPost, c.Args["cmd"].(string))
			return PostDecision{}, nil
		}},
	}
	f := newPipelineFixture(t, p, DispatchConfig{})

	out, err := f.call(ctx, "ls")
	require.NoError(t, err)
	require.Equal(t, "ran ls", out.Result)

	out, err = f.call(ctx, "rm")
	requireRejected(t, out, err, "use trash instead; rm is blocked")
	_, ran := f.ran.Load("rm")
	require.False(t, ran)
	require.Equal(t, []string{"ls", "rm"}, seenPost, "denied calls still pass through post hooks")

	_, err = f.call(ctx, "halt")
	var stop *StopError
	require.ErrorAs(t, err, &stop)
	require.Equal(t, "budget exhausted", stop.Reason)

	// 参数不合法的调用不进入管线。
	out, err = modelCall(ctx, f.d, "run", &interfaces.ToolInput{Args: map[string]any{}})
	require.NoError(t, err)
	_, isArg := ArgumentErrorResult(out)
	require.True(t, isArg)
	require.Equal(t, []string{"ls", "rm"}, seenPost)
}

func TestPipelineApproval(t *testing.T) {
	ctx := WithSessionID(context.Background(), "s1")
	var asked atomic.Int32
	answer := ApprovalResult{Outcome: ApprovalAllowedSession}
	var mu sync.Mutex
	p := &Pipeline{
		Pre: []PreHook{askWithGrant},
		Approver: ApproverFunc(func(_ context.Context, req ApprovalRequest) (ApprovalResult, error) {
			asked.Add(1)
			require.Equal(t, "run commands starting with go ", req.Grant.Describe())
			require.Equal(t, "needs approval", req.Reason)
			mu.Lock()
			defer mu.Unlock()
			return answer, nil
		}),
	}
	f := newPipelineFixture(t, p, DispatchConfig{})

	out, err := f.call(ctx, "go test")
	require.NoError(t, err)
	require.Equal(t, "ran go test", out.Result)
	require.EqualValues(t, 1, asked.Load())
	require.Equal(t, []string{"run commands starting with go "}, p.Grants("s1"))

	// 会话级批准放行同会话中匹配的调用，不再询问；其他会话或不匹配的调用仍询问。
	_, err = f.call(ctx, "go vet")
	require.NoError(t, err)
	require.EqualValues(t, 1, asked.Load())
	_, err = f.call(WithSessionID(context.Background(), "s2"), "go vet")
	require.NoError(t, err)
	require.EqualValues(t, 2, asked.Load())

	mu.Lock()
	answer = ApprovalResult{Outcome: ApprovalRejected, Feedback: "not now"}
	mu.Unlock()
	out, err = f.call(ctx, "make")
	requireRejected(t, out, err, "the user denied run: not now")

	p.RevokeGrants("s1")
	out, err = f.call(ctx, "go vet")
	requireRejected(t, out, err, "the user denied run")

	mu.Lock()
	answer = ApprovalResult{Outcome: ApprovalCancelled}
	mu.Unlock()
	out, err = f.call(ctx, "go vet")
	requireRejected(t, out, err, "the user denied run")

	noApprover := newPipelineFixture(t, &Pipeline{Pre: []PreHook{askWithGrant}}, DispatchConfig{})
	out, err = noApprover.call(ctx, "go test")
	requireRejected(t, out, err, "no approver is available")
	require.Zero(t, noApprover.calls.Load())

	failing := newPipelineFixture(t, &Pipeline{Pre: []PreHook{askWithGrant}, Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
		return ApprovalResult{}, errors.New("approval service down")
	})}, DispatchConfig{})
	_, err = failing.call(ctx, "go test")
	require.ErrorContains(t, err, "approval service down")
}

// 审批等待不计入工具超时；超时后迟到的批准按拒绝处理。
func TestPipelineApprovalTiming(t *testing.T) {
	ctx := context.Background()
	slow := &Pipeline{Pre: []PreHook{decide(PreAsk, "")}, Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
		time.Sleep(100 * time.Millisecond)
		return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
	})}
	f := newPipelineFixture(t, slow, DispatchConfig{Timeout: 30 * time.Millisecond})
	out, err := f.call(ctx, "ls")
	require.NoError(t, err)
	require.Equal(t, "ran ls", out.Result)

	late := &Pipeline{Pre: []PreHook{decide(PreAsk, "")}, ApprovalTimeout: 20 * time.Millisecond, Approver: ApproverFunc(func(ctx context.Context, _ ApprovalRequest) (ApprovalResult, error) {
		<-ctx.Done()
		return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
	})}
	f = newPipelineFixture(t, late, DispatchConfig{})
	out, err = f.call(ctx, "ls")
	requireRejected(t, out, err, "not approved in time")
	require.Zero(t, f.calls.Load())
}

// 同一会话中相同的并发审批请求只询问一次。
func TestPipelineCoalescesApprovals(t *testing.T) {
	var asked atomic.Int32
	release := make(chan struct{})
	p := &Pipeline{Pre: []PreHook{decide(PreAsk, "")}, Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
		asked.Add(1)
		<-release
		return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
	})}
	f := newPipelineFixture(t, p, DispatchConfig{MaxConcurrency: 4})
	ctx := WithSessionID(context.Background(), "s")
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.call(ctx, "same")
			require.NoError(t, err)
		}()
	}
	require.Eventually(t, func() bool { return asked.Load() == 1 }, time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	require.EqualValues(t, 1, asked.Load())
	require.EqualValues(t, 3, f.calls.Load())
}

// Guard 在审批之后执行，人的批准也无法越过它。
func TestPipelineGuardOverridesApproval(t *testing.T) {
	p := &Pipeline{
		Pre: []PreHook{decide(PreAsk, "")},
		Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
			return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
		}),
		Guards: []Guard{func(_ context.Context, c *Call) string {
			if strings.Contains(c.Args["cmd"].(string), "/etc") {
				return "outside the workspace"
			}
			return ""
		}},
	}
	f := newPipelineFixture(t, p, DispatchConfig{})
	out, err := f.call(context.Background(), "cat /etc/passwd")
	requireRejected(t, out, err, "outside the workspace")
	require.Zero(t, f.calls.Load())
}

func TestPipelineAroundAndPost(t *testing.T) {
	var order []string
	wrap := func(name string) Around {
		return func(ctx context.Context, c *Call, next func(context.Context) (*interfaces.ToolOutput, error)) (*interfaces.ToolOutput, error) {
			order = append(order, name+">")
			out, err := next(ctx)
			order = append(order, "<"+name)
			return out, err
		}
	}
	p := &Pipeline{
		Around: []Around{wrap("outer"), wrap("inner")},
		Post: []PostHook{
			func(_ context.Context, _ *Call, out *interfaces.ToolOutput) (PostDecision, error) {
				return PostDecision{AdditionalContext: []string{"lint passed"}}, nil
			},
			func(_ context.Context, _ *Call, out *interfaces.ToolOutput) (PostDecision, error) {
				replaced := *out
				replaced.Result = "[truncated] " + out.Result.(string)
				return PostDecision{Replace: &replaced, AdditionalContext: []string{"output was truncated"}}, nil
			},
		},
	}
	f := newPipelineFixture(t, p, DispatchConfig{})
	out, err := f.call(context.Background(), "ls")
	require.NoError(t, err)
	require.Equal(t, []string{"outer>", "inner>", "<inner", "<outer"}, order)
	require.Equal(t, "[truncated] ran ls", out.Result)
	require.Equal(t, []string{"lint passed", "output was truncated"}, out.AdditionalContext)

	// Around 可以不调用 next 直接给出结果。
	short := newPipelineFixture(t, &Pipeline{Around: []Around{func(context.Context, *Call, func(context.Context) (*interfaces.ToolOutput, error)) (*interfaces.ToolOutput, error) {
		return Rejected("dry run", nil), nil
	}}}, DispatchConfig{})
	out, err = short.call(context.Background(), "ls")
	requireRejected(t, out, err, "dry run")
	require.Zero(t, short.calls.Load())
}

func TestPipelineSubstitute(t *testing.T) {
	substitute := func(tool string, args map[string]any) PreHook {
		return func(_ context.Context, c *Call) (PreDecision, error) {
			if c.Tool != "run" {
				return PreDecision{}, nil
			}
			return PreDecision{Kind: PreSubstitute, Substitute: &Substitution{Tool: tool, Args: args}}, nil
		}
	}
	f := newPipelineFixture(t, &Pipeline{Pre: []PreHook{substitute("safe_run", map[string]any{"cmd": "ls"})}}, DispatchConfig{})
	out, err := f.call(context.Background(), "rm -rf")
	require.NoError(t, err)
	require.Equal(t, "safely ran ls", out.Result)
	require.Equal(t, "run", out.Metadata["substituted_for"])
	require.Equal(t, "safe_run", out.Metadata["substitute"])
	require.Zero(t, f.calls.Load())

	for _, hook := range []PreHook{
		substitute("missing", map[string]any{"cmd": "x"}),
		substitute("safe_run", map[string]any{"bad": 1}),
		substitute("safe_run", nil),
	} {
		f = newPipelineFixture(t, &Pipeline{Pre: []PreHook{hook}}, DispatchConfig{})
		_, err = f.call(context.Background(), "x")
		require.Error(t, err)
	}

	// 替代调用不能再被替代。
	loop := newPipelineFixture(t, &Pipeline{Pre: []PreHook{func(_ context.Context, c *Call) (PreDecision, error) {
		return PreDecision{Kind: PreSubstitute, Substitute: &Substitution{Tool: "safe_run", Args: map[string]any{"cmd": "x"}}}, nil
	}}}, DispatchConfig{})
	_, err = loop.call(context.Background(), "x")
	require.ErrorContains(t, err, "invalid substitution")
}

func TestPipelineHookPanics(t *testing.T) {
	boom := func() { panic("boom") }
	cases := map[string]*Pipeline{
		"调用前钩子": {Pre: []PreHook{func(context.Context, *Call) (PreDecision, error) { boom(); return PreDecision{}, nil }}},
		"守卫":    {Guards: []Guard{func(context.Context, *Call) string { boom(); return "" }}},
		"执行包装": {Around: []Around{func(context.Context, *Call, func(context.Context) (*interfaces.ToolOutput, error)) (*interfaces.ToolOutput, error) {
			boom()
			return nil, nil
		}}},
		"调用后钩子": {Post: []PostHook{func(context.Context, *Call, *interfaces.ToolOutput) (PostDecision, error) {
			boom()
			return PostDecision{}, nil
		}}},
		"审批回调": {Pre: []PreHook{decide(PreAsk, "")}, Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) { boom(); return ApprovalResult{}, nil })},
	}
	for name, p := range cases {
		f := newPipelineFixture(t, p, DispatchConfig{})
		_, err := f.call(context.Background(), "ls")
		require.ErrorContains(t, err, name, name)
	}
}

// 已批准并完成的调用回放时不再询问，被拒绝的调用回放为同样的拒绝。
func TestPipelineDecisionsAreJournaled(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
	var asked atomic.Int32
	p := &Pipeline{
		Pre: []PreHook{func(_ context.Context, c *Call) (PreDecision, error) {
			if c.Args["cmd"] == "rm" {
				return PreDecision{Kind: PreDeny, Feedback: "no"}, nil
			}
			return PreDecision{Kind: PreAsk}, nil
		}},
		Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
			asked.Add(1)
			return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
		}),
	}
	f := newPipelineFixture(t, p, DispatchConfig{})
	call := func(id, cmd string) (*interfaces.ToolOutput, error) {
		return modelCall(ctx, f.d, "run", &interfaces.ToolInput{CallID: id, Args: map[string]any{"cmd": cmd}})
	}
	for range 2 {
		out, err := call("a", "ls")
		require.NoError(t, err)
		require.Equal(t, "ran ls", out.Result)
		out, err = call("b", "rm")
		requireRejected(t, out, err, "no")
	}
	require.EqualValues(t, 1, asked.Load())
	require.EqualValues(t, 1, f.calls.Load())
}

// 准入阶段在日志之外：终止与审批中取消不留记录、不消耗尝试次数。
func TestPipelineAdmissionOutsideJournal(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
	var stop, block atomic.Bool
	stop.Store(true)
	approverCalls := make(chan struct{}, 4)
	p := &Pipeline{
		Pre: []PreHook{func(context.Context, *Call) (PreDecision, error) {
			if stop.Load() {
				return PreDecision{Kind: PreStop, Feedback: "halt"}, nil
			}
			return PreDecision{Kind: PreAsk}, nil
		}},
		Approver: ApproverFunc(func(ctx context.Context, _ ApprovalRequest) (ApprovalResult, error) {
			approverCalls <- struct{}{}
			if block.Load() {
				<-ctx.Done()
				return ApprovalResult{}, ctx.Err()
			}
			return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
		}),
	}
	f := newPipelineFixture(t, p, DispatchConfig{MaxAttempts: 1})
	call := func(ctx context.Context) (*interfaces.ToolOutput, error) {
		return modelCall(ctx, f.d, "run", &interfaces.ToolInput{CallID: "a", Args: map[string]any{"cmd": "ls"}})
	}
	for range 2 {
		_, err = call(ctx)
		var stopErr *StopError
		require.ErrorAs(t, err, &stopErr)
	}
	stop.Store(false)
	block.Store(true)
	cancelled, cancel := context.WithCancel(ctx)
	go func() { <-approverCalls; cancel() }()
	_, err = call(cancelled)
	require.ErrorIs(t, err, context.Canceled)
	block.Store(false)

	out, err := call(ctx)
	require.NoError(t, err)
	require.Equal(t, "ran ls", out.Result)
	_, err = durable.InspectOperation(ctx, "tool/a")
	require.NoError(t, err)
}

// 发起询问的调用被取消时，合并等待的调用重新询问，而不是跟着失败。
func TestPipelineCoalescedApprovalSurvivesLeaderCancel(t *testing.T) {
	var asked atomic.Int32
	p := &Pipeline{Pre: []PreHook{decide(PreAsk, "")}, Approver: ApproverFunc(func(ctx context.Context, _ ApprovalRequest) (ApprovalResult, error) {
		if asked.Add(1) == 1 {
			<-ctx.Done()
			return ApprovalResult{}, ctx.Err()
		}
		return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
	})}
	f := newPipelineFixture(t, p, DispatchConfig{MaxConcurrency: 2})
	base := WithSessionID(context.Background(), "s")
	leaderCtx, cancel := context.WithCancel(base)
	leaderErr := make(chan error, 1)
	go func() { _, err := f.call(leaderCtx, "same"); leaderErr <- err }()
	require.Eventually(t, func() bool { return asked.Load() == 1 }, time.Second, 5*time.Millisecond)
	followerOut := make(chan *interfaces.ToolOutput, 1)
	go func() {
		out, err := f.call(base, "same")
		require.NoError(t, err)
		followerOut <- out
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	require.ErrorIs(t, <-leaderErr, context.Canceled)
	require.Equal(t, "ran same", (<-followerOut).Result)
	require.EqualValues(t, 2, asked.Load())
}

// 没有会话 ID 时不合并审批，会话级批准降级为单次批准。
func TestPipelineWithoutSessionID(t *testing.T) {
	var asked atomic.Int32
	p := &Pipeline{Pre: []PreHook{askWithGrant}, Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
		asked.Add(1)
		return ApprovalResult{Outcome: ApprovalAllowedSession}, nil
	})}
	f := newPipelineFixture(t, p, DispatchConfig{})
	for range 2 {
		_, err := f.call(context.Background(), "go test")
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, asked.Load())
	require.Empty(t, p.Grants(""))
}

// 替代调用按目标工具执行授权与自定义校验。
func TestPipelineSubstituteIsAuthorized(t *testing.T) {
	var authorized []string
	p := &Pipeline{Pre: []PreHook{func(_ context.Context, c *Call) (PreDecision, error) {
		if c.Tool != "run" {
			return PreDecision{}, nil
		}
		return PreDecision{Kind: PreSubstitute, Substitute: &Substitution{Tool: "safe_run", Args: map[string]any{"cmd": "ls"}}}, nil
	}}}
	f := newPipelineFixture(t, p, DispatchConfig{Authorize: func(_ context.Context, name string, _ *interfaces.ToolInput) error {
		authorized = append(authorized, name)
		if name == "safe_run" {
			return errors.New("safe_run is not allowed")
		}
		return nil
	}})
	_, err := f.call(context.Background(), "x")
	require.ErrorContains(t, err, "safe_run is not allowed")
	require.Equal(t, []string{"run", "safe_run"}, authorized)
}

// Around 与 PostHook 在单次超时之内；审批人不响应 ctx 时审批也按时结束。
func TestPipelineTimeoutsCoverExecution(t *testing.T) {
	slowAround := &Pipeline{Around: []Around{func(ctx context.Context, _ *Call, next func(context.Context) (*interfaces.ToolOutput, error)) (*interfaces.ToolOutput, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
			return next(ctx)
		}
	}}}
	f := newPipelineFixture(t, slowAround, DispatchConfig{Timeout: 30 * time.Millisecond})
	_, err := f.call(context.Background(), "ls")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	stubborn := &Pipeline{Pre: []PreHook{decide(PreAsk, "")}, ApprovalTimeout: 20 * time.Millisecond, Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
		time.Sleep(500 * time.Millisecond)
		return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
	})}
	f = newPipelineFixture(t, stubborn, DispatchConfig{})
	start := time.Now()
	out, err := f.call(context.Background(), "ls")
	requireRejected(t, out, err, "not approved in time")
	require.Less(t, time.Since(start), 300*time.Millisecond)
}

// Around 与 PostHook 给出的失败结果按工具失败处理。
func TestPipelineRejectsUnsuccessfulHookOutput(t *testing.T) {
	p := &Pipeline{Post: []PostHook{func(context.Context, *Call, *interfaces.ToolOutput) (PostDecision, error) {
		return PostDecision{Replace: &interfaces.ToolOutput{Success: false, Error: "broken"}}, nil
	}}}
	f := newPipelineFixture(t, p, DispatchConfig{})
	_, err := f.call(context.Background(), "ls")
	require.ErrorContains(t, err, "broken")
}

// 裁决按 Stop > Deny > Substitute > Ask > Allow 聚合，与钩子顺序无关；获批的 Ask 不能越过其他钩子的 Deny。
func TestPipelineAggregatesDecisions(t *testing.T) {
	approveAll := ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
		return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
	})
	sub := func(_ context.Context, c *Call) (PreDecision, error) {
		if c.Tool != "run" {
			return PreDecision{}, nil
		}
		return PreDecision{Kind: PreSubstitute, Substitute: &Substitution{Tool: "safe_run", Args: map[string]any{"cmd": "ls"}}}, nil
	}
	cases := []struct {
		name  string
		hooks []PreHook
		check func(t *testing.T, out *interfaces.ToolOutput, err error)
	}{
		{"ask then deny", []PreHook{decide(PreAsk, ""), decide(PreDeny, "no")}, func(t *testing.T, out *interfaces.ToolOutput, err error) {
			requireRejected(t, out, err, "no")
		}},
		{"deny then stop", []PreHook{decide(PreDeny, "no"), decide(PreStop, "halt")}, func(t *testing.T, _ *interfaces.ToolOutput, err error) {
			var stop *StopError
			require.ErrorAs(t, err, &stop)
		}},
		{"ask then substitute", []PreHook{decide(PreAsk, ""), sub}, func(t *testing.T, out *interfaces.ToolOutput, err error) {
			require.NoError(t, err)
			require.Equal(t, "safely ran ls", out.Result)
		}},
		{"two substitutes", []PreHook{sub, sub}, func(t *testing.T, _ *interfaces.ToolOutput, err error) {
			require.ErrorContains(t, err, "invalid substitution")
		}},
	}
	for _, c := range cases {
		for _, hooks := range [][]PreHook{c.hooks, {c.hooks[1], c.hooks[0]}} {
			f := newPipelineFixture(t, &Pipeline{Pre: hooks, Approver: approveAll}, DispatchConfig{})
			out, err := f.call(context.Background(), "x")
			c.check(t, out, err)
			require.Zero(t, f.calls.Load(), c.name)
		}
	}
}

type toolOnlyGrant struct{}

func (toolOnlyGrant) Describe() string     { return "all run calls" }
func (toolOnlyGrant) Matches(c *Call) bool { return c.Tool == "run" }

// 多个 Ask 合并为一次审批；会话授权按钩子分别记录，一个钩子的授权不满足另一个钩子的 Ask。
func TestPipelineCombinesAsks(t *testing.T) {
	var requests []ApprovalRequest
	p := &Pipeline{
		Pre: []PreHook{
			askWithGrant,
			func(_ context.Context, c *Call) (PreDecision, error) {
				if c.Args["cmd"] == "go build" {
					return PreDecision{Kind: PreAsk, Feedback: "builds are slow", Grant: toolOnlyGrant{}}, nil
				}
				return PreDecision{}, nil
			},
		},
		Approver: ApproverFunc(func(_ context.Context, req ApprovalRequest) (ApprovalResult, error) {
			requests = append(requests, req)
			return ApprovalResult{Outcome: ApprovalAllowedSession}, nil
		}),
	}
	f := newPipelineFixture(t, p, DispatchConfig{})
	ctx := WithSessionID(context.Background(), "s")
	call := func(cmd string) {
		out, err := f.call(ctx, cmd)
		require.NoError(t, err)
		require.Equal(t, "ran "+cmd, out.Result)
	}

	call("go test")
	require.Len(t, requests, 1)
	require.Equal(t, prefixGrant("go "), requests[0].Grant)

	// 第一个钩子的授权已覆盖 go build，但第二个钩子的 Ask 仍要询问，两条理由合并为一次。
	call("go build")
	require.Len(t, requests, 2)
	require.Equal(t, "needs approval; builds are slow", requests[1].Reason)
	require.Equal(t, "run commands starting with go ；all run calls", requests[1].Grant.Describe())

	call("go build")
	call("go vet")
	require.Len(t, requests, 2, "both hooks' grants now cover these calls")
	require.Equal(t, []string{"run commands starting with go ", "run commands starting with go ", "all run calls"}, p.Grants("s"))
}

// 任一钩子的 Ask 没有给出 Grant 时不提供会话级批准。
func TestPipelineCombinedAskWithoutGrant(t *testing.T) {
	var asked atomic.Int32
	p := &Pipeline{
		Pre: []PreHook{askWithGrant, decide(PreAsk, "")},
		Approver: ApproverFunc(func(_ context.Context, req ApprovalRequest) (ApprovalResult, error) {
			asked.Add(1)
			require.Nil(t, req.Grant)
			return ApprovalResult{Outcome: ApprovalAllowedSession}, nil
		}),
	}
	f := newPipelineFixture(t, p, DispatchConfig{})
	ctx := WithSessionID(context.Background(), "s")
	for range 2 {
		_, err := f.call(ctx, "go test")
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, asked.Load())
	require.Empty(t, p.Grants("s"))
}
