package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeSnapshotAndVisit(t *testing.T) {
	g := NewGraph[int]()
	require.NoError(t, g.AddNode("a", func(_ context.Context, n int) (int, error) { return n + 1, nil }))
	require.NoError(t, g.AddEdge(START, "a"))
	require.NoError(t, g.AddEdge("a", END))
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	require.NoError(t, g.AddNode("later", func(_ context.Context, n int) (int, error) { return n, nil }))
	description, ok := run.Description().(GraphDescription)
	require.True(t, ok)
	require.Equal(t, "goagent-graph-v1", description.FormatVersion)
	require.Len(t, description.Nodes, 1)
	require.Equal(t, "a", description.Nodes[0].ID)
}
