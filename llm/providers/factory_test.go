package providers

import (
	"testing"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClientFactory(t *testing.T) {
	factory := NewClientFactory()
	assert.NotNil(t, factory)
}

func TestClientFactory_CreateClient_UnsupportedProvider(t *testing.T) {
	factory := NewClientFactory()

	config := &agentllm.LLMOptions{
		Provider: constants.Provider("unsupported"),
		APIKey:   "test-key",
	}

	client, err := factory.CreateClient(config)

	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "unsupported provider")
}

func TestClientFactory_CreateClient_OpenAI(t *testing.T) {
	factory := NewClientFactory()

	config := &agentllm.LLMOptions{
		Provider: constants.ProviderOpenAI,
		APIKey:   "test-key",
		Model:    "gpt-4",
	}

	client, err := factory.CreateClient(config)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderOpenAI, client.Provider())
}

func TestClientFactory_CreateClient_Anthropic(t *testing.T) {
	factory := NewClientFactory()

	config := &agentllm.LLMOptions{
		Provider: constants.ProviderAnthropic,
		APIKey:   "test-key",
		Model:    "claude-3-opus-20240229",
	}

	client, err := factory.CreateClient(config)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderAnthropic, client.Provider())
}

func TestClientFactory_CreateClient_DeepSeek(t *testing.T) {
	factory := NewClientFactory()

	config := &agentllm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		Model:    "deepseek-chat",
	}

	client, err := factory.CreateClient(config)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderDeepSeek, client.Provider())
}

func TestClientFactory_CreateClient_Cohere(t *testing.T) {
	factory := NewClientFactory()

	config := &agentllm.LLMOptions{
		Provider: constants.ProviderCohere,
		APIKey:   "test-key",
		Model:    "command-r-plus",
	}

	client, err := factory.CreateClient(config)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderCohere, client.Provider())
}

func TestClientFactory_CreateClient_HuggingFace(t *testing.T) {
	factory := NewClientFactory()

	config := &agentllm.LLMOptions{
		Provider: constants.ProviderHuggingFace,
		APIKey:   "test-key",
		Model:    "mistralai/Mistral-7B-Instruct-v0.1",
	}

	client, err := factory.CreateClient(config)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderHuggingFace, client.Provider())
}

func TestClientFactory_CreateClientWithOptions(t *testing.T) {
	factory := NewClientFactory()

	client, err := factory.CreateClientWithOptions(
		agentllm.WithProvider(constants.ProviderOpenAI),
		agentllm.WithAPIKey("test-key"),
		agentllm.WithModel("gpt-4"),
		agentllm.WithMaxTokens(100),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderOpenAI, client.Provider())
}

func TestCreateOpenAIClient(t *testing.T) {
	client, err := CreateOpenAIClient("test-key",
		agentllm.WithModel("gpt-4"),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderOpenAI, client.Provider())
}

func TestCreateAnthropicClient(t *testing.T) {
	client, err := CreateAnthropicClient("test-key",
		agentllm.WithModel("claude-3-opus-20240229"),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderAnthropic, client.Provider())
}

func TestCreateGeminiClient(t *testing.T) {
	client, err := CreateGeminiClient("test-key",
		agentllm.WithModel("gemini-1.5-pro"),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderGemini, client.Provider())
}

func TestCreateOllamaClient(t *testing.T) {
	client, err := CreateOllamaClient("llama2")

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderOllama, client.Provider())
}

func TestCreateOllamaClient_WithCustomBaseURL(t *testing.T) {
	client, err := CreateOllamaClient("llama2",
		agentllm.WithBaseURL("http://custom:11434"),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderOllama, client.Provider())
}

func TestCreateClientForUseCase_Balanced(t *testing.T) {
	client, err := CreateClientForUseCase(
		constants.ProviderOpenAI,
		"test-key",
		agentllm.UseCaseChat,
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderOpenAI, client.Provider())
}

func TestCreateClientForUseCase_Speed(t *testing.T) {
	client, err := CreateClientForUseCase(
		constants.ProviderAnthropic,
		"test-key",
		agentllm.UseCaseCodeGeneration,
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderAnthropic, client.Provider())
}

func TestCreateProductionClient(t *testing.T) {
	client, err := CreateProductionClient(
		constants.ProviderOpenAI,
		"test-key",
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderOpenAI, client.Provider())
}

func TestCreateProductionClient_WithOverrides(t *testing.T) {
	client, err := CreateProductionClient(
		constants.ProviderOpenAI,
		"test-key",
		agentllm.WithModel("gpt-4-turbo"),
		agentllm.WithMaxTokens(200),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderOpenAI, client.Provider())
}

func TestCreateDevelopmentClient(t *testing.T) {
	client, err := CreateDevelopmentClient(
		constants.ProviderOpenAI,
		"test-key",
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderOpenAI, client.Provider())
}

func TestCreateDevelopmentClient_WithOverrides(t *testing.T) {
	client, err := CreateDevelopmentClient(
		constants.ProviderAnthropic,
		"test-key",
		agentllm.WithModel("claude-3-haiku-20240307"),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, constants.ProviderAnthropic, client.Provider())
}

func TestClientFactory_CreateClient_AllProviders(t *testing.T) {
	testCases := []struct {
		name     string
		provider constants.Provider
		model    string
	}{
		{
			name:     "OpenAI",
			provider: constants.ProviderOpenAI,
			model:    "gpt-4",
		},
		{
			name:     "Anthropic",
			provider: constants.ProviderAnthropic,
			model:    "claude-3-opus-20240229",
		},
		{
			name:     "Gemini",
			provider: constants.ProviderGemini,
			model:    "gemini-1.5-pro",
		},
		{
			name:     "DeepSeek",
			provider: constants.ProviderDeepSeek,
			model:    "deepseek-chat",
		},
		{
			name:     "Kimi",
			provider: constants.ProviderKimi,
			model:    "moonshot-v1-8k",
		},
		{
			name:     "SiliconFlow",
			provider: constants.ProviderSiliconFlow,
			model:    "Qwen/Qwen2.5-7B-Instruct",
		},
		{
			name:     "Ollama",
			provider: constants.ProviderOllama,
			model:    "llama2",
		},
		{
			name:     "Cohere",
			provider: constants.ProviderCohere,
			model:    "command-r-plus",
		},
		{
			name:     "HuggingFace",
			provider: constants.ProviderHuggingFace,
			model:    "mistralai/Mistral-7B-Instruct-v0.1",
		},
	}

	factory := NewClientFactory()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &agentllm.LLMOptions{
				Provider: tc.provider,
				APIKey:   "test-key",
				Model:    tc.model,
			}

			client, err := factory.CreateClient(config)

			require.NoError(t, err)
			require.NotNil(t, client)
			assert.Equal(t, tc.provider, client.Provider())
		})
	}
}
