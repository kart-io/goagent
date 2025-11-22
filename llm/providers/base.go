package providers

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
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

// NewBaseProviderWithConfig creates a BaseProvider from an existing config (for backward compatibility).
func NewBaseProviderWithConfig(config *agentllm.LLMOptions) *BaseProvider {
	if config == nil {
		config = agentllm.DefaultLLMOptions()
	}
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

// ConfigToOptions converts LLMOptions to a list of ClientOptions (for backward compatibility).
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
		if !isRetryable(err) {
			return zero, err
		}

		// Last attempt failed
		if attempt == maxAttempts {
			return zero, agentErrors.ErrorWithRetry(err, attempt, maxAttempts)
		}

		// Exponential backoff with jitter
		delay := baseDelay * time.Duration(1<<uint(attempt-1))
		if cfg.MaxDelay > 0 && delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
		jitter := time.Duration(rand.Int63n(int64(delay) / 2))

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
		retryAfter := parseRetryAfter(err.Headers["Retry-After"])
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
