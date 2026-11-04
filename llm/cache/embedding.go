package cache

import (
	"context"
)

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
