package retrieval

import (
	"context"
	"testing"
)

// TestRAGRetrieverConfiguration tests RAG retriever configuration
func TestRAGRetrieverConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		config    RAGRetrieverConfig
		wantErr   bool
		wantPanic bool
	}{
		{
			name: "Valid configuration",
			config: RAGRetrieverConfig{
				VectorStore:      NewMockVectorStore(),
				TopK:             5,
				ScoreThreshold:   0.3,
				IncludeMetadata:  true,
				MaxContentLength: 500,
			},
			wantErr: false,
		},
		{
			name: "Nil vector store",
			config: RAGRetrieverConfig{
				VectorStore: nil,
			},
			wantErr: true,
		},
		{
			name: "Negative TopK defaults to 4",
			config: RAGRetrieverConfig{
				VectorStore: NewMockVectorStore(),
				TopK:        -5,
			},
			wantErr: false,
		},
		{
			name: "Negative score threshold defaults to 0",
			config: RAGRetrieverConfig{
				VectorStore:    NewMockVectorStore(),
				ScoreThreshold: -1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retriever, err := NewRAGRetriever(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if retriever == nil {
					t.Error("Expected non-nil retriever")
				}
			}
		})
	}
}

// TestRAGRetrieverRetrieveEmptyStore tests retrieval from empty store
func TestRAGRetrieverRetrieveEmptyStore(t *testing.T) {
	ctx := context.Background()

	store := NewMockVectorStore()
	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             5,
		ScoreThreshold:   0,
		IncludeMetadata:  false,
		MaxContentLength: 1000,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	results, err := retriever.Retrieve(ctx, "test query")
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results from empty store, got %d", len(results))
	}
}

// TestRAGRetrieverScoreThreshold tests score threshold filtering
func TestRAGRetrieverScoreThreshold(t *testing.T) {
	ctx := context.Background()

	store := NewMockVectorStore()
	docs := []*Document{
		NewDocument("High relevance machine learning", nil),
		NewDocument("Medium relevance data science", nil),
		NewDocument("Low relevance random text", nil),
	}
	_ = store.AddDocuments(ctx, docs)

	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             10,
		ScoreThreshold:   0.6,
		IncludeMetadata:  false,
		MaxContentLength: 1000,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	results, err := retriever.Retrieve(ctx, "machine learning")
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	// All results should exceed threshold
	for _, doc := range results {
		if doc.Score < 0.6 {
			t.Errorf("Document score %.2f below threshold 0.6", doc.Score)
		}
	}
}

// TestRAGRetrieverMaxContentLength tests content truncation
func TestRAGRetrieverMaxContentLength(t *testing.T) {
	ctx := context.Background()

	store := NewMockVectorStore()
	docs := []*Document{
		NewDocument("This is a very long document content that should be truncated because it exceeds the maximum length", nil),
	}
	_ = store.AddDocuments(ctx, docs)

	maxLen := 50
	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             5,
		ScoreThreshold:   0,
		IncludeMetadata:  false,
		MaxContentLength: maxLen,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	results, err := retriever.Retrieve(ctx, "long document")
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(results) > 0 {
		if len(results[0].PageContent) > maxLen+3 { // +3 for "..."
			t.Errorf("Content not truncated properly")
		}
	}
}

// TestRAGRetrieverAddDocuments tests adding documents
func TestRAGRetrieverAddDocuments(t *testing.T) {
	ctx := context.Background()

	store := NewMockVectorStore()
	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             5,
		ScoreThreshold:   0,
		IncludeMetadata:  true,
		MaxContentLength: 1000,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	docs := []*Document{
		NewDocument("Document 1", nil),
		NewDocument("Document 2", nil),
	}

	err = retriever.AddDocuments(ctx, docs)
	if err != nil {
		t.Fatalf("AddDocuments failed: %v", err)
	}
}

// TestRAGRetrieverClear tests clearing the retriever
func TestRAGRetrieverClear(t *testing.T) {
	ctx := context.Background()

	store := NewMemoryVectorStore(MemoryVectorStoreConfig{
		Embedder: NewSimpleEmbedder(50),
	})

	docs := []*Document{
		NewDocument("Test document", nil),
	}
	_ = store.AddDocuments(ctx, docs)

	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             5,
		ScoreThreshold:   0,
		IncludeMetadata:  false,
		MaxContentLength: 1000,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	err = retriever.Clear()
	if err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if store.Count() != 0 {
		t.Errorf("Expected empty store after clear, got %d documents", store.Count())
	}
}

// TestRAGRetrieverSetters tests setter methods
func TestRAGRetrieverSetters(t *testing.T) {
	config := RAGRetrieverConfig{
		VectorStore:      NewMockVectorStore(),
		TopK:             4,
		ScoreThreshold:   0.1,
		IncludeMetadata:  false,
		MaxContentLength: 1000,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	// Test SetTopK
	retriever.SetTopK(10)
	if retriever.topK != 10 {
		t.Errorf("Expected TopK 10, got %d", retriever.topK)
	}

	retriever.SetTopK(-5) // Should not change negative values
	if retriever.topK != 10 {
		t.Errorf("Negative TopK should be ignored, expected 10, got %d", retriever.topK)
	}

	// Test SetScoreThreshold
	retriever.SetScoreThreshold(0.8)
	if retriever.scoreThreshold != 0.8 {
		t.Errorf("Expected score threshold 0.8, got %.2f", retriever.scoreThreshold)
	}

	retriever.SetScoreThreshold(-0.5) // Should not change negative values
	if retriever.scoreThreshold != 0.8 {
		t.Errorf("Negative threshold should be ignored, expected 0.8, got %.2f", retriever.scoreThreshold)
	}
}

// TestRAGRetrieverRetrieveAndFormat tests RetrieveAndFormat
func TestRAGRetrieverRetrieveAndFormat(t *testing.T) {
	ctx := context.Background()

	store := NewMockVectorStore()
	docs := []*Document{
		NewDocument("Python is a programming language", map[string]interface{}{"type": "language"}),
		NewDocument("Machine learning uses algorithms", nil),
	}
	_ = store.AddDocuments(ctx, docs)

	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             5,
		ScoreThreshold:   0,
		IncludeMetadata:  true,
		MaxContentLength: 1000,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	template := "Query: {query}\nDocs: {documents}\nCount: {num_docs}"

	formatted, err := retriever.RetrieveAndFormat(ctx, "python programming", template)
	if err != nil {
		t.Fatalf("RetrieveAndFormat failed: %v", err)
	}

	if formatted == "" {
		t.Error("Expected non-empty formatted result")
	}

	if !contains(formatted, "python") {
		t.Error("Expected formatted result to contain query")
	}
}

// TestRAGRetrieverWithEmptyTemplate tests with empty template
func TestRAGRetrieverWithEmptyTemplate(t *testing.T) {
	ctx := context.Background()

	store := NewMockVectorStore()
	docs := []*Document{
		NewDocument("Test content", nil),
	}
	_ = store.AddDocuments(ctx, docs)

	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             5,
		ScoreThreshold:   0,
		IncludeMetadata:  false,
		MaxContentLength: 1000,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	formatted, err := retriever.RetrieveAndFormat(ctx, "test", "")
	if err != nil {
		t.Fatalf("RetrieveAndFormat with empty template failed: %v", err)
	}

	if formatted == "" {
		t.Error("Expected non-empty formatted result with default template")
	}
}

// TestRAGChainRun tests RAG chain execution
func TestRAGChainRun(t *testing.T) {
	ctx := context.Background()

	store := NewMockVectorStore()
	docs := []*Document{
		NewDocument("Kubernetes is a container orchestration system", nil),
		NewDocument("Docker provides containerization", nil),
	}
	_ = store.AddDocuments(ctx, docs)

	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             5,
		ScoreThreshold:   0,
		IncludeMetadata:  false,
		MaxContentLength: 1000,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	chain := NewRAGChain(retriever)

	result, err := chain.Run(ctx, "What is Kubernetes?")
	if err != nil {
		t.Fatalf("RAG chain run failed: %v", err)
	}

	if result == "" {
		t.Error("Expected non-empty chain result")
	}
}

// TestRAGChainRunEmptyResults tests RAG chain with no documents
func TestRAGChainRunEmptyResults(t *testing.T) {
	ctx := context.Background()

	store := NewMockVectorStore()

	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             5,
		ScoreThreshold:   0,
		IncludeMetadata:  false,
		MaxContentLength: 1000,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create retriever: %v", err)
	}

	chain := NewRAGChain(retriever)

	result, err := chain.Run(ctx, "test query")
	if err != nil {
		t.Fatalf("RAG chain run failed: %v", err)
	}

	if !contains(result, "No relevant documents") {
		t.Error("Expected 'No relevant documents' message for empty results")
	}
}

// TestRAGMultiQueryRetrieverConfiguration tests multi-query retriever configuration
func TestRAGMultiQueryRetrieverConfiguration(t *testing.T) {
	store := NewMockVectorStore()
	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             5,
		ScoreThreshold:   0,
		IncludeMetadata:  false,
		MaxContentLength: 1000,
	}

	retriever, _ := NewRAGRetriever(config)

	t.Run("Default num queries", func(t *testing.T) {
		mqr := NewRAGMultiQueryRetriever(retriever, 0)
		if mqr.NumQueries != 3 {
			t.Errorf("Expected default NumQueries 3, got %d", mqr.NumQueries)
		}
	})

	t.Run("Custom num queries", func(t *testing.T) {
		mqr := NewRAGMultiQueryRetriever(retriever, 5)
		if mqr.NumQueries != 5 {
			t.Errorf("Expected NumQueries 5, got %d", mqr.NumQueries)
		}
	})
}

// TestRAGMultiQueryRetrieverRetrieve tests multi-query retrieval
func TestRAGMultiQueryRetrieverRetrieve(t *testing.T) {
	ctx := context.Background()

	store := NewMockVectorStore()
	docs := []*Document{
		NewDocument("Machine learning algorithms", nil),
		NewDocument("Deep learning networks", nil),
		NewDocument("Natural language processing", nil),
	}
	_ = store.AddDocuments(ctx, docs)

	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             10,
		ScoreThreshold:   0,
		IncludeMetadata:  false,
		MaxContentLength: 1000,
	}

	retriever, _ := NewRAGRetriever(config)
	mqr := NewRAGMultiQueryRetriever(retriever, 3)

	results, err := mqr.Retrieve(ctx, "machine learning")
	if err != nil {
		t.Fatalf("Multi-query retrieval failed: %v", err)
	}

	if results == nil {
		t.Error("Expected non-nil results")
	}
}

// TestRAGMultiQueryRetrieverDeduplication tests document deduplication
func TestRAGMultiQueryRetrieverDeduplication(t *testing.T) {
	ctx := context.Background()

	store := NewMockVectorStore()
	doc := NewDocument("Kubernetes container orchestration platform", nil)
	_ = store.AddDocuments(ctx, []*Document{doc})

	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             5,
		ScoreThreshold:   0,
		IncludeMetadata:  false,
		MaxContentLength: 1000,
	}

	retriever, _ := NewRAGRetriever(config)
	mqr := NewRAGMultiQueryRetriever(retriever, 5)

	results, err := mqr.Retrieve(ctx, "Kubernetes")
	if err != nil {
		t.Fatalf("Retrieval failed: %v", err)
	}

	// Deduplication should prevent duplicates
	seen := make(map[string]bool)
	for _, doc := range results {
		if seen[doc.ID] {
			t.Errorf("Duplicate document found: %s", doc.ID)
		}
		seen[doc.ID] = true
	}
}
