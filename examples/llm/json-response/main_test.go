package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfflineJSONResponse(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), nil, &out))
	without, with, ok := strings.Cut(out.String(), "with WithJSONResponse\n")
	require.True(t, ok, out.String())
	require.Contains(t, without, "parse: failed")
	require.Equal(t, "  reply: {\"city\":\"Paris\",\"temp_c\":18}\n  parse: ok city=Paris temp_c=18\n", with)
}

func TestLiveRequiresCredential(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	require.ErrorContains(t, run(context.Background(), []string{"-live"}, &bytes.Buffer{}), "DEEPSEEK_API_KEY")
	require.ErrorContains(t, run(context.Background(), []string{"-live", "-provider", "anthropic"}, &bytes.Buffer{}), "unknown provider")
}
