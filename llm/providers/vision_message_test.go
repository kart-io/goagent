package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	agentllm "github.com/kart-io/goagent/llm"
)

func captureOpenAIMessages(t *testing.T, msg agentllm.Message) []any {
	t.Helper()
	bodies := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.Decode(r.Body, &req); err != nil {
			t.Error(err)
			return
		}
		bodies <- req
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client, err := NewOpenAIWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL+"/v1"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	chunks, err := client.CompleteStream(ctx, &agentllm.CompletionRequest{Model: "m", Messages: []agentllm.Message{msg}})
	require.NoError(t, err)
	for range chunks {
	}
	return (<-bodies)["messages"].([]any)
}

func TestOpenAISendsImagesAsContentParts(t *testing.T) {
	messages := captureOpenAIMessages(t, agentllm.Message{
		Role: "user", Content: "看图",
		Images: []agentllm.ImagePart{{URL: "data:image/png;base64,AAA"}, {URL: "https://example.com/b.png"}},
	})
	require.Equal(t, []any{
		map[string]any{"type": "text", "text": "看图"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAA"}},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/b.png"}},
	}, messages[0].(map[string]any)["content"])
}

func TestOpenAIImageOnlyMessageOmitsTextPart(t *testing.T) {
	messages := captureOpenAIMessages(t, agentllm.Message{Role: "user", Images: []agentllm.ImagePart{{URL: "data:image/png;base64,AAA"}}})
	require.Equal(t, []any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAA"}},
	}, messages[0].(map[string]any)["content"])
}

func TestOpenAIPlainMessageStaysString(t *testing.T) {
	messages := captureOpenAIMessages(t, agentllm.Message{Role: "user", Content: "hi"})
	require.Equal(t, "hi", messages[0].(map[string]any)["content"])
}

func TestAnthropicRejectsImages(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer server.Close()
	p := newTestAnthropic(t, server.URL)
	_, err := p.CompleteStream(context.Background(), &agentllm.CompletionRequest{
		Messages: []agentllm.Message{{Role: "user", Content: "看图", Images: []agentllm.ImagePart{{URL: "data:image/png;base64,AAA"}}}},
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "images not supported"), err.Error())
	require.Zero(t, hits.Load())
}

func TestGeminiRejectsImages(t *testing.T) {
	_, _, err := geminiMessages([]agentllm.Message{{Role: "user", Content: "看图", Images: []agentllm.ImagePart{{URL: "data:image/png;base64,AAA"}}}}, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "images not supported")
}
