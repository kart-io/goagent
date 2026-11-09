package adapters

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/execution"
	"github.com/kart-io/goagent/interfaces"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

func adapterResources(t *testing.T) (context.Context, mg.Resources) {
	t.Helper()
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ev, err := session.NewEvent("management/session", "session")
	require.NoError(t, err)
	_, err = store.Append(ctx, "log", 0, ev)
	require.NoError(t, err)
	r := mg.Resources{Task: mg.Task{Meta: mg.Meta{ID: "task"}, RunID: "task", ExecutionKey: "task", Input: json.RawMessage(`"请求"`), Budget: budget.Config{RootKey: "task", Version: "v1", MaxModelRequests: 10}}, Session: mg.Session{LogID: "log"}, Sessions: store, Journal: session.DurableStore(store, "log")}
	r.Budget, err = budget.NewDurable(r.Task.Budget, r.Journal)
	require.NoError(t, err)
	r.Scope, err = execution.NewScope(execution.Config{RunID: "task", SessionID: "log", Journal: r.Journal, Budget: r.Budget})
	require.NoError(t, err)
	bound, cancel, err := r.Scope.Bind(ctx)
	require.NoError(t, err)
	t.Cleanup(cancel)
	return bound, r
}

func TestNativeFactoryPlainChatRebuildsWithoutRequests(t *testing.T) {
	ctx, r := adapterResources(t)
	model := mocks.NewScriptedClient(mocks.TextReply("回答", "已完成"))
	f := NativeFactory{Check: func(context.Context, mg.Version) error { return nil }, BuildAgent: func(context.Context, mg.Version, mg.Resources, tools.Approver) (*builder.SimpleAgentBuilder, func(context.Context) error, error) {
		return builder.NewSimpleBuilder(model), nil, nil
	}}
	v := mg.Version{AgentID: "agent", Version: "v1", ConfigHash: "config"}
	caps, err := f.Validate(ctx, v)
	require.NoError(t, err)
	require.True(t, caps.Fork)
	binding, err := f.Build(ctx, v, r)
	require.NoError(t, err)
	spec := mg.RunSpec{Task: r.Task}
	evidence, err := binding.Runtime.Inspect(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, "absent", evidence.State)
	result, err := binding.Runtime.Run(ctx, spec)
	require.NoError(t, err)
	require.JSONEq(t, `"已完成"`, string(result.Output))
	before, err := r.Sessions.Read(ctx, r.Session.LogID, 0)
	require.NoError(t, err)
	binding, err = f.Build(ctx, v, r)
	require.NoError(t, err)
	evidence, err = binding.Runtime.Inspect(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, "completed", evidence.State)
	require.Equal(t, result.Output, evidence.Result.Output)
	after, err := r.Sessions.Read(ctx, r.Session.LogID, 0)
	require.NoError(t, err)
	require.Equal(t, before, after, "只读检查不能向原会话追加证据")
	require.Len(t, model.Requests(), 1)
	usage, err := r.Budget.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, usage.ReservedRequests)
	require.Equal(t, 2, usage.KnownTokens.TotalTokens)
}

func TestNativeFactoryApprovalBoundToStoredDecision(t *testing.T) {
	for _, decision := range []string{"approve", "reject"} {
		t.Run(decision, func(t *testing.T) {
			ctx, r := adapterResources(t)
			var approved mg.Approval
			r.ApprovalReader = func(context.Context, string) (mg.Approval, error) { return approved, nil }
			model := mocks.NewScriptedClient(mocks.ToolCallReply("申请操作", mocks.ToolCall("effect-1", "effect", `{}`)), mocks.TextReply("最终回复", "操作已处理"))
			effects := 0
			f := NativeFactory{Check: func(context.Context, mg.Version) error { return nil }, BuildAgent: func(_ context.Context, _ mg.Version, _ mg.Resources, ap tools.Approver) (*builder.SimpleAgentBuilder, func(context.Context) error, error) {
				reg := tools.NewRegistry()
				err := reg.Register(tools.NewBaseTool("effect", "受控操作", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
					effects++
					return &interfaces.ToolOutput{Success: true, Result: "回执"}, nil
				}))
				if err != nil {
					return nil, nil, err
				}
				d, err := tools.NewDispatcher(reg, tools.DispatchConfig{Pipeline: &tools.Pipeline{Approver: ap, Pre: []tools.PreHook{func(context.Context, *tools.Call) (tools.PreDecision, error) {
					return tools.PreDecision{Kind: tools.PreAsk}, nil
				}}}})
				if err != nil {
					return nil, nil, err
				}
				return builder.NewSimpleBuilder(model).WithTools(d.Tools()...).WithMaxIterations(4), nil, nil
			}}
			v := mg.Version{AgentID: "agent", Version: "v1", ConfigHash: "hash"}
			binding, err := f.Build(ctx, v, r)
			require.NoError(t, err)
			spec := mg.RunSpec{Task: r.Task}
			result, err := binding.Runtime.Run(ctx, spec)
			require.Error(t, err)
			require.Equal(t, "waiting", result.Outcome)
			require.Len(t, result.Waits, 1)
			require.Zero(t, effects)
			wait := result.Waits[0]
			approved = mg.Approval{Meta: mg.Meta{ID: "approval"}, TaskID: r.Task.ID, Wait: wait, Decision: decision, State: "approved"}
			if decision == "reject" {
				approved.State = "rejected"
			}
			raw, err := json.Marshal(mg.ApprovalAnswer{ApprovalID: approved.ID, Decision: decision, IntentHash: wait.IntentHash})
			require.NoError(t, err)
			spec.ResumeInputs = map[string]string{wait.Key: string(raw)}
			binding, err = f.Build(ctx, v, r)
			require.NoError(t, err)
			result, err = binding.Runtime.Run(ctx, spec)
			require.NoError(t, err)
			require.Equal(t, "completed", result.Outcome)
			if decision == "approve" {
				require.Equal(t, 1, effects)
			} else {
				require.Zero(t, effects)
			}
			require.Len(t, model.Requests(), 2)
			evidence, err := binding.Runtime.Inspect(ctx, mg.RunSpec{Task: r.Task})
			require.NoError(t, err)
			require.Equal(t, "completed", evidence.State)
			require.Len(t, model.Requests(), 2)
		})
	}
}

func TestFactoryValidationAndCleanupOnBuildFailure(t *testing.T) {
	ctx := context.Background()
	_, err := (NativeFactory{}).Validate(ctx, mg.Version{})
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	_, err = (GraphFactory[int]{}).Validate(ctx, mg.Version{})
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	want := errors.New("宿主构建失败")
	cleaned := false
	f := NativeFactory{BuildAgent: func(context.Context, mg.Version, mg.Resources, tools.Approver) (*builder.SimpleAgentBuilder, func(context.Context) error, error) {
		return nil, func(context.Context) error { cleaned = true; return nil }, want
	}}
	binding, err := f.Build(ctx, mg.Version{}, mg.Resources{})
	require.ErrorIs(t, err, want)
	require.NotNil(t, binding.Cleanup)
	require.NoError(t, binding.Cleanup(ctx))
	require.True(t, cleaned)
	f.BuildAgent = func(context.Context, mg.Version, mg.Resources, tools.Approver) (*builder.SimpleAgentBuilder, func(context.Context) error, error) {
		return nil, nil, nil
	}
	_, err = f.Build(ctx, mg.Version{}, mg.Resources{})
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
}

func TestGraphFactoryApprovalAndCompletedInspection(t *testing.T) {
	ctx, r := adapterResources(t)
	r.Task.Input = json.RawMessage(`2`)
	var approval mg.Approval
	r.ApprovalReader = func(context.Context, string) (mg.Approval, error) { return approval, nil }
	effects := 0
	f := GraphFactory[int]{Check: func(context.Context, mg.Version) error { return nil }, Initial: func(raw json.RawMessage) (v int, err error) { err = json.Unmarshal(raw, &v); return }, BuildGraph: func(ctx context.Context, _ mg.Version, r mg.Resources) (*workflow.DurableGraph[int], func(context.Context) error, error) {
		g := workflow.NewGraph[int]()
		require.NoError(t, g.AddNode("操作", func(_ context.Context, v int) (int, error) { effects++; return v + 1, nil }))
		require.NoError(t, g.AddEdge(constants.WorkflowStart, "操作"))
		require.NoError(t, g.AddEdge("操作", constants.WorkflowEnd))
		compiled, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: r.Journal, Version: "v1", ApprovalNodes: []string{"操作"}}, workflow.WithResumeHandler(func(_ context.Context, _ string, snap *workflow.GraphSnapshot[int], _ json.RawMessage) (int, error) {
			return snap.State, nil
		}))
		return compiled, nil, err
	}}
	caps, err := f.Validate(ctx, mg.Version{})
	require.NoError(t, err)
	require.True(t, caps.Approval)
	require.False(t, caps.Fork)
	binding, err := f.Build(ctx, mg.Version{}, r)
	require.NoError(t, err)
	spec := mg.RunSpec{Task: r.Task}
	evidence, err := binding.Runtime.Inspect(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, "absent", evidence.State)
	result, err := binding.Runtime.Run(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, "waiting", result.Outcome)
	require.Zero(t, effects)
	evidence, err = binding.Runtime.Inspect(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, "waiting", evidence.State)
	approval = mg.Approval{TaskID: r.Task.ID, Decision: "approve", State: "approved", Wait: result.Waits[0]}
	raw, err := json.Marshal(mg.ApprovalAnswer{ApprovalID: "approval", Decision: "approve"})
	require.NoError(t, err)
	spec.ResumeInputs = map[string]string{result.Waits[0].Key: string(raw)}
	result, err = binding.Runtime.Run(ctx, spec)
	require.NoError(t, err)
	require.JSONEq(t, `3`, string(result.Output))
	evidence, err = binding.Runtime.Inspect(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, "completed", evidence.State)
	require.Equal(t, result.Output, evidence.Result.Output)
	require.Equal(t, 1, effects)
}
