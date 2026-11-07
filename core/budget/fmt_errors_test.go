package budget

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/usage"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestFormattedBudgetErrorClassification(t *testing.T) {
	t.Run("配置错误", func(t *testing.T) {
		_, err := New(Config{})
		require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
		require.ErrorContains(t, err, "budget requires root key")
	})
	t.Run("预留输入无效不消耗额度", func(t *testing.T) {
		c, err := New(Config{RootKey: "run", Version: "v1", MaxModelRequests: 1})
		require.NoError(t, err)
		err = c.ReserveEstimated(context.Background(), "", 1)
		require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
		var business *agentErrors.AgentError
		require.ErrorAs(t, err, &business)
		require.Equal(t, "budget", business.Component)
		require.NotEmpty(t, business.Operation)
		remaining, err := c.Remaining(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, remaining)
	})
	t.Run("损坏持久账本拒绝准入", func(t *testing.T) {
		store, err := durable.NewFileStore(t.TempDir())
		require.NoError(t, err)
		cfg := Config{RootKey: "run", Version: "v1", MaxModelRequests: 1}
		c, err := NewDurable(cfg, store)
		require.NoError(t, err)
		id := c.storageID()
		require.NoError(t, store.Save(context.Background(), id, &durable.Snapshot{RunID: id, Version: "unexpected", Data: []byte(`{}`)}))
		err = c.Reserve(context.Background(), "attempt")
		require.Equal(t, agentErrors.CodeStateValidation, agentErrors.GetCode(err))
		require.ErrorContains(t, err, `unsupported budget record format "unexpected"`)
		saved, err := store.Load(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, "unexpected", saved.Version)
	})
}

func TestSavedBudgetEvidenceErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		evidence usage.Evidence
	}{
		{name: "负 token", evidence: usage.Evidence{KnownFields: usage.Prompt, Tokens: interfaces.TokenUsage{PromptTokens: -1}}},
		{name: "非法 mask", evidence: usage.Evidence{KnownFields: usage.Fields(128)}},
		{name: "未报告字段非零", evidence: usage.Evidence{Tokens: interfaces.TokenUsage{PromptTokens: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := durable.NewFileStore(t.TempDir())
			require.NoError(t, err)
			c, err := NewDurable(Config{RootKey: "run", Version: "v1", MaxModelRequests: 2}, store)
			require.NoError(t, err)
			id := c.storageID()
			data, err := json.Marshal(ledger{Config: c.config, Entries: map[string]entry{"saved": {Evidence: tc.evidence}}})
			require.NoError(t, err)
			corrupt := &durable.Snapshot{RunID: id, Version: ledgerFormat, Data: data}
			unlock, err := store.Lock(ctx, id)
			require.NoError(t, err)
			require.NoError(t, store.Save(ctx, id, corrupt))
			require.NoError(t, unlock())

			err = c.Reserve(ctx, "next")
			require.Equal(t, agentErrors.CodeStateValidation, agentErrors.GetCode(err))
			var business *agentErrors.AgentError
			require.ErrorAs(t, err, &business)
			require.Equal(t, "budget", business.Component)
			require.Equal(t, "transaction", business.Operation)
			require.NotNil(t, business.Cause)
			require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(business.Cause))
			require.ErrorContains(t, err, tc.evidence.Validate().Error())
			saved, err := store.Load(ctx, id)
			require.NoError(t, err)
			require.Equal(t, corrupt, saved)

			_, inputErr := c.RecordEvidence(ctx, "saved", nil, tc.evidence)
			require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(inputErr))
			saved, err = store.Load(ctx, id)
			require.NoError(t, err)
			require.Equal(t, corrupt, saved)
		})
	}
}
