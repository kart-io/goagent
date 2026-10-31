package builder

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/llm"
)

// WithOffload 把 o 装为带身份（o.Identity()）的 PreStep：超长的工具结果写入结果存储并换成引用，最后一次请求前还原原文。
// 读取工具即 o 本身，须由宿主与其他工具一起注册（WithTools）；持久运行中须经 Dispatcher，与其他工具一样记入日志。
//
// 结果作用域：宿主经 contextpolicy.WithResultScope 提供时由宿主回收；否则持久执行使用执行键（由宿主在运行不再恢复后
// 调用 o.Release），非持久执行使用临时作用域并在执行结束时释放。
func (b *AgentBuilder[C, S]) WithOffload(o *contextpolicy.Offload) *AgentBuilder[C, S] {
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
func (b *AgentBuilder[C, S]) validateOffloads() error {
	if len(b.config.Offloads) == 0 {
		return nil
	}
	if len(b.config.Offloads) > 1 {
		return fmt.Errorf("同一 Agent 只能装一个 Offload：读取工具名 %q 唯一", contextpolicy.ReadToolName)
	}
	if b.config.Offloads[0] == nil {
		return fmt.Errorf("WithOffload 需要 Offload")
	}
	readers := 0
	for _, tool := range b.tools {
		if tool != nil && tool.Name() == contextpolicy.ReadToolName {
			readers++
		}
	}
	if readers != 1 {
		return fmt.Errorf("WithOffload 需要把 Offload 作为读取工具 %q 注册一次（持久运行经 Dispatcher）", contextpolicy.ReadToolName)
	}
	return nil
}

// offloadScope 补齐结果作用域，返回的 release 在执行结束时释放本次生成的临时作用域；无需释放时为 nil。
func (a *ConfigurableAgent[C, S]) offloadScope(ctx context.Context) (context.Context, func(context.Context) error) {
	if len(a.config.Offloads) == 0 || contextpolicy.ResultScope(ctx) != "" {
		return ctx, nil
	}
	if key := durable.ExecutionKey(ctx); key != "" && durable.HasJournal(ctx) {
		return contextpolicy.WithResultScope(ctx, key), nil
	}
	ctx = contextpolicy.WithResultScope(ctx, uuid.NewString())
	return ctx, func(ctx context.Context) error {
		// 不受执行取消影响，回收在超时或取消后同样进行。
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var errs []error
		for _, o := range a.config.Offloads {
			errs = append(errs, o.Release(ctx))
		}
		return errors.Join(errs...)
	}
}

// trackOffloads 用本步的对话重建引用表：持久运行恢复时 PreStep 的结果被回放，Reduce 没有执行。
func (a *ConfigurableAgent[C, S]) trackOffloads(ctx context.Context, messages []llm.Message) {
	for _, o := range a.config.Offloads {
		o.Track(ctx, messages)
	}
}
