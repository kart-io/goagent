package core

import (
	"context"
	"errors"
	"io"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// StreamMode describes the actual output capability; time to first token cannot
// be inferred from the method name Stream.
type StreamMode string

const (
	StreamModeFinal  StreamMode = "final"
	StreamModeSteps  StreamMode = "steps"
	StreamModeTokens StreamMode = "tokens"
)

// StreamEvent holds business events across Runnables; Data still uses the
// existing generic output type. Usage is a cumulative snapshot; consumers
// should not add multiple snapshots together.
type StreamEvent struct {
	Type    string                       `json:"type"`
	Delta   string                       `json:"delta,omitempty"`
	Step    string                       `json:"step,omitempty"`
	Tool    string                       `json:"tool,omitempty"`
	Sources []interfaces.RetrievalSource `json:"sources,omitempty"`
	Usage   *interfaces.TokenUsage       `json:"usage,omitzero"`
}

// StreamCapability lets callers explicitly distinguish final chunks, step
// streams, and native text deltas.
type StreamCapability interface{ StreamMode() StreamMode }

// StreamSource must call emit serially and stop emitting before it returns. On
// success it commits exactly one Done chunk; errors propagate through the
// return value. Errors from emit must be returned.
type StreamSource[I, O any] func(context.Context, I, func(StreamChunk[O]) error) error

// StreamRunnable provides Invoke, Stream, Batch, and Pipe from a single
// execution source. The final chunk is emitted only after both the source and
// the completion callbacks succeed, so no successful terminal chunk appears
// after an error.
type StreamRunnable[I, O any] struct {
	*BaseRunnable[I, O]
	source          StreamSource[I, O]
	mode            StreamMode
	description     func() any
	finalValidators []func(context.Context, O) error
}

func NewStreamRunnable[I, O any](mode StreamMode, source StreamSource[I, O]) *StreamRunnable[I, O] {
	return &StreamRunnable[I, O]{BaseRunnable: NewBaseRunnable[I, O](), source: source, mode: mode}
}

func (r *StreamRunnable[I, O]) StreamMode() StreamMode { return r.mode }

func (r *StreamRunnable[I, O]) run(ctx context.Context, input I, send func(StreamChunk[O]) error) (result O, err error) {
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if r.source == nil {
		return result, agentErrors.Newf(agentErrors.CodeInvalidConfig, "流式执行源未配置").WithComponent("stream_runnable").WithOperation("run")
	}
	callbacks := r.GetConfig().Callbacks
	callbackPanicked := false
	defer func() {
		if recovered := recover(); recovered != nil {
			err = panicToError(ctx, "stream_runnable", "run", recovered)
		}
		if err != nil && !callbackPanicked {
			failure := err
			if panicked, panicErr := NotifyCallbacks(ctx, "stream_runnable", "on_error", callbacks, func(cb Callback) error {
				_ = cb.OnError(ctx, failure)
				return nil
			}); panicked {
				// 配置验收时保留原错误；无验收时沿用回调异常优先的既有契约。
				err = panicErr
				if len(r.finalValidators) > 0 {
					err = errors.Join(failure, panicErr)
				}
			}
		}
	}()
	if callbackPanicked, err = NotifyCallbacks(ctx, "stream_runnable", "on_start", callbacks, func(cb Callback) error {
		return cb.OnStart(ctx, input)
	}); err != nil {
		return result, err
	}
	var final StreamChunk[O]
	finished := false
	var emitErr error
	err = r.source(ctx, input, func(chunk StreamChunk[O]) error {
		if emitErr != nil {
			return emitErr
		}
		switch {
		case ctx.Err() != nil:
			emitErr = ctx.Err()
		case chunk.Error != nil:
			emitErr = chunk.Error
		case finished:
			emitErr = agentErrors.Newf(agentErrors.CodeInvalidOutput, "最终块之后不能继续发送事件").WithComponent("stream_runnable").WithOperation("run")
		case chunk.Done:
			final, finished = chunk, true
		default:
			emitErr = send(chunk)
		}
		return emitErr
	})
	// 只有执行源报错时保留其具体类型，兼容现有调用方。
	for _, additional := range []error{emitErr, ctx.Err()} {
		if additional != nil && !errors.Is(err, additional) {
			if err == nil {
				err = additional
			} else {
				err = errors.Join(err, additional)
			}
		}
	}
	if err != nil {
		return result, err
	}
	if !finished {
		return result, io.ErrUnexpectedEOF
	}
	result = final.Data
	if callbackPanicked, err = NotifyCallbacks(ctx, "stream_runnable", "on_end", callbacks, func(cb Callback) error {
		return cb.OnEnd(ctx, result)
	}); err != nil {
		return result, err
	}
	for _, validate := range r.finalValidators {
		if validate == nil {
			var zero O
			return zero, agentErrors.Newf(agentErrors.CodeInvalidConfig, "最终验收器不能为空").WithComponent("stream_runnable").WithOperation("run")
		}
		if err = validate(ctx, result); err != nil {
			var zero O
			return zero, err
		}
	}
	if err = ctx.Err(); err != nil {
		var zero O
		return zero, err
	}
	err = send(final)
	return result, err
}

// NotifyCallbacks invokes host callbacks in order and stops at the first error;
// all components that call Callback share it. If a callback itself panics,
// panicked is true and errors.As can detect *agentErrors.PanicError in err
// (converted by the global PanicHandler). The caller should return that error,
// not call the remaining callbacks, and not pass it to OnError, to avoid
// recursion from callbacks handling their own panic (consistent with the
// builder's handleError/notifyEnd). For notifications that ignore callback
// errors (such as OnError), have call return nil.
func NotifyCallbacks(ctx context.Context, component, operation string, callbacks []Callback, call func(Callback) error) (panicked bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicked, err = true, panicToError(ctx, component, operation, recovered)
		}
	}()
	for _, cb := range callbacks {
		if err = call(cb); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (r *StreamRunnable[I, O]) Invoke(ctx context.Context, input I) (O, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return r.run(ctx, input, func(StreamChunk[O]) error { return nil })
}

func (r *StreamRunnable[I, O]) Stream(ctx context.Context, input I) (<-chan StreamChunk[O], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.source == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "流式执行源未配置").WithComponent("stream_runnable").WithOperation("stream")
	}
	output := make(chan StreamChunk[O], 1)
	go func() {
		defer close(output)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		send := func(chunk StreamChunk[O]) error {
			select {
			case output <- chunk:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if _, err := r.run(ctx, input, send); err != nil {
			_ = send(StreamChunk[O]{Done: true, Error: err})
		}
	}()
	return output, nil
}

func (r *StreamRunnable[I, O]) Batch(ctx context.Context, inputs []I) ([]O, error) {
	return r.BaseRunnable.Batch(ctx, inputs, r.Invoke)
}

func (r *StreamRunnable[I, O]) Pipe(next Runnable[O, any]) Runnable[I, any] {
	return NewRunnablePipe[I, O, any](r, next)
}

func (r *StreamRunnable[I, O]) WithConfig(config RunnableConfig) Runnable[I, O] {
	clone := *r
	clone.BaseRunnable = r.BaseRunnable.WithConfig(config)
	return &clone
}

func (r *StreamRunnable[I, O]) WithCallbacks(callbacks ...Callback) Runnable[I, O] {
	clone := *r
	clone.BaseRunnable = r.BaseRunnable.WithCallbacks(callbacks...)
	return &clone
}

// WithDescription attaches an optional read-only description factory without changing Runnable.
func (r *StreamRunnable[I, O]) WithDescription(describe func() any) *StreamRunnable[I, O] {
	copy := *r
	copy.description = describe
	return &copy
}

// Description returns a fresh metadata value, if supplied by the compiler.
func (r *StreamRunnable[I, O]) Description() any {
	if r.description == nil {
		return nil
	}
	return r.description()
}

// forwardRunnableStream 收到 Done 后仍等待通道关闭，由共同执行源检查
// 缺失或重复终态，以及最终块之后的错误。
// 取消会解除本适配器的接收和发送阻塞。忽略 ctx 的第三方 Invoke、Stream
// 或生产者函数无法被强制停止；生产者发送时必须同时监听 ctx.Done，退出时关闭通道。
func forwardRunnableStream[T any](ctx context.Context, stream <-chan StreamChunk[T], emit func(StreamChunk[T]) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-stream:
			if !ok {
				return nil
			}
			if err := emit(chunk); err != nil {
				return err
			}
		}
	}
}

// WithFinalValidator appends validators that run before a successful terminal
// result is published; WithConfig and WithCallbacks keep these validators.
// Validation runs after the completion callbacks and must be read-only and safe
// to call repeatedly; it does not roll back callbacks or external effects that
// have already run.
func (r *StreamRunnable[I, O]) WithFinalValidator(validators ...func(context.Context, O) error) *StreamRunnable[I, O] {
	copy := *r
	copy.finalValidators = append(append([]func(context.Context, O) error(nil), r.finalValidators...), validators...)
	return &copy
}
