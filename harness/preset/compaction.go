package preset

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/llm"
)

// ComponentCompaction 是对话压缩组件名。
const ComponentCompaction = "compaction"

// CompactionConfig 是 compaction 组件的配置。估算的对话长度超过 MaxTokens 时，把较早的消息交给
// Host.CompactionModel 写成一条摘要，最近约 KeepTokens 的消息与本步新收到的输入原样保留；
// 模型请求因超出上下文失败时（非持久运行）强制压缩一次后重试。压缩发生在 PreStep，持久运行中摘要随该步记入日志。
// 只作用于原生工具流。
type CompactionConfig struct {
	// MaxTokens 是触发压缩的估算 token 数，默认 100000。按 UTF-8 字节数的三分之一估算，对英文偏保守。
	MaxTokens int `yaml:"maxTokens"`
	// KeepTokens 是原样保留的最近消息的估算 token 数，默认 MaxTokens 的四分之一。
	KeepTokens int `yaml:"keepTokens"`
	// Instructions 是对摘要的补充要求，如必须保留的信息。
	Instructions string `yaml:"instructions"`
}

func init() {
	Register(Spec[CompactionConfig]{
		Name: ComponentCompaction, Version: "1", Singleton: true,
		Validate: func(c *CompactionConfig) error {
			if c.MaxTokens == 0 {
				c.MaxTokens = 100000
			}
			if c.KeepTokens == 0 {
				c.KeepTokens = c.MaxTokens / 4
			}
			if c.MaxTokens < 0 || c.KeepTokens < 0 {
				return errors.New("maxTokens 与 keepTokens 不能为负")
			}
			if c.KeepTokens >= c.MaxTokens {
				return errors.New("keepTokens 须小于 maxTokens")
			}
			return nil
		},
		Build: func(_ context.Context, a *Assembler, c *CompactionConfig) error {
			if a.host.CompactionModel == nil {
				return errors.New("压缩需要 Host.CompactionModel")
			}
			cp := &compactor{model: a.host.CompactionModel, cfg: *c}
			a.AddPreStep(cp.preStep)
			a.AddRequestError(cp.requestError)
			return nil
		},
	})
}

type compactor struct {
	model llm.Client
	cfg   CompactionConfig
}

const compactionPrompt = "你负责压缩 Agent 的对话历史。把用户给出的对话整理成摘要，供 Agent 接着完成任务：" +
	"保留用户的目标与要求、已经做出的决定、已完成与未完成的工作、关键事实（文件路径、命令、错误信息、数值）以及尚未解决的问题；" +
	"省略寒暄与重复内容。只输出摘要。"

// summaryPrefix 标记摘要消息。
const summaryPrefix = "[此前对话的摘要]\n"

// 摘要请求中单条消息的上限，避免个别超长的工具结果撑满摘要模型的上下文。
const maxRenderedRunes = 4000

func (cp *compactor) preStep(ctx context.Context, step *builder.Step) (builder.PreStepResult, error) {
	if estimateTokens(step.Messages) <= cp.cfg.MaxTokens {
		return builder.PreStepResult{}, nil
	}
	out, _, err := cp.compact(ctx, step.Messages, step.Inputs)
	if err != nil {
		return builder.PreStepResult{}, fmt.Errorf("压缩对话: %w", err)
	}
	step.Messages = out
	return builder.PreStepResult{}, nil
}

func (cp *compactor) requestError(ctx context.Context, f *builder.RequestFailure) (builder.RequestErrorDecision, error) {
	if !f.Class.ShouldCompress {
		return builder.RequestErrorDecision{}, nil
	}
	out, changed, err := cp.compact(ctx, f.Messages, 0)
	if err != nil || !changed {
		return builder.RequestErrorDecision{}, err
	}
	return builder.RequestErrorDecision{Action: builder.RequestRetry, Messages: out}, nil
}

// compact 把 system 之后、保留区之前的消息替换为一条摘要。保留区从非 tool 消息开始，
// 因而不会拆开 assistant 的工具调用与其结果；末尾 inputs 条消息（至少一条）总在保留区内。
// 找不到可压缩的区间时原样返回，changed 为假。
func (cp *compactor) compact(ctx context.Context, messages []llm.Message, inputs int) (out []llm.Message, changed bool, err error) {
	head := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		head = 1
	}
	limit := len(messages) - max(inputs, 1)
	cut := -1
	tail := 0
	for _, m := range messages[max(limit, 0):] {
		tail += messageTokens(m)
	}
	for i := limit; i > head; i-- {
		if i < limit {
			tail += messageTokens(messages[i])
		}
		if messages[i].Role == "tool" {
			continue
		}
		if cut < 0 || tail <= cp.cfg.KeepTokens {
			cut = i
		}
		if tail > cp.cfg.KeepTokens {
			break
		}
	}
	if cut <= head {
		return messages, false, nil
	}
	resp, err := cp.model.Chat(ctx, []llm.Message{
		llm.SystemMessage(strings.TrimSpace(compactionPrompt + "\n" + cp.cfg.Instructions)),
		llm.UserMessage(renderTranscript(messages[head:cut])),
	})
	if err != nil {
		return nil, false, err
	}
	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		return nil, false, errors.New("摘要模型返回了空内容")
	}
	out = append([]llm.Message{}, messages[:head]...)
	out = append(out, llm.UserMessage(summaryPrefix+summary))
	return append(out, messages[cut:]...), true, nil
}

// renderTranscript 把消息写成纯文本交给摘要模型，不依赖提供方对工具调用消息的支持。
func renderTranscript(messages []llm.Message) string {
	var sb strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&sb, "[%s]\n%s\n", m.Role, truncateRunes(m.Content, maxRenderedRunes))
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

func estimateTokens(messages []llm.Message) int {
	n := 0
	for _, m := range messages {
		n += messageTokens(m)
	}
	return n
}

// messageTokens 按 UTF-8 字节数的三分之一估算：中文约一字一 token，英文约四字符一 token，因此偏保守。
func messageTokens(m llm.Message) int {
	n := len(m.Content) + len(m.ReasoningContent)
	for _, tc := range m.ToolCalls {
		n += len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	return n/3 + 4
}
