package providers

import (
	"github.com/kart-io/goagent/core/usage"
	agentllm "github.com/kart-io/goagent/llm"
)

// sdkUsage preserves the reported total, including provider-specific reasoning
// accounting. These SDKs expose scalar counts and erase JSON field presence.
// A nonzero count is evidence; zero remains unknown, even when the provider
// explicitly sent zero. In particular an empty usage object never means free.
func sdkUsage(prompt, completion, total, cached int) *agentllm.Usage {
	fields := usage.Fields(0)
	if prompt != 0 {
		fields |= usage.Prompt
	}
	if completion != 0 {
		fields |= usage.Completion
	}
	if total != 0 {
		fields |= usage.Total
	}
	if cached != 0 {
		fields |= usage.Cached
	}
	u := &agentllm.Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total, CachedTokens: cached}
	// Preserve legacy fixture and serialization shape when all basic fields are
	// evidenced and no cached field is available.
	if fields != usage.BasicFields {
		u.KnownFields = &fields
	}
	return u
}

// mergeSDKUsage retains prior field evidence when a later cumulative usage
// frame omits a field. Reported counts replace earlier counts; they never add.
func mergeSDKUsage(previous, next *agentllm.Usage) *agentllm.Usage {
	if previous == nil {
		return next
	}
	if next == nil {
		return previous
	}
	result := *previous
	fields := previous.Fields() | next.Fields()
	if next.Fields()&usage.Prompt != 0 {
		result.PromptTokens = next.PromptTokens
	}
	if next.Fields()&usage.Completion != 0 {
		result.CompletionTokens = next.CompletionTokens
	}
	if next.Fields()&usage.Total != 0 {
		result.TotalTokens = next.TotalTokens
	}
	if next.Fields()&usage.Cached != 0 {
		result.CachedTokens = next.CachedTokens
	}
	result.KnownFields = nil
	if fields != usage.BasicFields {
		result.KnownFields = &fields
	}
	return &result
}
