package usage

import (
	"context"
	"github.com/kart-io/goagent/interfaces"
	"testing"
)

func TestLineageCopiesAncestors(t *testing.T) {
	r := Owner{RunID: "r", AgentID: "root", AgentCallKey: "r"}
	ctx := WithInvocation(context.Background(), Invocation{Owner: r})
	c := Owner{RunID: "r", AgentID: "child", AgentCallKey: "c", ParentAgentCallKey: "r"}
	ctx = WithInvocation(ctx, Invocation{Owner: c})
	l := Lineage(ctx)
	if len(l) != 2 || l[0] != r || l[1] != c {
		t.Fatal(l)
	}
	l[0].AgentID = "mutated"
	if Lineage(ctx)[0] != r {
		t.Fatal("alias")
	}
}
func TestEvidenceMerge(t *testing.T) {
	a := Evidence{KnownFields: Total, Source: "provider"}
	a.Tokens.TotalTokens = 5
	b, e := Merge(a, Evidence{KnownFields: Total, Source: "saved", Tokens: a.Tokens})
	if e != nil || b.Source != "provider" || b.Revision != 0 {
		t.Fatal(b, e)
	}
}

func TestEvidencePartialMonotonicAndIndependentFields(t *testing.T) {
	a, e := Merge(Evidence{}, Evidence{KnownFields: Total, Tokens: interfaces.TokenUsage{TotalTokens: 5}, Source: "provider"})
	if e != nil || a.Tokens.PromptTokens != 0 || a.Revision != 1 {
		t.Fatal(a, e)
	}
	b, e := Merge(a, Evidence{KnownFields: Prompt | Completion, Tokens: interfaces.TokenUsage{PromptTokens: 3, CompletionTokens: 2}, Source: "saved"})
	if e != nil || b.Source != "provider" || b.Revision != 2 || b.KnownFields != BasicFields {
		t.Fatal(b, e)
	}
	if _, e = Merge(b, Evidence{KnownFields: Total, Tokens: interfaces.TokenUsage{TotalTokens: 7}}); e == nil {
		t.Fatal("accepted changed field")
	}
	if _, e = Merge(b, Evidence{KnownFields: Fields(128)}); e == nil {
		t.Fatal("accepted invalid mask")
	}
	if _, e = Merge(b, Evidence{Tokens: interfaces.TokenUsage{CachedTokens: -1}}); e == nil {
		t.Fatal("accepted negative usage")
	}
}

func TestEvidenceRejectsUnknownNonzeroAndSavedOrigin(t *testing.T) {
	if e := (Evidence{KnownFields: Total, Tokens: interfaces.TokenUsage{PromptTokens: 999, TotalTokens: 5}}).Validate(); e == nil {
		t.Fatal("unreported nonzero prompt accepted")
	}
	unknown, e := Merge(Evidence{}, Evidence{Source: "provider"})
	if e != nil || unknown.Source != "provider" || unknown.Revision != 0 {
		t.Fatal(unknown, e)
	}
	known, e := Merge(unknown, Evidence{KnownFields: Total, Tokens: interfaces.TokenUsage{TotalTokens: 5}, Source: "saved"})
	if e != nil || known.Source != "provider" || known.Revision != 1 {
		t.Fatal(known, e)
	}
	legacy, e := Merge(Evidence{}, Evidence{KnownFields: Total, Tokens: interfaces.TokenUsage{TotalTokens: 5}, Source: "saved"})
	if e != nil || legacy.Source != "" {
		t.Fatal("saved delivery fabricated origin", legacy, e)
	}
}
