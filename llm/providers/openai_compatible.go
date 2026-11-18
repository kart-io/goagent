package providers

import (
	"context"

	shared "github.com/kart-io/goagent/constants"
	agentllm "github.com/kart-io/goagent/llm"
)

// NewKimiWithOptions creates a Moonshot (Kimi) client that supports streaming
// and native tool streams via the OpenAI-compatible protocol.
func NewKimiWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	return newOpenAICompatible(openAICompatibleSpec{
		provider:       shared.ProviderKimi,
		defaultBaseURL: shared.KimiBaseURL,
		defaultModel:   shared.KimiDefaultModel,
		envAPIKey:      shared.EnvKimiAPIKey,
		envBaseURL:     shared.EnvKimiBaseURL,
		envModel:       shared.EnvKimiModel,
		capabilities:   openAICompatibleCapabilities,
	}, opts...)
}

// NewSiliconFlowWithOptions creates a SiliconFlow client that supports
// streaming and native tool streams via the OpenAI-compatible protocol.
func NewSiliconFlowWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	return newOpenAICompatible(openAICompatibleSpec{
		provider:       shared.ProviderSiliconFlow,
		defaultBaseURL: shared.SiliconFlowBaseURL,
		defaultModel:   shared.SiliconFlowDefaultModel,
		envAPIKey:      shared.EnvSiliconFlowAPIKey,
		envBaseURL:     shared.EnvSiliconFlowBaseURL,
		envModel:       shared.EnvSiliconFlowModel,
		capabilities:   openAICompatibleCapabilities,
	}, opts...)
}

// NewOllamaWithOptions creates a local Ollama client; no API key is needed.
// BaseURL must point to the /v1 compatible endpoint. Local models are slow to
// produce the first chunk, so 120 seconds is used when the default timeout is
// unchanged.
func NewOllamaWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	return newOpenAICompatible(openAICompatibleSpec{
		provider:       shared.ProviderOllama,
		defaultBaseURL: shared.OllamaBaseURL,
		defaultModel:   shared.OllamaDefaultModel,
		envBaseURL:     shared.EnvOllamaBaseURL,
		envModel:       shared.EnvOllamaModel,
		apiKeyOptional: true,
		defaultTimeout: shared.DefaultOllamaTimeout,
		capabilities:   openAICompatibleCapabilities,
	}, opts...)
}

// NewOllamaClientSimple creates an Ollama client for the given model with the
// default configuration.
func NewOllamaClientSimple(model string) (*OpenAIProvider, error) {
	return NewOllamaWithOptions(agentllm.WithModel(model))
}

// ListModels returns the model IDs listed by the /models endpoint.
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
