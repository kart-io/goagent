package interfaces

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// VectorStore is the canonical interface for vector storage and similarity search.
//
// This interface unifies multiple previous definitions:
//   - retrieval/vector_store.go (Document-based vector search)
//   - memory/manager.go (Embedding-based vector search)
//
// Implementations: goagent-contrib/retrieval 的各向量库适配。
//
// The interface provides document-oriented vector operations, which is the most
// common use case for RAG and retrieval systems.
type VectorStore interface {
	// SimilaritySearch performs vector similarity search and returns the most similar documents.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - query: The query string to search for
	//   - topK: Maximum number of results to return
	//
	// Returns documents sorted by similarity (most similar first).
	SimilaritySearch(ctx context.Context, query string, topK int) ([]*Document, error)

	// SimilaritySearchWithScore returns documents with their similarity scores.
	//
	// This is useful when you need to filter results based on a score threshold
	// or display confidence levels to users.
	//
	// The Score field in each returned Document will be populated.
	SimilaritySearchWithScore(ctx context.Context, query string, topK int) ([]*Document, error)

	// AddDocuments adds new documents to the vector store.
	//
	// The implementation should:
	//   - Generate embeddings for documents without them
	//   - Store the document content and metadata
	//   - Index the vectors for efficient similarity search
	AddDocuments(ctx context.Context, docs []*Document) error

	// Delete removes documents by their IDs.
	//
	// This is useful for:
	//   - Removing outdated information
	//   - GDPR/data deletion compliance
	//   - Managing store size
	Delete(ctx context.Context, ids []string) error
}

// Document represents a document with optional vector embedding.
//
// This is the canonical document type used throughout the agent framework
// for retrieval, RAG, and memory systems.
//
// Previously defined in:
//   - retrieval/document.go
//   - retrieval/vector_store.go (inline)
type Document struct {
	// ID is the unique identifier for the document.
	//
	// If not provided when creating a document, implementations should
	// generate a unique ID automatically.
	ID string `json:"id"`

	// PageContent is the main text content of the document.
	//
	// This is the primary searchable text that embeddings are generated from.
	PageContent string `json:"page_content"`

	// Metadata contains additional structured information about the document.
	//
	// Common metadata fields:
	//   - "source": Document source (file path, URL, etc.)
	//   - "title": Document title
	//   - "author": Document author
	//   - "created_at": Creation timestamp
	//   - "tags": Document tags/categories
	//
	// Metadata can be used for filtering and enriching search results.
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Embedding is the vector representation of the document.
	//
	// This field may be nil if embeddings haven't been generated yet.
	// VectorStore implementations should generate embeddings if missing.
	Embedding []float32 `json:"embedding,omitempty"`

	// Score represents the similarity or relevance score.
	//
	// This field is populated by search operations:
	//   - SimilaritySearch: Cosine similarity (typically 0.0 to 1.0)
	//   - Reranking: Relevance score from reranker
	//   - BM25: BM25 score
	//
	// Higher scores indicate better matches.
	Score float64 `json:"score,omitzero"`
}

// Clone creates a deep copy of the document.
func (d *Document) Clone() *Document {
	metadata := make(map[string]interface{})
	for k, v := range d.Metadata {
		metadata[k] = v
	}

	embedding := make([]float32, len(d.Embedding))
	copy(embedding, d.Embedding)

	return &Document{
		ID:          d.ID,
		PageContent: d.PageContent,
		Metadata:    metadata,
		Embedding:   embedding,
		Score:       d.Score,
	}
}

// GetMetadata retrieves a metadata value by key.
func (d *Document) GetMetadata(key string) (interface{}, bool) {
	if d.Metadata == nil {
		return nil, false
	}
	val, ok := d.Metadata[key]
	return val, ok
}

// SetMetadata sets a metadata value.
func (d *Document) SetMetadata(key string, value interface{}) {
	if d.Metadata == nil {
		d.Metadata = make(map[string]interface{})
	}
	d.Metadata[key] = value
}

// String returns a string representation of the document.
func (d *Document) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Document(ID=%s, Score=%.4f", d.ID, d.Score)
	if len(d.Metadata) > 0 {
		sb.WriteString(", Metadata={")
		keys := make([]string, 0, len(d.Metadata))
		for k := range d.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "%s: %v", k, d.Metadata[k])
		}
		sb.WriteString("}")
	}
	contentPreview := d.PageContent
	if len(contentPreview) > 50 {
		contentPreview = contentPreview[:50] + "..."
	}
	fmt.Fprintf(&sb, ", Content='%s')", contentPreview)
	return sb.String()
}
