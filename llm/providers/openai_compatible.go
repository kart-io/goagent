package providers

import (
	"context"
	"time"

	shared "github.com/kart-io/goagent/constants"
	agentllm "github.com/kart-io/goagent/llm"
)

// OllamaBaseURL 是 Ollama 的 OpenAI 兼容端点；原生 /api 路径不再使用。
const OllamaBaseURL = "http://localhost:11434/v1"

// NewKimiWithOptions 创建 Moonshot（Kimi）客户端，经 OpenAI 兼容协议支持流式与原生工具流。
func NewKimiWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	return newOpenAICompatible(openAICompatibleSpec{
		provider:       shared.ProviderKimi,
		defaultBaseURL: shared.KimiBaseURL,
		defaultModel:   "moonshot-v1-8k",
		envAPIKey:      shared.EnvKimiAPIKey,
		envBaseURL:     shared.EnvKimiBaseURL,
		envModel:       shared.EnvKimiModel,
		capabilities:   openAICompatibleCapabilities,
	}, opts...)
}

// NewSiliconFlowWithOptions 创建 SiliconFlow 客户端，经 OpenAI 兼容协议支持流式与原生工具流。
func NewSiliconFlowWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	return newOpenAICompatible(openAICompatibleSpec{
		provider:       shared.ProviderSiliconFlow,
		defaultBaseURL: shared.SiliconFlowBaseURL,
		defaultModel:   "Qwen/Qwen2-7B-Instruct",
		envAPIKey:      shared.EnvSiliconFlowAPIKey,
		envBaseURL:     shared.EnvSiliconFlowBaseURL,
		envModel:       shared.EnvSiliconFlowModel,
		capabilities:   openAICompatibleCapabilities,
	}, opts...)
}

// NewOllamaWithOptions 创建本地 Ollama 客户端，不需要 API Key。BaseURL 须指向 /v1 兼容端点；
// 本地模型首块较慢，未改动默认超时时使用 120 秒。
func NewOllamaWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	return newOpenAICompatible(openAICompatibleSpec{
		provider:       shared.ProviderOllama,
		defaultBaseURL: OllamaBaseURL,
		defaultModel:   "llama2",
		envBaseURL:     shared.EnvOllamaBaseURL,
		envModel:       shared.EnvOllamaModel,
		apiKeyOptional: true,
		defaultTimeout: 120 * time.Second,
		capabilities:   openAICompatibleCapabilities,
	}, opts...)
}

// NewOllamaClientSimple 使用默认配置创建指定模型的 Ollama 客户端。
func NewOllamaClientSimple(model string) (*OpenAIProvider, error) {
	return NewOllamaWithOptions(agentllm.WithModel(model))
}

// ListModels 返回 /models 端点列出的模型 ID。
func (p *OpenAIProvider) ListModels(ctx context.Context) ([]string, error) {
	list, err := p.client.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list.Models))
	for _, m := range list.Models {
		ids = append(ids, m.ID)
	}
	return ids, nil
}
