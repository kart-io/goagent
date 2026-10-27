package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func contractFactory[T llm.Client](newClient func(...llm.ClientOption) (T, error)) func(...llm.ClientOption) (llm.Client, error) {
	return func(opts ...llm.ClientOption) (llm.Client, error) { return newClient(opts...) }
}

// This matrix verifies executable interfaces and local entry-point rejection.
// It does not infer that every remote model supports all provider features.
func TestProviderCapabilityContractMatrix(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		factory                                  func(...llm.ClientOption) (llm.Client, error)
		tokens, structured, tools, native, embed bool
	}{
		{"openai", contractFactory(NewOpenAIWithOptions), true, true, true, true, true},
		{"deepseek", contractFactory(NewDeepSeekWithOptions), true, true, true, true, false},
		{"deepseek-wrapper", contractFactory(NewDeepSeekStreamingWithOptions), true, true, true, true, false},
		{"gemini", contractFactory(NewGeminiWithOptions), true, true, true, true, true},
		{"gemini-wrapper", contractFactory(NewGeminiStreamingWithOptions), true, true, true, true, true},
		{"anthropic", contractFactory(NewAnthropicWithOptions), true, false, false, false, false},
		{"cohere", contractFactory(NewCohereWithOptions), true, false, false, false, false},
		{"huggingface", contractFactory(NewHuggingFaceWithOptions), true, false, false, false, false},
		{"kimi", contractFactory(NewKimiWithOptions), false, false, false, false, false},
		{"siliconflow", contractFactory(NewSiliconFlowWithOptions), false, false, false, false, false},
		{"ollama", contractFactory(NewOllamaWithOptions), false, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "unexpected call", 500) }))
			defer server.Close()
			client, err := tc.factory(llm.WithAPIKey("contract-test"), llm.WithBaseURL(server.URL), llm.WithModel("contract-model"), llm.WithTimeout(time.Second))
			require.NoError(t, err)
			checker, ok := client.(llm.CapabilityChecker)
			require.True(t, ok, "all built-in providers must expose capability declarations")
			require.True(t, checker.HasCapability(llm.CapabilityCompletion))
			require.True(t, checker.HasCapability(llm.CapabilityChat))
			for capability, want := range map[llm.Capability]bool{
				llm.CapabilityStreaming:           tc.tokens || tc.structured,
				llm.CapabilityStructuredStreaming: tc.structured,
				llm.CapabilityToolCalling:         tc.tools,
				llm.CapabilityToolStreaming:       tc.native,
				llm.CapabilityEmbedding:           tc.embed,
			} {
				require.Equal(t, want, checker.HasCapability(capability), string(capability))
			}
			require.Equal(t, tc.tokens, llm.HasTokenStreaming(client))
			require.Equal(t, tc.structured, llm.HasStreaming(client))
			require.Equal(t, tc.tools, llm.HasToolCalling(client))
			require.Equal(t, tc.native, llm.HasToolStreaming(client))
			require.Equal(t, tc.embed, llm.HasEmbedding(client))
			require.Equal(t, tc.embed, llm.AsEmbedder(client) != nil)
			if !tc.structured {
				agent, err := builder.NewSimpleBuilder(client).Build()
				require.NoError(t, err)
				_, err = agent.AsRunnable().Invoke(context.Background(), "must reject unsupported structured streaming")
				require.ErrorContains(t, err, "StreamClient")
			}
			if !tc.native {
				_, err = builder.NewSimpleBuilder(client).WithReturnDirectTools("lookup").Build()
				require.ErrorContains(t, err, "ToolStreamClient")
			}
			require.Zero(t, calls.Load(), "capability rejection must happen before HTTP")
		})
	}
}

func TestGeminiEmbeddingCannotReturnMockSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "upstream failure", 503) }))
	defer server.Close()
	p, err := NewGeminiWithOptions(llm.WithAPIKey("contract-test"), llm.WithBaseURL(server.URL))
	require.NoError(t, err)
	for _, text := range []string{"order A", "completely different text"} {
		vector, err := p.Embed(context.Background(), text)
		require.Error(t, err)
		require.Nil(t, vector)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Embed(ctx, "cancel")
	require.ErrorIs(t, err, context.Canceled)
}
