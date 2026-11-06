package llm

import (
	"testing"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core/usage"
	"github.com/stretchr/testify/require"
)

func TestUsageFieldCompatibility(t *testing.T) {
	old := &Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 9}
	require.Equal(t, usage.BasicFields, old.Fields())
	data, err := json.Marshal(old)
	require.NoError(t, err)
	require.JSONEq(t, `{"prompt_tokens":2,"completion_tokens":3,"total_tokens":9}`, string(data))
	fields := usage.Total | usage.Cached
	u := &Usage{TotalTokens: 9, CachedTokens: 2, KnownFields: &fields}
	require.Equal(t, fields, u.Fields())
	require.Equal(t, 2, u.Tokens().CachedTokens)
	require.Equal(t, 9, u.Tokens().TotalTokens)
	var missing *Usage
	require.Zero(t, missing.Fields())
	require.Zero(t, missing.Tokens().TotalTokens)
}
