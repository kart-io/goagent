package providers

import "github.com/kart-io/goagent/interfaces"

// Default Values
const (
	// DefaultTemperature is the default temperature value
	DefaultTemperature = 0.7
	// DefaultMaxTokens is the default max tokens value
	DefaultMaxTokens = 2048
	// DefaultTopP is the default top_p value
	DefaultTopP = 1.0
	// DefaultFrequencyPenalty is the default frequency penalty
	DefaultFrequencyPenalty = 0.0
	// DefaultPresencePenalty is the default presence penalty
	DefaultPresencePenalty = 0.0
)

// ToolCall represents a function/tool call by the LLM
type ToolCall struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type,omitempty"` // "function"
	Name      string                 `json:"name,omitempty"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Function  *struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function,omitempty"`
}

// ToolCallResponse represents the response from tool-enabled completion
type ToolCallResponse struct {
	Content   string                 `json:"content"`
	ToolCalls []ToolCall             `json:"tool_calls,omitempty"`
	Usage     *interfaces.TokenUsage `json:"usage,omitempty"`
}

// ToolChunk represents a streaming chunk from tool-enabled completion
type ToolChunk struct {
	Type  string      `json:"type"`  // "content", "tool_call", "tool_name", "tool_args", "error"
	Value interface{} `json:"value"` // Content string, ToolCall, or error
}
