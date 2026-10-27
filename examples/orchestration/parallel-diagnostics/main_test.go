package main

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTypedParallelRetainsZerosAndSource(t *testing.T) {
	r, err := build(context.Background())
	require.NoError(t, err)
	in := diagnostics{Values: map[string]int{"cpu": 0, "memory": 4}}
	out, err := r.Invoke(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, 4, out.Total)
	out.Values["cpu"] = 9
	require.Zero(t, in.Values["cpu"])
	_, err = r.Invoke(context.Background(), diagnostics{})
	require.Error(t, err)
}
