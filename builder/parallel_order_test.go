package builder

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// 默认（ParallelToolCalls=0）与显式 1 都按顺序执行：即使工具经 Dispatcher 声明可并发，
// 执行先后也必须等于模型给出的顺序。
type orderRecorder struct {
	mu    sync.Mutex
	order []string
}

func (r *orderRecorder) invoke(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	id := in.Args["id"].(string)
	r.mu.Lock()
	r.order = append(r.order, id)
	r.mu.Unlock()
	return &interfaces.ToolOutput{Success: true, Result: "r:" + id}, nil
}

func runOrder(t *testing.T, parallel *int, viaDispatcher bool) ([]string, []string) {
	t.Helper()
	rec := &orderRecorder{}
	tool := tools.NewBaseTool("probe", "probe", argSchema, rec.invoke)
	var ts []interfaces.Tool
	if viaDispatcher {
		reg := tools.NewRegistry()
		require.NoError(t, reg.Register(tool))
		d, err := tools.NewDispatcher(reg, tools.DispatchConfig{MaxConcurrency: 1})
		require.NoError(t, err)
		ts = d.Tools()
	} else {
		ts = []interfaces.Tool{tool}
	}
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: probeCalls("0", "1", "2", "3", "4")}},
		[]*llm.StreamChunk{{Delta: "汇总"}, {Done: true, FinishReason: "stop"}},
	)
	b := NewSimpleBuilder(model).WithTools(ts...).WithMaxIterations(3)
	if parallel != nil {
		b = b.WithParallelToolCalls(*parallel)
	}
	agent, err := b.Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "顺序")
	require.NoError(t, err)
	var toolMsgs []string
	for _, m := range model.requests[1].Messages {
		if m.Role == "tool" {
			toolMsgs = append(toolMsgs, m.ToolCallID+"="+m.Content)
		}
	}
	return rec.order, toolMsgs
}

func TestDefaultParallelOrder(t *testing.T) {
	want := []string{"0", "1", "2", "3", "4"}
	wantMsgs := []string{"c0=r:0", "c1=r:1", "c2=r:2", "c3=r:3", "c4=r:4"}
	one := 1
	const runs = 200
	for _, tc := range []struct {
		name          string
		parallel      *int
		viaDispatcher bool
	}{
		{"默认0_Dispatcher_MaxConcurrency1", nil, true},
		{"显式1_Dispatcher_MaxConcurrency1", &one, true},
		{"默认0_普通工具", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outOfOrder := 0
			samples := map[string]int{}
			for range runs {
				order, msgs := runOrder(t, tc.parallel, tc.viaDispatcher)
				require.Equal(t, wantMsgs, msgs, "交还模型的工具消息始终按模型顺序")
				if !slices.Equal(order, want) {
					outOfOrder++
					samples[fmt.Sprint(order)]++
				}
			}
			require.Zero(t, outOfOrder, "%d/%d 次执行顺序与模型顺序不一致；样例：%v", outOfOrder, runs, samples)
		})
	}
}
