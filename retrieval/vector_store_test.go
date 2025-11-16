package retrieval

import (
	"context"
	"testing"
)

// TestVectorStoreRetrieverSearchTypes tests different search type configurations
func TestVectorStoreRetrieverSearchTypes(t *testing.T) {
	ctx := context.Background()
	vectorStore := NewMockVectorStore()

	docs := []*Document{
		NewDocument("Machine learning algorithms", nil),
		NewDocument("Deep learning neural networks", nil),
		NewDocument("Natural language processing", nil),
	}

	err := vectorStore.AddDocuments(ctx, docs)
	if err != nil {
		t.Fatalf("Failed to add documents: %v", err)
	}

	tests := []struct {
		name       string
		searchType SearchType
		wantErr    bool
	}{
		{
			name:       "Similarity search",
			searchType: SearchTypeSimilarity,
			wantErr:    false,
		},
		{
			name:       "Similarity with score threshold",
			searchType: SearchTypeSimilarityScoreThreshold,
			wantErr:    false,
		},
		{
			name:       "MMR search",
			searchType: SearchTypeMMR,
			wantErr:    false,
		},
		{
			name:       "Unknown search type",
			searchType: "unknown",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := RetrieverConfig{TopK: 2, MinScore: 0.0, Name: "test_retriever"}
			retriever := NewVectorStoreRetriever(vectorStore, config)
			retriever.SearchType = tt.searchType

			results, err := retriever.GetRelevantDocuments(ctx, "machine learning")

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if results == nil {
					t.Error("Expected non-nil results")
				}
			}
		})
	}
}

// TestVectorStoreRetrieverWithSearchKwargs tests setting search kwargs
func TestVectorStoreRetrieverWithSearchKwargs(t *testing.T) {
	config := RetrieverConfig{TopK: 5, MinScore: 0.5, Name: "test"}
	retriever := NewVectorStoreRetriever(NewMockVectorStore(), config)

	kwargs := map[string]interface{}{
		"filter": "source=wiki",
		"boost":  1.5,
	}

	retriever.WithSearchKwargs(kwargs)

	if len(retriever.SearchKwargs) != len(kwargs) {
		t.Errorf("Expected %d kwargs, got %d", len(kwargs), len(retriever.SearchKwargs))
	}

	if retriever.SearchKwargs["filter"] != "source=wiki" {
		t.Error("Filter kwarg not set correctly")
	}
}

// TestMockVectorStoreAddDocuments tests adding multiple documents
func TestMockVectorStoreAddDocuments(t *testing.T) {
	ctx := context.Background()
	store := NewMockVectorStore()

	docs := []*Document{
		NewDocument("Document 1 content", map[string]interface{}{"id": 1}),
		NewDocument("Document 2 content", map[string]interface{}{"id": 2}),
		NewDocument("Document 3 content", map[string]interface{}{"id": 3}),
	}

	err := store.AddDocuments(ctx, docs)
	if err != nil {
		t.Fatalf("Failed to add documents: %v", err)
	}

	allDocs := store.GetAllDocuments()
	if len(allDocs) != len(docs) {
		t.Errorf("Expected %d documents, got %d", len(docs), len(allDocs))
	}
}

// TestMockVectorStoreDelete tests deleting documents
func TestMockVectorStoreDelete(t *testing.T) {
	ctx := context.Background()
	store := NewMockVectorStore()

	docs := []*Document{
		NewDocumentWithID("doc1", "Content 1", nil),
		NewDocumentWithID("doc2", "Content 2", nil),
		NewDocumentWithID("doc3", "Content 3", nil),
	}

	_ = store.AddDocuments(ctx, docs)

	err := store.Delete(ctx, []string{"doc1", "doc3"})
	if err != nil {
		t.Fatalf("Failed to delete documents: %v", err)
	}

	remaining := store.GetAllDocuments()
	if len(remaining) != 1 {
		t.Errorf("Expected 1 remaining document, got %d", len(remaining))
	}

	if remaining[0].ID != "doc2" {
		t.Errorf("Expected doc2 to remain, got %s", remaining[0].ID)
	}
}

// TestMockVectorStoreClear tests clearing the store
func TestMockVectorStoreClear(t *testing.T) {
	ctx := context.Background()
	store := NewMockVectorStore()

	docs := []*Document{
		NewDocument("Content 1", nil),
		NewDocument("Content 2", nil),
	}

	_ = store.AddDocuments(ctx, docs)

	if len(store.GetAllDocuments()) != 2 {
		t.Fatal("Documents not added properly")
	}

	store.Clear()

	remaining := store.GetAllDocuments()
	if len(remaining) != 0 {
		t.Errorf("Expected 0 documents after clear, got %d", len(remaining))
	}
}

// TestMockVectorStoreSimilaritySearchEmptyStore tests searching empty store
func TestMockVectorStoreSimilaritySearchEmptyStore(t *testing.T) {
	ctx := context.Background()
	store := NewMockVectorStore()

	results, err := store.SimilaritySearch(ctx, "test query", 5)
	if err != nil {
		t.Fatalf("Search on empty store failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results on empty store, got %d", len(results))
	}
}

// TestMockVectorStoreSimilaritySearchTopK tests topK limiting
func TestMockVectorStoreSimilaritySearchTopK(t *testing.T) {
	ctx := context.Background()
	store := NewMockVectorStore()

	// Add multiple documents
	docs := make([]*Document, 10)
	for i := 0; i < 10; i++ {
		docs[i] = NewDocument("Machine learning content", nil)
	}

	_ = store.AddDocuments(ctx, docs)

	tests := []struct {
		name  string
		topK  int
		query string
	}{
		{
			name:  "Get top 3",
			topK:  3,
			query: "machine learning",
		},
		{
			name:  "Get top 1",
			topK:  1,
			query: "machine",
		},
		{
			name:  "TopK larger than results",
			topK:  20,
			query: "learning",
		},
		{
			name:  "TopK zero",
			topK:  0,
			query: "content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.SimilaritySearch(ctx, tt.query, tt.topK)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}

			if tt.topK > 0 && len(results) > tt.topK {
				t.Errorf("Expected at most %d results, got %d", tt.topK, len(results))
			}
		})
	}
}

// TestMockVectorStoreSimilaritySearchWithScore tests similarity with score
func TestMockVectorStoreSimilaritySearchWithScore(t *testing.T) {
	ctx := context.Background()
	store := NewMockVectorStore()

	docs := []*Document{
		NewDocument("Kubernetes orchestration platform", nil),
		NewDocument("Docker containerization technology", nil),
		NewDocument("Python programming language", nil),
	}

	_ = store.AddDocuments(ctx, docs)

	results, err := store.SimilaritySearchWithScore(ctx, "kubernetes container", 2)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected non-empty results")
	}

	// Check that results are sorted by score
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Error("Results should be sorted in descending order by score")
		}
	}
}

// TestVectorStoreRetrieverWithMinScore tests min score filtering
func TestVectorStoreRetrieverWithMinScore(t *testing.T) {
	ctx := context.Background()
	vectorStore := NewMockVectorStore()

	docs := []*Document{
		NewDocument("High relevance machine learning", nil),
		NewDocument("Medium relevance data science", nil),
		NewDocument("Low relevance cooking recipe", nil),
	}

	_ = vectorStore.AddDocuments(ctx, docs)

	config := RetrieverConfig{
		TopK:     5,
		MinScore: 0.2,
		Name:     "filtered_retriever",
	}

	retriever := NewVectorStoreRetriever(vectorStore, config)

	results, err := retriever.GetRelevantDocuments(ctx, "machine learning")
	if err != nil {
		t.Fatalf("Retrieval failed: %v", err)
	}

	// All returned documents should have score >= MinScore
	for _, doc := range results {
		if doc.Score < config.MinScore {
			t.Errorf("Document score %.2f is below minimum threshold %.2f", doc.Score, config.MinScore)
		}
	}
}

// TestVectorStoreRetrieverConfiguration tests configuration methods
func TestVectorStoreRetrieverConfiguration(t *testing.T) {
	config := RetrieverConfig{
		TopK:     10,
		MinScore: 0.3,
		Name:     "test_retriever",
	}

	retriever := NewVectorStoreRetriever(NewMockVectorStore(), config)

	if retriever.TopK != 10 {
		t.Errorf("Expected TopK 10, got %d", retriever.TopK)
	}

	if retriever.MinScore != 0.3 {
		t.Errorf("Expected MinScore 0.3, got %.2f", retriever.MinScore)
	}

	if retriever.Name != "test_retriever" {
		t.Errorf("Expected name 'test_retriever', got '%s'", retriever.Name)
	}

	// Change search type
	retriever.WithSearchType(SearchTypeSimilarityScoreThreshold)
	if retriever.SearchType != SearchTypeSimilarityScoreThreshold {
		t.Errorf("Search type not updated correctly")
	}
}
