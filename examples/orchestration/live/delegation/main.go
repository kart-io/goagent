// 多级委派：经理 Agent → 财务专家子 Agent → 换算图工具 / 计税工具，三层共用一份持久根预算。
// 每层使用独立 Dispatcher；嵌套深度由 RunnableTool 的 MaxDepth 约束，只会逐层收紧。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

// 合成汇率与税率；Expected 由宿主按同一数据计算，用来校验模型的最终答案。
var rates = map[string]float64{"USD": 7.10, "EUR": 7.72, "JPY": 0.047}

const vatRate = 0.06

// Question 是交给经理的任务。
const Question = "供应商报价 1200 美元。请换算成人民币并加 6% 增值税，告诉我含税总价。"

// Expected 是 Question 的正确含税总价（元）。
var Expected = round2(round2(1200*rates["USD"]) * (1 + vatRate))

func round2(v float64) float64 { return math.Round(v*100) / 100 }

const managerPrompt = `你是采购经理助理。涉及汇率换算和税费的问题必须交给 finance_expert，把原始需求完整写进 question，不要自己计算。
拿到结果后作答：第一行“结论：<含税总价数字> 元”（保留两位小数，不要千分位），第二行一句说明。`

const expertPrompt = `你是财务专家。先调用 fx_convert 把外币金额换算为人民币，再把换算结果传给 tax_calc 计算含税总价。
不要自己做任何算术，只引用工具返回的数字，用一句话回答含税总价和计算过程。`

// Result 汇总答案与各层执行计数。
type Result struct {
	Answer                      string
	Total                       float64
	ExpertsCreated, ExpertsDone int64
	ConvertRuns, TaxRuns        int64
	NewModelCalls               int64
	Budget                      budget.Snapshot
}

// options 只供测试注入异常配置。
type options struct {
	expertMaxDepth  int  // 财务专家工具的 MaxDepth；0 表示默认 8
	shareDispatcher bool // 让专家复用经理的 Dispatcher，用来演示重入被拒
}

type counters struct{ created, closed, convert, tax atomic.Int64 }

// convertTool 是两节点规则图：查汇率 → 换算，包装成第 2 层工具。
func convertTool(c *counters) (interfaces.Tool, error) {
	type conv struct {
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
		Rate     float64 `json:"cny_per_unit"`
		CNY      float64 `json:"amount_cny"`
	}
	g := workflow.NewGraph[conv]()
	if err := g.AddNode("rate", func(_ context.Context, v conv) (conv, error) {
		r, ok := rates[strings.ToUpper(v.Currency)]
		if !ok {
			return v, fmt.Errorf("不支持的币种 %s", v.Currency)
		}
		c.convert.Add(1)
		v.Currency, v.Rate = strings.ToUpper(v.Currency), r
		return v, nil
	}); err != nil {
		return nil, err
	}
	if err := g.AddNode("multiply", func(_ context.Context, v conv) (conv, error) { v.CNY = round2(v.Amount * v.Rate); return v, nil }); err != nil {
		return nil, err
	}
	for _, e := range [][2]string{{workflow.START, "rate"}, {"rate", "multiply"}, {"multiply", workflow.END}} {
		if err := g.AddEdge(e[0], e[1]); err != nil {
			return nil, err
		}
	}
	run, err := g.Compile(context.Background())
	if err != nil {
		return nil, err
	}
	return tools.NewRunnableTool(run, tools.RunnableToolConfig[conv, conv]{
		Name: "fx_convert", Description: "把外币金额按今日汇率换算为人民币", Version: "v1",
		ArgsSchema: `{"type":"object","properties":{"amount":{"type":"number","minimum":0},"currency":{"type":"string","enum":["USD","EUR","JPY"]}},"required":["amount","currency"],"additionalProperties":false}`,
		Decode: func(a map[string]any) (conv, error) {
			amount, _ := a["amount"].(float64)
			currency, _ := a["currency"].(string)
			return conv{Amount: amount, Currency: currency}, nil
		},
		Encode: func(v conv) (any, error) { return v, nil },
	})
}

func taxTool(c *counters) interfaces.Tool {
	return tools.NewFunctionTool("tax_calc", "按 6% 增值税计算人民币含税总价",
		`{"type":"object","properties":{"amount_cny":{"type":"number","minimum":0}},"required":["amount_cny"],"additionalProperties":false}`,
		func(_ context.Context, a map[string]any) (any, error) {
			c.tax.Add(1)
			amount, _ := a["amount_cny"].(float64)
			tax := round2(amount * vatRate)
			return map[string]float64{"amount_cny": amount, "vat": tax, "total_cny": round2(amount + tax)}, nil
		})
}

// expertTool 每次调用新建财务专家：独立会话、独立 Dispatcher（除非测试要求共享）。
func expertTool(client *livemodel.Client, c *counters, opt options, shared *[]interfaces.Tool) (interfaces.Tool, error) {
	return tools.NewRunnableToolFactory(func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		var expertTools []interfaces.Tool
		if opt.shareDispatcher {
			expertTools = *shared
		} else {
			conv, err := convertTool(c)
			if err != nil {
				return tools.OwnedRunnable[any, *builder.AgentOutput]{}, err
			}
			if expertTools, err = livecase.DispatchTools([]interfaces.Tool{conv, taxTool(c)}); err != nil {
				return tools.OwnedRunnable[any, *builder.AgentOutput]{}, err
			}
		}
		agent, err := builder.NewSimpleBuilder(client).WithDurableScope("expert").WithSystemPrompt(expertPrompt).
			WithSessionID("expert/" + uuid.NewString()).WithTools(expertTools...).WithMaxIterations(4).WithMaxTokens(livemodel.MaxTokens).Build()
		if err != nil {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{}, err
		}
		c.created.Add(1)
		return tools.OwnedRunnable[any, *builder.AgentOutput]{Runnable: agent.AsRunnable(), Close: func(context.Context) error { c.closed.Add(1); return nil }}, nil
	}, tools.RunnableToolConfig[any, *builder.AgentOutput]{
		Name: "finance_expert", Description: "财务专家：负责汇率换算与税费计算", Version: "v1", ModelInvoking: true,
		MaxDepth: opt.expertMaxDepth, CleanupTimeout: 10 * time.Second,
		ArgsSchema: `{"type":"object","properties":{"question":{"type":"string"}},"required":["question"],"additionalProperties":false}`,
		Decode: func(a map[string]any) (any, error) {
			q, _ := a["question"].(string)
			if q == "" {
				return nil, fmt.Errorf("question 不能为空")
			}
			return q, nil
		},
		Encode: func(a *builder.AgentOutput) (any, error) {
			if a == nil {
				return nil, fmt.Errorf("财务专家没有返回结果")
			}
			return a.Result, nil
		},
	})
}

var conclusionNumber = regexp.MustCompile(`结论[:：]\s*\**\s*([0-9]+(?:\.[0-9]+)?)`)

// ParseTotal 从“结论：<数字> 元”中取出金额。
func ParseTotal(answer string) (float64, error) {
	m := conclusionNumber.FindStringSubmatch(strings.ReplaceAll(answer, ",", ""))
	if m == nil {
		return 0, fmt.Errorf("回答缺少“结论：<金额>”")
	}
	return strconv.ParseFloat(m[1], 64)
}

func run(ctx context.Context, client *livemodel.Client, dir string, opt options) (Result, error) {
	var out Result
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	controller, err := budget.NewDurable(budget.Config{RootKey: "procure/q-1", Version: "v1/" + client.Live.Provider + "/" + client.Live.Model, MaxModelRequests: 10}, store)
	if err != nil {
		return out, err
	}
	ctx = budget.WithController(durable.WithJournal(durable.WithExecutionKey(ctx, "procure/q-1/agent"), store), controller)
	var c counters
	var shared []interfaces.Tool
	expert, err := expertTool(client, &c, opt, &shared)
	if err != nil {
		return out, err
	}
	managerTools := []interfaces.Tool{expert}
	if opt.shareDispatcher {
		conv, e := convertTool(&c)
		if e != nil {
			return out, e
		}
		managerTools = append(managerTools, conv, taxTool(&c))
	}
	dispatched, err := livecase.DispatchTools(managerTools)
	if err != nil {
		return out, err
	}
	shared = dispatched[1:]
	manager, err := builder.NewSimpleBuilder(client).WithDurableScope("manager").WithSystemPrompt(managerPrompt).
		WithTools(dispatched[0]).WithMaxIterations(3).WithMaxTokens(livemodel.MaxTokens).WithTimeout(6 * livemodel.RequestTimeout).Build()
	if err != nil {
		return out, err
	}
	before := client.Calls()
	answer, err := manager.AsRunnable().Invoke(ctx, Question)
	out.NewModelCalls = client.Calls() - before
	out.ExpertsCreated, out.ExpertsDone, out.ConvertRuns, out.TaxRuns = c.created.Load(), c.closed.Load(), c.convert.Load(), c.tax.Load()
	out.Budget, _ = controller.Snapshot(ctx)
	if err != nil {
		return out, err
	}
	out.Answer = fmt.Sprint(answer.Result)
	out.Total, err = ParseTotal(out.Answer)
	return out, err
}

// Run 以默认配置处理采购问题。dir 保存模型、工具日志与根预算。
func Run(ctx context.Context, client *livemodel.Client, dir string) (Result, error) {
	return run(ctx, client, dir, options{})
}

func main() {
	dir := flag.String("dir", "", "持久化目录；同一目录再次运行只回放")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "用法：delegation -dir <目录>")
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
	fmt.Printf("期望含税总价：%.2f 元\n", Expected)
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
