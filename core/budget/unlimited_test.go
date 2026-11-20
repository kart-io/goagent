package budget

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/usage"
)

func TestBudgetZeroIsUnlimited(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "zero model requests", cfg: Config{RootKey: "r", Version: "v2"}},
		{name: "zero model requests with tool cap", cfg: Config{RootKey: "r", Version: "v2", MaxToolCalls: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(tt.cfg)
			require.NoError(t, err)
			require.Zero(t, c.Configuration().MaxAgentCalls, "0 must stay 0, not be filled with a default")
			for i := 0; i < 101; i++ {
				require.NoError(t, c.Reserve(ctx, fmt.Sprintf("k%d", i)))
			}
			remaining, err := c.Remaining(ctx)
			require.NoError(t, err)
			require.Equal(t, constants.BudgetUnlimited, remaining)
		})
	}
}

func TestBudgetZeroAgentCallsAcceptsLongLineage(t *testing.T) {
	ctx := context.Background()
	c, err := New(Config{RootKey: "r", Version: "v2"})
	require.NoError(t, err)
	for i := 0; i < 1001; i++ {
		owner := usage.Owner{RunID: "r", AgentID: "a", AgentCallKey: fmt.Sprintf("call-%d", i)}
		require.NoError(t, c.RegisterAgent(ctx, []usage.Owner{owner}))
	}
}
