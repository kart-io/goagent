package budget

import (
	"context"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUnifiedDefaultEvidenceAndUnattributed(t *testing.T) {
	ctx := context.Background()
	c, e := New(Config{RootKey: "r", Version: "policy", MaxModelRequests: 3})
	require.NoError(t, e)
	require.Equal(t, 1000, c.Configuration().MaxAgentCalls)
	require.NoError(t, c.ReserveEstimated(ctx, "a", 100))
	_, e = c.RecordEvidence(ctx, "a", nil, usage.Evidence{KnownFields: usage.Prompt, Tokens: interfaces.TokenUsage{PromptTokens: 10}})
	require.NoError(t, e)
	s, e := c.Snapshot(ctx)
	require.NoError(t, e)
	require.Zero(t, s.KnownTokenDelta)
	r, e := c.Attribution(ctx)
	require.NoError(t, e)
	require.Equal(t, s, r.Root)
	require.Equal(t, 1, r.Unattributed.UnknownTotalEntries)
	require.False(t, r.Complete)
	require.NoError(t, c.Reserve(ctx, "b"))
	require.NoError(t, c.RecordUsage(ctx, "b", &interfaces.TokenUsage{TotalTokens: 5, CachedTokens: 2}))
	r, e = c.Attribution(ctx)
	require.NoError(t, e)
	require.Equal(t, 1, r.Unattributed.UnknownCachedEntries)
}

func TestUnifiedOwnerCannotBeRemovedOrAddedOnReplay(t *testing.T) {
	ctx := context.Background()
	c, e := New(Config{RootKey: "r", Version: "policy", MaxModelRequests: 2})
	require.NoError(t, e)
	chain := []usage.Owner{{RunID: "r", AgentID: "root", AgentCallKey: "r"}}
	require.Error(t, c.RegisterAgent(ctx, nil))
	require.NoError(t, c.ReserveAttributed(ctx, "owned", 0, chain))
	require.NoError(t, c.Reserve(ctx, "unowned"))
	partial := usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 5}, Source: "provider"}
	_, e = c.RecordEvidence(ctx, "owned", nil, partial)
	require.Error(t, e)
	require.Error(t, c.RecordUsage(ctx, "owned", &partial.Tokens))
	_, e = c.RecordEvidence(ctx, "unowned", chain, partial)
	require.Error(t, e)
	committed, e := c.RecordEvidence(ctx, "owned", chain, partial)
	require.NoError(t, e)
	again, e := c.RecordEvidence(ctx, "owned", chain, partial)
	require.NoError(t, e)
	require.Equal(t, committed, again)
	wrong := append([]usage.Owner(nil), chain...)
	wrong[0].AgentID = "changed"
	_, e = c.RecordEvidence(ctx, "owned", wrong, partial)
	require.Error(t, e)
	_, e = c.RecordEvidence(ctx, "unowned", nil, partial)
	require.NoError(t, e)
	r, e := c.Attribution(ctx)
	require.NoError(t, e)
	require.Equal(t, r.Calls[0].Self.KnownTokens, r.Unattributed.KnownTokens)
	require.Equal(t, r.Calls[0].Self.UnknownPromptEntries, r.Unattributed.UnknownPromptEntries)
	snap, e := c.Snapshot(ctx)
	require.NoError(t, e)
	require.Equal(t, snap, r.Root)
}

func TestUnifiedAgentCallLimitDefaults(t *testing.T) {
	_, e := New(Config{RootKey: "r", Version: "policy", MaxModelRequests: 1, MaxAgentCalls: -1})
	require.Error(t, e)
	c, e := New(Config{RootKey: "r", Version: "policy", MaxModelRequests: 1, MaxAgentCalls: 1})
	require.NoError(t, e)
	ctx := context.Background()
	root := usage.Owner{RunID: "r", AgentID: "root", AgentCallKey: "r"}
	require.NoError(t, c.RegisterAgent(ctx, []usage.Owner{root}))
	require.NoError(t, c.RegisterAgent(ctx, []usage.Owner{root}))
	child := usage.Owner{RunID: "r", AgentID: "child", AgentCallKey: "c", ParentAgentCallKey: "r"}
	require.Error(t, c.RegisterAgent(ctx, []usage.Owner{root, child}))
	require.NoError(t, c.Reserve(ctx, "unowned"))
	r, e := c.Attribution(ctx)
	require.NoError(t, e)
	require.Len(t, r.Calls, 1)
	require.Equal(t, 1, r.Unattributed.ReservedRequests)
}

func TestConvenienceAPIsForwardContextLineage(t *testing.T) {
	ctx := usage.WithInvocation(context.Background(), usage.Invocation{Owner: usage.Owner{RunID: "r", AgentID: "root", AgentCallKey: "r"}})
	c, e := New(Config{RootKey: "r", Version: "policy", MaxModelRequests: 2})
	require.NoError(t, e)
	require.NoError(t, c.ReserveEstimated(ctx, "a", 10))
	_, e = c.RecordEvidence(ctx, "a", usage.Lineage(ctx), usage.Evidence{KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 3}})
	require.NoError(t, e)
	require.NoError(t, c.Reserve(ctx, "b"))
	require.NoError(t, c.RecordUsage(ctx, "b", &interfaces.TokenUsage{TotalTokens: 4}))
	r, e := c.Attribution(ctx)
	require.NoError(t, e)
	require.Zero(t, r.Unattributed.ReservedRequests)
	require.Len(t, r.Calls, 1)
	require.Equal(t, 7, r.Calls[0].Self.KnownTokens.TotalTokens)
	require.Error(t, c.RecordUsage(context.Background(), "b", &interfaces.TokenUsage{TotalTokens: 4}))
}
