package constants

import "time"

// Provider 是模型提供商标识，允许通过字符串转换扩展自定义提供商。
type Provider string

// 模型提供商标识。
const (
	ProviderOpenAI      Provider = "openai"
	ProviderGemini      Provider = "gemini"
	ProviderDeepSeek    Provider = "deepseek"
	ProviderOllama      Provider = "ollama"
	ProviderSiliconFlow Provider = "siliconflow"
	ProviderKimi        Provider = "kimi"
	ProviderCustom      Provider = "custom"
	ProviderAnthropic   Provider = "anthropic"
)

// 提供商环境变量名。
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

// 工具与函数调用约定。
const (
	ToolTypeFunction = "function"
)

// 提供商默认参数。
const (
	DefaultProviderMaxTokens = 1000
	DefaultTemperature       = 0.7
	DefaultProviderTimeout   = 30 * time.Second
	DefaultTopP              = 1.0
)

// 提供商服务地址与参数。
const (
	AnthropicBaseURL      = "https://api.anthropic.com"
	AnthropicDefaultModel = "claude-opus-5-5"
	KimiBaseURL           = "https://api.moonshot.cn/v1"
	SiliconFlowBaseURL    = "https://api.siliconflow.cn/v1"
	DeepSeekDefaultModel  = "deepseek-chat"
	DeepSeekBaseURL       = "https://api.deepseek.com/v1"
)

// 连接池参数。
const (
	MaxIdleConns        = 100
	MaxIdleConnsPerHost = 10
	IdleConnTimeout     = 90 * time.Second
)

// 重试参数。
const (
	DefaultMaxAttempts = 3
	DefaultBaseDelay   = 1 * time.Second
	DefaultMaxDelay    = 30 * time.Second
)

// 补全请求默认参数。
const (
	DefaultCompletionMaxTokens = 2048
	DefaultFrequencyPenalty    = 0.0
	DefaultPresencePenalty     = 0.0
)

// 共享默认策略与协议值；领域实现直接引用本包。
const (
	OllamaBaseURL = "http://localhost:11434/v1"
)

// 提供商模型和首块等待时间分别保留各自策略。
const (
	KimiDefaultModel        = "moonshot-v1-8k"
	SiliconFlowDefaultModel = "Qwen/Qwen2-7B-Instruct"
	OllamaDefaultModel      = "llama2"
	DefaultOllamaTimeout    = 120 * time.Second
)
