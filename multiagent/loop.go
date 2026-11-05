package multiagent

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/kart-io/goagent/workflow"
)

// LoopConfig 描述循环：每轮依次执行 Steps，最后一步之后由 Until 判断是否结束。
type LoopConfig[T any] struct {
	Steps         []Step[T]
	Until         func(context.Context, T) (bool, error) // 必填；每轮结束后判断是否结束，必须是纯函数
	MaxIterations int                                    // 必填，> 0；防失控的硬上限，达到后返回 workflow.ErrMaxSteps
	Fallback      *Step[T]                               // 可选；Until 返回包装 ErrInvalidRoute 的错误时执行，之后结束
}

// Loop 构造 START → s1 → … → sn，sn 的分支目标为 s1 与 END（有 Fallback 时再加 fb，fb → END），
// MaxSteps 为 MaxIterations × n。Fallback 不增加步数：最后一轮判断无效时返回 workflow.ErrMaxSteps。
func Loop[T any](cfg workflow.Config, l LoopConfig[T]) (*workflow.Graph[T], error) {
	n := len(l.Steps)
	switch {
	case n == 0:
		return nil, errors.New("Loop 需要至少一个 Step")
	case l.Until == nil:
		return nil, errors.New("Loop 需要 Until")
	case l.MaxIterations <= 0:
		return nil, errors.New("Loop 的 MaxIterations 必须大于 0")
	case l.MaxIterations > math.MaxInt/n:
		return nil, errors.New("Loop 的 MaxIterations × 步数超出 int 范围")
	}
	g, err := newGraph[T](cfg, l.MaxIterations*n)
	if err != nil {
		return nil, err
	}
	if err = chain(g, l.Steps); err != nil {
		return nil, err
	}
	first := l.Steps[0].Name
	until := l.Until
	next := func(ctx context.Context, state T) (string, error) {
		done, err := until(ctx, state)
		if err != nil {
			return "", err
		}
		if done {
			return Finish, nil
		}
		return first, nil
	}
	fallback, err := addFallback(g, l.Fallback)
	if err != nil {
		return nil, err
	}
	route, targets, err := GuardRoute(next, []string{first}, fallback)
	if err != nil {
		return nil, err
	}
	if err = g.AddBranch(l.Steps[n-1].Name, route, targets...); err != nil {
		return nil, err
	}
	return g, nil
}

// addFallback 注册兜底 Step 并连到 END，返回其名称；fb 为 nil 时返回空名。
func addFallback[T any](g *workflow.Graph[T], fb *Step[T]) (string, error) {
	if fb == nil {
		return "", nil
	}
	if fb.Name == Finish {
		return "", fmt.Errorf("兜底 Step 不能命名为 %s", Finish)
	}
	if err := AddStep(g, *fb); err != nil {
		return "", fmt.Errorf("兜底 Step: %w", err)
	}
	if err := g.AddEdge(fb.Name, workflow.END); err != nil {
		return "", err
	}
	return fb.Name, nil
}
