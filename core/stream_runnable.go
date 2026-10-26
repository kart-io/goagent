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
	source StreamSource[I, O]
	mode   StreamMode
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
	defer func() {
		if recovered := recover(); recovered != nil {
			err = panicToError(ctx, "stream_runnable", "run", recovered)
		}
		if err != nil {
			for _, cb := range callbacks {
				_ = cb.OnError(ctx, err)
			}
		}
	}()
	for _, cb := range callbacks {
		if err = cb.OnStart(ctx, input); err != nil {
			return result, err
		}
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
	for _, cb := range callbacks {
		if err = cb.OnEnd(ctx, result); err != nil {
			return result, err
		}
	}
	err = send(final)
	return result, err
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
