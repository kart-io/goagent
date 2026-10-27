package tools

import (
	"context"
	"fmt"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestDispatcherMultipleEffectsAndRecovery(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
	seen := map[string]string{}
	calls := map[string]int{}
	keys := map[string]string{}
	ambiguous := true
	reg := NewRegistry()
	require.NoError(t, reg.Register(NewBaseTool("write", "test", `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`, func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		id := in.Args["id"].(string)
		calls[id]++
		keys[id] = in.ExecutionKey
		require.Equal(t, in.ExecutionKey, durable.ExecutionKey(ctx))
		if _, ok := seen[in.ExecutionKey]; !ok {
			seen[in.ExecutionKey] = "receipt-" + id
		}
		if id == "B" && ambiguous {
			return nil, fmt.Errorf("lost response after effect")
		}
		return &interfaces.ToolOutput{Success: true, Result: seen[in.ExecutionKey]}, nil
	})))
	d, err := NewDispatcher(reg, DispatchConfig{})
	require.NoError(t, err)
	req := func(id string) ToolRequest {
		return ToolRequest{Name: "write", Input: &interfaces.ToolInput{CallID: id, Args: map[string]any{"id": id}}}
	}
	_, err = d.Invoke(ctx, req("A"))
	require.NoError(t, err)
	_, err = d.Invoke(ctx, req("B"))
	require.Error(t, err)
	ambiguous = false
	for _, id := range []string{"A", "B"} {
		out, err := d.Invoke(ctx, req(id))
		require.NoError(t, err)
		require.Equal(t, "receipt-"+id, out.Result)
	}
	require.NotEqual(t, keys["A"], keys["B"])
	require.Len(t, seen, 2)
	require.Equal(t, map[string]int{"A": 1, "B": 2}, calls)
}
