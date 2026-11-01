package providers

import (
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/sashabaranov/go-openai"
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
	if next.Fields()&usage.Reasoning != 0 {
		result.ReasoningTokens = next.ReasoningTokens
	}
	if next.Fields()&usage.ToolUsePrompt != 0 {
		result.ToolUsePromptTokens = next.ToolUsePromptTokens
	}
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

// sdkUsageDetails retains only nonzero scalar evidence: SDK scalar fields do
// not distinguish omitted fields from an explicitly reported zero.
func sdkUsageDetails(u *agentllm.Usage, reasoning, toolInput int) *agentllm.Usage {
	fields := u.Fields()
	u.ReasoningTokens, u.ToolUsePromptTokens = reasoning, toolInput
	if reasoning != 0 {
		fields |= usage.Reasoning
	}
	if toolInput != 0 {
		fields |= usage.ToolUsePrompt
	}
	if fields != usage.BasicFields {
		u.KnownFields = &fields
	}
	return u
}

func openAIUsage(u *openai.Usage) *agentllm.Usage {
	if u == nil {
		return nil
	}
	cached, reasoning := 0, 0
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	if u.CompletionTokensDetails != nil {
		reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	return sdkUsageDetails(sdkUsage(u.PromptTokens, u.CompletionTokens, u.TotalTokens, cached), reasoning, 0)
}

func tokenValue(u *agentllm.Usage) *interfaces.TokenUsage {
	if u == nil {
		return nil
	}
	v := u.Tokens()
	return &v
}
