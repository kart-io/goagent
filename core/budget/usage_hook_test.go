package budget

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestUsageHookIncludesEvidenceAndIndependentTotals(t *testing.T) {
	c, err := New(Config{RootKey: "hook", Version: "policy", MaxModelRequests: 1})
	require.NoError(t, err)
	ctx := WithController(context.Background(), c)
	require.NoError(t, c.Reserve(ctx, "attempt"))
	e := usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 7}, Source: "provider"}
	_, err = c.RecordEvidence(ctx, "attempt", nil, e)
	require.NoError(t, err)
	called := 0
	ctx = WithUsageHook(ctx, func(callCtx context.Context, event UsageEvent) {
		called++
		require.True(t, event.HasStatistics)
		require.NoError(t, event.StatisticsError)
		require.Equal(t, 7, event.Model.Tokens.TotalTokens)
		require.Equal(t, 7, event.Statistics.Root.KnownTokens.TotalTokens)
		_, err := c.Snapshot(callCtx)
		require.NoError(t, err, "hook runs outside ledger lock")
		event.Model.Tokens.TotalTokens = 999
		event.Statistics.Root.KnownTokens.TotalTokens = 999
	})
	notification := usage.ModelEvidence{AttemptKey: "attempt", Evidence: e, AccountingCommitted: true}
	NotifyUsage(ctx, notification, "")
	require.Equal(t, 1, called)
	require.Equal(t, 7, notification.Tokens.TotalTokens)
	s, err := c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 7, s.KnownTokens.TotalTokens)
}

type hookQueryFailure struct {
	durable.Store
	panics bool
}

func (s hookQueryFailure) Lock(context.Context, string) (func() error, error) {
	return func() error { return nil }, nil
}
func (s hookQueryFailure) Load(context.Context, string) (*durable.Snapshot, error) {
	if s.panics {
		panic("query panic")
	}
	return nil, errors.New("query unavailable")
}
func TestUsageHookStillReceivesEvidenceWhenStatisticsFail(t *testing.T) {
	for _, panics := range []bool{false, true} {
		c, err := New(Config{RootKey: "query", Version: "policy", MaxModelRequests: 1})
		require.NoError(t, err)
		c.store = hookQueryFailure{panics: panics}
		called := false
		ctx := WithUsageHook(WithController(context.Background(), c), func(_ context.Context, event UsageEvent) {
			called = true
			require.False(t, event.HasStatistics)
			require.Error(t, event.StatisticsError)
			require.Equal(t, "attempt", event.Model.AttemptKey)
		})
		require.NotPanics(t, func() { NotifyUsage(ctx, usage.ModelEvidence{AttemptKey: "attempt"}, "") })
		require.True(t, called)
	}
}

func TestUsageHookWithoutBudgetCancellationAndPanicIsolation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	ctx = WithUsageHook(ctx, func(callCtx context.Context, event UsageEvent) {
		called = true
		require.NoError(t, callCtx.Err())
		require.False(t, event.HasStatistics)
		require.NoError(t, event.StatisticsError)
		panic("metrics sink panic")
	})
	require.NotPanics(t, func() { NotifyUsage(ctx, usage.ModelEvidence{AttemptKey: "attempt"}, "") })
	require.True(t, called)
	ctx = WithUsageHook(ctx, nil)
	require.False(t, HasUsageHook(ctx))
}
