// 并行诊断：CPU、数据库、网络三个 Agent 并行判断指标，汇总 Agent 给出处置结论。
// 全程记录观测事件（JSONL），导出拓扑和离线 HTML 报告；事件不含提示词和模型正文。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/devtools"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/live/internal/livecase"
	"github.com/kart-io/goagent/workflow"
)

// Incident 是图状态；Findings 由各分支写入自己的键，Merge 统一合并。
type Incident struct {
	Metrics  map[string]string
	Findings map[string]string
	Decision string
}

// Snapshot 是合成指标；按下方规则，CPU 和网络异常、数据库正常。
var Snapshot = map[string]string{
	"cpu":     "过去 10 分钟 CPU 平均使用率 97%，load1=31（16 核），出现持续限流。",
	"db":      "数据库 P99 延迟 18ms，连接池使用率 42%，慢查询 0 条，主从延迟 0.3s。",
	"network": "跨机房链路丢包率 12%，重传率 9%，RTT 从 2ms 升至 45ms。",
}

// Expected 是按规则应得的分支结论和汇总结论。
var Expected = map[string]string{"cpu": "异常", "db": "正常", "network": "异常", "decision": "升级"}

var rules = map[string]string{
	"cpu":     "CPU 平均使用率 ≥85% 或 load1 超过核数 1.5 倍判为异常，否则正常。",
	"db":      "P99 延迟 ≥200ms、连接池使用率 ≥90% 或慢查询 >10 条任一满足判为异常，否则正常。",
	"network": "丢包率 ≥2% 或 RTT 超过基线 5 倍判为异常，否则正常。",
}

// Result 汇总诊断结论、计量和观测产物。
type Result struct {
	Findings      map[string]string
	Decision      string
	NewModelCalls int64
	Budget        budget.Snapshot
	EventCounts   map[string]int
	Dropped       uint64
	EventsPath    string
	ReportPath    string
}

func diagnoser(client *livemodel.Client, area string) (workflow.Branch[Incident], error) {
	agent, err := builder.NewSimpleBuilder(client).WithDurableScope("diagnose-" + area).
		WithSystemPrompt("你是 " + area + " 诊断专家。判定规则：" + rules[area] + "\n第一行只能是“结论：异常”或“结论：正常”，第二行一句依据。").
		WithMaxIterations(2).WithMaxTokens(livemodel.MaxTokens).Build()
	if err != nil {
		return workflow.Branch[Incident]{}, err
	}
	return workflow.Branch[Incident]{Name: area, Run: func(ctx context.Context, s Incident) (Incident, error) {
		out, err := agent.AsRunnable().Invoke(ctx, s.Metrics[area])
		if err != nil {
			return s, err
		}
		s.Findings[area], err = livecase.Decision(fmt.Sprint(out.Result), "异常", "正常")
		return s, err
	}}, nil
}

func build(client *livemodel.Client) (*workflow.Graph[Incident], error) {
	branches := []workflow.Branch[Incident]{}
	for _, area := range []string{"cpu", "db", "network"} {
		b, err := diagnoser(client, area)
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	summary, err := builder.NewSimpleBuilder(client).WithDurableScope("summarize").
		WithSystemPrompt("你是值班负责人。输入是各模块诊断结论。规则：两个及以上模块异常输出“结论：升级”，否则输出“结论：观察”。第二行一句理由。").
		WithMaxIterations(2).WithMaxTokens(livemodel.MaxTokens).Build()
	if err != nil {
		return nil, err
	}
	g := workflow.NewGraph[Incident]()
	if err = g.AddParallel("diagnose", workflow.ParallelConfig[Incident]{Branches: branches,
		Clone: func(s Incident) Incident { s.Findings = maps.Clone(s.Findings); return s },
		Merge: func(_ context.Context, s Incident, outs []Incident) (Incident, error) {
			s.Findings = maps.Clone(s.Findings)
			for i, o := range outs {
				name := branches[i].Name
				s.Findings[name] = o.Findings[name]
			}
			return s, nil
		}}); err != nil {
		return nil, err
	}
	if err = g.AddNode("summarize", func(ctx context.Context, s Incident) (Incident, error) {
		data, _ := json.Marshal(s.Findings)
		out, err := summary.AsRunnable().Invoke(ctx, string(data))
		if err != nil {
			return s, err
		}
		s.Decision, err = livecase.Decision(fmt.Sprint(out.Result), "升级", "观察")
		return s, err
	}); err != nil {
		return nil, err
	}
	for _, e := range [][2]string{{workflow.START, "diagnose"}, {"diagnose", "summarize"}, {"summarize", workflow.END}} {
		if err = g.AddEdge(e[0], e[1]); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// Run 执行一次诊断，在 dir 下写出 events.jsonl 与 report.html。
// ctx 可携带宿主的 OTel tracer（observe.WithTracer），事件会挂在其 trace 下。
func Run(ctx context.Context, client *livemodel.Client, dir string) (Result, error) {
	out := Result{EventsPath: filepath.Join(dir, "events.jsonl"), ReportPath: filepath.Join(dir, "report.html")}
	g, err := build(client)
	if err != nil {
		return out, err
	}
	topology, err := g.Describe()
	if err != nil {
		return out, err
	}
	recorder, err := devtools.NewRecorder(devtools.RecorderConfig{Path: out.EventsPath})
	if err != nil {
		return out, err
	}
	controller, err := budget.New(budget.Config{RootKey: "incident/net-1", Version: "v1", MaxModelRequests: 6})
	if err != nil {
		return out, err
	}
	run, err := g.Compile(ctx)
	if err != nil {
		return out, err
	}
	before := client.Calls()
	final, runErr := run.Invoke(observe.WithObserver(budget.WithController(ctx, controller), recorder), Incident{Metrics: Snapshot, Findings: map[string]string{}})
	out.NewModelCalls = client.Calls() - before
	out.Budget, _ = controller.Snapshot(ctx)
	out.Findings, out.Decision = final.Findings, final.Decision
	// 观测失败不改变业务结果，但案例要求事件与报告完整，所以一并返回。
	// 关闭后再读丢弃数：关闭之后才到达的事件也计为丢弃。
	if err = recorder.Close(context.Background()); err != nil {
		return out, err
	}
	out.Dropped = recorder.Stats().Dropped
	events, err := readEvents(out.EventsPath)
	if err != nil {
		return out, err
	}
	out.EventCounts = map[string]int{}
	for _, e := range events {
		out.EventCounts[e.Type]++
	}
	report, err := os.OpenFile(out.ReportPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return out, err
	}
	if err = devtools.WriteReport(report, topology, events); err != nil {
		_ = report.Close()
		return out, err
	}
	if err = report.Close(); err != nil {
		return out, err
	}
	return out, runErr
}

func readEvents(path string) ([]observe.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var events []observe.Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e observe.Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, scanner.Err()
}

func main() {
	dir := flag.String("dir", "", "输出目录（需为空或不存在），写入 events.jsonl 与 report.html")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "用法：parallel-diagnostics -dir <目录>")
		os.Exit(2)
	}
	client, err := livemodel.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := Run(context.Background(), client, *dir)
	types := make([]string, 0, len(out.EventCounts))
	for t := range out.EventCounts {
		types = append(types, t)
	}
	sort.Strings(types)
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	fmt.Println("事件类型：", strings.Join(types, ", "))
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
