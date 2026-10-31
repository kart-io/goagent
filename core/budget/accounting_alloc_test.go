package budget_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

// 固定规模账本的幂等记账应避免为每个JSON键重新分配字符串；这个资源上限保留足够余量。
func TestInMemoryAccountingAllocationBudget(t *testing.T) {
	const entries = 128
	ctx := context.Background()
	c, err := budget.New(budget.Config{RootKey: "allocation-test", Version: "v1", MaxModelRequests: entries})
	require.NoError(t, err)
	usage := &interfaces.TokenUsage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}
	for i := range entries {
		key := fmt.Sprint(i)
		require.NoError(t, c.Reserve(ctx, key))
		require.NoError(t, c.RecordUsage(ctx, key, usage))
	}
	allocations := testing.AllocsPerRun(20, func() { err = c.RecordUsage(ctx, "0", usage) })
	require.NoError(t, err)
	require.LessOrEqual(t, allocations, float64(entries+64), "固定128条账本的重复记账分配不能超过192次")
	snapshot, err := c.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, entries, snapshot.ReservedRequests)
	require.Equal(t, entries*3, snapshot.KnownTokens.TotalTokens)
}
