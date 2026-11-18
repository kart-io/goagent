package builder

import (
	"context"
	"io"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

// AsRunnable adapts the Builder to the standard generic execution contract.
// Invoke collects the final result of ExecuteStream, so tools, retrieval,
// middleware, and usage follow the same path. It requires a StreamClient;
// buffered models can declare their actual streaming capability. The state and
// memory of one Builder belong to one session, so the adapter executes
// serially, and waiting can be canceled.
func (a *ConfigurableAgent[S]) AsRunnable() *core.StreamRunnable[any, *AgentOutput] {
	mode := core.StreamModeTokens
	if capability, ok := a.llmClient.(core.StreamCapability); ok {
		mode = capability.StreamMode()
		if mode == core.StreamModeFinal {
			mode = core.StreamModeSteps
		}
	}
	r := core.NewStreamRunnable(mode, func(ctx context.Context, input any, emit func(core.StreamChunk[*AgentOutput]) error) error {
		events, err := a.ExecuteStream(ctx, input)
		if err != nil {
			return err
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case event, ok := <-events:
				if !ok {
					return io.ErrUnexpectedEOF
				}
				if event.Error != nil {
					return event.Error
				}
				if event.Type == "error" {
					return agentErrors.Newf(agentErrors.CodeAgentExecution, "执行失败：%s", event.Message).WithComponent("builder").WithOperation("as_runnable")
				}
				chunk := core.StreamChunk[*AgentOutput]{
					Data: event.Output, Done: event.Type == "done",
					Event: &core.StreamEvent{Type: event.Type, Delta: event.Delta, Step: event.Step, Tool: event.Tool, Sources: event.Sources, Usage: event.Usage},
				}
				if chunk.Done && chunk.Data == nil {
					return agentErrors.Newf(agentErrors.CodeInvalidOutput, "最终事件缺少输出").WithComponent("builder").WithOperation("as_runnable")
				}
				if err := emit(chunk); err != nil {
					return err
				}
				if chunk.Done {
					return nil
				}
			}
		}
	})
	if len(a.config.OutputValidators) > 0 {
		return r.WithFinalValidator(a.validateOutput)
	}
	return r
}
