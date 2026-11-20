package contextpolicy

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

func TestOffloadZeroPageReadsWhole(t *testing.T) {
	ctx := WithResultScope(context.Background(), "exec")
	p, err := NewOffload(OffloadConfig{Store: NewMemoryStore(), MaxToolResultRunes: 10})
	require.NoError(t, err)
	_, err = p.Reduce(ctx, []llm.Message{toolRound("c1", "fetch"), {Role: "tool", ToolCallID: "c1", Content: strings.Repeat("x", 5000)}}, false)
	require.NoError(t, err)
	out, err := p.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"key": "r1"}})
	require.NoError(t, err)
	require.Contains(t, out.Result, `"done":true`, "旧默认每页 1024 字")
	require.Contains(t, out.Result, strings.Repeat("x", 5000))
	_, err = NewOffload(OffloadConfig{Store: NewMemoryStore(), MaxToolResultRunes: 10, ReadPageRunes: 1 << 20})
	require.NoError(t, err, "不再有 16384 的分页上限")
}

func TestRecallConfigLimits(t *testing.T) {
	store := session.NewMemoryStore()
	for _, tc := range []struct {
		name    string
		cfg     RecallConfig
		invalid bool
		schema  string
	}{
		{"不限", RecallConfig{}, false, `"limit":{"type":"integer","minimum":1}`},
		{"显式上限", RecallConfig{MaxLimit: 7, MaxRunes: 10}, false, `"maximum":7`},
		{"负数", RecallConfig{MaxRunes: -1}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewRecallToolConfig(store, tc.cfg)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Contains(t, r.ArgsSchema(), tc.schema)
		})
	}
	old, err := NewRecallTool(store)
	require.NoError(t, err)
	require.Contains(t, old.ArgsSchema(), `"maximum":100`, "NewRecallTool 保留原上限")
}
