package providers

import (
	"context"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

// DeepSeek 偶尔不走 tool_calls，而把调用以 DSML 标记写进正文，工具因此不执行、模型却声称已完成。
// 这里把这类正文还原为工具调用：正文出现 dsmlOpen 起先暂存增量，终态时整段是合法调用块且
// 工具名都在本轮声明内才还原，否则原样补发。
const dsmlOpen = "<｜"

var (
	dsmlTag    = `｜+\s*DSML\s*｜+\s*`
	dsmlBlock  = regexp.MustCompile(`(?s)^<` + dsmlTag + `(?:function_)?calls>(.*)</` + dsmlTag + `(?:function_)?calls>\s*$`)
	dsmlInvoke = regexp.MustCompile(`(?s)<` + dsmlTag + `invoke\s+name="([^"]+)"\s*>(.*?)</` + dsmlTag + `invoke>`)
	dsmlParam  = regexp.MustCompile(`(?s)<` + dsmlTag + `parameter\s+name="([^"]+)"(?:\s+string="(true|false)")?\s*>(.*?)</` + dsmlTag + `parameter>`)
)

// recoverDSMLToolCalls 包装原生工具流；增量与累积正文只发到 dsmlOpen 之前。
func recoverDSMLToolCalls(ctx context.Context, in <-chan *agentllm.StreamChunk, tools []interfaces.Tool) <-chan *agentllm.StreamChunk {
	out := make(chan *agentllm.StreamChunk)
	go func() {
		defer close(out)
		sent := 0
		for chunk := range in {
			if chunk == nil {
				continue
			}
			c := *chunk
			end := len(c.Content)
			if !c.Done {
				end = dsmlSafeLen(c.Content)
			} else if c.Error == nil && c.FinishReason == "stop" && len(c.ToolCalls) == 0 {
				if start := strings.Index(c.Content, dsmlOpen); start >= 0 {
					if calls, ok := parseDSMLCalls(c.Content[start:], tools); ok {
						end, c.ToolCalls, c.FinishReason = start, calls, "tool_calls"
					}
				}
			}
			end = max(end, sent)
			c.Delta, c.Content, sent = c.Content[sent:end], c.Content[:end], end
			select {
			case out <- &c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// dsmlSafeLen 返回可以立即下发的正文长度；末尾单独的 "<" 可能是标记的开头，也先暂存。
func dsmlSafeLen(content string) int {
	if i := strings.Index(content, dsmlOpen); i >= 0 {
		return i
	}
	return len(strings.TrimSuffix(content, "<"))
}

func parseDSMLCalls(text string, tools []interfaces.Tool) ([]agentllm.ToolCall, bool) {
	block := dsmlBlock.FindStringSubmatch(text)
	if block == nil {
		return nil, false
	}
	declared := make(map[string]bool, len(tools))
	for _, tool := range tools {
		declared[tool.Name()] = true
	}
	invokes := dsmlInvoke.FindAllStringSubmatch(block[1], -1)
	if len(invokes) == 0 || strings.TrimSpace(dsmlInvoke.ReplaceAllString(block[1], "")) != "" {
		return nil, false
	}
	calls := make([]agentllm.ToolCall, 0, len(invokes))
	for _, invoke := range invokes {
		if !declared[invoke[1]] || strings.TrimSpace(dsmlParam.ReplaceAllString(invoke[2], "")) != "" {
			return nil, false
		}
		args := map[string]any{}
		for _, param := range dsmlParam.FindAllStringSubmatch(invoke[2], -1) {
			if param[2] == "true" {
				args[param[1]] = param[3]
				continue
			}
			var value any
			if json.Unmarshal([]byte(param[3]), &value) != nil {
				return nil, false
			}
			args[param[1]] = value
		}
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, false
		}
		call := agentllm.ToolCall{ID: "call_dsml_" + uuid.NewString(), Type: "function"}
		call.Function.Name, call.Function.Arguments = invoke[1], string(raw)
		calls = append(calls, call)
	}
	return calls, true
}
