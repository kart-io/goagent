package builder

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestHostTokenEstimatorControlsNativeRequests(t *testing.T) {
	for _, estimated := range []bool{false, true} {
		t.Run(map[bool]string{false: "缺少估计", true: "保守预留"}[estimated], func(t *testing.T) {
			model := nativeModel([]*llm.StreamChunk{{Delta: "答案", Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 3}}})
			b := NewSimpleBuilder(model).WithTools(NewMockTool("lookup", "x"))
			if estimated {
				b = b.WithTokenEstimator(func(context.Context, *llm.CompletionRequest, []interfaces.Tool) (int, error) { return 6, nil })
			}
			a, err := b.Build()
			require.NoError(t, err)
			c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 2, MaxReservedTokens: 6, TokenPolicyVersion: "host-v1"})
			require.NoError(t, err)
			_, err = a.AsRunnable().Invoke(budget.WithController(context.Background(), c), "问题")
			if !estimated {
				require.ErrorIs(t, err, budget.ErrTokenEstimateRequired)
				require.Empty(t, model.requests)
				return
			}
			require.NoError(t, err)
			snap, err := c.Snapshot(context.Background())
			require.NoError(t, err)
			require.Equal(t, 6, snap.ReservedTokens)
			require.Equal(t, -3, snap.KnownTokenDelta)
		})
	}
}

func TestLegacyBudgetCompletedModelIntentStillReplays(t *testing.T) {
	ctx := context.Background()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	cfg := budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 2}
	c, err := budget.NewDurable(cfg, store)
	require.NoError(t, err)
	ctx = durable.WithJournal(durable.WithExecutionKey(budget.WithController(ctx, c), "parent"), store)
	key := durable.ChildKey("parent", "model")
	require.NoError(t, c.Reserve(ctx, key))
	intent := json.RawMessage(`{"Request":"旧请求"}`)
	// 使用升级前的配置结构写入completed模型操作，不能由新Config生成夹具。
	oldIntent, err := json.Marshal(struct {
		Original json.RawMessage
		Budget   struct {
			RootKey, Version string
			MaxModelRequests int
		}
	}{intent, struct {
		RootKey, Version string
		MaxModelRequests int
	}{"root", "v1", 2}})
	require.NoError(t, err)
	_, err = durable.Do(ctx, "model", oldIntent, durable.OperationPolicy{}, func(context.Context) (json.RawMessage, error) {
		return json.RawMessage(`{"content":"旧答案","done":true,"finish_reason":"stop"}`), nil
	})
	require.NoError(t, err)
	turn, err := modelOperation(ctx, "model", intent, func(context.Context, func(AgentEvent) error) (*llm.StreamChunk, error) {
		return nil, errors.New("不应重新请求模型")
	}, nil, func(AgentEvent) error { return nil })
	require.NoError(t, err)
	require.Equal(t, "旧答案", turn.Content)
}
