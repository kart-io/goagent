package common

import (
	"context"
	"fmt"
	"math"
	mathrand "math/rand"
	"os"
	"time"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"

	shared2 "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/utils/httpclient"
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
func (b *BaseProvider) ApplyProviderDefaults(provider shared2.Provider, defaultBaseURL, defaultModel string, envBaseURL, envModel string) {
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
	if config.EmbeddingModel != "" {
		opts = append(opts, agentllm.WithEmbeddingModel(config.EmbeddingModel))
	}
	if config.MaxTokens > 0 {
		opts = append(opts, agentllm.WithMaxTokens(config.MaxTokens))
	}
	if config.TemperatureSet || config.Temperature > 0 {
		opts = append(opts, agentllm.WithTemperature(config.Temperature))
	}
	if config.Timeout > 0 {
		opts = append(opts, agentllm.WithTimeout(time.Duration(config.Timeout)*time.Second))
	}
	if config.Reasoning != nil {
		opts = append(opts, agentllm.WithReasoning(*config.Reasoning))
	}
	if config.TopP > 0 {
		opts = append(opts, agentllm.WithTopP(config.TopP))
	}
	if config.SystemPrompt != "" {
		opts = append(opts, agentllm.WithSystemPrompt(config.SystemPrompt))
	}
	if len(config.CustomHeaders) > 0 {
		opts = append(opts, agentllm.WithCustomHeaders(config.CustomHeaders))
	}
	return opts
}

// EnsureAPIKey validates and sets the API key, supporting environment variable fallback.
func (b *BaseProvider) EnsureAPIKey(envVar string, providerName shared2.Provider) error {
	if b.Config.Reasoning != nil && providerName != shared2.ProviderOpenAI && providerName != shared2.ProviderDeepSeek && providerName != shared2.ProviderGemini {
		return b.RejectReasoning(nil)
	}
	if b.Config.APIKey == "" {
		b.Config.APIKey = os.Getenv(envVar)
	}
	if b.Config.APIKey == "" {
		return agentErrors.NewInvalidConfigError(string(providerName), agentErrors.ErrorFieldAPIKey, fmt.Sprintf(agentErrors.MessageAPIKeyMissing, string(providerName)))
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
	return shared2.DefaultProviderMaxTokens
}

// GetTemperature returns the temperature parameter with fallback to default value.
func (b *BaseProvider) GetTemperature(reqTemperature float64) float64 {
	if reqTemperature > 0 {
		return reqTemperature
	}
	if b.Config.TemperatureSet || b.Config.Temperature > 0 {
		return b.Config.Temperature
	}
	return shared2.DefaultTemperature
}

// ResolveTemperature 在请求显式设置（包括零）时优先使用请求，否则沿用客户端与默认值回落。
func (b *BaseProvider) ResolveTemperature(req *agentllm.CompletionRequest) float64 {
	if req == nil {
		return b.GetTemperature(0)
	}
	if req.TemperatureSet {
		return req.Temperature
	}
	return b.GetTemperature(req.Temperature)
}

// GetTimeout returns the timeout duration with fallback to default value.
func (b *BaseProvider) GetTimeout() time.Duration {
	if b.Config.Timeout > 0 {
		return time.Duration(b.Config.Timeout) * time.Second
	}
	return shared2.DefaultProviderTimeout
}

// GetTopP returns the TopP parameter with fallback to default value.
func (b *BaseProvider) GetTopP(reqTopP float64) float64 {
	if reqTopP > 0 {
		return reqTopP
	}
	if b.Config.TopP > 0 {
		return b.Config.TopP
	}
	return shared2.DefaultTopP
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
	Timeout time.Duration `json:"timeout"`
	// Headers contains default HTTP headers to include in requests
	Headers map[string]string `json:"headers"`
	// BaseURL is the base URL for API requests
	BaseURL string `json:"base_url"`
}

// NewHTTPClient creates a configured HTTP client using the provider's settings.
// It merges the provided headers with any custom headers from the config.
//
// 性能优化：使用全局客户端缓存，减少连接建立开销（10-20ms/次）
// 相同配置（BaseURL、Headers、超时等）的客户端实例将被复用
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

	// 使用缓存机制创建或获取客户端
	// 相同配置的客户端将被复用，显著提升并发场景性能
	return httpclient.GetOrCreateClient(&httpclient.Config{
		Timeout: timeout,
		Headers: headers,
		BaseURL: cfg.BaseURL,
	})
}

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts
	MaxAttempts int `json:"max_attempts"`
	// BaseDelay is the initial delay between retries
	BaseDelay time.Duration `json:"base_delay"`
	// MaxDelay is the maximum delay between retries
	MaxDelay time.Duration `json:"max_delay"`
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: shared2.DefaultMaxAttempts,
		BaseDelay:   shared2.DefaultBaseDelay,
		MaxDelay:    shared2.DefaultMaxDelay,
	}
}

// ExecuteFunc is a function type for executing a single request.
// It should return the response and any error encountered.
type ExecuteFunc[T any] func(ctx context.Context) (T, error)

// ExecuteWithRetry executes a function with exponential backoff retry logic.
// It respects context cancellation and uses test-friendly delay settings.
func ExecuteWithRetry[T any](ctx context.Context, cfg RetryConfig, providerName string, execute ExecuteFunc[T]) (T, error) {
	var zero T
	if cfg.BaseDelay < 0 || cfg.MaxDelay < 0 {
		return zero, agentErrors.NewInvalidConfigError(providerName, "retry_delay", "重试延迟不能为负数")
	}

	// Use shorter delays in test environment
	baseDelay := cfg.BaseDelay
	if testDelay, ok := ctx.Value("test_retry_delay").(time.Duration); ok && testDelay > 0 {
		baseDelay = testDelay
	} else if os.Getenv("GO_TEST_MODE") == "true" {
		baseDelay = 10 * time.Millisecond
	}

	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = shared2.DefaultMaxAttempts
	}

	delay := baseDelay
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, retryContextError(err)
		}
		result, err := execute(ctx)
		if err == nil {
			return result, nil
		}

		if contextErr := ctx.Err(); contextErr != nil {
			return zero, retryContextError(contextErr)
		}

		// Check if error is retryable
		if !IsRetryable(err) {
			return zero, err
		}

		// Last attempt failed
		if attempt == maxAttempts {
			return zero, agentErrors.ErrorWithRetry(err, attempt, maxAttempts)
		}

		// 纳秒级或零延迟不需要随机抖动；饱和运算避免大退避值溢出。
		if cfg.MaxDelay > 0 && delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
		wait := delay
		if bound := int64(delay) / 2; bound > 0 {
			wait += min(time.Duration(mathrand.Int63n(bound)), time.Duration(math.MaxInt64)-wait)
		}
		if cfg.MaxDelay > 0 && wait > cfg.MaxDelay {
			wait = cfg.MaxDelay
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, retryContextError(ctx.Err())
		case <-timer.C:
		}
		delay += min(delay, time.Duration(math.MaxInt64)-delay)
	}

	return zero, agentErrors.NewInternalError(providerName, "execute_with_retry", agentErrors.Newf(agentErrors.CodeInternal, "%s", agentErrors.MessageMaxRetriesExceeded).
		WithComponent("llm").
		WithOperation("execute_with_retry").
		WithContext("provider", providerName))
}

// retryContextError 保留原始上下文原因，调用方可继续使用 errors.Is。
func retryContextError(err error) error {
	if err == context.DeadlineExceeded {
		return agentErrors.NewContextTimeoutError("llm_request", 0)
	}
	return agentErrors.NewContextCanceledError("llm_request")
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
	return WithHTTPStatus(mapHTTPError(err, providerName, model, parseError), err)
}

func mapHTTPError(err HTTPError, providerName, model string, parseError func(body string) string) *agentErrors.AgentError {
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
		return agentErrors.NewInvalidInputError(providerName, "request", agentErrors.MessageBadRequest)
	case 401:
		if errorMsg != "" {
			return agentErrors.NewInvalidConfigError(providerName, agentErrors.ErrorFieldAPIKey, errorMsg)
		}
		return agentErrors.NewInvalidConfigError(providerName, agentErrors.ErrorFieldAPIKey, agentErrors.MessageInvalidAPIKey)
	case 403:
		if errorMsg != "" {
			return agentErrors.NewInvalidConfigError(providerName, agentErrors.ErrorFieldAPIKey, errorMsg)
		}
		return agentErrors.NewInvalidConfigError(providerName, agentErrors.ErrorFieldAPIKey, agentErrors.MessageAPIKeyLacksPermissions)
	case 404:
		if errorMsg != "" {
			return agentErrors.NewLLMResponseError(providerName, model, errorMsg)
		}
		return agentErrors.NewLLMResponseError(providerName, model, agentErrors.MessageModelNotFound)
	case 429:
		retryAfter := ParseRetryAfter(err.Headers["Retry-After"])
		return agentErrors.NewLLMRateLimitError(providerName, model, retryAfter)
	case 500, 502, 503, 504:
		if errorMsg != "" {
			return agentErrors.NewLLMRequestError(providerName, model, agentErrors.Newf(agentErrors.CodeLLMRequest, "server error: %s", errorMsg).
				WithComponent("llm").
				WithOperation("request").
				WithContext("provider", providerName).
				WithContext("model", model))
		}
		return agentErrors.NewLLMRequestError(providerName, model, agentErrors.Newf(agentErrors.CodeLLMRequest, "server error: %d", err.StatusCode).
			WithComponent("llm").
			WithOperation("request").
			WithContext("provider", providerName).
			WithContext("model", model))
	default:
		return agentErrors.NewLLMRequestError(providerName, model, agentErrors.Newf(agentErrors.CodeLLMRequest, "unexpected status: %d", err.StatusCode).
			WithComponent("llm").
			WithOperation("request").
			WithContext("provider", providerName).
			WithContext("model", model))
	}
}

// WithHTTPStatus 把非 2xx 响应的状态码与 Retry-After 作为 *llm.ProviderError 挂到 e 的原因上，
// 使 llm.ClassifyError 能判定；错误码不变。e 须是刚构造、尚未共享的错误。
func WithHTTPStatus(e *agentErrors.AgentError, resp HTTPError) *agentErrors.AgentError {
	cause := e.Cause
	if cause == nil {
		cause = agentErrors.Newf(agentErrors.CodeLLMRequest, "HTTP %d", resp.StatusCode).
			WithComponent("llm").
			WithOperation("request").
			WithContext("status_code", resp.StatusCode)
	}
	e.Cause = &agentllm.ProviderError{
		StatusCode: resp.StatusCode,
		RetryAfter: RetryAfterDuration(resp.Headers["Retry-After"], time.Now()),
		Err:        cause,
	}
	return e
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
