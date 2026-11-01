package usage

import (
	"github.com/kart-io/goagent/interfaces"
	"math"
	"testing"
)

func TestDetailEvidence(t *testing.T) {
	e := Evidence{Tokens: interfaces.TokenUsage{TotalTokens: 20, ReasoningTokens: 7, ToolUsePromptTokens: 3}, KnownFields: Total | Reasoning | ToolUsePrompt}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	merged, err := Merge(Evidence{}, e)
	if err != nil || merged.Tokens != e.Tokens || merged.KnownFields != e.KnownFields {
		t.Fatalf("lost detail: %+v %v", merged, err)
	}
	changed := e
	changed.Tokens.ReasoningTokens++
	if _, err = Merge(merged, changed); err == nil {
		t.Fatal("changed evidence accepted")
	}
	invalid := e
	invalid.KnownFields = Total
	if invalid.Validate() == nil {
		t.Fatal("unreported detail accepted")
	}
	invalid = e
	invalid.Tokens.ToolUsePromptTokens = -1
	if invalid.Validate() == nil {
		t.Fatal("negative detail accepted")
	}
	full := merged
	full.Revision = math.MaxUint64
	if _, err = Merge(full, Evidence{KnownFields: Cached}); err == nil {
		t.Fatal("revision overflow accepted")
	}
}
