package core

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/kart-io/goagent/interfaces"
)

// StreamMode 描述实际输出能力，不能从方法名 Stream 推断首字延迟。
type StreamMode string

const (
	StreamModeFinal  StreamMode = "final"
	StreamModeSteps  StreamMode = "steps"
	StreamModeTokens StreamMode = "tokens"
)

// StreamEvent 保存跨 Runnable 的业务事件，Data 仍使用原有泛型输出类型。
// Usage 是累计快照，不应在消费端将多个快照再次相加。
type StreamEvent struct {
	Type    string                       `json:"type"`
	Delta   string                       `json:"delta,omitempty"`
	Step    string                       `json:"step,omitempty"`
	Tool    string                       `json:"tool,omitempty"`
	Sources []interfaces.RetrievalSource `json:"sources,omitempty"`
	Usage   *interfaces.TokenUsage       `json:"usage,omitempty"`
}

// StreamCapability 让调用方显式区分最终块、步骤流和原生文本增量。
type StreamCapability interface{ StreamMode() StreamMode }

// StreamSource 必须串行调用 emit，并在返回前停止发送。
// 成功时恰好提交一个 Done 块；错误通过返回值传播。emit 的错误必须返回。
type StreamSource[I, O any] func(context.Context, I, func(StreamChunk[O]) error) error

// StreamRunnable 从同一个执行源提供 Invoke、Stream、Batch 与 Pipe。
// 最终块在执行源和完成回调均成功后发出，避免错误后仍出现成功终止块。
type StreamRunnable[I, O any] struct {
	*BaseRunnable[I, O]
	source      StreamSource[I, O]
	mode        StreamMode
	description func() any
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
		return result, fmt.Errorf("流式执行源未配置")
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
				err = panicErr
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
		case finished:
			emitErr = fmt.Errorf("最终块之后不能继续发送事件")
		case chunk.Error != nil:
			emitErr = chunk.Error
		case chunk.Done:
			final, finished = chunk, true
		default:
			emitErr = send(chunk)
		}
		return emitErr
	})
	if err = errors.Join(err, emitErr, ctx.Err()); err != nil {
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
	err = send(final)
	return result, err
}

// NotifyCallbacks 依次调用宿主回调，遇到错误即停止，供所有调用 Callback 的组件共用。
// 回调自身 panic 时 panicked 为 true，err 可用 errors.As 识别出 *agentErrors.PanicError（经全局 PanicHandler 转换）；
// 调用方应返回该错误，不再调用剩余回调，也不把它交给 OnError，避免回调处理自身 panic 形成递归
// （与 builder 的 handleError/notifyEnd 一致）。忽略回调错误的通知（如 OnError）让 call 返回 nil。
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
		return nil, fmt.Errorf("流式执行源未配置")
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
