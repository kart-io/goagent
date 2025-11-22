package constants

// Provider 定义 LLM 提供商类型
type Provider string

const (
	ProviderOpenAI      Provider = "openai"
	ProviderGemini      Provider = "gemini"
	ProviderDeepSeek    Provider = "deepseek"
	ProviderOllama      Provider = "ollama"
	ProviderSiliconFlow Provider = "siliconflow"
	ProviderKimi        Provider = "kimi"
	ProviderCustom      Provider = "custom"

	// New providers
	ProviderAnthropic   Provider = "anthropic"
	ProviderCohere      Provider = "cohere"
	ProviderHuggingFace Provider = "huggingface"
)

// Environment variable names for API keys and configuration
const (
	// OpenAI
	EnvOpenAIAPIKey  = "OPENAI_API_KEY"
	EnvOpenAIBaseURL = "OPENAI_BASE_URL"
	EnvOpenAIModel   = "OPENAI_MODEL"

	// Anthropic
	EnvAnthropicAPIKey  = "ANTHROPIC_API_KEY"
	EnvAnthropicBaseURL = "ANTHROPIC_BASE_URL"
	EnvAnthropicModel   = "ANTHROPIC_MODEL"

	// Cohere
	EnvCohereAPIKey  = "COHERE_API_KEY"
	EnvCohereBaseURL = "COHERE_BASE_URL"
	EnvCohereModel   = "COHERE_MODEL"

	// Hugging Face
	EnvHuggingFaceAPIKey  = "HUGGINGFACE_API_KEY"
	EnvHuggingFaceBaseURL = "HUGGINGFACE_BASE_URL"
	EnvHuggingFaceModel   = "HUGGINGFACE_MODEL"

	// Kimi (Moonshot)
	EnvKimiAPIKey  = "KIMI_API_KEY"
	EnvKimiBaseURL = "KIMI_BASE_URL"
	EnvKimiModel   = "KIMI_MODEL"

	// SiliconFlow
	EnvSiliconFlowAPIKey  = "SILICONFLOW_API_KEY"
	EnvSiliconFlowBaseURL = "SILICONFLOW_BASE_URL"
	EnvSiliconFlowModel   = "SILICONFLOW_MODEL"

	// DeepSeek
	EnvDeepSeekAPIKey  = "DEEPSEEK_API_KEY"
	EnvDeepSeekBaseURL = "DEEPSEEK_BASE_URL"
	EnvDeepSeekModel   = "DEEPSEEK_MODEL"

	// Gemini
	EnvGeminiAPIKey  = "GEMINI_API_KEY"
	EnvGeminiBaseURL = "GEMINI_BASE_URL"
	EnvGeminiModel   = "GEMINI_MODEL"

	// Ollama
	EnvOllamaBaseURL = "OLLAMA_BASE_URL"
	EnvOllamaModel   = "OLLAMA_MODEL"
)

// Error field constants
const (
	ErrorFieldAPIKey  = "api_key"
	ErrorFieldBaseURL = "base_url"
	ErrorFieldModel   = "model"
	ErrorFieldTimeout = "timeout"
)

// LLM Parameter Keys define common parameter names for LLM requests
const (
	// ParamTemperature controls randomness in generation (0.0-2.0)
	ParamTemperature = "temperature"
	// ParamMaxTokens limits the maximum number of tokens to generate
	ParamMaxTokens = "max_tokens"
	// ParamTopP controls nucleus sampling probability mass (0.0-1.0)
	ParamTopP = "top_p"
	// ParamTopK controls top-k sampling (number of top tokens to consider)
	ParamTopK = "top_k"
	// ParamFrequencyPenalty penalizes frequent tokens (-2.0 to 2.0)
	ParamFrequencyPenalty = "frequency_penalty"
	// ParamPresencePenalty penalizes tokens based on presence (-2.0 to 2.0)
	ParamPresencePenalty = "presence_penalty"
	// ParamStop defines stop sequences for generation
	ParamStop = "stop"
	// ParamStream enables streaming responses
	ParamStream = "stream"
	// ParamN specifies number of completions to generate
	ParamN = "n"
	// ParamLogprobs enables log probabilities in response
	ParamLogprobs = "logprobs"
	// ParamEcho echoes the prompt in the response
	ParamEcho = "echo"
	// ParamSeed sets random seed for reproducibility
	ParamSeed = "seed"
)

// Message Field Names define fields in message structures
const (
	// MessageFieldRole represents the role field in a message
	MessageFieldRole = "role"
	// MessageFieldContent represents the content field in a message
	MessageFieldContent = "content"
	// MessageFieldName represents the name field in a message
	MessageFieldName = "name"
	// MessageFieldFunctionCall represents a function call in a message
	MessageFieldFunctionCall = "function_call"
	// MessageFieldToolCalls represents tool calls in a message
	MessageFieldToolCalls = "tool_calls"
)

// Tool and Function Call Constants
const (
	// ToolTypeFunction represents a function tool type
	ToolTypeFunction = "function"
	// FunctionCallAuto enables automatic function calling
	FunctionCallAuto = "auto"
	// FunctionCallNone disables function calling
	FunctionCallNone = "none"
)

// Response Field Names
const (
	// ResponseFieldID represents the response ID field
	ResponseFieldID = "id"
	// ResponseFieldObject represents the object type field
	ResponseFieldObject = "object"
	// ResponseFieldCreated represents the creation timestamp field
	ResponseFieldCreated = "created"
	// ResponseFieldModel represents the model field in response
	ResponseFieldModel = "model"
	// ResponseFieldChoices represents the choices array field
	ResponseFieldChoices = "choices"
	// ResponseFieldUsage represents the usage statistics field
	ResponseFieldUsage = "usage"
)

// Token Usage Field Names
const (
	// UsageFieldPromptTokens represents prompt token count
	UsageFieldPromptTokens = "prompt_tokens"
	// UsageFieldCompletionTokens represents completion token count
	UsageFieldCompletionTokens = "completion_tokens"
	// UsageFieldTotalTokens represents total token count
	UsageFieldTotalTokens = "total_tokens"
)

// Stream Event Types
const (
	// StreamEventChunk represents a content chunk event
	StreamEventChunk = "chunk"
	// StreamEventDone represents stream completion event
	StreamEventDone = "done"
	// StreamEventToolCall represents a tool call event
	StreamEventToolCall = "tool_call"
)
