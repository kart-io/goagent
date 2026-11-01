package budget

import (
	"context"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"testing"
)

func TestDetailAggregation(t *testing.T) {
	c, err := New(Config{RootKey: "details", Version: "policy", MaxModelRequests: 2})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, k := range []string{"a", "b"} {
		if err = c.Reserve(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	_, err = c.RecordEvidence(ctx, "a", nil, usage.Evidence{KnownFields: usage.Total | usage.Reasoning | usage.ToolUsePrompt, Tokens: interfaces.TokenUsage{TotalTokens: 20, ReasoningTokens: 7, ToolUsePromptTokens: 3}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Attribution(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Root.KnownTokens.TotalTokens != 20 || r.Root.KnownTokens.ReasoningTokens != 7 || r.Root.KnownTokens.ToolUsePromptTokens != 3 {
		t.Fatalf("bad root: %+v", r.Root)
	}
	if r.Unattributed.UnknownReasoningEntries != 1 || r.Unattributed.UnknownToolUsePromptEntries != 1 {
		t.Fatalf("missing unknowns: %+v", r.Unattributed)
	}
}
