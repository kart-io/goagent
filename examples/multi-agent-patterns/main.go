// multi-agent-patterns 用 multiagent 预置模式离线演示客服场景 S1、S2、S3、S5、S7、S9 与组合流程，
// 模型由 testing/mocks.ScriptedClient 按脚本回答，不需要 API 密钥。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/multiagent"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

func main() {
	if err := run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, w io.Writer) error {
	dir, err := os.MkdirTemp("", "multi-agent-patterns-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	k := &kit{dir: dir}
	for _, s := range []struct {
		name string
		run  func(context.Context, io.Writer, *kit) error
	}{
		{"S1", ticketRouting},
		{"S2", replyReview},
		{"S3", parallelLookup},
		{"S5", refundApproval},
		{"S7", delegation},
		{"S9", invalidDecision},
		{"组合流程", composedPipeline},
	} {
		if err := s.run(ctx, w, k); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		_, _ = fmt.Fprintf(w, "%s 完成\n", s.name)
	}
	return nil
}

// kit 是各场景共用的装配辅助：会话工厂、文本 Step、根预算与持久图。
type kit struct{ dir string }

// agent 每次新建一个 Builder 会话；tools 经独立 Dispatcher 包装，满足持久接入契约。
func (k *kit) agent(client *mocks.ScriptedClient, scope string, list ...interfaces.Tool) (multiagent.AgentFactory, error) {
	var wrapped []interfaces.Tool
	if len(list) > 0 {
		reg := tools.NewRegistry()
		for _, t := range list {
			if err := reg.Register(t); err != nil {
				return nil, err
			}
		}
		d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
		if err != nil {
			return nil, err
		}
		wrapped = d.Tools()
	}
	return func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		b := builder.NewSimpleBuilder(client).WithDurableScope(scope)
		if len(wrapped) > 0 {
			b = b.WithTools(wrapped...).WithMaxIterations(4)
		}
		a, err := b.Build()
		if err != nil {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{}, err
		}
		return tools.OwnedRunnable[any, *builder.AgentOutput]{Runnable: a.AsRunnable(), Close: a.Shutdown}, nil
	}, nil
}

func (k *kit) text(name string, client *mocks.ScriptedClient) (multiagent.Step[multiagent.Conversation], error) {
	factory, err := k.agent(client, name)
	if err != nil {
		return multiagent.Step[multiagent.Conversation]{}, err
	}
	return multiagent.TextStep(name, factory)
}

func (k *kit) texts(clients map[string]*mocks.ScriptedClient, names ...string) ([]multiagent.Step[multiagent.Conversation], error) {
	steps := make([]multiagent.Step[multiagent.Conversation], len(names))
	for i, name := range names {
		var err error
		if steps[i], err = k.text(name, clients[name]); err != nil {
			return nil, err
		}
	}
	return steps, nil
}

// budgetFor 返回根预算上下文；持久运行的预算与图共用存储，RootKey 与运行 ID 一致。
func budgetFor(ctx context.Context, runID string, store durable.Store) (context.Context, error) {
	cfg := budget.Config{RootKey: runID, Version: "v1", MaxModelRequests: 20}
	var c *budget.Controller
	var err error
	if store == nil {
		c, err = budget.New(cfg)
	} else {
		c, err = budget.NewDurable(cfg, store)
	}
	if err != nil {
		return nil, err
	}
	return budget.WithController(ctx, c), nil
}

func invoke[T any](ctx context.Context, g *workflow.Graph[T], in T) (T, error) {
	var zero T
	run, err := g.Compile(ctx)
	if err != nil {
		return zero, err
	}
	ctx, err = budgetFor(ctx, "demo", nil)
	if err != nil {
		return zero, err
	}
	return run.Invoke(ctx, in)
}

func show(w io.Writer, c multiagent.Conversation) {
	for _, t := range c.Turns {
		_, _ = fmt.Fprintf(w, "  [%s] %s\n", t.Agent, strings.ReplaceAll(t.Output, "\n", " "))
	}
}

func reply(name, text string) mocks.ScriptedRule { return mocks.TextReply(name, text) }

// S1 工单分流：分流 Agent 依次交给退款、物流，最后 FINISH。
func ticketRouting(ctx context.Context, w io.Writer, k *kit) error {
	steps, err := k.texts(map[string]*mocks.ScriptedClient{
		"triage":    mocks.NewScriptedClient(reply("选退款", "refund"), reply("选物流", "logistics"), reply("结束", "FINISH")),
		"refund":    mocks.NewScriptedClient(reply("退款", "已登记退款")),
		"logistics": mocks.NewScriptedClient(reply("物流", "已催促承运商")),
	}, "triage", "refund", "logistics")
	if err != nil {
		return err
	}
	next, err := multiagent.TextNext(steps[1:]...)
	if err != nil {
		return err
	}
	g, err := multiagent.RouterSupervisor(workflow.Config{}, multiagent.RouterConfig[multiagent.Conversation]{
		Supervisor: steps[0], Members: steps[1:], Next: next, MaxRounds: 4,
	})
	if err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "重复扣款，且包裹三天未更新"})
	show(w, out)
	return err
}

// parseVerdict 读取最后一轮评审：PASS 通过，REJECT 开头不通过，其余包装 ErrInvalidRoute。
func parseVerdict(c multiagent.Conversation) (bool, error) {
	for i := len(c.Turns) - 1; i >= 0; i-- {
		if c.Turns[i].Agent != "review" {
			continue
		}
		line, _, _ := strings.Cut(c.Turns[i].Output, "\n")
		switch line = strings.TrimSpace(line); {
		case line == "PASS":
			return true, nil
		case strings.HasPrefix(line, "REJECT"):
			return false, nil
		}
		return false, fmt.Errorf("%w：评审结论 %q 无法解析", multiagent.ErrInvalidRoute, line)
	}
	return false, fmt.Errorf("%w：没有评审结论", multiagent.ErrInvalidRoute)
}

// S2 回复写作与评审：第一轮不通过，带意见重写后通过。
func replyReview(ctx context.Context, w io.Writer, k *kit) error {
	steps, err := k.texts(map[string]*mocks.ScriptedClient{
		"write":  mocks.NewScriptedClient(reply("初稿", "退款已处理。"), reply("二稿", "退款已处理，预计 3 个工作日到账。")),
		"review": mocks.NewScriptedClient(reply("不通过", "REJECT 缺少到账时间"), reply("通过", "PASS")),
	}, "write", "review")
	if err != nil {
		return err
	}
	g, err := multiagent.Loop(workflow.Config{}, multiagent.LoopConfig[multiagent.Conversation]{
		Steps: steps, MaxIterations: 3,
		Until: func(_ context.Context, c multiagent.Conversation) (bool, error) { return parseVerdict(c) },
	})
	if err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "客户询问退款进度"})
	show(w, out)
	return err
}

// S3 多源并行查询：知识库、订单、物流并行，结果按登记顺序合并后交给回答 Agent。
func parallelLookup(ctx context.Context, w io.Writer, k *kit) error {
	steps, err := k.texts(map[string]*mocks.ScriptedClient{
		"kb":       mocks.NewScriptedClient(reply("知识库", "七天无理由退货")),
		"order":    mocks.NewScriptedClient(reply("订单", "A1 已签收")),
		"shipping": mocks.NewScriptedClient(reply("物流", "签收于 11-01")),
		"answer":   mocks.NewScriptedClient(reply("回答", "可在 11-08 前申请退货")),
	}, "kb", "order", "shipping", "answer")
	if err != nil {
		return err
	}
	lookup, err := multiagent.ParallelStep("lookup", multiagent.ParallelConfig[multiagent.Conversation]{
		Steps: steps[:3], Clone: multiagent.CloneConversation, Merge: multiagent.MergeTurns,
	})
	if err != nil {
		return err
	}
	g, err := multiagent.Sequential(workflow.Config{}, lookup, steps[3])
	if err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "A1 还能退货吗"})
	show(w, out)
	return err
}

// refundBook 是演示用的退款账本：登记与判定同一次写入，回执按申请 ID 去重。
// 真实业务使用 workflow/refund.Service 与带事务的存储，见客服编排案例。
type refundBook struct {
	mu       sync.Mutex
	verdicts map[string]bool
	receipts map[string]string
}

type reviewInput struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note"`
	Approver string `json:"approver"`
	Reason   string `json:"reason,omitempty"`
}

// refundMember 是 S5 的退款成员：提议 → 登记并固定争议判定 → 有争议时等待审核 → 先查回执再退款。
func (b *refundBook) member(agent multiagent.AgentFactory) multiagent.Step[multiagent.Conversation] {
	return multiagent.Step[multiagent.Conversation]{Name: "refund", Run: core.NewRunnableFunc(func(ctx context.Context, c multiagent.Conversation) (multiagent.Conversation, error) {
		answer := func(text string) (multiagent.Conversation, error) {
			c.Turns = append(c.Turns, multiagent.Turn{Agent: "refund", Output: text})
			return c, nil
		}
		owned, err := agent(ctx)
		if err != nil {
			return c, err
		}
		defer func() { _ = owned.Close(context.WithoutCancel(ctx)) }()
		out, err := owned.Runnable.Invoke(ctx, c.Input)
		if err != nil {
			return c, err
		}
		var p struct {
			OrderID     string `json:"order_id"`
			AmountCents int64  `json:"amount_cents"`
		}
		if err := json.Unmarshal([]byte(out.Result.(string)), &p); err != nil {
			return c, err
		}
		id := "refund-" + p.OrderID
		intent, _ := json.Marshal(map[string]any{"request_id": id, "proposal": p})
		reg, err := durable.Do(ctx, "refund/register", intent, durable.OperationPolicy{Reconcile: b.lookupVerdict(id)}, func(context.Context) (json.RawMessage, error) {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.verdicts[id] = p.AmountCents > 50000 // 规则：超过 500 元有争议
			return json.Marshal(b.verdicts[id])
		})
		if err != nil {
			return c, err
		}
		approver := "rule:auto"
		if string(reg.Value) == "true" {
			info, _ := json.Marshal(map[string]any{"order_id": p.OrderID, "amount_cents": p.AmountCents, "reason": "金额超过 500 元"})
			raw, err := durable.AwaitInput(ctx, "refund-review", info, func(raw json.RawMessage) error {
				var in reviewInput
				if json.Unmarshal(raw, &in) != nil || (in.Approved && in.Approver == "") {
					return errors.New("审核输入需要 approved，批准时需要 approver")
				}
				return nil
			})
			if err != nil {
				return c, err
			}
			var in reviewInput
			_ = json.Unmarshal(raw, &in)
			if !in.Approved {
				return answer("人工拒绝：" + in.Note)
			}
			approver = in.Approver
		}
		intent, _ = json.Marshal(map[string]string{"request_id": id, "approver": approver})
		exec, err := durable.Do(ctx, "refund/execute", intent, durable.OperationPolicy{Reconcile: b.lookupReceipt(id)}, func(context.Context) (json.RawMessage, error) {
			b.mu.Lock()
			defer b.mu.Unlock()
			if receipt, ok := b.receipts[id]; ok { // 先查回执，已退款的申请不再批准或退款
				return json.Marshal(receipt)
			}
			b.receipts[id] = "receipt-" + id
			return json.Marshal(b.receipts[id])
		})
		if err != nil {
			return c, err
		}
		return answer("已退款，回执 " + strings.Trim(string(exec.Value), `"`) + "，审核人 " + approver)
	})}
}

func (b *refundBook) lookupVerdict(id string) func(context.Context) (json.RawMessage, bool, error) {
	return func(context.Context) (json.RawMessage, bool, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		v, ok := b.verdicts[id]
		if !ok {
			return nil, false, nil
		}
		data, err := json.Marshal(v)
		return data, err == nil, err
	}
}

func (b *refundBook) lookupReceipt(id string) func(context.Context) (json.RawMessage, bool, error) {
	return func(context.Context) (json.RawMessage, bool, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		r, ok := b.receipts[id]
		if !ok {
			return nil, false, nil
		}
		data, err := json.Marshal(r)
		return data, err == nil, err
	}
}

// S5 高风险操作审批：有争议的退款在执行前停下，审核人批准后从检查点续跑。
func refundApproval(ctx context.Context, w io.Writer, k *kit) error {
	store, err := durable.NewFileStore(k.dir)
	if err != nil {
		return err
	}
	book := &refundBook{verdicts: map[string]bool{}, receipts: map[string]string{}}
	refundAgent, err := k.agent(mocks.NewScriptedClient(reply("提议", `{"order_id":"A1","amount_cents":80000}`)), "refund")
	if err != nil {
		return err
	}
	triage, err := k.text("triage", mocks.NewScriptedClient(reply("选退款", "refund"), reply("结束", "FINISH")))
	if err != nil {
		return err
	}
	member := book.member(refundAgent)
	next, err := multiagent.TextNext(member)
	if err != nil {
		return err
	}
	g, err := multiagent.RouterSupervisor(workflow.Config{}, multiagent.RouterConfig[multiagent.Conversation]{
		Supervisor: triage, Members: []multiagent.Step[multiagent.Conversation]{member}, Next: next, MaxRounds: 2,
	})
	if err != nil {
		return err
	}
	d, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1"})
	if err != nil {
		return err
	}
	const runID = "ticket-s5"
	runCtx, err := budgetFor(ctx, runID, store)
	if err != nil {
		return err
	}
	snap, err := d.Run(runCtx, runID, multiagent.Conversation{Input: "订单 A1 申请退款 800 元"}, "")
	if !errors.Is(err, durable.ErrApprovalRequired) || len(snap.Interrupts) != 1 {
		return fmt.Errorf("应等待审核，得到 %v", err)
	}
	_, _ = fmt.Fprintf(w, "  等待审核：%s\n", snap.Interrupts[0].Info)
	// 宿主先认证审核人，再把 approver 填入回应。
	snap, err = d.Resume(runCtx, runID, workflow.ResumeRequest{Key: snap.Interrupts[0].Key, Input: json.RawMessage(`{"approved":true,"note":"核实无误","approver":"finance:li"}`)})
	if err != nil {
		return err
	}
	show(w, snap.State)
	return nil
}

// S7 复杂咨询由模型委派：主 Agent 调用账单与技术两个专家工具后汇总。
func delegation(ctx context.Context, w io.Writer, k *kit) error {
	billing, err := k.agent(mocks.NewScriptedClient(reply("账单", "确认重复扣款 1 次")), "billing")
	if err != nil {
		return err
	}
	tech, err := k.agent(mocks.NewScriptedClient(reply("技术", "需要重置密码")), "tech")
	if err != nil {
		return err
	}
	members, err := multiagent.SupervisorTools(
		multiagent.Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: billing},
		multiagent.Member{Name: "tech", Description: "技术专家", Version: "v1", Factory: tech},
	)
	if err != nil {
		return err
	}
	supervisor, err := k.agent(mocks.NewScriptedClient(
		mocks.ToolCallReply("委派", mocks.ToolCall("c1", "billing", `{"task":"核对扣款"}`), mocks.ToolCall("c2", "tech", `{"task":"排查登录"}`)),
		reply("汇总", "已为您退回重复扣款，并请按指引重置密码"),
	), "supervisor", members...)
	if err != nil {
		return err
	}
	step, err := multiagent.TextStep("supervisor", supervisor)
	if err != nil {
		return err
	}
	g, err := multiagent.Sequential(workflow.Config{}, step)
	if err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "扣款两次，而且登录不了"})
	show(w, out)
	return err
}

func transferToHuman() *multiagent.Step[multiagent.Conversation] {
	return &multiagent.Step[multiagent.Conversation]{Name: "human", Run: core.NewRunnableFunc(func(_ context.Context, c multiagent.Conversation) (multiagent.Conversation, error) {
		c.Turns = append(c.Turns, multiagent.Turn{Agent: "human", Output: "已转人工，交接上下文含 " + fmt.Sprint(len(c.Turns)) + " 轮记录"})
		return c, nil
	})}
}

// S9 判断无效转人工：分流输出无法识别时进入兜底 Step 后结束。
func invalidDecision(ctx context.Context, w io.Writer, k *kit) error {
	steps, err := k.texts(map[string]*mocks.ScriptedClient{
		"triage": mocks.NewScriptedClient(reply("无效", "退款（原因：重复扣款）")),
		"refund": mocks.NewScriptedClient(),
	}, "triage", "refund")
	if err != nil {
		return err
	}
	next, err := multiagent.TextNext(steps[1])
	if err != nil {
		return err
	}
	g, err := multiagent.RouterSupervisor(workflow.Config{}, multiagent.RouterConfig[multiagent.Conversation]{
		Supervisor: steps[0], Members: steps[1:], Next: next, MaxRounds: 3, Fallback: transferToHuman(),
	})
	if err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "重复扣款"})
	show(w, out)
	return err
}

const reviewRounds = 3 // MaxSteps = 1 + 2*reviewRounds + 1

// composedPipeline 手写“抽取 → 写作⇄评审 → 合规”，用 AddStep 复用 Step，用 GuardRoute 复用兜底。
func composedPipeline(ctx context.Context, w io.Writer, k *kit) error {
	steps, err := k.texts(map[string]*mocks.ScriptedClient{
		"extract":    mocks.NewScriptedClient(reply("抽取", "订单 A1，退款")),
		"write":      mocks.NewScriptedClient(reply("初稿", "退款已处理。"), reply("二稿", "退款已处理，3 个工作日到账。")),
		"review":     mocks.NewScriptedClient(reply("不通过", "REJECT 缺少到账时间"), reply("通过", "PASS")),
		"compliance": mocks.NewScriptedClient(reply("合规", "合规")),
	}, "extract", "write", "review", "compliance")
	if err != nil {
		return err
	}
	g := workflow.NewGraph[multiagent.Conversation](workflow.Config{MaxSteps: 1 + 2*reviewRounds + 1})
	for _, s := range append(steps, *transferToHuman()) {
		if err := multiagent.AddStep(g, s); err != nil {
			return err
		}
	}
	for _, e := range [][2]string{{workflow.START, "extract"}, {"extract", "write"}, {"write", "review"}, {"compliance", workflow.END}, {"human", workflow.END}} {
		if err := g.AddEdge(e[0], e[1]); err != nil {
			return err
		}
	}
	reviewExit := func(_ context.Context, c multiagent.Conversation) (string, error) {
		passed, err := parseVerdict(c)
		if err != nil {
			return "", err
		}
		if passed {
			return "compliance", nil
		}
		rounds := 0
		for _, t := range c.Turns {
			if t.Agent == "review" {
				rounds++
			}
		}
		if rounds >= reviewRounds {
			return "", fmt.Errorf("%w: review loop limit %d", workflow.ErrMaxSteps, reviewRounds)
		}
		return "write", nil
	}
	route, targets, err := multiagent.GuardRoute(reviewExit, []string{"write", "compliance"}, "human")
	if err != nil {
		return err
	}
	if err := g.AddBranch("review", route, targets...); err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "我要退款"})
	show(w, out)
	return err
}
