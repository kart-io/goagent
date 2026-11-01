package budget

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestAttributionTree(t *testing.T) {
	ctx := context.Background()
	c, e := New(Config{RootKey: "r", Version: "v1", MaxModelRequests: 5, MaxAgentCalls: 3})
	require.NoError(t, e)
	root := usage.Owner{RunID: "r", AgentID: "parent", AgentCallKey: "r"}
	for i, x := range []struct {
		id string
		n  int
	}{{"r", 9}, {"a", 4}, {"b", 5}} {
		chain := []usage.Owner{root}
		if i > 0 {
			chain = append(chain, usage.Owner{RunID: "r", AgentID: x.id, AgentCallKey: x.id, ParentAgentCallKey: "r"})
		}
		require.NoError(t, c.ReserveAttributed(ctx, x.id, 0, chain))
		_, e = c.RecordEvidence(ctx, x.id, chain, usage.Evidence{KnownFields: usage.BasicFields, Tokens: interfaces.TokenUsage{TotalTokens: x.n}, Source: "provider"})
		require.NoError(t, e)
	}
	report, e := c.Attribution(ctx)
	require.NoError(t, e)
	require.Equal(t, 18, report.Root.KnownTokens.TotalTokens)
	require.Len(t, report.Calls, 3)
	require.Equal(t, 9, report.Calls[2].Self.KnownTokens.TotalTokens)
	require.Equal(t, 18, report.Calls[2].Inclusive.KnownTokens.TotalTokens)
}

func TestAttributionZeroModelAncestorAndOwnerConflicts(t *testing.T) {
	ctx := context.Background()
	c, e := New(Config{RootKey: "r", Version: "v1", MaxModelRequests: 4, MaxAgentCalls: 3})
	require.NoError(t, e)
	chain := []usage.Owner{{RunID: "r", AgentID: "root", AgentCallKey: "root"}, {RunID: "r", AgentID: "middle", AgentCallKey: "middle", ParentAgentCallKey: "root"}, {RunID: "r", AgentID: "leaf", AgentCallKey: "leaf", ParentAgentCallKey: "middle"}}
	require.NoError(t, c.RegisterAgent(ctx, chain))
	require.NoError(t, c.ReserveAttributed(ctx, "a", 0, chain))
	out, e := c.RecordEvidence(ctx, "a", chain, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 5}, Source: "provider"})
	require.NoError(t, e)
	require.Equal(t, uint64(1), out.Revision)
	again, e := c.RecordEvidence(ctx, "a", chain, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 5}, Source: "saved"})
	require.NoError(t, e)
	require.Equal(t, out, again)
	report, e := c.Attribution(ctx)
	require.NoError(t, e)
	require.Equal(t, 1, report.PartialUsageEntries)
	require.False(t, report.Complete)
	require.Zero(t, report.Calls[1].Self.ReservedRequests)
	require.Equal(t, 5, report.Calls[1].Inclusive.KnownTokens.TotalTokens)
	report.Calls[1].AgentID = "mutation"
	againReport, e := c.Attribution(ctx)
	require.NoError(t, e)
	require.Equal(t, "middle", againReport.Calls[1].AgentID)
	full, e := c.RecordEvidence(ctx, "a", chain, usage.Evidence{KnownFields: usage.BasicFields, Tokens: interfaces.TokenUsage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}, Source: "saved"})
	require.NoError(t, e)
	require.Equal(t, uint64(2), full.Revision)
	require.Equal(t, "provider", full.Source)
	_, e = c.RecordEvidence(ctx, "a", chain, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 6}})
	require.Error(t, e)
	wrong := append([]usage.Owner(nil), chain...)
	wrong[2].AgentID = "other"
	_, e = c.RecordEvidence(ctx, "a", wrong, full)
	require.Error(t, e)
	require.Error(t, c.ReserveAttributed(ctx, "extra", 0, append(chain, usage.Owner{RunID: "r", AgentID: "extra", AgentCallKey: "extra", ParentAgentCallKey: "leaf"})))
	snap, e := c.Snapshot(ctx)
	require.NoError(t, e)
	require.Equal(t, 1, snap.ReservedRequests)
}

func TestAttributionConfigAndDurableVersionIsolation(t *testing.T) {
	ctx := context.Background()
	store, e := durable.NewFileStore(t.TempDir())
	require.NoError(t, e)
	cfg := Config{RootKey: "r", Version: "v1", MaxModelRequests: 3}
	_, e = NewDurable(cfg, store)
	require.NoError(t, e)
	cfg.MaxAgentCalls = 3
	_, e = NewDurable(cfg, store)
	require.Error(t, e)
	store, e = durable.NewFileStore(t.TempDir())
	require.NoError(t, e)
	c, e := NewDurable(cfg, store)
	require.NoError(t, e)
	chain := []usage.Owner{{RunID: "r", AgentID: "root", AgentCallKey: "r"}}
	require.NoError(t, c.ReserveAttributed(ctx, "a", 0, chain))
	_, e = c.RecordEvidence(ctx, "a", chain, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 7}, Source: "provider"})
	require.NoError(t, e)
	resumed, e := NewDurable(cfg, store)
	require.NoError(t, e)
	report, e := resumed.Attribution(ctx)
	require.NoError(t, e)
	require.Equal(t, 7, report.Root.KnownTokens.TotalTokens)
	require.Equal(t, "root", report.Calls[0].AgentID)
	cfg.MaxAgentCalls = 0
	_, e = NewDurable(cfg, store)
	require.Error(t, e)
}

func TestAttributionConcurrentAdmissionAndOverflow(t *testing.T) {
	ctx := context.Background()
	c, e := New(Config{RootKey: "r", Version: "v1", MaxModelRequests: 2, MaxAgentCalls: 1})
	require.NoError(t, e)
	chain := []usage.Owner{{RunID: "r", AgentID: "root", AgentCallKey: "r"}}
	ch := make(chan error, 8)
	for i := range 8 {
		go func() { ch <- c.ReserveAttributed(ctx, fmt.Sprint(i), 0, chain) }()
	}
	success := 0
	for range 8 {
		if <-ch == nil {
			success++
		}
	}
	require.Equal(t, 2, success)
	r, e := c.Attribution(ctx)
	require.NoError(t, e)
	require.Equal(t, 2, r.UnknownUsageEntries)
	for key := range c.memory.Entries {
		_, e = c.RecordEvidence(ctx, key, chain, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: math.MaxInt}})
		require.NoError(t, e)
	}
	_, e = c.Attribution(ctx)
	require.ErrorContains(t, e, "overflow")
	_, e = c.Snapshot(ctx)
	require.ErrorContains(t, e, "overflow")
}

func TestAttributedSaveFailureIsAtomic(t *testing.T) {
	ctx := context.Background()
	store, e := durable.NewFileStore(t.TempDir())
	require.NoError(t, e)
	cfg := Config{RootKey: "r", Version: "v1", MaxModelRequests: 2, MaxAgentCalls: 2}
	c, e := NewDurable(cfg, store)
	require.NoError(t, e)
	chain := []usage.Owner{{RunID: "r", AgentID: "root", AgentCallKey: "r"}}
	c.store = failingStore{store}
	require.Error(t, c.ReserveAttributed(ctx, "a", 0, chain))
	c.store = store
	r, e := c.Attribution(ctx)
	require.NoError(t, e)
	require.Zero(t, r.Root.ReservedRequests)
	require.Empty(t, r.Calls)
	require.NoError(t, c.ReserveAttributed(ctx, "a", 0, chain))
	c.store = failingStore{store}
	_, e = c.RecordEvidence(ctx, "a", chain, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 4}})
	require.Error(t, e)
	c.store = store
	r, e = c.Attribution(ctx)
	require.NoError(t, e)
	require.Equal(t, 1, r.UnknownUsageEntries)
	require.Zero(t, r.Root.KnownTokens.TotalTokens)
}

func TestAttributionFieldUnknownCounts(t *testing.T) {
	ctx := context.Background()
	c, e := New(Config{RootKey: "r", Version: "v1", MaxModelRequests: 3, MaxAgentCalls: 1})
	require.NoError(t, e)
	chain := []usage.Owner{{RunID: "r", AgentID: "root", AgentCallKey: "r"}}
	for _, key := range []string{"unknown", "total", "basic"} {
		require.NoError(t, c.ReserveAttributed(ctx, key, 0, chain))
	}
	_, e = c.RecordEvidence(ctx, "total", chain, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 3}})
	require.NoError(t, e)
	_, e = c.RecordEvidence(ctx, "basic", chain, usage.Evidence{KnownFields: usage.BasicFields, Tokens: interfaces.TokenUsage{TotalTokens: 4}})
	require.NoError(t, e)
	r, e := c.Attribution(ctx)
	require.NoError(t, e)
	s := r.Calls[0].Self
	require.Equal(t, 2, s.UnknownPromptEntries)
	require.Equal(t, 2, s.UnknownCompletionEntries)
	require.Equal(t, 1, s.UnknownTotalEntries)
	require.Equal(t, 3, s.UnknownCachedEntries)
	require.Equal(t, s, r.Calls[0].Inclusive)
	require.Equal(t, s, r.Agents[0].Self)
	require.False(t, r.Complete)
	_, e = c.RecordEvidence(ctx, "unknown", chain, usage.Evidence{KnownFields: usage.BasicFields})
	require.NoError(t, e)
	_, e = c.RecordEvidence(ctx, "total", chain, usage.Evidence{KnownFields: usage.Prompt | usage.Completion})
	require.NoError(t, e)
	r, e = c.Attribution(ctx)
	require.NoError(t, e)
	require.True(t, r.Complete)
	require.Equal(t, 3, r.Calls[0].Self.UnknownCachedEntries)
	require.Zero(t, r.Calls[0].Self.UnknownPromptEntries)
}

func TestUnattributedCompleteUsageFieldCounts(t *testing.T) {
	ctx := context.Background()
	c, e := New(Config{RootKey: "legacy", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, e)
	require.NoError(t, c.Reserve(ctx, "unknown"))
	require.NoError(t, c.Reserve(ctx, "known"))
	require.NoError(t, c.RecordUsage(ctx, "known", &interfaces.TokenUsage{TotalTokens: 7}))
	r, e := c.Attribution(ctx)
	require.NoError(t, e)
	require.Empty(t, r.Calls)
	require.False(t, r.Complete)
	require.Equal(t, 7, r.Root.KnownTokens.TotalTokens)
	require.Equal(t, 2, r.Unattributed.ReservedRequests)
	require.Equal(t, 1, r.Unattributed.UnknownPromptEntries)
	require.Equal(t, 1, r.Unattributed.UnknownCompletionEntries)
	require.Equal(t, 1, r.Unattributed.UnknownTotalEntries)
	require.Equal(t, 2, r.Unattributed.UnknownCachedEntries)
}

func TestPartialUnknownTotalDoesNotClaimKnownDelta(t *testing.T) {
	ctx := context.Background()
	c, e := New(Config{RootKey: "r", Version: "v1", MaxModelRequests: 2, MaxReservedTokens: 200, TokenPolicyVersion: "est-v1", MaxAgentCalls: 1})
	require.NoError(t, e)
	chain := []usage.Owner{{RunID: "r", AgentID: "root", AgentCallKey: "r"}}
	require.NoError(t, c.ReserveAttributed(ctx, "a", 100, chain))
	_, e = c.RecordEvidence(ctx, "a", chain, usage.Evidence{KnownFields: usage.Prompt, Tokens: interfaces.TokenUsage{PromptTokens: 10}})
	require.NoError(t, e)
	snap, e := c.Snapshot(ctx)
	require.NoError(t, e)
	assert.Zero(t, snap.KnownTokenDelta, "unknown total cannot establish token delta")
	report, e := c.Attribution(ctx)
	require.NoError(t, e)
	assert.Zero(t, report.Root.KnownTokenDelta, "unknown total cannot establish token delta")
	_, e = c.RecordEvidence(ctx, "a", chain, usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 25}})
	require.NoError(t, e)
	snap, e = c.Snapshot(ctx)
	require.NoError(t, e)
	require.Equal(t, -75, snap.KnownTokenDelta)
	report, e = c.Attribution(ctx)
	require.NoError(t, e)
	require.Equal(t, -75, report.Root.KnownTokenDelta)
}
