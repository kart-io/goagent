package workflow_test

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

// panicLoadStore 的 Load 直接 panic。
type panicLoadStore struct{ *durable.FileStore }

func (panicLoadStore) Load(context.Context, string) (*durable.Snapshot, error) { panic("load boom") }

// panicDecodeState 反序列化时 panic，用于触发检查点解码中的 panic。
type panicDecodeState struct{ N int }

func (*panicDecodeState) UnmarshalJSON([]byte) error { panic("decode boom") }

func requireWorkflowPanic(t *testing.T, err error, op string, value any) {
	t.Helper()
	var panicErr *agentErrors.PanicError
	require.ErrorAs(t, err, &panicErr)
	require.Equal(t, op, panicErr.Op)
	require.Equal(t, value, panicErr.Value)
	require.NotEmpty(t, panicErr.Stack)
	require.NotContains(t, err.Error(), value)
}

// 持久图中存储、检查点解码与分支路由的 panic 以 *PanicError 返回，保留 panic 值且错误文本不含该值。
func TestDurableGraphPanicIsPanicError(t *testing.T) {
	ctx := context.Background()
	noop := func(_ context.Context, n int) (int, error) { return n, nil }

	t.Run("storage", func(t *testing.T) {
		run, err := oneNode(t, noop).CompileDurable(ctx, workflow.DurableConfig{Store: panicLoadStore{newStore(t)}, Version: "v1"})
		require.NoError(t, err)
		_, err = run.Run(ctx, "run", 1, "")
		requireWorkflowPanic(t, err, "durable graph storage or serialization", "load boom")
	})

	t.Run("decode", func(t *testing.T) {
		g := workflow.NewGraph[*panicDecodeState]()
		require.NoError(t, g.AddNode("effect", func(_ context.Context, s *panicDecodeState) (*panicDecodeState, error) { return s, nil }))
		require.NoError(t, g.AddEdge(workflow.START, "effect"))
		require.NoError(t, g.AddEdge("effect", workflow.END))
		run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: newStore(t), Version: "v1"})
		require.NoError(t, err)
		_, err = run.Run(ctx, "run", &panicDecodeState{N: 1}, "")
		requireWorkflowPanic(t, err, "checkpoint decoding", "decode boom")
	})

	t.Run("branch", func(t *testing.T) {
		g := workflow.NewGraph[int]()
		require.NoError(t, g.AddNode("a", noop))
		require.NoError(t, g.AddNode("b", noop))
		require.NoError(t, g.AddEdge(workflow.START, "a"))
		require.NoError(t, g.AddBranch("a", func(context.Context, int) (string, error) { panic("route boom") }, "b"))
		require.NoError(t, g.AddEdge("b", workflow.END))
		run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: newStore(t), Version: "v1"})
		require.NoError(t, err)
		_, err = run.Run(ctx, "run", 1, "")
		requireWorkflowPanic(t, err, `branch "a"`, "route boom")
	})
}
