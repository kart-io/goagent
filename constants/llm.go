package constants

import "time"

// Provider identifies a model provider. Custom providers convert from a
// string.
type Provider string

// Model providers.
const (
	ProviderOpenAI      Provider = "openai"
	ProviderGemini      Provider = "gemini"
	ProviderDeepSeek    Provider = "deepseek"
	ProviderOllama      Provider = "ollama"
	ProviderSiliconFlow Provider = "siliconflow"
	ProviderKimi        Provider = "kimi"
	ProviderMiniMax     Provider = "minimax"
	ProviderCustom      Provider = "custom"
	ProviderAnthropic   Provider = "anthropic"
)

// Provider environment variable names.
const (
	EnvOpenAIAPIKey         = "OPENAI_API_KEY"
	EnvOpenAIBaseURL        = "OPENAI_BASE_URL"
	EnvOpenAIModel          = "OPENAI_MODEL"
	EnvAnthropicAPIKey      = "ANTHROPIC_API_KEY"
	EnvAnthropicBaseURL     = "ANTHROPIC_BASE_URL"
	EnvAnthropicModel       = "ANTHROPIC_MODEL"
	EnvKimiAPIKey           = "KIMI_API_KEY"
	EnvKimiBaseURL          = "KIMI_BASE_URL"
	EnvKimiModel            = "KIMI_MODEL"
	EnvMiniMaxAPIKey        = "MINIMAX_API_KEY" // #nosec G101 -- variable name, not a credential
	EnvMiniMaxBaseURL       = "MINIMAX_BASE_URL"
	EnvMiniMaxModel         = "MINIMAX_MODEL"
	EnvSiliconFlowAPIKey    = "SILICONFLOW_API_KEY"
	EnvSiliconFlowBaseURL   = "SILICONFLOW_BASE_URL"
	EnvSiliconFlowModel     = "SILICONFLOW_MODEL"
	EnvDeepSeekAPIKey       = "DEEPSEEK_API_KEY"
	EnvDeepSeekBaseURL      = "DEEPSEEK_BASE_URL"
	EnvDeepSeekModel        = "DEEPSEEK_MODEL"
	EnvGeminiAPIKey         = "GEMINI_API_KEY"
	EnvGeminiBaseURL        = "GEMINI_BASE_URL"
	EnvGeminiModel          = "GEMINI_MODEL"
	EnvGeminiEmbeddingModel = "GEMINI_EMBEDDING_MODEL"
	EnvOllamaBaseURL        = "OLLAMA_BASE_URL"
	EnvOllamaModel          = "OLLAMA_MODEL"
)

// Provider defaults. DefaultProviderMaxTokens and DefaultProviderTimeout are
// former fallbacks that providers no longer apply.
const (
	DefaultProviderMaxTokens = 1000
	DefaultTemperature       = 0.7
	DefaultProviderTimeout   = 30 * time.Second
	DefaultTopP              = 1.0
)

// Provider endpoints and models.
const (
	AnthropicBaseURL      = "https://api.anthropic.com"
	AnthropicDefaultModel = "claude-opus-5-5"
	KimiBaseURL           = "https://api.moonshot.cn/v1"
	MiniMaxBaseURL        = "https://api.minimaxi.com/v1"
	SiliconFlowBaseURL    = "https://api.siliconflow.cn/v1"
	DeepSeekDefaultModel  = "deepseek-chat"
	DeepSeekBaseURL       = "https://api.deepseek.com/v1"
)

// Connection pool settings.
const (
	MaxIdleConnsPerHost = 10
)

// Retry settings.
const (
	DefaultMaxAttempts = 3
	DefaultBaseDelay   = 1 * time.Second
	DefaultMaxDelay    = 30 * time.Second
)

// Completion request defaults.
const (
	DefaultCompletionMaxTokens = 2048
)

// Local provider endpoint.
const (
	OllamaBaseURL = "http://localhost:11434/v1"
)

// Provider default models and timeouts.
const (
	KimiDefaultModel        = "moonshot-v1-8k"
	MiniMaxDefaultModel     = "minimax-m2.7"
	SiliconFlowDefaultModel = "Qwen/Qwen2-7B-Instruct"
	OllamaDefaultModel      = "llama2"
	DefaultOllamaTimeout    = 120 * time.Second // former default; the Ollama preset no longer applies it
)
