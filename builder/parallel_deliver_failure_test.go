package builder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// 同轮并发调用中间一个的 OnResult 失败：前面的调用照常结束，失败调用以 tool_error 结束且不发 tool_end，
// 后面的调用仍按模型顺序交付；本轮以错误终止，失败调用的工具消息不交还模型。
func TestParallelDeliverFailureStopsRound(t *testing.T) {
	deliverErr := errors.New("结果回调失败")
	sink := &orderedSink{}
	// id 越小越慢：完成顺序为 2,1,0，与模型顺序相反。
	tool := tools.NewBaseTool("probe", "probe", argSchema, func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		var n int
		_, _ = fmt.Sscanf(in.Args["id"].(string), "%d", &n)
		time.Sleep(time.Duration(30-n*10) * time.Millisecond)
		return &interfaces.ToolOutput{Success: true, Result: "r:" + in.Args["id"].(string)}, nil
	})
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tool))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{MaxConcurrency: 3, OnResult: func(_ context.Context, r tools.DispatchResult) error {
		id := r.Args["id"].(string)
		sink.add("on_result(" + id + ")")
		if id == "1" {
			return deliverErr
		}
		return nil
	}})
	require.NoError(t, err)
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: probeCalls("0", "1", "2")}},
		[]*llm.StreamChunk{{Delta: "不应到达"}, {Done: true, FinishReason: "stop"}},
	)
	agent, err := NewSimpleBuilder(model).WithTools(d.Tools()...).WithParallelToolCalls(3).WithMaxIterations(3).Build()
	require.NoError(t, err)

	events, err := agent.ExecuteStream(observe.WithObserver(context.Background(), sink), "并发")
	require.NoError(t, err)
	var steps []string
	var last AgentEvent
	for e := range events {
		last = e
		if e.Type == "step" {
			steps = append(steps, e.Step)
		} else {
			steps = append(steps, e.Type)
		}
	}
	require.Equal(t, []string{"model_start", "tool_start", "tool_start", "tool_start", "tool_end", "error"}, steps)
	require.ErrorIs(t, last.Error, deliverErr)

	// 只保留回调标记与工具、Agent 的结束观测；回调标记紧邻其后的观测，表明观测属于哪次调用。
	var seq []string
	for _, v := range sink.seq {
		if strings.HasPrefix(v, "on_result(") || v == "tool_end" || v == "tool_error" || v == "agent_end" || v == "agent_error" {
			seq = append(seq, v)
		}
	}
	require.Equal(t, []string{
		"on_result(0)", "tool_end",
		"on_result(1)", "tool_error",
		"on_result(2)", "tool_end",
		"agent_error",
	}, seq)

	require.Len(t, model.requests, 1, "本轮失败后不再请求模型")
}
