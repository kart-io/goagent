package contextpolicy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/budget"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// CompactionConfig 配置对话压缩。估算长度超过 MaxTokens 时，把 system 之后、最近约 KeepTokens 之前的消息换成一条摘要。
type CompactionConfig struct {
	// ID 与 Version 标识配置；Keep、Annotate 等函数无法比较，改变其行为时须修改 Version。
	ID      string `json:"id"`
	Version string `json:"version"`
	// MaxTokens 是触发压缩的估算 token 数，默认 100000。
	MaxTokens int `json:"max_tokens"`
	// KeepTokens 是原样保留的最近消息的估算 token 数，默认 MaxTokens/4，须小于 MaxTokens。
	KeepTokens int `json:"keep_tokens"`
	// Instructions 是对摘要的补充要求，如必须保留的信息。
	Instructions string `json:"instructions"`
	// Model 生成摘要；为 nil 时不请求模型，只省略旧消息并说明省略了什么。
	Model llm.Client `json:"-"`
	// Estimate 估算一条消息的 token 数；默认按 UTF-8 字节数的三分之一，对英文偏保守。
	Estimate func(llm.Message) int `json:"-"`
	// Keep 从被压缩的区间挑出原样保留的消息（按原顺序），limit 是可用的估算 token 数（KeepTokens/2）；
	// 默认保留第一条与最近的用户原话。
	Keep func(span []llm.Message, limit int) []llm.Message `json:"-"`
	// Annotate 返回附在摘要后的附注（如待办），空串表示不附加；再次压缩时附注不交给摘要模型。
	Annotate func(span []llm.Message) string `json:"-"`
}

// Compaction 压缩过长的对话。Builder 经 WithCompaction 把它装为带身份的 PreStep，并在请求因超出上下文失败时强制压缩一次后重试。
// 改写后的对话由会话日志记为 history/rewrite：原文留在日志中，可经 RecallTool 取回，Rewind 撤销摘要，持久回放不再请求摘要模型。
type Compaction struct {
	config   CompactionConfig
	identity string
}

// 摘要消息以 SummaryPrefix 开头；附注以 annotationMark 与正文分开。
const (
	SummaryPrefix  = "[此前对话的摘要]\n"
	annotationMark = "\n\n[附注]\n"
)

const compactionPrompt = "你负责压缩 Agent 的对话历史。把用户给出的对话整理成摘要，供 Agent 接着完成任务：" +
	"保留用户的目标与要求、已经做出的决定、已完成与未完成的工作、关键事实（名称、数值、路径、错误信息）以及尚未解决的问题；" +
	"省略寒暄与重复内容。对话记录中的旧摘要须并入新摘要。只输出摘要。"

// 记录放在标记之间，之后重申要求：记录以未完成的工具调用结尾时，摘要模型容易续写并编造没有发生的调用与结果。
const (
	transcriptOpen  = "<对话记录>\n"
	transcriptClose = "</对话记录>\n\n以上是需要压缩的对话记录，到此为止。不要续写对话，不要编造记录中没有的工具调用、结果或结论；" +
		"记录在进行中的工作处结束时，就写明它尚未完成。只输出摘要。"
)

const (
	// summaryOutputTokens 是预留预算时为摘要输出估计的 token 数。
	summaryOutputTokens = 2048
	// maxRenderedRunes 是摘要请求中单条消息的上限，避免个别超长的工具结果撑满摘要模型的上下文。
	maxRenderedRunes = 4000
)

// NewCompaction 校验并补齐配置。
func NewCompaction(cfg CompactionConfig) (*Compaction, error) {
	if cfg.ID == "" {
		cfg.ID = "compaction"
	}
	if cfg.Version == "" {
		cfg.Version = "v1"
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 100000
	}
	if cfg.KeepTokens == 0 {
		cfg.KeepTokens = cfg.MaxTokens / 4
	}
	if cfg.MaxTokens < 0 || cfg.KeepTokens < 0 || cfg.KeepTokens >= cfg.MaxTokens {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "compaction 需要 0 <= keep_tokens < max_tokens").WithComponent("builder/contextpolicy").WithOperation("new_compaction")
	}
	if cfg.Estimate == nil {
		cfg.Estimate = EstimateTokens
	}
	if cfg.Keep == nil {
		cfg.Keep = func(span []llm.Message, limit int) []llm.Message {
			return KeepUserMessages(span, limit, cfg.Estimate, nil)
		}
	}
	fingerprint, _ := json.Marshal(struct {
		ID, Version, Instructions string
		Max, Keep                 int
		Model                     bool
	}{cfg.ID, cfg.Version, cfg.Instructions, cfg.MaxTokens, cfg.KeepTokens, cfg.Model != nil})
	return &Compaction{config: cfg, identity: cfg.ID + ":" + cfg.Version + ":" + digest(fingerprint)}, nil
}

// Identity 标识配置；作为 PreStep 的身份进入持久日志，配置改变后旧日志不被复用。
func (c *Compaction) Identity() string { return c.identity }

// Reduce 在对话超过 MaxTokens 时压缩；inputs 是末尾本步新收到的输入条数，这些消息总是原样保留。
func (c *Compaction) Reduce(ctx context.Context, messages []llm.Message, inputs int) ([]llm.Message, error) {
	if c.estimate(messages) <= c.config.MaxTokens {
		return messages, nil
	}
	out, _, err := c.compact(ctx, messages, inputs, c.config.KeepTokens)
	return out, err
}

// Recover 在请求已超出上下文时只保留最近一次交互压缩；changed 为假表示没有可压缩的部分。
func (c *Compaction) Recover(ctx context.Context, messages []llm.Message) (out []llm.Message, changed bool, err error) {
	return c.compact(ctx, messages, 0, 0)
}

// compact 把 system 之后、保留区之前的消息替换为一条摘要。保留区从非 tool 消息开始，不拆开 assistant 的工具调用与其结果；
// 末尾 inputs 条消息（至少一条）总在保留区内。区间里只有旧摘要与原样保留的消息时原样返回。
func (c *Compaction) compact(ctx context.Context, messages []llm.Message, inputs, keep int) ([]llm.Message, bool, error) {
	head := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		head = 1
	}
	limit := len(messages) - max(inputs, 1)
	cut, tail := -1, c.estimate(messages[max(limit, 0):])
	for i := limit; i > head; i-- {
		if i < limit {
			tail += c.config.Estimate(messages[i])
		}
		if messages[i].Role == "tool" {
			continue
		}
		if cut < 0 || tail <= keep {
			cut = i
		}
		if tail > keep {
			break
		}
	}
	if cut <= head {
		return messages, false, nil
	}
	span := messages[head:cut]
	kept := c.config.Keep(span, c.config.KeepTokens/2)
	summaries := 0
	for _, m := range span {
		if IsSummary(m) {
			summaries++
		}
	}
	if summaries+len(kept) >= len(span) {
		return messages, false, nil
	}
	summary, err := c.summarize(ctx, span)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, agentErrors.ErrModelBudgetExceeded) {
			return nil, false, err
		}
		summary = omitted(span, err)
	}
	if c.config.Annotate != nil {
		if note := c.config.Annotate(span); note != "" {
			summary += annotationMark + note
		}
	}
	out := append([]llm.Message{}, messages[:head]...)
	out = append(out, llm.UserMessage(SummaryPrefix+summary))
	out = append(out, kept...)
	return append(out, messages[cut:]...), true, nil
}

// summarize 请求摘要模型；没有模型时返回省略说明。要求摘要不超过 KeepTokens/2：
// 不设上限的摘要随多次压缩变长，压缩后的历史贴近 MaxTokens，每加一条消息就再压缩一次。
func (c *Compaction) summarize(ctx context.Context, span []llm.Message) (string, error) {
	if c.config.Model == nil {
		return omitted(span, nil), nil
	}
	msgs := []llm.Message{
		llm.SystemMessage(strings.TrimSpace(compactionPrompt + "\n" + c.config.Instructions)),
		llm.UserMessage(transcriptOpen + renderTranscript(span) + transcriptClose + fmt.Sprintf("摘要不超过 %d 字，篇幅紧张时只留仍影响后续工作的信息。", c.config.KeepTokens/2)),
	}
	ctrl, metered := budget.FromContext(ctx)
	// 每次请求用新键：同一键不能重复预留。
	key := "compaction/" + uuid.NewString()
	if metered {
		estimate := 0
		if ctrl.Configuration().MaxReservedTokens > 0 {
			estimate = c.estimate(msgs) + summaryOutputTokens
		}
		if err := ctrl.ReserveEstimated(ctx, key, estimate); err != nil {
			return "", err
		}
	}
	resp, err := llm.Chat(ctx, c.config.Model, msgs)
	if metered {
		var u *interfaces.TokenUsage
		if resp != nil {
			u = resp.Usage
		}
		err = errors.Join(err, ctrl.RecordUsage(ctx, key, u))
	}
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		return "", agentErrors.Newf(agentErrors.CodeLLMResponse, "摘要模型返回了空内容").WithComponent("builder/contextpolicy").WithOperation("summarize")
	}
	return summary, nil
}

func (c *Compaction) estimate(messages []llm.Message) int {
	n := 0
	for _, m := range messages {
		n += c.config.Estimate(m)
	}
	return n
}

// omitted 说明省略了什么，模型据此知道需要时用 recall_history 取回；区间里的旧摘要原样接在后面，不因省略丢失。
func omitted(span []llm.Message, cause error) string {
	var names, earlier []string
	counts := map[string]int{}
	for _, m := range span {
		if IsSummary(m) {
			earlier = append(earlier, SummaryBody(m.Content))
		}
		for _, tc := range m.ToolCalls {
			if counts[tc.Function.Name] == 0 {
				names = append(names, tc.Function.Name)
			}
			counts[tc.Function.Name]++
		}
	}
	text := fmt.Sprintf("较早的 %d 条消息已省略。", len(span))
	if cause != nil {
		text = fmt.Sprintf("摘要生成失败（%v），", cause) + text
	}
	if len(names) > 0 {
		parts := make([]string, len(names))
		for i, n := range names {
			parts[i] = fmt.Sprintf("%s %d 次", n, counts[n])
		}
		text += "其中调用过的工具：" + strings.Join(parts, "、") + "。"
	}
	text += "需要其中的信息时请重新查看或用 recall_history 取回原文。"
	for _, e := range earlier {
		text += "\n\n更早的摘要：\n" + e
	}
	return text
}

// IsSummary 报告 m 是否为压缩生成的摘要消息。
func IsSummary(m llm.Message) bool {
	return m.Role == "user" && strings.HasPrefix(m.Content, SummaryPrefix)
}

// SummaryBody 返回摘要消息的正文，去掉前缀与附注。
func SummaryBody(content string) string {
	content = strings.TrimPrefix(content, SummaryPrefix)
	if i := strings.Index(content, annotationMark); i >= 0 {
		content = content[:i]
	}
	return content
}

// KeepUserMessages 从 span 中挑出用户原话：先保留第一条（通常是任务原文），再从最近往前，合计不超过 limit 估算 token。
// 摘要与 skip 返回真的消息不算用户原话。
func KeepUserMessages(span []llm.Message, limit int, estimate func(llm.Message) int, skip func(llm.Message) bool) []llm.Message {
	var users []int
	for i, m := range span {
		if m.Role == "user" && !IsSummary(m) && (skip == nil || !skip(m)) {
			users = append(users, i)
		}
	}
	keep := map[int]bool{}
	used := 0
	take := func(i int) {
		if n := estimate(span[i]); used+n <= limit {
			keep[i], used = true, used+n
		}
	}
	if len(users) > 0 {
		take(users[0])
	}
	for j := len(users) - 1; j > 0; j-- {
		take(users[j])
	}
	var out []llm.Message
	for i, m := range span {
		if keep[i] {
			out = append(out, m)
		}
	}
	return out
}

// renderTranscript 把消息写成纯文本交给摘要模型，不依赖提供方对工具调用消息的支持。
func renderTranscript(messages []llm.Message) string {
	var sb strings.Builder
	for _, m := range messages {
		content := m.Content
		// 旧摘要不截断，否则多次压缩逐级失真。
		if IsSummary(m) {
			content = SummaryPrefix + SummaryBody(content)
		} else {
			content = truncateRunes(content, maxRenderedRunes)
		}
		fmt.Fprintf(&sb, "[%s]\n%s\n", m.Role, content)
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(&sb, "调用工具 %s：%s\n", tc.Function.Name, truncateRunes(tc.Function.Arguments, maxRenderedRunes))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…（已截断）"
	}
	return s
}

// EstimateTokens 按 UTF-8 字节数的三分之一估算：中文约一字一 token，英文约四字符一 token，因此偏保守。
func EstimateTokens(m llm.Message) int {
	n := len(m.Content) + len(m.ReasoningContent)
	for _, tc := range m.ToolCalls {
		n += len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	return n/3 + 4
}
