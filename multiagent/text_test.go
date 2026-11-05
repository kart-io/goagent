package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/workflow"
)

// capture 返回记录 Agent 收到的输入并回答 answer 的会话工厂。
func capture(s *sessions, inputs *[]any, answer any) AgentFactory {
	return s.fake(func(_ context.Context, in any) (any, error) {
		*inputs = append(*inputs, in)
		return answer, nil
	})
}

func runText(t *testing.T, s Step[Conversation], c Conversation) Conversation {
	t.Helper()
	g, err := Sequential(workflow.Config{}, s)
	require.NoError(t, err)
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(context.Background(), c)
	require.NoError(t, err)
	return out
}

var history = Conversation{Input: "客户：我要退款", Turns: []Turn{{"triage", "refund"}, {"refund", "已登记"}, {"triage", "FINISH"}}}

func TestTextStepDefaultInputHasAllTurns(t *testing.T) {
	var s sessions
	var inputs []any
	step, err := TextStep("writer", capture(&s, &inputs, "草稿"))
	require.NoError(t, err)
	out := runText(t, step, history)
	require.Equal(t, "客户：我要退款\n[triage]: refund\n[refund]: 已登记\n[triage]: FINISH", inputs[0])
	require.Equal(t, Turn{Agent: "writer", Output: "草稿"}, out.Turns[3])
	require.Len(t, history.Turns, 3, "不修改输入状态")
}

func TestTextStepWindowAndRender(t *testing.T) {
	var s sessions
	var inputs []any
	step, err := TextStep("writer", capture(&s, &inputs, ""), WithLastTurns(1))
	require.NoError(t, err)
	out := runText(t, step, history)
	require.Equal(t, "客户：我要退款\n[triage]: FINISH", inputs[0])
	require.Equal(t, Turn{Agent: "writer"}, out.Turns[3], "空回答也记录")

	step, err = TextStep("writer", capture(&s, &inputs, "ok"), WithRender(func(c Conversation) any {
		data, _ := json.Marshal(c)
		return string(data)
	}))
	require.NoError(t, err)
	runText(t, step, history)
	require.JSONEq(t, `{"Input":"客户：我要退款","Turns":[{"Agent":"triage","Output":"refund"},{"Agent":"refund","Output":"已登记"},{"Agent":"triage","Output":"FINISH"}]}`, inputs[1].(string))
}

func TestTextStepOptionValidation(t *testing.T) {
	var s sessions
	agent := s.fake(func(context.Context, any) (any, error) { return "", nil })
	for name, opts := range map[string][]TextOption{
		"互斥":              {WithLastTurns(1), WithRender(func(Conversation) any { return "" })},
		"k 为 0":           {WithLastTurns(0)},
		"k 为负":            {WithLastTurns(-1)},
		"WithRender(nil)": {WithRender(nil)},
		"nil 选项":          {nil},
	} {
		_, err := TextStep("writer", agent, opts...)
		require.Error(t, err, name)
	}
	_, err := TextStep("writer", nil)
	require.Error(t, err)
}

func TestTextStepEncodesNonStringResult(t *testing.T) {
	var s sessions
	var inputs []any
	step, err := TextStep("extract", capture(&s, &inputs, map[string]any{"order": "A1"}))
	require.NoError(t, err)
	out := runText(t, step, Conversation{Input: "q"})
	require.Equal(t, `{"order":"A1"}`, out.Turns[0].Output)
}

func TestCloneConversationIsolatesSharedCapacity(t *testing.T) {
	base := Conversation{Input: "q", Turns: make([]Turn, 1, 4)}
	a, b := CloneConversation(base), CloneConversation(base)
	a.Turns = append(a.Turns, Turn{Agent: "kb", Output: "知识"})
	b.Turns = append(b.Turns, Turn{Agent: "order", Output: "订单"})
	require.Equal(t, "kb", a.Turns[1].Agent)
	require.Equal(t, "order", b.Turns[1].Agent)
	require.Len(t, base.Turns, 1)
}

func TestMergeTurns(t *testing.T) {
	base := Conversation{Input: "q", Turns: []Turn{{"triage", "lookup"}}}
	kb := CloneConversation(base)
	kb.Turns = append(kb.Turns, Turn{"kb", "知识"})
	order := CloneConversation(base)
	order.Turns = append(order.Turns, Turn{"order", "订单"}, Turn{"order", "补充"})
	out, err := MergeTurns(context.Background(), base, []Conversation{kb, order})
	require.NoError(t, err)
	require.Equal(t, []Turn{{"triage", "lookup"}, {"kb", "知识"}, {"order", "订单"}, {"order", "补充"}}, out.Turns)

	_, err = MergeTurns(context.Background(), base, []Conversation{{Input: "q"}})
	require.Error(t, err, "分支短于 base")
	_, err = MergeTurns(context.Background(), base, []Conversation{{Input: "q", Turns: []Turn{{"x", "y"}}}})
	require.Error(t, err, "前缀不同")
}

func textMember(t *testing.T, name string) Step[Conversation] {
	t.Helper()
	var s sessions
	step, err := TextStep(name, s.fake(func(context.Context, any) (any, error) { return "", nil }))
	require.NoError(t, err)
	return step
}

func TestTextNextValidation(t *testing.T) {
	_, err := TextNext()
	require.Error(t, err)
	for name, members := range map[string][]Step[Conversation]{
		"多行":     {{Name: "a\nb"}},
		"回车":     {{Name: "a\rb"}},
		"重复":     {textMember(t, "a"), textMember(t, "a")},
		"FINISH": {{Name: Finish}},
		"空名":     {{Name: ""}},
	} {
		_, err := TextNext(members...)
		require.Error(t, err, name)
	}
}

func TestTextNextReadsFirstLine(t *testing.T) {
	next, err := TextNext(textMember(t, "refund"), textMember(t, "logistics"))
	require.NoError(t, err)
	last := func(output string) Conversation {
		return Conversation{Input: "q", Turns: []Turn{{"triage", output}}}
	}
	for output, want := range map[string]string{
		"refund":                 "refund",
		"  logistics  \n理由：物流延误": "logistics",
		"FINISH\r\n":             Finish,
	} {
		got, err := next(context.Background(), last(output))
		require.NoError(t, err, output)
		require.Equal(t, want, got, output)
	}
	for name, c := range map[string]Conversation{
		"空记录":  {Input: "q"},
		"空首行":  last("\n refund"),
		"模糊匹配": last("退款（原因：重复扣款）"),
		"大小写":  last("Refund"),
		"前缀":   last("refund please"),
	} {
		_, err := next(context.Background(), c)
		require.ErrorIs(t, err, ErrInvalidRoute, name)
	}
}

func TestTextRouterWithScriptedOutputs(t *testing.T) {
	var s sessions
	decisions := []string{"refund", "FINISH"}
	calls := 0
	triage, err := TextStep("triage", s.fake(func(context.Context, any) (any, error) {
		calls++
		return decisions[calls-1], nil
	}))
	require.NoError(t, err)
	refund, err := TextStep("refund", s.fake(func(context.Context, any) (any, error) { return "已退款", nil }))
	require.NoError(t, err)
	next, err := TextNext(refund)
	require.NoError(t, err)
	g, err := RouterSupervisor(workflow.Config{}, RouterConfig[Conversation]{Supervisor: triage, Members: []Step[Conversation]{refund}, Next: next, MaxRounds: 2})
	require.NoError(t, err)
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(context.Background(), Conversation{Input: "退款"})
	require.NoError(t, err)
	require.Equal(t, fmt.Sprint([]Turn{{"triage", "refund"}, {"refund", "已退款"}, {"triage", "FINISH"}}), fmt.Sprint(out.Turns))
}
