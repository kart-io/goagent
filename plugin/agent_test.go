package plugin

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

type countingMiddleware struct {
	*middleware.BaseMiddleware
	calls *atomic.Int32
}

func (m *countingMiddleware) OnBefore(ctx context.Context, r *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
	m.calls.Add(1)
	return m.BaseMiddleware.OnBefore(ctx, r)
}

type startCallback struct {
	*core.BaseCallback
	calls *atomic.Int32
}

func (c *startCallback) OnStart(context.Context, interface{}) error {
	c.calls.Add(1)
	return nil
}

// 每类贡献都真正作用于 Agent。
func TestPluginContributionsDriveAgent(t *testing.T) {
	var toolRuns, preSteps, stops, mw, cb atomic.Int32
	p := contributing("orders", Contribution{
		Tools:          []interfaces.Tool{toolNamed("lookup_order", &toolRuns)},
		PromptSections: []builder.PromptSection{section("orders", "退款须提供订单号")},
		PromptContexts: []builder.PromptContext{{Name: "clock", Text: func(builder.PromptScope) string { return "现在是工作时间" }}},
		PreSteps: []PreStep{{Identity: "count", Hook: func(context.Context, *builder.Step) (builder.PreStepResult, error) {
			preSteps.Add(1)
			return builder.PreStepResult{}, nil
		}}},
		RequestHooks: []builder.RequestHook{func(_ context.Context, r *builder.ModelRequest) error {
			r.Request.Temperature = 0.3
			return nil
		}},
		TurnStopping: []builder.TurnStoppingHook{func(_ context.Context, s *builder.Stopping) (string, error) {
			if stops.Add(1) == 1 {
				return "请给出订单号", nil
			}
			return "", nil
		}},
		Middleware: []middleware.Middleware{&countingMiddleware{BaseMiddleware: middleware.NewBaseMiddleware("count"), calls: &mw}},
		Callbacks:  []core.Callback{&startCallback{BaseCallback: core.NewBaseCallback(), calls: &cb}},
	})
	h := startedHost(t, p)
	model := newModel(calls("lookup_order"), answer("已退款"), answer("订单 A1 已退款"))
	b := builder.NewSimpleBuilder(model).WithMaxIterations(6)
	require.NoError(t, Install(context.Background(), b, h))
	agent, err := b.Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "退款")
	require.NoError(t, err)
	require.Equal(t, "订单 A1 已退款", out.Result)

	first := model.request(0)
	require.Contains(t, first.Messages[0].Content, "退款须提供订单号")
	require.Contains(t, first.Messages[len(first.Messages)-1].Content, "现在是工作时间")
	require.Equal(t, 0.3, first.Temperature)
	require.EqualValues(t, 1, toolRuns.Load())
	require.GreaterOrEqual(t, preSteps.Load(), int32(1))
	require.EqualValues(t, 2, stops.Load())
	require.GreaterOrEqual(t, mw.Load(), int32(1))
	require.GreaterOrEqual(t, cb.Load(), int32(1))
}

// 持久运行中插件 PreStep 的身份包含名称与版本：升级版本后恢复旧运行以 intent or attempt policy changed 失败。
func TestPluginVersionEntersDurableIdentity(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "plugin-run"), store)
	var runs atomic.Int32
	run := func(version string, model *scriptModel) (*builder.AgentOutput, error) {
		p := &testPlugin{name: "redact", version: version, contribute: func(context.Context, *Env) (Contribution, error) {
			return Contribution{
				Tools: []interfaces.Tool{toolNamed("lookup_order", &runs)},
				PreSteps: []PreStep{{Identity: "mask", Hook: func(context.Context, *builder.Step) (builder.PreStepResult, error) {
					return builder.PreStepResult{}, nil
				}}},
			}, nil
		}}
		h := startedHost(t, p)
		b := builder.NewSimpleBuilder(model).WithDurableScope("support").WithMaxIterations(4)
		if err := Install(ctx, b, h, Dispatch(tools.DispatchConfig{MaxConcurrency: 1})); err != nil {
			return nil, err
		}
		agent, err := b.Build()
		if err != nil {
			return nil, err
		}
		return agent.AsRunnable().Invoke(ctx, "查订单")
	}

	out, err := run("1", newModel(calls("lookup_order"), answer("已查到")))
	require.NoError(t, err)
	require.Equal(t, "已查到", out.Result)

	replay := newModel()
	out, err = run("1", replay)
	require.NoError(t, err)
	require.Equal(t, "已查到", out.Result)
	require.Zero(t, replay.count(), "同版本恢复只回放")
	require.EqualValues(t, 1, runs.Load())

	_, err = run("2", newModel())
	require.ErrorContains(t, err, "intent or attempt policy changed")
}

var _ llm.ToolStreamClient = (*scriptModel)(nil)
