package reflection

import (
	"context"
	"errors"

	"github.com/kart-io/goagent/core"
)

var ErrTaskExecutorRequired = errors.New("自反思 Agent 需要通过 WithTaskExecutor 配置真实任务执行器")

// TaskExecutor 复用已有 Agent 的调用契约，自反思组件不再模拟任务结果。
type TaskExecutor interface {
	Invoke(context.Context, *core.AgentInput) (*core.AgentOutput, error)
}

func WithTaskExecutor(executor TaskExecutor) ReflectionOption {
	return func(a *SelfReflectiveAgent) { a.taskExecutor = executor }
}

// runnable 显式绑定 Execute，避免嵌入 BaseAgent 后落入基类的空实现。
func (a *SelfReflectiveAgent) runnable() core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return core.NewRunnableFunc(a.Execute).WithConfig(a.GetConfig())
}

func (a *SelfReflectiveAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	return a.runnable().Invoke(ctx, input)
}

func (a *SelfReflectiveAgent) InvokeFast(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	return a.Invoke(ctx, input)
}

// Stream 仅返回最终结果；执行器的内部增量不会被伪装成自反思事件。
func (a *SelfReflectiveAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	return a.runnable().Stream(ctx, input)
}

func (a *SelfReflectiveAgent) StreamMode() core.StreamMode { return core.StreamModeFinal }

func (a *SelfReflectiveAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	return a.BaseRunnable.Batch(ctx, inputs, a.Invoke)
}

func (a *SelfReflectiveAgent) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return core.NewRunnablePipe[*core.AgentInput, *core.AgentOutput, any](a, next)
}

func (a *SelfReflectiveAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return a.runnable().WithCallbacks(callbacks...)
}

func (a *SelfReflectiveAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return a.runnable().WithConfig(config)
}
