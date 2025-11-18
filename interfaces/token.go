package interfaces

// TokenUsage represents detailed token usage statistics for LLM calls.
//
// TokenUsage tracks the number of tokens consumed by LLM API calls:
//   - PromptTokens: Tokens in the input/prompt
//   - CompletionTokens: Tokens in the generated output
//   - TotalTokens: Total tokens used (PromptTokens + CompletionTokens)
//   - CachedTokens: Tokens served from cache (if applicable)
//
// This information is useful for:
//   - Cost tracking and budgeting
//   - Performance monitoring
//   - Usage analytics
//   - Rate limit management
type TokenUsage struct {
	// PromptTokens is the number of tokens in the input/prompt.
	PromptTokens int `json:"prompt_tokens"`

	// CompletionTokens is the number of tokens in the generated output.
	CompletionTokens int `json:"completion_tokens"`

	// TotalTokens is the total number of tokens used.
	// Usually equals PromptTokens + CompletionTokens.
	TotalTokens int `json:"total_tokens"`

	// CachedTokens is the number of tokens served from cache (if applicable).
	// Not all LLM providers support this field.
	CachedTokens int `json:"cached_tokens,omitempty"`
}

// Add adds another TokenUsage to this one, summing all fields.
// This is useful for accumulating token usage across multiple LLM calls.
func (t *TokenUsage) Add(other *TokenUsage) {
	if other == nil {
		return
	}
	t.PromptTokens += other.PromptTokens
	t.CompletionTokens += other.CompletionTokens
	t.TotalTokens += other.TotalTokens
	t.CachedTokens += other.CachedTokens
}

// IsEmpty returns true if no tokens were used.
func (t *TokenUsage) IsEmpty() bool {
	return t.TotalTokens == 0 && t.PromptTokens == 0 && t.CompletionTokens == 0
}
