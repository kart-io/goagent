package builder

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/builder/contextpolicy"
)

// WithCompaction installs c as a PreStep with an identity (c.Identity()): when
// the conversation exceeds the threshold, older messages are replaced with a
// summary. It also appends a RequestError hook: when a request fails because it
// exceeds the context, it keeps only the most recent exchange, forces one
// compaction, and retries; that retry counts against WithRequestError's
// maxRecoveries, so with the default of 0 it never runs. It applies only to the
// native tool stream. To let
// the model retrieve compacted originals, register contextpolicy.NewRecallTool
// separately.
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
	return b
}
