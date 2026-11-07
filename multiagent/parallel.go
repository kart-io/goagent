package multiagent

import (
	"context"
	"fmt"
	"slices"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
)

// ParallelConfig 描述一组并行分支。Clone 必须深拷贝分支会修改的切片、映射与指针字段；
// Merge 按分支登记顺序收到各分支输出，必须是纯函数（恢复后可能重复执行）。
type ParallelConfig[T any] struct {
	Steps []Step[T]
	Clone func(T) T
	Merge func(context.Context, T, []T) (T, error)
}

// ParallelStep 把一组并行分支包装成名为 name 的一步；整组计 1 步。分支名只需在本节点内唯一，
// 分支不得再是 ParallelStep。返回值的 Run 直接调用即返回错误，构造器与 AddStep 忽略它。
func ParallelStep[T any](name string, p ParallelConfig[T]) (Step[T], error) {
	if !validName(name) {
		return Step[T]{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "Step 名称 %q 无效", name).WithComponent("multiagent").WithOperation("parallel_step")
	}
	if p.Clone == nil || p.Merge == nil || len(p.Steps) == 0 {
		return Step[T]{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "ParallelStep %q 需要至少一个分支以及 Clone 与 Merge", name).WithComponent("multiagent").WithOperation("parallel_step")
	}
	seen := map[string]bool{}
	for _, s := range p.Steps {
		if err := checkStep(s); err != nil {
			return Step[T]{}, fmt.Errorf("ParallelStep %q: %w", name, err)
		}
		if s.parallel != nil {
			return Step[T]{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "ParallelStep %q 的分支 %q 不能是 ParallelStep", name, s.Name).WithComponent("multiagent").WithOperation("parallel_step")
		}
		if seen[s.Name] {
			return Step[T]{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "ParallelStep %q 的分支名 %q 重复", name, s.Name).WithComponent("multiagent").WithOperation("parallel_step")
		}
		seen[s.Name] = true
	}
	run := core.NewRunnableFunc(func(_ context.Context, in T) (T, error) { return in, agentErrors.ErrParallelRegistration })
	return Step[T]{Name: name, Run: run, parallel: &parallelSpec[T]{steps: slices.Clone(p.Steps), clone: p.Clone, merge: p.Merge}}, nil
}

// AddStep 把 Step 加入宿主手写的图：ParallelStep 以 AddParallel 注册，其余以 AddRunnable 注册。
// 三个构造器内部也用它注册节点。
func AddStep[T any](g *workflow.Graph[T], s Step[T]) error {
	if g == nil {
		return agentErrors.NewInvalidConfigError("multiagent", "graph", "AddStep 需要非 nil 的图")
	}
	if err := checkStep(s); err != nil {
		return err
	}
	if s.parallel == nil {
		return g.AddRunnable(s.Name, s.Run)
	}
	branches := make([]workflow.Branch[T], len(s.parallel.steps))
	for i, b := range s.parallel.steps {
		run := b.Run
		branches[i] = workflow.Branch[T]{Name: b.Name, Run: func(ctx context.Context, in T) (T, error) { return run.Invoke(ctx, in) }}
	}
	return g.AddParallel(s.Name, workflow.ParallelConfig[T]{Branches: branches, Clone: s.parallel.clone, Merge: s.parallel.merge})
}
