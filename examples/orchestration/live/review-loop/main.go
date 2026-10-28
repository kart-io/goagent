// 文稿审校循环：写作 Agent 起草公告，审校 Agent 按清单把关，不通过就带着意见回到写作，
// 最多 MaxRounds 轮。整个循环是持久图，每次节点访问都有检查点，进程中途退出后可恢复。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/live/internal/livecase"
	"github.com/kart-io/goagent/workflow"
)

// MaxRounds 限制写作次数；根预算按“每轮写 + 审各一次”留足额度。
const MaxRounds = 3

// 推理模型偶尔在改稿/审校时推理过长，2048 会触发 length 截断并被判为不完整输出；
// 正常一次约 300 Token，这里只是留出余量，不改变计费请求数。
const nodeMaxTokens = 8192

// Doc 是图状态。Round 在每次进入写作节点时加一。
type Doc struct {
	Topic, Draft, Feedback, Verdict string
	Round                           int
}

// RequiredFacts 只出现在审校清单里，写作 Agent 第一轮无从得知，循环必然发生。
var RequiredFacts = []string{"3个工作日", "定制", "refund@demo.example"}

// 字数这类机械检查不交给模型：模型数字数不可靠，会把合格稿件判为修改。
const checklist = `审校清单（全部满足才算通过，不检查字数）：
1. 写明退款在 3 个工作日内原路退回；
2. 写明定制刻字类商品不适用无理由退款；
3. 给出咨询邮箱 refund@demo.example。`

// 任务书写明字数上限（真实需求通常会给），但不提具体条款，条款只在审校清单里。
const topic = "为电商店铺写一段退款政策公告，面向普通消费者，语气友好，不超过 150 字。"

// MissingFacts 由宿主独立检查终稿，不依赖审校 Agent 的判断。
func MissingFacts(draft string) []string {
	compact := strings.Join(strings.Fields(draft), "")
	var missing []string
	for _, f := range RequiredFacts {
		if !strings.Contains(compact, f) {
			missing = append(missing, f)
		}
	}
	return missing
}

// Result 汇总终稿、循环轮次与计量。
type Result struct {
	Draft, Verdict           string
	Rounds                   int
	WriterRuns, ReviewerRuns int64
	NewModelCalls            int64
	Budget                   budget.Snapshot
	Done                     bool
}

// Hooks 用于测试注入：beforeWrite 在每次实际执行写作节点前调用。
type Hooks struct{ BeforeWrite func(round int) }

func build(ctx context.Context, client *livemodel.Client, store durable.Store, writes, reviews *atomic.Int64, hooks Hooks) (*workflow.DurableGraph[Doc], error) {
	g := workflow.NewGraph[Doc]()
	// 每次节点访问新建 Agent，避免跨轮次的内存会话影响恢复后的行为。
	if err := g.AddNode("write", func(ctx context.Context, d Doc) (Doc, error) {
		if hooks.BeforeWrite != nil {
			hooks.BeforeWrite(d.Round + 1)
		}
		writes.Add(1)
		agent, err := builder.NewSimpleBuilder(client).WithDurableScope("writer").
			WithSystemPrompt("你是文案编辑，只输出公告正文，不要解释。").WithMaxIterations(1).WithMaxTokens(nodeMaxTokens).Build()
		if err != nil {
			return d, err
		}
		input := "任务：" + d.Topic
		if d.Draft != "" {
			input += "\n\n上一稿：\n" + d.Draft + "\n\n审校意见（必须全部落实）：\n" + d.Feedback
		}
		out, err := agent.AsRunnable().Invoke(ctx, input)
		if err != nil {
			return d, err
		}
		d.Round++
		d.Draft = strings.TrimSpace(fmt.Sprint(out.Result))
		return d, nil
	}); err != nil {
		return nil, err
	}
	if err := g.AddNode("review", func(ctx context.Context, d Doc) (Doc, error) {
		reviews.Add(1)
		agent, err := builder.NewSimpleBuilder(client).WithDurableScope("reviewer").
			WithSystemPrompt("你是严格的审校。" + checklist + "\n第一行只能是“结论：通过”或“结论：修改”；若修改，最多再写 3 行，每行一句话指出缺失项及应补充的原文。").
			WithMaxIterations(1).WithMaxTokens(nodeMaxTokens).Build()
		if err != nil {
			return d, err
		}
		out, err := agent.AsRunnable().Invoke(ctx, "待审稿件：\n"+d.Draft)
		if err != nil {
			return d, err
		}
		text := strings.TrimSpace(fmt.Sprint(out.Result))
		if d.Verdict, err = livecase.Decision(text, "通过", "修改"); err != nil {
			return d, err
		}
		d.Feedback = text
		return d, nil
	}); err != nil {
		return nil, err
	}
	if err := g.AddEdge(workflow.START, "write"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("write", "review"); err != nil {
		return nil, err
	}
	if err := g.AddBranch("review", func(_ context.Context, d Doc) (string, error) {
		if d.Verdict == "通过" || d.Round >= MaxRounds {
			return workflow.END, nil
		}
		return "write", nil
	}, "write", workflow.END); err != nil {
		return nil, err
	}
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "review-loop-v1/" + client.Live.Provider + "/" + client.Live.Model})
}

// Run 推进同一个持久运行，直到结束或出错；同目录再次调用从检查点继续。
func Run(ctx context.Context, client *livemodel.Client, dir string, hooks Hooks) (Result, error) {
	var out Result
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	controller, err := budget.NewDurable(budget.Config{RootKey: "doc/refund-notice", Version: "v1/" + client.Live.Provider + "/" + client.Live.Model, MaxModelRequests: 2 * MaxRounds}, store)
	if err != nil {
		return out, err
	}
	var writes, reviews atomic.Int64
	g, err := build(ctx, client, store, &writes, &reviews, hooks)
	if err != nil {
		return out, err
	}
	before := client.Calls()
	snap, runErr := g.Run(budget.WithController(ctx, controller), "doc-1", Doc{Topic: topic}, "")
	out.NewModelCalls = client.Calls() - before
	out.WriterRuns, out.ReviewerRuns = writes.Load(), reviews.Load()
	out.Budget, _ = controller.Snapshot(ctx)
	if snap != nil {
		out.Draft, out.Verdict, out.Rounds, out.Done = snap.State.Draft, snap.State.Verdict, snap.State.Round, snap.Done
	}
	if runErr != nil {
		return out, runErr
	}
	if !out.Done {
		return out, errors.New("审校循环没有结束")
	}
	return out, nil
}

func main() {
	dir := flag.String("dir", "", "持久化目录；中断后用同一目录重新运行即可继续")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "用法：review-loop -dir <目录>")
		os.Exit(2)
	}
	client, err := livemodel.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := Run(context.Background(), client, *dir, Hooks{})
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	if missing := MissingFacts(out.Draft); len(missing) > 0 {
		fmt.Println("宿主检查：终稿缺少", missing)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
