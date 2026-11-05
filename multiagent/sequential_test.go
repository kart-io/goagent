package multiagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/workflow"
)

func count(r record, name string) int {
	n := 0
	for _, s := range r.Log {
		if s == name {
			n++
		}
	}
	return n
}

// runBoth 用普通与持久两种方式各执行一次图，返回两次的结果与错误。
func runBoth(t *testing.T, build func() (*workflow.Graph[record], error), approvals ...string) (record, error, record, error) {
	t.Helper()
	g, err := build()
	require.NoError(t, err)
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	plain, plainErr := run.Invoke(context.Background(), record{})
	e := newEnv(t, "both")
	snap, durableErr := compile(t, e, g, "v1", approvals...).Run(context.Background(), e.id, record{}, "")
	var durableOut record
	if snap != nil {
		durableOut = snap.State
	}
	return plain, plainErr, durableOut, durableErr
}

func requireSameResult(t *testing.T, build func() (*workflow.Graph[record], error), want []string) {
	t.Helper()
	plain, plainErr, durableOut, durableErr := runBoth(t, build)
	require.NoError(t, plainErr)
	require.NoError(t, durableErr)
	require.Equal(t, want, plain.Log)
	require.Equal(t, want, durableOut.Log, "两种编译方式结果一致")
}

func TestSequentialRunsInOrder(t *testing.T) {
	requireSameResult(t, func() (*workflow.Graph[record], error) {
		return Sequential(workflow.Config{}, mark("extract"), lookups(t), mark("answer"))
	}, []string{"extract", "kb", "order", "ship", "answer"})
}

func TestSequentialValidation(t *testing.T) {
	bad := Step[record]{Name: "nil-run"}
	invalidName := Step[record]{Name: " x", Run: mark("x").Run}
	for name, build := range map[string]func() (*workflow.Graph[record], error){
		"Sequential 无 Step":   func() (*workflow.Graph[record], error) { return Sequential[record](workflow.Config{}) },
		"Sequential Run 为空":   func() (*workflow.Graph[record], error) { return Sequential(workflow.Config{}, bad) },
		"Sequential 名称无效":     func() (*workflow.Graph[record], error) { return Sequential(workflow.Config{}, invalidName) },
		"Sequential 重名":       func() (*workflow.Graph[record], error) { return Sequential(workflow.Config{}, mark("a"), mark("a")) },
		"Sequential MaxSteps": func() (*workflow.Graph[record], error) { return Sequential(workflow.Config{MaxSteps: 5}, mark("a")) },
	} {
		g, err := build()
		require.Error(t, err, name)
		require.Nil(t, g, name)
	}
}
