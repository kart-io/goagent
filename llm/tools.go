package llm

import (
	"github.com/kart-io/goagent/codec/json"
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

// ArgumentsObject parses Arguments with the default rules, accepting only a
// JSON object (null does not count); numbers are float64.
func (c ToolCall) ArgumentsObject() (map[string]any, bool) {
	var args map[string]any
	if json.Unmarshal([]byte(c.Function.Arguments), &args) != nil || args == nil {
		return nil, false
	}
	return args, true
}
