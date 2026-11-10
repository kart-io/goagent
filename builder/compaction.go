package builder

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/builder/contextpolicy"
)

// WithCompaction 把 c 装为带身份（c.Identity()）的 PreStep：对话超过阈值时把较早的消息换成摘要；
// 并追加一个 RequestError 钩子，请求因超出上下文失败时只保留最近一次交互强制压缩一次后重试。仅作用于原生工具流。
// 需要模型取回被压缩的原文时，另行注册 contextpolicy.NewRecallTool。
func (b *AgentBuilder[S]) WithCompaction(c *contextpolicy.Compaction) *AgentBuilder[S] {
	if c == nil {
		return b
	}
	b.WithIdentifiedPreStep("compaction/"+c.Identity(), func(ctx context.Context, step *Step) (PreStepResult, error) {
		messages, err := c.Reduce(ctx, step.Messages, step.Inputs)
		if err != nil {
			return PreStepResult{}, fmt.Errorf("压缩对话: %w", err)
		}
		step.Messages = messages
		return PreStepResult{}, nil
	})
	b.config.RequestErrorHooks = append(b.config.RequestErrorHooks, func(ctx context.Context, f *RequestFailure) (RequestErrorDecision, error) {
		if !f.Class.ShouldCompress {
			return RequestErrorDecision{}, nil
		}
		messages, changed, err := c.Recover(ctx, f.Messages)
		if err != nil || !changed {
			return RequestErrorDecision{}, err
		}
		return RequestErrorDecision{Action: RequestRetry, Messages: messages}, nil
	})
	if b.config.MaxRequestRecoveries == 0 {
		b.config.MaxRequestRecoveries = 3
	}
	return b
}
