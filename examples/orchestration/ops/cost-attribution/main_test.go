package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/internal/livemodel/livemodeltest"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/examples/orchestration/ops/internal/opscase"
	"github.com/kart-io/goagent/interfaces"
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

func turn(c llm.ToolCall) llm.StreamChunk { return llm.StreamChunk{ToolCalls: []llm.ToolCall{c}} }

func submit(id, summary string, evidence ...opscase.Citation) llm.StreamChunk {
	return submitTop(id, "USD", "ai", summary, evidence...)
}

func submitTop(id, currency, category, summary string, evidence ...opscase.Citation) llm.StreamChunk {
	return turn(call(id, "submit_conclusion", map[string]any{"top_currency": currency, "top_category": category, "summary": summary, "evidence": evidence}))
}

var (
	compare   = turn(call("c1", "cost_query", map[string]any{"action": "compare", "month": "2026-09", "base_month": "2026-08"}))
	breakdown = turn(call("c2", "cost_query", map[string]any{"action": "breakdown", "month": "2026-09", "category": "ai", "currency": "USD"}))
	// 引用号由执行键派生，脚本用 "@<调用 ID>" 指代该调用结果的 ref，见 opscase.BindRefs。
	usdAI     = opscase.Citation{Ref: "@c1", Excerpt: "pct: 87.5%"}
	inference = opscase.Citation{Ref: "@c2", Excerpt: "amount: 750.00"}
)

const (
	goodSummary = "USD 的 ai 大类 2026-09 为 1125.00，较 2026-08 增加 525.00，环比 87.5%，为最大涨幅；CNY 最大为 compute 的 8.1%。"
	fabricated  = "USD 的 ai 环比 88.0%，为最大涨幅。"
	crossSum    = "CNY 与 USD 的增长合计为 1000.00 加 525.00。"
	final       = "结论：USD/ai\n" + goodSummary // 通过核验的提交直接给出的最终回答
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

func granted() map[string]bool { return map[string]bool{"cn-prod": true, "us-ml": true} }

func find(items []Item, key string) *Item {
	for i := range items {
		if items[i].Key == key {
			return &items[i]
		}
	}
	return nil
}

// 环比计算精确到分：每个币种内按差值降序，金额不完整的大类单独列出。
func TestCompareCents(t *testing.T) {
	groups := Compare(2, 1, filter{accounts: granted()})
	require.Len(t, groups, 2)
	cny, usd := groups[0], groups[1]
	require.Equal(t, "CNY", cny.Currency)
	keys := func(items []Item) (out []string) {
		for _, it := range items {
			out = append(out, it.Key)
		}
		return
	}
	require.Equal(t, []string{"compute", "storage", "ai", "network"}, keys(cny.Ranked))
	require.Equal(t, []string{"ai", "compute", "storage", "network", "database"}, keys(usd.Ranked))
	require.Equal(t, Item{Key: "ai", Base: "600.00", Current: "1125.00", Delta: "525.00", Pct: "87.5%", DeltaCents: 52500, BaseCents: 60000}, usd.Ranked[0])
	require.Equal(t, Item{Key: "compute", Base: "12300.00", Current: "13300.00", Delta: "1000.00", Pct: "8.1%", DeltaCents: 100000, BaseCents: 1230000}, cny.Ranked[0])
	require.Equal(t, Item{Key: "network", Base: "3100.00", Current: "3000.00", Delta: "-100.00", Pct: "-3.2%", DeltaCents: -10000, BaseCents: 310000}, cny.Ranked[3])
	require.Equal(t, "0.0%", find(usd.Ranked, "database").Pct)

	// 同一币种的多个账号相加：加入 us-legacy 后 USD network = egress + cdn。
	all := Compare(2, 1, filter{accounts: map[string]bool{"cn-prod": true, "us-ml": true, "us-legacy": true}, currency: "USD"})
	require.Len(t, all, 1)
	require.Equal(t, int64(81000), find(all[0].Ranked, "network").DeltaCents)
	require.Equal(t, "270.0%", find(all[0].Ranked, "network").Pct)
}

// 金额缺失不当 0：database 08 月缺 rds，不参与排序，也不会算出 +600% 的假涨幅。
func TestMissingAmountNotZero(t *testing.T) {
	cny := Compare(2, 1, filter{accounts: granted()})[0]
	require.Nil(t, find(cny.Ranked, "database"))
	db := find(cny.Unranked, "database")
	require.NotNil(t, db)
	require.Equal(t, []string{"incomplete", "7000.00", ""}, []string{db.Base, db.Current, db.Delta})
	require.Equal(t, []string{"rds"}, db.Missing)

	out, err := CostQuery(context.Background(), map[string]any{"action": "totals", "month": "2026-08", "currency": "CNY"})
	require.NoError(t, err)
	item := find(out.(map[string]any)["currencies"].([]Group)[0].Items, "database")
	require.Equal(t, "incomplete", item.Amount)
	require.Equal(t, []string{"rds"}, item.Missing)
}

// 数据起点之前标注 no_data：us-ml 从 08 月开始，07 月既不是 0 也不计算增长。
func TestNoDataBeforeStart(t *testing.T) {
	usd := Compare(1, 0, filter{accounts: granted(), currency: "USD"})[0]
	require.Empty(t, usd.Ranked)
	require.Len(t, usd.Unranked, 5)
	for _, it := range usd.Unranked {
		require.Equal(t, "no_data", it.Base, it.Key)
		require.Empty(t, it.Pct)
	}
	out, err := CostQuery(context.Background(), map[string]any{"action": "totals", "month": "2026-07", "currency": "USD"})
	require.NoError(t, err)
	require.Equal(t, "no_data", find(out.(map[string]any)["currencies"].([]Group)[0].Items, "ai").Amount)
}

// 多账号聚合时，任一账号处于数据起点之前，整格保持 no_data：us-legacy 的 07 月金额不能单独当作基数，算出假涨幅。
func TestNoDataAcrossAccounts(t *testing.T) {
	both := map[string]bool{"us-ml": true, "us-legacy": true}
	usd := Compare(1, 0, filter{accounts: both, currency: "USD"})[0]
	require.Empty(t, usd.Ranked)
	network := find(usd.Unranked, "network")
	require.NotNil(t, network)
	require.Equal(t, []string{"no_data", "300.00", "", ""}, []string{network.Base, network.Current, network.Delta, network.Pct})

	out, err := CostQuery(context.Background(), map[string]any{"action": "totals", "month": "2026-07", "currency": "USD", "account_ids": []any{"us-ml", "us-legacy"}})
	require.NoError(t, err)
	require.Equal(t, "no_data", find(out.(map[string]any)["currencies"].([]Group)[0].Items, "compute").Amount)
}

// 基数为 0 时不计算百分比（不会输出 +Inf% 或 NaN），放进 unranked 并注明。
func TestZeroBaseUnranked(t *testing.T) {
	orig := bill
	bill = append(slices.Clone(orig), line{"us-ml", "ai", "agent", [3]int64{0, 0, 5000}})
	t.Cleanup(func() { bill = orig })
	usd := Compare(2, 1, filter{accounts: granted(), currency: "USD", category: "ai", byProduct: true})[0]
	require.Equal(t, []string{"inference", "training"}, []string{usd.Ranked[0].Key, usd.Ranked[1].Key})
	agent := find(usd.Unranked, "agent")
	require.NotNil(t, agent)
	require.Equal(t, []string{"0.00", "50.00", "50.00", ""}, []string{agent.Base, agent.Current, agent.Delta, agent.Pct})
	require.Contains(t, agent.Note, "基数为 0")
	b, _ := json.Marshal(usd)
	require.NotRegexp(t, `Inf|NaN`, string(b))
}

// 参数组合不合法时返回 *tools.ArgumentError，交还模型改正。
func TestCostQueryArgumentErrors(t *testing.T) {
	cases := map[string]map[string]any{
		"compare 缺 base_month":     {"action": "compare", "month": "2026-09"},
		"base_month 不早于 month":     {"action": "compare", "month": "2026-08", "base_month": "2026-09"},
		"month 超出账期":               {"action": "totals", "month": "2026-10"},
		"breakdown 缺 category":     {"action": "breakdown", "month": "2026-09"},
		"totals 带 base_month":      {"action": "totals", "month": "2026-09", "base_month": "2026-08"},
		"product 维度缺 category":     {"action": "compare", "month": "2026-09", "base_month": "2026-08", "dimension": "product"},
		"product 维度用于非 compare 动作": {"action": "totals", "month": "2026-09", "dimension": "product", "category": "ai"},
	}
	for name, args := range cases {
		_, err := CostQuery(context.Background(), args)
		var argErr *tools.ArgumentError
		require.True(t, errors.As(err, &argErr), name)
	}
}

// ledgerValues 让 cost_query 经账本与 Dispatcher 执行一次，返回账本登记的全部标量值。
func ledgerValues(t *testing.T, args map[string]any) []string {
	ledger := opscase.NewLedger()
	ts, err := opscase.Dispatch([]interfaces.Tool{ledger.Wrap(costTool())}, tools.DispatchConfig{OnResult: ledger.OnResult})
	require.NoError(t, err)
	_, err = ts[0].Invoke(context.Background(), &interfaces.ToolInput{Args: args})
	require.NoError(t, err)
	return ledger.Values()
}

func TestCheckSummary(t *testing.T) {
	values := ledgerValues(t, map[string]any{"action": "compare", "month": "2026-09", "base_month": "2026-08"})
	require.Empty(t, CheckSummary(goodSummary, values))
	require.Empty(t, CheckSummary("CNY network 下降 100.00（3.2%）", values), "负值可去掉符号引用")
	require.Len(t, CheckSummary(fabricated, values), 1)
	require.Len(t, CheckSummary("ai 涨了 7.5%", values), 1, "不能只匹配 87.5% 的尾部")
	require.Equal(t, []string{"summary 把 CNY 与 USD 合计，不同币种不换汇、不相加"}, CheckSummary(crossSum, values))
	require.Empty(t, CheckSummary("CNY 合计 1000.00。USD 为 525.00。", values), "分句各自只涉及一个币种")
}

// 数字按完整字段值核对：整数百分比、带单位的整数金额、少一位小数都要识别；千分位逗号可去掉；月份不算金额。
func TestCheckSummaryNumbers(t *testing.T) {
	values := ledgerValues(t, map[string]any{"action": "compare", "month": "2026-09", "base_month": "2026-08"})
	for _, bad := range []string{"USD 的 ai 环比 88%", "USD 的 ai 上月 600 美元", "USD 的 ai 本月 1125.0", "本月 1,125 USD"} {
		require.Len(t, CheckSummary(bad, values), 1, bad)
	}
	require.Empty(t, CheckSummary("USD 的 ai 本月 1,125.00，上月 600.00 USD", values))
	require.Empty(t, CheckSummary("9 月比 8 月上涨，2026-09 的 ai 为最大涨幅", values))
	require.Empty(t, CheckSummary("2026-09 USD 的 ai 较 2026-08 USD 增加 525.00", values), "日期后紧跟币种不是金额")
	require.Len(t, CheckSummary("下降 -100 USD", values), 1, "负号金额仍要核验")
}

// 审查发现的绕过方式：冒号后的数字、前置币种的整数、省略负号却没有方向词，都要被拒。
func TestCheckSummaryBypasses(t *testing.T) {
	values := ledgerValues(t, map[string]any{"action": "compare", "month": "2026-09", "base_month": "2026-08"})
	for _, bad := range []string{
		"USD 的 ai 涨幅:99.9%",             // 冒号后的数字不是时间
		"USD 的 ai 涨幅：99.9%",             // 全角冒号
		"USD 9999",                      // 前置币种
		"本月花费 $9999",                    // 前置币种符号
		"USD 87.0%",                     // 前置币种的百分比
		"CNY network 环比 3.2%",           // 省略负号且没有方向词，方向反转；100.00 另有正值，不能用来举例
		"CNY network 增加 3.2%",           // 方向与数据相反
		"CNY network 下降 100.00。增加 3.2%", // 方向词只在另一句
	} {
		require.NotEmpty(t, CheckSummary(bad, values), bad)
	}
	for _, ok := range []string{
		"CNY network 下降 100.00（3.2%）",
		"CNY network 减少 100.00",
		"CNY network 为 -100.00，环比 -3.2%",
		"USD 525.00",
		"USD 87.5%",
		"10:05 起 USD 的 ai 为 1125.00",    // 时间整段剔除
		"2026-09-30 USD 的 ai 为 1125.00", // 日期整段剔除
		"USD 9 月的 ai 为 1125.00",         // 前置币种后紧跟月份
	} {
		require.Empty(t, CheckSummary(ok, values), ok)
	}
}

// top_currency/top_category 由宿主复算：跨币种只比较 pct，CNY/compute 的差值更大但涨幅更小。
func TestCheckTop(t *testing.T) {
	require.Empty(t, CheckTop(Conclusion{TopCurrency: "USD", TopCategory: "ai"}))
	for _, bad := range [][2]string{{"CNY", "compute"}, {"USD", "compute"}, {"CNY", "ai"}} {
		problems := CheckTop(Conclusion{TopCurrency: bad[0], TopCategory: bad[1]})
		require.Len(t, problems, 1, bad)
		require.Contains(t, problems[0], "USD/ai（87.5%）")
	}
}

func TestScriptedConclusionAccepted(t *testing.T) {
	s := &scripted{turns: []llm.StreamChunk{compare, breakdown, submit("c3", goodSummary, usdAI, inference)}}
	client, dir := s.client(), t.TempDir()
	first, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Equal(t, ExpectedDecision, first.Decision)
	require.NotNil(t, first.Accepted)
	require.Equal(t, final, first.Answer)
	require.Equal(t, [2]int{1, 0}, [2]int{first.Submissions, first.Repairs})
	require.Equal(t, 2, first.DataToolRuns)
	require.Equal(t, int64(3), first.NewModelCalls)
	require.Equal(t, 3, first.Budget.ReservedRequests)

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls)
	require.Zero(t, again.DataToolRuns+again.Submissions, "回放不执行工具")
	require.Equal(t, first.Answer, again.Answer)
	require.Equal(t, first.Budget, again.Budget)
	require.Equal(t, first.Accepted, again.Accepted, "回放由提交调用恢复已核验的结论")
	require.Equal(t, ExpectedDecision, again.Decision)
}

// 宿主只发布通过核验的结论：模型不提交就直接作答报错。
func TestScriptedAnswerBoundToConclusion(t *testing.T) {
	skip := &scripted{turns: []llm.StreamChunk{compare, {Content: "结论：USD/ai\n猜的。"}}}
	out, err := Run(context.Background(), skip.client(), t.TempDir())
	require.ErrorContains(t, err, "没有提交通过核验的结论")
	require.Empty(t, out.Decision)
}

// 模型把差值最大的 CNY/compute 当成涨幅最大：宿主复算后交还一次，改正后通过。
func TestScriptedTopRepair(t *testing.T) {
	wrong := submitTop("c2", "CNY", "compute", "CNY 的 compute 增加 1000.00，环比 8.1%。", usdAI)
	s := &scripted{turns: []llm.StreamChunk{compare, wrong, submit("c3", goodSummary, usdAI)}}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.NoError(t, err)
	require.Equal(t, ExpectedDecision, out.Decision)
	require.Equal(t, [2]int{2, 1}, [2]int{out.Submissions, out.Repairs})
	require.Contains(t, s.seen[1], "USD/ai（87.5%）")
	require.Equal(t, int64(3), out.NewModelCalls)
}

// compare 缺 base_month 作为参数错误交还模型；改正后继续，不中止。
func TestScriptedArgumentRepair(t *testing.T) {
	bad := turn(call("c0", "cost_query", map[string]any{"action": "compare", "month": "2026-09"}))
	s := &scripted{turns: []llm.StreamChunk{bad, compare, breakdown, submit("c3", goodSummary, usdAI, inference)}}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.NoError(t, err)
	require.Equal(t, ExpectedDecision, out.Decision)
	require.Equal(t, 1, s.invalidSeen())
	require.Equal(t, 2, out.DataToolRuns, "参数组合错误不进入账本")
	require.Equal(t, [2]int{1, 0}, [2]int{out.Submissions, out.Repairs})
	require.Equal(t, int64(4), out.NewModelCalls)
}

// summary 编造数字、跨币种合计各交还一次；第三次提交通过。
func TestScriptedSummaryRepair(t *testing.T) {
	s := &scripted{turns: []llm.StreamChunk{compare, submit("c2", fabricated, usdAI), submit("c3", crossSum, usdAI), submit("c4", goodSummary, usdAI)}}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.NoError(t, err)
	require.Equal(t, ExpectedDecision, out.Decision)
	require.Equal(t, [2]int{3, 2}, [2]int{out.Submissions, out.Repairs})
	require.Equal(t, 2, s.invalidSeen())
	require.Contains(t, s.seen[1], "88.0%")
	require.Contains(t, s.seen[2], "不同币种不换汇")
	require.Equal(t, goodSummary, out.Accepted.Summary)
	require.Equal(t, int64(4), out.NewModelCalls)
}

// 修复次数用尽后中止，不发布未经核验的结论。
func TestScriptedRepairLimit(t *testing.T) {
	s := &scripted{turns: []llm.StreamChunk{compare, submit("c2", fabricated, usdAI), submit("c3", fabricated, usdAI), submit("c4", fabricated, usdAI), submit("c5", fabricated, usdAI)}}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.ErrorContains(t, err, "结论核验 3 次仍未通过")
	require.Nil(t, out.Accepted)
	require.Equal(t, [2]int{3, MaxRepairs}, [2]int{out.Submissions, out.Repairs})
	require.Equal(t, MaxRepairs, s.invalidSeen())
	require.Equal(t, int64(4), out.NewModelCalls)
}

// account_ids 含未授权账号：授权拒绝是致命错误，整个 Agent 中止，工具没有执行。
func TestScriptedUnauthorizedAborts(t *testing.T) {
	bad := turn(call("c1", "cost_query", map[string]any{"action": "compare", "month": "2026-09", "base_month": "2026-08", "account_ids": []string{"cn-prod", "us-legacy"}}))
	s := &scripted{turns: []llm.StreamChunk{bad, {Content: final}}}
	out, err := Run(context.Background(), s.client(), t.TempDir())
	require.ErrorContains(t, err, "无权访问账号 us-legacy")
	require.Zero(t, out.DataToolRuns)
	require.Equal(t, int64(1), out.NewModelCalls)
	require.Empty(t, s.seen, "模型没有收到任何工具结果")
}

func TestLiveCostAttribution(t *testing.T) {
	client := livemodeltest.Require(t)
	dir := t.TempDir()
	first, err := Run(context.Background(), client, dir)
	t.Logf("%s/%s：%+v", client.Live.Provider, client.Live.Model, first)
	require.NoError(t, err)
	require.Equal(t, ExpectedDecision, first.Decision, first.Answer)
	require.NotNil(t, first.Accepted, "必须提交并通过核验")
	require.Equal(t, [2]string{"USD", "ai"}, [2]string{first.Accepted.TopCurrency, first.Accepted.TopCategory})
	require.LessOrEqual(t, first.Repairs, MaxRepairs)
	require.GreaterOrEqual(t, first.DataToolRuns, 1)
	require.Equal(t, int(first.NewModelCalls), first.Budget.ReservedRequests)

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls)
	require.Equal(t, first.Answer, again.Answer)
	require.Equal(t, first.Budget, again.Budget)
}
