package constants

import (
	"time"
)

// Default LLM parameters
const (
	DefaultMaxTokens   = 1000
	DefaultTemperature = 0.7
	DefaultTimeout     = 30 * time.Second
	DefaultTopP        = 1.0
	DefaultTopK        = 0
)

// Base URLs for different providers
const (
	// Anthropic
	AnthropicBaseURL      = "https://api.anthropic.com"
	AnthropicDefaultModel = "claude-3-5-sonnet-20241022"
	AnthropicAPIVersion   = "2023-06-01"
	AnthropicMaxAttempts  = 3
	AnthropicBaseDelay    = 1 * time.Second
	AnthropicMaxDelay     = 30 * time.Second
	AnthropicMessagesPath = "/v1/messages"

	// Cohere
	CohereBaseURL      = "https://api.cohere.ai"
	CohereDefaultModel = "command-r-plus"
	CohereChatEndpoint = "/v1/chat"
	CohereChatPath     = "/v1/chat"
	CohereMaxAttempts  = 3
	CohereBaseDelay    = 1 * time.Second
	CohereMaxDelay     = 30 * time.Second

	// Hugging Face
	HuggingFaceBaseURL              = "https://api-inference.huggingface.co"
	HuggingFaceDefaultModel         = "meta-llama/Meta-Llama-3-8B-Instruct"
	HuggingFaceDefaultMaxTokens     = 2000
	HuggingFaceTimeout              = 120 * time.Second
	HuggingFaceMaxAttempts          = 5
	HuggingFaceBaseDelay            = 3 * time.Second
	HuggingFaceMaxDelay             = 60 * time.Second
	HuggingFaceDefaultEstimatedTime = 20

	// Kimi (Moonshot AI)
	KimiBaseURL = "https://api.moonshot.cn/v1"

	// SiliconFlow
	SiliconFlowBaseURL = "https://api.siliconflow.cn/v1"

	// DeepSeek
	DeepSeekDefaultModel = "deepseek-chat"
	DeepSeekBaseURL      = "https://api.deepseek.com/v1"
	DeepSeekMaxTokens    = 4096
	DeepSeekTemperature  = 0.7
	DeepSeekTimeout      = 30 * time.Second
	DeepSeekRetryCount   = 3
	DeepSeekRetryDelay   = 1 * time.Second
)

// HTTP Headers
const (
	HeaderContentType      = "Content-Type"
	HeaderAuthorization    = "Authorization"
	HeaderAPIKey           = "x-api-key"
	HeaderXAPIKey          = "x-api-key"
	HeaderAccept           = "Accept"
	HeaderRetryAfter       = "Retry-After"
	HeaderAnthropicVersion = "anthropic-version"
)

// Content Types
const (
	ContentTypeJSON        = "application/json"
	ContentTypeEventStream = "text/event-stream"
	AcceptEventStream      = "text/event-stream"
)

// Authorization prefixes
const (
	AuthBearerPrefix = "Bearer "
	AuthAPIKeyPrefix = "x-api-key: "
)

// Message roles
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"

	// Cohere specific roles
	CohereRoleUser    = "USER"
	CohereRoleChatbot = "CHATBOT"
	CohereRoleSystem  = "SYSTEM"
)

// HTTP connection pooling
const (
	MaxIdleConns        = 100
	MaxIdleConnsPerHost = 10
	IdleConnTimeout     = 90 * time.Second
)

// Error messages
const (
	ErrAPIKeyMissing         = "%s API key is required (set via config or environment variable)"
	ErrFailedMarshalRequest  = "failed to marshal request"
	ErrFailedCreateRequest   = "failed to create HTTP request"
	ErrFailedDecodeResponse  = "failed to decode response"
	ErrEmptyResponseArray    = "empty response array from API"
	ErrMaxRetriesExceeded    = "max retries exceeded"
	ErrStreamingNotSupported = "streaming not supported for this provider"
	ErrNoCompletionChoices   = "no completion choices returned"
	ErrNoChoicesReturned     = "no choices returned in response"
	ErrNoEmbeddingsReturned  = "no embeddings returned"
)

// HTTP Status messages
const (
	StatusComplete               = "complete"
	StatusBadRequest             = "bad request"
	StatusInvalidAPIKey          = "invalid API key"
	StatusAPIKeyLacksPermissions = "API key lacks required permissions"
	StatusModelNotFound          = "model not found"
	StatusEndpointNotFound       = "endpoint not found"
	StatusRateLimitExceeded      = "rate limit exceeded"
	StatusServerError            = "server error"
)

// Retry configuration
const (
	DefaultMaxAttempts = 3
	DefaultBaseDelay   = 1 * time.Second
	DefaultMaxDelay    = 30 * time.Second
)

// Stream event types
const (
	StreamEventStart   = "stream-start"
	StreamEventContent = "content"
	StreamEventEnd     = "stream-end"
	StreamEventError   = "error"
)

// SSE (Server-Sent Events) constants
const (
	SSEDataPrefix  = "data: "
	SSEDoneMessage = "[DONE]"
)

// Stream event type constants
const (
	// Anthropic events
	EventContentBlockDelta = "content_block_delta"
	EventMessageStart      = "message_start"
	EventMessageDelta      = "message_delta"
	EventMessageStop       = "message_stop"

	// Cohere events
	EventTextGeneration = "text-generation"
	EventStreamEnd      = "stream-end"
)
