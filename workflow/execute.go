package workflow

import (
	"context"
	"fmt"
	"io"

	"github.com/kart-io/goagent/core"
)

func (g *compiled[T]) run(ctx context.Context, state T, emit func(core.StreamChunk[T]) error) error {
	name := g.entry
	for step := 0; step < g.config.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := emit(core.StreamChunk[T]{Event: &core.StreamEvent{Type: "node_start", Step: name}}); err != nil {
			return err
		}
		next, err := g.nodes[name](ctx, state, func(chunk core.StreamChunk[T]) error {
			if chunk.Event != nil {
				event := *chunk.Event
				event.Step = name
				if chunk.Event.Step != "" {
					event.Step += "/" + chunk.Event.Step
				}
				chunk.Event = &event
			}
			return emit(chunk)
		})
		if err != nil {
			return fmt.Errorf("node %q: %w", name, err)
		}
		state = next
		if err := emit(core.StreamChunk[T]{Event: &core.StreamEvent{Type: "node_end", Step: name}}); err != nil {
			return err
		}
		target := g.edges[name]
		if branch, ok := g.branches[name]; ok {
			target, err = branch.choose(ctx, state)
			if err != nil {
				return fmt.Errorf("branch %q: %w", name, err)
			}
			if !branch.targets[target] {
				return fmt.Errorf("branch %q selected undeclared destination %q", name, target)
			}
		}
		if target == END {
			return emit(core.StreamChunk[T]{Done: true, Data: state})
		}
		name = target
	}
	return fmt.Errorf("%w: limit %d", ErrMaxSteps, g.config.MaxSteps)
}

// Node finalization is local: drain until close to detect late errors and
// duplicate final chunks before scheduling downstream work.
func consumeNode[I, O any](ctx context.Context, node core.Runnable[I, O], state I, emit func(core.StreamChunk[O]) error) (result O, err error) {
	nodeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	chunks, err := node.Stream(nodeCtx, state)
	if err != nil {
		return result, err
	}
	if chunks == nil {
		return result, fmt.Errorf("node returned a nil stream")
	}
	finished := false
	for {
		select {
		case <-nodeCtx.Done():
			return result, nodeCtx.Err()
		case chunk, ok := <-chunks:
			if !ok {
				if err := nodeCtx.Err(); err != nil {
					return result, err
				}
				if !finished {
					return result, io.ErrUnexpectedEOF
				}
				return result, nil
			}
			if chunk.Error != nil {
				return result, chunk.Error
			}
			if finished {
				return result, fmt.Errorf("node emitted data after its final chunk")
			}
			if chunk.Done {
				result = chunk.Data
				finished = true
				continue
			}
			if err := emit(chunk); err != nil {
				return result, err
			}
		}
	}
}
