package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/stretchr/testify/require"
)

func TestDurableAdaptStreamPreservesPreviewWithoutCommittingIt(t *testing.T) {
	for _, fail := range []bool{false, true} {
		ctx := context.Background()
		source := core.NewStreamRunnable(core.StreamModeTokens, func(_ context.Context, in int, emit func(core.StreamChunk[int]) error) error {
			if err := emit(core.StreamChunk[int]{Data: 0}); err != nil {
				return err
			}
			if err := emit(core.StreamChunk[int]{Data: 7, Event: &core.StreamEvent{Type: "preview"}}); err != nil {
				return err
			}
			if fail {
				return errors.New("injected after preview")
			}
			return emit(core.StreamChunk[int]{Done: true, Data: 9})
		})
		adapted, err := AdaptStream(source, func(n int) (int, error) { return n, nil }, func(_, n int) (int, error) { return n, nil }, func(_ int, c core.StreamChunk[int]) (core.StreamChunk[int], error) { return c, nil })
		require.NoError(t, err)
		graph := NewGraph[int]()
		require.NoError(t, graph.AddRunnable("stream", adapted))
		require.NoError(t, graph.AddEdge(START, "stream"))
		require.NoError(t, graph.AddEdge("stream", END))
		store, err := durable.NewFileStore(t.TempDir())
		require.NoError(t, err)
		run, err := graph.CompileDurable(ctx, DurableConfig{Store: store, Version: "v1"})
		require.NoError(t, err)
		stream, err := run.AsRunnable().Stream(ctx, RunRequest[int]{RunID: "run", Initial: 1})
		require.NoError(t, err)
		sawPreview, sawZero, sawError := false, false, false
		for c := range stream {
			if !c.Done && c.Event == nil && c.Data != nil && c.Data.Preview {
				require.Zero(t, c.Data.State)
				sawZero = true
			}
			if c.Event != nil && c.Event.Type == "preview" {
				require.NotNil(t, c.Data, "typed preview was dropped")
				require.Equal(t, 7, c.Data.State)
				require.False(t, c.Done)
				require.False(t, c.Data.Done)
				require.True(t, c.Data.Preview)
				sawPreview = true
			}
			if c.Error != nil {
				require.True(t, fail)
				sawError = true
			}
			if c.Done && c.Error == nil {
				require.False(t, c.Data.Preview)
				require.True(t, c.Data.Done)
			}
		}
		require.True(t, sawPreview)
		require.True(t, sawZero)
		require.Equal(t, fail, sawError)
		saved, err := run.Snapshot(ctx, "run")
		require.NoError(t, err)
		require.False(t, saved.Preview)
		if fail {
			require.Equal(t, 1, saved.State)
			require.False(t, saved.Done)
		} else {
			require.Equal(t, 9, saved.State)
			require.True(t, saved.Done)
		}
	}
}

func TestDurablePreviewCancellationDoesNotCommit(t *testing.T) {
	stopped := make(chan struct{})
	node := core.NewStreamRunnable(core.StreamModeTokens, func(ctx context.Context, in int, emit func(core.StreamChunk[int]) error) error {
		defer close(stopped)
		if err := emit(core.StreamChunk[int]{Data: 7}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	g := NewGraph[int]()
	require.NoError(t, g.AddRunnable("stream", node))
	require.NoError(t, g.AddEdge(START, "stream"))
	require.NoError(t, g.AddEdge("stream", END))
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	run, err := g.CompileDurable(context.Background(), DurableConfig{Store: store, Version: "v1"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := run.AsRunnable().Stream(ctx, RunRequest[int]{RunID: "run", Initial: 1})
	require.NoError(t, err)
	sawPreview := false
	for c := range stream {
		if c.Data != nil && c.Data.Preview {
			sawPreview = true
			require.Equal(t, 7, c.Data.State)
			cancel()
		}
	}
	require.True(t, sawPreview)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("preview producer did not stop")
	}
	saved, err := run.Snapshot(context.Background(), "run")
	require.NoError(t, err)
	require.Equal(t, 1, saved.State)
	require.False(t, saved.Done)
	require.False(t, saved.Preview)
}
