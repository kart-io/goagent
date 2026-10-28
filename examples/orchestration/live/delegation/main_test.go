package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func call(id, name string, args any) llm.ToolCall {
	c := llm.ToolCall{ID: id, Type: "function"}
	c.Function.Name = name
	b, _ := json.Marshal(args)
	c.Function.Arguments = string(b)
	return c
}

// scripted 按系统提示区分经理与专家，按已有工具结果数决定下一步；记录模型看到的工具结果。
type scripted struct {
	mu   sync.Mutex
	seen []string
}

func (s *scripted) client() *livemodel.Client {
	var calls atomic.Int64
	return livemodel.Wrap(&demomodel.Model{Calls: &calls, Respond: s.respond}, livemodel.Config{Provider: "scripted", Model: "v1"})
}

func (s *scripted) respond(r *llm.CompletionRequest) llm.StreamChunk {
	expert := false
	results := 0
	for _, m := range r.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "财务专家") {
			expert = true
		}
		if m.Role == "tool" {
			results++
			s.mu.Lock()
			s.seen = append(s.seen, m.Content)
			s.mu.Unlock()
		}
	}
	switch {
	case !expert && results == 0:
		return llm.StreamChunk{ToolCalls: []llm.ToolCall{call("m1", "finance_expert", map[string]string{"question": Question})}}
	case !expert:
		return llm.StreamChunk{Content: "结论：9031.20 元\n由财务专家计算。"}
	case results == 0:
		return llm.StreamChunk{ToolCalls: []llm.ToolCall{call("e1", "fx_convert", map[string]any{"amount": 1200, "currency": "USD"})}}
	case results == 1:
		return llm.StreamChunk{ToolCalls: []llm.ToolCall{call("e2", "tax_calc", map[string]any{"amount_cny": 8520})}}
	default:
		return llm.StreamChunk{Content: "含税总价 9031.20 元。"}
	}
}

func (s *scripted) toolResults() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.seen, "\n")
}

func TestExpectedAndParse(t *testing.T) {
	require.InDelta(t, 9031.20, Expected, 1e-9)
	for answer, want := range map[string]float64{"结论：9031.20 元": 9031.2, "**结论：** 9,031.2 元": 9031.2, "结论: 12 元\n理由": 12} {
		got, err := ParseTotal(answer)
		require.NoError(t, err, answer)
		require.InDelta(t, want, got, 1e-9)
	}
	_, err := ParseTotal("含税总价 9031.20 元")
	require.Error(t, err)
}

func TestScriptedDelegation(t *testing.T) {
	s := &scripted{}
	client := s.client()
	dir := t.TempDir()
	first, err := run(context.Background(), client, dir, options{})
	require.NoError(t, err)
	require.InDelta(t, Expected, first.Total, 0.001)
	require.Equal(t, int64(5), first.NewModelCalls, "经理 2 次 + 专家 3 次")
	require.Equal(t, 5, first.Budget.ReservedRequests, "三层共用根预算")
	require.Equal(t, [2]int64{1, 1}, [2]int64{first.ExpertsCreated, first.ExpertsDone})
	require.Equal(t, [2]int64{1, 1}, [2]int64{first.ConvertRuns, first.TaxRuns})
	require.Contains(t, s.toolResults(), `"total_cny":9031.2`)

	again, err := run(context.Background(), client, dir, options{})
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls)
	require.Zero(t, again.ExpertsCreated+again.ConvertRuns+again.TaxRuns)
	require.Equal(t, first.Budget, again.Budget)
}

// 违反嵌套约束是宿主配置错误：整条调用链快速失败并带类型错误返回，
// 不作为工具结果交给模型，避免模型绕过限制继续执行。
func TestScriptedDepthLimit(t *testing.T) {
	// finance_expert 占第 1 层；限制为 1 后，第 2 层的 fx_convert 必须在执行前被拒。
	s := &scripted{}
	out, err := run(context.Background(), s.client(), t.TempDir(), options{expertMaxDepth: 1})
	require.ErrorIs(t, err, tools.ErrNestedExecutionLimit)
	require.Zero(t, out.ConvertRuns+out.TaxRuns, "超过深度的工具不得执行")
	require.Empty(t, s.toolResults(), "错误不交给模型")
	require.Equal(t, int64(2), out.NewModelCalls, "经理与专家各 1 次后停止")
	require.Equal(t, [2]int64{1, 1}, [2]int64{out.ExpertsCreated, out.ExpertsDone}, "出错时子会话也要清理")
}

func TestScriptedDispatcherReentry(t *testing.T) {
	// 专家复用经理正在占用的 Dispatcher，会在排队等待槽位之前被拒，而不是死锁。
	s := &scripted{}
	out, err := run(context.Background(), s.client(), t.TempDir(), options{shareDispatcher: true})
	require.ErrorIs(t, err, tools.ErrDispatcherReentry)
	require.Zero(t, out.ConvertRuns+out.TaxRuns)
	require.Empty(t, s.toolResults())
	require.Equal(t, int64(2), out.NewModelCalls)
	require.Equal(t, [2]int64{1, 1}, [2]int64{out.ExpertsCreated, out.ExpertsDone})
}

func TestLiveDelegation(t *testing.T) {
	client := livemodel.Require(t)
	dir := t.TempDir()
	first, err := Run(context.Background(), client, dir)
	t.Logf("%s/%s：%+v", client.Live.Provider, client.Live.Model, first)
	require.NoError(t, err)
	require.InDelta(t, Expected, first.Total, 0.005, first.Answer)
	require.GreaterOrEqual(t, first.ExpertsCreated, int64(1), "必须委派给财务专家")
	require.Equal(t, first.ExpertsCreated, first.ExpertsDone)
	require.GreaterOrEqual(t, first.ConvertRuns, int64(1))
	require.GreaterOrEqual(t, first.TaxRuns, int64(1))
	require.Equal(t, int(first.NewModelCalls), first.Budget.ReservedRequests, "三层请求都经过根预算")
	require.Positive(t, first.Budget.KnownTokens.TotalTokens)

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls)
	require.Zero(t, again.ExpertsCreated+again.ConvertRuns+again.TaxRuns)
	require.Equal(t, first.Answer, again.Answer)
	require.Equal(t, first.Budget, again.Budget)
}
