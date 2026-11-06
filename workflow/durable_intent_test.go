package workflow_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/workflow"
)

// 意图快照只更新位置与键，状态沿用上一次提交的原文：不重新编码，也不把旧版本写入的原文改写为当前编码。
func TestDurableGraphIntentKeepsCommittedStateBytes(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var seen []byte
	g := oneNode(t, func(ctx context.Context, n int) (int, error) {
		cp, err := store.Load(ctx, "run")
		require.NoError(t, err)
		seen = append([]byte(nil), cp.Data...)
		return n + 1, nil
	})
	run, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}})
	require.NoError(t, err)
	wait, err := run.Run(ctx, "run", 41, "")
	require.Error(t, err)

	cp, err := store.Load(ctx, "run")
	require.NoError(t, err)
	committed := `{"state":41,"routing":false,"node":"effect","written_by":"newer"}`
	cp.Data = []byte(committed)
	require.NoError(t, store.Save(ctx, "run", cp))

	done, err := run.Run(ctx, "run", 0, wait.Key)
	require.NoError(t, err)
	require.Equal(t, 42, done.State)
	require.Equal(t, committed, string(seen))
}
