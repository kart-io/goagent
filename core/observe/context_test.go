package observe

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunIDFromContext(t *testing.T) {
	require.Empty(t, RunIDFromContext(nil))
	require.Empty(t, RunIDFromContext(context.Background()))
	ctx := WithRunID(context.Background(), "run")
	require.Equal(t, "run", RunIDFromContext(ctx))
	require.Equal(t, "run", RunIDFromContext(context.WithValue(ctx, struct{}{}, true)))
	require.NotEmpty(t, RunIDFromContext(WithRunID(context.Background(), "")))
}
