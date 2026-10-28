// 云费用环比归因：Agent 只能用只读的 cost_query 查询合成账本，最后用 submit_conclusion 提交结构化结论。
// 差值、百分比与排序都由确定性代码计算；不同币种不换汇、不相加；金额缺失不当 0、不参与排序；
// 数据起点之前标注 no_data。查询前按调用者绑定的账号授权，越权即中止。数据全部是合成的。
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/ops/internal/opscase"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Question 是交给 Agent 的问题。按合成数据应得 USD/ai：环比 +87.5%；CNY 最大涨幅是 compute 的 +8.1%。
const Question = "请对比 2026-09 与 2026-08 的云费用，找出环比涨幅（百分比）最大的币种与大类，并说明依据。"

const ExpectedDecision = "USD/ai"

// MaxRepairs 是结论核验失败后允许模型修复的次数。
const MaxRepairs = 2

var (
	months     = []string{"2026-07", "2026-08", "2026-09"}
	currencies = []string{"CNY", "USD"}
	categories = []string{"compute", "storage", "network", "database", "ai"}
)

// Granted 是宿主为调用者绑定的账号，不来自模型参数；us-legacy 存在但调用者无权访问。
var Granted = []string{"cn-prod", "us-ml"}

type account struct {
	Currency string
	Start    int // 数据起点在 months 中的下标，之前的月份没有账单
}

var accounts = map[string]account{"cn-prod": {"CNY", 0}, "us-ml": {"USD", 1}, "us-legacy": {"USD", 0}}

// null 表示账期内金额缺失（不是 0）。金额单位为分。
const null = -1

type line struct {
	Account, Category, Product string
	Cents                      [3]int64
}

// 起点之前的取值不会被读取，写 0 只为占位。
var bill = []line{
	{"cn-prod", "compute", "ecs", [3]int64{800000, 820000, 900000}},
	{"cn-prod", "compute", "k8s", [3]int64{400000, 410000, 430000}},
	{"cn-prod", "storage", "oss", [3]int64{300000, 310000, 320000}},
	{"cn-prod", "storage", "nas", [3]int64{100000, 100000, 100000}},
	{"cn-prod", "network", "cdn", [3]int64{250000, 260000, 250000}},
	{"cn-prod", "network", "eip", [3]int64{50000, 50000, 50000}},
	{"cn-prod", "database", "rds", [3]int64{500000, null, 600000}},
	{"cn-prod", "database", "redis", [3]int64{100000, 100000, 100000}},
	{"cn-prod", "ai", "llm-api", [3]int64{50000, 50000, 52000}},
	{"us-ml", "compute", "gpu-vm", [3]int64{0, 300000, 310000}},
	{"us-ml", "storage", "s3", [3]int64{0, 50000, 52000}},
	{"us-ml", "network", "egress", [3]int64{0, 20000, 21000}},
	{"us-ml", "database", "postgres", [3]int64{0, 30000, 30000}},
	{"us-ml", "ai", "inference", [3]int64{0, 40000, 75000}},
	{"us-ml", "ai", "training", [3]int64{0, 20000, 37500}},
	{"us-legacy", "network", "cdn", [3]int64{10000, 10000, 90000}},
	{"us-legacy", "compute", "vm", [3]int64{200000, 200000, 210000}},
}

type cell struct {
	cents   int64
	status  string // 空、no_data 或 incomplete
	missing []string
}

func (c *cell) show() string {
	if c.status != "" {
		return c.status
	}
	return money(c.cents)
}

// filter 是一次查询的范围；byProduct 为 true 时按产品而非大类分组。
type filter struct {
	accounts           map[string]bool
	currency, category string
	byProduct          bool
}

// aggregate 按币种、再按大类（或产品）汇总某月金额；只有同一币种的账号才会相加。
// 任一账号在该月处于数据起点之前，整格标注 no_data，其他账号的金额不能把它补成完整值。
func aggregate(m int, f filter) map[string]map[string]*cell {
	out := map[string]map[string]*cell{"CNY": {}, "USD": {}}
	for _, l := range bill {
		a := accounts[l.Account]
		if !f.accounts[l.Account] || (f.currency != "" && a.Currency != f.currency) || (f.category != "" && l.Category != f.category) {
			continue
		}
		key := l.Category
		if f.byProduct {
			key = l.Product
		}
		c := out[a.Currency][key]
		if c == nil {
			c = &cell{}
			out[a.Currency][key] = c
		}
		switch v := l.Cents[m]; {
		case m < a.Start:
			c.status = "no_data"
		case v == null:
			c.status, c.missing = cmp.Or(c.status, "incomplete"), append(c.missing, l.Product)
		default:
			c.cents += v
		}
	}
	return out
}

func money(c int64) string {
	if c < 0 {
		return "-" + money(-c)
	}
	return fmt.Sprintf("%d.%02d", c/100, c%100)
}

// percent 返回 delta/base 的百分比，四舍五入到一位小数；它只用于展示，不参与金额计算。
func percent(delta, base int64) string {
	t := math.Round(float64(delta) * 1000 / float64(base))
	return fmt.Sprintf("%.1f%%", t/10+0) // +0 把 -0 规整为 0
}

// Item 是一行结果。ranked 中的项有差值与百分比；unranked 中的项缺金额或缺数据，不参与排序；
// items 是 totals/breakdown 的单月金额。
type Item struct {
	Key        string   `json:"key"`
	Amount     string   `json:"amount,omitempty"`
	Base       string   `json:"base,omitempty"`
	Current    string   `json:"current,omitempty"`
	Delta      string   `json:"delta,omitempty"`
	Pct        string   `json:"pct,omitempty"`
	Missing    []string `json:"missing,omitempty"`
	Note       string   `json:"note,omitempty"`
	DeltaCents int64    `json:"-"`
	BaseCents  int64    `json:"-"`
}

// Group 是一个币种的结果；不同币种分开列出，从不相加。
type Group struct {
	Currency string `json:"currency"`
	Ranked   []Item `json:"ranked,omitempty"`
	Unranked []Item `json:"unranked,omitempty"`
	Items    []Item `json:"items,omitempty"`
}

// Compare 计算 month 相对 base 的环比：每个币种内按差值降序；缺金额或缺数据的项单独列出。
// base 为 -1 时只列出 month 的金额。
func Compare(month, base int, f filter) []Group {
	cur, prev := aggregate(month, f), aggregate(max(base, 0), f)
	var groups []Group
	for _, ccy := range currencies {
		g := Group{Currency: ccy}
		for _, key := range slices.Sorted(maps.Keys(cur[ccy])) {
			c, b := cur[ccy][key], prev[ccy][key]
			it := Item{Key: key, Base: b.show(), Current: c.show(), Missing: slices.Concat(b.missing, c.missing)}
			switch {
			case base < 0:
				g.Items = append(g.Items, Item{Key: key, Amount: c.show(), Missing: c.missing})
			case b.status == "no_data" || c.status == "no_data":
				it.Note = "数据起点之前没有账单，不计算增长，不参与排序"
				g.Unranked = append(g.Unranked, it)
			case b.status != "" || c.status != "":
				it.Note = "金额不完整（缺失不等于 0），不参与排序"
				g.Unranked = append(g.Unranked, it)
			case b.cents == 0:
				it.Delta, it.Note = money(c.cents), "基数为 0，不计算百分比，不参与排序"
				g.Unranked = append(g.Unranked, it)
			default:
				it.DeltaCents, it.BaseCents = c.cents-b.cents, b.cents
				it.Delta, it.Pct = money(it.DeltaCents), percent(it.DeltaCents, b.cents)
				g.Ranked = append(g.Ranked, it)
			}
		}
		slices.SortStableFunc(g.Ranked, func(x, y Item) int { return cmp.Compare(y.DeltaCents, x.DeltaCents) })
		if len(cur[ccy]) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

func argErr(format string, a ...any) error {
	return &tools.ArgumentError{Message: fmt.Sprintf(format, a...)}
}

// CostQuery 是 cost_query 的实现。参数已通过 Schema；这里只校验参数组合与月份范围。
func CostQuery(_ context.Context, a map[string]any) (any, error) {
	str := func(k string) string { s, _ := a[k].(string); return s }
	action, base := str("action"), str("base_month")
	f := filter{accounts: map[string]bool{}, currency: str("currency"), category: str("category"), byProduct: str("dimension") == "product"}
	ids, _ := a["account_ids"].([]any)
	for _, id := range ids {
		f.accounts[fmt.Sprint(id)] = true
	}
	if len(ids) == 0 {
		for _, id := range Granted {
			f.accounts[id] = true
		}
	}
	m, b := slices.Index(months, str("month")), slices.Index(months, base)
	switch {
	case m < 0:
		return nil, argErr("month 只能是 %s", strings.Join(months, "、"))
	case action == "compare" && (b < 0 || b >= m):
		return nil, argErr("compare 需要 base_month，且必须是早于 month 的 %s 之一", strings.Join(months, "、"))
	case action != "compare" && base != "":
		return nil, argErr("base_month 只用于 action=compare")
	case action == "breakdown" && f.category == "":
		return nil, argErr("breakdown 需要 category")
	case f.byProduct && (action != "compare" || f.category == ""):
		return nil, argErr("dimension=product 只用于 compare，且需要同时给出 category")
	}
	out := map[string]any{"action": action, "month": months[m]}
	switch action {
	case "compare":
		out["base_month"], out["currencies"] = base, Compare(m, b, f)
		out["note"] = "ranked 在每个币种内按 delta 降序；unranked 不参与排序；不同币种不可相加"
	case "breakdown":
		f.byProduct = true
		out["category"], out["currencies"] = f.category, Compare(m, -1, f)
	default:
		out["currencies"] = Compare(m, -1, f)
	}
	return out, nil
}

// authorize 在工具执行前核对 account_ids（只有 cost_query 带这个参数）；拒绝是致命错误，整个 Agent 中止。
func authorize(_ context.Context, _ string, in *interfaces.ToolInput) error {
	ids, _ := in.Args["account_ids"].([]any)
	for _, id := range ids {
		if !slices.Contains(Granted, fmt.Sprint(id)) {
			return fmt.Errorf("调用者无权访问账号 %v", id)
		}
	}
	return nil
}

func costTool() interfaces.Tool {
	month := `{"type":"string","pattern":"^[0-9]{4}-(0[1-9]|1[0-2])$"}`
	schema := `{"type":"object","properties":{
"action":{"type":"string","enum":["totals","compare","breakdown"]},"month":` + month + `,"base_month":` + month + `,
"dimension":{"type":"string","enum":["category","product"]},"category":{"type":"string","enum":` + opscase.Enum(categories) + `},
"currency":{"type":"string","enum":` + opscase.Enum(currencies) + `},
"account_ids":{"type":"array","items":{"type":"string","pattern":"^[a-z]+-[a-z]+$"},"minItems":1,"maxItems":3,"uniqueItems":true}},
"required":["action","month"],"additionalProperties":false}`
	return opscase.Tool("cost_query", "只读查询云费用账单（2026-07 至 2026-09）。totals：某月各大类金额；compare：month 相对 base_month 的环比（dimension=product 时需 category）；breakdown：某月某大类下各产品金额。不传 account_ids 时查询全部已授权账号。", schema, CostQuery)
}

var (
	// clock 与 date 是时间与日期（10:05、2026-09），先整段剔除，其中的数字不是金额。
	clock = regexp.MustCompile(`\b\d{1,2}:\d{2}(?::\d{2})?\b`)
	date  = regexp.MustCompile(`\b\d{4}-\d{2}(?:-\d{2})?\b`)
	// number 识别前置币种的数、百分比、带小数的数，以及后面紧跟币种单位的整数金额；年份、月份等裸整数不算。
	number   = regexp.MustCompile(`(?:USD|CNY|US\$|\$|¥|￥)\s*(-?\d[\d,]*(?:\.\d+)?%?)|(-?\d[\d,]*(?:\.\d+)?%|-?\d[\d,]*\.\d+)|(-?\d[\d,]*)\s*(?:元|美元|CNY|USD)`)
	calendar = regexp.MustCompile(`^\s*[年月日号]`)
	falling  = regexp.MustCompile(`下降|减少|降低|回落|下滑|下跌`)
	sentence = regexp.MustCompile(`[。；;！!？?\n]`)
)

// CheckSummary 是示例层核验：summary 中的金额与百分比去掉千分位逗号后，必须与某个工具结果的字段值完全相等；
// 省略负号引用负值（“下降 100.00”对应 -100.00）时，同一句里必须有下降、减少之类的方向词。
// 不得把 CNY 与 USD 合计。values 是账本登记的全部标量值。
func CheckSummary(summary string, values []string) []string {
	var problems []string
	stripped := date.ReplaceAllString(clock.ReplaceAllString(summary, " "), " ")
	for _, s := range sentence.Split(stripped, -1) {
		for _, idx := range number.FindAllStringSubmatchIndex(s, -1) {
			var n string
			for g := 1; g <= 3; g++ {
				if idx[2*g] >= 0 {
					n = strings.ReplaceAll(s[idx[2*g]:idx[2*g+1]], ",", "")
				}
			}
			if idx[2] >= 0 && calendar.MatchString(s[idx[1]:]) {
				continue // “USD 9 月”里的 9 是月份
			}
			quoted := strings.TrimSpace(s[idx[0]:idx[1]])
			switch {
			case slices.Contains(values, n):
			case !strings.HasPrefix(n, "-") && slices.Contains(values, "-"+n):
				if !falling.MatchString(s) {
					problems = append(problems, fmt.Sprintf("summary 中的 %s 只对应工具结果中的 -%s，省略负号会反转方向：写成 -%s，或在同一句注明下降、减少", quoted, n, n))
				}
			default:
				problems = append(problems, fmt.Sprintf("summary 中的 %s 不在任何工具结果中，金额与百分比只能原样引用工具结果的字段值", quoted))
			}
		}
		if strings.Contains(s, "CNY") && strings.Contains(s, "USD") && (strings.Contains(s, "合计") || strings.Contains(s, "总计")) {
			problems = append(problems, "summary 把 CNY 与 USD 合计，不同币种不换汇、不相加")
		}
	}
	return problems
}

// CheckTop 用确定性代码复算 Question 的答案：在已授权账号中，2026-09 相对 2026-08 环比涨幅最大的币种与大类。
// 跨币种只比较 pct，不比较金额；unranked 的项不参与。结论与之不符就交还模型。
func CheckTop(c Conclusion) []string {
	f := filter{accounts: map[string]bool{}}
	for _, id := range Granted {
		f.accounts[id] = true
	}
	type top struct{ ccy, key, pct string }
	var best []top
	var bestItem Item
	for _, g := range Compare(slices.Index(months, "2026-09"), slices.Index(months, "2026-08"), f) {
		for _, it := range g.Ranked {
			// 比较 delta/base：交叉相乘避免浮点误差，base 恒为正。
			switch d := cmp.Compare(it.DeltaCents*bestItem.BaseCents, bestItem.DeltaCents*it.BaseCents); {
			case best == nil || d > 0:
				best, bestItem = []top{{g.Currency, it.Key, it.Pct}}, it
			case d == 0:
				best = append(best, top{g.Currency, it.Key, it.Pct})
			}
		}
	}
	var want []string
	for _, b := range best {
		if b.ccy == c.TopCurrency && b.key == c.TopCategory {
			return nil
		}
		want = append(want, fmt.Sprintf("%s/%s（%s）", b.ccy, b.key, b.pct))
	}
	return []string{fmt.Sprintf("top_currency/top_category 为 %s/%s，但按 cost_query 的 ranked 计算，环比涨幅最大的是 %s；跨币种只比较 pct，不比较金额", c.TopCurrency, c.TopCategory, strings.Join(want, "、"))}
}

// Conclusion 是被接受的结构化结论。
type Conclusion struct {
	TopCurrency string             `json:"top_currency"`
	TopCategory string             `json:"top_category"`
	Summary     string             `json:"summary"`
	Evidence    []opscase.Citation `json:"evidence"`
}

var conclusionSchema = `{"type":"object","properties":{
"top_currency":{"type":"string","enum":` + opscase.Enum(currencies) + `},"top_category":{"type":"string","enum":` + opscase.Enum(categories) + `},
"summary":{"type":"string","minLength":1},` + opscase.EvidenceSchema + `},
"required":["top_currency","top_category","summary","evidence"],"additionalProperties":false}`

const systemPrompt = `你是云费用分析助手，只能依据 cost_query 的结果作答，不要自己计算金额、差值或百分比。
规则：不同币种不换汇、不相加、不比较金额，跨币种只比较 pct；unranked 中的项（金额不完整或无数据）不参与排序，缺失不等于 0；no_data 表示数据起点之前没有账单。
调查完必须调用 submit_conclusion：summary 中出现的金额与百分比必须原样来自工具结果；evidence 每条填写结果里的 ref，以及从该结果原样复制的片段。
若工具返回 invalid_arguments，按 message 修正后重试；submit_conclusion 被接受即结束分析。`

// Result 汇总回答、结论与计数。
type Result struct {
	Answer        string
	Decision      string
	Accepted      *Conclusion
	Submissions   int
	Repairs       int
	Rejections    []string // 每次核验不通过的原因
	DataToolRuns  int      // 本进程实际执行的 cost_query 次数，回放不计
	NewModelCalls int64
	Budget        budget.Snapshot
}

// Run 回答 Question。dir 保存模型、工具日志与根预算；同一目录再次运行只回放。
func Run(ctx context.Context, client *livemodel.Client, dir string) (Result, error) {
	var out Result
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	controller, err := budget.NewDurable(budget.Config{RootKey: "cost/2026-09-mom", Version: "v2/" + client.Live.Provider + "/" + client.Live.Model, MaxModelRequests: 12}, store)
	if err != nil {
		return out, err
	}
	ctx = budget.WithController(durable.WithJournal(durable.WithExecutionKey(ctx, "cost/2026-09-mom/agent"), store), controller)
	ledger := opscase.NewLedger()
	sub := &opscase.Submitter[Conclusion]{Ledger: ledger, MaxRepairs: MaxRepairs, Answer: answer,
		Evidence: func(c Conclusion) []opscase.Citation { return c.Evidence },
		Check:    func(c Conclusion) []string { return append(CheckTop(c), CheckSummary(c.Summary, ledger.Values())...) }}
	submit := sub.Tool("提交归因结论；证据引用工具结果的 ref 并原样摘录片段", conclusionSchema)
	dispatched, err := opscase.Dispatch([]interfaces.Tool{ledger.Wrap(costTool()), submit}, tools.DispatchConfig{Authorize: authorize, OnResult: sub.OnResult})
	if err != nil {
		return out, err
	}
	agent, err := builder.NewSimpleBuilder(client).WithDurableScope("cost").WithSystemPrompt(systemPrompt).
		WithTools(dispatched...).WithReturnDirectTools(opscase.SubmitTool).WithEmptyAnswerRetries(1).WithMaxIterations(9).WithMaxTokens(livemodel.MaxTokens).WithTimeout(10 * livemodel.RequestTimeout).Build()
	if err != nil {
		return out, err
	}
	before := client.Calls()
	final, err := agent.AsRunnable().Invoke(ctx, Question)
	out.NewModelCalls = client.Calls() - before
	out.Budget, _ = controller.Snapshot(ctx)
	_, out.Submissions, out.Repairs = sub.Stats()
	out.Rejections = sub.Rejections()
	out.DataToolRuns = ledger.Runs()
	if err != nil {
		return out, err
	}
	out.Answer = fmt.Sprint(final.Result)
	// 只发布通过核验的结论；首次运行与回放都由提交调用恢复。
	if out.Accepted, err = sub.Accepted(); err != nil {
		return out, err
	}
	out.Decision = out.Accepted.TopCurrency + "/" + out.Accepted.TopCategory
	return out, nil
}

// answer 是结论通过核验后直接交给调用方的最终回答。
func answer(c Conclusion) string {
	return "结论：" + c.TopCurrency + "/" + c.TopCategory + "\n" + c.Summary
}

func main() {
	dir := flag.String("dir", "", "持久化目录；同一目录再次运行只回放")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "用法：cost-attribution -dir <目录>")
		os.Exit(2)
	}
	client, err := livemodel.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := Run(context.Background(), client, *dir)
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	fmt.Println("期望结论：" + ExpectedDecision)
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
