package llm

import "github.com/kart-io/goagent/interfaces"

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

// ToolCall represents a function/tool call by the LLM
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

// ToolCallResponse represents the response from tool-enabled completion
type ToolCallResponse struct {
	Content   string                 `json:"content"`
	ToolCalls []ToolCall             `json:"tool_calls,omitempty"`
	Usage     *interfaces.TokenUsage `json:"usage,omitempty"`
}

// ToolChunk represents a streaming chunk from tool-enabled completion
type ToolChunk struct {
	Type  string      `json:"type"`  // "content", "tool_call", "tool_name", "tool_args", "error"
	Value interface{} `json:"value"` // Content string, ToolCall, or error
}
