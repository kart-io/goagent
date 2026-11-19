package tools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
)

// 审批不持久化：恢复时未完成的调用重新询问，已完成的调用回放不再询问；同一 CallID 换参数不能借旧记录执行。
func TestApprovalIsNotReusedAcrossResume(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(WithSessionID(context.Background(), "s1"), "run"), store)
	var asked atomic.Int32
	var ran []string
	fail := true
	tool := NewBaseTool("run", "test", cmdSchema, func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		ran = append(ran, in.Args["cmd"].(string))
		if fail {
			fail = false
			return nil, errors.New("transient")
		}
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	})
	p := &Pipeline{
		Pre: []PreHook{decide(PreAsk, "")},
		Approver: ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
			asked.Add(1)
			return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
		}),
	}
	d := argDispatcher(t, DispatchConfig{Pipeline: p}, tool)
	invoke := func(cmd string) error {
		_, err := d.Invoke(ctx, ToolRequest{Name: "run", Input: &interfaces.ToolInput{CallID: "c1", Args: map[string]any{"cmd": cmd}}})
		return err
	}

	require.Error(t, invoke("ls"))
	require.NoError(t, invoke("ls"))
	require.EqualValues(t, 2, asked.Load())

	require.NoError(t, invoke("ls"))
	require.EqualValues(t, 2, asked.Load())

	require.Error(t, invoke("rm -rf /"))
	require.Equal(t, []string{"ls", "ls"}, ran)
}

// 工具执行的参数必须是审批人批准的参数：钩子改写 Call.Args 时报错且不执行，改参数只能经 PreSubstitute。
func TestApprovedArgumentsAreExecuted(t *testing.T) {
	const approved, tampered = "ls", "rm -rf /"
	allow := ApproverFunc(func(context.Context, ApprovalRequest) (ApprovalResult, error) {
		return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
	})
	for _, tc := range []struct {
		name string
		p    *Pipeline
	}{
		{"approver", &Pipeline{Pre: []PreHook{decide(PreAsk, "")}, Approver: ApproverFunc(func(_ context.Context, req ApprovalRequest) (ApprovalResult, error) {
			req.Call.Args["cmd"] = tampered
			return ApprovalResult{Outcome: ApprovalAllowedOnce}, nil
		})}},
		{"guard", &Pipeline{Pre: []PreHook{decide(PreAsk, "")}, Approver: allow, Guards: []Guard{func(_ context.Context, c *Call) string {
			c.Args["cmd"] = tampered
			return ""
		}}}},
		{"around", &Pipeline{Pre: []PreHook{decide(PreAsk, "")}, Approver: allow, Around: []Around{func(ctx context.Context, c *Call, next func(context.Context) (*interfaces.ToolOutput, error)) (*interfaces.ToolOutput, error) {
			c.Args["cmd"] = tampered
			return next(ctx)
		}}}},
		{"prehook", &Pipeline{Pre: []PreHook{func(_ context.Context, c *Call) (PreDecision, error) {
			c.Args["cmd"] = tampered
			return PreDecision{}, nil
		}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPipelineFixture(t, tc.p, DispatchConfig{})
			_, err := f.call(context.Background(), approved)
			require.ErrorContains(t, err, "use PreSubstitute")
			require.Zero(t, f.calls.Load())
		})
	}

	// 不改写参数的钩子与替代调用照常执行。
	f := newPipelineFixture(t, &Pipeline{Pre: []PreHook{func(_ context.Context, c *Call) (PreDecision, error) {
		if c.Tool != "safe_run" {
			return PreDecision{}, nil
		}
		return PreDecision{Kind: PreSubstitute, Substitute: &Substitution{Tool: "run", Args: map[string]any{"cmd": approved}}}, nil
	}}}, DispatchConfig{})
	_, err := modelCall(context.Background(), f.d, "safe_run", &interfaces.ToolInput{Args: map[string]any{"cmd": tampered}})
	require.NoError(t, err)
	_, ran := f.ran.Load(approved)
	require.True(t, ran)
}
