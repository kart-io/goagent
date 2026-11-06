package llm

import "github.com/kart-io/goagent/codec/json"

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

// ArgumentsObject 按默认规则解析 Arguments，只接受 JSON 对象（null 不算），数字为 float64。
func (c ToolCall) ArgumentsObject() (map[string]any, bool) {
	var args map[string]any
	if json.Unmarshal([]byte(c.Function.Arguments), &args) != nil || args == nil {
		return nil, false
	}
	return args, true
}
