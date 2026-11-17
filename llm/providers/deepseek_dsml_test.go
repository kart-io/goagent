package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

// 线上 deepseek 实测泄漏的原样文本（含全角竖线），工具未执行而模型回复“已记住”。
const leakedSave = "\n\n<｜｜DSML｜｜ calls>\n<｜｜DSML｜｜ invoke name=\"memory_save\">\n" +
	"<｜｜DSML｜｜ parameter name=\"content\" string=\"true\">客户只用邮件联系</｜｜DSML｜｜ parameter>\n" +
	"<｜｜DSML｜｜ parameter name=\"pinned\" string=\"false\">true</｜｜DSML｜｜ parameter>\n" +
	"</｜｜DSML｜｜ invoke>\n</｜｜DSML｜｜ calls>"

// deltaFrames 把正文按给定字符位置拆成多个 SSE 增量帧，最后一帧带 finish_reason。
func deltaFrames(content, finish string, cuts ...int) []json.RawMessage {
	runes := []rune(content)
	parts := []string{}
	prev := 0
	for _, c := range cuts {
		parts = append(parts, string(runes[prev:c]))
		prev = c
	}
	parts = append(parts, string(runes[prev:]))
	frames := make([]json.RawMessage, 0, len(parts))
	for i, part := range parts {
		delta, _ := json.Marshal(map[string]any{"content": part})
		f := `{"choices":[{"delta":` + string(delta)
		if i == len(parts)-1 {
			f += `,"finish_reason":"` + finish + `"`
		}
		frames = append(frames, json.RawMessage(f+`}]}`))
	}
	return frames
}

func TestDeepSeekRecoversDSMLToolCalls(t *testing.T) {
	memoryTools := []interfaces.Tool{NewMockTool("memory_save", ""), NewMockTool("memory_search", "")}
	two := "<｜｜DSML｜｜ calls>" +
		"<｜｜DSML｜｜ invoke name=\"memory_search\"><｜｜DSML｜｜ parameter name=\"query\" string=\"true\">联系方式</｜｜DSML｜｜ parameter></｜｜DSML｜｜ invoke>" +
		"<｜｜DSML｜｜ invoke name=\"memory_save\"><｜｜DSML｜｜ parameter name=\"content\" string=\"true\">a\"b</｜｜DSML｜｜ parameter>" +
		"<｜｜DSML｜｜ parameter name=\"importance\" string=\"false\">0.9</｜｜DSML｜｜ parameter></｜｜DSML｜｜ invoke>" +
		"</｜｜DSML｜｜ calls>"
	native := []json.RawMessage{
		json.RawMessage(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"n1","type":"function","function":{"name":"memory_save","arguments":"{\"content\":\"x\"}"}}]}}]}`),
		json.RawMessage(`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`),
	}
	tests := []struct {
		name      string
		frames    []json.RawMessage
		tools     []interfaces.Tool
		wantText  string           // 下游拼接增量得到的正文，也应等于终态 Content
		wantCalls []map[string]any // 名称 -> 参数，按顺序
		wantNames []string
		wantStop  string
	}{
		{
			name:      "泄漏的单个调用还原为工具调用，DSML 不出现在增量里",
			frames:    deltaFrames(leakedSave, "stop", 1, 3, 9, 60),
			tools:     memoryTools,
			wantText:  "\n\n",
			wantNames: []string{"memory_save"},
			wantCalls: []map[string]any{{"content": "客户只用邮件联系", "pinned": true}},
			wantStop:  "tool_calls",
		},
		{
			name:      "调用前的说明文字照常输出，多个调用按顺序还原",
			frames:    deltaFrames("好的，我查一下。"+two+"\n", "stop", 3, 9),
			tools:     memoryTools,
			wantText:  "好的，我查一下。",
			wantNames: []string{"memory_search", "memory_save"},
			wantCalls: []map[string]any{{"query": "联系方式"}, {"content": "a\"b", "importance": 0.9}},
			wantStop:  "tool_calls",
		},
		{
			name:     "未声明的工具名不还原，正文原样补发",
			frames:   deltaFrames(strings.ReplaceAll(leakedSave, "memory_save", "shell_exec"), "stop", 5),
			tools:    memoryTools,
			wantText: strings.ReplaceAll(leakedSave, "memory_save", "shell_exec"),
			wantStop: "stop",
		},
		{
			name:     "缺少结束标签不还原",
			frames:   deltaFrames(strings.TrimSuffix(leakedSave, "</｜｜DSML｜｜ calls>"), "stop"),
			tools:    memoryTools,
			wantText: strings.TrimSuffix(leakedSave, "</｜｜DSML｜｜ calls>"),
			wantStop: "stop",
		},
		{
			name:     "调用块之后还有正文不还原",
			frames:   deltaFrames(leakedSave+"已记住。", "stop"),
			tools:    memoryTools,
			wantText: leakedSave + "已记住。",
			wantStop: "stop",
		},
		{
			name:     "非字符串参数不是合法 JSON 不还原",
			frames:   deltaFrames(strings.Replace(leakedSave, ">true<", ">yes<", 1), "stop"),
			tools:    memoryTools,
			wantText: strings.Replace(leakedSave, ">true<", ">yes<", 1),
			wantStop: "stop",
		},
		{
			name:     "本轮没有声明工具时不还原",
			frames:   deltaFrames(leakedSave, "stop"),
			wantText: leakedSave,
			wantStop: "stop",
		},
		{
			name:     "被截断时不还原",
			frames:   deltaFrames(leakedSave, "length"),
			tools:    memoryTools,
			wantText: leakedSave,
			wantStop: "length",
		},
		{
			name:     "普通正文中的尖括号照常输出",
			frames:   deltaFrames("a<b 且 c<", "stop", 2),
			tools:    memoryTools,
			wantText: "a<b 且 c<",
			wantStop: "stop",
		},
		{
			name:      "原生 tool_calls 保持不变",
			frames:    native,
			tools:     memoryTools,
			wantNames: []string{"memory_save"},
			wantCalls: []map[string]any{{"content": "x"}},
			wantStop:  "tool_calls",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeDeepSeekSSE(w, true, tt.frames...)
			}))
			defer server.Close()
			client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL), agentllm.WithTimeout(time.Second))
			require.NoError(t, err)
			chunks, err := client.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}}, tt.tools)
			require.NoError(t, err)
			var text strings.Builder
			var last *agentllm.StreamChunk
			for chunk := range chunks {
				text.WriteString(chunk.Delta)
				last = chunk
			}
			require.NotNil(t, last)
			require.True(t, last.Done)
			require.NoError(t, last.Error)
			require.Equal(t, tt.wantText, text.String())
			require.Equal(t, tt.wantText, last.Content)
			require.Equal(t, tt.wantStop, last.FinishReason)
			require.Len(t, last.ToolCalls, len(tt.wantNames))
			ids := map[string]bool{}
			for i, call := range last.ToolCalls {
				require.Equal(t, tt.wantNames[i], call.Function.Name)
				require.Equal(t, "function", call.Type)
				require.NotEmpty(t, call.ID)
				require.False(t, ids[call.ID], "工具调用 ID 重复")
				ids[call.ID] = true
				args, ok := call.ArgumentsObject()
				require.True(t, ok)
				require.Equal(t, tt.wantCalls[i], args)
			}
		})
	}
}
