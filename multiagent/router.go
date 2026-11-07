package multiagent

import (
	"context"
	"fmt"
	"math"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
)

// RouterConfig 描述 Supervisor 分流：Supervisor 执行后由 Next 选择成员或 Finish，成员执行后回到 Supervisor。
type RouterConfig[T any] struct {
	Supervisor Step[T]
	Members    []Step[T]
	Next       func(context.Context, T) (string, error) // 返回成员名或 Finish，必须是纯函数
	MaxRounds  int                                      // 必填，> 0：成员最多执行的次数
	Fallback   *Step[T]                                 // 可选；路由无效时执行，之后结束
}

// RouterSupervisor 构造 START → sup，sup 的分支目标为全部成员与 END（有 Fallback 时再加 fb，fb → END），
// 每个成员回 sup，MaxSteps 为 2 × MaxRounds + 1。Fallback 占用一次成员轮数。
func RouterSupervisor[T any](cfg workflow.Config, r RouterConfig[T]) (*workflow.Graph[T], error) {
	switch {
	case r.MaxRounds <= 0:
		return nil, agentErrors.NewInvalidConfigError("multiagent", "max_rounds", "RouterSupervisor 的 MaxRounds 必须大于 0")
	case r.MaxRounds > (math.MaxInt-1)/2:
		return nil, agentErrors.NewInvalidConfigError("multiagent", "max_rounds", "RouterSupervisor 的 2 × MaxRounds + 1 超出 int 范围")
	case r.Next == nil:
		return nil, agentErrors.NewInvalidConfigError("multiagent", "next", "RouterSupervisor 需要 Next")
	case len(r.Members) == 0:
		return nil, agentErrors.NewInvalidConfigError("multiagent", "members", "RouterSupervisor 需要至少一个成员")
	}
	names := make([]string, len(r.Members))
	for i, m := range r.Members {
		if m.Name == Finish || m.Name == r.Supervisor.Name {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "成员名 %q 不能是 %s 或 Supervisor 名", m.Name, Finish).WithComponent("multiagent").WithOperation("router_supervisor")
		}
		names[i] = m.Name
	}
	if r.Fallback != nil && r.Fallback.Name == r.Supervisor.Name {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "兜底名 %q 不能与 Supervisor 同名", r.Fallback.Name).WithComponent("multiagent").WithOperation("router_supervisor")
	}
	g, err := newGraph[T](cfg, 2*r.MaxRounds+1)
	if err != nil {
		return nil, err
	}
	if err = chain(g, []Step[T]{r.Supervisor}); err != nil {
		return nil, fmt.Errorf("注册 Supervisor: %w", err)
	}
	for _, m := range r.Members {
		if err = AddStep(g, m); err != nil {
			return nil, fmt.Errorf("成员: %w", err)
		}
		if err = g.AddEdge(m.Name, r.Supervisor.Name); err != nil {
			return nil, err
		}
	}
	fallback, err := addFallback(g, r.Fallback)
	if err != nil {
		return nil, err
	}
	route, targets, err := GuardRoute(r.Next, names, fallback)
	if err != nil {
		return nil, err
	}
	if err = g.AddBranch(r.Supervisor.Name, route, targets...); err != nil {
		return nil, err
	}
	return g, nil
}
