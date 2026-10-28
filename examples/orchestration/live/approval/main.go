// 人工审批暂停：Agent 判断退款申请是否符合政策；小额直接模拟打款，大额在 pay_large
// 节点暂停等待审批。审批后恢复执行，模拟账本按申请单号去重，崩溃重试也只付一次。
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
	"sync/atomic"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/live/internal/livecase"
	"github.com/kart-io/goagent/workflow"
)

// LargeAmount 以上（不含）需要人工审批。
const LargeAmount = 500.0

// Refund 是图状态。金额只取申请单，不采信模型输出的数字。
type Refund struct {
	ID                string  `json:"id"`
	Amount            float64 `json:"amount"`
	Reason            string  `json:"reason"`
	DaysSinceDelivery int     `json:"days_since_delivery"`
	Decision          string  `json:"decision"`
	Receipt           string  `json:"receipt"`
}

// Requests 是合成申请及按政策应得的路径。
var Requests = map[string]struct {
	Refund
	Decision string
	Approval bool
}{
	"R-SMALL":  {Refund{ID: "R-SMALL", Amount: 199, Reason: "耳机未拆封，不想要了", DaysSinceDelivery: 3}, "退款", false},
	"R-LARGE":  {Refund{ID: "R-LARGE", Amount: 2899, Reason: "平板电脑未拆封，买重复了", DaysSinceDelivery: 5}, "退款", true},
	"R-REJECT": {Refund{ID: "R-REJECT", Amount: 459, Reason: "鞋子已经穿过两周，不太合脚", DaysSinceDelivery: 25}, "拒绝", false},
}

const assessPrompt = `你是退款审核员。政策：签收 7 天内（含）且商品未拆封、未使用的，可全额退款；否则拒绝。
只依据申请内容判断，不要考虑金额大小。第一行只能是“结论：退款”或“结论：拒绝”，第二行一句理由。`

// Result 汇总一次推进的状态。Status 为 completed、waiting。
type Result struct {
	Status, Decision, Receipt, ApprovalKey string
	NewPayments                            int64
	NewModelCalls                          int64
	Budget                                 budget.Snapshot
}

// Hooks 用于测试注入：AfterPay 在账本写入成功、检查点保存之前调用。
type Hooks struct{ AfterPay func() }

type ledgerEntry struct {
	Receipt, RequestID, ExecutionKey string
	Amount                           float64
}

func ledgerID(requestID string) string { return "demo-ledger/" + requestID }

// pay 模拟外部支付：以申请单号为唯一键，重复调用返回原收据。
func pay(ctx context.Context, store durable.Store, r Refund, payments *atomic.Int64) (string, error) {
	id := ledgerID(r.ID)
	unlock, err := store.Lock(ctx, id)
	if err != nil {
		return "", err
	}
	defer func() { _ = unlock() }()
	if snap, err := store.Load(ctx, id); err == nil {
		var e ledgerEntry
		if err := json.Unmarshal(snap.Data, &e); err != nil {
			return "", err
		}
		if e.Amount != r.Amount {
			return "", fmt.Errorf("同一申请 %s 的金额不一致", r.ID)
		}
		return e.Receipt, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	sum := sha256.Sum256([]byte(r.ID))
	e := ledgerEntry{Receipt: "PAY-" + hex.EncodeToString(sum[:4]), RequestID: r.ID, ExecutionKey: workflow.DurableKey(ctx), Amount: r.Amount}
	data, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	if err = store.Save(ctx, id, &durable.Snapshot{RunID: id, Version: "demo-ledger-v1", Data: data}); err != nil {
		return "", err
	}
	payments.Add(1)
	return e.Receipt, nil
}

// LedgerReceipt 读取账本，测试用来确认是否已付款。
func LedgerReceipt(dir, requestID string) (string, bool, error) {
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return "", false, err
	}
	snap, err := store.Load(context.Background(), ledgerID(requestID))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var e ledgerEntry
	err = json.Unmarshal(snap.Data, &e)
	return e.Receipt, true, err
}

func build(ctx context.Context, client *livemodel.Client, store durable.Store, payments *atomic.Int64, hooks Hooks) (*workflow.DurableGraph[Refund], error) {
	g := workflow.NewGraph[Refund]()
	if err := g.AddNode("assess", func(ctx context.Context, r Refund) (Refund, error) {
		agent, err := builder.NewSimpleBuilder(client).WithDurableScope("assessor").WithSystemPrompt(assessPrompt).
			WithMaxIterations(1).WithMaxTokens(8192).Build()
		if err != nil {
			return r, err
		}
		input, _ := json.Marshal(map[string]any{"reason": r.Reason, "days_since_delivery": r.DaysSinceDelivery})
		out, err := agent.AsRunnable().Invoke(ctx, "退款申请："+string(input))
		if err != nil {
			return r, err
		}
		r.Decision, err = livecase.Decision(fmt.Sprint(out.Result), "退款", "拒绝")
		return r, err
	}); err != nil {
		return nil, err
	}
	payNode := func(ctx context.Context, r Refund) (Refund, error) {
		receipt, err := pay(ctx, store, r, payments)
		if err != nil {
			return r, err
		}
		if hooks.AfterPay != nil {
			hooks.AfterPay()
		}
		r.Receipt = receipt
		return r, nil
	}
	for _, n := range []string{"pay_small", "pay_large"} {
		if err := g.AddNode(n, payNode); err != nil {
			return nil, err
		}
		if err := g.AddEdge(n, workflow.END); err != nil {
			return nil, err
		}
	}
	if err := g.AddEdge(workflow.START, "assess"); err != nil {
		return nil, err
	}
	if err := g.AddBranch("assess", func(_ context.Context, r Refund) (string, error) {
		switch {
		case r.Decision != "退款":
			return workflow.END, nil
		case r.Amount > LargeAmount:
			return "pay_large", nil
		default:
			return "pay_small", nil
		}
	}, "pay_small", "pay_large", workflow.END); err != nil {
		return nil, err
	}
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "approval-v1/" + client.Live.Provider + "/" + client.Live.Model, ApprovalNodes: []string{"pay_large"}})
}

// Run 推进申请 requestID。approvalKey 为空表示开始或继续；非空表示批准当前等待的节点。
func Run(ctx context.Context, client *livemodel.Client, dir, requestID, approvalKey string, hooks Hooks) (Result, error) {
	var out Result
	req, ok := Requests[requestID]
	if !ok {
		return out, fmt.Errorf("未知申请 %s", requestID)
	}
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	controller, err := budget.NewDurable(budget.Config{RootKey: "refund/" + requestID, Version: "v1/" + client.Live.Provider + "/" + client.Live.Model, MaxModelRequests: 2}, store)
	if err != nil {
		return out, err
	}
	var payments atomic.Int64
	g, err := build(ctx, client, store, &payments, hooks)
	if err != nil {
		return out, err
	}
	before := client.Calls()
	snap, runErr := g.Run(budget.WithController(ctx, controller), "refund/"+requestID, req.Refund, approvalKey)
	out.NewModelCalls = client.Calls() - before
	out.NewPayments = payments.Load()
	out.Budget, _ = controller.Snapshot(ctx)
	if snap != nil {
		out.Decision, out.Receipt = snap.State.Decision, snap.State.Receipt
	}
	switch {
	case errors.Is(runErr, durable.ErrApprovalRequired):
		out.Status, out.ApprovalKey = "waiting", snap.Key
		return out, nil
	case runErr != nil:
		return out, runErr
	case snap == nil || !snap.Done:
		return out, errors.New("流程没有结束")
	}
	out.Status = "completed"
	return out, nil
}

func main() {
	dir := flag.String("dir", "", "持久化目录（含模拟账本）")
	request := flag.String("request", "R-SMALL", "申请单号：R-SMALL、R-LARGE 或 R-REJECT")
	approve := flag.String("approve", "", "批准等待中的节点：传入上次输出的 ApprovalKey")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "用法：approval -dir <目录> [-request R-LARGE] [-approve <key>]")
		os.Exit(2)
	}
	client, err := livemodel.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := Run(context.Background(), client, *dir, *request, *approve, Hooks{})
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
