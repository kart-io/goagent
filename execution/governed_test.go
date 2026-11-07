package execution_test

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/execution"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func governedScope(t *testing.T, cfg budget.Config, timeout time.Duration) *execution.Scope {
	t.Helper()
	c, err := budget.New(cfg)
	require.NoError(t, err)
	s, err := execution.NewScope(execution.Config{RunID: "run", Budget: c, Timeout: timeout})
	require.NoError(t, err)
	return s
}

func governedRegistry(t *testing.T, effects *int) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tools.NewBaseTool("lookup", "合成查询", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		*effects++
		return &interfaces.ToolOutput{Success: true, Result: "回执"}, nil
	})))
	return reg
}

func governedConfig() *tools.DispatchConfig {
	return &tools.DispatchConfig{MaxAdmissionConcurrency: 2, MaxQueued: 4, Authorize: func(context.Context, string, *interfaces.ToolInput) error { return nil }}
}

func TestGovernedBuilderRejectsMissingControls(t *testing.T) {
	effects := 0
	registry := governedRegistry(t, &effects)
	for _, which := range []string{"无预算", "无工具额度", "无期限", "无授权", "无准入限制", "无排队限制", "无工具"} {
		t.Run(which, func(t *testing.T) {
			budgetCfg := budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 2, MaxToolCalls: 1}
			timeout := time.Second
			if which == "无工具额度" {
				budgetCfg.MaxToolCalls = 0
			}
			if which == "无期限" {
				timeout = 0
			}
			scope := governedScope(t, budgetCfg, timeout)
			if which == "无预算" {
				var err error
				scope, err = execution.NewScope(execution.Config{RunID: "run", Timeout: time.Second})
				require.NoError(t, err)
			}
			cfg := *governedConfig()
			if which == "无授权" {
				cfg.Authorize = nil
			}
			if which == "无准入限制" {
				cfg.MaxAdmissionConcurrency = 0
			}
			if which == "无排队限制" {
				cfg.MaxQueued = 0
			}
			reg := registry
			if which == "无工具" {
				reg = tools.NewRegistry()
			}
			b, _, err := execution.NewGovernedBuilder(mocks.NewScriptedClient(), scope, "agent", reg, cfg)
			require.Error(t, err)
			require.Nil(t, b)
		})
	}
}

func TestGovernedBuilderRequiresBoundScopeAndEnforcesBudget(t *testing.T) {
	scope := governedScope(t, budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 2, MaxToolCalls: 1}, time.Second)
	effects, authorized := 0, 0
	registry := governedRegistry(t, &effects)
	model := mocks.NewScriptedClient(mocks.ToolCallReply("查询", mocks.ToolCall("c1", "lookup", "{}")), mocks.TextReply("完成", "回执"))
	cfg := *governedConfig()
	cfg.Authorize = func(context.Context, string, *interfaces.ToolInput) error { authorized++; return nil }
	b, report, err := execution.NewGovernedBuilder(model, scope, "agent", registry, cfg)
	require.NoError(t, err)
	require.Equal(t, "run", report.RunID)
	require.Equal(t, "agent", report.AgentID)
	require.Equal(t, 2, report.MaxModelRequests)
	require.Equal(t, 1, report.MaxToolCalls)
	require.True(t, report.Dispatch.AuthorizationConfigured)
	require.Equal(t, 4, report.Dispatch.MaxConcurrency)
	require.Equal(t, 30*time.Second, report.Dispatch.Timeout)
	a, err := b.BuildContext(context.Background())
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(context.Background(), "没有绑定")
	require.Error(t, err)
	require.Empty(t, model.Requests())
	ctx, cancel, err := scope.Bind(context.Background())
	require.NoError(t, err)
	defer cancel()
	out, err := a.AsRunnable().Invoke(ctx, "核验")
	require.NoError(t, err)
	require.Equal(t, "回执", out.Result)
	require.Equal(t, 1, effects)
	require.Equal(t, 1, authorized)
	_, err = a.AsRunnable().Invoke(ctx, "额度耗尽")
	require.ErrorIs(t, err, agentErrors.ErrModelBudgetExceeded)
	require.Len(t, model.Requests(), 2)
	require.Equal(t, 1, effects)
}

func TestGovernedBuilderCannotMixBareTools(t *testing.T) {
	effects := 0
	reg := governedRegistry(t, &effects)
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	bare := tools.NewBaseTool("bare", "裸工具", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		t.Fatal("不应执行")
		return nil, nil
	})
	for _, reverse := range []bool{false, true} {
		b := builder.NewSimpleBuilder(mocks.NewScriptedClient())
		if reverse {
			b.WithTools(bare).WithDispatcher(d)
		} else {
			b.WithDispatcher(d).WithTools(bare)
		}
		_, err = b.Build()
		require.Error(t, err)
	}
	_, err = builder.NewSimpleBuilder(mocks.NewScriptedClient()).WithDispatcher(nil).Build()
	require.Error(t, err)
	b := builder.NewSimpleBuilder(mocks.NewScriptedClient()).WithDispatcher(d)
	_, err = b.Build()
	require.NoError(t, err)
	_, err = b.Build()
	require.NoError(t, err, "重复构建不应把派发工具回写到可变Builder")
}

func TestGovernedBuilderDurableReplay(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	model := mocks.NewScriptedClient(mocks.ToolCallReply("查询", mocks.ToolCall("c1", "lookup", "{}")), mocks.TextReply("完成", "回执"))
	effects := 0
	for range 2 {
		controller, e := budget.NewDurable(budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 2, MaxToolCalls: 1}, store)
		require.NoError(t, e)
		scope, e := execution.NewScope(execution.Config{RunID: "run", Journal: store, Budget: controller, Timeout: time.Second})
		require.NoError(t, e)
		b, report, e := execution.NewGovernedBuilder(model, scope, "agent", governedRegistry(t, &effects), *governedConfig())
		require.NoError(t, e)
		require.True(t, report.Journaled)
		require.True(t, report.DurableBudget)
		a, e := b.BuildContext(context.Background())
		require.NoError(t, e)
		ctx, cancel, e := scope.Bind(context.Background())
		require.NoError(t, e)
		out, e := a.AsRunnable().Invoke(ctx, "核验")
		cancel()
		require.NoError(t, e)
		require.Equal(t, "回执", out.Result)
	}
	require.Equal(t, 1, effects)
	require.Len(t, model.Requests(), 2)
}
