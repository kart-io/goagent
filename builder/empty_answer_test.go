package builder

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func emptyAnswerContext(t *testing.T, persistent bool) (context.Context, *budget.Controller) {
	t.Helper()
	cfg := budget.Config{RootKey: "empty", Version: "v1", MaxModelRequests: 10}
	if !persistent {
		c, err := budget.New(cfg)
		require.NoError(t, err)
		return budget.WithController(context.Background(), c), c
	}
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	c, err := budget.NewDurable(cfg, store)
	require.NoError(t, err)
	return budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "empty"), store), c), c
}

// 空回答后追加固定重问消息，不追加空 assistant 消息；重问占一次预算，持久模式第二次运行全部回放。
func TestEmptyAnswerRetrySucceeds(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "非持久", true: "持久"}[persistent], func(t *testing.T) {
			ctx, controller := emptyAnswerContext(t, persistent)
			model := nativeModel(
				[]*llm.StreamChunk{{Done: true, FinishReason: "stop"}},
				[]*llm.StreamChunk{{Delta: "答案"}, {Done: true, FinishReason: "stop"}},
			)
			runs := 1
			if persistent {
				runs = 2
			}
			for range runs {
				agent, err := NewSimpleBuilder(model).WithTools(argumentTools(t, tools.DispatchConfig{}, map[string]int{})...).WithDurableScope("empty").WithEmptyAnswerRetries(1).WithMaxIterations(3).Build()
				require.NoError(t, err)
				out, err := agent.AsRunnable().Invoke(ctx, "问题")
				require.NoError(t, err)
				require.Equal(t, "答案", out.Result)
				require.Equal(t, 1, out.Metadata["empty_answer_retries"])
				require.Equal(t, 2, out.Metadata["model_calls"])
			}
			require.Len(t, model.requests, 2, "持久模式第二次运行全部回放")
			first, second := model.requests[0].Messages, model.requests[1].Messages
			require.Len(t, second, len(first)+1)
			require.Equal(t, llm.UserMessage(emptyAnswerPrompt), second[len(second)-1])
			snapshot, err := controller.Snapshot(ctx)
			require.NoError(t, err)
			require.Equal(t, 2, snapshot.ReservedRequests)
			if persistent {
				record, err := durable.InspectOperation(durable.WithExecutionKey(ctx, durable.ChildKey("empty", "agent/empty")), "model/turn/0")
				require.NoError(t, err)
				require.Equal(t, "completed", record.Status)
			}
		})
	}
}

// 关闭、超过次数或已是最后一轮时按原方式报错，持久模式回合记为失败。
func TestEmptyAnswerRetryLimits(t *testing.T) {
	for _, tc := range []struct {
		name               string
		retries, maxRounds int
		requests           int
	}{
		{"关闭", 0, 4, 1},
		{"超过次数", 1, 4, 2},
		{"最后一轮", 3, 2, 2},
	} {
		for _, persistent := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/非持久", true: "/持久"}[persistent], func(t *testing.T) {
				ctx, _ := emptyAnswerContext(t, persistent)
				empty := []*llm.StreamChunk{{Done: true, FinishReason: "stop"}}
				model := nativeModel(empty, empty, empty)
				agent, err := NewSimpleBuilder(model).WithTools(argumentTools(t, tools.DispatchConfig{}, map[string]int{})...).WithDurableScope("empty").WithEmptyAnswerRetries(tc.retries).WithMaxIterations(tc.maxRounds).Build()
				require.NoError(t, err)
				_, err = agent.AsRunnable().Invoke(ctx, "问题")
				if persistent {
					require.ErrorContains(t, err, "model returned no final answer or tool calls")
					_, err = agent.AsRunnable().Invoke(ctx, "问题")
					require.ErrorIs(t, err, durable.ErrOperationAttention)
				} else {
					require.ErrorContains(t, err, "模型未返回最终答案或完整工具调用")
				}
				require.Len(t, model.requests, tc.requests)
			})
		}
	}
}
