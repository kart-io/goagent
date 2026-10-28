// 容器容量两层复评：规则引擎先给出每个工作负载的建议，模型分批初筛，再为可执行项各开一个子 Agent 深评。
// AI 只能降级：只有“证据完整 + 初筛同方向 + 深评 confirm 且计划不比规则更激进”才保留可执行计划，其余降为人工复核并清空计划；
// 任何 AI 失败（调用出错、输出非法、预算不足）都降为人工复核并记录原因。数据全部是合成的，单位为 CPU 毫核。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/ops/internal/opscase"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Workload 是一个合成工作负载的 CPU 现状与 7 天观测。
type Workload struct {
	ID       string `json:"id"`
	Request  int    `json:"request_m"`
	Limit    int    `json:"limit_m"`
	P99      int    `json:"p99_7d_m"`
	Max      int    `json:"max_7d_m"`
	Evidence bool   `json:"evidence_complete"`
}

// Workloads 按规则应得：cap-a/d/h 缩容，cap-b/f/k 扩容，cap-c/g/j 保持，cap-e/i/l 证据不足。
var Workloads = []Workload{
	{"cap-a", 1000, 2000, 220, 500, true}, {"cap-b", 500, 1000, 480, 960, true}, {"cap-c", 800, 1600, 500, 1000, true},
	{"cap-d", 2000, 4000, 300, 900, true}, {"cap-e", 600, 1200, 350, 700, false}, {"cap-f", 400, 800, 390, 700, true},
	{"cap-g", 1000, 2000, 600, 1100, true}, {"cap-h", 1500, 3000, 200, 600, true}, {"cap-i", 300, 600, 100, 200, false},
	{"cap-j", 1200, 2400, 700, 1500, true}, {"cap-k", 250, 500, 240, 480, true}, {"cap-l", 900, 1800, 150, 400, false},
}

// 规则动作与最终结论。
const (
	ScaleDown, ScaleUp, Keep, Insufficient = "scale_down", "scale_up", "keep", "insufficient_evidence"
	Execute, ManualReview                  = "execute", "manual_review"
)

// 每批初筛 6 项；默认额度覆盖 2 次初筛与 6 次深评；深评最多 3 个并发。
const BatchSize, DefaultMaxRequests, DeepConcurrency = 6, 8, 3

// Plan 是新的 request/limit（毫核）。
type Plan struct {
	Request int `json:"request_m"`
	Limit   int `json:"limit_m"`
}

// 推理模型的思考也计入输出上限：Gemini 2.5 Flash 在 2048 下初筛会因 MAX_TOKENS 截断而整批作废。不改变请求数。
const reviewMaxTokens = 8192

func up50(v float64) int { return int(math.Ceil(v/50) * 50) }

// Rule 是确定性规则：证据不完整不给建议；接近上限扩容，长期低于 40% 缩容。新值取 P99×1.25、Max×1.3 并向上取整到 50。
// 扩容只看 P99、Max 任一项，另一项算出的新值可能低于当前值，所以扩容时每项不低于当前值；
// 缩容要求两项都低于 40%，新值必然低于当前值（P99×1.25 < 0.5×request，Max×1.3 < 0.52×limit），无需钳制。
func Rule(w Workload) (string, *Plan) {
	plan := &Plan{up50(float64(w.P99) * 1.25), up50(float64(w.Max) * 1.3)}
	switch {
	case !w.Evidence:
		return Insufficient, nil
	case 10*w.P99 > 9*w.Request || 10*w.Max > 9*w.Limit:
		return ScaleUp, &Plan{max(plan.Request, w.Request), max(plan.Limit, w.Limit)}
	case 10*w.P99 < 4*w.Request && 10*w.Max < 4*w.Limit:
		return ScaleDown, plan
	}
	return Keep, nil
}

// Guard 是服务端数值护栏，返回计划 p 的违反项；rule 是规则计划，规则自检时 p 就是 rule。
// 计划必须落在规则计划与当前值之间（逐项比较），深评只能比规则保守，不能更激进；与当前值相同的计划没有可执行的变更。
func Guard(w Workload, action string, rule, p Plan) (problems []string) {
	check := func(bad bool, msg string) {
		if bad {
			problems = append(problems, msg)
		}
	}
	check(p.Request < w.P99, "request 低于 P99")
	check(p.Limit < w.Max, "limit 低于 Max")
	check(p.Request > 2*w.Request || p.Limit > 2*w.Limit, "超过当前值 2 倍")
	check(p.Limit < p.Request, "limit 小于 request")
	check(action == ScaleDown && (p.Request > w.Request || p.Limit > w.Limit), "缩容却调高")
	check(action == ScaleUp && (p.Request < w.Request || p.Limit < w.Limit), "扩容却调低")
	check(action == ScaleDown && (p.Request < rule.Request || p.Limit < rule.Limit), "缩容比规则更激进")
	check(action == ScaleUp && (p.Request > rule.Request || p.Limit > rule.Limit), "扩容比规则更激进")
	check(p == Plan{w.Request, w.Limit}, "计划与当前值相同")
	return problems
}

// Item 是一个工作负载的最终结论：Screen/Verdict 为空表示没有有效的初筛或深评。
type Item struct {
	ID, Rule               string
	RulePlan               *Plan `json:",omitempty"`
	Screen, Verdict, Final string
	Plan                   *Plan `json:",omitempty"`
	Reasons                []string
}

// Result 汇总矩阵与计数。
type Result struct {
	Items                           []Item
	BatchErrors                     []string
	DeepReviews                     int
	ChildrenCreated, ChildrenClosed int64
	NewModelCalls                   int64
	Budget                          budget.Snapshot
}

const screenPrompt = `你是容器容量初筛助手。输入是一批工作负载的 CPU 数据（毫核）：当前 request/limit、7 天 P99 与 Max、证据是否完整。
判定：证据不完整只能给 keep；P99 > request×0.9 或 Max > limit×0.9 给 scale_up；P99 < request×0.4 且 Max < limit×0.4 给 scale_down；其余 keep。
只输出一个 JSON 对象，不要其他文字：{"items":[{"id":"...","direction":"scale_down|scale_up|keep","evidence_complete":true|false}]}
本批每个 id 恰好出现一次，evidence_complete 原样抄输入。`

const deepPrompt = `你是容器容量深评助手，只审阅一个工作负载的规则建议，不执行任何操作。输入含当前 request/limit、7 天 P99 与 Max（毫核）及规则建议。
新值必须满足：request ≥ P99，limit ≥ Max，limit ≥ request，两者都不超过当前值 2 倍；request 与 limit 各自落在规则值与当前值之间（scale_down：规则值 ≤ 新值 ≤ 当前值；scale_up：当前值 ≤ 新值 ≤ 规则值），且不能与当前值完全相同。
同意给 confirm 并填写最终 request/limit（可沿用规则值，或取更保守的值）；拿不准给 manual_review；不同意给 reject。
只输出一个 JSON 对象：{"id":"<输入 id>","verdict":"confirm|manual_review|reject","request":<整数>,"limit":<整数>,"reason":"<一句中文理由，不要写命令>"}`

// checkScreen 校验一批初筛，返回 id → direction；违反任一条整批作废。
func checkScreen(text string, batch []Workload) (map[string]string, error) {
	var out struct {
		Items []struct {
			ID               string `json:"id"`
			Direction        string `json:"direction"`
			EvidenceComplete *bool  `json:"evidence_complete"`
		} `json:"items"`
	}
	if err := opscase.StrictJSON(text, &out); err != nil {
		return nil, err
	}
	got := map[string]string{}
	for _, s := range out.Items {
		i := slices.IndexFunc(batch, func(w Workload) bool { return w.ID == s.ID })
		_, dup := got[s.ID]
		switch {
		case i < 0:
			return nil, fmt.Errorf("出现本批之外的 id %q", s.ID)
		case dup:
			return nil, fmt.Errorf("id %q 重复", s.ID)
		case !slices.Contains([]string{ScaleDown, ScaleUp, Keep}, s.Direction) || s.EvidenceComplete == nil:
			return nil, fmt.Errorf("id %q 的 direction 或 evidence_complete 不合法", s.ID)
		case s.Direction != Keep && (!batch[i].Evidence || !*s.EvidenceComplete):
			return nil, fmt.Errorf("id %q 证据不完整却给出 %s", s.ID, s.Direction)
		}
		got[s.ID] = s.Direction
	}
	if len(got) != len(batch) {
		return nil, fmt.Errorf("覆盖 %d/%d 个 id", len(got), len(batch))
	}
	return got, nil
}

func screen(ctx context.Context, client *livemodel.Client, n int, batch []Workload) (map[string]string, error) {
	agent, err := builder.NewSimpleBuilder(client).WithDurableScope(fmt.Sprintf("screen-%d", n)).WithSystemPrompt(screenPrompt).
		WithMaxIterations(1).WithMaxTokens(reviewMaxTokens).WithTimeout(2 * livemodel.RequestTimeout).Build()
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(map[string]any{"workloads": batch})
	out, err := agent.AsRunnable().Invoke(ctx, string(data))
	if err != nil {
		return nil, err
	}
	return checkScreen(fmt.Sprint(out.Result), batch)
}

type counters struct{ created, closed atomic.Int64 }

// deepTool 每次调用新建一个深评子 Agent（独立会话），参数只有 id，深评输入由宿主按可信数据生成。
func deepTool(client *livemodel.Client, c *counters) (interfaces.Tool, error) {
	return tools.NewRunnableToolFactory(func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		agent, err := builder.NewSimpleBuilder(client).WithDurableScope("deep").WithSystemPrompt(deepPrompt).
			WithSessionID("deep/" + uuid.NewString()).WithMaxIterations(1).WithMaxTokens(reviewMaxTokens).Build()
		if err != nil {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{}, err
		}
		c.created.Add(1)
		return tools.OwnedRunnable[any, *builder.AgentOutput]{Runnable: agent.AsRunnable(), Close: func(context.Context) error { c.closed.Add(1); return nil }}, nil
	}, tools.RunnableToolConfig[any, *builder.AgentOutput]{
		// 每次深评只发一个模型请求；Timeout 不含排队，按单次请求留出余量。
		Name: "deep_review", Description: "逐项深评一个工作负载的容量建议", Version: "v1", ModelInvoking: true, CleanupTimeout: 10 * time.Second,
		Timeout:    2 * livemodel.RequestTimeout,
		ArgsSchema: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`,
		Decode: func(a map[string]any) (any, error) {
			i := slices.IndexFunc(Workloads, func(w Workload) bool { return w.ID == a["id"] })
			if i < 0 {
				return nil, fmt.Errorf("未知工作负载 %v", a["id"])
			}
			action, plan := Rule(Workloads[i])
			data, err := json.Marshal(map[string]any{"workload": Workloads[i], "rule": map[string]any{"action": action, "plan": plan}})
			return string(data), err
		},
		Encode: func(a *builder.AgentOutput) (any, error) {
			if a == nil {
				return nil, fmt.Errorf("深评没有返回结果")
			}
			return fmt.Sprint(a.Result), nil
		},
	})
}

// checkDeep 校验深评输出：只有 confirm 且通过全部检查才返回计划，否则返回降级原因。
func checkDeep(w Workload, text string) (verdict string, plan *Plan, problem string) {
	var d struct {
		ID      string `json:"id"`
		Verdict string `json:"verdict"`
		Request int    `json:"request"`
		Limit   int    `json:"limit"`
		Reason  string `json:"reason"`
	}
	if err := opscase.StrictJSON(text, &d); err != nil {
		return "", nil, "深评输出非法：" + err.Error()
	}
	p := Plan{d.Request, d.Limit}
	action, rule := Rule(w)
	guard := Guard(w, action, *rule, p)
	switch {
	case d.ID != w.ID:
		return d.Verdict, nil, fmt.Sprintf("深评回显 id %q 不符", d.ID)
	case d.Verdict != "confirm":
		return d.Verdict, nil, fmt.Sprintf("深评 %s：%s", d.Verdict, d.Reason)
	case guard != nil:
		return d.Verdict, nil, "深评越过护栏：" + strings.Join(guard, "；")
	}
	return d.Verdict, &p, ""
}

// run 的返回值具名：延迟统计要写进返回结果。maxRequests 是根预算的请求额度。
func run(ctx context.Context, client *livemodel.Client, dir string, maxRequests int) (out Result, err error) {
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	controller, err := budget.NewDurable(budget.Config{RootKey: "capacity/wk-44", Version: "v1/" + client.Live.Provider + "/" + client.Live.Model, MaxModelRequests: maxRequests}, store)
	if err != nil {
		return out, err
	}
	ctx = budget.WithController(durable.WithJournal(ctx, store), controller)
	var c counters
	deep, err := deepTool(client, &c)
	if err != nil {
		return out, err
	}
	reg := tools.NewRegistry()
	if err = reg.Register(deep); err != nil {
		return out, err
	}
	disp, err := tools.NewDispatcher(reg, tools.DispatchConfig{MaxConcurrency: DeepConcurrency})
	if err != nil {
		return out, err
	}
	before := client.Calls()
	defer func() {
		out.NewModelCalls, out.ChildrenCreated, out.ChildrenClosed = client.Calls()-before, c.created.Load(), c.closed.Load()
		out.Budget, _ = controller.Snapshot(ctx)
	}()

	// 第一层：分批初筛，串行执行，额度消耗顺序确定。
	screens := map[string]string{}
	batches := 0
	screenCtx := durable.WithExecutionKey(ctx, "capacity/wk-44/screen")
	for start := 0; start < len(Workloads); start += BatchSize {
		batches++
		got, err := screen(screenCtx, client, batches, Workloads[start:min(start+BatchSize, len(Workloads))])
		if err != nil {
			out.BatchErrors = append(out.BatchErrors, fmt.Sprintf("初筛第 %d 批作废：%v", batches, err))
			continue
		}
		maps.Copy(screens, got)
	}

	// 选出深评候选；额度按配置预先分配（不读快照，回放时同样确定），超出部分降级。
	// 每项默认 manual_review；keep 与证据不足由规则定论，AI 的初筛与深评都改变不了。
	out.Items = make([]Item, len(Workloads))
	quota := maxRequests - batches
	var admitted []int
	for i, w := range Workloads {
		it := &out.Items[i]
		it.ID, it.Final = w.ID, ManualReview
		it.Rule, it.RulePlan = Rule(w)
		direction, ok := screens[w.ID]
		it.Screen = direction
		switch {
		case it.Rule == Keep:
			it.Final = Keep
		case it.Rule == Insufficient:
			it.Reasons = append(it.Reasons, "规则：证据不完整")
		case !ok:
			it.Reasons = append(it.Reasons, "缺少有效初筛")
		case direction != it.Rule: // 初筛给出 scale_* 时 checkScreen 已保证证据完整
			it.Reasons = append(it.Reasons, fmt.Sprintf("初筛给出 %s，与规则 %s 不一致", direction, it.Rule))
		case len(admitted) >= quota:
			it.Reasons = append(it.Reasons, "预算不足，未深评")
		default:
			admitted = append(admitted, i)
		}
	}
	out.DeepReviews = len(admitted)

	// 第二层：宿主经 Dispatcher 并行深评（CallID 由 id 派生，回放直接取已保存结果）；通过全部检查才改为 execute，各 goroutine 只写自己那一项。
	deepCtx := durable.WithExecutionKey(ctx, "capacity/wk-44/deep")
	var wg sync.WaitGroup
	for _, i := range admitted {
		wg.Add(1)
		go func(it *Item, w Workload) {
			defer wg.Done()
			res, err := disp.Invoke(deepCtx, tools.ToolRequest{Name: "deep_review", Input: &interfaces.ToolInput{CallID: "deep-" + w.ID, Args: map[string]any{"id": w.ID}}})
			if err != nil {
				it.Reasons = append(it.Reasons, "深评失败："+livemodel.Redact(err.Error()))
				return
			}
			var problem string
			if it.Verdict, it.Plan, problem = checkDeep(w, fmt.Sprint(res.Result)); it.Plan == nil {
				it.Reasons = append(it.Reasons, problem)
				return
			}
			it.Final = Execute
		}(&out.Items[i], Workloads[i])
	}
	wg.Wait()
	return out, nil
}

// Run 以默认额度复评全部工作负载。dir 保存模型日志、深评工具日志与根预算；同一目录再次运行只回放。
func Run(ctx context.Context, client *livemodel.Client, dir string) (Result, error) {
	return run(ctx, client, dir, DefaultMaxRequests)
}

func main() {
	dir := flag.String("dir", "", "持久化目录；同一目录再次运行只回放")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "用法：capacity-review -dir <目录>")
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
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
