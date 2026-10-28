// 工单分诊：父 Agent 先用规则图查订单，再把政策判断交给独立会话的子 Agent。
// 父子共用一份持久根预算；用同一目录再次运行只回放，不发新的模型请求。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

// order 是合成数据，DeliveredDaysAgo 以“今天”为基准，避免依赖真实时钟。
type order struct {
	ID               string `json:"order_id"`
	Category         string `json:"category"`
	Opened           bool   `json:"opened"`
	DeliveredDaysAgo int    `json:"delivered_days_ago"`
	WithinWindow     bool   `json:"within_7_day_window"`
}

var orders = map[string]order{
	"A1001": {ID: "A1001", Category: "电子配件", Opened: false, DeliveredDaysAgo: 3},
	"A1002": {ID: "A1002", Category: "电子配件", Opened: false, DeliveredDaysAgo: 20},
	"A1003": {ID: "A1003", Category: "定制刻字", Opened: false, DeliveredDaysAgo: 2},
}

// Tickets 列出案例工单及按政策应得的结论。
var Tickets = map[string]struct{ Text, Expected string }{
	"A1001": {"订单 A1001 申请退款：耳机还没拆封，不想要了。", "批准"},
	"A1002": {"订单 A1002 申请退款：充电器没拆封，但最近才有空处理。", "拒绝"},
	"A1003": {"订单 A1003 申请退款：刻字钢笔未使用，想退。", "拒绝"},
}

const refundPolicy = `退款政策 v3：
1. 签收 7 天内（含第 7 天）且商品未拆封，可无理由退款。
2. 定制类商品（如刻字、定制尺寸）不支持无理由退款。
3. 超过 7 天不支持无理由退款，质量问题走售后流程。`

const parentPrompt = `你是售后工单分诊助手。必须严格按以下步骤处理：
1. 调用 order_lookup 查询工单中的订单。
2. 调用 policy_agent，把订单查询结果原样写进 question，询问是否符合退款政策。
3. 根据两个工具的结果作答。第一行必须是“结论：批准”或“结论：拒绝”，第二行写一句理由。
不要编造订单数据，不要跳过任何工具。`

// Result 汇总一次运行的业务结果与计量证据。
type Result struct {
	Ticket, Decision, Answer string
	NewModelCalls            int64
	LookupRuns               int64
	ChildCreated, ChildClose int64
	Budget                   budget.Snapshot
}

func orderLookupTool(runs *atomic.Int64) (interfaces.Tool, error) {
	g := workflow.NewGraph[order]()
	if err := g.AddNode("load", func(_ context.Context, o order) (order, error) {
		found, ok := orders[o.ID]
		if !ok {
			return o, fmt.Errorf("订单 %s 不存在", o.ID)
		}
		runs.Add(1)
		return found, nil
	}); err != nil {
		return nil, err
	}
	if err := g.AddNode("window", func(_ context.Context, o order) (order, error) {
		o.WithinWindow = o.DeliveredDaysAgo <= 7
		return o, nil
	}); err != nil {
		return nil, err
	}
	for _, e := range [][2]string{{workflow.START, "load"}, {"load", "window"}, {"window", workflow.END}} {
		if err := g.AddEdge(e[0], e[1]); err != nil {
			return nil, err
		}
	}
	run, err := g.Compile(context.Background())
	if err != nil {
		return nil, err
	}
	return tools.NewRunnableTool(run, tools.RunnableToolConfig[order, order]{
		Name: "order_lookup", Description: "按订单号查询订单类目、是否拆封、签收天数及是否在 7 天窗口内", Version: "v1",
		ArgsSchema: `{"type":"object","properties":{"order_id":{"type":"string","description":"订单号，如 A1001"}},"required":["order_id"],"additionalProperties":false}`,
		Decode: func(args map[string]any) (order, error) {
			id, _ := args["order_id"].(string)
			return order{ID: id}, nil
		},
		Encode: func(o order) (any, error) { return o, nil },
	})
}

// policyAgentTool 每次调用新建子 Agent 和唯一会话；父对话不会传入子会话。
func policyAgentTool(client *livemodel.Client, mem interfaces.MemoryManager, created, closed *atomic.Int64) (interfaces.Tool, error) {
	return tools.NewRunnableToolFactory(func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		child, err := builder.NewSimpleBuilder(client).WithDurableScope("policy").
			WithSystemPrompt(refundPolicy + "\n你只依据上述政策判断，回答“符合”或“不符合”并引用条款编号，不超过两句话。").
			WithMemory(mem).WithSessionID("policy/" + uuid.NewString()).WithMaxIterations(2).WithMaxTokens(livemodel.MaxTokens).Build()
		if err != nil {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{}, err
		}
		created.Add(1)
		return tools.OwnedRunnable[any, *builder.AgentOutput]{Runnable: child.AsRunnable(), Close: func(context.Context) error { closed.Add(1); return nil }}, nil
	}, tools.RunnableToolConfig[any, *builder.AgentOutput]{
		Name: "policy_agent", Description: "退款政策专家：判断给定订单信息是否符合退款政策", Version: "v1", ModelInvoking: true, CleanupTimeout: 10 * time.Second,
		ArgsSchema: `{"type":"object","properties":{"question":{"type":"string","description":"包含订单查询结果的问题"}},"required":["question"],"additionalProperties":false}`,
		Decode: func(args map[string]any) (any, error) {
			q, _ := args["question"].(string)
			if q == "" {
				return nil, fmt.Errorf("question 不能为空")
			}
			return q, nil
		},
		Encode: func(a *builder.AgentOutput) (any, error) {
			if a == nil {
				return nil, fmt.Errorf("子 Agent 没有返回结果")
			}
			return a.Result, nil
		},
	})
}

// Run 处理一张工单。dir 保存模型、工具日志与根预算账本。
func Run(ctx context.Context, client *livemodel.Client, dir, ticketID string) (Result, error) {
	out := Result{Ticket: ticketID}
	ticket, ok := Tickets[ticketID]
	if !ok {
		return out, fmt.Errorf("未知工单 %s", ticketID)
	}
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	// 版本绑定模型，换模型时不会误用旧账本。
	controller, err := budget.NewDurable(budget.Config{RootKey: "ticket/" + ticketID, Version: "v1/" + client.Live.Provider + "/" + client.Live.Model, MaxModelRequests: 8}, store)
	if err != nil {
		return out, err
	}
	ctx = budget.WithController(durable.WithJournal(durable.WithExecutionKey(ctx, "ticket/"+ticketID+"/agent"), store), controller)
	var lookups, created, closed atomic.Int64
	lookup, err := orderLookupTool(&lookups)
	if err != nil {
		return out, err
	}
	policy, err := policyAgentTool(client, memory.NewInMemoryManager(nil), &created, &closed)
	if err != nil {
		return out, err
	}
	business, err := livecase.DispatchTools([]interfaces.Tool{lookup, policy})
	if err != nil {
		return out, err
	}
	agent, err := builder.NewSimpleBuilder(client).WithDurableScope("triage").WithSystemPrompt(parentPrompt).
		WithTools(business...).WithMaxIterations(5).WithMaxTokens(livemodel.MaxTokens).WithTimeout(5 * livemodel.RequestTimeout).Build()
	if err != nil {
		return out, err
	}
	before := client.Calls()
	answer, err := agent.AsRunnable().Invoke(ctx, ticket.Text)
	out.NewModelCalls = client.Calls() - before
	out.LookupRuns, out.ChildCreated, out.ChildClose = lookups.Load(), created.Load(), closed.Load()
	if snapshot, e := controller.Snapshot(ctx); e == nil {
		out.Budget = snapshot
	}
	if err != nil {
		return out, err
	}
	out.Answer = fmt.Sprint(answer.Result)
	out.Decision, err = livecase.Decision(out.Answer, "批准", "拒绝")
	return out, err
}

func main() {
	dir := flag.String("dir", "", "持久化目录；再次使用同一目录会回放")
	ticket := flag.String("ticket", "A1001", "工单订单号：A1001、A1002 或 A1003")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "用法：ticket-triage -dir <目录> [-ticket A1001]")
		os.Exit(2)
	}
	client, err := livemodel.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := Run(context.Background(), client, *dir, *ticket)
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
