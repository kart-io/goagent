package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

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
			stream, err := p.CompleteToolsStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("查询")}}, nil)
			require.NoError(t, err)
			var calls []llm.ToolCall
			var streamErr error
			for chunk := range stream {
				if chunk.Error != nil {
					streamErr = chunk.Error
				}
				if chunk.Done {
					calls = chunk.ToolCalls
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
