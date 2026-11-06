package builder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/llm"
)

// ErrHandleDone 表示句柄的全部轮次已经结束，不再接收输入。
var ErrHandleDone = errors.New("agent handle is done")

// errRedirected 是 Redirect 取消进行中的模型请求时的原因。
var errRedirected = errors.New("model request redirected")

// inboxWriteTimeout 限制一次收件箱写入；写入不跟随运行的取消，取消后的确认仍要落盘。
const inboxWriteTimeout = 5 * time.Second

// 持久收件箱记录的输入种类。
const (
	inboxKindSteer    = "steer"
	inboxKindInject   = "inject"
	inboxKindFollowup = "followup"
)

type inboxKind int

const (
	inboxSteer inboxKind = iota
	inboxInject
)

type inboxItem struct {
	Kind inboxKind `json:"kind"`
	Text string    `json:"text"`
	// Seq 是持久收件箱中的序号，非持久运行为 0。领取记录带上它，恢复时才能确认同一条输入。
	Seq uint64 `json:"seq,omitzero"`
}

type followupItem struct {
	Text string
	Seq  uint64
}

// followupBatch 是一轮 Followup 的领取记录。
type followupBatch struct {
	Texts []string `json:"texts"`
	Seqs  []uint64 `json:"seqs,omitempty"`
}

// inbox 是运行中的 Agent 收到的输入。Steer 与 Inject 在下一步作为 user 消息追加（Steer 在即将结束时唤醒续跑，
// Inject 不唤醒）；Redirect 取消进行中的模型请求；Followup 排到下一轮。
type inbox struct {
	mu        sync.Mutex
	items     []inboxItem
	redirects []string
	followups []followupItem
	// durable 非 nil 时，输入写入持久收件箱后才算接受。
	durable *durable.Inbox
	// seen 是非持久运行的 SteerOnce 去重键；持久运行的键随收件箱落盘。
	seen map[string]struct{}
	// cancelModel 只在模型请求进行中且允许重定向时非空。
	cancelModel context.CancelCauseFunc
	done        bool
	// transcript 是上一轮结束时的对话（不含 system 与运行上下文），供 Followup 续接。
	transcript []llm.Message
}

type inboxKey struct{}
type historyKey struct{}

func inboxFrom(ctx context.Context) *inbox {
	b, _ := ctx.Value(inboxKey{}).(*inbox)
	return b
}

// persist 把输入写入持久收件箱，返回序号；非持久运行为 0。调用方持有 b.mu：
// 写入期间不能结束收件箱，否则已落盘的输入会同时收到 ErrHandleDone。
func (b *inbox) persist(kind, text string) (uint64, error) {
	if b.durable == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), inboxWriteTimeout)
	defer cancel()
	return b.durable.Append(ctx, kind, text)
}

func (b *inbox) push(kind inboxKind, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return ErrHandleDone
	}
	name := inboxKindInject
	if kind == inboxSteer {
		name = inboxKindSteer
	}
	seq, err := b.persist(name, text)
	if err != nil {
		return err
	}
	b.items = append(b.items, inboxItem{Kind: kind, Text: text, Seq: seq})
	return nil
}

// ack 确认输入已领取：从持久收件箱和内存中移除。恢复时领取记录会重放，内存中可能还有这些输入，所以两边都按序号移除。
func (b *inbox) ack(seqs []uint64) error {
	if b == nil || b.durable == nil || len(seqs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), inboxWriteTimeout)
	defer cancel()
	if err := b.durable.Ack(ctx, seqs...); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = slices.DeleteFunc(b.items, func(item inboxItem) bool { return slices.Contains(seqs, item.Seq) })
	b.followups = slices.DeleteFunc(b.followups, func(item followupItem) bool { return slices.Contains(seqs, item.Seq) })
	return nil
}

// restore 载入上次运行留下的未确认输入。
func (b *inbox) restore(entries []durable.InboxEntry) {
	for _, entry := range entries {
		switch entry.Kind {
		case inboxKindSteer:
			b.items = append(b.items, inboxItem{Kind: inboxSteer, Text: entry.Text, Seq: entry.Seq})
		case inboxKindInject:
			b.items = append(b.items, inboxItem{Kind: inboxInject, Text: entry.Text, Seq: entry.Seq})
		case inboxKindFollowup:
			b.followups = append(b.followups, followupItem{Text: entry.Text, Seq: entry.Seq})
		}
	}
}

// claimFollowups 领取下一轮的 Followup 批次，返回输入文本；没有批次时 ok 为假。调用方持有 b.mu。
// 持久运行中批次先记入日志，恢复时按记录继续同一轮；已有记录时优先使用记录。
func (b *inbox) claimFollowups(ctx context.Context, turn int) (batch followupBatch, ok bool, err error) {
	take := func() followupBatch {
		var claimed followupBatch
		for _, item := range b.followups {
			claimed.Texts = append(claimed.Texts, item.Text)
			if item.Seq != 0 {
				claimed.Seqs = append(claimed.Seqs, item.Seq)
			}
		}
		return claimed
	}
	if !durable.HasJournal(ctx) {
		batch = take()
		b.followups = nil
		return batch, len(batch.Texts) > 0, nil
	}
	id := fmt.Sprintf("followup-batch/%d", turn)
	record, err := durable.InspectOperation(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		if len(b.followups) == 0 {
			return batch, false, nil
		}
	} else if err != nil {
		return batch, false, err
	} else if record.Status == "completed" {
		if err = json.Unmarshal(record.Value, &batch); err != nil {
			return batch, false, fmt.Errorf("%s: %w", id, err)
		}
		return batch, true, nil
	}
	// 记录停在 pending 时重新领取：确认在记录完成之后才发生，这些输入还在收件箱里。
	batch, err = journaled(ctx, id, func(context.Context) (followupBatch, error) { return take(), nil })
	return batch, err == nil, err
}

// drain 取出全部 Steer 与 Inject，按到达顺序。
func (b *inbox) drain() []inboxItem {
	b.mu.Lock()
	defer b.mu.Unlock()
	items := b.items
	b.items = nil
	return items
}

// wakes 报告是否有待处理的 Steer。
func (b *inbox) wakes() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, item := range b.items {
		if item.Kind == inboxSteer {
			return true
		}
	}
	return false
}

// requeue 把未能用于重定向的纠正放回收件箱，按 Steer 处理。
func (b *inbox) requeue(msgs []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, msg := range msgs {
		b.items = append(b.items, inboxItem{Kind: inboxSteer, Text: msg})
	}
}

// beginModel 登记进行中的模型请求；持久运行不登记，Redirect 因此降级为 Steer。
func (b *inbox) beginModel(cancel context.CancelCauseFunc) {
	b.mu.Lock()
	b.cancelModel = cancel
	b.mu.Unlock()
}

func (b *inbox) endModel() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancelModel = nil
	redirects := b.redirects
	b.redirects = nil
	return redirects
}

// Handle 是运行中的 Agent 句柄，由 ConfigurableAgent.Start 创建。
type Handle struct {
	inbox  *inbox
	cancel context.CancelCauseFunc
	done   chan struct{}
	out    *AgentOutput
	err    error
}

// Start 在后台运行 Agent 并返回句柄，宿主可在运行中追加输入。只支持原生工具流（llm.ToolStreamClient 且配置了工具）。
// 持久运行中输入写入持久收件箱后才算接受，写入失败时输入方法返回错误。用同一执行键再次 Start 时恢复未领取的输入，
// 并按记录的批次继续 Followup 轮次，宿主不需要重建也不应重复提交。
func (a *ConfigurableAgent[C, S]) Start(ctx context.Context, input string) *Handle {
	ctx, cancel := context.WithCancelCause(ctx)
	h := &Handle{inbox: &inbox{}, cancel: cancel, done: make(chan struct{})}
	fail := func(err error) *Handle {
		h.err = err
		h.inbox.done = true
		close(h.done)
		cancel(nil)
		return h
	}
	if _, ok := a.llmClient.(llm.ToolStreamClient); !ok || len(a.tools) == 0 {
		return fail(errors.New("Handle 需要原生工具流：llm.ToolStreamClient 且配置了工具"))
	}
	box, entries, err := durable.OpenInbox(ctx)
	if err != nil {
		return fail(fmt.Errorf("打开持久收件箱: %w", err))
	}
	h.inbox.durable = box
	h.inbox.restore(entries)
	go h.run(ctx, a.invokeHandleTurn, input)
	return h
}

// invokeHandleTurn consumes the same ExecuteStream source as AsRunnable, but
// waits for its producer to close even on cancellation. Handle owns this turn:
// reporting completion while its tools/cleanup still use the inbox or execution
// scope would make Wait and a terminal PendingInputs snapshot premature.
func (a *ConfigurableAgent[C, S]) invokeHandleTurn(ctx context.Context, input string) (*AgentOutput, error) {
	var result streamResult
	events, err := a.executeStream(ctx, input, &result)
	if err != nil {
		return nil, err
	}
	for range events {
	}
	// Closing events synchronizes result publication, including errors which
	// could not be sent as public events after ctx was canceled.
	if result.err != nil {
		return nil, result.err
	}
	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}
	if result.output == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return result.output, nil
}

func (h *Handle) run(ctx context.Context, invoke func(context.Context, string) (*AgentOutput, error), input string) {
	defer close(h.done)
	defer h.cancel(nil)
	parentKey := durable.ExecutionKey(ctx)
	var history []llm.Message
	for turn := 0; ; turn++ {
		turnCtx := context.WithValue(ctx, inboxKey{}, h.inbox)
		if history != nil {
			turnCtx = context.WithValue(turnCtx, historyKey{}, history)
		}
		if turn > 0 && durable.HasJournal(ctx) {
			turnCtx = durable.WithExecutionKey(turnCtx, durable.ChildKey(parentKey, fmt.Sprintf("followup/%d", turn)))
		}
		out, err := invoke(turnCtx, input)
		h.inbox.mu.Lock()
		if cause := context.Cause(ctx); cause != nil {
			if err == nil {
				err = cause
			} else if !errors.Is(err, cause) {
				err = errors.Join(err, cause)
			}
		}
		h.out, h.err = out, err
		if err != nil {
			h.inbox.done = true
			h.inbox.mu.Unlock()
			return
		}
		batch, ok, err := h.inbox.claimFollowups(ctx, turn+1)
		if err != nil || !ok {
			h.err = err
			if err != nil {
				h.out = nil
			}
			h.inbox.done = true
			h.inbox.mu.Unlock()
			return
		}
		history = h.inbox.transcript
		h.inbox.mu.Unlock()
		if err := h.inbox.ack(batch.Seqs); err != nil {
			h.inbox.mu.Lock()
			h.out, h.err = nil, fmt.Errorf("确认 Followup: %w", err)
			h.inbox.done = true
			h.inbox.mu.Unlock()
			return
		}
		input = strings.Join(batch.Texts, "\n\n")
	}
}

// PendingInput is one input still queued in a Handle. Kind is "steer", "inject",
// "redirect", or "followup". A Redirect falling back to Steer is reported as steer.
type PendingInput struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// PendingInputs returns an independent, non-consuming snapshot of inputs the
// execution loop has not claimed. It is safe during execution and after Wait.
// Order is preserved within each queue, not across queues. Claimed inputs are
// absent even if a later model request fails: absence is not delivery or effect
// confirmation. In a durable run an accepted input is also on disk until claimed,
// so a later Start with the same execution key restores it.
// After the Handle ends, the host decides what to do with remaining inputs;
// this method does not start new turns or bypass iteration/budget limits.
func (h *Handle) PendingInputs() []PendingInput {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	var pending []PendingInput
	for _, item := range h.inbox.items {
		kind := "inject"
		if item.Kind == inboxSteer {
			kind = "steer"
		}
		pending = append(pending, PendingInput{Kind: kind, Text: item.Text})
	}
	for _, text := range h.inbox.redirects {
		pending = append(pending, PendingInput{Kind: "redirect", Text: text})
	}
	for _, item := range h.inbox.followups {
		pending = append(pending, PendingInput{Kind: "followup", Text: item.Text})
	}
	return pending
}

// Steer 排到下一步：当前工具批次结束后作为 user 消息追加；模型即将结束本轮时唤醒续跑。
// 入队成功不保证已处理；结束时未领取的输入可通过 PendingInputs 查询。持久运行中入队成功表示已落盘。
func (h *Handle) Steer(msg string) error { return h.inbox.push(inboxSteer, msg) }

// SteerOnce 与 Steer 相同，但同一 key 只接受一次，重复调用返回 nil。持久运行中键与输入原子落盘，投递方崩溃后重投、
// 输入已被领取或进程已重启都不会产生第二条（同一执行键内，记住最近 256 个键）；非持久运行只在本 Handle 内去重。
func (h *Handle) SteerOnce(key, msg string) error {
	b := h.inbox
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return ErrHandleDone
	}
	var seq uint64
	if b.durable != nil {
		ctx, cancel := context.WithTimeout(context.Background(), inboxWriteTimeout)
		defer cancel()
		var duplicate bool
		var err error
		if seq, duplicate, err = b.durable.AppendOnce(ctx, key, inboxKindSteer, msg); err != nil || duplicate {
			return err
		}
	} else {
		if _, duplicate := b.seen[key]; duplicate {
			return nil
		}
		if b.seen == nil {
			b.seen = map[string]struct{}{}
		}
		b.seen[key] = struct{}{}
	}
	b.items = append(b.items, inboxItem{Kind: inboxSteer, Text: msg, Seq: seq})
	return nil
}

// Inject 排到下一步，随下一次模型请求发送，但不唤醒即将结束的轮次。
func (h *Handle) Inject(msg string) error { return h.inbox.push(inboxInject, msg) }

// Followup 排到本轮结束之后，作为新一轮的用户输入，续接本轮的对话。
func (h *Handle) Followup(msg string) error {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	if h.inbox.done {
		return ErrHandleDone
	}
	seq, err := h.inbox.persist(inboxKindFollowup, msg)
	if err != nil {
		return err
	}
	h.inbox.followups = append(h.inbox.followups, followupItem{Text: msg, Seq: seq})
	return nil
}

// Redirect 取消进行中的模型请求，保留已流出的文本，追加 msg 后重新请求；没有进行中的模型请求（如工具执行中）
// 或持久运行中降级为 Steer。
func (h *Handle) Redirect(msg string) error {
	h.inbox.mu.Lock()
	if h.inbox.done {
		h.inbox.mu.Unlock()
		return ErrHandleDone
	}
	if cancel := h.inbox.cancelModel; cancel != nil {
		h.inbox.redirects = append(h.inbox.redirects, msg)
		h.inbox.mu.Unlock()
		cancel(errRedirected)
		return nil
	}
	seq, err := h.inbox.persist(inboxKindSteer, msg)
	if err == nil {
		h.inbox.items = append(h.inbox.items, inboxItem{Kind: inboxSteer, Text: msg, Seq: seq})
	}
	h.inbox.mu.Unlock()
	return err
}

// Interrupt 中止运行，Wait 返回 cause（为 nil 时为 context.Canceled）。
func (h *Handle) Interrupt(cause error) { h.cancel(cause) }

// Wait 等待全部轮次结束，返回最后一轮的结果。
// ctx 只控制等待，不取消 Handle。返回运行结果后可用 PendingInputs 查询未领取输入。
// Interrupt 后仍等待本轮 ExecuteStream 退出，包括工具和清理；宿主扩展必须合作返回。
func (h *Handle) Wait(ctx context.Context) (*AgentOutput, error) {
	select {
	case <-h.done:
		return h.out, h.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
