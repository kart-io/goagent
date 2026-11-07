package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

func TestOpenAICompatiblePresetsDefaults(t *testing.T) {
	for _, tc := range []struct {
		name     string
		new      func(...agentllm.ClientOption) (*OpenAIProvider, error)
		provider shared.Provider
		baseURL  string
		model    string
	}{
		{"kimi", NewKimiWithOptions, shared.ProviderKimi, shared.KimiBaseURL, "moonshot-v1-8k"},
		{"siliconflow", NewSiliconFlowWithOptions, shared.ProviderSiliconFlow, shared.SiliconFlowBaseURL, "Qwen/Qwen2-7B-Instruct"},
		{"ollama", NewOllamaWithOptions, shared.ProviderOllama, OllamaBaseURL, "llama2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(shared.EnvKimiBaseURL, "")
			t.Setenv(shared.EnvKimiModel, "")
			t.Setenv(shared.EnvSiliconFlowBaseURL, "")
			t.Setenv(shared.EnvSiliconFlowModel, "")
			t.Setenv(shared.EnvOllamaBaseURL, "")
			t.Setenv(shared.EnvOllamaModel, "")
			p, err := tc.new(agentllm.WithAPIKey("key"))
			require.NoError(t, err)
			require.Equal(t, tc.provider, p.Provider())
			require.Equal(t, tc.baseURL, p.Config.BaseURL)
			require.Equal(t, tc.model, p.Config.Model)
			require.True(t, agentllm.HasToolStreaming(p))
			require.True(t, p.HasCapability(agentllm.CapabilityToolStreaming))
			// Embed 固定使用 OpenAI 的向量模型，兼容服务不声明该能力。
			require.False(t, agentllm.HasEmbedding(p))
		})
	}
}

func TestOpenAICompatiblePresetsRequireAPIKey(t *testing.T) {
	t.Setenv(shared.EnvKimiAPIKey, "")
	t.Setenv(shared.EnvSiliconFlowAPIKey, "")
	_, err := NewKimiWithOptions()
	require.Error(t, err)
	_, err = NewSiliconFlowWithOptions()
	require.Error(t, err)
}

func TestOllamaPresetWithoutAPIKey(t *testing.T) {
	p, err := NewOllamaClientSimple("qwen3")
	require.NoError(t, err)
	require.Equal(t, "qwen3", p.Config.Model)
	require.NotEmpty(t, p.Config.APIKey)
}

// 兼容服务不接受 reasoning_effort：构造与单次请求中的显式推理配置都在发出请求前报错。
func TestOpenAICompatiblePresetsRejectReasoning(t *testing.T) {
	for _, newClient := range []func(...agentllm.ClientOption) (*OpenAIProvider, error){NewKimiWithOptions, NewSiliconFlowWithOptions, NewOllamaWithOptions} {
		_, err := newClient(agentllm.WithAPIKey("key"), agentllm.WithReasoning(usage.ReasoningConfig{Effort: "low"}))
		require.ErrorContains(t, err, "does not support")
		p, err := newClient(agentllm.WithAPIKey("key"))
		require.NoError(t, err)
		_, err = p.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Reasoning: &usage.ReasoningConfig{Effort: "low"}}, nil)
		require.ErrorContains(t, err, "does not support")
	}
}

func TestCompatibleTimeout(t *testing.T) {
	def := time.Duration(agentllm.DefaultLLMOptions().Timeout) * time.Second
	require.Equal(t, 120*time.Second, compatibleTimeout(def, 120*time.Second))
	require.Equal(t, 5*time.Second, compatibleTimeout(5*time.Second, 120*time.Second))
	require.Equal(t, def, compatibleTimeout(def, 0))
}

// 预设沿用 OpenAI 的原生工具流：请求发往兼容端点并携带 Bearer 密钥。
func TestOpenAICompatiblePresetNativeToolStream(t *testing.T) {
	var path, auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range []string{
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	p, err := NewKimiWithOptions(agentllm.WithAPIKey("secret"), agentllm.WithBaseURL(server.URL+"/v1"))
	require.NoError(t, err)
	stream, err := p.CompleteToolsStream(context.Background(), &agentllm.CompletionRequest{Messages: []agentllm.Message{{Role: "user", Content: "q"}}},
		[]interfaces.Tool{&mockTool{name: "lookup", schema: map[string]any{"type": "object"}}})
	require.NoError(t, err)
	var final *agentllm.StreamChunk
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		if chunk.Done {
			final = chunk
		}
	}
	require.NotNil(t, final)
	require.Len(t, final.ToolCalls, 1)
	require.Equal(t, 5, final.Usage.TotalTokens)
	require.Equal(t, "/v1/chat/completions", path)
	require.Equal(t, "Bearer secret", auth)
}

func TestOpenAIProviderListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"qwen3","object":"model"},{"id":"llama2","object":"model"}]}`)
	}))
	defer server.Close()
	p, err := NewOllamaWithOptions(agentllm.WithBaseURL(server.URL + "/v1"))
	require.NoError(t, err)
	models, err := p.ListModels(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"qwen3", "llama2"}, models)
}
