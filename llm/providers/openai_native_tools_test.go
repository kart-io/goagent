package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/utils/json"
	"github.com/stretchr/testify/require"
)

func TestNativeToolStreamPreservesMessagesAndUsage(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`{"choices":[{"index":0,"delta":{"reasoning_content":"续接上下文","tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}},{"index":1,"id":"b","type":"function","function":{"name":"lookup","arguments":"{"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"}"}},{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
		}
		for _, frame := range frames {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	provider, err := NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
	require.NoError(t, err)
	call := llm.ToolCall{ID: "previous", Type: "function"}
	call.Function.Name = "lookup"
	call.Function.Arguments = "{}"
	req := &llm.CompletionRequest{Model: "test-model", MaxTokens: 88, Temperature: 0.2, Messages: []llm.Message{{Role: "system", Content: "规则"}, {Role: "user", Content: "问题"}, {Role: "assistant", ToolCalls: []llm.ToolCall{call}, ReasoningContent: "前轮上下文"}, {Role: "tool", ToolCallID: "previous", Content: `"资料"`}}}
	stream, err := provider.CompleteToolsStream(context.Background(), req, []interfaces.Tool{&mockTool{name: "lookup", schema: map[string]any{"type": "object"}}})
	require.NoError(t, err)
	var final *llm.StreamChunk
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		if chunk.Done {
			final = chunk
		} else {
			require.Empty(t, chunk.ToolCalls)
		}
	}
	require.NotNil(t, final)
	require.Len(t, final.ToolCalls, 2)
	require.Equal(t, `{"q":1}`, final.ToolCalls[0].Function.Arguments)
	require.Equal(t, `{}`, final.ToolCalls[1].Function.Arguments)
	require.Equal(t, 15, final.Usage.TotalTokens)
	require.Equal(t, "续接上下文", final.ReasoningContent)
	require.Empty(t, final.Content)
	wire := <-requests
	require.Equal(t, float64(88), wire["max_tokens"])
	require.Equal(t, 0.2, wire["temperature"])
	require.NotEmpty(t, wire["tools"])
	require.Equal(t, map[string]any{"include_usage": true}, wire["stream_options"])
	messages := wire["messages"].([]any)
	require.Len(t, messages, 4)
	assistant := messages[2].(map[string]any)
	require.Equal(t, "前轮上下文", assistant["reasoning_content"])
	require.Len(t, assistant["tool_calls"], 1)
	require.Equal(t, "previous", messages[3].(map[string]any)["tool_call_id"])
}

func TestNativeFinalAnswerRequestOmitsTools(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		requests <- req
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"false\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	provider, err := NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
	require.NoError(t, err)
	call := llm.ToolCall{ID: "a", Type: "function"}
	call.Function.Name = "lookup"
	call.Function.Arguments = "{}"
	stream, err := provider.CompleteToolsStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("原样返回"), {Role: "assistant", ToolCalls: []llm.ToolCall{call}}, {Role: "tool", Content: "false", ToolCallID: "a"}}}, nil)
	require.NoError(t, err)
	var answer string
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		answer += chunk.Delta
	}
	require.Equal(t, "false", answer)
	wire := <-requests
	require.NotContains(t, wire, "tools")
	require.NotContains(t, wire, "functions")
	messages := wire["messages"].([]any)
	require.Equal(t, "false", messages[2].(map[string]any)["content"])
	require.Equal(t, "a", messages[2].(map[string]any)["tool_call_id"])
}
