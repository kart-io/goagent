package flow

import (
	"context"
	"errors"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

// Fold creates an independent accumulator per invocation. Finish is optional.
type Fold[D, O any] struct {
	Init   func() O
	Add    func(context.Context, O, D) (O, error)
	Finish func(context.Context, O) (O, error)
}

// Limits bounds admitted items and optional bytes. Size must be nonnegative.
type Limits[D any] struct {
	MaxItems int
	MaxBytes int64
	Size     func(D) int64
}

// AsRunnable publishes preview events immediately and commits data only after source and fold success.
func AsRunnable[I, D, O any](open func(context.Context, I) (Source[D], error), fold Fold[D, O], preview func(D) (*core.StreamEvent, error), limits Limits[D]) (core.Runnable[I, O], error) {
	if open == nil || fold.Init == nil || fold.Add == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "open, fold Init and Add required").WithComponent("flow").WithOperation("as_runnable")
	}
	if limits.MaxItems < 0 || limits.MaxBytes < 0 || limits.MaxBytes > 0 && limits.Size == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "invalid flow limits").WithComponent("flow").WithOperation("as_runnable")
	}
	if limits.MaxItems == 0 {
		limits.MaxItems = 1024
	}
	mode := core.StreamModeFinal
	if preview != nil {
		mode = core.StreamModeSteps
	}
	return core.NewStreamRunnable(mode, func(ctx context.Context, input I, emit func(core.StreamChunk[O]) error) error {
		source, err := open(ctx, input)
		if err != nil {
			return err
		}
		if source == nil {
			return agentErrors.Newf(agentErrors.CodeInvalidOutput, "nil source").WithComponent("flow").WithOperation("as_runnable")
		}
		acc := fold.Init()
		count := 0
		var size int64
		var first error
		err = source(ctx, func(data D) error {
			if first != nil {
				return first
			}
			if first = ctx.Err(); first != nil {
				return first
			}
			count++
			if count > limits.MaxItems {
				first = agentErrors.Newf(agentErrors.CodeExecutionLimit, "flow item limit exceeded").WithComponent("flow").WithOperation("as_runnable")
				return first
			}
			if limits.Size != nil {
				n := limits.Size(data)
				if n < 0 {
					first = agentErrors.Newf(agentErrors.CodeInvalidOutput, "negative flow size").WithComponent("flow").WithOperation("as_runnable")
					return first
				}
				if limits.MaxBytes > 0 && n > limits.MaxBytes-size {
					first = agentErrors.Newf(agentErrors.CodeExecutionLimit, "flow byte limit exceeded").WithComponent("flow").WithOperation("as_runnable")
					return first
				}
				if limits.MaxBytes > 0 {
					size += n
				}
			}
			acc, first = fold.Add(ctx, acc, data)
			if first != nil {
				return first
			}
			if preview != nil {
				var event *core.StreamEvent
				event, first = preview(data)
				if first == nil && event != nil {
					first = emit(core.StreamChunk[O]{Event: event})
				}
			}
			return first
		})
		if err = errors.Join(err, first, ctx.Err()); err != nil {
			return err
		}
		if fold.Finish != nil {
			acc, err = fold.Finish(ctx, acc)
			if err != nil {
				return err
			}
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		return emit(core.StreamChunk[O]{Done: true, Data: acc})
	}), nil
}
