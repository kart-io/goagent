package cache

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

// OpenAIEmbeddingProvider uses OpenAI's embedding API
type OpenAIEmbeddingProvider struct {
	client    *openai.Client
	model     openai.EmbeddingModel
	dimension int
}

// OpenAIEmbeddingConfig configures the OpenAI embedding provider
type OpenAIEmbeddingConfig struct {
	// APIKey is the OpenAI API key
	APIKey string

	// BaseURL is the optional custom base URL
	BaseURL string

	// Model is the embedding model to use
	// Default: text-embedding-3-small
	Model openai.EmbeddingModel

	// Dimension is the embedding dimension
	// Default: 1536 for text-embedding-3-small
	Dimension int
}

// NewOpenAIEmbeddingProvider creates a new OpenAI embedding provider
func NewOpenAIEmbeddingProvider(config *OpenAIEmbeddingConfig) *OpenAIEmbeddingProvider {
	if config == nil {
		config = &OpenAIEmbeddingConfig{}
	}

	clientConfig := openai.DefaultConfig(config.APIKey)
	if config.BaseURL != "" {
		clientConfig.BaseURL = config.BaseURL
	}

	model := config.Model
	if model == "" {
		model = openai.SmallEmbedding3
	}

	dimension := config.Dimension
	if dimension == 0 {
		// Default dimensions for common models
		switch model {
		case openai.SmallEmbedding3:
			dimension = 1536
		case openai.LargeEmbedding3:
			dimension = 3072
		case openai.AdaEmbeddingV2:
			dimension = 1536
		default:
			dimension = 1536
		}
	}

	return &OpenAIEmbeddingProvider{
		client:    openai.NewClientWithConfig(clientConfig),
		model:     model,
		dimension: dimension,
	}
}

// Embed generates an embedding for the given text
func (p *OpenAIEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: []string{text},
		Model: p.model,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}

	return resp.Data[0].Embedding, nil
}

// EmbedBatch generates embeddings for multiple texts
func (p *OpenAIEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: texts,
		Model: p.model,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create batch embeddings: %w", err)
	}

	results := make([][]float32, len(texts))
	for _, data := range resp.Data {
		if data.Index < len(results) {
			results[data.Index] = data.Embedding
		}
	}

	return results, nil
}

// Dimension returns the embedding dimension
func (p *OpenAIEmbeddingProvider) Dimension() int {
	return p.dimension
}

// MockEmbeddingProvider is a mock provider for testing
type MockEmbeddingProvider struct {
	dimension  int
	embeddings map[string][]float32
}

// NewMockEmbeddingProvider creates a mock embedding provider
func NewMockEmbeddingProvider(dimension int) *MockEmbeddingProvider {
	return &MockEmbeddingProvider{
		dimension:  dimension,
		embeddings: make(map[string][]float32),
	}
}

// SetEmbedding sets a predefined embedding for a text
func (p *MockEmbeddingProvider) SetEmbedding(text string, embedding []float32) {
	p.embeddings[text] = embedding
}

// Embed returns the predefined embedding or generates a simple one
func (p *MockEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if embedding, ok := p.embeddings[text]; ok {
		return embedding, nil
	}

	// Generate a simple deterministic embedding based on text hash
	embedding := make([]float32, p.dimension)
	hash := uint32(0)
	for _, c := range text {
		hash = hash*31 + uint32(c)
	}

	for i := range embedding {
		hash = hash*1103515245 + 12345
		embedding[i] = float32(hash%1000) / 1000.0
	}

	return Normalize(embedding), nil
}

// EmbedBatch generates embeddings for multiple texts
func (p *MockEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		embedding, err := p.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		results[i] = embedding
	}
	return results, nil
}

// Dimension returns the embedding dimension
func (p *MockEmbeddingProvider) Dimension() int {
	return p.dimension
}
