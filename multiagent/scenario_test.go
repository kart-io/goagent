package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/workflow"
)

var modes = []struct {
	name    string
	durable bool
}{{"普通", false}, {"持久", true}}

// execute 用普通或持久方式执行图，返回最终状态。
func execute[T any](t *testing.T, g *workflow.Graph[T], durableMode bool, init T) (T, *workflow.GraphSnapshot[T], error) {
	t.Helper()
	if !durableMode {
		run, err := g.Compile(context.Background())
		require.NoError(t, err)
		out, err := run.Invoke(memoryBudget(t), init)
		return out, nil, err
	}
	e := newEnv(t, "scenario")
	snap, err := compile(t, e, g, "v1").Run(e.ctx(), e.id, init, "")
	var out T
	if snap != nil {
		out = snap.State
	}
	return out, snap, err
}

func textAgent(t *testing.T, name string, client *mocks.ScriptedClient, opts ...TextOption) Step[Conversation] {
	t.Helper()
	var s sessions
	step, err := TextStep(name, s.scripted(client, name, nil), opts...)
	require.NoError(t, err)
	return step
}

func agents(c Conversation) []string {
	out := make([]string, len(c.Turns))
	for i, turn := range c.Turns {
		out[i] = turn.Agent
	}
	return out
}

// parseVerdict 读取最后一轮评审结论：首行 PASS 为通过，以 REJECT 开头为不通过，其余无法解析。
func parseVerdict(c Conversation) (bool, error) {
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
		return false, fmt.Errorf("%w：评审结论 %q 无法解析", ErrInvalidRoute, line)
	}
	return false, fmt.Errorf("%w：没有评审结论", ErrInvalidRoute)
}

func untilPassed(_ context.Context, c Conversation) (bool, error) { return parseVerdict(c) }

// transferToHuman 是转人工兜底：记录收到的状态并追加交接结果。
func transferToHuman(received *[]Conversation) *Step[Conversation] {
	return &Step[Conversation]{Name: "human", Run: core.NewRunnableFunc(func(_ context.Context, c Conversation) (Conversation, error) {
		*received = append(*received, CloneConversation(c))
		c.Turns = append(c.Turns, Turn{Agent: "human", Output: "已转人工"})
		return c, nil
	})}
}

func TestScenarioTicketRouting(t *testing.T) {
	const ticketText = "工单：重复扣款，且包裹三天未更新"
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			triage := mocks.NewScriptedClient(
				mocks.ScriptedRule{Name: "选退款", Match: mocks.RequestContains(ticketText), Reply: mocks.ScriptedReply{Text: "refund"}},
				mocks.ScriptedRule{Name: "选物流", Match: mocks.RequestContains(ticketText, "[refund]: 已登记退款"), Reply: mocks.ScriptedReply{Text: "logistics"}},
				mocks.ScriptedRule{Name: "结束", Match: mocks.RequestContains(ticketText, "[logistics]: 已催单"), Reply: mocks.ScriptedReply{Text: "FINISH"}},
			)
			refund := textAgent(t, "refund", mocks.NewScriptedClient(mocks.TextReply("退款", "已登记退款")))
			logistics := textAgent(t, "logistics", mocks.NewScriptedClient(mocks.TextReply("物流", "已催单")))
			next, err := TextNext(refund, logistics)
			require.NoError(t, err)
			g, err := RouterSupervisor(workflow.Config{}, RouterConfig[Conversation]{
				Supervisor: textAgent(t, "triage", triage),
				Members:    []Step[Conversation]{refund, logistics}, Next: next, MaxRounds: 4,
			})
			require.NoError(t, err)
			out, _, err := execute(t, g, mode.durable, Conversation{Input: ticketText})
			require.NoError(t, err)
			require.Equal(t, []string{"triage", "refund", "triage", "logistics", "triage"}, agents(out))
			require.Equal(t, 3, triage.Calls(), "分流角色共享一个脚本实例")
			for _, req := range triage.Requests() {
				require.True(t, mocks.RequestContains(ticketText)(&req), "每次 Supervisor 请求都包含原始工单")
			}
		})
	}
}

func TestScenarioReplyReview(t *testing.T) {
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			writer := mocks.NewScriptedClient(
				mocks.TextReply("初稿", "您好，退款已处理。"),
				mocks.ScriptedRule{Name: "按意见重写", Match: mocks.RequestContains("[write]: 您好，退款已处理。", "[review]: REJECT 缺少到账时间"), Reply: mocks.ScriptedReply{Text: "您好，退款已处理，预计 3 个工作日到账。"}},
			)
			reviewer := mocks.NewScriptedClient(mocks.TextReply("不通过", "REJECT 缺少到账时间"), mocks.TextReply("通过", "PASS"))
			g, err := Loop(workflow.Config{}, LoopConfig[Conversation]{
				Steps: []Step[Conversation]{textAgent(t, "write", writer), textAgent(t, "review", reviewer)},
				Until: untilPassed, MaxIterations: 3,
			})
			require.NoError(t, err)
			out, _, err := execute(t, g, mode.durable, Conversation{Input: "客户询问退款进度"})
			require.NoError(t, err)
			require.Equal(t, []string{"write", "review", "write", "review"}, agents(out))
			require.Equal(t, "PASS", out.Turns[3].Output)
			require.Zero(t, writer.Remaining(), "第二轮请求包含上一稿与评审意见")
		})
	}
}

func TestScenarioParallelLookup(t *testing.T) {
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			lookup, err := ParallelStep("lookup", ParallelConfig[Conversation]{Steps: []Step[Conversation]{
				textAgent(t, "kb", mocks.NewScriptedClient(mocks.TextReply("知识库", "七天无理由"))),
				textAgent(t, "order", mocks.NewScriptedClient(mocks.TextReply("订单", "A1 已签收"))),
				textAgent(t, "shipping", mocks.NewScriptedClient(mocks.TextReply("物流", "签收于 11-01"))),
			}, Clone: CloneConversation, Merge: MergeTurns})
			require.NoError(t, err)
			answer := mocks.NewScriptedClient(mocks.ScriptedRule{
				Name:  "汇总",
				Match: mocks.RequestContains("[kb]: 七天无理由", "[order]: A1 已签收", "[shipping]: 签收于 11-01"),
				Reply: mocks.ScriptedReply{Text: "可在 11-08 前退货"},
			})
			g, err := Sequential(workflow.Config{}, lookup, textAgent(t, "answer", answer))
			require.NoError(t, err)
			out, snap, err := execute(t, g, mode.durable, Conversation{Input: "A1 能退货吗"})
			require.NoError(t, err)
			require.Equal(t, []string{"kb", "order", "shipping", "answer"}, agents(out), "结果按登记顺序合并")
			if snap != nil {
				require.Equal(t, 2, snap.Steps, "整组并行计 1 步")
			}
		})
	}
}

type caseFile struct {
	Ticket  string
	OrderID string
	Answer  string
}

func TestScenarioPipeline(t *testing.T) {
	build := func(t *testing.T, verdict string) *workflow.Graph[caseFile] {
		var s sessions
		extract, err := AgentStep("extract", s.scripted(mocks.NewScriptedClient(mocks.TextReply("抽取", `{"order_id":"A1","type":"refund"}`)), "extract", nil),
			func(c caseFile) (any, error) { return c.Ticket, nil },
			func(c caseFile, o *builder.AgentOutput) (caseFile, error) {
				var fields struct {
					OrderID string `json:"order_id"`
				}
				if err := json.Unmarshal([]byte(o.Result.(string)), &fields); err != nil {
					return c, err
				}
				c.OrderID = fields.OrderID
				return c, nil
			})
		require.NoError(t, err)
		answer, err := AgentStep("answer", s.scripted(mocks.NewScriptedClient(mocks.ScriptedRule{Name: "回答", Match: mocks.RequestContains("订单 A1"), Reply: mocks.ScriptedReply{Text: "A1 已退款"}}), "answer", nil),
			func(c caseFile) (any, error) { return "订单 " + c.OrderID + "：" + c.Ticket, nil },
			func(c caseFile, o *builder.AgentOutput) (caseFile, error) {
				c.Answer = o.Result.(string)
				return c, nil
			})
		require.NoError(t, err)
		compliance, err := AgentStep("compliance", s.scripted(mocks.NewScriptedClient(mocks.ScriptedRule{Name: "合规", Match: mocks.RequestContains("A1", "A1 已退款"), Reply: mocks.ScriptedReply{Text: verdict}}), "compliance", nil),
			func(c caseFile) (any, error) { return c.OrderID + "\n" + c.Answer, nil },
			func(c caseFile, o *builder.AgentOutput) (caseFile, error) {
				if o.Result != "PASS" {
					return c, fmt.Errorf("合规检查不通过：%v", o.Result)
				}
				return c, nil
			})
		require.NoError(t, err)
		g, err := Sequential(workflow.Config{}, extract, answer, compliance)
		require.NoError(t, err)
		return g
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			out, _, err := execute(t, build(t, "PASS"), mode.durable, caseFile{Ticket: "我要退款"})
			require.NoError(t, err)
			require.Equal(t, caseFile{Ticket: "我要退款", OrderID: "A1", Answer: "A1 已退款"}, out)
			_, _, err = execute(t, build(t, "含承诺性措辞"), mode.durable, caseFile{Ticket: "我要退款"})
			require.ErrorContains(t, err, `node "compliance"`)
			require.ErrorContains(t, err, "合规检查不通过")
		})
	}
}

func TestScenarioDelegation(t *testing.T) {
	t.Run("普通成员", func(t *testing.T) {
		members := runDelegation(t)
		require.EqualValues(t, 2, members.closed.Load())
	})
	t.Run("Resumable 成员", runResumableDelegation)
}

func TestScenarioEscalation(t *testing.T) {
	loop := func(t *testing.T, reviewer *mocks.ScriptedClient, writer *mocks.ScriptedClient) *workflow.Graph[Conversation] {
		g, err := Loop(workflow.Config{}, LoopConfig[Conversation]{
			Steps: []Step[Conversation]{textAgent(t, "write", writer), textAgent(t, "review", reviewer)},
			Until: untilPassed, MaxIterations: 2,
		})
		require.NoError(t, err)
		return g
	}
	always := func(text string, n int) *mocks.ScriptedClient {
		rules := make([]mocks.ScriptedRule, n)
		for i := range rules {
			rules[i] = mocks.TextReply(fmt.Sprintf("第 %d 次", i+1), text)
		}
		return mocks.NewScriptedClient(rules...)
	}
	t.Run("Loop 普通", func(t *testing.T) {
		_, _, err := execute(t, loop(t, always("REJECT 仍不合格", 2), always("稿", 2)), false, Conversation{Input: "q"})
		require.ErrorIs(t, err, workflow.ErrMaxSteps)
	})
	t.Run("Router 普通", func(t *testing.T) {
		refund := textAgent(t, "refund", always("处理中", 2))
		next, err := TextNext(refund)
		require.NoError(t, err)
		triage := always("refund", 3)
		g, err := RouterSupervisor(workflow.Config{}, RouterConfig[Conversation]{Supervisor: textAgent(t, "triage", triage), Members: []Step[Conversation]{refund}, Next: next, MaxRounds: 2})
		require.NoError(t, err)
		_, _, err = execute(t, g, false, Conversation{Input: "q"})
		require.ErrorIs(t, err, workflow.ErrMaxSteps)
		require.Equal(t, 3, triage.Calls(), "第 MaxRounds 次成员后 Supervisor 再请求一次")
	})
	t.Run("持久转人工", func(t *testing.T) {
		e := newEnv(t, "ticket-1")
		reviewer, writer := always("REJECT 仍不合格", 2), always("稿", 2)
		d := compile(t, e, loop(t, reviewer, writer), "v1")
		_, err := d.Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.ErrorIs(t, err, workflow.ErrMaxSteps)
		_, err = d.Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.ErrorIs(t, err, workflow.ErrMaxSteps, "同一运行 ID 再 Run 仍报错")
		require.Equal(t, 2, reviewer.Calls())

		e.renew("ticket-1-human")
		snap, err := compile(t, e, loop(t, always("PASS", 1), always("人工改写稿", 1)), "v1").Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.NoError(t, err, "换运行 ID 与新根预算后可重新处理")
		require.Equal(t, "PASS", snap.State.Turns[1].Output)
	})
}

func TestScenarioInvalidDecision(t *testing.T) {
	for _, mode := range modes {
		t.Run("Router/"+mode.name, func(t *testing.T) {
			var received []Conversation
			refund := textAgent(t, "refund", mocks.NewScriptedClient())
			next, err := TextNext(refund)
			require.NoError(t, err)
			g, err := RouterSupervisor(workflow.Config{}, RouterConfig[Conversation]{
				Supervisor: textAgent(t, "triage", mocks.NewScriptedClient(mocks.TextReply("无效", "退款（原因：重复扣款）"))),
				Members:    []Step[Conversation]{refund}, Next: next, MaxRounds: 3, Fallback: transferToHuman(&received),
			})
			require.NoError(t, err)
			out, _, err := execute(t, g, mode.durable, Conversation{Input: "重复扣款"})
			require.NoError(t, err)
			require.Equal(t, []string{"triage", "human"}, agents(out))
			require.Len(t, received, 1)
			require.Equal(t, "退款（原因：重复扣款）", received[0].Turns[0].Output)
		})
		t.Run("Loop/"+mode.name, func(t *testing.T) {
			var received []Conversation
			g, err := Loop(workflow.Config{}, LoopConfig[Conversation]{Steps: []Step[Conversation]{
				textAgent(t, "write", mocks.NewScriptedClient(mocks.TextReply("稿", "回复稿"))),
				textAgent(t, "review", mocks.NewScriptedClient(mocks.TextReply("无法解析", "看起来还行吧"))),
			}, Until: untilPassed, MaxIterations: 3, Fallback: transferToHuman(&received)})
			require.NoError(t, err)
			out, _, err := execute(t, g, mode.durable, Conversation{Input: "q"})
			require.NoError(t, err)
			require.Equal(t, []string{"write", "review", "human"}, agents(out))
			require.Equal(t, "看起来还行吧", received[0].Turns[1].Output)
		})
	}
}

const reviewRounds = 2 // MaxSteps = 3 + 2*reviewRounds：抽取、并行查询、合规各 1 步

// reviewExit 是组合流程中评审后的出口路由：先看解析错误，再看是否通过，最后从状态数出轮数。
func reviewExit(_ context.Context, c Conversation) (string, error) {
	passed, err := parseVerdict(c)
	if err != nil {
		return "", err
	}
	if passed {
		return "compliance", nil
	}
	rounds := 0
	for _, turn := range c.Turns {
		if turn.Agent == "review" {
			rounds++
		}
	}
	if rounds >= reviewRounds {
		return "", fmt.Errorf("%w: review loop limit %d", workflow.ErrMaxSteps, reviewRounds)
	}
	return "write", nil
}

type composedScripts struct{ writer, reviewer, compliance *mocks.ScriptedClient }

func composedGraph(t *testing.T, sc composedScripts, received *[]Conversation) *workflow.Graph[Conversation] {
	t.Helper()
	g := workflow.NewGraph[Conversation](workflow.Config{MaxSteps: 3 + 2*reviewRounds})
	lookup, err := ParallelStep("lookup", ParallelConfig[Conversation]{Steps: []Step[Conversation]{
		textAgent(t, "kb", mocks.NewScriptedClient(mocks.TextReply("知识库", "七天无理由"))),
		textAgent(t, "order", mocks.NewScriptedClient(mocks.TextReply("订单", "A1 已签收"))),
	}, Clone: CloneConversation, Merge: MergeTurns})
	require.NoError(t, err)
	for _, s := range []Step[Conversation]{
		textAgent(t, "extract", mocks.NewScriptedClient(mocks.TextReply("抽取", "A1"))), lookup,
		textAgent(t, "write", sc.writer), textAgent(t, "review", sc.reviewer), textAgent(t, "compliance", sc.compliance), *transferToHuman(received),
	} {
		require.NoError(t, AddStep(g, s))
	}
	for _, edge := range [][2]string{{workflow.START, "extract"}, {"extract", "lookup"}, {"lookup", "write"}, {"write", "review"}, {"compliance", workflow.END}, {"human", workflow.END}} {
		require.NoError(t, g.AddEdge(edge[0], edge[1]))
	}
	route, targets, err := GuardRoute(reviewExit, []string{"write", "compliance"}, "human")
	require.NoError(t, err)
	require.NoError(t, g.AddBranch("review", route, targets...))
	return g
}

func TestScenarioComposedPipeline(t *testing.T) {
	script := func(texts ...string) *mocks.ScriptedClient {
		rules := make([]mocks.ScriptedRule, len(texts))
		for i, text := range texts {
			rules[i] = mocks.TextReply(fmt.Sprintf("第 %d 次", i+1), text)
		}
		return mocks.NewScriptedClient(rules...)
	}
	for _, mode := range modes {
		t.Run("第 N 轮通过/"+mode.name, func(t *testing.T) {
			sc := composedScripts{script("稿一", "稿二"), script("REJECT 太长", "PASS"), script("合规")}
			out, snap, err := execute(t, composedGraph(t, sc, nil), mode.durable, Conversation{Input: "A1 能退货吗"})
			require.NoError(t, err, "MaxSteps 恰好够用")
			require.Equal(t, []string{"extract", "kb", "order", "write", "review", "write", "review", "compliance"}, agents(out))
			if snap != nil {
				require.Equal(t, 3+2*reviewRounds, snap.Steps)
			}
		})
		t.Run("评审结论无法解析/"+mode.name, func(t *testing.T) {
			var received []Conversation
			sc := composedScripts{script("稿一"), script("嗯"), script()}
			out, _, err := execute(t, composedGraph(t, sc, &received), mode.durable, Conversation{Input: "q"})
			require.NoError(t, err)
			require.Equal(t, "human", out.Turns[len(out.Turns)-1].Agent)
			require.Len(t, received, 1)
		})
	}
	t.Run("始终不通过/普通", func(t *testing.T) {
		sc := composedScripts{script("稿一", "稿二"), script("REJECT", "REJECT"), script()}
		_, _, err := execute(t, composedGraph(t, sc, nil), false, Conversation{Input: "q"})
		require.ErrorIs(t, err, workflow.ErrMaxSteps)
		require.Equal(t, reviewRounds, sc.writer.Calls())
		require.Equal(t, reviewRounds, sc.reviewer.Calls())
		require.Zero(t, sc.compliance.Calls())
	})
	t.Run("始终不通过/持久", func(t *testing.T) {
		sc := composedScripts{script("稿一", "稿二"), script("REJECT", "REJECT"), script()}
		e := newEnv(t, "composed")
		d := compile(t, e, composedGraph(t, sc, nil), "v1")
		_, err := d.Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.ErrorIs(t, err, workflow.ErrMaxSteps)
		snap, err := d.Snapshot(context.Background(), e.id)
		require.NoError(t, err)
		require.Equal(t, "review", snap.Node)
		require.True(t, snap.Routing)
		require.Equal(t, 2+2*reviewRounds, snap.Steps)
		_, err = d.Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.ErrorIs(t, err, workflow.ErrMaxSteps, "同一运行 ID 再 Run 重新路由，得到同样的错误")
		require.Equal(t, reviewRounds, sc.writer.Calls())
		require.Equal(t, reviewRounds, sc.reviewer.Calls())
		require.Zero(t, sc.compliance.Calls())
	})
}

var orderIDPattern = regexp.MustCompile(`^A\d+$`)

// validateOrderID 是 AwaitInput 的纯函数校验：格式不对时保持等待，宿主用同一中断键提交修正值。
func validateOrderID(raw json.RawMessage) error {
	var id string
	if err := json.Unmarshal(raw, &id); err != nil || !orderIDPattern.MatchString(id) {
		return errors.New("订单号格式应为 A 加数字")
	}
	return nil
}

// askCustomerStep 抽取订单号，缺失时暂停向客户追问；补充后用另一个 scope 再次抽取。
func askCustomerStep(t *testing.T, client *mocks.ScriptedClient) Step[caseFile] {
	t.Helper()
	var s sessions
	invoke := func(ctx context.Context, scope, input string) (string, error) {
		owned, err := s.scripted(client, scope, nil)(ctx)
		if err != nil {
			return "", err
		}
		defer func() { _ = owned.Close(context.WithoutCancel(ctx)) }()
		out, err := owned.Runnable.Invoke(ctx, input)
		if err != nil {
			return "", err
		}
		return out.Result.(string), nil
	}
	return Step[caseFile]{Name: "extract", Run: core.NewRunnableFunc(func(ctx context.Context, c caseFile) (caseFile, error) {
		text, err := invoke(ctx, "extract", c.Ticket)
		if err != nil {
			return c, err
		}
		var fields struct {
			OrderID string `json:"order_id"`
		}
		if err := json.Unmarshal([]byte(text), &fields); err != nil {
			return c, err
		}
		if fields.OrderID == "" {
			raw, err := durable.AwaitInput(ctx, "order-id", json.RawMessage(`{"question":"请提供订单号"}`), validateOrderID)
			if err != nil {
				return c, err
			}
			_ = json.Unmarshal(raw, &fields.OrderID)
			// 同一节点内第二次调用 Agent 必须换 scope，否则与第一次调用的意图冲突。
			if c.Answer, err = invoke(ctx, "extract-confirm", "确认订单 "+fields.OrderID+"："+c.Ticket); err != nil {
				return c, err
			}
		}
		c.OrderID = fields.OrderID
		return c, nil
	})}
}

func TestScenarioAskCustomer(t *testing.T) {
	scripts := func() *mocks.ScriptedClient {
		return mocks.NewScriptedClient(mocks.TextReply("抽取", `{"order_id":""}`),
			mocks.ScriptedRule{Name: "确认", Match: mocks.RequestContains("A123"), Reply: mocks.ScriptedReply{Text: "订单 A123 已确认"}})
	}
	graph := func(t *testing.T, e *env, client *mocks.ScriptedClient) *workflow.DurableGraph[caseFile] {
		g, err := Sequential(workflow.Config{}, askCustomerStep(t, client))
		require.NoError(t, err)
		return compile(t, e, g, "v1")
	}
	t.Run("追问与格式校验", func(t *testing.T) {
		e := newEnv(t, "ask-customer")
		client := scripts()
		snap, err := graph(t, e, client).Run(e.ctx(), e.id, caseFile{Ticket: "我要退款"}, "")
		require.ErrorIs(t, err, durable.ErrApprovalRequired)
		require.Len(t, snap.Interrupts, 1)
		key := snap.Interrupts[0].Key

		e.restart()
		d := graph(t, e, client)
		_, err = d.Resume(e.ctx(), e.id, workflow.ResumeRequest{Key: key, Input: json.RawMessage(`"123"`)})
		var rejected *workflow.ResumeRejectedError
		require.ErrorAs(t, err, &rejected)
		snap, err = d.Snapshot(context.Background(), e.id)
		require.NoError(t, err)
		require.True(t, snap.Waiting)
		require.True(t, snap.Interrupts[0].Rejected)
		require.Equal(t, 1, client.Calls(), "续跑时抽取 Agent 从日志回放")

		snap, err = d.Resume(e.ctx(), e.id, workflow.ResumeRequest{Key: key, Input: json.RawMessage(`"A123"`)})
		require.NoError(t, err)
		require.Equal(t, "A123", snap.State.OrderID)
		require.Equal(t, "订单 A123 已确认", snap.State.Answer)
		require.Equal(t, 2, client.Calls(), "只多出补充后的那次抽取，它使用另一个 scope")
	})
	for name, window := range map[string]time.Duration{"截止时间已过": 300 * time.Millisecond, "截止时间覆盖等待": time.Hour} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, "ask-deadline")
			e.budgetDir = t.TempDir() // 截止时间属于根预算配置，运行开始前确定，之后不可改
			e.budget.DeadlineUnixMilli = time.Now().Add(window).UnixMilli()
			e.restart()
			client := scripts()
			snap, err := graph(t, e, client).Run(e.ctx(), e.id, caseFile{Ticket: "我要退款"}, "")
			require.ErrorIs(t, err, durable.ErrApprovalRequired)
			if window < time.Second {
				time.Sleep(window) // 客户隔了一段时间才回复
			}
			e.restart()
			_, err = graph(t, e, client).Resume(e.ctx(), e.id, workflow.ResumeRequest{Key: snap.Interrupts[0].Key, Input: json.RawMessage(`"A123"`)})
			if window < time.Second {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				return
			}
			require.NoError(t, err)
		})
	}
}

// refundCase 装配 S5：分流 → 退款成员（登记、审核、执行）→ 分流 FINISH。
type refundCase struct {
	e              *env
	desk           *refundDesk
	triage, refund *mocks.ScriptedClient
}

func newRefundCase(t *testing.T, root string, amount, threshold int64, agentDisputed bool) *refundCase {
	t.Helper()
	p, err := json.Marshal(proposal{OrderID: "A1", AmountCents: amount, Disputed: agentDisputed, Reason: "客户情绪激动"})
	require.NoError(t, err)
	return &refundCase{
		e: newEnv(t, root), desk: newRefundDesk(100000, threshold),
		triage: mocks.NewScriptedClient(mocks.TextReply("选退款", "refund"),
			mocks.ScriptedRule{Name: "结束", Match: mocks.RequestContains("[refund]: "), Reply: mocks.ScriptedReply{Text: "FINISH"}}),
		refund: mocks.NewScriptedClient(mocks.TextReply("提议", string(p))),
	}
}

func (rc *refundCase) graph(t *testing.T) *workflow.DurableGraph[Conversation] {
	t.Helper()
	var s sessions
	member := refundMember(rc.desk, s.scripted(rc.refund, "refund", nil))
	next, err := TextNext(member)
	require.NoError(t, err)
	g, err := RouterSupervisor(workflow.Config{}, RouterConfig[Conversation]{
		Supervisor: textAgent(t, "triage", rc.triage),
		Members:    []Step[Conversation]{member}, Next: next, MaxRounds: 2,
	})
	require.NoError(t, err)
	return compile(t, rc.e, g, "v1")
}

// start 运行到审核等待，返回中断。
func (rc *refundCase) start(t *testing.T) workflow.InterruptPoint {
	t.Helper()
	snap, err := rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{Input: "订单 A1 重复扣款，申请退款"}, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.Len(t, snap.Interrupts, 1)
	require.Equal(t, "refund-review", snap.Interrupts[0].ID)
	require.Zero(t, rc.desk.ledger.refunds(), "等待期间账本没有退款")
	return snap.Interrupts[0]
}

func (rc *refundCase) answer(t *testing.T, key, input string) (*workflow.GraphSnapshot[Conversation], error) {
	t.Helper()
	rc.e.restart()
	return rc.graph(t).Resume(rc.e.ctx(), rc.e.id, workflow.ResumeRequest{Key: key, Input: json.RawMessage(input)})
}

func lastOutput(c Conversation, agent string) string {
	for i := len(c.Turns) - 1; i >= 0; i-- {
		if c.Turns[i].Agent == agent {
			return c.Turns[i].Output
		}
	}
	return ""
}

func TestScenarioRefundApproval(t *testing.T) {
	const approve = `{"approved":true,"note":"核实无误","approver":"finance:li"}`
	t.Run("有争议时等待审核后退款一次", func(t *testing.T) {
		rc := newRefundCase(t, "refund-approve", 80000, 50000, false)
		point := rc.start(t)
		var info map[string]any
		require.NoError(t, json.Unmarshal(point.Info, &info))
		require.Equal(t, "A1", info["order_id"])
		require.EqualValues(t, 80000, info["amount_cents"])
		require.Contains(t, info["reason"], "金额超过")

		_, err := rc.answer(t, point.Key, `{"approved":true,"note":"x","approver":""}`)
		var rejected *workflow.ResumeRejectedError
		require.ErrorAs(t, err, &rejected, "批准时必须有审核人")

		snap, err := rc.answer(t, point.Key, approve)
		require.NoError(t, err)
		require.True(t, snap.Done)
		require.Contains(t, lastOutput(snap.State, "refund"), "已退款：")
		require.Equal(t, 1, rc.desk.ledger.refunds(), "退款恰好执行一次")
		require.Equal(t, 1, rc.refund.Calls(), "退款 Agent 的模型请求从日志回放")
		require.EqualValues(t, 1, rc.desk.registers.Load(), "登记操作从日志回放")
		rc.desk.ledger.read(func(s *ledgerState) {
			require.Equal(t, 1, s.verdicts["refund-A1"].Seq, "判定与登记序号经 OnRegister 写入一次")
		})
		require.Equal(t, 2, rc.triage.Calls(), "分流只多出退款后的那次请求")
	})
	t.Run("拒绝时不退款", func(t *testing.T) {
		rc := newRefundCase(t, "refund-reject", 80000, 50000, false)
		snap, err := rc.answer(t, rc.start(t).Key, `{"approved":false,"note":"重复申请","approver":"finance:li"}`)
		require.NoError(t, err)
		require.Equal(t, "人工拒绝：重复申请", lastOutput(snap.State, "refund"))
		require.Zero(t, rc.desk.ledger.refunds())
	})
	t.Run("无争议时直接退款", func(t *testing.T) {
		rc := newRefundCase(t, "refund-direct", 3000, 50000, false)
		snap, err := rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{Input: "订单 A1 申请退款"}, "")
		require.NoError(t, err)
		require.Contains(t, lastOutput(snap.State, "refund"), "已退款：")
		require.Equal(t, 1, rc.desk.ledger.refunds())
	})
	t.Run("Agent 标记有争议时进入等待", func(t *testing.T) {
		rc := newRefundCase(t, "refund-agent-flag", 3000, 50000, true)
		var info map[string]any
		require.NoError(t, json.Unmarshal(rc.start(t).Info, &info))
		require.Contains(t, info["reason"], "Agent 标记")
	})
	t.Run("等待期间规则或订单变化不改变判定", func(t *testing.T) {
		rc := newRefundCase(t, "refund-rule-change", 80000, 50000, false)
		point := rc.start(t)
		rc.desk.ledger.read(func(*ledgerState) { rc.desk.ledger.threshold = 1000000 }) // 新阈值下本应无争议
		rc.desk.ledger.changeOrder()
		snap, err := rc.answer(t, point.Key, `{"approved":false,"note":"不同意","approver":"finance:li"}`)
		require.NoError(t, err)
		require.Equal(t, "人工拒绝：不同意", lastOutput(snap.State, "refund"), "判定已固定，审核结果仍然生效")
		require.Zero(t, rc.desk.ledger.refunds())
		require.EqualValues(t, 1, rc.desk.registers.Load())
	})
	t.Run("订单变化导致批准失败写回状态", func(t *testing.T) {
		rc := newRefundCase(t, "refund-order-change", 80000, 50000, false)
		point := rc.start(t)
		rc.desk.ledger.changeOrder()
		snap, err := rc.answer(t, point.Key, approve)
		require.NoError(t, err, "业务结果不是节点错误")
		require.Equal(t, "订单已变化，需重新申请", lastOutput(snap.State, "refund"))
		require.Zero(t, rc.desk.ledger.refunds())
	})
	t.Run("等待超时按拒绝处理", func(t *testing.T) {
		rc := newRefundCase(t, "refund-timeout", 80000, 50000, false)
		snap, err := rc.answer(t, rc.start(t).Key, `{"approved":false,"reason":"timeout"}`)
		require.NoError(t, err)
		require.Equal(t, "审核超时，按拒绝处理", lastOutput(snap.State, "refund"))
		require.Zero(t, rc.desk.ledger.refunds())
	})
	t.Run("回执已写入而执行记录停在 pending", func(t *testing.T) {
		rc := newRefundCase(t, "refund-reconcile", 80000, 50000, false)
		point := rc.start(t)
		rc.e.fault.arm(false, operationFails("refund/execute", "completed"))
		_, err := rc.answer(t, point.Key, approve)
		require.ErrorIs(t, err, errInjected)
		require.Equal(t, 1, rc.desk.ledger.refunds())

		rc.e.restart()
		snap, err := rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{}, "")
		require.NoError(t, err, "已回应的中断无需再次提交，用 Run 续跑")
		require.Contains(t, lastOutput(snap.State, "refund"), "已退款：")
		require.Equal(t, 1, rc.desk.ledger.refunds(), "Reconcile 补记完成，不重复退款")
		require.EqualValues(t, 1, rc.desk.executions.Load(), "执行操作没有重跑")
	})
	t.Run("退款已提交但执行返回错误时重跑先查回执", func(t *testing.T) {
		rc := newRefundCase(t, "refund-ack-lost", 80000, 50000, false)
		point := rc.start(t)
		rc.desk.ledger.loseNextRefundAck()
		_, err := rc.answer(t, point.Key, approve)
		require.ErrorIs(t, err, errRefundAckLost)
		require.Equal(t, 1, rc.desk.ledger.refunds())
		r := inspectAt(t, rc.e, rc.desk.nodeKey.Load().(string), "refund/execute")
		require.Equal(t, "failed", r.Status, "记录为 failed，Reconcile 不会被调用")
		require.Equal(t, "execution", r.FailureKind)

		// 退款后订单事实可能变化（如退款更新订单版本）；重跑若先比对订单或重新批准，会把已完成的退款误报为需重新申请。
		rc.desk.ledger.changeOrder()
		rc.e.restart()
		snap, err := rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{}, "")
		require.NoError(t, err)
		receipt, ok := rc.desk.receipt("refund-A1")
		require.True(t, ok)
		require.Equal(t, "已退款："+receipt, lastOutput(snap.State, "refund"), "返回原回执")
		require.Equal(t, 1, rc.desk.ledger.refunds(), "不重复退款")
		require.EqualValues(t, 2, rc.desk.executions.Load(), "RetryFailed 让执行操作重跑一次")
		r = inspectAt(t, rc.e, rc.desk.nodeKey.Load().(string), "refund/execute")
		require.Equal(t, "completed", r.Status)
		require.Equal(t, 2, r.Attempts)
		require.False(t, r.Reconciled)
	})
	t.Run("登记记录停在 pending 时按申请与判定补记", func(t *testing.T) {
		rc := newRefundCase(t, "refund-register-reconcile", 80000, 50000, false)
		rc.e.fault.arm(false, operationFails("refund/register", "completed"))
		_, err := rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{Input: "订单 A1 重复扣款，申请退款"}, "")
		require.ErrorIs(t, err, errInjected)
		require.Equal(t, "pending", inspectAt(t, rc.e, rc.desk.nodeKey.Load().(string), "refund/register").Status)

		rc.e.restart()
		snap, err := rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{}, "")
		require.ErrorIs(t, err, durable.ErrApprovalRequired, "补记的判定仍为有争议，停在审核")
		require.Len(t, snap.Interrupts, 1)
		require.Equal(t, "refund-review", snap.Interrupts[0].ID)
		require.EqualValues(t, 1, rc.desk.registers.Load(), "登记没有重跑")
		r := inspectAt(t, rc.e, rc.desk.nodeKey.Load().(string), "refund/register")
		require.Equal(t, "completed", r.Status)
		require.True(t, r.Reconciled)
		require.Zero(t, rc.desk.ledger.refunds())
	})
	t.Run("只有申请没有判定时不补记也不放行", func(t *testing.T) {
		rc := newRefundCase(t, "refund-register-no-verdict", 80000, 50000, false)
		rc.e.fault.arm(false, operationFails("refund/register", "completed"))
		_, err := rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{Input: "订单 A1 重复扣款，申请退款"}, "")
		require.ErrorIs(t, err, errInjected)
		// 内存账本的事务是原子的，无法自然出现“有申请无判定”，这里直接删除判定来构造。
		rc.desk.ledger.read(func(s *ledgerState) { delete(s.verdicts, "refund-A1") })

		rc.e.restart()
		_, err = rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{}, "")
		require.ErrorContains(t, err, "缺少争议判定")
		require.EqualValues(t, 2, rc.desk.registers.Load(), "未补记，RetryInterrupted 重跑登记")
		r := inspectAt(t, rc.e, rc.desk.nodeKey.Load().(string), "refund/register")
		require.Equal(t, "failed", r.Status)
		require.False(t, r.Reconciled)
		require.Zero(t, rc.desk.ledger.refunds())
	})
}
