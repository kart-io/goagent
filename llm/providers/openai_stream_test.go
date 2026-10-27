package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// The no-index terminal shape reproduces the real Gemini 2.5 Flash response.
func TestOpenAIStreamCompleteUnindexedTools(t *testing.T) {
	call := `{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"order_id\":\"SHARED-1\"}"}}`
	second := strings.ReplaceAll(call, "call-1", "call-2")
	indexed := strings.Replace(call, `{"id":`, `{"index":0,"id":`, 1)
	frame := func(calls, finish string) string {
		return fmt.Sprintf("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[%s]},\"finish_reason\":%q}]}\n\n", calls, finish)
	}
	for _, tc := range []struct {
		name, frames string
		wantCalls    int
	}{
		{"single", frame(call, "tool_calls"), 1},
		{"multiple", frame(call+","+second, "tool_calls"), 2},
		{"nonterminal", frame(call, ""), 0},
		{"wrong_finish", frame(call, "stop"), 0},
		{"duplicate_ids", frame(call+","+call, "tool_calls"), 0},
		{"mixed_indices", frame(call+","+indexed, "tool_calls"), 0},
		{"previous_fragment", frame(indexed, "") + frame(second, "tool_calls"), 0},
		{"later_fragment", frame(call, "tool_calls") + frame(indexed, "tool_calls"), 0},
		{"missing_id", frame(strings.ReplaceAll(call, "call-1", ""), "tool_calls"), 0},
		{"missing_name", frame(strings.ReplaceAll(call, "lookup", ""), "tool_calls"), 0},
		{"incomplete_args", frame(strings.Replace(call, `SHARED-1\"}`, `SHARED-1\"`, 1), "tool_calls"), 0},
		{"null_args", frame(`{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"null"}}`, "tool_calls"), 0},
		{"negative_index", frame(strings.Replace(indexed, `"index":0`, `"index":-1`, 1), "tool_calls"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, tc.frames)
				fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":48,\"completion_tokens\":17,\"total_tokens\":120}}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			client, err := NewOpenAIWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL))
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			chunks, err := client.ChatStream(ctx, []agentllm.Message{agentllm.UserMessage("query")})
			require.NoError(t, err)
			var last *agentllm.StreamChunk
			for chunk := range chunks {
				last = chunk
			}
			require.NotNil(t, last)
			require.True(t, last.Done)
			if tc.wantCalls == 0 {
				require.Error(t, last.Error)
				require.Empty(t, last.ToolCalls)
				return
			}
			require.NoError(t, last.Error)
			require.Len(t, last.ToolCalls, tc.wantCalls)
			for i, c := range last.ToolCalls {
				require.Equal(t, fmt.Sprintf("call-%d", i+1), c.ID)
				require.Equal(t, "lookup", c.Function.Name)
				require.JSONEq(t, `{"order_id":"SHARED-1"}`, c.Function.Arguments)
			}
			require.Equal(t, "tool_calls", last.FinishReason)
			require.Equal(t, 120, last.Usage.TotalTokens)
		})
	}
}
