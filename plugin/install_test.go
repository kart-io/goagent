package plugin

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func section(name, text string) builder.PromptSection {
	return builder.PromptSection{Name: name, Text: func(builder.PromptScope) string { return text }}
}

func TestInstallRequiresStartedHost(t *testing.T) {
	h, err := NewHost(contributing("a", Contribution{}))
	require.NoError(t, err)
	b := builder.NewSimpleBuilder(newModel())
	require.ErrorContains(t, Install(context.Background(), b, h), "未启动")
	require.NoError(t, h.Start(context.Background()))
	require.NoError(t, h.Stop(context.Background()))
	require.ErrorContains(t, Install(context.Background(), b, h), "未启动")
	var nilBuilder *builder.SimpleAgentBuilder
	require.ErrorContains(t, Install(context.Background(), nilBuilder, h), "非 nil")
}

func TestInstallOnly(t *testing.T) {
	var otherCalled atomic.Bool
	other := &testPlugin{name: "other", version: "1", contribute: func(context.Context, *Env) (Contribution, error) {
		otherCalled.Store(true)
		return Contribution{}, nil
	}}
	var runs atomic.Int32
	orders := contributing("orders", Contribution{Tools: []interfaces.Tool{toolNamed("lookup_order", &runs)}, PromptSections: []builder.PromptSection{section("orders", "退款须提供订单号")}})
	h := startedHost(t, orders, other)

	require.ErrorContains(t, Install(context.Background(), builder.NewSimpleBuilder(newModel()), h, Only("missing")), `"missing"`)
	require.ErrorContains(t, Install(context.Background(), builder.NewSimpleBuilder(newModel()), h, Only()), "至少")

	model := newModel(calls("lookup_order"), answer("已查到"))
	b := builder.NewSimpleBuilder(model).WithMaxIterations(4)
	require.NoError(t, Install(context.Background(), b, h, Only("orders")))
	agent, err := b.Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "订单 A1 呢")
	require.NoError(t, err)
	require.Equal(t, "已查到", out.Result)
	require.EqualValues(t, 1, runs.Load())
	require.False(t, otherCalled.Load())
	require.Contains(t, model.request(0).Messages[0].Content, "退款须提供订单号")
}

func TestInstallRejectsConflictsWithoutChangingBuilder(t *testing.T) {
	noop := func(context.Context, *builder.Step) (builder.PreStepResult, error) {
		return builder.PreStepResult{}, nil
	}
	for _, tc := range []struct {
		name string
		bad  Contribution
		want string
	}{
		{"工具重名", Contribution{Tools: []interfaces.Tool{toolNamed("lookup", nil)}}, `插件 "good" 与 "bad" 提供了同名工具 "lookup"`},
		{"片段重名", Contribution{PromptSections: []builder.PromptSection{section("rules", "x")}}, "同名提示词片段"},
		{"上下文重名", Contribution{PromptContexts: []builder.PromptContext{{Name: "ctx", Text: func(builder.PromptScope) string { return "" }}}}, "同名提示词上下文"},
		{"中间件重名", Contribution{Middleware: []middleware.Middleware{middleware.NewBaseMiddleware("audit")}}, "同名中间件"},
		{"片段缺少名称", Contribution{PromptSections: []builder.PromptSection{section("", "x")}}, "缺少 Name"},
		{"缺少身份", Contribution{PreSteps: []PreStep{{Hook: noop}}}, "缺少 Identity"},
		{"nil 钩子", Contribution{TurnStopping: []builder.TurnStoppingHook{nil}}, "为 nil"},
		{"nil 工具", Contribution{Tools: []interfaces.Tool{nil}}, "为 nil"},
		{"负的恢复次数", Contribution{MaxRecoveries: -1}, "MaxRecoveries"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			good := contributing("good", Contribution{
				Tools:          []interfaces.Tool{toolNamed("lookup", nil)},
				PromptSections: []builder.PromptSection{section("rules", "好插件的规则")},
				PromptContexts: []builder.PromptContext{{Name: "ctx", Text: func(builder.PromptScope) string { return "" }}},
				Middleware:     []middleware.Middleware{middleware.NewBaseMiddleware("audit")},
			})
			h := startedHost(t, good, contributing("bad", tc.bad))
			model := newModel(answer("答"))
			b := builder.NewSimpleBuilder(model).WithTools(toolNamed("host_tool", nil))
			require.ErrorContains(t, Install(context.Background(), b, h), tc.want)

			agent, err := b.Build()
			require.NoError(t, err)
			_, err = agent.AsRunnable().Invoke(context.Background(), "问")
			require.NoError(t, err)
			require.NotContains(t, model.request(0).Messages[0].Content, "好插件的规则", "校验失败时 Builder 不变")
		})
	}
}

func TestInstallContributeError(t *testing.T) {
	boom := errors.New("配置缺失")
	h := startedHost(t, &testPlugin{name: "a", version: "1", contribute: func(context.Context, *Env) (Contribution, error) { return Contribution{}, boom }})
	err := Install(context.Background(), builder.NewSimpleBuilder(newModel()), h)
	require.ErrorIs(t, err, boom)
	require.ErrorContains(t, err, `插件 "a" 贡献失败`)
}

func TestInstallMergesRequestErrors(t *testing.T) {
	down := &llm.ProviderError{StatusCode: 503, Err: errors.New("unavailable")}
	var observed, retried atomic.Int32
	observer := contributing("observer", Contribution{
		MaxRecoveries: 1,
		RequestErrors: []builder.RequestErrorHook{func(context.Context, *builder.RequestFailure) (builder.RequestErrorDecision, error) {
			observed.Add(1)
			return builder.RequestErrorDecision{}, nil
		}},
	})
	retry := contributing("retry", Contribution{
		Tools:         []interfaces.Tool{toolNamed("lookup", nil)},
		MaxRecoveries: 2,
		RequestErrors: []builder.RequestErrorHook{func(context.Context, *builder.RequestFailure) (builder.RequestErrorDecision, error) {
			retried.Add(1)
			return builder.RequestErrorDecision{Action: builder.RequestRetry}, nil
		}},
	})
	h := startedHost(t, observer, retry)

	b := builder.NewSimpleBuilder(newModel(failing(down), failing(down), answer("恢复")))
	require.NoError(t, Install(context.Background(), b, h))
	agent, err := b.Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	require.Equal(t, "恢复", out.Result)
	require.EqualValues(t, 2, observed.Load(), "先安装的插件的钩子先执行")
	require.EqualValues(t, 2, retried.Load())

	b = builder.NewSimpleBuilder(newModel(failing(down), failing(down), failing(down)))
	require.NoError(t, Install(context.Background(), b, h))
	agent, err = b.Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "问")
	require.ErrorIs(t, err, down, "恢复次数取各插件的最大值 2")
}

func TestInstallDispatchWrapsPluginTools(t *testing.T) {
	var runs atomic.Int32
	h := startedHost(t, contributing("orders", Contribution{Tools: []interfaces.Tool{toolNamed("lookup_order", &runs)}}))
	model := newModel(calls("lookup_order"), answer("好"))
	b := builder.NewSimpleBuilder(model).WithMaxIterations(4)
	require.NoError(t, Install(context.Background(), b, h, Dispatch(tools.DispatchConfig{MaxConcurrency: 1})))
	agent, err := b.Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "问")
	require.NoError(t, err)
	require.EqualValues(t, 1, runs.Load())
	var sawResult bool
	for _, m := range model.request(1).Messages {
		sawResult = sawResult || (m.Role == "tool" && strings.Contains(m.Content, "lookup_order 的结果"))
	}
	require.True(t, sawResult)
}
