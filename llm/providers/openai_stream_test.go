package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/utils/json"
)

func TestOpenAICompleteStreamContract(t *testing.T) {
	requests := make(chan map[string]interface{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		requests <- req
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"你\"}}]}\n\n")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":2,\"total_tokens\":10}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client, err := NewOpenAIWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL+"/v1"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	chunks, err := client.CompleteStream(ctx, &agentllm.CompletionRequest{
		Model: "test-model", MaxTokens: 42, Temperature: 0.2, TopP: 0.8, Stop: []string{"end"},
		Messages: []agentllm.Message{agentllm.SystemMessage("instruction"), agentllm.UserMessage("question")},
	})
	require.NoError(t, err)
	var result []*agentllm.StreamChunk
	for chunk := range chunks {
		result = append(result, chunk)
	}
	require.Len(t, result, 3)
	require.Equal(t, "你", result[0].Delta)
	require.Equal(t, "你好", result[1].Content)
	require.False(t, result[1].Done, "finish_reason must not discard the following usage frame")
	require.True(t, result[2].Done)
	require.NoError(t, result[2].Error)
	require.Equal(t, "stop", result[2].FinishReason)
	require.Equal(t, &agentllm.Usage{PromptTokens: 8, CompletionTokens: 2, TotalTokens: 10}, result[2].Usage)
	for i, chunk := range result {
		require.Equal(t, i, chunk.Index)
	}
	req := <-requests
	require.Equal(t, "test-model", req["model"])
	require.Equal(t, float64(42), req["max_tokens"])
	require.Equal(t, 0.2, req["temperature"])
	require.Equal(t, true, req["stream"])
	require.Equal(t, map[string]interface{}{"include_usage": true}, req["stream_options"])
	require.Len(t, req["messages"], 2)
}

func TestOpenAICompleteStreamPropagatesReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		fmt.Fprint(w, "data: invalid-json\n\n")
	}))
	defer server.Close()
	client, err := NewOpenAIWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL+"/v1"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	chunks, err := client.ChatStream(ctx, []agentllm.Message{agentllm.UserMessage("question")})
	require.NoError(t, err)
	var last *agentllm.StreamChunk
	for chunk := range chunks {
		last = chunk
	}
	require.NotNil(t, last)
	require.True(t, last.Done)
	require.Error(t, last.Error)
	require.Equal(t, "partial", last.Content)
}

func TestOpenAICompleteStreamCancellationClosesRequest(t *testing.T) {
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	client, err := NewOpenAIWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL+"/v1"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	chunks, err := client.ChatStream(ctx, []agentllm.Message{agentllm.UserMessage("question")})
	require.NoError(t, err)
	select {
	case <-chunks:
	case <-ctx.Done():
		t.Fatal("no first chunk")
	}
	cancel()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request was not canceled")
	}
	for range chunks {
	}
}
