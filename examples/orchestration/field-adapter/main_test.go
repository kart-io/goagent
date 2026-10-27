package main

import (
	"context"
	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTypedAdapterErrorsStopNode(t *testing.T) {
	calls := 0
	node := core.NewStreamRunnable(core.StreamModeFinal, func(_ context.Context, n int, emit func(core.StreamChunk[int]) error) error {
		calls++
		return emit(core.StreamChunk[int]{Done: true, Data: n})
	})
	r, err := adapter(node)
	require.NoError(t, err)
	_, err = r.Invoke(context.Background(), order{ID: "42"})
	require.Error(t, err)
	require.Zero(t, calls)
	zero := 0
	source := order{ID: "42", Discount: &zero}
	out, err := r.Invoke(context.Background(), source)
	require.NoError(t, err)
	require.Zero(t, out.Total)
	require.Equal(t, 1, calls)
	require.Equal(t, source, order{ID: "42", Discount: &zero})
}
