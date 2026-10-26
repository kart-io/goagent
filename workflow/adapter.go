package workflow

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/core"
)

// Adapt explicitly maps a Runnable's input and final output to graph state.
// Intermediate events pass through; intermediate typed data is not implicitly
// converted. Output mapping runs only after a valid, completed node stream.
func Adapt[S, I, O any](runnable core.Runnable[I, O], input func(S) (I, error), output func(S, O) (S, error)) (*core.StreamRunnable[S, S], error) {
	if isNil(runnable) || input == nil || output == nil {
		return nil, fmt.Errorf("workflow Adapt requires a runnable and both state mappings")
	}
	mode := core.StreamModeSteps
	if capability, ok := runnable.(core.StreamCapability); ok {
		mode = capability.StreamMode()
	}
	return core.NewStreamRunnable(mode, func(ctx context.Context, state S, emit func(core.StreamChunk[S]) error) error {
		in, err := input(state)
		if err != nil {
			return fmt.Errorf("workflow input mapping: %w", err)
		}
		final, err := consumeNode(ctx, runnable, in, func(chunk core.StreamChunk[O]) error {
			if chunk.Event == nil {
				return nil
			}
			return emit(core.StreamChunk[S]{Event: chunk.Event})
		})
		if err != nil {
			return err
		}
		result, err := output(state, final)
		if err != nil {
			return fmt.Errorf("workflow output mapping: %w", err)
		}
		return emit(core.StreamChunk[S]{Done: true, Data: result})
	}), nil
}
