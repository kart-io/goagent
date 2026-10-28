package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/internal/livemodel/livemodeltest"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/examples/orchestration/ops/internal/opscase"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func call(id, name string, args any) llm.ToolCall {
	c := llm.ToolCall{ID: id, Type: "function"}
	c.Function.Name = name
	b, _ := json.Marshal(args)
	c.Function.Arguments = string(b)
	return c
}

func submit(id string, evidence ...opscase.Citation) llm.StreamChunk {
	return llm.StreamChunk{ToolCalls: []llm.ToolCall{call(id, "submit_conclusion", map[string]any{
		"root_cause": "config_change", "summary": "连接池上限被改小", "evidence": evidence})}}
}

// 引用号由执行键派生，脚本用 "@<调用 ID>" 指代该调用结果的 ref，见 opscase.BindRefs。
var (
	poolLog   = opscase.Citation{Ref: "@c2", Excerpt: "pool exhausted (max_open=50 in_use=50)"}
	poolCfg   = opscase.Citation{Ref: "@c3", Excerpt: "10:02 config db.max_open_conns 200 -> 50"}
	fabricate = opscase.Citation{Ref: "@c3", Excerpt: "max_open_conns lowered to 50 at 10:01"}
)

// scripted 按回合依次给出预先写好的回复，并记录模型看到的工具结果。
type scripted struct {
	turns []llm.StreamChunk
	mu    sync.Mutex
	seen  []string
}

func (s *scripted) client() *livemodel.Client {
	var calls atomic.Int64
	return livemodel.Wrap(&demomodel.Model{Calls: &calls, Respond: s.respond}, livemodel.Config{Provider: "scripted", Model: "v1"})
}

func (s *scripted) respond(r *llm.CompletionRequest) llm.StreamChunk {
	step := 0
	for _, m := range r.Messages {
		if m.Role == "assistant" {
			step++
		}
	}
	if last := r.Messages[len(r.Messages)-1]; last.Role == "tool" {
		s.mu.Lock()
		s.seen = append(s.seen, last.Content)
		s.mu.Unlock()
	}
	return opscase.BindRefs(r, s.turns[min(step, len(s.turns)-1)])
}

func (s *scripted) invalidSeen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.seen {
		if strings.Contains(c, `"invalid_arguments"`) {
			n++
		}
	}
	return n
}

var investigate = []llm.StreamChunk{
	{ToolCalls: []llm.ToolCall{call("c1", "metrics_query", map[string]string{"service": "checkout-api", "metric": "error_rate"})}},
	{ToolCalls: []llm.ToolCall{call("c2", "logs_query", map[string]string{"service": "checkout-api", "level": "ERROR"})}},
	{ToolCalls: []llm.ToolCall{call("c3", "change_query", map[string]string{"service": "checkout-api"})}},
}

// 通过核验的提交直接给出最终回答，不再请求模型总结。
const final = "结论：config_change\n连接池上限被改小"

func TestScriptedConclusionAccepted(t *testing.T) {
	s := &scripted{turns: append(append([]llm.StreamChunk{}, investigate...), submit("c4", poolLog, poolCfg))}
	client, dir := s.client(), t.TempDir()
	first, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Equal(t, ExpectedRootCause, first.Decision)
	require.NotNil(t, first.Accepted)
	require.Equal(t, final, first.Answer)
	require.Equal(t, [2]int{1, 0}, [2]int{first.Submissions, first.Repairs})
	require.Equal(t, 3, first.DataToolRuns)
	require.Equal(t, int64(4), first.NewModelCalls)
	require.Equal(t, 4, first.Budget.ReservedRequests)

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls)
	require.Zero(t, again.DataToolRuns+again.Submissions, "回放不执行工具")
	require.Equal(t, first.Answer, again.Answer)
	require.Equal(t, first.Budget, again.Budget)
	require.Equal(t, first.Accepted, again.Accepted, "回放由提交调用恢复已核验的结论")
}

// 参数不合法与证据编造都作为参数错误交还模型：模型改正后继续，不中止整个调查。
func TestScriptedRepair(t *testing.T) {
	turns := []llm.StreamChunk{{ToolCalls: []llm.ToolCall{call("c0", "metrics_query", map[string]string{"service": "checkout-api", "metric": "errors"})}}}
	turns = append(turns, investigate...)
	turns = append(turns, submit("c4", poolLog, fabricate), submit("c5", poolLog, poolCfg))
	s := &scripted{turns: turns}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.NoError(t, err)
	require.Equal(t, ExpectedRootCause, out.Decision)
	require.Equal(t, [2]int{2, 1}, [2]int{out.Submissions, out.Repairs})
	require.Equal(t, 2, s.invalidSeen(), "枚举错误与摘录不符各交还一次")
	require.Equal(t, 3, out.DataToolRuns, "参数不合法的调用不执行工具")
	require.Equal(t, int64(6), out.NewModelCalls)
	require.Equal(t, final, out.Answer)
}

// 修复次数用尽后中止，不发布未经核验的结论。
func TestScriptedRepairLimit(t *testing.T) {
	turns := append(append([]llm.StreamChunk{}, investigate...), submit("c4", fabricate), submit("c5", fabricate), submit("c6", fabricate), submit("c7", fabricate))
	s := &scripted{turns: turns}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.ErrorContains(t, err, "结论核验 3 次仍未通过")
	require.Nil(t, out.Accepted)
	require.Equal(t, [2]int{3, MaxRepairs}, [2]int{out.Submissions, out.Repairs})
	require.Equal(t, MaxRepairs, s.invalidSeen())
	require.Equal(t, int64(6), out.NewModelCalls)
}

func TestLiveAlertRCA(t *testing.T) {
	client := livemodeltest.Require(t)
	dir := t.TempDir()
	first, err := Run(context.Background(), client, dir)
	t.Logf("%s/%s：%+v", client.Live.Provider, client.Live.Model, first)
	require.NoError(t, err)
	require.Equal(t, ExpectedRootCause, first.Decision, first.Answer)
	require.NotNil(t, first.Accepted, "必须提交并通过核验")
	require.Equal(t, ExpectedRootCause, first.Accepted.RootCause)
	require.LessOrEqual(t, first.Repairs, MaxRepairs)
	require.GreaterOrEqual(t, first.DataToolRuns, 2)
	require.Equal(t, int(first.NewModelCalls), first.Budget.ReservedRequests)

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls)
	require.Equal(t, first.Answer, again.Answer)
	require.Equal(t, first.Budget, again.Budget)
}

// 宿主只发布通过核验的结论：模型不提交就直接作答报错。
func TestScriptedAnswerBoundToConclusion(t *testing.T) {
	skip := &scripted{turns: append(append([]llm.StreamChunk{}, investigate...), llm.StreamChunk{Content: "结论：code_deploy\n猜的。"})}
	out, err := Run(context.Background(), skip.client(), t.TempDir())
	require.ErrorContains(t, err, "没有提交通过核验的结论")
	require.Empty(t, out.Decision)
}
