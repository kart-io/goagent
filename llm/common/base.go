package common

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	mathrand "math/rand"
	"os"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/httpclient"
)

// insecureRand is a fallback random number generator used when crypto/rand fails.
// This maintains jitter effect even in degraded scenarios, preventing thundering herd.
var (
	insecureRand     *mathrand.Rand
	insecureRandOnce sync.Once
	insecureRandMu   sync.Mutex
)

// BaseProvider encapsulates common configuration and logic for all LLM providers.
// It provides unified handling of configuration, parameter resolution, HTTP client
// creation, and retry logic.
type BaseProvider struct {
	Config *agentllm.LLMOptions
}

// NewBaseProvider initializes a BaseProvider with unified options handling.
func NewBaseProvider(opts ...agentllm.ClientOption) *BaseProvider {
	config := agentllm.NewLLMOptionsWithOptions(opts...)
	return &BaseProvider{
		Config: config,
	}
}

// ApplyProviderDefaults applies provider-specific default values.
func (b *BaseProvider) ApplyProviderDefaults(provider constants.Provider, defaultBaseURL, defaultModel string, envBaseURL, envModel string) {
	b.Config.Provider = provider
	b.EnsureBaseURL(envBaseURL, defaultBaseURL)
	b.EnsureModel(envModel, defaultModel)
}

// ConfigToOptions converts LLMOptions to a list of ClientOptions.
// This is useful for factory methods that need to accept LLMOptions.
func ConfigToOptions(config *agentllm.LLMOptions) []agentllm.ClientOption {
	if config == nil {
		return nil
	}

	var opts []agentllm.ClientOption
	if config.Provider != "" {
		opts = append(opts, agentllm.WithProvider(config.Provider))
	}
	if config.APIKey != "" {
		opts = append(opts, agentllm.WithAPIKey(config.APIKey))
	}
	if config.BaseURL != "" {
		opts = append(opts, agentllm.WithBaseURL(config.BaseURL))
	}
	if config.Model != "" {
		opts = append(opts, agentllm.WithModel(config.Model))
	}
	if config.MaxTokens > 0 {
		opts = append(opts, agentllm.WithMaxTokens(config.MaxTokens))
	}
	if config.Temperature > 0 {
		opts = append(opts, agentllm.WithTemperature(config.Temperature))
	}
	if config.Timeout > 0 {
		opts = append(opts, agentllm.WithTimeout(time.Duration(config.Timeout)*time.Second))
	}
	if config.TopP > 0 {
		opts = append(opts, agentllm.WithTopP(config.TopP))
	}
	if config.ProxyURL != "" {
		opts = append(opts, agentllm.WithProxy(config.ProxyURL))
	}
	if config.RetryCount > 0 {
		opts = append(opts, agentllm.WithRetryCount(config.RetryCount))
	}
	if config.RetryDelay > 0 {
		opts = append(opts, agentllm.WithRetryDelay(config.RetryDelay))
	}
	if config.RateLimitRPM > 0 {
		opts = append(opts, agentllm.WithRateLimiting(config.RateLimitRPM))
	}
	if config.SystemPrompt != "" {
		opts = append(opts, agentllm.WithSystemPrompt(config.SystemPrompt))
	}
	if config.CacheEnabled {
		opts = append(opts, agentllm.WithCache(config.CacheEnabled, config.CacheTTL))
	}
	if config.StreamingEnabled {
		opts = append(opts, agentllm.WithStreamingEnabled(config.StreamingEnabled))
	}
	if config.OrganizationID != "" {
		opts = append(opts, agentllm.WithOrganizationID(config.OrganizationID))
	}
	if len(config.CustomHeaders) > 0 {
		opts = append(opts, agentllm.WithCustomHeaders(config.CustomHeaders))
	}
	return opts
}

// EnsureAPIKey validates and sets the API key, supporting environment variable fallback.
func (b *BaseProvider) EnsureAPIKey(envVar string, providerName constants.Provider) error {
	if b.Config.APIKey == "" {
		b.Config.APIKey = os.Getenv(envVar)
	}
	if b.Config.APIKey == "" {
		return agentErrors.NewInvalidConfigError(string(providerName), constants.ErrorFieldAPIKey, fmt.Sprintf(constants.ErrAPIKeyMissing, string(providerName)))
	}
	return nil
}

// EnsureBaseURL validates and sets the base URL, supporting environment variable fallback and default value.
func (b *BaseProvider) EnsureBaseURL(envVar string, defaultURL string) {
	if b.Config.BaseURL == "" {
		b.Config.BaseURL = os.Getenv(envVar)
	}
	if b.Config.BaseURL == "" {
		b.Config.BaseURL = defaultURL
	}
}

// EnsureModel validates and sets the model, supporting environment variable fallback and default value.
func (b *BaseProvider) EnsureModel(envVar string, defaultModel string) {
	if b.Config.Model == "" {
		b.Config.Model = os.Getenv(envVar)
	}
	if b.Config.Model == "" {
		b.Config.Model = defaultModel
	}
}

// GetModel returns the model name, preferring the request model over the configured model.
func (b *BaseProvider) GetModel(reqModel string) string {
	if reqModel != "" {
		return reqModel
	}
	return b.Config.Model
}

// GetMaxTokens returns the max tokens value with fallback to default.
func (b *BaseProvider) GetMaxTokens(reqMaxTokens int) int {
	if reqMaxTokens > 0 {
		return reqMaxTokens
	}
	if b.Config.MaxTokens > 0 {
		return b.Config.MaxTokens
	}
	return constants.DefaultMaxTokens
}

// GetTemperature returns the temperature parameter with fallback to default value.
func (b *BaseProvider) GetTemperature(reqTemperature float64) float64 {
	if reqTemperature > 0 {
		return reqTemperature
	}
	if b.Config.Temperature > 0 {
		return b.Config.Temperature
	}
	return constants.DefaultTemperature
}

// GetTimeout returns the timeout duration with fallback to default value.
func (b *BaseProvider) GetTimeout() time.Duration {
	if b.Config.Timeout > 0 {
		return time.Duration(b.Config.Timeout) * time.Second
	}
	return constants.DefaultTimeout
}

// GetTopP returns the TopP parameter with fallback to default value.
func (b *BaseProvider) GetTopP(reqTopP float64) float64 {
	if reqTopP > 0 {
		return reqTopP
	}
	if b.Config.TopP > 0 {
		return b.Config.TopP
	}
	return constants.DefaultTopP
}

// ModelName returns the configured model name.
// This is a convenience method that delegates to GetModel with an empty request model.
func (b *BaseProvider) ModelName() string {
	return b.GetModel("")
}

// MaxTokensValue returns the configured max tokens value.
// This is a convenience method that delegates to GetMaxTokens with zero request tokens.
func (b *BaseProvider) MaxTokensValue() int {
	return b.GetMaxTokens(0)
}

// ProviderName returns the provider name as a string.
func (b *BaseProvider) ProviderName() string {
	return string(b.Config.Provider)
}

// HTTPClientConfig holds configuration for creating HTTP clients.
type HTTPClientConfig struct {
	// Timeout is the request timeout duration
	Timeout time.Duration
	// Headers contains default HTTP headers to include in requests
	Headers map[string]string
	// BaseURL is the base URL for API requests
	BaseURL string
}

// NewHTTPClient creates a configured HTTP client using the provider's settings.
// It merges the provided headers with any custom headers from the config.
func (b *BaseProvider) NewHTTPClient(cfg HTTPClientConfig) *httpclient.Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = b.GetTimeout()
	}

	headers := make(map[string]string)
	// Apply provided headers first
	for k, v := range cfg.Headers {
		headers[k] = v
	}
	// Merge with custom headers from config (config headers take precedence)
	for k, v := range b.Config.CustomHeaders {
		headers[k] = v
	}

	return httpclient.NewClient(&httpclient.Config{
		Timeout: timeout,
		Headers: headers,
	})
}

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts
	MaxAttempts int
	// BaseDelay is the initial delay between retries
	BaseDelay time.Duration
	// MaxDelay is the maximum delay between retries
	MaxDelay time.Duration
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: constants.DefaultMaxAttempts,
		BaseDelay:   constants.DefaultBaseDelay,
		MaxDelay:    constants.DefaultMaxDelay,
	}
}

// secureRandomInt63n generates a cryptographically secure random int64 in [0, n).
// If n <= 0, it returns 0.
//
// Fallback Strategy:
// When crypto/rand fails (e.g., due to entropy exhaustion or system issues), this function
// falls back to math/rand to maintain jitter effect. This prevents thundering herd where
// all retry attempts happen simultaneously, which could overwhelm the backend service.
//
// While the fallback is not cryptographically secure, it's sufficient for retry jitter
// since the goal is randomness distribution, not cryptographic security.
func SecureRandomInt63n(n int64) int64 {
	if n <= 0 {
		return 0
	}

	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback to insecure random to maintain jitter effect
		// This prevents thundering herd when crypto/rand fails
		insecureRandOnce.Do(func() {
			insecureRand = mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
		})

		insecureRandMu.Lock()
		result := insecureRand.Int63n(n)
		insecureRandMu.Unlock()

		return result
	}

	// Convert bytes to uint64, then to int64 in range [0, n)
	randomUint64 := binary.BigEndian.Uint64(b[:])
	return int64(randomUint64 % uint64(n))
}

// ExecuteFunc is a function type for executing a single request.
// It should return the response and any error encountered.
type ExecuteFunc[T any] func(ctx context.Context) (T, error)

// ExecuteWithRetry executes a function with exponential backoff retry logic.
// It respects context cancellation and uses test-friendly delay settings.
func ExecuteWithRetry[T any](ctx context.Context, cfg RetryConfig, providerName string, execute ExecuteFunc[T]) (T, error) {
	var zero T

	// Use shorter delays in test environment
	baseDelay := cfg.BaseDelay
	if testDelay, ok := ctx.Value("test_retry_delay").(time.Duration); ok && testDelay > 0 {
		baseDelay = testDelay
	} else if os.Getenv("GO_TEST_MODE") == "true" {
		baseDelay = 10 * time.Millisecond
	}

	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = constants.DefaultMaxAttempts
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := execute(ctx)
		if err == nil {
			return result, nil
		}

		// Check if error is retryable
		if !IsRetryable(err) {
			return zero, err
		}

		// Last attempt failed
		if attempt == maxAttempts {
			return zero, agentErrors.ErrorWithRetry(err, attempt, maxAttempts)
		}

		// Exponential backoff with cryptographically secure jitter
		delay := baseDelay * time.Duration(1<<uint(attempt-1))
		if cfg.MaxDelay > 0 && delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
		jitter := time.Duration(SecureRandomInt63n(int64(delay) / 2))

		select {
		case <-ctx.Done():
			return zero, agentErrors.NewContextCanceledError("llm_request")
		case <-time.After(delay + jitter):
			// Continue to next attempt
		}
	}

	return zero, agentErrors.NewInternalError(providerName, "execute_with_retry", fmt.Errorf("%s", constants.ErrMaxRetriesExceeded))
}

// HTTPError represents an HTTP error with status code and response body.
type HTTPError struct {
	StatusCode int
	Body       string
	Headers    map[string]string
}

// ProviderCapabilities holds the capabilities supported by a provider.
// Embed this in providers to easily implement the CapabilityChecker interface.
type ProviderCapabilities struct {
	caps map[agentllm.Capability]bool
}

// NewProviderCapabilities creates a new ProviderCapabilities with the given capabilities.
func NewProviderCapabilities(capabilities ...agentllm.Capability) *ProviderCapabilities {
	caps := make(map[agentllm.Capability]bool)
	for _, cap := range capabilities {
		caps[cap] = true
	}
	return &ProviderCapabilities{caps: caps}
}

// HasCapability checks if the provider supports the given capability.
func (p *ProviderCapabilities) HasCapability(cap agentllm.Capability) bool {
	if p.caps == nil {
		return false
	}
	return p.caps[cap]
}

// Capabilities returns all supported capabilities.
func (p *ProviderCapabilities) Capabilities() []agentllm.Capability {
	if p.caps == nil {
		return nil
	}
	result := make([]agentllm.Capability, 0, len(p.caps))
	for cap := range p.caps {
		result = append(result, cap)
	}
	return result
}

// AddCapability adds a capability to the provider.
func (p *ProviderCapabilities) AddCapability(cap agentllm.Capability) {
	if p.caps == nil {
		p.caps = make(map[agentllm.Capability]bool)
	}
	p.caps[cap] = true
}

// MapHTTPError maps an HTTP error to an appropriate AgentError based on status code.
// This provides consistent error handling across all providers.
func MapHTTPError(err HTTPError, providerName, model string, parseError func(body string) string) error {
	// Try to get error message from response body
	errorMsg := ""
	if parseError != nil {
		errorMsg = parseError(err.Body)
	}

	switch err.StatusCode {
	case 400:
		if errorMsg != "" {
			return agentErrors.NewInvalidInputError(providerName, "request", errorMsg)
		}
		return agentErrors.NewInvalidInputError(providerName, "request", constants.StatusBadRequest)
	case 401:
		if errorMsg != "" {
			return agentErrors.NewInvalidConfigError(providerName, constants.ErrorFieldAPIKey, errorMsg)
		}
		return agentErrors.NewInvalidConfigError(providerName, constants.ErrorFieldAPIKey, constants.StatusInvalidAPIKey)
	case 403:
		if errorMsg != "" {
			return agentErrors.NewInvalidConfigError(providerName, constants.ErrorFieldAPIKey, errorMsg)
		}
		return agentErrors.NewInvalidConfigError(providerName, constants.ErrorFieldAPIKey, constants.StatusAPIKeyLacksPermissions)
	case 404:
		if errorMsg != "" {
			return agentErrors.NewLLMResponseError(providerName, model, errorMsg)
		}
		return agentErrors.NewLLMResponseError(providerName, model, constants.StatusModelNotFound)
	case 429:
		retryAfter := ParseRetryAfter(err.Headers["Retry-After"])
		return agentErrors.NewLLMRateLimitError(providerName, model, retryAfter)
	case 500, 502, 503, 504:
		if errorMsg != "" {
			return agentErrors.NewLLMRequestError(providerName, model, fmt.Errorf("server error: %s", errorMsg))
		}
		return agentErrors.NewLLMRequestError(providerName, model, fmt.Errorf("server error: %d", err.StatusCode))
	default:
		return agentErrors.NewLLMRequestError(providerName, model, fmt.Errorf("unexpected status: %d", err.StatusCode))
	}
}

// RestyResponseToHTTPError converts a resty.Response to an HTTPError.
func RestyResponseToHTTPError(resp *resty.Response) HTTPError {
	headers := make(map[string]string)
	for k, v := range resp.Header() {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return HTTPError{
		StatusCode: resp.StatusCode(),
		Body:       resp.String(),
		Headers:    headers,
	}
}

// ============================================================================
// Message Conversion Utilities
// ============================================================================

// MessageConverter is a function type that converts an agentllm.Message to a provider-specific message type.
// This enables generic message conversion across different providers.
type MessageConverter[T any] func(msg agentllm.Message) T

// ConvertMessages converts a slice of agentllm.Message to a slice of provider-specific messages
// using the provided converter function. This reduces boilerplate code across providers.
//
// Example usage:
//
//	messages := ConvertMessages(req.Messages, func(msg agentllm.Message) OpenAIMessage {
//	    return OpenAIMessage{Role: msg.Role, Content: msg.Content, Name: msg.Name}
//	})
func ConvertMessages[T any](messages []agentllm.Message, converter MessageConverter[T]) []T {
	result := make([]T, len(messages))
	for i, msg := range messages {
		result[i] = converter(msg)
	}
	return result
}

// StandardMessage represents a common message format used by OpenAI-compatible APIs.
// This struct can be used directly by providers that follow the OpenAI message format.
type StandardMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// ToStandardMessage converts an agentllm.Message to a StandardMessage.
// Use this for providers that follow OpenAI-compatible message formats.
func ToStandardMessage(msg agentllm.Message) StandardMessage {
	return StandardMessage{
		Role:    msg.Role,
		Content: msg.Content,
		Name:    msg.Name,
	}
}

// ConvertToStandardMessages is a convenience function that converts agentllm.Message slice
// to StandardMessage slice. Useful for OpenAI-compatible providers.
func ConvertToStandardMessages(messages []agentllm.Message) []StandardMessage {
	return ConvertMessages(messages, ToStandardMessage)
}

// RoleMapper is a function type for mapping standard roles to provider-specific roles.
type RoleMapper func(role string) string

// DefaultRoleMapper returns the role unchanged (for OpenAI-compatible providers).
func DefaultRoleMapper(role string) string {
	return role
}

// ConvertMessagesWithRoleMapping converts messages with custom role mapping.
// This is useful for providers like Cohere that use different role names.
//
// Example usage:
//
//	messages := ConvertMessagesWithRoleMapping(req.Messages, func(role string) string {
//	    switch role {
//	    case "user": return "USER"
//	    case "assistant": return "CHATBOT"
//	    case "system": return "SYSTEM"
//	    default: return "USER"
//	    }
//	}, func(msg agentllm.Message, mappedRole string) CohereMessage {
//	    return CohereMessage{Role: mappedRole, Message: msg.Content}
//	})
func ConvertMessagesWithRoleMapping[T any](
	messages []agentllm.Message,
	roleMapper RoleMapper,
	converter func(msg agentllm.Message, mappedRole string) T,
) []T {
	result := make([]T, len(messages))
	for i, msg := range messages {
		mappedRole := roleMapper(msg.Role)
		result[i] = converter(msg, mappedRole)
	}
	return result
}

// MessagesToPrompt concatenates messages into a single prompt string.
// This is useful for providers like HuggingFace that expect a single input string.
// The format function determines how each message is formatted.
//
// Example usage:
//
//	prompt := MessagesToPrompt(messages, func(msg agentllm.Message) string {
//	    return fmt.Sprintf("%s: %s\n", strings.Title(msg.Role), msg.Content)
//	})
func MessagesToPrompt(messages []agentllm.Message, format func(msg agentllm.Message) string) string {
	var result string
	for _, msg := range messages {
		result += format(msg)
	}
	return result
}

// DefaultPromptFormatter formats a message in the standard "Role: Content" format.
func DefaultPromptFormatter(msg agentllm.Message) string {
	roleNames := map[string]string{
		"system":    "System",
		"user":      "User",
		"assistant": "Assistant",
	}
	roleName := roleNames[msg.Role]
	if roleName == "" {
		roleName = msg.Role
	}
	return fmt.Sprintf("%s: %s\n", roleName, msg.Content)
}
