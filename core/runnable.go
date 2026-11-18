package core

import (
	"context"
	"maps"
	"slices"

	agentErrors "github.com/kart-io/goagent/errors"
)

// Runnable defines a unified executable interface.
//
// Modeled on LangChain's Runnable, it provides a unified execution interface
// that supports:
//   - single-input execution (Invoke)
//   - streaming execution (Stream)
//   - batch execution (Batch)
//   - pipeline composition (Pipe)
//   - callbacks (WithCallbacks)
//
// Type parameters:
//   - I: the input type
//   - O: the output type
type Runnable[I, O any] interface {
	// Invoke executes a single input and returns the output.
	Invoke(ctx context.Context, input I) (O, error)

	// Stream executes in streaming mode and returns an output channel. The
	// caller is responsible for reading and handling results from the channel.
	Stream(ctx context.Context, input I) (<-chan StreamChunk[O], error)

	// Batch executes multiple inputs as a batch.
	Batch(ctx context.Context, inputs []I) ([]O, error)

	// Pipe connects to another Runnable to form a pipeline. The output of this
	// Runnable becomes the input of the next one.
	Pipe(next Runnable[O, any]) Runnable[I, any]

	// WithCallbacks adds callback handlers.
	WithCallbacks(callbacks ...Callback) Runnable[I, O]

	// WithConfig configures the Runnable.
	WithConfig(config RunnableConfig) Runnable[I, O]
}

// StreamChunk is a chunk of streaming output.
type StreamChunk[T any] struct {
	Data  T            // data
	Error error        // error, if any
	Done  bool         // whether the stream is finished
	Event *StreamEvent // optional incremental event; Data is the final result only when Done is set and Error is nil
}

// RunnableConfig is the Runnable configuration.
type RunnableConfig struct {
	// Callbacks is the list of callback handlers.
	Callbacks []Callback `json:"-"`

	// Tags are labels used for identification and classification.
	Tags []string `json:"tags"`

	// Metadata is the metadata.
	Metadata map[string]interface{} `json:"metadata"`

	// MaxConcurrency is the maximum concurrency (used by Batch).
	MaxConcurrency int `json:"max_concurrency"`
}

// =============================================================================
// Panic Recovery for Plugin Isolation
// =============================================================================

// panicToError 将 panic 转换为错误（使用全局 PanicHandler），结果总能用 errors.As 识别出 *agentErrors.PanicError
//
// 这是插件隔离的关键：即使第三方插件发生 panic（如空指针解引用），
// 也不会导致整个 Agent 系统崩溃。
//
// 此函数现在使用可热插拔的 PanicHandler，支持自定义错误转换、
// 指标收集和日志记录。
//
// 参数:
//   - ctx: 执行上下文
//   - component: 发生 panic 的组件名称
//   - operation: 正在执行的操作
//   - r: panic 的值
//
// 返回:
//   - 表示 panic 的错误；必须在 defer 的 recover 处调用，调用栈才指向 panic 发生处
func panicToError(ctx context.Context, component, operation string, r interface{}) error {
	return GlobalPanicHandlerRegistry().HandlePanic(ctx, component, operation, r)
}

// safeInvoke 为 Runnable 提供 panic 捕获保护（使用可配置的 PanicHandler）
//
// 包装任何可能 panic 的函数调用，确保：
// 1. 捕获所有 panic（nil pointer、index out of bounds 等）
// 2. 使用可配置的 PanicHandler 转换错误
// 3. 自动记录指标和日志（如果配置）
// 4. 防止 panic 向上传播导致系统崩溃
//
// 此函数现在支持热插拔：
//   - 自定义错误转换策略
//   - 自定义指标收集（Prometheus、StatsD 等）
//   - 自定义日志记录
func safeInvoke[I, O any](fn func(context.Context, I) (O, error), ctx context.Context, input I) (output O, err error) {
	defer func() {
		if r := recover(); r != nil {
			var zero O
			output = zero
			// 使用全局 PanicHandlerRegistry 处理 panic
			err = panicToError(ctx, "runnable", "invoke", r)
		}
	}()

	return fn(ctx, input)
}

// BaseRunnable provides a base implementation of Runnable.
//
// It implements common functionality such as batching and callbacks. Concrete
// Invoke and Stream must be implemented by embedding types.
type BaseRunnable[I, O any] struct {
	config RunnableConfig
}

// NewBaseRunnable creates a base Runnable.
func NewBaseRunnable[I, O any]() *BaseRunnable[I, O] {
	return &BaseRunnable[I, O]{
		config: RunnableConfig{
			Callbacks:      []Callback{},
			Tags:           []string{},
			Metadata:       make(map[string]interface{}),
			MaxConcurrency: 10,
		},
	}
}

// Batch is the default batch implementation.
//
// It executes multiple inputs concurrently using goroutines.
func (r *BaseRunnable[I, O]) Batch(ctx context.Context, inputs []I, invoker func(context.Context, I) (O, error)) ([]O, error) {
	if len(inputs) == 0 {
		return []O{}, nil
	}

	// 限制并发数
	maxConcurrency := r.config.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = len(inputs)
	}

	type result struct {
		index  int
		output O
		err    error
	}

	if maxConcurrency > len(inputs) {
		maxConcurrency = len(inputs)
	}
	jobs := make(chan int)
	results := make(chan result, maxConcurrency)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for worker := 0; worker < maxConcurrency; worker++ {
		go func() {
			for index := range jobs {
				if workerCtx.Err() != nil {
					return
				}
				output, err := safeInvoke(invoker, workerCtx, inputs[index])
				select {
				case results <- result{index: index, output: output, err: err}:
				case <-workerCtx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range inputs {
			select {
			case jobs <- index:
			case <-workerCtx.Done():
				return
			}
		}
	}()

	// 收集结果
	outputs := make([]O, len(inputs))
	var firstError error

	for i := 0; i < len(inputs); i++ {
		select {
		case res := <-results:
			outputs[res.index] = res.output
			if res.err != nil && firstError == nil {
				firstError = res.err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return outputs, firstError
}

// WithCallbacks adds callbacks.
func (r *BaseRunnable[I, O]) WithCallbacks(callbacks ...Callback) *BaseRunnable[I, O] {
	newRunnable := *r
	newRunnable.config.Callbacks = append(append([]Callback(nil), r.config.Callbacks...), callbacks...)
	return &newRunnable
}

// WithConfig configures the Runnable.
func (r *BaseRunnable[I, O]) WithConfig(config RunnableConfig) *BaseRunnable[I, O] {
	newRunnable := *r
	newRunnable.config = cloneRunnableConfig(config)
	return &newRunnable
}

// cloneRunnableConfig 复制配置容器，避免克隆间互相修改；回调对象本身仍共享。
func cloneRunnableConfig(config RunnableConfig) RunnableConfig {
	config.Callbacks = slices.Clone(config.Callbacks)
	config.Tags = slices.Clone(config.Tags)
	config.Metadata = maps.Clone(config.Metadata)
	return config
}

// GetConfig returns the configuration.
func (r *BaseRunnable[I, O]) GetConfig() RunnableConfig {
	return r.config
}

// RunnablePipe is a pipeline Runnable that connects two Runnables.
type RunnablePipe[I, M, O any] struct {
	first  Runnable[I, M]
	second Runnable[M, O]
	config RunnableConfig
}

// NewRunnablePipe creates a pipeline Runnable.
func NewRunnablePipe[I, M, O any](first Runnable[I, M], second Runnable[M, O]) *RunnablePipe[I, M, O] {
	return &RunnablePipe[I, M, O]{
		first:  first,
		second: second,
		config: RunnableConfig{
			Callbacks:      []Callback{},
			MaxConcurrency: 10,
		},
	}
}

// Invoke executes the pipeline.
func (p *RunnablePipe[I, M, O]) Invoke(ctx context.Context, input I) (O, error) {
	runner := NewStreamRunnable(StreamModeFinal, func(ctx context.Context, input I, emit func(StreamChunk[O]) error) error {
		output, err := p.invoke(ctx, input)
		if err != nil {
			return err
		}
		return emit(StreamChunk[O]{Data: output, Done: true})
	})
	return runner.WithConfig(p.config).Invoke(ctx, input)
}

func (p *RunnablePipe[I, M, O]) invoke(ctx context.Context, input I) (O, error) {
	// 执行第一个 Runnable（带 panic 保护）
	middle, err := safeInvoke(p.first.Invoke, ctx, input)
	if err != nil {
		var zero O
		return zero, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "first runnable failed").
			WithComponent("runnable_pipe").
			WithOperation("invoke")
	}

	if err := ctx.Err(); err != nil {
		var zero O
		return zero, err
	}

	// 执行第二个 Runnable（带 panic 保护）
	output, err := safeInvoke(p.second.Invoke, ctx, middle)
	if err != nil {
		var zero O
		return zero, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "second runnable failed").
			WithComponent("runnable_pipe").
			WithOperation("invoke")
	}

	return output, nil
}

// Stream executes the pipeline in streaming mode.
func (p *RunnablePipe[I, M, O]) Stream(ctx context.Context, input I) (<-chan StreamChunk[O], error) {
	runner := NewStreamRunnable(StreamModeSteps, func(ctx context.Context, input I, emit func(StreamChunk[O]) error) error {
		middle, err := safeInvoke(p.first.Invoke, ctx, input)
		if err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "first runnable failed").WithComponent("runnable_pipe").WithOperation("stream")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		stream, err := safeInvoke(p.second.Stream, ctx, middle)
		if err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeStreamRead, "second runnable stream failed").WithComponent("runnable_pipe").WithOperation("stream")
		}
		return forwardRunnableStream(ctx, stream, emit)
	})
	return runner.WithConfig(p.config).Stream(ctx, input)
}

// Batch executes the pipeline as a batch.
func (p *RunnablePipe[I, M, O]) Batch(ctx context.Context, inputs []I) ([]O, error) {
	base := NewBaseRunnable[I, O]().WithConfig(p.config)
	return base.Batch(ctx, inputs, p.Invoke)
}

// Pipe connects to another Runnable.
func (p *RunnablePipe[I, M, O]) Pipe(next Runnable[O, any]) Runnable[I, any] {
	return NewRunnablePipe[I, O, any](p, next)
}

// WithCallbacks adds callbacks.
func (p *RunnablePipe[I, M, O]) WithCallbacks(callbacks ...Callback) Runnable[I, O] {
	newPipe := *p
	newPipe.config.Callbacks = append(slices.Clone(p.config.Callbacks), callbacks...)
	return &newPipe
}

// WithConfig configures the pipeline.
func (p *RunnablePipe[I, M, O]) WithConfig(config RunnableConfig) Runnable[I, O] {
	newPipe := *p
	newPipe.config = cloneRunnableConfig(config)
	return &newPipe
}

// RunnableFunc is a function-based Runnable.
//
// It wraps an ordinary function as a Runnable.
type RunnableFunc[I, O any] struct {
	*BaseRunnable[I, O]
	fn func(context.Context, I) (O, error)
}

// NewRunnableFunc creates a function-based Runnable.
func NewRunnableFunc[I, O any](fn func(context.Context, I) (O, error)) *RunnableFunc[I, O] {
	return &RunnableFunc[I, O]{
		BaseRunnable: NewBaseRunnable[I, O](),
		fn:           fn,
	}
}

// Invoke executes the function.
func (f *RunnableFunc[I, O]) Invoke(ctx context.Context, input I) (O, error) {
	callbacks := f.GetConfig().Callbacks
	if _, err := NotifyCallbacks(ctx, "runnable_func", "on_start", callbacks, func(cb Callback) error {
		return cb.OnStart(ctx, input)
	}); err != nil {
		var zero O
		return zero, err
	}

	// 执行函数（带 panic 保护 - 这是保护第三方插件的关键）
	output, err := safeInvoke(f.fn, ctx, input)

	// 完成回调可以拒绝结果；错误回调不能覆盖原始执行错误。
	operation, notify := "on_end", func(cb Callback) error { return cb.OnEnd(ctx, output) }
	if err != nil {
		failure := err
		operation, notify = "on_error", func(cb Callback) error { _ = cb.OnError(ctx, failure); return nil }
	}
	if _, callbackErr := NotifyCallbacks(ctx, "runnable_func", operation, callbacks, notify); callbackErr != nil {
		return output, callbackErr
	}
	return output, err
}

// Stream executes in streaming mode (default implementation: wraps the result
// in a single chunk).
func (f *RunnableFunc[I, O]) Stream(ctx context.Context, input I) (<-chan StreamChunk[O], error) {
	outChan := make(chan StreamChunk[O], 1)

	go func() {
		defer close(outChan)

		output, err := f.Invoke(ctx, input)
		outChan <- StreamChunk[O]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// Batch executes as a batch.
func (f *RunnableFunc[I, O]) Batch(ctx context.Context, inputs []I) ([]O, error) {
	return f.BaseRunnable.Batch(ctx, inputs, f.Invoke)
}

// Pipe connects to another Runnable.
func (f *RunnableFunc[I, O]) Pipe(next Runnable[O, any]) Runnable[I, any] {
	return NewRunnablePipe[I, O, any](f, next)
}

// WithCallbacks adds callbacks (overridden to return the correct type).
func (f *RunnableFunc[I, O]) WithCallbacks(callbacks ...Callback) Runnable[I, O] {
	newFunc := *f
	newFunc.BaseRunnable = f.BaseRunnable.WithCallbacks(callbacks...)
	return &newFunc
}

// WithConfig configures the Runnable (overridden to return the correct type).
func (f *RunnableFunc[I, O]) WithConfig(config RunnableConfig) Runnable[I, O] {
	newFunc := *f
	newFunc.BaseRunnable = f.BaseRunnable.WithConfig(config)
	return &newFunc
}

// RunnableSequence executes multiple Runnables in sequence.
//
// It is similar to a Unix pipeline: r1 | r2 | r3
type RunnableSequence struct {
	runnables []Runnable[any, any]
	config    RunnableConfig
}

// NewRunnableSequence creates a sequence of Runnables executed in order.
func NewRunnableSequence(runnables ...Runnable[any, any]) *RunnableSequence {
	return &RunnableSequence{
		runnables: slices.Clone(runnables),
		config: RunnableConfig{
			Callbacks:      []Callback{},
			MaxConcurrency: 10,
		},
	}
}

// Invoke executes all Runnables in order.
func (s *RunnableSequence) Invoke(ctx context.Context, input any) (any, error) {
	runner := NewStreamRunnable(StreamModeFinal, func(ctx context.Context, input any, emit func(StreamChunk[any]) error) error {
		output, err := s.invoke(ctx, input)
		if err != nil {
			return err
		}
		return emit(StreamChunk[any]{Data: output, Done: true})
	})
	return runner.WithConfig(s.config).Invoke(ctx, input)
}

func (s *RunnableSequence) invoke(ctx context.Context, input any) (any, error) {
	current := input

	for i, runnable := range s.runnables {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// 带 panic 保护执行每个 Runnable
		output, err := safeInvoke(runnable.Invoke, ctx, current)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "runnable failed").
				WithComponent("runnable_sequence").
				WithOperation("invoke").
				WithContext("index", i)
		}
		current = output
	}

	return current, nil
}

// Stream executes the sequence in streaming mode.
func (s *RunnableSequence) Stream(ctx context.Context, input any) (<-chan StreamChunk[any], error) {
	runner := NewStreamRunnable(StreamModeSteps, func(ctx context.Context, input any, emit func(StreamChunk[any]) error) error {
		current := input
		for i, runnable := range s.runnables {
			if err := ctx.Err(); err != nil {
				return err
			}
			if i == len(s.runnables)-1 {
				stream, err := safeInvoke(runnable.Stream, ctx, current)
				if err != nil {
					return agentErrors.Wrap(err, agentErrors.CodeStreamRead, "runnable stream failed").WithComponent("runnable_sequence").WithOperation("stream").WithContext("index", i)
				}
				return forwardRunnableStream(ctx, stream, emit)
			}
			output, err := safeInvoke(runnable.Invoke, ctx, current)
			if err != nil {
				return agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "runnable failed").WithComponent("runnable_sequence").WithOperation("stream").WithContext("index", i)
			}
			current = output
		}
		return emit(StreamChunk[any]{Data: current, Done: true})
	})
	return runner.WithConfig(s.config).Stream(ctx, input)
}

// Batch executes the sequence as a batch.
func (s *RunnableSequence) Batch(ctx context.Context, inputs []any) ([]any, error) {
	base := NewBaseRunnable[any, any]().WithConfig(s.config)
	return base.Batch(ctx, inputs, s.Invoke)
}

// Pipe connects to another Runnable.
func (s *RunnableSequence) Pipe(next Runnable[any, any]) Runnable[any, any] {
	newRunnables := append(slices.Clone(s.runnables), next)
	clone := *s
	clone.runnables = newRunnables
	clone.config = cloneRunnableConfig(s.config)
	return &clone
}

// WithCallbacks adds callbacks.
func (s *RunnableSequence) WithCallbacks(callbacks ...Callback) Runnable[any, any] {
	newSeq := *s
	newSeq.config.Callbacks = append(slices.Clone(s.config.Callbacks), callbacks...)
	return &newSeq
}

// WithConfig configures the sequence.
func (s *RunnableSequence) WithConfig(config RunnableConfig) Runnable[any, any] {
	newSeq := *s
	newSeq.config = cloneRunnableConfig(config)
	return &newSeq
}
