package multiagent

import (
	"fmt"

	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
)

// newGraph 用构造器算出的步数上限创建图；宿主设置的 MaxSteps 会让上限与拓扑不一致，因此拒绝。
func newGraph[T any](cfg workflow.Config, maxSteps int) (*workflow.Graph[T], error) {
	if cfg.MaxSteps != 0 {
		return nil, agentErrors.NewInvalidConfigError("multiagent", "max_steps", "cfg.MaxSteps 由构造器计算，必须为 0")
	}
	cfg.MaxSteps = maxSteps
	return workflow.NewGraph[T](cfg), nil
}

// chain 注册 steps 并按顺序连边：START → s1 → … → sn，不连 sn 的出边。
func chain[T any](g *workflow.Graph[T], steps []Step[T]) error {
	prev := constants.WorkflowStart
	for i, s := range steps {
		if err := AddStep(g, s); err != nil {
			return fmt.Errorf("第 %d 步: %w", i+1, err)
		}
		if err := g.AddEdge(prev, s.Name); err != nil {
			return err
		}
		prev = s.Name
	}
	return nil
}

// Sequential 构造 START → s1 → … → sn → END，MaxSteps 为 n。
func Sequential[T any](cfg workflow.Config, steps ...Step[T]) (*workflow.Graph[T], error) {
	if len(steps) == 0 {
		return nil, agentErrors.NewInvalidConfigError("multiagent", "steps", "Sequential 需要至少一个 Step")
	}
	g, err := newGraph[T](cfg, len(steps))
	if err != nil {
		return nil, err
	}
	if err = chain(g, steps); err != nil {
		return nil, err
	}
	if err = g.AddEdge(steps[len(steps)-1].Name, constants.WorkflowEnd); err != nil {
		return nil, err
	}
	return g, nil
}
