// 受限的子调查：主调查 Agent 用 delegate_lookup 按方向（metrics/logs/traces）派出子调查。
// 每个子调查是独立会话、独立 Dispatcher，只拿到该方向的一个数据工具，最多 4 步，不能再委派；
// 主 Agent 只看到子调查的摘要与引用号。数据工具共用一个证据账本，主 Agent 的结论可以引用子调查内部的 ref，
// 宿主按账本核对，不通过就交还模型修复，次数有上限；通过核验的提交直接给出最终回答。
// 同一轮的多个子调查并发执行。数据全部是合成的。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase"
	"github.com/kart-io/goagent/examples/orchestration/ops/internal/opscase"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Alert 是交给主调查 Agent 的告警。合成场景的真实原因是 inventory-svc 的 reserve 操作变慢。
const Alert = "告警：order-svc 下单接口 p99 延迟 14:05 起从 180ms 升至约 1.2s，持续 15 分钟。请定位拖慢下单的下游服务及其操作。"

// Downstreams 与 Operations 是 submit_conclusion 允许的取值；Expected* 是按合成数据应得的结论。
var (
	Downstreams = []string{"inventory-svc", "payment-svc", "pricing-svc"}
	Operations  = map[string][]string{"inventory-svc": {"reserve", "query_stock"}, "payment-svc": {"authorize", "capture"}, "pricing-svc": {"quote"}}
	allOps      = []string{"reserve", "query_stock", "authorize", "capture", "quote"}
)

// MaxRepairs 是结论核验失败后允许模型修复的次数；MaxParallel 是主 Dispatcher 同时执行的子调查上限。
const (
	ExpectedDownstream, ExpectedOperation = "inventory-svc", "reserve"
	MaxRepairs, MaxParallel               = 2, 3
)

// focusTool 是每个方向唯一被授予的数据工具；focuses 是方向的固定排列，Result.Lookups 按它排序。
var (
	focusTool = map[string]string{"metrics": "metrics_query", "logs": "logs_query", "traces": "traces_query"}
	focuses   = []string{"metrics", "logs", "traces"}
)

// 以下均为 order-svc 视角下对各下游调用的观测，时间窗 14:00–14:20。
var metrics = map[string]map[string][]string{
	"inventory-svc": {"latency_p99": {"14:00 45ms", "14:10 820ms", "14:20 870ms"}, "error_rate": {"14:00 0.1%", "14:10 2.4%", "14:20 2.6%"}, "call_qps": {"14:00 310", "14:10 305", "14:20 312"}},
	"payment-svc":   {"latency_p99": {"14:00 110ms", "14:10 128ms", "14:20 125ms"}, "error_rate": {"14:00 0.2%", "14:10 0.2%", "14:20 0.3%"}, "call_qps": {"14:00 300", "14:10 298", "14:20 301"}},
	"pricing-svc":   {"latency_p99": {"14:00 22ms", "14:10 23ms", "14:20 22ms"}, "error_rate": {"14:00 0.0%", "14:10 0.0%", "14:20 0.0%"}, "call_qps": {"14:00 640", "14:10 636", "14:20 641"}},
}

var logs = map[string]map[string][]string{
	"inventory-svc": {
		"WARN":  {"14:06:12 WARN call inventory-svc.reserve timeout after 500ms, retry 1/2", "14:08:47 WARN call inventory-svc.reserve timeout after 500ms, retry 2/2"},
		"ERROR": {"14:09:40 ERROR call inventory-svc.reserve failed after 2 retries: deadline exceeded"},
	},
	"payment-svc": {"WARN": {"14:03:05 WARN call payment-svc.authorize slow 128ms (threshold 120ms)"}, "ERROR": {}},
	"pricing-svc": {"WARN": {}, "ERROR": {}},
}

var traces = map[string]map[string]string{
	"inventory-svc": {"reserve": "p99 810ms; in 92% of slow order traces; child span db.lock_wait p99 760ms", "query_stock": "p99 31ms; in 1% of slow order traces"},
	"payment-svc":   {"authorize": "p99 126ms; in 5% of slow order traces", "capture": "p99 40ms; in 0% of slow order traces"},
	"pricing-svc":   {"quote": "p99 21ms; in 2% of slow order traces"},
}

const parentPrompt = `你是值班 SRE 主调查员。你没有数据工具，只能用 delegate_lookup 派子调查：focus 取 metrics、logs、traces 之一，每个方向至多一次，尽量在同一轮同时派出多个方向。
子调查返回摘要和经宿主核对过的证据列表（ref 与 excerpt）。调查完必须调用 submit_conclusion：downstream、operation 取枚举值；evidence 从子调查返回的证据中原样复制 ref 与 excerpt，不要改写、翻译或拼接。
operation 必须是该 downstream 的操作。若工具返回 invalid_arguments，按 message 修正后重试；submit_conclusion 被接受即结束调查。`

const childPrompt = `你是只负责 %s 方向的子调查员，只能使用 %s。围绕问题查询 order-svc 调用的下游（%s），最多查询 2 次。
然后回答：第一行用一句中文写结论；之后每条证据单独一行，格式为“证据：<ref>｜<从该结果中原样复制的片段>”，1–3 条。不要推测其他方向。若工具返回 invalid_arguments，按 message 修正。`

// Conclusion 是被接受的结构化结论。
type Conclusion struct {
	Downstream string             `json:"downstream"`
	Operation  string             `json:"operation"`
	Summary    string             `json:"summary"`
	Evidence   []opscase.Citation `json:"evidence"`
}

// Lookup 记录一个子会话：方向、实际调用的工具与产生的引用号。
type Lookup struct {
	Focus       string
	Calls, Refs []string
}

type lookupInput struct{ Focus, Question string }

// Report 是子调查交给主 Agent 的全部内容：摘要、本会话产生的引用号，以及宿主核对通过的证据。
// 主 Agent 看不到原始结果，只能从 Evidence 里照抄摘录。
type Report struct {
	Focus    string             `json:"focus"`
	Summary  string             `json:"summary"`
	Refs     []string           `json:"refs"`
	Evidence []opscase.Citation `json:"evidence"`
}

var evidenceLine = regexp.MustCompile(`^证据[:：]\s*(E[0-9a-f]+)\s*[|｜]\s*(.+)$`)

// report 从子调查回答中拆出证据行，只保留本会话产生且摘录核对通过的条目；其余行作为摘要。
func (l *lookups) report(focus, answer string, refs []string) Report {
	r := Report{Focus: focus, Refs: refs, Evidence: []opscase.Citation{}}
	var summary []string
	for _, line := range strings.Split(answer, "\n") {
		m := evidenceLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			summary = append(summary, line)
			continue
		}
		c := opscase.Citation{Ref: m[1], Excerpt: strings.Trim(strings.TrimSpace(m[2]), "“”\"`")}
		if slices.Contains(refs, c.Ref) && l.ledger.Verify([]opscase.Citation{c}) == nil {
			r.Evidence = append(r.Evidence, c)
		}
	}
	r.Summary = strings.TrimSpace(strings.Join(summary, "\n"))
	return r
}

// Result 汇总回答、结论与各层计数。
type Result struct {
	Answer, Decision                         string
	Accepted                                 *Conclusion
	Submissions, Repairs, DataToolRuns       int      // DataToolRuns 只计本进程实际执行的数据工具，回放不计
	Rejections                               []string // 每次核验不通过的原因
	Lookups                                  []Lookup // 按 metrics、logs、traces 排列，与并发完成顺序无关
	LookupsCreated, LookupsDone, PeakLookups int
	NewModelCalls                            int64
	Budget                                   budget.Snapshot
}

// options 只供测试注入异常配置。
type options struct {
	nest        bool                       // 给子 Agent 也挂上 delegate_lookup，用来演示嵌套被拒
	strictChild bool                       // 子 Dispatcher 不交还参数错误，用来演示内层参数错误不交给主模型
	onResult    func(tools.DispatchResult) // 主 Dispatcher 每次得到结果后调用，用来模拟崩溃
}

func dataTool(name string) interfaces.Tool {
	ds := `"downstream":{"type":"string","enum":` + opscase.Enum(Downstreams) + `}`
	switch name {
	case "metrics_query":
		return opscase.Tool(name, "查询 order-svc 调用某下游的指标（14:00–14:20）",
			`{"type":"object","properties":{`+ds+`,"metric":{"type":"string","enum":["latency_p99","error_rate","call_qps"]}},"required":["downstream","metric"],"additionalProperties":false}`,
			func(_ context.Context, a map[string]any) (any, error) {
				return map[string]any{"downstream": a["downstream"], "metric": a["metric"], "points": metrics[a["downstream"].(string)][a["metric"].(string)]}, nil
			})
	case "logs_query":
		return opscase.Tool(name, "查询 order-svc 调用某下游的日志（14:00–14:20）",
			`{"type":"object","properties":{`+ds+`,"level":{"type":"string","enum":["ERROR","WARN"]}},"required":["downstream","level"],"additionalProperties":false}`,
			func(_ context.Context, a map[string]any) (any, error) {
				return map[string]any{"downstream": a["downstream"], "level": a["level"], "lines": logs[a["downstream"].(string)][a["level"].(string)]}, nil
			})
	}
	return opscase.Tool("traces_query", "查询慢下单链路中某下游操作的 span 统计（14:00–14:20）",
		`{"type":"object","properties":{`+ds+`,"operation":{"type":"string","enum":`+opscase.Enum(allOps)+`}},"required":["downstream","operation"],"additionalProperties":false}`,
		func(_ context.Context, a map[string]any) (any, error) {
			d, op := a["downstream"].(string), a["operation"].(string)
			stats, ok := traces[d][op]
			if !ok {
				return nil, &tools.ArgumentError{Message: fmt.Sprintf("%s 没有操作 %s，可选：%s", d, op, strings.Join(Operations[d], ","))}
			}
			return map[string]any{"downstream": d, "operation": op, "spans": stats}, nil
		})
}

// lookups 管理子会话：创建与清理计数、并发峰值、每个会话的工具调用记录。
type lookups struct {
	client                        *livemodel.Client
	ledger                        *opscase.Ledger
	opt                           options
	delegate                      interfaces.Tool
	mu                            sync.Mutex
	all                           []*Lookup
	created, closed, active, peak int
}

func (l *lookups) open() *Lookup {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := &Lookup{}
	l.all = append(l.all, s)
	l.created++
	l.active++
	l.peak = max(l.peak, l.active)
	return s
}

func (l *lookups) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed++
	l.active--
}

// record 记录子会话实际调用的工具名，以及账本为成功结果分配的 ref。
func (l *lookups) record(s *Lookup, t interfaces.Tool) interfaces.Tool {
	return tools.NewBaseTool(t.Name(), t.Description(), t.ArgsSchema(), func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		l.mu.Lock()
		s.Calls = append(s.Calls, t.Name())
		l.mu.Unlock()
		out, err := t.Invoke(ctx, in)
		if err != nil || out == nil {
			return out, err
		}
		if text, ok := out.Result.(string); ok {
			var head struct{ Ref string }
			_ = json.Unmarshal([]byte(text), &head)
			l.mu.Lock()
			s.Refs = append(s.Refs, head.Ref)
			l.mu.Unlock()
		}
		return out, err
	})
}

// investigate 在子会话里按 focus 收窄工具集并运行子 Agent。factory 拿不到解码后的参数，所以收窄放在这里。
func (l *lookups) investigate(ctx context.Context, s *Lookup, in lookupInput) (Report, error) {
	l.mu.Lock()
	s.Focus = in.Focus
	l.mu.Unlock()
	name := focusTool[in.Focus]
	scoped := []interfaces.Tool{l.record(s, l.ledger.Wrap(dataTool(name)))}
	if l.opt.nest {
		scoped = append(scoped, l.delegate)
	}
	dispatch := func(ts []interfaces.Tool) ([]interfaces.Tool, error) {
		return opscase.Dispatch(ts, tools.DispatchConfig{OnResult: l.ledger.OnResult})
	}
	if l.opt.strictChild {
		// 不登记账本：这一模式只用来演示整条链中止。
		dispatch = livecase.DispatchTools
	}
	scoped, err := dispatch(scoped)
	if err != nil {
		return Report{}, err
	}
	agent, err := builder.NewSimpleBuilder(l.client).WithDurableScope("lookup").WithSessionID("lookup/" + in.Focus + "/" + uuid.NewString()).
		WithSystemPrompt(fmt.Sprintf(childPrompt, in.Focus, name, strings.Join(Downstreams, "、"))).
		WithTools(scoped...).WithEmptyAnswerRetries(1).WithMaxIterations(4).WithMaxTokens(livemodel.MaxTokens).WithTimeout(5 * livemodel.RequestTimeout).Build()
	if err != nil {
		return Report{}, err
	}
	out, err := agent.AsRunnable().Invoke(ctx, in.Question)
	if err != nil {
		return Report{}, err
	}
	l.mu.Lock()
	refs := append([]string{}, s.Refs...)
	l.mu.Unlock()
	return l.report(in.Focus, fmt.Sprint(out.Result), refs), nil
}

// delegateTool 每次调用新建一个子会话；MaxDepth=1 使子会话内的任何委派在执行前被拒。
// 子调查整体运行在主 Dispatcher 的一次调用里，Timeout 要覆盖子 Agent 的全部模型请求。
func (l *lookups) delegateTool() (interfaces.Tool, error) {
	return tools.NewRunnableToolFactory(func(context.Context) (tools.OwnedRunnable[lookupInput, Report], error) {
		s := l.open()
		run := core.NewRunnableFunc(func(ctx context.Context, in lookupInput) (Report, error) { return l.investigate(ctx, s, in) })
		return tools.OwnedRunnable[lookupInput, Report]{Runnable: run, Close: func(context.Context) error { l.close(); return nil }}, nil
	}, tools.RunnableToolConfig[lookupInput, Report]{
		Name: "delegate_lookup", Description: "派出一个只看单一方向（metrics/logs/traces）的子调查，返回摘要与核对过的证据", Version: "v1",
		ModelInvoking: true, MaxDepth: 1, CleanupTimeout: 10 * time.Second, Timeout: 5 * livemodel.RequestTimeout,
		ArgsSchema: `{"type":"object","properties":{"focus":{"type":"string","enum":["metrics","logs","traces"]},"question":{"type":"string","minLength":1}},"required":["focus","question"],"additionalProperties":false}`,
		Decode: func(a map[string]any) (lookupInput, error) {
			return lookupInput{Focus: a["focus"].(string), Question: a["question"].(string)}, nil
		},
		Encode: func(r Report) (any, error) { return r, nil },
	})
}

var conclusionSchema = `{"type":"object","properties":{
"downstream":{"type":"string","enum":` + opscase.Enum(Downstreams) + `},
"operation":{"type":"string","enum":` + opscase.Enum(allOps) + `},
"summary":{"type":"string","minLength":1},` + opscase.EvidenceSchema + `},
"required":["downstream","operation","summary","evidence"],"additionalProperties":false}`

// checkPair 拒绝 operation 与 downstream 不配套的结论，例如 inventory-svc/authorize。
func checkPair(c Conclusion) []string {
	if ops := Operations[c.Downstream]; !slices.Contains(ops, c.Operation) {
		return []string{fmt.Sprintf("operation %s 不是 %s 的操作，可选：%s", c.Operation, c.Downstream, strings.Join(ops, ","))}
	}
	return nil
}

// answer 是结论通过核验后直接交给调用方的最终回答。
func answer(c Conclusion) string {
	return "结论：" + c.Downstream + "\n操作：" + c.Operation + "\n" + c.Summary
}

// restoreEvidence 把子调查报告中核对过的摘录补登到账本。首次执行时账本已有完整结果，这里不改动；
// 崩溃恢复后 delegate_lookup 从主日志回放、子会话不会重跑，账本靠这些摘录核对主 Agent 之后的提交。
func restoreEvidence(ledger *opscase.Ledger, r tools.DispatchResult) error {
	if r.Name != "delegate_lookup" || r.Output == nil || !r.Output.Success {
		return nil
	}
	var rep Report
	data, err := json.Marshal(r.Output.Result)
	if err == nil {
		err = json.Unmarshal(data, &rep)
	}
	if err != nil {
		return fmt.Errorf("解析子调查报告失败：%w", err)
	}
	byRef := map[string][]string{}
	for _, c := range rep.Evidence {
		byRef[c.Ref] = append(byRef[c.Ref], c.Excerpt)
	}
	for ref, excerpts := range byRef {
		ledger.Restore(ref, excerpts)
	}
	return nil
}

func run(ctx context.Context, client *livemodel.Client, dir string, opt options) (Result, error) {
	var out Result
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	controller, err := budget.NewDurable(budget.Config{RootKey: "alert/order-p99-1405", Version: "v2/" + client.Live.Provider + "/" + client.Live.Model, MaxModelRequests: 30}, store)
	if err != nil {
		return out, err
	}
	ctx = budget.WithController(durable.WithJournal(durable.WithExecutionKey(ctx, "alert/order-p99-1405/agent"), store), controller)
	ledger := opscase.NewLedger()
	l := &lookups{client: client, ledger: ledger, opt: opt}
	if l.delegate, err = l.delegateTool(); err != nil {
		return out, err
	}
	sub := &opscase.Submitter[Conclusion]{Ledger: ledger, MaxRepairs: MaxRepairs, Check: checkPair, Answer: answer,
		Evidence: func(c Conclusion) []opscase.Citation { return c.Evidence }}
	submitTool := sub.Tool("提交结论；每条证据引用子调查给出的 ref 并原样摘录该结果中的片段", conclusionSchema)
	onResult := func(ctx context.Context, r tools.DispatchResult) error {
		if err := restoreEvidence(ledger, r); err != nil {
			return err
		}
		if err := sub.OnResult(ctx, r); err != nil {
			return err
		}
		if opt.onResult != nil {
			opt.onResult(r)
		}
		return nil
	}
	dispatched, err := opscase.Dispatch([]interfaces.Tool{l.delegate, submitTool}, tools.DispatchConfig{MaxConcurrency: MaxParallel, OnResult: onResult})
	if err != nil {
		return out, err
	}
	agent, err := builder.NewSimpleBuilder(client).WithDurableScope("lead").WithSystemPrompt(parentPrompt).
		WithTools(dispatched...).WithReturnDirectTools(opscase.SubmitTool).WithEmptyAnswerRetries(1).WithMaxIterations(7).WithMaxTokens(livemodel.MaxTokens).WithTimeout(20 * livemodel.RequestTimeout).Build()
	if err != nil {
		return out, err
	}
	before := client.Calls()
	final, err := agent.AsRunnable().Invoke(ctx, Alert)
	out.NewModelCalls = client.Calls() - before
	out.Budget, _ = controller.Snapshot(ctx)
	_, out.Submissions, out.Repairs = sub.Stats()
	out.Rejections = sub.Rejections()
	l.mu.Lock()
	for _, s := range l.all {
		out.Lookups = append(out.Lookups, *s)
	}
	out.LookupsCreated, out.LookupsDone, out.PeakLookups = l.created, l.closed, l.peak
	l.mu.Unlock()
	slices.SortStableFunc(out.Lookups, func(a, b Lookup) int { return slices.Index(focuses, a.Focus) - slices.Index(focuses, b.Focus) })
	out.DataToolRuns = ledger.Runs()
	if err != nil {
		return out, err
	}
	out.Answer = fmt.Sprint(final.Result)
	// 只发布通过核验的结论；首次运行与回放都由提交调用恢复。
	if out.Accepted, err = sub.Accepted(); err != nil {
		return out, err
	}
	out.Decision = out.Accepted.Downstream
	return out, nil
}

// Run 以默认配置处理 Alert。dir 保存模型、工具日志与根预算；同一目录再次运行只回放。
func Run(ctx context.Context, client *livemodel.Client, dir string) (Result, error) {
	return run(ctx, client, dir, options{})
}

func main() {
	dir := flag.String("dir", "", "持久化目录；同一目录再次运行只回放")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "用法：scoped-lookup -dir <目录>")
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
	fmt.Println("期望结论：" + ExpectedDownstream + " / " + ExpectedOperation)
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
