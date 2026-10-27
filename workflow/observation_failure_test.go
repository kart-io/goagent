package workflow

import (
	"context"
	"github.com/kart-io/goagent/core/observe"
	"github.com/stretchr/testify/require"
	"testing"
)

type eventCollector struct{ events []observe.Event }

func (c *eventCollector) Observe(_ context.Context, e observe.Event) { c.events = append(c.events, e) }
func TestPanickingNodeObservationIsNotSuccess(t *testing.T) {
	g := NewGraph[int]()
	require.NoError(t, g.AddNode("panic", func(context.Context, int) (int, error) { panic("sensitive panic body") }))
	require.NoError(t, g.AddEdge(START, "panic"))
	require.NoError(t, g.AddEdge("panic", END))
	r, err := g.Compile(context.Background())
	require.NoError(t, err)
	sink := &eventCollector{}
	_, err = r.Invoke(observe.WithObserver(context.Background(), sink), 0)
	require.Error(t, err)
	types := []string{}
	for _, e := range sink.events {
		types = append(types, e.Type)
		require.NotContains(t, e.ErrorKind, "sensitive")
	}
	require.Contains(t, types, "node_error")
	require.Contains(t, types, "graph_error")
	require.NotContains(t, types, "graph_end")
}
