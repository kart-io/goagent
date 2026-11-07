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

// 模型请求参数名。
const (
	ParamTemperature      = "temperature"
	ParamMaxTokens        = "max_tokens"
	ParamTopP             = "top_p"
	ParamTopK             = "top_k"
	ParamFrequencyPenalty = "frequency_penalty"
	ParamPresencePenalty  = "presence_penalty"
	ParamStop             = "stop"
	ParamStream           = "stream"
	ParamN                = "n"
	ParamLogprobs         = "logprobs"
	ParamEcho             = "echo"
	ParamSeed             = "seed"
)

// 消息字段名。
const (
	MessageFieldRole         = "role"
	MessageFieldContent      = "content"
	MessageFieldName         = "name"
	MessageFieldFunctionCall = "function_call"
	MessageFieldToolCalls    = "tool_calls"
)

// 工具与函数调用约定。
const (
	ToolTypeFunction = "function"
	FunctionCallAuto = "auto"
	FunctionCallNone = "none"
)

// 响应字段名。
const (
	ResponseFieldID      = "id"
	ResponseFieldObject  = "object"
	ResponseFieldCreated = "created"
	ResponseFieldModel   = "model"
	ResponseFieldChoices = "choices"
	ResponseFieldUsage   = "usage"
)

// 用量统计字段名。
const (
	UsageFieldPromptTokens     = "prompt_tokens"
	UsageFieldCompletionTokens = "completion_tokens"
	UsageFieldTotalTokens      = "total_tokens"
)

// 流式事件标识。
const (
	StreamEventChunk    = "chunk"
	StreamEventDone     = "done"
	StreamEventToolCall = "tool_call"
)

// 提供商默认参数。
const (
	DefaultProviderMaxTokens = 1000
	DefaultTemperature       = 0.7
	DefaultProviderTimeout   = 30 * time.Second
	DefaultTopP              = 1.0
	DefaultTopK              = 0
)

// 提供商服务地址与参数。
const (
	AnthropicBaseURL      = "https://api.anthropic.com"
	AnthropicDefaultModel = "claude-opus-5-5"
	KimiBaseURL           = "https://api.moonshot.cn/v1"
	SiliconFlowBaseURL    = "https://api.siliconflow.cn/v1"
	DeepSeekDefaultModel  = "deepseek-chat"
	DeepSeekBaseURL       = "https://api.deepseek.com/v1"
	DeepSeekMaxTokens     = 4096
	DeepSeekTemperature   = 0.7
	DeepSeekTimeout       = 30 * time.Second
	DeepSeekRetryCount    = 3
	DeepSeekRetryDelay    = 1 * time.Second
)

// 请求头名称。
const (
	HeaderAPIKey     = "x-api-key"
	HeaderRetryAfter = "Retry-After"
)

// 认证前缀。
const (
	AuthBearerPrefix = "Bearer "
	AuthAPIKeyPrefix = "x-api-key: "
)

// 连接池参数。
const (
	MaxIdleConns        = 100
	MaxIdleConnsPerHost = 10
	IdleConnTimeout     = 90 * time.Second
)

// 响应完成状态。
const (
	StatusComplete = "complete"
)

// 重试参数。
const (
	DefaultMaxAttempts = 3
	DefaultBaseDelay   = 1 * time.Second
	DefaultMaxDelay    = 30 * time.Second
)

// 流式事件类型。
const (
	StreamEventStart   = "stream-start"
	StreamEventContent = "content"
	StreamEventEnd     = "stream-end"
	StreamEventError   = "error"
)

// 消息生命周期事件。
const (
	EventMessageStart = "message_start"
	EventMessageDelta = "message_delta"
	EventMessageStop  = "message_stop"
)

// 补全请求默认参数。
const (
	DefaultCompletionMaxTokens = 2048
	DefaultFrequencyPenalty    = 0.0
	DefaultPresencePenalty     = 0.0
)
