package core

import (
	"context"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// Chain 定义链式处理接口
//
// Chain 是一种串行执行的处理模式，适用于：
// - 多步骤的数据处理流程
// - 需要按顺序执行的分析任务
// - 每个步骤依赖前一步骤的输出
//
// Chain 现在直接实现 Runnable 接口，享受其所有能力：
// - Invoke: 单次执行
// - Stream: 流式执行
// - Batch: 批量执行
// - Pipe: 管道连接
// - WithCallbacks: 回调支持
type Chain interface {
	Runnable[*ChainInput, *ChainOutput]

	// Name 返回 Chain 的名称
	Name() string

	// Steps 返回包含的步骤数量
	Steps() int
}

// ChainInput Chain 输入
type ChainInput struct {
	// 输入数据
	Data interface{} `json:"data"` // 主要输入数据

	// 变量和上下文
	Vars map[string]interface{} `json:"vars,omitempty"` // 变量集合，用于在步骤间传递上下文

	// 执行控制
	Options ChainOptions `json:"options,omitempty"` // 执行选项
}

// ChainOutput Chain 输出
type ChainOutput struct {
	// 输出数据
	Data interface{} `json:"data"` // 最终输出数据

	// 执行信息
	StepsExecuted []StepExecution `json:"steps_executed"` // 执行的步骤详情
	TotalLatency  time.Duration   `json:"total_latency"`  // 总耗时
	Status        string          `json:"status"`         // 执行状态: "success", "failed", "partial"

	// 额外结果
	Metadata map[string]interface{} `json:"metadata,omitempty"` // 额外元数据
}

// ChainOptions Chain 执行选项
type ChainOptions struct {
	// 执行控制
	StopOnError bool          `json:"stop_on_error,omitempty"` // 出错时是否停止
	Timeout     time.Duration `json:"timeout,omitempty"`       // 超时时间
	Parallel    bool          `json:"parallel,omitempty"`      // 是否并行执行（如果可能）

	// 步骤控制
	SkipSteps []int `json:"skip_steps,omitempty"` // 跳过的步骤编号
	OnlySteps []int `json:"only_steps,omitempty"` // 仅执行的步骤编号

	// 额外选项
	Extra map[string]interface{} `json:"extra,omitempty"` // 额外选项
}

// StepExecution 步骤执行记录
type StepExecution struct {
	StepNumber  int           `json:"step_number"` // 步骤编号
	StepName    string        `json:"step_name"`   // 步骤名称
	Description string        `json:"description"` // 步骤描述
	Input       interface{}   `json:"input"`       // 输入
	Output      interface{}   `json:"output"`      // 输出
	Duration    time.Duration `json:"duration"`    // 耗时
	Success     bool          `json:"success"`     // 是否成功
	Error       string        `json:"error"`       // 错误信息
	Skipped     bool          `json:"skipped"`     // 是否跳过
}

// Step 定义 Chain 中的单个步骤接口
type Step interface {
	// Execute 执行步骤
	Execute(ctx context.Context, input interface{}) (interface{}, error)

	// Name 返回步骤名称
	Name() string

	// Description 返回步骤描述
	Description() string
}

// BaseChain 提供 Chain 的基础实现
//
// 实现了完整的 Runnable 接口，包括：
// - Invoke: 执行链式处理
// - Stream: 流式执行（将步骤结果作为流输出）
// - Batch: 批量执行多个输入
// - Pipe: 管道连接
// - WithCallbacks: 回调支持
type BaseChain struct {
	*BaseRunnable[*ChainInput, *ChainOutput]
	name  string
	steps []Step
}

// NewBaseChain 创建基础 Chain
func NewBaseChain(name string, steps []Step) *BaseChain {
	return &BaseChain{
		BaseRunnable: NewBaseRunnable[*ChainInput, *ChainOutput](),
		name:         name,
		steps:        steps,
	}
}

// Name 返回 Chain 名称
func (c *BaseChain) Name() string {
	return c.name
}

// Steps 返回步骤数量
func (c *BaseChain) Steps() int {
	return len(c.steps)
}

// Invoke 执行链式处理（实现 Runnable 接口）
func (c *BaseChain) Invoke(ctx context.Context, input *ChainInput) (*ChainOutput, error) {
	start := time.Now()

	// 触发 Chain 开始回调
	config := c.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnChainStart(ctx, c.name, input); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "callback OnChainStart failed").
				WithComponent("base_chain").
				WithOperation("invoke").
				WithContext("chain_name", c.name)
		}
	}

	output := &ChainOutput{
		StepsExecuted: make([]StepExecution, 0),
		Status:        "success",
		Metadata:      make(map[string]interface{}),
	}

	// 应用超时
	if input.Options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, input.Options.Timeout)
		defer cancel()
	}

	// 执行步骤
	currentData := input.Data
	for i, step := range c.steps {
		// 检查是否跳过
		if shouldSkipStep(i+1, input.Options) {
			output.StepsExecuted = append(output.StepsExecuted, StepExecution{
				StepNumber: i + 1,
				StepName:   step.Name(),
				Skipped:    true,
			})
			continue
		}

		// 执行步骤
		stepStart := time.Now()
		result, err := step.Execute(ctx, currentData)
		duration := time.Since(stepStart)

		execution := StepExecution{
			StepNumber:  i + 1,
			StepName:    step.Name(),
			Description: step.Description(),
			Input:       currentData,
			Output:      result,
			Duration:    duration,
			Success:     err == nil,
		}

		if err != nil {
			execution.Error = err.Error()
			output.StepsExecuted = append(output.StepsExecuted, execution)

			if input.Options.StopOnError {
				output.Status = "failed"
				output.TotalLatency = time.Since(start)

				// 触发 Chain 错误回调
				for _, cb := range config.Callbacks {
					_ = cb.OnChainError(ctx, c.name, err)
				}

				return output, err
			}

			output.Status = "partial"
		} else {
			currentData = result
			output.StepsExecuted = append(output.StepsExecuted, execution)
		}
	}

	output.Data = currentData
	output.TotalLatency = time.Since(start)

	// 触发 Chain 结束回调
	for _, cb := range config.Callbacks {
		_ = cb.OnChainEnd(ctx, c.name, output)
	}

	return output, nil
}

// Stream 流式执行（实现 Runnable 接口）
//
// 每个步骤执行完成后，立即发送一个流块
func (c *BaseChain) Stream(ctx context.Context, input *ChainInput) (<-chan StreamChunk[*ChainOutput], error) {
	outChan := make(chan StreamChunk[*ChainOutput])

	go func() {
		defer close(outChan)

		start := time.Now()
		config := c.GetConfig()

		// 触发 Chain 开始回调
		for _, cb := range config.Callbacks {
			if err := cb.OnChainStart(ctx, c.name, input); err != nil {
				outChan <- StreamChunk[*ChainOutput]{
					Error: agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "callback OnChainStart failed").
						WithComponent("base_chain").
						WithOperation("stream").
						WithContext("chain_name", c.name),
					Done: true,
				}
				return
			}
		}

		output := &ChainOutput{
			StepsExecuted: make([]StepExecution, 0),
			Status:        "success",
			Metadata:      make(map[string]interface{}),
		}

		// 应用超时
		if input.Options.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, input.Options.Timeout)
			defer cancel()
		}

		// 执行步骤，每个步骤完成后发送中间结果
		currentData := input.Data
		for i, step := range c.steps {
			// 检查是否跳过
			if shouldSkipStep(i+1, input.Options) {
				execution := StepExecution{
					StepNumber: i + 1,
					StepName:   step.Name(),
					Skipped:    true,
				}
				output.StepsExecuted = append(output.StepsExecuted, execution)

				// 发送中间状态
				outChan <- StreamChunk[*ChainOutput]{
					Data: &ChainOutput{
						Data:          currentData,
						StepsExecuted: append([]StepExecution{}, output.StepsExecuted...),
						TotalLatency:  time.Since(start),
						Status:        output.Status,
						Metadata:      output.Metadata,
					},
					Done: false,
				}
				continue
			}

			// 执行步骤
			stepStart := time.Now()
			result, err := step.Execute(ctx, currentData)
			duration := time.Since(stepStart)

			execution := StepExecution{
				StepNumber:  i + 1,
				StepName:    step.Name(),
				Description: step.Description(),
				Input:       currentData,
				Output:      result,
				Duration:    duration,
				Success:     err == nil,
			}

			if err != nil {
				execution.Error = err.Error()
				output.StepsExecuted = append(output.StepsExecuted, execution)

				if input.Options.StopOnError {
					output.Status = "failed"
					output.TotalLatency = time.Since(start)
					output.Data = currentData

					// 触发错误回调
					for _, cb := range config.Callbacks {
						_ = cb.OnChainError(ctx, c.name, err)
					}

					// 发送错误结果
					outChan <- StreamChunk[*ChainOutput]{
						Data:  output,
						Error: err,
						Done:  true,
					}
					return
				}

				output.Status = "partial"
			} else {
				currentData = result
				output.StepsExecuted = append(output.StepsExecuted, execution)
			}

			// 发送中间状态
			outChan <- StreamChunk[*ChainOutput]{
				Data: &ChainOutput{
					Data:          currentData,
					StepsExecuted: append([]StepExecution{}, output.StepsExecuted...),
					TotalLatency:  time.Since(start),
					Status:        output.Status,
					Metadata:      output.Metadata,
				},
				Done: false,
			}
		}

		// 最终结果
		output.Data = currentData
		output.TotalLatency = time.Since(start)

		// 触发结束回调
		for _, cb := range config.Callbacks {
			_ = cb.OnChainEnd(ctx, c.name, output)
		}

		// 发送最终结果
		outChan <- StreamChunk[*ChainOutput]{
			Data: output,
			Done: true,
		}
	}()

	return outChan, nil
}

// Batch 批量执行（实现 Runnable 接口）
func (c *BaseChain) Batch(ctx context.Context, inputs []*ChainInput) ([]*ChainOutput, error) {
	return c.BaseRunnable.Batch(ctx, inputs, c.Invoke)
}

// Pipe 连接到另一个 Runnable（实现 Runnable 接口）
func (c *BaseChain) Pipe(next Runnable[*ChainOutput, any]) Runnable[*ChainInput, any] {
	return NewRunnablePipe[*ChainInput, *ChainOutput, any](c, next)
}

// WithCallbacks 添加回调（重写返回类型）
func (c *BaseChain) WithCallbacks(callbacks ...Callback) Runnable[*ChainInput, *ChainOutput] {
	newChain := &BaseChain{
		BaseRunnable: c.BaseRunnable.WithCallbacks(callbacks...),
		name:         c.name,
		steps:        c.steps,
	}
	return newChain
}

// WithConfig 配置 Runnable（重写返回类型）
func (c *BaseChain) WithConfig(config RunnableConfig) Runnable[*ChainInput, *ChainOutput] {
	newChain := &BaseChain{
		BaseRunnable: c.BaseRunnable.WithConfig(config),
		name:         c.name,
		steps:        c.steps,
	}
	return newChain
}

// shouldSkipStep 检查是否应该跳过步骤
func shouldSkipStep(stepNum int, options ChainOptions) bool {
	// 如果指定了 OnlySteps，只执行这些步骤
	if len(options.OnlySteps) > 0 {
		for _, only := range options.OnlySteps {
			if only == stepNum {
				return false
			}
		}
		return true
	}

	// 检查 SkipSteps
	for _, skip := range options.SkipSteps {
		if skip == stepNum {
			return true
		}
	}

	return false
}

// DefaultChainOptions 返回默认的 Chain 选项
func DefaultChainOptions() ChainOptions {
	return ChainOptions{
		StopOnError: true,
		Timeout:     60 * time.Second,
		Parallel:    false,
		Extra:       make(map[string]interface{}),
	}
}
