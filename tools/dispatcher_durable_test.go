package tools

import (
	"context"
	"fmt"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/toolargs"
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

// 同一 CallID 换成 float64 相同但实际不同的大整数时，不得回放旧结果。
func TestDispatcherIntentUsesExactArguments(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
	reg := NewRegistry()
	require.NoError(t, reg.Register(NewBaseTool("lookup", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "first"}, nil
	})))
	d, err := NewDispatcher(reg, DispatchConfig{})
	require.NoError(t, err)
	req := func(raw string) ToolRequest {
		args, ok := toolargs.Parse(raw)
		require.True(t, ok)
		return ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{CallID: "c1", Args: args}}
	}
	_, err = d.Invoke(ctx, req(`{"id":9007199254740992}`))
	require.NoError(t, err)
	_, err = d.Invoke(ctx, req(`{"id":9007199254740993}`))
	require.Error(t, err, "参数不同却回放了旧结果")
}

// 大整数参数通过 integer 校验，授权钩子看到的值与工具执行的值相同。
func TestDispatcherAuthorizesAndExecutesSameExactArguments(t *testing.T) {
	var authorized, executed any
	reg := NewRegistry()
	require.NoError(t, reg.Register(NewBaseTool("refund", "test", `{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`, func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		executed = in.Args["id"]
		return &interfaces.ToolOutput{Success: true}, nil
	})))
	d, err := NewDispatcher(reg, DispatchConfig{Authorize: func(_ context.Context, _ string, in *interfaces.ToolInput) error {
		authorized = in.Args["id"]
		return nil
	}})
	require.NoError(t, err)
	args, ok := toolargs.Parse(`{"id":9007199254740993}`)
	require.True(t, ok)
	_, err = d.Invoke(context.Background(), ToolRequest{Name: "refund", Input: &interfaces.ToolInput{Args: args}})
	require.NoError(t, err)
	require.Equal(t, json.Number("9007199254740993"), authorized)
	require.Equal(t, authorized, executed)
}

// 舍入后才满足 integer 的参数作为参数错误拒绝，不交给授权与执行。
func TestDispatcherRejectsNumbersThatOnlyPassAfterRounding(t *testing.T) {
	executed := false
	reg := NewRegistry()
	require.NoError(t, reg.Register(NewBaseTool("refund", "test", `{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		executed = true
		return &interfaces.ToolOutput{Success: true}, nil
	})))
	d, err := NewDispatcher(reg, DispatchConfig{})
	require.NoError(t, err)
	args, ok := toolargs.Parse(`{"id":1.0000000000000001}`)
	require.True(t, ok)
	_, err = d.Invoke(context.Background(), ToolRequest{Name: "refund", Input: &interfaces.ToolInput{Args: args}})
	var argErr *ArgumentError
	require.ErrorAs(t, err, &argErr)
	require.False(t, executed)
}
