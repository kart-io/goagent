package providers

import "github.com/kart-io/goagent/llm/common"

// Default Values - aliased for backward compatibility
const (
	// DefaultTemperature is the default temperature value
	// Deprecated: Use common.DefaultTemperature directly.
	DefaultTemperature = common.DefaultTemperature
	// DefaultMaxTokens is the default max tokens value
	// Deprecated: Use common.DefaultMaxTokens directly.
	DefaultMaxTokens = common.DefaultMaxTokens
	// DefaultTopP is the default top_p value
	// Deprecated: Use common.DefaultTopP directly.
	DefaultTopP = common.DefaultTopP
	// DefaultFrequencyPenalty is the default frequency penalty
	// Deprecated: Use common.DefaultFrequencyPenalty directly.
	DefaultFrequencyPenalty = common.DefaultFrequencyPenalty
	// DefaultPresencePenalty is the default presence penalty
	// Deprecated: Use common.DefaultPresencePenalty directly.
	DefaultPresencePenalty = common.DefaultPresencePenalty
)

// ToolCall is now an alias to common.ToolCall for backward compatibility.
// Deprecated: Use common.ToolCall directly.
type ToolCall = common.ToolCall

// ToolCallResponse is now an alias to common.ToolCallResponse for backward compatibility.
// Deprecated: Use common.ToolCallResponse directly.
type ToolCallResponse = common.ToolCallResponse

// ToolChunk is now an alias to common.ToolChunk for backward compatibility.
// Deprecated: Use common.ToolChunk directly.
type ToolChunk = common.ToolChunk
