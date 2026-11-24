package usage

import (
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// Fields is a bit mask of the TokenUsage fields a provider actually reported.
// An unset bit means the field is unknown, not zero.
type Fields uint8

// Field bits, one per interfaces.TokenUsage counter. BasicFields is the
// prompt/completion/total triple; AllFields is every defined bit.
const (
	Prompt Fields = 1 << iota
	Completion
	Total
	Cached
	Reasoning
	ToolUsePrompt
	BasicFields = Prompt | Completion | Total
	AllFields   = BasicFields | Cached | Reasoning | ToolUsePrompt
)

// Evidence is the token usage reported for one model attempt. KnownFields
// marks which Tokens counters were reported; Source names the origin (for
// example "provider") and Revision increments each time Merge adds fields.
type Evidence struct {
	Tokens      interfaces.TokenUsage
	KnownFields Fields
	Source      string
	Revision    uint64
}

// ModelEvidence is the usage notification for one model attempt: its owner,
// operation and attempt keys, provider and model, reasoning settings and
// Evidence. Replayed marks delivery from a durable record; AccountingCommitted
// reports whether the evidence was recorded in the budget ledger.
type ModelEvidence struct {
	ResponseModel       string          `json:",omitempty"`
	Reasoning           ReasoningConfig `json:",omitzero"`
	ReasoningConfigured bool            `json:",omitzero"`
	Owner
	OperationKey, AttemptKey, Provider, Model string
	Attempt                                   int
	Evidence
	Replayed, AccountingCommitted bool
}

// Validate rejects invalid masks, negative tokens, and nonzero unreported fields.
func (e Evidence) Validate() error {
	if e.KnownFields & ^AllFields != 0 || e.Tokens.PromptTokens < 0 || e.Tokens.CompletionTokens < 0 || e.Tokens.TotalTokens < 0 || e.Tokens.CachedTokens < 0 || e.Tokens.ReasoningTokens < 0 || e.Tokens.ToolUsePromptTokens < 0 {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "invalid usage evidence").WithComponent("usage").WithOperation("validate")
	}
	for i, n := range []int{e.Tokens.PromptTokens, e.Tokens.CompletionTokens, e.Tokens.TotalTokens, e.Tokens.CachedTokens, e.Tokens.ReasoningTokens, e.Tokens.ToolUsePromptTokens} {
		if e.KnownFields&Fields(1<<i) == 0 && n != 0 {
			return agentErrors.Newf(agentErrors.CodeInvalidInput, "nonzero unreported usage field").WithComponent("usage").WithOperation("validate")
		}
	}
	if len(e.Source) > 256 {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "usage source too long").WithComponent("usage").WithOperation("validate")
	}
	return nil
}

// Merge adds only newly known fields. Duplicate delivery preserves source and revision.
func Merge(old, next Evidence) (Evidence, error) {
	if err := old.Validate(); err != nil {
		return Evidence{}, err
	}
	if err := next.Validate(); err != nil {
		return Evidence{}, err
	}
	out := old
	// saved describes delivery, never the original evidence source.
	if out.Source == "" && next.Source != "saved" {
		out.Source = next.Source
	}
	dst := []*int{&out.Tokens.PromptTokens, &out.Tokens.CompletionTokens, &out.Tokens.TotalTokens, &out.Tokens.CachedTokens, &out.Tokens.ReasoningTokens, &out.Tokens.ToolUsePromptTokens}
	src := []int{next.Tokens.PromptTokens, next.Tokens.CompletionTokens, next.Tokens.TotalTokens, next.Tokens.CachedTokens, next.Tokens.ReasoningTokens, next.Tokens.ToolUsePromptTokens}
	for i, p := range dst {
		f := Fields(1 << i)
		if next.KnownFields&f == 0 {
			continue
		}
		if old.KnownFields&f != 0 && *p != src[i] {
			return Evidence{}, agentErrors.Newf(agentErrors.CodeConflict, "model usage evidence changed").WithComponent("usage").WithOperation("merge")
		}
		*p = src[i]
	}
	if next.KnownFields & ^old.KnownFields != 0 {
		if out.Revision == ^uint64(0) {
			return Evidence{}, agentErrors.Newf(agentErrors.CodeStateValidation, "usage revision overflow").WithComponent("usage").WithOperation("merge")
		}
		out.Revision++
		out.KnownFields |= next.KnownFields
	}
	return out, nil
}
