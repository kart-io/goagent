// 告警根因：Agent 用指标、日志、变更三类只读工具调查，最后必须用 submit_conclusion 提交结构化结论。
// 每条证据引用某次工具结果的 ref 并原样摘录片段；宿主按账本核对，不通过就作为参数错误交还模型修复，
// 修复次数有上限，超过即中止。通过核验的提交直接给出最终回答并结束运行。数据全部是合成的。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/ops/internal/opscase"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Alert 是交给 Agent 的告警。合成场景的真实根因是 10:02 的连接池配置变更（config_change）。
const Alert = "告警：checkout-api 5xx 错误率 10:05 起从 0.2% 升至约 18%，持续 10 分钟。请排查根因。"

// RootCauses 是 submit_conclusion 允许的根因类别；ExpectedRootCause 是按合成数据应得的结论。
var RootCauses = []string{"config_change", "code_deploy", "traffic_spike", "dependency_outage"}

const ExpectedRootCause = "config_change"

// MaxRepairs 是结论核验失败后允许模型修复的次数。
const MaxRepairs = 2

var services = []string{"checkout-api", "order-db-proxy", "search-api"}

var metrics = map[string]map[string][]string{
	"checkout-api": {
		"error_rate":     {"10:00 0.2%", "10:05 17.8%", "10:10 18.1%"},
		"latency_p99":    {"10:00 120ms", "10:05 2900ms", "10:10 3010ms"},
		"qps":            {"10:00 820", "10:05 815", "10:10 825"},
		"db_pool_in_use": {"10:00 31/200", "10:05 50/50", "10:10 50/50"},
	},
	"order-db-proxy": {
		"error_rate":     {"10:00 0.0%", "10:05 0.0%", "10:10 0.0%"},
		"latency_p99":    {"10:00 8ms", "10:05 9ms", "10:10 8ms"},
		"qps":            {"10:00 2100", "10:05 1240", "10:10 1230"},
		"db_pool_in_use": {"10:00 n/a", "10:05 n/a", "10:10 n/a"},
	},
	"search-api": {
		"error_rate":     {"10:00 0.1%", "10:05 0.1%", "10:10 0.1%"},
		"latency_p99":    {"10:00 95ms", "10:05 97ms", "10:10 96ms"},
		"qps":            {"10:00 430", "10:05 428", "10:10 431"},
		"db_pool_in_use": {"10:00 12/100", "10:05 12/100", "10:10 13/100"},
	},
}

var logs = map[string]map[string][]string{
	"checkout-api": {
		"ERROR": {
			"10:05:12 ERROR acquire db conn timeout after 2s: pool exhausted (max_open=50 in_use=50)",
			"10:05:13 ERROR POST /checkout 503 upstream=order-db-proxy cause=pool exhausted",
			"10:07:40 ERROR acquire db conn timeout after 2s: pool exhausted (max_open=50 in_use=50)",
		},
		"WARN": {"10:04:58 WARN db pool wait queue length 120"},
	},
	"order-db-proxy": {"ERROR": {}, "WARN": {"10:05:30 WARN client checkout-api connections dropped from 180 to 50"}},
	"search-api":     {"ERROR": {}, "WARN": {"09:31:02 WARN cache warmup slow after deploy v2.14.0"}},
}

var changes = map[string][]string{
	"checkout-api":   {"10:02 config db.max_open_conns 200 -> 50 by deploy-bot ticket CFG-118"},
	"order-db-proxy": {},
	"search-api":     {"09:30 deploy v2.14.0 by release-pipeline"},
}

const systemPrompt = `你是值班 SRE 的根因分析助手，只能依据工具结果下结论。
先用 metrics_query、logs_query、change_query 调查 checkout-api 及其依赖 order-db-proxy；search-api 与告警无直接关系，可按需对照。
调查完必须调用 submit_conclusion：root_cause 取枚举值之一；evidence 每条填写工具结果里的 ref，以及从该结果中原样复制的片段（不要改写、翻译或拼接）。
summary 用一句话说明根因。若工具返回 invalid_arguments，按 message 修正后重试；submit_conclusion 被接受即结束调查。`

// Conclusion 是被接受的结构化结论。
type Conclusion struct {
	RootCause string             `json:"root_cause"`
	Summary   string             `json:"summary"`
	Evidence  []opscase.Citation `json:"evidence"`
}

var conclusionSchema = `{"type":"object","properties":{
"root_cause":{"type":"string","enum":` + opscase.Enum(RootCauses) + `},
"summary":{"type":"string","minLength":1},` + opscase.EvidenceSchema + `},
"required":["root_cause","summary","evidence"],"additionalProperties":false}`

// Result 汇总回答、结论与计数。
type Result struct {
	Answer        string
	Decision      string
	Accepted      *Conclusion
	Submissions   int
	Repairs       int
	Rejections    []string // 每次核验不通过的原因
	DataToolRuns  int      // 本进程实际执行的数据工具次数，回放不计
	NewModelCalls int64
	Budget        budget.Snapshot
}

func dataTools() []interfaces.Tool {
	svc := `"service":{"type":"string","enum":` + opscase.Enum(services) + `}`
	return []interfaces.Tool{
		opscase.Tool("metrics_query", "查询服务指标在 10:00–10:10 的取值",
			`{"type":"object","properties":{`+svc+`,"metric":{"type":"string","enum":["error_rate","latency_p99","qps","db_pool_in_use"]}},"required":["service","metric"],"additionalProperties":false}`,
			func(_ context.Context, a map[string]any) (any, error) {
				return map[string]any{"service": a["service"], "metric": a["metric"], "points": metrics[a["service"].(string)][a["metric"].(string)]}, nil
			}),
		opscase.Tool("logs_query", "查询服务在 09:00–10:30 的日志",
			`{"type":"object","properties":{`+svc+`,"level":{"type":"string","enum":["ERROR","WARN"]}},"required":["service","level"],"additionalProperties":false}`,
			func(_ context.Context, a map[string]any) (any, error) {
				return map[string]any{"service": a["service"], "level": a["level"], "lines": logs[a["service"].(string)][a["level"].(string)]}, nil
			}),
		opscase.Tool("change_query", "查询服务在 09:00–10:30 的发布与配置变更",
			`{"type":"object","properties":{`+svc+`},"required":["service"],"additionalProperties":false}`,
			func(_ context.Context, a map[string]any) (any, error) {
				return map[string]any{"service": a["service"], "changes": changes[a["service"].(string)]}, nil
			}),
	}
}

// Run 处理 Alert。dir 保存模型、工具日志与根预算；同一目录再次运行只回放。
func Run(ctx context.Context, client *livemodel.Client, dir string) (Result, error) {
	var out Result
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	controller, err := budget.NewDurable(budget.Config{RootKey: "alert/5xx-1005", Version: "v2/" + client.Live.Provider + "/" + client.Live.Model, MaxModelRequests: 12}, store)
	if err != nil {
		return out, err
	}
	ctx = budget.WithController(durable.WithJournal(durable.WithExecutionKey(ctx, "alert/5xx-1005/agent"), store), controller)
	ledger := opscase.NewLedger()
	sub := &opscase.Submitter[Conclusion]{Ledger: ledger, MaxRepairs: MaxRepairs, Evidence: func(c Conclusion) []opscase.Citation { return c.Evidence }, Answer: answer}
	raw := []interfaces.Tool{}
	for _, t := range dataTools() {
		raw = append(raw, ledger.Wrap(t))
	}
	dispatched, err := opscase.Dispatch(append(raw, sub.Tool("提交根因结论；每条证据引用工具结果的 ref 并原样摘录片段", conclusionSchema)), tools.DispatchConfig{OnResult: sub.OnResult})
	if err != nil {
		return out, err
	}
	agent, err := builder.NewSimpleBuilder(client).WithDurableScope("rca").WithSystemPrompt(systemPrompt).
		WithTools(dispatched...).WithReturnDirectTools(opscase.SubmitTool).WithEmptyAnswerRetries(1).WithMaxIterations(9).WithMaxTokens(livemodel.MaxTokens).WithTimeout(10 * livemodel.RequestTimeout).Build()
	if err != nil {
		return out, err
	}
	before := client.Calls()
	final, err := agent.AsRunnable().Invoke(ctx, Alert)
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
	out.Decision = out.Accepted.RootCause
	return out, nil
}

// answer 是结论通过核验后直接交给调用方的最终回答。
func answer(c Conclusion) string { return "结论：" + c.RootCause + "\n" + c.Summary }

func main() {
	dir := flag.String("dir", "", "持久化目录；同一目录再次运行只回放")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "用法：alert-rca -dir <目录>")
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
	fmt.Println("期望根因：" + ExpectedRootCause)
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
