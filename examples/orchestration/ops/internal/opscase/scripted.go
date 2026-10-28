package opscase

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/kart-io/goagent/llm"
)

var firstRef = regexp.MustCompile(`"ref":"(E[0-9a-f]+)"`)

// BindRefs 供脚本化模型使用：持久调用的引用号由执行键派生，脚本写不出字面值，
// 改写成 "@<调用 ID>"，这里替换为请求中该调用的工具结果里第一个 ref。找不到时保持原样，交给核验报错。
func BindRefs(r *llm.CompletionRequest, turn llm.StreamChunk) llm.StreamChunk {
	results := map[string]string{}
	for _, m := range r.Messages {
		if m.Role == "tool" {
			results[m.ToolCallID] = m.Content
		}
	}
	calls := make([]llm.ToolCall, len(turn.ToolCalls))
	for i, c := range turn.ToolCalls {
		calls[i] = c
		var args map[string]any
		if json.Unmarshal([]byte(c.Function.Arguments), &args) != nil {
			continue
		}
		evidence, _ := args["evidence"].([]any)
		for _, item := range evidence {
			cite, _ := item.(map[string]any)
			ref, _ := cite["ref"].(string)
			if id, ok := strings.CutPrefix(ref, "@"); ok {
				if m := firstRef.FindStringSubmatch(results[id]); m != nil {
					cite["ref"] = m[1]
				}
			}
		}
		b, _ := json.Marshal(args)
		calls[i].Function.Arguments = string(b)
	}
	turn.ToolCalls = calls
	return turn
}
