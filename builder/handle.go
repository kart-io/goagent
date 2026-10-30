package builder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/llm"
)

// ErrHandleDone 表示句柄的全部轮次已经结束，不再接收输入。
var ErrHandleDone = errors.New("agent handle is done")

// errRedirected 是 Redirect 取消进行中的模型请求时的原因。
var errRedirected = errors.New("model request redirected")

type inboxKind int

const (
	inboxSteer inboxKind = iota
	inboxInject
)

type inboxItem struct {
	Kind inboxKind `json:"kind"`
	Text string    `json:"text"`
}

// inbox 是运行中的 Agent 收到的输入。Steer 与 Inject 在下一步作为 user 消息追加（Steer 在即将结束时唤醒续跑，
// Inject 不唤醒）；Redirect 取消进行中的模型请求；Followup 排到下一轮。
type inbox struct {
	mu        sync.Mutex
	items     []inboxItem
	redirects []string
	followups []string
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

func (b *inbox) push(kind inboxKind, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return ErrHandleDone
	}
	b.items = append(b.items, inboxItem{Kind: kind, Text: text})
	return nil
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
// 持久运行中收件箱的输入按步骤记入日志；Followup 的每一轮使用由执行键派生的独立子键。
func (a *ConfigurableAgent[C, S]) Start(ctx context.Context, input string) *Handle {
	ctx, cancel := context.WithCancelCause(ctx)
	h := &Handle{inbox: &inbox{}, cancel: cancel, done: make(chan struct{})}
	if _, ok := a.llmClient.(llm.ToolStreamClient); !ok || len(a.tools) == 0 {
		h.err = errors.New("Handle 需要原生工具流：llm.ToolStreamClient 且配置了工具")
		h.inbox.done = true
		close(h.done)
		cancel(nil)
		return h
	}
	go h.run(ctx, func(c context.Context, in string) (*AgentOutput, error) { return a.AsRunnable().Invoke(c, in) }, input)
	return h
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
		if err != nil && ctx.Err() != nil {
			err = context.Cause(ctx)
		}
		h.inbox.mu.Lock()
		h.out, h.err = out, err
		next := h.inbox.followups
		h.inbox.followups = nil
		if err != nil || len(next) == 0 {
			h.inbox.done = true
			h.inbox.mu.Unlock()
			return
		}
		history = h.inbox.transcript
		h.inbox.mu.Unlock()
		input = strings.Join(next, "\n\n")
	}
}

// Steer 排到下一步：当前工具批次结束后作为 user 消息追加；模型即将结束本轮时唤醒续跑。
func (h *Handle) Steer(msg string) error { return h.inbox.push(inboxSteer, msg) }

// Inject 排到下一步，随下一次模型请求发送，但不唤醒即将结束的轮次。
func (h *Handle) Inject(msg string) error { return h.inbox.push(inboxInject, msg) }

// Followup 排到本轮结束之后，作为新一轮的用户输入，续接本轮的对话。
func (h *Handle) Followup(msg string) error {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	if h.inbox.done {
		return ErrHandleDone
	}
	h.inbox.followups = append(h.inbox.followups, msg)
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
	h.inbox.items = append(h.inbox.items, inboxItem{Kind: inboxSteer, Text: msg})
	h.inbox.mu.Unlock()
	return nil
}

// Interrupt 中止运行，Wait 返回 cause（为 nil 时为 context.Canceled）。
func (h *Handle) Interrupt(cause error) { h.cancel(cause) }

// Wait 等待全部轮次结束，返回最后一轮的结果。
func (h *Handle) Wait(ctx context.Context) (*AgentOutput, error) {
	select {
	case <-h.done:
		return h.out, h.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
