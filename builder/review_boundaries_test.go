package builder

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core/budget"
	"github.com/stretchr/testify/require"
)

func TestLegacyEntriesRejectBoundRootBudgetBeforeRequest(t *testing.T) {
	for _, entry := range []string{"Execute", "ExecuteWithTools"} {
		t.Run(entry, func(t *testing.T) {
			model := NewMockLLMClient()
			agent, err := NewSimpleBuilder(model).Build()
			require.NoError(t, err)
			controller, err := budget.New(budget.Config{RootKey: "legacy", Version: "v1", MaxModelRequests: 1})
			require.NoError(t, err)
			ctx := budget.WithController(context.Background(), controller)
			for range 2 {
				if entry == "Execute" {
					_, err = agent.Execute(ctx, "question")
				} else {
					_, err = agent.ExecuteWithTools(ctx, "question")
				}
				require.ErrorContains(t, err, "root budget")
			}
			require.Zero(t, model.index)
			snapshot, err := controller.Snapshot(ctx)
			require.NoError(t, err)
			require.Zero(t, snapshot.ReservedRequests)
			// The old, unconfigured synchronous entry is still supported.
			_, err = agent.Execute(context.Background(), "question")
			require.NoError(t, err)
			require.Equal(t, 1, model.index)
		})
	}
}
