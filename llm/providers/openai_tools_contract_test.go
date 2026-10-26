package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/utils/json"
	"github.com/stretchr/testify/require"
)

func TestOpenAIToolsModernProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Contains(t, request, "tools")
		require.NotContains(t, request, "functions")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"lookup","arguments":"{}"}},{"id":"b","type":"function","function":{"name":"lookup","arguments":"{\"q\":2}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	}))
	defer server.Close()
	p, err := NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
	require.NoError(t, err)
	result, err := p.GenerateWithTools(context.Background(), "查询", []interfaces.Tool{&mockTool{name: "lookup", schema: map[string]any{"type": "object"}}})
	require.NoError(t, err)
	require.Len(t, result.ToolCalls, 2)
	require.Equal(t, "a", result.ToolCalls[0].ID)
	require.Equal(t, `{"q":2}`, result.ToolCalls[1].Function.Arguments)
	require.Equal(t, 15, result.Usage.TotalTokens)
}

func TestOpenAIToolStreamInterleavingAndFailure(t *testing.T) {
	for _, scenario := range []string{"完整", "截断", "上游错误"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				frames := []string{
					`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"one","arguments":"{\"x\":"}},{"index":1,"id":"b","type":"function","function":{"name":"two","arguments":"{"}}]}}]}`,
					`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"}"}},{"index":0,"function":{"arguments":"1}"}}]}}]}`,
				}
				if scenario == "完整" {
					frames = append(frames, `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
				}
				if scenario == "上游错误" {
					frames = append(frames, `{"error":{"message":"上游失败","type":"server_error"}}`)
				}
				for _, frame := range frames {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
				}
				if scenario == "完整" {
					_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				}
			}))
			defer server.Close()
			p, err := NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
			require.NoError(t, err)
			stream, err := p.StreamWithTools(context.Background(), "查询", nil)
			require.NoError(t, err)
			var calls []*llm.ToolCall
			var streamErr error
			for chunk := range stream {
				if chunk.Type == "tool_call" {
					calls = append(calls, chunk.Value.(*llm.ToolCall))
				}
				if chunk.Type == "error" {
					streamErr = chunk.Value.(error)
				}
			}
			if scenario == "完整" {
				require.NoError(t, streamErr)
				require.Len(t, calls, 2)
				require.Equal(t, `{"x":1}`, calls[0].Function.Arguments)
				require.Equal(t, `{}`, calls[1].Function.Arguments)
				require.Equal(t, "b", calls[1].ID)
			} else {
				require.Error(t, streamErr)
				require.Empty(t, calls)
				if scenario == "截断" {
					require.ErrorIs(t, streamErr, io.ErrUnexpectedEOF)
				}
			}
		})
	}
}
