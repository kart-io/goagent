package core

import (
	"context"

	agentErrors "github.com/kart-io/goagent/errors"
)

// Runnable 定义统一的可执行接口
//
// 借鉴 LangChain 的 Runnable 设计，提供统一的执行接口，支持:
// - 单个输入执行 (Invoke)
// - 流式执行 (Stream)
// - 批量执行 (Batch)
// - 管道连接 (Pipe)
// - 回调支持 (WithCallbacks)
//
// 泛型参数:
//   - I: 输入类型
//   - O: 输出类型
type Runnable[I, O any] interface {
	// Invoke 执行单个输入并返回输出
	Invoke(ctx context.Context, input I) (O, error)

	// Stream 流式执行，返回输出通道
	// 调用者负责从通道读取并处理结果
	Stream(ctx context.Context, input I) (<-chan StreamChunk[O], error)

	// Batch 批量执行多个输入
	Batch(ctx context.Context, inputs []I) ([]O, error)

	// Pipe 连接到另一个 Runnable，形成管道
	// 当前 Runnable 的输出成为下一个 Runnable 的输入
	Pipe(next Runnable[O, any]) Runnable[I, any]

	// WithCallbacks 添加回调处理器
	WithCallbacks(callbacks ...Callback) Runnable[I, O]

	// WithConfig 配置 Runnable
	WithConfig(config RunnableConfig) Runnable[I, O]
}

// StreamChunk 流式输出的数据块
type StreamChunk[T any] struct {
	Data  T     // 数据
	Error error // 错误（如果有）
	Done  bool  // 是否完成
}

// RunnableConfig Runnable 配置
type RunnableConfig struct {
	// Callbacks 回调处理器列表
	Callbacks []Callback

	// Tags 标签，用于标识和分类
	Tags []string

	// Metadata 元数据
	Metadata map[string]interface{}

	// MaxConcurrency 最大并发数（用于 Batch）
	MaxConcurrency int

	// RetryPolicy 重试策略
	RetryPolicy *RetryPolicy
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	MaxRetries int           // 最大重试次数
	Backoff    BackoffPolicy // 退避策略
}

// BackoffPolicy 退避策略
type BackoffPolicy interface {
	// NextDelay 计算下一次重试的延迟
	NextDelay(attempt int) int64 // 返回毫秒数
}

// BaseRunnable 提供 Runnable 的基础实现
//
// 实现了通用的功能如批处理、回调等
// 具体的 Invoke 和 Stream 需要由子类实现
type BaseRunnable[I, O any] struct {
	config RunnableConfig
}

// NewBaseRunnable 创建基础 Runnable
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

// Batch 默认的批处理实现
//
// 使用 goroutines 并发执行多个输入
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

	results := make(chan result, len(inputs))
	semaphore := make(chan struct{}, maxConcurrency)

	// 并发执行
	for i, input := range inputs {
		go func(index int, inp I) {
			semaphore <- struct{}{}        // 获取信号量
			defer func() { <-semaphore }() // 释放信号量

			output, err := invoker(ctx, inp)
			results <- result{
				index:  index,
				output: output,
				err:    err,
			}
		}(i, input)
	}

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

// WithCallbacks 添加回调
func (r *BaseRunnable[I, O]) WithCallbacks(callbacks ...Callback) *BaseRunnable[I, O] {
	newRunnable := *r
	newRunnable.config.Callbacks = append(newRunnable.config.Callbacks, callbacks...)
	return &newRunnable
}

// WithConfig 配置 Runnable
func (r *BaseRunnable[I, O]) WithConfig(config RunnableConfig) *BaseRunnable[I, O] {
	newRunnable := *r
	newRunnable.config = config
	return &newRunnable
}

// GetConfig 获取配置
func (r *BaseRunnable[I, O]) GetConfig() RunnableConfig {
	return r.config
}

// RunnablePipe 管道 Runnable，连接两个 Runnable
type RunnablePipe[I, M, O any] struct {
	first  Runnable[I, M]
	second Runnable[M, O]
	config RunnableConfig
}

// NewRunnablePipe 创建管道 Runnable
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

// Invoke 执行管道
func (p *RunnablePipe[I, M, O]) Invoke(ctx context.Context, input I) (O, error) {
	// 执行第一个 Runnable
	middle, err := p.first.Invoke(ctx, input)
	if err != nil {
		var zero O
		return zero, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "first runnable failed").
			WithComponent("runnable_pipe").
			WithOperation("invoke")
	}

	// 执行第二个 Runnable
	output, err := p.second.Invoke(ctx, middle)
	if err != nil {
		var zero O
		return zero, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "second runnable failed").
			WithComponent("runnable_pipe").
			WithOperation("invoke")
	}

	return output, nil
}

// Stream 流式执行管道
func (p *RunnablePipe[I, M, O]) Stream(ctx context.Context, input I) (<-chan StreamChunk[O], error) {
	outChan := make(chan StreamChunk[O])

	go func() {
		defer close(outChan)

		// 执行第一个 Runnable
		middle, err := p.first.Invoke(ctx, input)
		if err != nil {
			outChan <- StreamChunk[O]{Error: agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "first runnable failed").
				WithComponent("runnable_pipe").
				WithOperation("stream")}
			return
		}

		// 流式执行第二个 Runnable
		stream, err := p.second.Stream(ctx, middle)
		if err != nil {
			outChan <- StreamChunk[O]{Error: agentErrors.Wrap(err, agentErrors.CodeStreamRead, "second runnable stream failed").
				WithComponent("runnable_pipe").
				WithOperation("stream")}
			return
		}

		// 转发流
		for chunk := range stream {
			outChan <- chunk
		}
	}()

	return outChan, nil
}

// Batch 批量执行管道
func (p *RunnablePipe[I, M, O]) Batch(ctx context.Context, inputs []I) ([]O, error) {
	if len(inputs) == 0 {
		return []O{}, nil
	}

	// 执行第一个 Runnable 的批处理
	middles, err := p.first.Batch(ctx, inputs)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "first runnable batch failed").
			WithComponent("runnable_pipe").
			WithOperation("batch")
	}

	// 执行第二个 Runnable 的批处理
	outputs, err := p.second.Batch(ctx, middles)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "second runnable batch failed").
			WithComponent("runnable_pipe").
			WithOperation("batch")
	}

	return outputs, nil
}

// Pipe 连接到另一个 Runnable
func (p *RunnablePipe[I, M, O]) Pipe(next Runnable[O, any]) Runnable[I, any] {
	return NewRunnablePipe[I, O, any](p, next)
}

// WithCallbacks 添加回调
func (p *RunnablePipe[I, M, O]) WithCallbacks(callbacks ...Callback) Runnable[I, O] {
	newPipe := *p
	newPipe.config.Callbacks = append(newPipe.config.Callbacks, callbacks...)
	return &newPipe
}

// WithConfig 配置管道
func (p *RunnablePipe[I, M, O]) WithConfig(config RunnableConfig) Runnable[I, O] {
	newPipe := *p
	newPipe.config = config
	return &newPipe
}

// RunnableFunc 函数式 Runnable
//
// 将普通函数包装成 Runnable
type RunnableFunc[I, O any] struct {
	*BaseRunnable[I, O]
	fn func(context.Context, I) (O, error)
}

// NewRunnableFunc 创建函数式 Runnable
func NewRunnableFunc[I, O any](fn func(context.Context, I) (O, error)) *RunnableFunc[I, O] {
	return &RunnableFunc[I, O]{
		BaseRunnable: NewBaseRunnable[I, O](),
		fn:           fn,
	}
}

// Invoke 执行函数
func (f *RunnableFunc[I, O]) Invoke(ctx context.Context, input I) (O, error) {
	// 触发回调
	for _, cb := range f.GetConfig().Callbacks {
		if err := cb.OnStart(ctx, input); err != nil {
			var zero O
			return zero, err
		}
	}

	// 执行函数
	output, err := f.fn(ctx, input)

	// 触发回调
	for _, cb := range f.GetConfig().Callbacks {
		if err != nil {
			_ = cb.OnError(ctx, err)
		} else {
			_ = cb.OnEnd(ctx, output)
		}
	}

	return output, err
}

// Stream 流式执行（默认实现：包装成单个块）
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

// Batch 批量执行
func (f *RunnableFunc[I, O]) Batch(ctx context.Context, inputs []I) ([]O, error) {
	return f.BaseRunnable.Batch(ctx, inputs, f.Invoke)
}

// Pipe 连接到另一个 Runnable
func (f *RunnableFunc[I, O]) Pipe(next Runnable[O, any]) Runnable[I, any] {
	return NewRunnablePipe[I, O, any](f, next)
}

// WithCallbacks 添加回调（重写以返回正确的类型）
func (f *RunnableFunc[I, O]) WithCallbacks(callbacks ...Callback) Runnable[I, O] {
	newFunc := *f
	newFunc.BaseRunnable = f.BaseRunnable.WithCallbacks(callbacks...)
	return &newFunc
}

// WithConfig 配置 Runnable（重写以返回正确的类型）
func (f *RunnableFunc[I, O]) WithConfig(config RunnableConfig) Runnable[I, O] {
	newFunc := *f
	newFunc.BaseRunnable = f.BaseRunnable.WithConfig(config)
	return &newFunc
}

// RunnableSequence 顺序执行多个 Runnable
//
// 类似于 Unix 管道: r1 | r2 | r3
type RunnableSequence struct {
	runnables []Runnable[any, any]
	config    RunnableConfig
}

// NewRunnableSequence 创建顺序执行的 Runnable 序列
func NewRunnableSequence(runnables ...Runnable[any, any]) *RunnableSequence {
	return &RunnableSequence{
		runnables: runnables,
		config: RunnableConfig{
			Callbacks:      []Callback{},
			MaxConcurrency: 10,
		},
	}
}

// Invoke 顺序执行所有 Runnable
func (s *RunnableSequence) Invoke(ctx context.Context, input any) (any, error) {
	current := input

	for i, runnable := range s.runnables {
		output, err := runnable.Invoke(ctx, current)
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

// Stream 流式执行序列
func (s *RunnableSequence) Stream(ctx context.Context, input any) (<-chan StreamChunk[any], error) {
	outChan := make(chan StreamChunk[any])

	go func() {
		defer close(outChan)

		current := input
		for i, runnable := range s.runnables {
			// 如果不是最后一个，使用 Invoke
			if i < len(s.runnables)-1 {
				output, err := runnable.Invoke(ctx, current)
				if err != nil {
					outChan <- StreamChunk[any]{Error: agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "runnable failed").
						WithComponent("runnable_sequence").
						WithOperation("stream").
						WithContext("index", i)}
					return
				}
				current = output
			} else {
				// 最后一个使用 Stream
				stream, err := runnable.Stream(ctx, current)
				if err != nil {
					outChan <- StreamChunk[any]{Error: agentErrors.Wrap(err, agentErrors.CodeStreamRead, "runnable stream failed").
						WithComponent("runnable_sequence").
						WithOperation("stream").
						WithContext("index", i)}
					return
				}

				// 转发流
				for chunk := range stream {
					outChan <- chunk
				}
			}
		}
	}()

	return outChan, nil
}

// Batch 批量执行序列
func (s *RunnableSequence) Batch(ctx context.Context, inputs []any) ([]any, error) {
	current := inputs

	for i, runnable := range s.runnables {
		outputs, err := runnable.Batch(ctx, current)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "runnable batch failed").
				WithComponent("runnable_sequence").
				WithOperation("batch").
				WithContext("index", i)
		}
		current = outputs
	}

	return current, nil
}

// Pipe 连接到另一个 Runnable
func (s *RunnableSequence) Pipe(next Runnable[any, any]) Runnable[any, any] {
	newRunnables := append(s.runnables, next)
	return NewRunnableSequence(newRunnables...)
}

// WithCallbacks 添加回调
func (s *RunnableSequence) WithCallbacks(callbacks ...Callback) Runnable[any, any] {
	newSeq := *s
	newSeq.config.Callbacks = append(newSeq.config.Callbacks, callbacks...)
	return &newSeq
}

// WithConfig 配置序列
func (s *RunnableSequence) WithConfig(config RunnableConfig) Runnable[any, any] {
	newSeq := *s
	newSeq.config = config
	return &newSeq
}
