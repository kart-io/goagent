package llm

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
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}
