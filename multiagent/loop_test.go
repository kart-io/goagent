package multiagent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/workflow"
)

// untilRounds 在最后一步执行满 n 次后结束。
func untilRounds(last string, n int) func(context.Context, record) (bool, error) {
	return func(_ context.Context, r record) (bool, error) { return count(r, last) >= n, nil }
}

func TestLoopRunsUntilDone(t *testing.T) {
	requireSameResult(t, func() (*workflow.Graph[record], error) {
		return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("write"), mark("review")}, Until: untilRounds("review", 2), MaxIterations: 3})
	}, []string{"write", "review", "write", "review"})
}

func TestLoopWithSingleStepAndParallel(t *testing.T) {
	requireSameResult(t, func() (*workflow.Graph[record], error) {
		return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{lookups(t)}, Until: untilRounds("kb", 2), MaxIterations: 2})
	}, []string{"kb", "order", "ship", "kb", "order", "ship"})
}

func TestConstructorsCopyCallerSteps(t *testing.T) {
	steps := []Step[record]{mark("write"), mark("review")}
	g, err := Loop(workflow.Config{}, LoopConfig[record]{Steps: steps, Until: untilRounds("review", 1), MaxIterations: 1})
	require.NoError(t, err)
	steps[0], steps[1] = mark("x"), mark("y")
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(context.Background(), record{})
	require.NoError(t, err)
	require.Equal(t, []string{"write", "review"}, out.Log)
}

func TestLoopIterationLimit(t *testing.T) {
	build := func(rounds int) func() (*workflow.Graph[record], error) {
		return func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("write"), mark("review")}, Until: untilRounds("review", rounds), MaxIterations: 2})
		}
	}
	requireSameResult(t, build(2), []string{"write", "review", "write", "review"})
	plain, plainErr, durableOut, durableErr := runBoth(t, build(3))
	require.ErrorIs(t, plainErr, workflow.ErrMaxSteps)
	require.ErrorIs(t, durableErr, workflow.ErrMaxSteps)
	require.Empty(t, plain.Log)
	require.Equal(t, 2, count(durableOut, "review"))
}

func loopWith(until func(context.Context, record) (bool, error), fallback *Step[record], iterations int) func() (*workflow.Graph[record], error) {
	return func() (*workflow.Graph[record], error) {
		return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("write"), mark("review")}, Until: until, MaxIterations: iterations, Fallback: fallback})
	}
}

func TestLoopFallback(t *testing.T) {
	invalidOnce := func(_ context.Context, r record) (bool, error) {
		return false, fmt.Errorf("%w：评审结论无法解析", ErrInvalidRoute)
	}
	fb := mark("human")
	plain, plainErr, durableOut, durableErr := runBoth(t, loopWith(invalidOnce, &fb, 2))
	require.NoError(t, plainErr)
	require.NoError(t, durableErr)
	require.Equal(t, []string{"write", "review", "human"}, plain.Log)
	require.Equal(t, plain.Log, durableOut.Log)

	_, plainErr, _, durableErr = runBoth(t, loopWith(invalidOnce, nil, 2))
	require.ErrorIs(t, plainErr, ErrInvalidRoute)
	require.ErrorIs(t, durableErr, ErrInvalidRoute)

	other := errors.New("评审服务不可用")
	_, plainErr, _, _ = runBoth(t, loopWith(func(context.Context, record) (bool, error) { return false, other }, &fb, 2))
	require.ErrorIs(t, plainErr, other)

	// 最后一轮判断无效：有兜底时返回 ErrMaxSteps，写作者不会多执行一次。
	_, plainErr, durableOut, durableErr = runBoth(t, loopWith(invalidOnce, &fb, 1))
	require.ErrorIs(t, plainErr, workflow.ErrMaxSteps)
	require.ErrorIs(t, durableErr, workflow.ErrMaxSteps)
	require.Equal(t, 1, count(durableOut, "write"))
	_, plainErr, _, durableErr = runBoth(t, loopWith(invalidOnce, nil, 1))
	require.ErrorIs(t, plainErr, ErrInvalidRoute)
	require.ErrorIs(t, durableErr, ErrInvalidRoute)
}

func TestLoopValidation(t *testing.T) {
	until := untilRounds("a", 1)
	bad := Step[record]{Name: "nil-run"}
	for name, build := range map[string]func() (*workflow.Graph[record], error){
		"Loop 无 Step": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Until: until, MaxIterations: 1})
		},
		"Loop 无 Until": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("a")}, MaxIterations: 1})
		},
		"Loop MaxIterations 0": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("a")}, Until: until})
		},
		"Loop 溢出": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("a"), mark("b")}, Until: until, MaxIterations: math.MaxInt/2 + 1})
		},
		"Loop MaxSteps": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{MaxSteps: 3}, LoopConfig[record]{Steps: []Step[record]{mark("a")}, Until: until, MaxIterations: 1})
		},
		"Loop 兜底重名": func() (*workflow.Graph[record], error) {
			fb := mark("a")
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("a")}, Until: until, MaxIterations: 1, Fallback: &fb})
		},
		"Loop 兜底 Run 为空": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("a")}, Until: until, MaxIterations: 1, Fallback: &bad})
		},
	} {
		g, err := build()
		require.Error(t, err, name)
		require.Nil(t, g, name)
	}
}
