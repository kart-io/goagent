package multiagent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
)

// routeBy 让第 k 次 Supervisor 之后选择 seq[k]，用完后 Finish；只依据状态，是纯函数。
func routeBy(seq ...string) func(context.Context, record) (string, error) {
	return func(_ context.Context, r record) (string, error) {
		k := count(r, "sup") - 1
		if k < len(seq) {
			return seq[k], nil
		}
		return constants.MultiagentFinish, nil
	}
}

func TestRouterSupervisorRoutesMembers(t *testing.T) {
	requireSameResult(t, func() (*workflow.Graph[record], error) {
		return RouterSupervisor(workflow.Config{}, RouterConfig[record]{
			Supervisor: mark("sup"), Members: []Step[record]{mark("refund"), lookups(t)},
			Next: routeBy("refund", "lookup"), MaxRounds: 2,
		})
	}, []string{"sup", "refund", "sup", "kb", "order", "ship", "sup"})
}

func TestRouterRoundLimit(t *testing.T) {
	build := func(seq ...string) func() (*workflow.Graph[record], error) {
		return func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: routeBy(seq...), MaxRounds: 2})
		}
	}
	requireSameResult(t, build("a", "a"), []string{"sup", "a", "sup", "a", "sup"})
	_, plainErr, durableOut, durableErr := runBoth(t, build("a", "a", "a"))
	require.ErrorIs(t, plainErr, agentErrors.ErrWorkflowMaxSteps)
	require.ErrorIs(t, durableErr, agentErrors.ErrWorkflowMaxSteps)
	require.Equal(t, 2, count(durableOut, "a"), "第 MaxRounds 次成员后 Supervisor 再执行一次，选成员即超限")
	require.Equal(t, 3, count(durableOut, "sup"))
}

func routerWith(next func(context.Context, record) (string, error), fallback *Step[record], rounds int) func() (*workflow.Graph[record], error) {
	return func() (*workflow.Graph[record], error) {
		return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: rounds, Fallback: fallback})
	}
}

func TestRouterFallback(t *testing.T) {
	invalid := fmt.Errorf("%w：无法识别", agentErrors.ErrInvalidRoute)
	for name, next := range map[string]func(context.Context, record) (string, error){
		"ErrInvalidRoute": fixed("", invalid),
		"未知成员":            fixed("x", nil),
		"END 字面值":         fixed(constants.WorkflowEnd, nil),
		"Supervisor 名":    fixed("sup", nil),
		"兜底名":             fixed("human", nil),
	} {
		fb := mark("human")
		plain, plainErr, durableOut, durableErr := runBoth(t, routerWith(next, &fb, 2))
		require.NoError(t, plainErr, name)
		require.NoError(t, durableErr, name)
		require.Equal(t, []string{"sup", "human"}, plain.Log, name)
		require.Equal(t, plain.Log, durableOut.Log, name)
		if name == "兜底名" {
			continue // 没有兜底时兜底名不存在，等同未知成员
		}
		_, plainErr, _, durableErr = runBoth(t, routerWith(next, nil, 2))
		require.ErrorIs(t, plainErr, agentErrors.ErrInvalidRoute, name)
		require.ErrorIs(t, durableErr, agentErrors.ErrInvalidRoute, name)
	}
	fb := mark("human")
	other := errors.New("分流服务不可用")
	_, plainErr, _, durableErr := runBoth(t, routerWith(fixed("", other), &fb, 2))
	require.ErrorIs(t, plainErr, other, "其他错误即使有兜底也失败")
	require.ErrorIs(t, durableErr, other)
}

func TestRouterFallbackOnLastRoundHitsStepLimit(t *testing.T) {
	// 第 1 次选 a，第 2 次 Supervisor 输出无效：MaxRounds 为 1 时兜底没有步数可用。
	next := func(_ context.Context, r record) (string, error) {
		if count(r, "sup") == 1 {
			return "a", nil
		}
		return "", agentErrors.ErrInvalidRoute
	}
	fb := mark("human")
	_, plainErr, durableOut, durableErr := runBoth(t, routerWith(next, &fb, 1))
	require.ErrorIs(t, plainErr, agentErrors.ErrWorkflowMaxSteps)
	require.ErrorIs(t, durableErr, agentErrors.ErrWorkflowMaxSteps)
	require.NotContains(t, durableOut.Log, "human")
	_, plainErr, _, durableErr = runBoth(t, routerWith(next, nil, 1))
	require.ErrorIs(t, plainErr, agentErrors.ErrInvalidRoute)
	require.ErrorIs(t, durableErr, agentErrors.ErrInvalidRoute)
}

func TestApprovalNodesAcceptOuterNames(t *testing.T) {
	fb := mark("human")
	g, err := RouterSupervisor(workflow.Config{}, RouterConfig[record]{
		Supervisor: mark("sup"), Members: []Step[record]{lookups(t)},
		Next: fixed("x", nil), MaxRounds: 1, Fallback: &fb,
	})
	require.NoError(t, err)
	e := newEnv(t, "approval")
	_, err = g.CompileDurable(context.Background(), workflow.DurableConfig{Store: e.store, Version: "v1", ApprovalNodes: []string{"kb"}})
	require.ErrorContains(t, err, "unknown or duplicate approval node", "分支名不是图节点")

	d := compile(t, e, g, "v1", "lookup", "human")
	snap, err := d.Run(context.Background(), e.id, record{}, "")
	require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)
	require.Equal(t, "human", snap.Node, "兜底可列入审批")
	snap, err = d.Run(context.Background(), e.id, record{}, snap.Key)
	require.NoError(t, err)
	require.Equal(t, []string{"sup", "human"}, snap.State.Log)
}

func TestRouterParallelMemberApprovedByOuterName(t *testing.T) {
	g, err := RouterSupervisor(workflow.Config{}, RouterConfig[record]{
		Supervisor: mark("sup"), Members: []Step[record]{lookups(t)},
		Next: routeBy("lookup"), MaxRounds: 1,
	})
	require.NoError(t, err)
	e := newEnv(t, "parallel-approval")
	d := compile(t, e, g, "v1", "lookup")
	snap, err := d.Run(context.Background(), e.id, record{}, "")
	require.ErrorIs(t, err, agentErrors.ErrApprovalRequired)
	require.Equal(t, "lookup", snap.Node)
	snap, err = d.Run(context.Background(), e.id, record{}, snap.Key)
	require.NoError(t, err)
	require.True(t, slices.Equal([]string{"sup", "kb", "order", "ship", "sup"}, snap.State.Log))
	require.Equal(t, 3, snap.Steps, "整组并行计 1 步")
}

func TestRouterValidation(t *testing.T) {
	next := routeBy()
	for name, build := range map[string]func() (*workflow.Graph[record], error){
		"Router MaxRounds 0": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next})
		},
		"Router 溢出": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: math.MaxInt/2 + 1})
		},
		"Router 无 Next": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, MaxRounds: 1})
		},
		"Router 无成员": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Next: next, MaxRounds: 1})
		},
		"Router 成员 FINISH": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark(constants.MultiagentFinish)}, Next: next, MaxRounds: 1})
		},
		"Router 成员与 Supervisor 同名": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("sup")}, Next: next, MaxRounds: 1})
		},
		"Router 成员重名": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a"), mark("a")}, Next: next, MaxRounds: 1})
		},
		"Router 兜底 FINISH": func() (*workflow.Graph[record], error) {
			fb := mark(constants.MultiagentFinish)
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: 1, Fallback: &fb})
		},
		"Router 兜底与 Supervisor 同名": func() (*workflow.Graph[record], error) {
			fb := mark("sup")
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: 1, Fallback: &fb})
		},
		"Router 兜底与成员同名": func() (*workflow.Graph[record], error) {
			fb := mark("a")
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: 1, Fallback: &fb})
		},
		"Router MaxSteps": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{MaxSteps: 9}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: 1})
		},
	} {
		g, err := build()
		require.Error(t, err, name)
		require.Nil(t, g, name)
	}
}
