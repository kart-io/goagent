package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

// captureServer 记录每次请求体，并按协议回一段最小的流式响应。
func captureServer(t *testing.T, sse string) (*httptest.Server, chan map[string]any) {
	t.Helper()
	bodies := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies <- body
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	t.Cleanup(server.Close)
	return server, bodies
}

const (
	openAIJSONStream = "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"ok\\\":true}\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	geminiJSONStream = "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"{\\\"ok\\\":true}\"}]},\"finishReason\":\"STOP\"}]}\n\n"
)

func streamText(t *testing.T, client agentllm.Client) string {
	t.Helper()
	chunks, err := client.CompleteStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}})
	require.NoError(t, err)
	var text string
	for chunk := range chunks {
		require.NoError(t, chunk.Error)
		text += chunk.Delta
	}
	return text
}

func TestResponseFormatReachesWire(t *testing.T) {
	newOpenAI := func(opts ...agentllm.ClientOption) (agentllm.Client, error) { return NewOpenAIWithOptions(opts...) }
	newDeepSeek := func(opts ...agentllm.ClientOption) (agentllm.Client, error) { return NewDeepSeekWithOptions(opts...) }
	newGemini := func(opts ...agentllm.ClientOption) (agentllm.Client, error) { return NewGeminiWithOptions(opts...) }
	openAIFormat := func(body map[string]any) any { return body["response_format"] }
	geminiMIME := func(body map[string]any) any {
		cfg, _ := body["generationConfig"].(map[string]any)
		return cfg["responseMimeType"]
	}
	cases := []struct {
		name   string
		newFn  func(...agentllm.ClientOption) (agentllm.Client, error)
		sse    string
		option []agentllm.ClientOption
		field  func(map[string]any) any
		want   any
	}{
		{"openai json", newOpenAI, openAIJSONStream, []agentllm.ClientOption{agentllm.WithJSONResponse()}, openAIFormat, map[string]any{"type": "json_object"}},
		{"openai unset", newOpenAI, openAIJSONStream, nil, openAIFormat, nil},
		{"deepseek json", newDeepSeek, openAIJSONStream, []agentllm.ClientOption{agentllm.WithJSONResponse()}, openAIFormat, map[string]any{"type": "json_object"}},
		{"gemini json", newGemini, geminiJSONStream, []agentllm.ClientOption{agentllm.WithJSONResponse()}, geminiMIME, "application/json"},
		{"gemini text", newGemini, geminiJSONStream, []agentllm.ClientOption{agentllm.WithResponseFormat(agentllm.ResponseFormatText)}, geminiMIME, "text/plain"},
		{"gemini unset", newGemini, geminiJSONStream, nil, geminiMIME, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, bodies := captureServer(t, tc.sse)
			client, err := tc.newFn(append([]agentllm.ClientOption{agentllm.WithAPIKey("k"), agentllm.WithBaseURL(server.URL)}, tc.option...)...)
			require.NoError(t, err)
			require.Equal(t, `{"ok":true}`, streamText(t, client))
			require.Equal(t, tc.want, tc.field(<-bodies))
		})
	}
}

func TestResponseFormatUnsupported(t *testing.T) {
	_, err := NewAnthropicWithOptions(agentllm.WithAPIKey("k"), agentllm.WithJSONResponse())
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidInput), "%v", err)
	_, err = NewAnthropicWithOptions(agentllm.WithAPIKey("k"), agentllm.WithResponseFormat(agentllm.ResponseFormatText))
	require.NoError(t, err)

	gemini, err := NewGeminiWithOptions(agentllm.WithAPIKey("k"), agentllm.WithBaseURL("http://127.0.0.1:1"), agentllm.WithResponseFormat(&agentllm.ResponseFormat{Type: "yaml"}))
	require.NoError(t, err)
	_, err = gemini.CompleteStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("q")}})
	require.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidInput), "%v", err)
}
