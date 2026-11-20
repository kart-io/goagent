package contextpolicy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/budget"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// CompactionConfig configures conversation compaction. When the estimated
// length exceeds MaxTokens, the messages after system and before roughly the
// last KeepTokens are replaced with one summary. Limits are caller policy: 0
// means unlimited, negative values are rejected.
type CompactionConfig struct {
	// ID and Version identify the configuration; functions such as Keep and
	// Annotate cannot be compared, so Version must change when their behavior
	// changes.
	ID      string `json:"id"`
	Version string `json:"version"`
	// MaxTokens is the estimated token count that triggers compaction. 0 never
	// compacts proactively; Recover still compacts after a context overflow.
	// Before limits became caller-owned 0 meant 100000.
	MaxTokens int `json:"max_tokens"`
	// KeepTokens is the estimated token count of recent messages kept verbatim;
	// it must be less than MaxTokens. 0 means MaxTokens/4, so it is required
	// when MaxTokens is 0.
	KeepTokens int `json:"keep_tokens"`
	// MaxRenderedRunes caps one message in the summary request so a single huge
	// tool result cannot fill the summary model's context; 0 sends messages
	// whole. Before limits became caller-owned it was fixed at 4000.
	MaxRenderedRunes int `json:"max_rendered_runes,omitzero"`
	// Instructions are additional requirements for the summary, such as
	// information that must be preserved.
	Instructions string `json:"instructions"`
	// Model generates the summary; when nil, no model is requested and old
	// messages are only omitted with a note of what was omitted.
	Model llm.Client `json:"-"`
	// Estimate estimates the token count of one message; by default it is one
	// third of the UTF-8 byte count, which is conservative for English.
	Estimate func(llm.Message) int `json:"-"`
	// Keep selects messages to keep verbatim from the compacted span (in
	// original order); limit is the available estimated token count
	// (KeepTokens/2). By default it keeps the first and the most recent
	// original user messages.
	Keep func(span []llm.Message, limit int) []llm.Message `json:"-"`
	// Annotate returns a note appended after the summary (such as to-dos); an
	// empty string means none. On later compactions the note is not passed to
	// the summary model.
	Annotate func(span []llm.Message) string `json:"-"`
}

// Compaction compacts overly long conversations. The Builder installs it via
// WithCompaction as a PreStep with an identity, and when a request fails
// because it exceeds the context, forces one compaction and retries. The
// rewritten conversation is recorded by the session log as history/rewrite: the
// originals stay in the log and can be retrieved via RecallTool, Rewind undoes
// the summary, and durable replay does not request the summary model again.
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
)

// NewCompaction validates the configuration and fills in defaults.
func NewCompaction(cfg CompactionConfig) (*Compaction, error) {
	if cfg.ID == "" {
		cfg.ID = "compaction"
	}
	if cfg.Version == "" {
		cfg.Version = "v1"
	}
	if cfg.KeepTokens == 0 {
		cfg.KeepTokens = cfg.MaxTokens / 4
	}
	if cfg.MaxTokens < 0 || cfg.KeepTokens < 0 || (cfg.MaxTokens == 0 && cfg.KeepTokens == 0) || (cfg.MaxTokens > 0 && cfg.KeepTokens >= cfg.MaxTokens) || cfg.MaxRenderedRunes < 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "compaction 需要 0 < keep_tokens，设置 max_tokens 时 keep_tokens < max_tokens，且上限不能为负").WithComponent("builder/contextpolicy").WithOperation("new_compaction")
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
		Render                    int `json:",omitzero"`
	}{cfg.ID, cfg.Version, cfg.Instructions, cfg.MaxTokens, cfg.KeepTokens, cfg.Model != nil, cfg.MaxRenderedRunes})
	return &Compaction{config: cfg, identity: cfg.ID + ":" + cfg.Version + ":" + digest(fingerprint)}, nil
}

// Identity identifies the configuration; it enters the durable journal as the
// PreStep identity, so old journals are not reused after the configuration
// changes.
func (c *Compaction) Identity() string { return c.identity }

// Reduce compacts the conversation when it exceeds MaxTokens; inputs is the
// number of inputs newly received this step at the end, and those messages are
// always kept verbatim.
func (c *Compaction) Reduce(ctx context.Context, messages []llm.Message, inputs int) ([]llm.Message, error) {
	if c.config.MaxTokens == 0 || c.estimate(messages) <= c.config.MaxTokens {
		return messages, nil
	}
	out, _, err := c.compact(ctx, messages, inputs, c.config.KeepTokens)
	return out, err
}

// Recover compacts, keeping only the most recent exchange, when a request has
// already exceeded the context; changed is false when there is nothing to
// compact.
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
		llm.UserMessage(transcriptOpen + renderTranscript(span, c.config.MaxRenderedRunes) + transcriptClose + fmt.Sprintf("摘要不超过 %d 字，篇幅紧张时只留仍影响后续工作的信息。", c.config.KeepTokens/2)),
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

// IsSummary reports whether m is a summary message produced by compaction.
func IsSummary(m llm.Message) bool {
	return m.Role == constants.RoleUser && strings.HasPrefix(m.Content, SummaryPrefix)
}

// SummaryBody returns the body of a summary message, without the prefix and
// note.
func SummaryBody(content string) string {
	content = strings.TrimPrefix(content, SummaryPrefix)
	if i := strings.Index(content, annotationMark); i >= 0 {
		content = content[:i]
	}
	return content
}

// KeepUserMessages selects original user messages from span: it keeps the first
// one (usually the original task) and then works backward from the most recent,
// with a total of at most limit estimated tokens. Summaries and messages for
// which skip returns true do not count as original user messages.
func KeepUserMessages(span []llm.Message, limit int, estimate func(llm.Message) int, skip func(llm.Message) bool) []llm.Message {
	var users []int
	for i, m := range span {
		if m.Role == constants.RoleUser && !IsSummary(m) && (skip == nil || !skip(m)) {
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
// limit 是单条消息的字数上限，0 = 不截断。
func renderTranscript(messages []llm.Message, limit int) string {
	clip := func(s string) string {
		if limit > 0 {
			return truncateRunes(s, limit)
		}
		return s
	}
	var sb strings.Builder
	for _, m := range messages {
		content := m.Content
		// 旧摘要不截断，否则多次压缩逐级失真。
		if IsSummary(m) {
			content = SummaryPrefix + SummaryBody(content)
		} else {
			content = clip(content)
		}
		fmt.Fprintf(&sb, "[%s]\n%s\n", m.Role, content)
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(&sb, "调用工具 %s：%s\n", tc.Function.Name, clip(tc.Function.Arguments))
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

// EstimateTokens estimates one third of the UTF-8 byte count: about one token
// per Chinese character and about one token per four English characters, so it
// is conservative.
func EstimateTokens(m llm.Message) int {
	n := len(m.Content) + len(m.ReasoningContent)
	for _, tc := range m.ToolCalls {
		n += len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	return n/3 + 4
}
