package builder

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// WithOffload installs o as a PreStep with an identity (o.Identity()):
// oversized tool results are written to the result store and replaced with
// references, and the originals are restored before the last request. The read
// tool is o itself and must be registered by the host along with the other
// tools (WithTools); in durable runs it must go through the Dispatcher and be
// recorded in the journal like any other tool.
//
// Result scope: when the host provides one via contextpolicy.WithResultScope,
// the host reclaims it; otherwise durable executions use the execution key (the
// host calls o.Release once the run will no longer resume), and non-durable
// executions use a temporary scope that is released when execution ends.
func (b *AgentBuilder[S]) WithOffload(o *contextpolicy.Offload) *AgentBuilder[S] {
	b.config.Offloads = append(b.config.Offloads, o)
	if o == nil {
		return b
	}
	return b.WithIdentifiedPreStep("offload/"+o.Identity(), func(ctx context.Context, step *Step) (PreStepResult, error) {
		messages, err := o.Reduce(ctx, step.Messages, step.Final)
		if err != nil {
			return PreStepResult{}, err
		}
		step.Messages = messages
		return PreStepResult{}, nil
	})
}

// validateOffloads 要求每个结果卸载的读取工具恰好注册一次。
func (b *AgentBuilder[S]) validateOffloads(toolset []interfaces.Tool) error {
	if len(b.config.Offloads) == 0 {
		return nil
	}
	if len(b.config.Offloads) > 1 {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "同一 Agent 只能装一个 Offload：读取工具名 %q 唯一", constants.ToolReadContextResult).WithComponent("builder").WithOperation("validate_offloads")
	}
	if b.config.Offloads[0] == nil {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "WithOffload 需要 Offload").WithComponent("builder").WithOperation("validate_offloads")
	}
	readers := 0
	for _, tool := range toolset {
		if tool != nil && tool.Name() == constants.ToolReadContextResult {
			readers++
		}
	}
	if readers != 1 {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "WithOffload 需要把 Offload 作为读取工具 %q 注册一次（持久运行经 Dispatcher）", constants.ToolReadContextResult).WithComponent("builder").WithOperation("validate_offloads")
	}
	return nil
}

// offloadScope 补齐结果作用域，返回的 release 在执行结束时释放本次生成的临时作用域；无需释放时为 nil。
func (a *ConfigurableAgent[S]) offloadScope(ctx context.Context) (context.Context, func(context.Context) error) {
	if len(a.config.Offloads) == 0 || contextpolicy.ResultScope(ctx) != "" {
		return ctx, nil
	}
	if key := durable.ExecutionKey(ctx); key != "" && durable.HasJournal(ctx) {
		return contextpolicy.WithResultScope(ctx, key), nil
	}
	ctx = contextpolicy.WithResultScope(ctx, uuid.NewString())
	return ctx, func(ctx context.Context) error {
		// 不受执行取消影响，回收在超时或取消后同样进行。
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.config.persistTimeout())
		defer cancel()
		var errs []error
		for _, o := range a.config.Offloads {
			errs = append(errs, o.Release(ctx))
		}
		return errors.Join(errs...)
	}
}

// trackOffloads 用本步的对话重建引用表：持久运行恢复时 PreStep 的结果被回放，Reduce 没有执行。
func (a *ConfigurableAgent[S]) trackOffloads(ctx context.Context, messages []llm.Message) {
	for _, o := range a.config.Offloads {
		o.Track(ctx, messages)
	}
}
