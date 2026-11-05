package multiagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	"github.com/kart-io/goagent/workflow/refund"
)

// 本文件是 S5 的业务侧替身：退款账本实现 refund.Store，退款成员是自定义 Step.Run。
// 它们演示业务如何接入，不属于 multiagent 的 API。

// verdict 是按业务规则计算并在登记时固定的争议判定。
type verdict struct {
	Disputed bool   `json:"disputed"`
	Reason   string `json:"reason"`
	Seq      int    `json:"seq"` // 登记序号，由 OnRegister 钩子在登记事务内分配
}

// proposal 是退款 Agent 给出的结构化提议；Agent 只能把无争议升级为有争议。
type proposal struct {
	OrderID     string `json:"order_id"`
	AmountCents int64  `json:"amount_cents"`
	Disputed    bool   `json:"disputed"`
	Reason      string `json:"reason,omitempty"`
}

type registration struct {
	Request refund.Request `json:"request"`
	Verdict verdict        `json:"verdict"`
}

type execution struct {
	Receipt      string `json:"receipt,omitempty"`
	OrderChanged bool   `json:"order_changed,omitempty"`
}

// reviewInput 是审核回应；approver 由宿主认证后填入，reason 为 timeout 表示等待超时按拒绝处理。
type reviewInput struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note"`
	Approver string `json:"approver"`
	Reason   string `json:"reason,omitempty"`
}

// validateReview 是纯函数：格式错误或批准却没有审核人时保持等待。
func validateReview(raw json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var in reviewInput
	if err := dec.Decode(&in); err != nil {
		return errors.New("审核输入格式应为 {approved, note, approver}")
	}
	if in.Approved && in.Approver == "" {
		return errors.New("批准时必须提供审核人")
	}
	return nil
}

type ledgerState struct {
	order    refund.Order
	requests map[string]refund.Request
	actors   map[string]string
	receipts map[string]string
	verdicts map[string]verdict
	seq      int
	total    int64
	refunds  int
}

func (s ledgerState) clone() ledgerState {
	s.requests, s.actors, s.receipts, s.verdicts = maps.Clone(s.requests), maps.Clone(s.actors), maps.Clone(s.receipts), maps.Clone(s.verdicts)
	return s
}

// ledger 是内存退款账本：事务复制状态、成功才提交，回调出错整体回滚。
type ledger struct {
	mu        sync.Mutex
	state     ledgerState
	threshold int64 // 金额超过它即有争议（分）
}

func newLedger(paidCents, threshold int64) *ledger {
	return &ledger{threshold: threshold, state: ledgerState{
		order:    refund.Order{PaidCents: paidCents, Currency: "CNY", DeliveryDays: 2, Version: 1},
		requests: map[string]refund.Request{}, actors: map[string]string{}, receipts: map[string]string{}, verdicts: map[string]verdict{},
	}}
}

func (l *ledger) Transaction(_ context.Context, f func(refund.Transaction) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	next := l.state.clone()
	if err := f(&ledgerTx{s: &next}); err != nil {
		return err
	}
	l.state = next
	return nil
}

func (l *ledger) read(fn func(*ledgerState)) { l.mu.Lock(); defer l.mu.Unlock(); fn(&l.state) }

// changeOrder 模拟审核等待期间订单事实变化。
func (l *ledger) changeOrder() {
	l.read(func(s *ledgerState) { s.order.Version++; s.order.DeliveryDays++ })
}

func (l *ledger) refunds() (n int) { l.read(func(s *ledgerState) { n = s.refunds }); return }

type ledgerTx struct{ s *ledgerState }

func (t *ledgerTx) Order(context.Context, string) (refund.Order, error) { return t.s.order, nil }
func (t *ledgerTx) Request(_ context.Context, id string) (refund.Request, string, error) {
	r, ok := t.s.requests[id]
	if !ok {
		return refund.Request{}, "", refund.ErrNotFound
	}
	return r, t.s.actors[id], nil
}

func (t *ledgerTx) Receipt(_ context.Context, id string) (string, error) {
	r, ok := t.s.receipts[id]
	if !ok {
		return "", refund.ErrNotFound
	}
	return r, nil
}
func (t *ledgerTx) TotalRefunded(context.Context, string) (int64, error) { return t.s.total, nil }
func (t *ledgerTx) InsertRequest(_ context.Context, r refund.Request) error {
	t.s.requests[r.RequestID] = r
	return nil
}

func (t *ledgerTx) Approve(_ context.Context, id, actor string, _ time.Time) error {
	t.s.actors[id] = actor
	return nil
}

func (t *ledgerTx) InsertRefund(_ context.Context, _ string, r refund.Request, receipt string) error {
	t.s.receipts[r.RequestID] = receipt
	t.s.total += r.AmountCents
	t.s.refunds++
	return nil
}

// refundDesk 组合账本、refund.Service 与计数，供退款成员使用。
type refundDesk struct {
	ledger                *ledger
	service               *refund.Service
	registers, executions atomic.Int32
}

func newRefundDesk(paidCents, threshold int64) *refundDesk {
	l := newLedger(paidCents, threshold)
	return &refundDesk{ledger: l, service: &refund.Service{
		Store: l,
		Authorizer: refund.AuthorizeFunc(func(_ context.Context, actor string, _ refund.Request) error {
			if actor == "" {
				return errors.New("审核人未认证")
			}
			return nil
		}),
		Policy: func(context.Context, refund.Request, refund.Order) error { return nil },
	}}
}

// register 登记申请，并经 refund.Service.OnRegister 在同一事务内写入争议判定；判定规则优先，Agent 只能升级为有争议。
func (d *refundDesk) register(ctx context.Context, id string, p proposal) (json.RawMessage, error) {
	d.registers.Add(1)
	var order refund.Order
	var threshold int64
	d.ledger.read(func(s *ledgerState) { order, threshold = s.order, d.ledger.threshold })
	v := verdict{}
	switch {
	case p.AmountCents > threshold:
		v = verdict{Disputed: true, Reason: fmt.Sprintf("金额超过 %d 分", threshold)}
	case p.Disputed:
		v = verdict{Disputed: true, Reason: "Agent 标记：" + p.Reason}
	}
	req := refund.Request{
		RequestID: id, OrderID: p.OrderID, AmountCents: p.AmountCents, Currency: order.Currency, PaidCents: order.PaidCents,
		DeliveryDays: order.DeliveryDays, OrderVersion: order.Version, ValidUntil: time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339Nano),
	}
	// 每次登记用一份带钩子的 Service 副本：钩子在新插入申请的同一事务内写入判定与登记序号。
	svc := *d.service
	svc.OnRegister = func(_ context.Context, tx refund.Transaction, r refund.Request) error {
		lt, ok := tx.(*ledgerTx)
		if !ok {
			return errors.New("账本事务类型不符")
		}
		lt.s.seq++
		v.Seq = lt.s.seq
		lt.s.verdicts[r.RequestID] = v
		return nil
	}
	registered, err := svc.Register(ctx, req)
	if err != nil {
		return nil, err
	}
	d.ledger.read(func(s *ledgerState) { v = s.verdicts[id] })
	return json.Marshal(registration{Request: registered, Verdict: v})
}

// reconcileRegister 只有申请与判定都已存在时才补记完成，避免以零值判定绕过审核。
func (d *refundDesk) reconcileRegister(id string) func(context.Context) (json.RawMessage, bool, error) {
	return func(context.Context) (json.RawMessage, bool, error) {
		var r registration
		var ok bool
		d.ledger.read(func(s *ledgerState) {
			var hasRequest, hasVerdict bool
			r.Request, hasRequest = s.requests[id]
			r.Verdict, hasVerdict = s.verdicts[id]
			ok = hasRequest && hasVerdict
		})
		if !ok {
			return nil, false, nil
		}
		data, err := json.Marshal(r)
		return data, err == nil, err
	}
}

// execute 先按申请 ID 查回执；订单事实已变化时把“需重新申请”作为业务结果返回；否则批准并退款。
func (d *refundDesk) execute(ctx context.Context, req refund.Request, approver string) (json.RawMessage, error) {
	d.executions.Add(1)
	if receipt, ok := d.receipt(req.RequestID); ok {
		return json.Marshal(execution{Receipt: receipt})
	}
	changed := false
	d.ledger.read(func(s *ledgerState) {
		changed = s.order.Version != req.OrderVersion || s.order.PaidCents != req.PaidCents || s.order.DeliveryDays != req.DeliveryDays
	})
	if changed {
		return json.Marshal(execution{OrderChanged: true})
	}
	if err := d.service.Approve(ctx, req, approver); err != nil {
		return nil, err
	}
	receipt, err := d.service.Refund(ctx, workflow.DurableKey(ctx), req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(execution{Receipt: receipt})
}

func (d *refundDesk) receipt(id string) (receipt string, ok bool) {
	d.ledger.read(func(s *ledgerState) { receipt, ok = s.receipts[id] })
	return
}

func (d *refundDesk) reconcileExecute(id string) func(context.Context) (json.RawMessage, bool, error) {
	return func(context.Context) (json.RawMessage, bool, error) {
		receipt, ok := d.receipt(id)
		if !ok {
			return nil, false, nil
		}
		data, err := json.Marshal(execution{Receipt: receipt})
		return data, err == nil, err
	}
}

// refundMember 是 S5 的退款成员：提议 → 登记（固定判定）→ 有争议时等待审核 → 执行（先查回执）。
func refundMember(desk *refundDesk, agent AgentFactory) Step[Conversation] {
	return Step[Conversation]{Name: "refund", Run: core.NewRunnableFunc(func(ctx context.Context, c Conversation) (Conversation, error) {
		reply := func(text string) (Conversation, error) {
			c.Turns = append(c.Turns, Turn{Agent: "refund", Output: text})
			return c, nil
		}
		owned, err := agent(ctx)
		if owned.Close != nil {
			defer func() { _ = owned.Close(context.WithoutCancel(ctx)) }()
		}
		if err != nil {
			return c, err
		}
		out, err := owned.Runnable.Invoke(ctx, renderTurns(c, 0))
		if err != nil {
			return c, err
		}
		var p proposal
		if err := json.Unmarshal([]byte(out.Result.(string)), &p); err != nil {
			return c, fmt.Errorf("退款提议无法解析: %w", err)
		}
		id := "refund-" + p.OrderID
		intent, err := json.Marshal(struct {
			RequestID string   `json:"request_id"`
			Proposal  proposal `json:"proposal"`
		}{id, p})
		if err != nil {
			return c, err
		}
		record, err := durable.Do(ctx, "refund/register", intent, durable.OperationPolicy{Reconcile: desk.reconcileRegister(id)},
			func(ctx context.Context) (json.RawMessage, error) { return desk.register(ctx, id, p) })
		if err != nil {
			return c, err
		}
		var reg registration
		if err := json.Unmarshal(record.Value, &reg); err != nil {
			return c, err
		}
		approver := "rule:auto"
		if reg.Verdict.Disputed {
			info, err := json.Marshal(map[string]any{"order_id": p.OrderID, "amount_cents": p.AmountCents, "reason": reg.Verdict.Reason, "submitter": "agent:refund"})
			if err != nil {
				return c, err
			}
			raw, err := durable.AwaitInput(ctx, "refund-review", info, validateReview)
			if err != nil {
				return c, err
			}
			var in reviewInput
			if err := json.Unmarshal(raw, &in); err != nil {
				return c, err
			}
			switch {
			case !in.Approved && in.Reason == "timeout":
				return reply("审核超时，按拒绝处理")
			case !in.Approved:
				return reply("人工拒绝：" + in.Note)
			}
			approver = in.Approver
		}
		intent, err = json.Marshal(struct {
			RequestID string `json:"request_id"`
			Approver  string `json:"approver"`
		}{id, approver})
		if err != nil {
			return c, err
		}
		record, err = durable.Do(ctx, "refund/execute", intent, durable.OperationPolicy{Reconcile: desk.reconcileExecute(id)},
			func(ctx context.Context) (json.RawMessage, error) { return desk.execute(ctx, reg.Request, approver) })
		if err != nil {
			return c, err
		}
		var result execution
		if err := json.Unmarshal(record.Value, &result); err != nil {
			return c, err
		}
		if result.OrderChanged {
			return reply("订单已变化，需重新申请")
		}
		return reply("已退款：" + result.Receipt)
	})}
}
