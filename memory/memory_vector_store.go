package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
)

// SimpleVectorStore interface for vector-based memory storage
type SimpleVectorStore interface {
	// Store stores a vector with an ID
	Store(ctx context.Context, id string, vector []float32) error

	// Search searches for similar vectors
	Search(ctx context.Context, query []float32, k int, threshold float64) (ids []string, scores []float64, err error)

	// Delete removes a vector
	Delete(ctx context.Context, id string) error

	// GenerateEmbedding generates an embedding for content
	GenerateEmbedding(ctx context.Context, content interface{}) ([]float32, error)

	// Clear clears all vectors
	Clear(ctx context.Context) error

	// Size returns the number of stored vectors
	Size() int
}

// InMemoryVectorStore is a simple in-memory vector store implementation
type InMemoryVectorStore struct {
	vectors   map[string][]float32
	dimension int
	mu        sync.RWMutex
}

// NewInMemoryVectorStore creates a new in-memory vector store
func NewInMemoryVectorStore(dimension int) *InMemoryVectorStore {
	return &InMemoryVectorStore{
		vectors:   make(map[string][]float32),
		dimension: dimension,
	}
}

// Store stores a vector with an ID
func (s *InMemoryVectorStore) Store(ctx context.Context, id string, vector []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(vector) != s.dimension {
		return fmt.Errorf("vector dimension mismatch: expected %d, got %d", s.dimension, len(vector))
	}

	// Normalize vector
	normalized := normalizeVector(vector)
	s.vectors[id] = normalized

	return nil
}

// Search searches for similar vectors using cosine similarity
func (s *InMemoryVectorStore) Search(ctx context.Context, query []float32, k int, threshold float64) ([]string, []float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(query) != s.dimension {
		return nil, nil, fmt.Errorf("query dimension mismatch: expected %d, got %d", s.dimension, len(query))
	}

	// Normalize query
	normalizedQuery := normalizeVector(query)

	// Calculate similarities
	type result struct {
		id    string
		score float64
	}

	var results []result
	for id, vector := range s.vectors {
		similarity := cosineSimilarity(normalizedQuery, vector)
		if similarity >= threshold {
			results = append(results, result{id: id, score: similarity})
		}
	}

	// Sort by similarity (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// Limit to k results
	if len(results) > k {
		results = results[:k]
	}

	// Extract IDs and scores
	ids := make([]string, len(results))
	scores := make([]float64, len(results))
	for i, r := range results {
		ids[i] = r.id
		scores[i] = r.score
	}

	return ids, scores, nil
}

// Delete removes a vector
func (s *InMemoryVectorStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.vectors, id)
	return nil
}

// GenerateEmbedding generates a simple embedding for content
func (s *InMemoryVectorStore) GenerateEmbedding(ctx context.Context, content interface{}) ([]float32, error) {
	// Simple hash-based embedding generation
	// In production, this would use a real embedding model

	data, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal content: %w", err)
	}

	// Generate pseudo-embedding based on content
	embedding := make([]float32, s.dimension)
	for i := 0; i < s.dimension; i++ {
		// Simple hash-based values
		hash := hashBytes(data, i)
		embedding[i] = float32(hash%1000) / 1000.0
	}

	return normalizeVector(embedding), nil
}

// Clear clears all vectors
func (s *InMemoryVectorStore) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.vectors = make(map[string][]float32)
	return nil
}

// Size returns the number of stored vectors
func (s *InMemoryVectorStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.vectors)
}

// EmbeddingVectorStore uses an external embedding model
type EmbeddingVectorStore struct {
	*InMemoryVectorStore
	embedder EmbeddingModel
}

// EmbeddingModel interface for generating embeddings
type EmbeddingModel interface {
	// Embed generates embeddings for text
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension returns the embedding dimension
	Dimension() int
}

// NewEmbeddingVectorStore creates a vector store with an embedding model
func NewEmbeddingVectorStore(embedder EmbeddingModel) *EmbeddingVectorStore {
	return &EmbeddingVectorStore{
		InMemoryVectorStore: NewInMemoryVectorStore(embedder.Dimension()),
		embedder:            embedder,
	}
}

// GenerateEmbedding uses the embedding model to generate embeddings
func (s *EmbeddingVectorStore) GenerateEmbedding(ctx context.Context, content interface{}) ([]float32, error) {
	// Convert content to text
	text := fmt.Sprintf("%v", content)

	// Generate embedding using model
	return s.embedder.Embed(ctx, text)
}

// ChromaVectorStore integrates with Chroma vector database
type ChromaVectorStore struct {
	client     ChromaClient
	collection string
	dimension  int
	mu         sync.RWMutex
}

// ChromaClient interface for Chroma database operations
type ChromaClient interface {
	// CreateCollection creates a new collection
	CreateCollection(ctx context.Context, name string, metadata map[string]interface{}) error

	// AddDocuments adds documents to a collection
	AddDocuments(ctx context.Context, collection string, ids []string, embeddings [][]float32, documents []map[string]interface{}) error

	// Query queries a collection
	Query(ctx context.Context, collection string, queryEmbeddings [][]float32, k int) (*ChromaQueryResult, error)

	// Delete deletes documents from a collection
	Delete(ctx context.Context, collection string, ids []string) error

	// DeleteCollection deletes a collection
	DeleteCollection(ctx context.Context, name string) error
}

// ChromaQueryResult represents query results from Chroma
type ChromaQueryResult struct {
	IDs       [][]string                 `json:"ids"`
	Distances [][]float64                `json:"distances"`
	Documents [][]map[string]interface{} `json:"documents"`
}

// NewChromaVectorStore creates a new Chroma vector store
func NewChromaVectorStore(client ChromaClient, collection string, dimension int) *ChromaVectorStore {
	return &ChromaVectorStore{
		client:     client,
		collection: collection,
		dimension:  dimension,
	}
}

// Store stores a vector in Chroma
func (s *ChromaVectorStore) Store(ctx context.Context, id string, vector []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Convert to 2D array for Chroma
	embeddings := [][]float32{vector}
	ids := []string{id}
	documents := []map[string]interface{}{
		{"id": id},
	}

	return s.client.AddDocuments(ctx, s.collection, ids, embeddings, documents)
}

// Search searches in Chroma
func (s *ChromaVectorStore) Search(ctx context.Context, query []float32, k int, threshold float64) ([]string, []float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Convert to 2D array for Chroma
	queryEmbeddings := [][]float32{query}

	result, err := s.client.Query(ctx, s.collection, queryEmbeddings, k)
	if err != nil {
		return nil, nil, err
	}

	if len(result.IDs) == 0 || len(result.IDs[0]) == 0 {
		return []string{}, []float64{}, nil
	}

	// Extract IDs and convert distances to similarities
	ids := result.IDs[0]
	distances := result.Distances[0]
	scores := make([]float64, len(distances))

	for i, distance := range distances {
		// Convert distance to similarity (assuming cosine distance)
		similarity := 1.0 - distance
		if similarity >= threshold {
			scores[i] = similarity
		}
	}

	return ids, scores, nil
}

// Delete deletes from Chroma
func (s *ChromaVectorStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.client.Delete(ctx, s.collection, []string{id})
}

// GenerateEmbedding generates embeddings (requires external model)
func (s *ChromaVectorStore) GenerateEmbedding(ctx context.Context, content interface{}) ([]float32, error) {
	// This would typically use an embedding model
	// For now, return a simple hash-based embedding
	data, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}

	embedding := make([]float32, s.dimension)
	for i := 0; i < s.dimension; i++ {
		hash := hashBytes(data, i)
		embedding[i] = float32(hash%1000) / 1000.0
	}

	return normalizeVector(embedding), nil
}

// Clear clears the collection
func (s *ChromaVectorStore) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Delete and recreate collection
	if err := s.client.DeleteCollection(ctx, s.collection); err != nil {
		return err
	}

	return s.client.CreateCollection(ctx, s.collection, map[string]interface{}{
		"dimension": s.dimension,
	})
}

// Size returns the size (not implemented for Chroma)
func (s *ChromaVectorStore) Size() int {
	// Would require a count query to Chroma
	return -1
}

// Helper functions

// normalizeVector normalizes a vector to unit length
func normalizeVector(v []float32) []float32 {
	magnitude := float32(0)
	for _, val := range v {
		magnitude += val * val
	}
	magnitude = float32(math.Sqrt(float64(magnitude)))

	if magnitude == 0 {
		return v
	}

	normalized := make([]float32, len(v))
	for i, val := range v {
		normalized[i] = val / magnitude
	}

	return normalized
}

// cosineSimilarity calculates cosine similarity between two normalized vectors
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	dotProduct := float64(0)
	for i := range a {
		dotProduct += float64(a[i] * b[i])
	}

	return dotProduct
}

// hashBytes generates a simple hash for bytes
func hashBytes(data []byte, seed int) int {
	hash := seed
	for _, b := range data {
		hash = hash*31 + int(b)
	}
	return hash
}

// SimpleEmbeddingModel is a simple embedding model for testing
type SimpleEmbeddingModel struct {
	dimension int
}

// NewSimpleEmbeddingModel creates a simple embedding model
func NewSimpleEmbeddingModel(dimension int) *SimpleEmbeddingModel {
	return &SimpleEmbeddingModel{dimension: dimension}
}

// Embed generates a simple embedding for text
func (m *SimpleEmbeddingModel) Embed(ctx context.Context, text string) ([]float32, error) {
	embedding := make([]float32, m.dimension)

	// Simple character-based embedding
	for i := 0; i < m.dimension; i++ {
		if i < len(text) {
			embedding[i] = float32(text[i]) / 255.0
		} else {
			embedding[i] = 0
		}
	}

	return normalizeVector(embedding), nil
}

// EmbedBatch generates embeddings for multiple texts
func (m *SimpleEmbeddingModel) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		embedding, err := m.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		embeddings[i] = embedding
	}
	return embeddings, nil
}

// Dimension returns the embedding dimension
func (m *SimpleEmbeddingModel) Dimension() int {
	return m.dimension
}
