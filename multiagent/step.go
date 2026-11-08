package multiagent

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/kart-io/goagent/builder"
	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

// Step 是图中的一步。Run 必须非 nil；ParallelStep 返回的 Step 由私有字段描述并行分支，忽略 Run。
type Step[T any] struct {
	Name     string
	Run      core.Runnable[T, T]
	parallel *parallelSpec[T]
}

// parallelSpec 是 ParallelStep 复制保存的分支配置，由 AddStep 注册为并行节点。
type parallelSpec[T any] struct {
	steps []Step[T]
	clone func(T) T
	merge func(context.Context, T, []T) (T, error)
}

// AgentFactory 为每次节点执行新建一个 Agent 会话，避免循环与重复调用共享可变记忆。
// 并行分支与并发运行会同时调用它，实现必须并发安全。
type AgentFactory func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error)

// AgentStep 把 Agent 接成一步：in 从状态取任务输入，out 把输出写回状态。
// in 返回 error 时在发出模型请求前失败，可用于拒绝不完整的状态；
// out 返回 error 时该步失败，可用于把“合规检查不通过”变成节点错误。
// in 与 out 必须是纯函数：in 的结果进入模型请求意图，out 会随节点重入再次执行。
func AgentStep[T any](name string, agent AgentFactory,
	in func(T) (any, error), out func(T, *builder.AgentOutput) (T, error),
) (Step[T], error) {
	if !validName(name) {
		return Step[T]{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "Step 名称 %q 无效", name).WithComponent("multiagent").WithOperation("agent_step")
	}
	if agent == nil || in == nil || out == nil {
		return Step[T]{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "AgentStep %q 需要 agent、in 与 out", name).WithComponent("multiagent").WithOperation("agent_step")
	}
	run, err := workflow.Adapt(sessionRunnable(agent), in, out)
	if err != nil {
		return Step[T]{}, err
	}
	return Step[T]{Name: name, Run: run}, nil
}

// sessionRunnable 把每次返回新会话的 Factory 包装成固定的 Runnable，供 workflow.Adapt 使用。
// 缺少 Done、重复 Done 与取消后的排空由 Adapt 内部的节点消费逻辑处理。
func sessionRunnable(factory AgentFactory) *core.StreamRunnable[any, *builder.AgentOutput] {
	return core.NewStreamRunnable(core.StreamModeSteps, func(ctx context.Context, input any, emit func(core.StreamChunk[*builder.AgentOutput]) error) (err error) {
		owned, err := factory(ctx)
		if owned.Close != nil {
			defer func() {
				cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shared.DefaultSessionCleanupTimeout)
				defer cancel()
				err = errors.Join(err, owned.Close(cleanCtx))
			}()
		}
		if err != nil {
			return err
		}
		if owned.Close == nil || isNil(owned.Runnable) {
			return agentErrors.NewInvalidConfigError("multiagent", "session_factory", "会话需要 Runnable 与 Close")
		}
		chunks, err := owned.Runnable.Stream(ctx, input)
		if err != nil {
			return err
		}
		if chunks == nil {
			return agentErrors.New(agentErrors.CodeInvalidOutput, "会话返回了 nil 流").WithComponent("multiagent").WithOperation("stream_session")
		}
		var failure error
		// 出错后仍读到流关闭，保证 Close 在会话结束之后调用。
		for chunk := range chunks {
			switch {
			case failure != nil:
			case chunk.Error != nil:
				failure = chunk.Error
			case chunk.Done && chunk.Data == nil:
				failure = agentErrors.New(agentErrors.CodeInvalidOutput, "会话没有返回输出").WithComponent("multiagent").WithOperation("stream_session")
			default:
				failure = emit(chunk)
			}
		}
		return failure
	})
}

func validName(name string) bool {
	return name != "" && strings.TrimSpace(name) == name && name != shared.WorkflowStart && name != shared.WorkflowEnd
}

// isNil 把 typed nil 也视为 nil，与 tools 校验 Runnable 的方式一致。
func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}

func checkStep[T any](s Step[T]) error {
	if !validName(s.Name) {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "Step 名称 %q 无效", s.Name).WithComponent("multiagent").WithOperation("check_step")
	}
	if isNil(s.Run) {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "Step %q 缺少 Run", s.Name).WithComponent("multiagent").WithOperation("check_step")
	}
	return nil
}
