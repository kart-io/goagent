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
	return adapt(runnable, input, output, nil)
}

// AdaptStream maps intermediate chunks explicitly, including typed Data. These
// are previews only: graph routing and durable state use the validated final
// output. The mapper must not mutate input state or return Done; it may map or
// preserve Event metadata. Replayed nodes may emit previews again.
func AdaptStream[S, I, O any](runnable core.Runnable[I, O], input func(S) (I, error), output func(S, O) (S, error), intermediate func(S, core.StreamChunk[O]) (core.StreamChunk[S], error)) (*core.StreamRunnable[S, S], error) {
	if intermediate == nil {
		return nil, fmt.Errorf("workflow AdaptStream requires an intermediate mapping")
	}
	return adapt(runnable, input, output, intermediate)
}

func adapt[S, I, O any](runnable core.Runnable[I, O], input func(S) (I, error), output func(S, O) (S, error), intermediate func(S, core.StreamChunk[O]) (core.StreamChunk[S], error)) (*core.StreamRunnable[S, S], error) {
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
			if intermediate != nil {
				mapped, err := intermediate(state, chunk)
				if err != nil {
					return fmt.Errorf("workflow intermediate mapping: %w", err)
				}
				if mapped.Done {
					return fmt.Errorf("workflow intermediate mapping cannot finalize a node")
				}
				return emit(mapped)
			}
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
