package cache

import (
	"context"
	"fmt"
	"strings"

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

// CompleteStream 命中缓存时返回一个完整块；未命中时转发上游流，
// 只有成功终止的回答才写入缓存。
func (c *CachedLLMClient) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	prompt := extractPromptFromRequest(req)
	model := ""
	if req != nil {
		model = req.Model
	}

	entry, _, err := c.cache.Get(ctx, prompt, model)
	if err != nil {
		// Log error but continue with actual request
		fmt.Printf("cache get error: %v\n", err)
	}
	if entry != nil {
		out := make(chan *llm.StreamChunk, 2)
		out <- &llm.StreamChunk{Delta: entry.Response, Content: entry.Response, ResponseModel: entry.Model}
		out <- &llm.StreamChunk{Content: entry.Response, ResponseModel: entry.Model, FinishReason: "cache_hit", Done: true}
		close(out)
		return out, nil
	}

	upstream, err := c.client.CompleteStream(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan *llm.StreamChunk)
	go func() {
		defer close(out)
		var content strings.Builder
		for chunk := range upstream {
			if chunk != nil {
				content.WriteString(chunk.Delta)
				if chunk.Done && chunk.Error == nil {
					answer := chunk.Content
					if answer == "" {
						answer = content.String()
					}
					tokens := 0
					if chunk.Usage != nil {
						tokens = chunk.Usage.TotalTokens
					}
					if cacheErr := c.cache.Set(ctx, prompt, answer, model, tokens); cacheErr != nil {
						// Log error but don't fail the request
						fmt.Printf("cache set error: %v\n", cacheErr)
					}
				}
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// Provider returns the underlying provider type
func (c *CachedLLMClient) Provider() constants.Provider {
	return c.provider
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
