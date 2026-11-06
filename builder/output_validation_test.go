package builder

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/middleware"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

type rewriteOutput struct{ *middleware.BaseMiddleware }

func (rewriteOutput) OnAfter(_ context.Context, r *middleware.MiddlewareResponse) (*middleware.MiddlewareResponse, error) {
	r.Output = "无效"
	return r, nil
}

type rewriteEnd struct{ *core.BaseCallback }

func (rewriteEnd) OnEnd(_ context.Context, v any) error {
	v.(*AgentOutput).Result = "无效"
	return nil
}

func TestOutputValidatorRejectsEveryAnswerSource(t *testing.T) {
	rejection := errors.New("最终验收失败")
	for _, mode := range []string{"纯文本", "钩子额度不足", "直接返回", "提前结束", "中间件改写", "Builder回调改写", "Runnable回调改写"} {
		t.Run(mode, func(t *testing.T) {
			model := nativeModel([]*llm.StreamChunk{{Done: true, Content: "通过", FinishReason: "stop"}})
			model.chunks = model.turns[0]
			b := NewSimpleBuilder(model).WithMaxIterations(1)
			tool := NewMockTool("lookup", "合成工具")
			switch mode {
			case "钩子额度不足":
				b.WithTools(tool).WithTurnStopping(func(context.Context, *Stopping) (string, error) {
					t.Fatal("额度不足时不会运行纠正钩子")
					return "", nil
				})
			case "直接返回":
				model.turns = [][]*llm.StreamChunk{{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "lookup", "{}")}}}}
				b.WithTools(tool).WithReturnDirectTools("lookup")
			case "提前结束":
				b.WithTools(tool).WithPreStep(func(context.Context, *Step) (PreStepResult, error) {
					return PreStepResult{Halt: true, Answer: "无效"}, nil
				})
			case "中间件改写":
				b.WithMiddleware(rewriteOutput{middleware.NewBaseMiddleware("改写")})
			case "Builder回调改写":
				b.WithCallbacks(rewriteEnd{core.NewBaseCallback()})
			}
			b.WithOutputValidator(func(_ context.Context, out *AgentOutput) error {
				if mode == "中间件改写" || mode == "Builder回调改写" || mode == "Runnable回调改写" {
					if out.Result == "通过" {
						return nil
					}
				}
				return rejection
			}).WithErrorHandler(func(error) error { return nil })
			a, err := b.Build()
			require.NoError(t, err)
			var r core.Runnable[any, *AgentOutput] = a.AsRunnable()
			if mode == "Runnable回调改写" {
				r = r.WithCallbacks(rewriteEnd{core.NewBaseCallback()})
			}
			stream, err := r.Stream(context.Background(), "核验")
			require.NoError(t, err)
			var got error
			for chunk := range stream {
				require.False(t, chunk.Done && chunk.Error == nil, "无效输出不能发布成功终态")
				if chunk.Error != nil {
					got = chunk.Error
				}
			}
			require.ErrorIs(t, got, rejection)
		})
	}
}

func TestOutputValidatorCancellationPanicAndNil(t *testing.T) {
	_, err := NewSimpleBuilder(NewMockLLMClient()).WithOutputValidator(nil).Build()
	require.Error(t, err)
	for _, mode := range []string{"取消", "异常"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cb := NewMockCallback()
			a, err := NewSimpleBuilder(&eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Done: true, Content: "通过"}}}).WithCallbacks(cb).WithOutputValidator(func(context.Context, *AgentOutput) error {
				if mode == "异常" {
					panic("验收异常")
				}
				cancel()
				return nil
			}).Build()
			require.NoError(t, err)
			out, err := a.AsRunnable().Invoke(ctx, "核验")
			require.Error(t, err)
			require.Nil(t, out)
			require.False(t, cb.onEndCalled)
			if mode == "取消" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				var pe *agentErrors.PanicError
				require.ErrorAs(t, err, &pe)
			}
		})
	}
}

func TestOutputValidatorRechecksDurableReplay(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "lookup", "{}")}}}, []*llm.StreamChunk{{Done: true, Content: "回执", FinishReason: "stop"}})
	effects := 0
	checks := 0
	rejection := errors.New("新验收规则拒绝")
	for attempt := range 2 {
		reg := tools.NewRegistry()
		require.NoError(t, reg.Register(tools.NewBaseTool("lookup", "合成工具", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			effects++
			return &interfaces.ToolOutput{Success: true, Result: "回执"}, nil
		})))
		d, e := tools.NewDispatcher(reg, tools.DispatchConfig{})
		require.NoError(t, e)
		a, e := NewSimpleBuilder(model).WithDurableScope("验收v1").WithTools(d.Tools()...).WithOutputValidator(func(context.Context, *AgentOutput) error {
			checks++
			if attempt == 1 {
				return rejection
			}
			return nil
		}).Build()
		require.NoError(t, e)
		ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "run"), store)
		out, e := a.AsRunnable().Invoke(ctx, "核验")
		if attempt == 0 {
			require.NoError(t, e)
			require.Equal(t, "回执", out.Result)
		} else {
			require.ErrorIs(t, e, rejection)
			require.Nil(t, out)
		}
	}
	require.GreaterOrEqual(t, checks, 2)
	require.Equal(t, 1, effects)
	require.Len(t, model.requests, 2)
}
