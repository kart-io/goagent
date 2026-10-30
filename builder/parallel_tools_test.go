package builder

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// concurrencyProbe 记录同时执行的调用数峰值；id 越小执行越久，使完成顺序与调用顺序相反。
type concurrencyProbe struct {
	mu            sync.Mutex
	active, peak  int
	calls         int
	siblingCancel bool
}

func (p *concurrencyProbe) invoke(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	id := in.Args["id"].(string)
	p.mu.Lock()
	p.calls++
	p.active++
	p.peak = max(p.peak, p.active)
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.active--
		p.mu.Unlock()
	}()
	if id == "bad" {
		return nil, errors.New("执行失败")
	}
	var n int
	_, _ = fmt.Sscanf(id, "%d", &n)
	time.Sleep(time.Duration(60-n*10) * time.Millisecond)
	if ctx.Err() != nil {
		p.mu.Lock()
		p.siblingCancel = true
		p.mu.Unlock()
	}
	return &interfaces.ToolOutput{Success: true, Result: "probe:" + id}, nil
}

func probeTools(t *testing.T, probe *concurrencyProbe, maxConcurrency int) []interfaces.Tool {
	t.Helper()
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tools.NewBaseTool("probe", "probe", argSchema, probe.invoke)))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{MaxConcurrency: maxConcurrency})
	require.NoError(t, err)
	return d.Tools()
}

func probeCalls(ids ...string) []llm.ToolCall {
	calls := make([]llm.ToolCall, len(ids))
	for i, id := range ids {
		calls[i] = nativeCall("c"+id, "probe", fmt.Sprintf(`{"id":%q}`, id))
	}
	return calls
}

// 默认顺序执行；n>=2 时峰值等于 min(n, MaxConcurrency)；工具消息与事件按调用顺序，与完成顺序无关。
func TestParallelToolCallsPeakAndOrder(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		limit, maxConcurrency int
		wantPeak              int
	}{
		{"默认顺序执行", 0, 5, 1},
		{"不另设上限受工具限流", math.MaxInt, 3, 3},
		{"不另设上限全部并发", math.MaxInt, 5, 5},
		{"上限更小", 2, 4, 2},
		{"关闭", 1, 4, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &concurrencyProbe{}
			model := nativeModel(
				[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: probeCalls("0", "1", "2", "3", "4")}},
				[]*llm.StreamChunk{{Delta: "汇总"}, {Done: true, FinishReason: "stop"}},
			)
			agent, err := NewSimpleBuilder(model).WithTools(probeTools(t, probe, tc.maxConcurrency)...).
				WithParallelToolCalls(tc.limit).WithMaxIterations(3).Build()
			require.NoError(t, err)
			events, err := agent.ExecuteStream(context.Background(), "并发")
			require.NoError(t, err)
			var steps []string
			for e := range events {
				require.NoError(t, e.Error)
				if e.Type == "step" && e.Tool != "" {
					steps = append(steps, e.Step)
				}
			}
			require.Equal(t, tc.wantPeak, probe.peak)
			if tc.wantPeak > 1 {
				require.Equal(t, []string{"tool_start", "tool_start", "tool_start", "tool_start", "tool_start", "tool_end", "tool_end", "tool_end", "tool_end", "tool_end"}, steps)
			}
			messages := model.requests[1].Messages
			results := messages[len(messages)-5:]
			for i, m := range results {
				require.Equal(t, "tool", m.Role)
				require.Equal(t, fmt.Sprintf("c%d", i), m.ToolCallID)
				require.Equal(t, fmt.Sprintf("probe:%d", i), m.Content)
			}
		})
	}
}

// 一个调用失败不取消其他调用：其他调用跑完并记为已完成，全部结束后才返回错误。
func TestParallelToolCallFailureDoesNotCancelSiblings(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
	probe := &concurrencyProbe{}
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: probeCalls("1", "bad", "2")}})
	agent, err := NewSimpleBuilder(model).WithDurableScope("parallel").WithTools(probeTools(t, probe, 4)...).WithParallelToolCalls(4).WithMaxIterations(3).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(ctx, "并发")
	require.ErrorContains(t, err, "执行失败")
	probe.mu.Lock()
	require.Equal(t, 3, probe.calls)
	require.Zero(t, probe.active, "返回前全部调用已结束")
	require.False(t, probe.siblingCancel)
	probe.mu.Unlock()
	scoped := durable.WithExecutionKey(ctx, durable.ChildKey("node", "agent/parallel"))
	for id, want := range map[string]string{"c1": "completed", "c2": "completed", "cbad": "failed"} {
		record, err := durable.InspectOperation(scoped, "tool/turn/0/call/"+id)
		require.NoError(t, err)
		require.Equal(t, want, record.Status, id)
		if want == "failed" {
			require.Equal(t, "execution", record.FailureKind)
		}
	}
}

// 持久模式并发执行后，第二次运行全部回放，不再请求模型或执行工具。
func TestParallelToolCallsReplay(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store)
	probe := &concurrencyProbe{}
	business := probeTools(t, probe, 4)
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: probeCalls("0", "1", "2")}},
		[]*llm.StreamChunk{{Delta: "汇总"}, {Done: true, FinishReason: "stop"}},
	)
	for range 2 {
		agent, err := NewSimpleBuilder(model).WithDurableScope("parallel").WithTools(business...).WithParallelToolCalls(4).WithMaxIterations(3).Build()
		require.NoError(t, err)
		out, err := agent.AsRunnable().Invoke(ctx, "并发")
		require.NoError(t, err)
		require.Equal(t, "汇总", out.Result)
	}
	require.Len(t, model.requests, 2)
	require.Equal(t, 3, probe.calls)
	require.Equal(t, 3, probe.peak)
}

type probePolicy struct{ tool interfaces.Tool }

func (p *probePolicy) Descriptor() contextpolicy.Descriptor {
	return contextpolicy.Descriptor{ID: "probe", Version: "v1", ConfigHash: "hash"}
}
func (p *probePolicy) BeforeTurn(_ context.Context, t contextpolicy.Turn) (contextpolicy.Turn, error) {
	return t, nil
}
func (p *probePolicy) Tools() []interfaces.Tool { return []interfaces.Tool{p.tool} }

// 含策略工具或未声明并发能力的工具时，整轮按顺序执行。
func TestParallelToolCallsFallBackToSequential(t *testing.T) {
	t.Run("策略工具", func(t *testing.T) {
		probe := &concurrencyProbe{}
		calls := append(probeCalls("0", "1"), nativeCall("p", "policy_probe", "{}"))
		model := nativeModel(
			[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: calls}},
			[]*llm.StreamChunk{{Delta: "汇总"}, {Done: true, FinishReason: "stop"}},
		)
		policyTool := NewMockTool("policy_probe", "策略结果")
		agent, err := NewSimpleBuilder(model).WithTools(probeTools(t, probe, 4)...).
			WithContextPolicies(func() contextpolicy.Policy { return &probePolicy{tool: policyTool} }).WithParallelToolCalls(4).WithMaxIterations(3).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(context.Background(), "并发")
		require.NoError(t, err)
		require.Equal(t, 1, probe.peak)
	})
	t.Run("普通工具", func(t *testing.T) {
		probe := &concurrencyProbe{}
		calls := append(probeCalls("0", "1"), nativeCall("p", "plain", "{}"))
		model := nativeModel(
			[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: calls}},
			[]*llm.StreamChunk{{Delta: "汇总"}, {Done: true, FinishReason: "stop"}},
		)
		agent, err := NewSimpleBuilder(model).WithTools(append(probeTools(t, probe, 4), NewMockTool("plain", "结果"))...).WithParallelToolCalls(4).WithMaxIterations(3).Build()
		require.NoError(t, err)
		_, err = agent.AsRunnable().Invoke(context.Background(), "并发")
		require.NoError(t, err)
		require.Equal(t, 1, probe.peak)
	})
}
