package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestOpenAIExplainability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "low", req["reasoning_effort"])
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"model\":\"resolved-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":8,\"total_tokens\":18,\"completion_tokens_details\":{\"reasoning_tokens\":6}}}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	p, err := NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL), llm.WithReasoning(usage.ReasoningConfig{Effort: "high"}))
	require.NoError(t, err)
	ch, err := p.CompleteStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: "test"}}, Reasoning: &usage.ReasoningConfig{Effort: "low"}})
	require.NoError(t, err)
	var last *llm.StreamChunk
	for v := range ch {
		last = v
	}
	require.NoError(t, last.Error)
	require.Equal(t, "resolved-model", last.ResponseModel)
	require.Equal(t, 6, last.Usage.ReasoningTokens)
	require.Equal(t, 18, last.Usage.TotalTokens)
	require.NotZero(t, last.Usage.Fields()&usage.Reasoning)
}
func TestGeminiExplainability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		cfg := req["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)
		require.Equal(t, float64(0), cfg["thinkingBudget"])
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"modelVersion\":\"gemini-resolved\",\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":2,\"thoughtsTokenCount\":6,\"toolUsePromptTokenCount\":3,\"totalTokenCount\":21}}\n\n"))
	}))
	defer server.Close()
	p, err := NewGeminiWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL), llm.WithReasoning(usage.ReasoningConfig{BudgetSet: true, BudgetTokens: 0}))
	require.NoError(t, err)
	ch, err := p.CompleteStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: "test"}}})
	require.NoError(t, err)
	var last *llm.StreamChunk
	for v := range ch {
		last = v
	}
	require.NoError(t, last.Error)
	require.Equal(t, "gemini-resolved", last.ResponseModel)
	require.Equal(t, 6, last.Usage.ReasoningTokens)
	require.Equal(t, 3, last.Usage.ToolUsePromptTokens)
	require.Equal(t, 21, last.Usage.TotalTokens)
}
func TestUnsupportedReasoningIsRejectedBeforeHTTP(t *testing.T) {
	p, err := NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL("http://127.0.0.1:1"))
	require.NoError(t, err)
	_, err = p.CompleteStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: "test"}}, Reasoning: &usage.ReasoningConfig{BudgetSet: true, BudgetTokens: 5}})
	require.ErrorContains(t, err, "budget")
}

func TestDetailCumulativeFramesDoNotEraseOrAdd(t *testing.T) {
	first := sdkUsageDetails(sdkUsage(10, 2, 21, 0), 6, 3)
	last := sdkUsage(10, 2, 21, 0)
	merged := mergeSDKUsage(first, last)
	require.Equal(t, 6, merged.ReasoningTokens)
	require.Equal(t, 3, merged.ToolUsePromptTokens)
	require.Equal(t, 21, merged.TotalTokens)
	zero := sdkUsageDetails(sdkUsage(10, 2, 12, 0), 0, 0)
	require.Zero(t, zero.Fields()&usage.Reasoning)
	require.Zero(t, zero.Fields()&usage.ToolUsePrompt)
}

func TestMalformedToolRetainsKnownExplainability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"model\":\"actual\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":8,\"total_tokens\":18,\"completion_tokens_details\":{\"reasoning_tokens\":6}}}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"function_call\":{\"name\":\"legacy\",\"arguments\":\"{}\"}}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	p, err := NewOpenAIWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
	require.NoError(t, err)
	ch, err := p.CompleteStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: "test"}}})
	require.NoError(t, err)
	var last *llm.StreamChunk
	for v := range ch {
		last = v
	}
	require.Error(t, last.Error)
	require.NotNil(t, last.Usage)
	require.Equal(t, 18, last.Usage.TotalTokens)
	require.Equal(t, 6, last.Usage.ReasoningTokens)
	require.Equal(t, "actual", last.ResponseModel)
}

func TestOtherProvidersRejectExplicitReasoning(t *testing.T) {
	constructors := []func(...llm.ClientOption) (llm.Client, error){
		func(o ...llm.ClientOption) (llm.Client, error) { return NewOllamaWithOptions(o...) },
	}
	for _, newClient := range constructors {
		_, err := newClient(llm.WithAPIKey("test"), llm.WithReasoning(usage.ReasoningConfig{Effort: "low"}))
		require.ErrorContains(t, err, "does not support")
		p, err := newClient(llm.WithAPIKey("test"))
		require.NoError(t, err)
		_, err = p.Complete(context.Background(), &llm.CompletionRequest{Reasoning: &usage.ReasoningConfig{Effort: "low"}})
		require.ErrorContains(t, err, "does not support")
	}
}
