package budget

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
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

func TestUsageHookTimeoutIsRequiredValue(t *testing.T) {
	_, err := WithUsageHookTimeout(context.Background(), -time.Second)
	require.Error(t, err)
	for _, tc := range []struct {
		name string
		set  time.Duration
		want time.Duration
	}{
		{"未设置用默认", -1, DefaultUsageHookTimeout},
		{"零用默认", 0, DefaultUsageHookTimeout},
		{"显式值", time.Minute, time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.set >= 0 {
				var err error
				ctx, err = WithUsageHookTimeout(ctx, tc.set)
				require.NoError(t, err)
			}
			var left time.Duration
			NotifyUsage(WithUsageHook(ctx, func(ctx context.Context, _ UsageEvent) {
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				left = time.Until(deadline)
			}), usage.ModelEvidence{}, "")
			require.InDelta(t, tc.want, left, float64(time.Second))
		})
	}
}
