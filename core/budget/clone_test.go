package budget

import (
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLedgerCloneIsolatesAllMutableEvidence(t *testing.T) {
	original := ledger{Config: Config{RootKey: "root"}, Entries: map[string]entry{"known": {Estimate: 5, Evidence: usage.Evidence{KnownFields: usage.Total | usage.Cached, Tokens: interfaces.TokenUsage{TotalTokens: 3, CachedTokens: 1}}}, "unknown": {}}, AgentCalls: map[string]usage.Owner{"r": {AgentID: "root"}}, Tools: map[string]bool{"tool": true}}
	copied := clone(original)
	item := copied.Entries["known"]
	item.Evidence.Tokens.TotalTokens = 100
	item.Estimate = 1
	copied.Entries["known"] = item
	delete(copied.Entries, "unknown")
	delete(copied.Tools, "tool")
	delete(copied.AgentCalls, "r")
	require.Equal(t, 3, original.Entries["known"].Evidence.Tokens.TotalTokens)
	require.Equal(t, 5, original.Entries["known"].Estimate)
	require.Contains(t, original.Entries, "unknown")
	require.Contains(t, original.AgentCalls, "r")
	require.True(t, original.Tools["tool"])
	require.Nil(t, clone(ledger{}).Entries)
}
