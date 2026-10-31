package budget

import (
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestLedgerCloneIsolatesAllMutableEvidence(t *testing.T) {
	original := ledger{Config: Config{RootKey: "root", Version: "v1", MaxModelRequests: 3}, Entries: map[string]*interfaces.TokenUsage{"known": {TotalTokens: 3, CachedTokens: 1}, "unknown": nil}, Estimates: map[string]int{"known": 5}, Tools: map[string]bool{"tool": true}}
	copied := clone(original)
	copied.Config.RootKey = "other"
	copied.Entries["known"].TotalTokens = 100
	copied.Entries["known"].CachedTokens = 99
	delete(copied.Entries, "unknown")
	copied.Estimates["known"] = 1
	delete(copied.Tools, "tool")
	require.Equal(t, "root", original.Config.RootKey)
	require.Equal(t, 3, original.Entries["known"].TotalTokens)
	require.Equal(t, 1, original.Entries["known"].CachedTokens)
	require.Contains(t, original.Entries, "unknown")
	require.Nil(t, original.Entries["unknown"])
	require.Equal(t, 5, original.Estimates["known"])
	require.True(t, original.Tools["tool"])
	empty := clone(ledger{})
	require.Nil(t, empty.Entries)
	require.Nil(t, empty.Estimates)
	require.Nil(t, empty.Tools)
}
