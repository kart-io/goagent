package budget

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestConfigurationNormalizesDefaultCallLimit(t *testing.T) {
	c, e := New(Config{RootKey: "root", Version: "policy", MaxModelRequests: 3})
	require.NoError(t, e)
	data, e := json.Marshal(c.Configuration())
	require.NoError(t, e)
	require.JSONEq(t, `{"RootKey":"root","Version":"policy","MaxModelRequests":3,"MaxAgentCalls":1000}`, string(data))
}

func TestTokenAndToolLimitsRemainConservativeAcrossRestart(t *testing.T) {
	ctx := context.Background()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	cfg := Config{RootKey: "root", Version: "v1", MaxModelRequests: 4, MaxReservedTokens: 10, TokenPolicyVersion: "tokens-v1", MaxToolCalls: 1}
	c, err := NewDurable(cfg, store)
	require.NoError(t, err)
	require.ErrorIs(t, c.Reserve(ctx, "missing"), ErrTokenEstimateRequired)
	require.NoError(t, c.ReserveEstimated(ctx, "one", 6))
	require.NoError(t, c.RecordUsage(ctx, "one", &interfaces.TokenUsage{TotalTokens: 3}))
	require.NoError(t, c.ReserveTool(ctx, "tool-one"))
	restarted, err := NewDurable(cfg, store)
	require.NoError(t, err)
	require.ErrorIs(t, restarted.ReserveEstimated(ctx, "two", 5), ErrBudgetExceeded)
	require.NoError(t, restarted.ReserveEstimated(ctx, "two", 4))
	require.ErrorIs(t, restarted.ReserveTool(ctx, "tool-two"), ErrBudgetExceeded)
	snap, err := restarted.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 10, snap.ReservedTokens)
	require.Equal(t, -3, snap.KnownTokenDelta)
	require.Equal(t, 1, snap.ReservedToolCalls)
	require.Equal(t, 1, snap.UnknownUsageEntries)
	cfg.TokenPolicyVersion = "tokens-v2"
	_, err = NewDurable(cfg, store)
	require.Error(t, err)
}

func TestBudgetDeadlineBindsExecutionAndStopsAdmission(t *testing.T) {
	c, err := New(Config{RootKey: "root", Version: "v1", MaxModelRequests: 2, DeadlineUnixMilli: time.Now().Add(-time.Second).UnixMilli()})
	require.NoError(t, err)
	ctx, cancel := Bind(context.Background(), c)
	defer cancel()
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.ErrorIs(t, c.Reserve(context.Background(), "model"), context.DeadlineExceeded)
	require.ErrorIs(t, c.ReserveTool(context.Background(), "tool"), context.DeadlineExceeded)
}

func TestBudgetRejectsOldStorageFormats(t *testing.T) {
	for _, version := range []string{"goagent-model-budget-v1", "goagent-model-budget-v2"} {
		t.Run(version, func(t *testing.T) {
			ctx := context.Background()
			store, e := durable.NewFileStore(t.TempDir())
			require.NoError(t, e)
			cfg := Config{RootKey: "root", Version: "policy", MaxModelRequests: 3}
			c, e := New(cfg)
			require.NoError(t, e)
			id := c.storageID()
			data, e := json.Marshal(c.memory)
			require.NoError(t, e)
			require.NoError(t, store.Save(ctx, id, &durable.Snapshot{RunID: id, Version: version, Data: data}))
			_, e = NewDurable(cfg, store)
			require.Error(t, e)
			unchanged, e := store.Load(ctx, id)
			require.NoError(t, e)
			require.Equal(t, version, unchanged.Version)
		})
	}
}

func TestExtendedLimitsAreAtomicAcrossControllers(t *testing.T) {
	ctx := context.Background()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	cfg := Config{RootKey: "root", Version: "v1", MaxModelRequests: 20, MaxReservedTokens: 3, TokenPolicyVersion: "host-v1", MaxToolCalls: 2}
	a, err := NewDurable(cfg, store)
	require.NoError(t, err)
	b, err := NewDurable(cfg, store)
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 20)
	for i := range 20 {
		c := a
		if i%2 == 1 {
			c = b
		}
		go func() { <-start; results <- c.ReserveEstimated(ctx, fmt.Sprint(i), 1) }()
	}
	close(start)
	success := 0
	for range 20 {
		if err := <-results; err == nil {
			success++
		} else {
			require.ErrorIs(t, err, ErrBudgetExceeded)
		}
	}
	require.Equal(t, 3, success)
	require.NoError(t, a.ReserveTool(ctx, "a"))
	require.NoError(t, b.ReserveTool(ctx, "b"))
	require.ErrorIs(t, a.ReserveTool(ctx, "c"), ErrBudgetExceeded)
	snap, err := b.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, snap.ReservedTokens)
	require.Equal(t, 2, snap.ReservedToolCalls)
}

func TestJournaledToolAdmissionRejectsVolatileBudget(t *testing.T) {
	c, err := New(Config{RootKey: "root", Version: "v1", MaxModelRequests: 2, MaxToolCalls: 1})
	require.NoError(t, err)
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "run"), store)
	require.Error(t, c.ReserveTool(ctx, "tool"))
	snap, err := c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Zero(t, snap.ReservedToolCalls)
}

func TestLedgerRejectsChangedLimitConfiguration(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*Config)
	}{
		{"工具限制", func(c *Config) { c.MaxToolCalls = 1 }},
		{"Token限制", func(c *Config) { c.MaxReservedTokens = 5; c.TokenPolicyVersion = "host-v1" }},
		{"截止时间", func(c *Config) { c.DeadlineUnixMilli = time.Now().Add(time.Hour).UnixMilli() }},
	} {
		t.Run(change.name, func(t *testing.T) {
			store, err := durable.NewFileStore(t.TempDir())
			require.NoError(t, err)
			cfg := Config{RootKey: "root", Version: "v1", MaxModelRequests: 3}
			_, err = NewDurable(cfg, store)
			require.NoError(t, err)
			change.apply(&cfg)
			_, err = NewDurable(cfg, store)
			require.Error(t, err, "旧运行不能被新的策略覆盖")
		})
	}
}

func TestTokenLimitedLedgerRejectsMissingOrZeroEstimate(t *testing.T) {
	for _, zero := range []bool{false, true} {
		t.Run(fmt.Sprint(zero), func(t *testing.T) {
			ctx := context.Background()
			store, err := durable.NewFileStore(t.TempDir())
			require.NoError(t, err)
			cfg := Config{RootKey: "root", Version: "v1", MaxModelRequests: 3, MaxReservedTokens: 5, TokenPolicyVersion: "host-v1"}
			c, err := NewDurable(cfg, store)
			require.NoError(t, err)
			require.NoError(t, c.ReserveEstimated(ctx, "one", 3))
			id := c.storageID()
			snap, err := store.Load(ctx, id)
			require.NoError(t, err)
			var data map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(snap.Data, &data))
			if zero {
				data["Entries"] = json.RawMessage(`{"one":{"Estimate":0,"Evidence":{}}}`)
			} else {
				data["Entries"] = json.RawMessage(`{"one":{"Evidence":{}}}`)
			}
			snap.Data, err = json.Marshal(data)
			require.NoError(t, err)
			unlock, err := store.Lock(ctx, id)
			require.NoError(t, err)
			require.NoError(t, store.Save(ctx, id, snap))
			require.NoError(t, unlock())
			_, err = NewDurable(cfg, store)
			require.Error(t, err, "损坏估计不能被视为零预留")
		})
	}
}
