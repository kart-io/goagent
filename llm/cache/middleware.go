package cache

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// CachedLLMClient wraps an LLM client with semantic caching
type CachedLLMClient struct {
	client   llm.Client
	cache    SemanticCache
	provider constants.Provider
}

// CachedLLMClientConfig configures the cached LLM client
type CachedLLMClientConfig struct {
	// Client is the underlying LLM client
	Client llm.Client

	// EmbeddingProvider generates embeddings for semantic matching
	EmbeddingProvider EmbeddingProvider

	// CacheConfig configures the semantic cache
	CacheConfig *SemanticCacheConfig
}

// NewCachedLLMClient creates a new cached LLM client
func NewCachedLLMClient(config *CachedLLMClientConfig) (*CachedLLMClient, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if config.Client == nil {
		return nil, fmt.Errorf("client is required")
	}
	if config.EmbeddingProvider == nil {
		return nil, fmt.Errorf("embedding provider is required")
	}

	cache := NewMemorySemanticCache(config.EmbeddingProvider, config.CacheConfig)

	return &CachedLLMClient{
		client:   config.Client,
		cache:    cache,
		provider: config.Client.Provider(),
	}, nil
}

// Complete sends a completion request with semantic caching
func (c *CachedLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// Extract prompt from request
	prompt := extractPromptFromRequest(req)
	model := req.Model

	// Try to get from cache
	entry, _, err := c.cache.Get(ctx, prompt, model)
	if err != nil {
		// Log error but continue with actual request
		fmt.Printf("cache get error: %v\n", err)
	}

	if entry != nil {
		// Cache hit - return cached response
		return &llm.CompletionResponse{
			Content:      entry.Response,
			Model:        entry.Model,
			TokensUsed:   0, // No tokens used for cached response
			FinishReason: "cache_hit",
		}, nil
	}

	// Cache miss - call actual LLM
	startTime := time.Now()
	resp, err := c.client.Complete(ctx, req)
	latency := time.Since(startTime)

	if err != nil {
		return nil, err
	}

	// Store in cache
	cacheErr := c.cache.Set(ctx, prompt, resp.Content, model, resp.TokensUsed)
	if cacheErr != nil {
		// Log error but don't fail the request
		fmt.Printf("cache set error: %v\n", cacheErr)
	}

	// Note: FinishReason already set by LLM, we don't modify it for cache miss
	_ = latency // latency captured for potential logging/metrics

	return resp, nil
}

// Chat sends a chat request with semantic caching
func (c *CachedLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	// Convert messages to completion request
	req := &llm.CompletionRequest{
		Messages: messages,
	}

	return c.Complete(ctx, req)
}

// Provider returns the underlying provider type
func (c *CachedLLMClient) Provider() constants.Provider {
	return c.provider
}

// IsAvailable checks if the client is available
func (c *CachedLLMClient) IsAvailable() bool {
	return c.client.IsAvailable()
}

// Stats returns cache statistics
func (c *CachedLLMClient) Stats() *CacheStats {
	return c.cache.Stats()
}

// ClearCache clears the semantic cache
func (c *CachedLLMClient) ClearCache(ctx context.Context) error {
	return c.cache.Clear(ctx)
}

// Close closes the cached client
func (c *CachedLLMClient) Close() error {
	return c.cache.Close()
}

// extractPromptFromRequest extracts the prompt string from a completion request
func extractPromptFromRequest(req *llm.CompletionRequest) string {
	if req == nil {
		return ""
	}

	// Combine messages into a prompt
	if len(req.Messages) > 0 {
		var parts []string
		for _, msg := range req.Messages {
			parts = append(parts, fmt.Sprintf("%s: %s", msg.Role, msg.Content))
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

// WithSemanticCache creates a middleware function for semantic caching
func WithSemanticCache(embeddingProvider EmbeddingProvider, config *SemanticCacheConfig) func(llm.Client) llm.Client {
	return func(client llm.Client) llm.Client {
		cached, err := NewCachedLLMClient(&CachedLLMClientConfig{
			Client:            client,
			EmbeddingProvider: embeddingProvider,
			CacheConfig:       config,
		})
		if err != nil {
			// Return original client if cache creation fails
			return client
		}
		return cached
	}
}
