package workflow

import (
	"context"
	"github.com/kart-io/goagent/core/durable"
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

func TestDurableResumesHaveDistinctObservationIdentities(t *testing.T) {
	g := NewGraph[int]()
	require.NoError(t, g.AddNode("node", func(_ context.Context, n int) (int, error) { return n + 1, nil }))
	require.NoError(t, g.AddEdge(START, "node"))
	require.NoError(t, g.AddEdge("node", END))
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	run, err := g.CompileDurable(context.Background(), DurableConfig{Store: store, Version: "v1"})
	require.NoError(t, err)
	sink := &eventCollector{}
	ctx := observe.WithObserver(context.Background(), sink)
	for range 2 {
		_, err = run.Run(ctx, "same-business-run", 0, "")
		require.NoError(t, err)
	}
	events, visits := map[string]bool{}, map[string]bool{}
	for _, event := range sink.events {
		require.Equal(t, "same-business-run", event.RunID)
		require.False(t, events[event.EventID], "duplicate EventID %s", event.EventID)
		events[event.EventID] = true
		if event.Type == "graph_start" {
			require.False(t, visits[event.VisitID], "duplicate VisitID %s", event.VisitID)
			visits[event.VisitID] = true
		}
	}
	require.Len(t, visits, 2)
}
