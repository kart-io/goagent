package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sashabaranov/go-openai"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
)

// NewOpenAIWithOptions creates a new OpenAI provider using options pattern
func NewOpenAIWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	// Create config with options
	config := agentllm.NewConfigWithOptions(opts...)

	// Ensure provider is set to OpenAI
	config.Provider = agentllm.ProviderOpenAI

	// Use existing NewOpenAI function
	return NewOpenAI(config)
}

// EnhancedOpenAIProvider extends OpenAIProvider with additional features from config
type EnhancedOpenAIProvider struct {
	*OpenAIProvider
	retryCount   int
	retryDelay   time.Duration
	cacheEnabled bool
	cacheTTL     time.Duration
	streaming    bool
	systemPrompt string
}

// NewEnhancedOpenAI creates an enhanced OpenAI provider with full option support
func NewEnhancedOpenAI(config *agentllm.Config) (*EnhancedOpenAIProvider, error) {
	// Create base provider
	base, err := NewOpenAI(config)
	if err != nil {
		return nil, err
	}

	// Apply enhancements from config
	enhanced := &EnhancedOpenAIProvider{
		OpenAIProvider: base,
		retryCount:     config.RetryCount,
		retryDelay:     config.RetryDelay,
		cacheEnabled:   config.CacheEnabled,
		cacheTTL:       config.CacheTTL,
		streaming:      config.StreamingEnabled,
		systemPrompt:   config.SystemPrompt,
	}

	// Apply custom headers if present
	if len(config.CustomHeaders) > 0 {
		// Note: The go-openai client doesn't directly support custom headers,
		// but we can extend it if needed
		enhanced.applyCustomHeaders(config.CustomHeaders)
	}

	// Apply organization ID if present
	if config.OrganizationID != "" {
		enhanced.applyOrganizationID(config.OrganizationID)
	}

	return enhanced, nil
}

// CompleteWithRetry implements completion with retry logic
func (p *EnhancedOpenAIProvider) CompleteWithRetry(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	var lastErr error
	retryCount := p.retryCount
	if retryCount <= 0 {
		retryCount = 1 // At least try once
	}

	for i := 0; i < retryCount; i++ {
		if i > 0 && p.retryDelay > 0 {
			select {
			case <-time.After(p.retryDelay):
				// Continue after delay
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		resp, err := p.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("failed after %d retries: %w", retryCount, lastErr)
}

// ChatWithSystemPrompt adds system prompt if configured
func (p *EnhancedOpenAIProvider) ChatWithSystemPrompt(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	// Prepend system prompt if configured
	if p.systemPrompt != "" {
		systemMsg := agentllm.SystemMessage(p.systemPrompt)
		messages = append([]agentllm.Message{systemMsg}, messages...)
	}

	// Use retry if configured
	if p.retryCount > 0 {
		return p.CompleteWithRetry(ctx, &agentllm.CompletionRequest{
			Messages: messages,
		})
	}

	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// applyCustomHeaders applies custom headers to the client
func (p *EnhancedOpenAIProvider) applyCustomHeaders(headers map[string]string) {
	// This would require modifying the underlying client
	// For now, we'll store them for future use
	// In a real implementation, you'd modify the HTTP client
}

// applyOrganizationID applies organization ID to the client
func (p *EnhancedOpenAIProvider) applyOrganizationID(orgID string) {
	// The go-openai client supports organization ID in the config
	// We'd need to recreate the client with the org ID
	clientConfig := openai.DefaultConfig(p.config.APIKey)
	clientConfig.OrgID = orgID
	if p.config.BaseURL != "" {
		clientConfig.BaseURL = p.config.BaseURL
	}
	p.client = openai.NewClientWithConfig(clientConfig)
}

// isRetryableError determines if an error should trigger a retry
func isRetryableError(err error) bool {
	// Check for network errors, rate limits, etc.
	if err == nil {
		return false
	}

	// Check for specific error types
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		// Retry on rate limits and server errors
		if apiErr.HTTPStatusCode == 429 || apiErr.HTTPStatusCode >= 500 {
			return true
		}
	}

	// Check for timeout errors
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check for temporary network errors
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	return false
}

// Builder pattern for OpenAI provider
type OpenAIProviderBuilder struct {
	config *agentllm.Config
	opts   []agentllm.ClientOption
}

// NewOpenAIBuilder creates a new builder
func NewOpenAIBuilder() *OpenAIProviderBuilder {
	return &OpenAIProviderBuilder{
		config: agentllm.DefaultClientConfig(),
		opts:   []agentllm.ClientOption{},
	}
}

// WithOption adds an option to the builder
func (b *OpenAIProviderBuilder) WithOption(opt agentllm.ClientOption) *OpenAIProviderBuilder {
	b.opts = append(b.opts, opt)
	return b
}

// WithAPIKey sets the API key
func (b *OpenAIProviderBuilder) WithAPIKey(apiKey string) *OpenAIProviderBuilder {
	return b.WithOption(agentllm.WithAPIKey(apiKey))
}

// WithModel sets the model
func (b *OpenAIProviderBuilder) WithModel(model string) *OpenAIProviderBuilder {
	return b.WithOption(agentllm.WithModel(model))
}

// WithTemperature sets the temperature
func (b *OpenAIProviderBuilder) WithTemperature(temperature float64) *OpenAIProviderBuilder {
	return b.WithOption(agentllm.WithTemperature(temperature))
}

// WithMaxTokens sets max tokens
func (b *OpenAIProviderBuilder) WithMaxTokens(maxTokens int) *OpenAIProviderBuilder {
	return b.WithOption(agentllm.WithMaxTokens(maxTokens))
}

// WithRetry configures retry logic
func (b *OpenAIProviderBuilder) WithRetry(count int, delay time.Duration) *OpenAIProviderBuilder {
	return b.WithOption(agentllm.WithRetryCount(count)).
		WithOption(agentllm.WithRetryDelay(delay))
}

// WithCache enables caching
func (b *OpenAIProviderBuilder) WithCache(ttl time.Duration) *OpenAIProviderBuilder {
	return b.WithOption(agentllm.WithCache(true, ttl))
}

// WithPreset applies a preset
func (b *OpenAIProviderBuilder) WithPreset(preset agentllm.PresetOption) *OpenAIProviderBuilder {
	return b.WithOption(agentllm.WithPreset(preset))
}

// WithUseCase optimizes for a use case
func (b *OpenAIProviderBuilder) WithUseCase(useCase agentllm.UseCase) *OpenAIProviderBuilder {
	return b.WithOption(agentllm.WithUseCase(useCase))
}

// Build creates the provider
func (b *OpenAIProviderBuilder) Build() (*EnhancedOpenAIProvider, error) {
	// Apply all options to config
	config := agentllm.ApplyOptions(b.config, b.opts...)

	// Ensure provider is set
	config.Provider = agentllm.ProviderOpenAI

	// Get API key from environment if not set
	if config.APIKey == "" {
		config.APIKey = os.Getenv("OPENAI_API_KEY")
	}

	// Validate config
	if config.APIKey == "" {
		return nil, agentErrors.NewInvalidConfigError(
			string(agentllm.ProviderOpenAI),
			"api_key",
			"OpenAI API key is required",
		)
	}

	return NewEnhancedOpenAI(config)
}

// BuildBasic creates a basic provider (without enhancements)
func (b *OpenAIProviderBuilder) BuildBasic() (*OpenAIProvider, error) {
	// Apply all options to config
	config := agentllm.ApplyOptions(b.config, b.opts...)

	// Ensure provider is set
	config.Provider = agentllm.ProviderOpenAI

	// Get API key from environment if not set
	if config.APIKey == "" {
		config.APIKey = os.Getenv("OPENAI_API_KEY")
	}

	return NewOpenAI(config)
}
