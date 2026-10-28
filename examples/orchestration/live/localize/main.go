// 本地化：同一个持久节点内先后运行翻译 Agent 与校对 Agent，二者都有名为 lookup_term
// 的工具但实现不同，靠 WithDurableScope 隔离日志。翻译 Agent 挂载自定义术语表策略，
// 策略的版本与配置哈希进入模型意图，术语表一改就不能复用旧日志。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/live/internal/livecase"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

// Glossary 是有序术语表；顺序也属于配置，参与哈希。
type Glossary struct {
	Version string
	Terms   [][2]string
}

// DefaultGlossary 的译法是案例的验收依据。
var DefaultGlossary = Glossary{Version: "v1", Terms: [][2]string{
	{"签收", "delivery confirmation"},
	{"无理由退款", "no-questions-asked refund"},
	{"原路退回", "refunded to the original payment method"},
	{"工作日", "business days"},
}}

// Source 中【】标出的术语必须按术语表翻译；商品编号的英文名不在术语表里，
// 只能经 lookup_term 查到，所以翻译 Agent 必须调用工具。
const Source = "【SKU-7731】自【签收】之日起 7 天内，未拆封商品支持【无理由退款】，款项将在 3 个【工作日】内【原路退回】。"

// products 是商品编号到英文商品名的合成目录。
var products = map[string]string{"SKU-7731": "Aurora Wireless Earbuds"}

// RequiredPhrases 是终稿必须出现的英文译法（不区分大小写）。
var RequiredPhrases = []string{"aurora wireless earbuds", "delivery confirmation", "no-questions-asked refund", "original payment method", "business days"}

// MissingPhrases 由宿主独立检查终稿。
func MissingPhrases(text string) []string {
	lower := strings.ToLower(strings.ReplaceAll(text, "‑", "-"))
	var missing []string
	for _, p := range RequiredPhrases {
		if !strings.Contains(lower, p) {
			missing = append(missing, p)
		}
	}
	return missing
}

const marker = "\n\n[术语表 "

// glossaryPolicy 把术语表追加到首条用户消息末尾。只改非 system 消息的正文，
// 不增删消息，满足策略运行时的协议约束；重复调用保持幂等。
type glossaryPolicy struct {
	glossary   Glossary
	descriptor contextpolicy.Descriptor
	injected   atomic.Int64
}

func newGlossaryPolicy(g Glossary) (*glossaryPolicy, error) {
	if g.Version == "" || len(g.Terms) == 0 {
		return nil, errors.New("术语表需要版本和至少一个术语")
	}
	data, err := json.Marshal(g.Terms)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return &glossaryPolicy{glossary: g, descriptor: contextpolicy.Descriptor{ID: "glossary", Version: g.Version, ConfigHash: hex.EncodeToString(sum[:]), Durable: true}}, nil
}

func (p *glossaryPolicy) Descriptor() contextpolicy.Descriptor { return p.descriptor }
func (p *glossaryPolicy) Stats() map[string]int64 {
	return map[string]int64{"injected": p.injected.Load()}
}
func (p *glossaryPolicy) BeforeTurn(_ context.Context, turn contextpolicy.Turn) (contextpolicy.Turn, error) {
	for i, m := range turn.Messages {
		if m.Role != "user" {
			continue
		}
		if !strings.Contains(m.Content, marker) {
			var b strings.Builder
			b.WriteString(m.Content + marker + p.glossary.Version + "，必须使用以下译法]")
			for _, t := range p.glossary.Terms {
				fmt.Fprintf(&b, "\n- %s → %s", t[0], t[1])
			}
			turn.Messages[i].Content = b.String()
			p.injected.Add(1)
		}
		break
	}
	return turn, nil
}

// Result 汇总终稿、两个 Agent 的工具执行次数与计量。
type Result struct {
	English                         string
	TranslatorLookups, StyleLookups int64
	GlossaryInjected                int64
	NewModelCalls                   int64
	Budget                          budget.Snapshot
}

// Hooks 用于测试注入：AfterTranslate 在翻译 Agent 完成、校对 Agent 开始前调用。
type Hooks struct{ AfterTranslate func() }

type state struct{ Source, Draft, Final string }

func lookupTool(desc string, fn func(term string) (any, error)) interfaces.Tool {
	return tools.NewFunctionTool("lookup_term", desc, `{"type":"object","properties":{"term":{"type":"string"}},"required":["term"],"additionalProperties":false}`,
		func(_ context.Context, a map[string]any) (any, error) { term, _ := a["term"].(string); return fn(term) })
}

// counters 记录节点内实际执行的工具与策略注入次数；回放时保持为 0。
type counters struct{ translatorLookups, styleLookups, injected atomic.Int64 }

func build(ctx context.Context, client *livemodel.Client, store durable.Store, glossary Glossary, c *counters, hooks Hooks) (*workflow.DurableGraph[state], error) {
	terms := map[string]string{}
	for _, t := range glossary.Terms {
		terms[t[0]] = t[1]
	}
	// 两个工具同名、实现不同，各自经独立 Dispatcher 记日志。
	translatorTools, err := livecase.DispatchTools([]interfaces.Tool{lookupTool("查询商品编号的英文商品名或中文术语的标准英文译法", func(term string) (any, error) {
		c.translatorLookups.Add(1)
		term = strings.Trim(term, "【】 ")
		if en, ok := products[term]; ok {
			return map[string]string{"term": term, "english": en}, nil
		}
		if en, ok := terms[term]; ok {
			return map[string]string{"term": term, "english": en}, nil
		}
		return map[string]string{"term": term, "english": ""}, nil
	})})
	if err != nil {
		return nil, err
	}
	proofTools, err := livecase.DispatchTools([]interfaces.Tool{lookupTool("查询英文风格规范，term 传 style", func(string) (any, error) {
		c.styleLookups.Add(1)
		return map[string]string{"rules": "Use plain English; keep glossary phrases verbatim; write 'business days', never 'working days'; one or two sentences."}, nil
	})})
	if err != nil {
		return nil, err
	}
	g := workflow.NewGraph[state]()
	if err = g.AddNode("localize", func(ctx context.Context, s state) (state, error) {
		policy, err := newGlossaryPolicy(glossary)
		if err != nil {
			return s, err
		}
		translator, err := builder.NewSimpleBuilder(client).WithDurableScope("translate").
			WithSystemPrompt("你是中译英译员。原文中的商品编号必须调用 lookup_term 查询英文商品名，译文用商品名代替编号；其余【】术语按术语表翻译。只输出英文译文。").
			WithTools(translatorTools...).WithContextPolicies(func() contextpolicy.Policy { return policy }).
			WithMaxIterations(6).WithMaxTokens(8192).Build()
		if err != nil {
			return s, err
		}
		draft, err := translator.AsRunnable().Invoke(ctx, "原文："+s.Source)
		c.injected.Add(policy.injected.Load())
		if err != nil {
			return s, err
		}
		s.Draft = strings.TrimSpace(fmt.Sprint(draft.Result))
		if hooks.AfterTranslate != nil {
			hooks.AfterTranslate()
		}
		// 校对 Agent 也挂同一术语表策略（独立实例），否则润色时可能改写术语译法。
		proofPolicy, err := newGlossaryPolicy(glossary)
		if err != nil {
			return s, err
		}
		proofreader, err := builder.NewSimpleBuilder(client).WithDurableScope("proofread").
			WithSystemPrompt("你是英文校对。先调用一次 lookup_term（term 传 style）获取风格规范，再按规范润色译文；术语表中的英文译法必须逐字保留。只输出最终英文。").
			WithTools(proofTools...).WithContextPolicies(func() contextpolicy.Policy { return proofPolicy }).
			WithMaxIterations(4).WithMaxTokens(8192).Build()
		if err != nil {
			return s, err
		}
		final, err := proofreader.AsRunnable().Invoke(ctx, "待校对译文："+s.Draft)
		c.injected.Add(proofPolicy.injected.Load())
		if err != nil {
			return s, err
		}
		s.Final = strings.TrimSpace(fmt.Sprint(final.Result))
		return s, nil
	}); err != nil {
		return nil, err
	}
	if err = g.AddEdge(workflow.START, "localize"); err != nil {
		return nil, err
	}
	if err = g.AddEdge("localize", workflow.END); err != nil {
		return nil, err
	}
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "localize-v1/" + client.Live.Provider + "/" + client.Live.Model})
}

// Run 推进本地化运行；glossary 改变会让已保存的翻译请求意图失配并在请求前失败。
func Run(ctx context.Context, client *livemodel.Client, dir string, glossary Glossary, hooks Hooks) (Result, error) {
	var out Result
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	controller, err := budget.NewDurable(budget.Config{RootKey: "localize/notice-1", Version: "v1/" + client.Live.Provider + "/" + client.Live.Model, MaxModelRequests: 12}, store)
	if err != nil {
		return out, err
	}
	var c counters
	g, err := build(ctx, client, store, glossary, &c, hooks)
	if err != nil {
		return out, err
	}
	before := client.Calls()
	snap, runErr := g.Run(budget.WithController(ctx, controller), "notice-1", state{Source: Source}, "")
	out.TranslatorLookups, out.StyleLookups, out.GlossaryInjected = c.translatorLookups.Load(), c.styleLookups.Load(), c.injected.Load()
	out.NewModelCalls = client.Calls() - before
	out.Budget, _ = controller.Snapshot(ctx)
	if snap != nil {
		out.English = snap.State.Final
	}
	if runErr != nil {
		return out, runErr
	}
	if !snap.Done {
		return out, errors.New("本地化流程没有结束")
	}
	return out, nil
}

func main() {
	dir := flag.String("dir", "", "持久化目录；中断后用同一目录重新运行即可继续")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "用法：localize -dir <目录>")
		os.Exit(2)
	}
	client, err := livemodel.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := Run(context.Background(), client, *dir, DefaultGlossary, Hooks{})
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	if missing := MissingPhrases(out.English); len(missing) > 0 {
		fmt.Println("宿主检查：终稿缺少", missing)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
