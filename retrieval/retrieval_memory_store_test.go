package retrieval

import (
	"context"
	"sync"
	"testing"
)

// TestMemoryVectorStoreDistanceMetrics tests different distance metrics
func TestMemoryVectorStoreDistanceMetrics(t *testing.T) {
	ctx := context.Background()

	metrics := []DistanceMetric{
		DistanceMetricCosine,
		DistanceMetricEuclidean,
		DistanceMetricDot,
	}

	for _, metric := range metrics {
		t.Run(string(metric), func(t *testing.T) {
			config := MemoryVectorStoreConfig{
				Embedder:       NewSimpleEmbedder(50),
				DistanceMetric: metric,
			}

			store := NewMemoryVectorStore(config)

			docs := []*Document{
				NewDocument("Machine learning algorithms", nil),
				NewDocument("Deep learning networks", nil),
				NewDocument("Natural language processing", nil),
			}

			err := store.AddDocuments(ctx, docs)
			if err != nil {
				t.Fatalf("Failed to add documents: %v", err)
			}

			results, err := store.Search(ctx, "machine learning", 2)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}

			if len(results) == 0 {
				t.Error("Expected non-empty results")
			}
		})
	}
}

// TestMemoryVectorStoreExplicitVectors tests adding documents with explicit vectors
func TestMemoryVectorStoreExplicitVectors(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(3),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	docs := []*Document{
		NewDocument("Document 1", nil),
		NewDocument("Document 2", nil),
	}

	vectors := [][]float32{
		{1.0, 0, 0},
		{0, 1.0, 0},
	}

	err := store.Add(ctx, docs, vectors)
	if err != nil {
		t.Fatalf("Failed to add with vectors: %v", err)
	}

	if store.Count() != 2 {
		t.Errorf("Expected 2 documents, got %d", store.Count())
	}
}

// TestMemoryVectorStoreVectorMismatch tests vector/doc count mismatch
func TestMemoryVectorStoreVectorMismatch(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(3),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	docs := []*Document{
		NewDocument("Document 1", nil),
		NewDocument("Document 2", nil),
	}

	vectors := [][]float32{
		{1.0, 0, 0},
	}

	err := store.Add(ctx, docs, vectors)
	if err == nil {
		t.Error("Expected error for mismatched docs and vectors")
	}
}

// TestMemoryVectorStoreSearchByVector tests searching by explicit vector
func TestMemoryVectorStoreSearchByVector(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(3),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	docs := []*Document{
		NewDocument("Test content", nil),
	}

	vectors := [][]float32{
		{1.0, 0, 0},
	}

	_ = store.Add(ctx, docs, vectors)

	queryVector := []float32{1.0, 0, 0}
	results, err := store.SearchByVector(ctx, queryVector, 1)

	if err != nil {
		t.Fatalf("SearchByVector failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
}

// TestMemoryVectorStoreUpdateDocument tests updating documents
func TestMemoryVectorStoreUpdateDocument(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	doc := NewDocumentWithID("doc1", "Original content", nil)
	_ = store.AddDocuments(ctx, []*Document{doc})

	// Update document
	updatedDoc := NewDocumentWithID("doc1", "Updated content", nil)
	err := store.Update(ctx, []*Document{updatedDoc})

	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Retrieve and verify update
	retrieved, err := store.Get(ctx, "doc1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.PageContent != "Updated content" {
		t.Errorf("Content not updated: got %s", retrieved.PageContent)
	}
}

// TestMemoryVectorStoreUpdateNonexistent tests updating nonexistent document
func TestMemoryVectorStoreUpdateNonexistent(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	doc := NewDocumentWithID("nonexistent", "Content", nil)
	err := store.Update(ctx, []*Document{doc})

	if err == nil {
		t.Error("Expected error for nonexistent document")
	}
}

// TestMemoryVectorStoreUpdateNoID tests updating document without ID
func TestMemoryVectorStoreUpdateNoID(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	doc := &Document{PageContent: "Content", ID: ""}
	err := store.Update(ctx, []*Document{doc})

	if err == nil {
		t.Error("Expected error for document without ID")
	}
}

// TestMemoryVectorStoreGetVector tests getting document vector
func TestMemoryVectorStoreGetVector(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	doc := NewDocumentWithID("doc1", "Test content", nil)
	_ = store.AddDocuments(ctx, []*Document{doc})

	vector, err := store.GetVector(ctx, "doc1")
	if err != nil {
		t.Fatalf("GetVector failed: %v", err)
	}

	if len(vector) != 50 {
		t.Errorf("Expected vector dimension 50, got %d", len(vector))
	}

	// Test nonexistent vector
	_, err = store.GetVector(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent vector")
	}
}

// TestMemoryVectorStoreGetNonexistentDocument tests getting nonexistent document
func TestMemoryVectorStoreGetNonexistentDocument(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	_, err := store.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent document")
	}
}

// TestMemoryVectorStoreConcurrentOperations tests concurrent read/write operations
func TestMemoryVectorStoreConcurrentOperations(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent writes
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			doc := NewDocument("Concurrent document", nil)
			_ = store.AddDocuments(ctx, []*Document{doc})
		}(i)
	}

	wg.Wait()

	if store.Count() != numGoroutines {
		t.Errorf("Expected %d documents, got %d", numGoroutines, store.Count())
	}

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			_, _ = store.Search(ctx, "concurrent", 5)
		}()
	}

	wg.Wait()
}

// TestMemoryVectorStoreConcurrentAddDelete tests concurrent add and delete
func TestMemoryVectorStoreConcurrentAddDelete(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	// Add initial documents
	docs := make([]*Document, 20)
	for i := 0; i < 20; i++ {
		docs[i] = NewDocumentWithID(string(rune('0'+i)), "Content", nil)
	}
	_ = store.AddDocuments(ctx, docs)

	var wg sync.WaitGroup

	// Concurrent delete operations
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer wg.Done()

			idStr := string(rune('0' + id))
			_ = store.Delete(ctx, []string{idStr})
		}(i)
	}

	// Concurrent add operations
	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func(id int) {
			defer wg.Done()

			doc := NewDocument("New concurrent content", nil)
			_ = store.AddDocuments(ctx, []*Document{doc})
		}(i)
	}

	wg.Wait()

	// Final count should be: 20 - 10 + 5 = 15
	if store.Count() < 14 || store.Count() > 16 {
		t.Errorf("Expected around 15 documents, got %d", store.Count())
	}
}

// TestMemoryVectorStoreDeleteMultiple tests deleting multiple documents
func TestMemoryVectorStoreDeleteMultiple(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	docs := []*Document{
		NewDocumentWithID("doc1", "Content 1", nil),
		NewDocumentWithID("doc2", "Content 2", nil),
		NewDocumentWithID("doc3", "Content 3", nil),
		NewDocumentWithID("doc4", "Content 4", nil),
	}

	_ = store.AddDocuments(ctx, docs)

	err := store.Delete(ctx, []string{"doc1", "doc3", "nonexistent"})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if store.Count() != 2 {
		t.Errorf("Expected 2 documents after delete, got %d", store.Count())
	}
}

// TestMemoryVectorStoreEmbeddingExtraction tests GetEmbedding method
func TestMemoryVectorStoreEmbeddingExtraction(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	embedding, err := store.GetEmbedding(ctx, "test text")
	if err != nil {
		t.Fatalf("GetEmbedding failed: %v", err)
	}

	if len(embedding) != 50 {
		t.Errorf("Expected embedding dimension 50, got %d", len(embedding))
	}
}

// TestMemoryVectorStoreDefaultConfig tests default configuration
func TestMemoryVectorStoreDefaultConfig(t *testing.T) {
	config := MemoryVectorStoreConfig{}

	store := NewMemoryVectorStore(config)

	if store.embedder == nil {
		t.Error("Expected embedder to be set to default")
	}

	if store.distanceMetric != DistanceMetricCosine {
		t.Errorf("Expected default distance metric cosine, got %s", store.distanceMetric)
	}
}

// TestMemoryVectorStoreAddWithoutVectors tests adding docs without vectors
func TestMemoryVectorStoreAddWithoutVectors(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	docs := []*Document{
		NewDocument("Test document 1", nil),
		NewDocument("Test document 2", nil),
	}

	// Add without explicit vectors
	err := store.Add(ctx, docs, nil)
	if err != nil {
		t.Fatalf("Add without vectors failed: %v", err)
	}

	if store.Count() != 2 {
		t.Errorf("Expected 2 documents, got %d", store.Count())
	}
}

// TestMemoryVectorStoreAutoIDGeneration tests automatic ID generation
func TestMemoryVectorStoreAutoIDGeneration(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	doc := &Document{PageContent: "Test", ID: ""}

	_ = store.AddDocuments(ctx, []*Document{doc})

	if doc.ID == "" {
		t.Error("Expected automatic ID generation")
	}
}

// TestMemoryVectorStoreSimilaritySearchEdgeCases tests edge cases in similarity search
func TestMemoryVectorStoreSimilaritySearchEdgeCases(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	docs := []*Document{
		NewDocument("Test document", nil),
	}

	_ = store.AddDocuments(ctx, docs)

	tests := []struct {
		name  string
		query string
		topK  int
	}{
		{
			name:  "Empty query",
			query: "",
			topK:  5,
		},
		{
			name:  "Very long query",
			query: "This is a very long query that contains many words and should still work properly with the search function",
			topK:  5,
		},
		{
			name:  "Special characters",
			query: "!@#$%^&*()",
			topK:  5,
		},
		{
			name:  "Negative topK",
			query: "test",
			topK:  -5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.Search(ctx, tt.query, tt.topK)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}

			if results == nil {
				t.Error("Expected non-nil results")
			}
		})
	}
}

// TestMemoryVectorStoreEuclideanSorting tests euclidean distance sorting (ascending)
func TestMemoryVectorStoreEuclideanSorting(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(3),
		DistanceMetric: DistanceMetricEuclidean,
	}

	store := NewMemoryVectorStore(config)

	docs := []*Document{
		NewDocument("Document 1", nil),
		NewDocument("Document 2", nil),
	}

	vectors := [][]float32{
		{1.0, 0, 0},
		{10.0, 0, 0},
	}

	_ = store.Add(ctx, docs, vectors)

	queryVector := []float32{2.0, 0, 0}
	results, err := store.SearchByVector(ctx, queryVector, 2)

	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) < 2 {
		t.Fatal("Expected 2 results")
	}

	// With euclidean, closer docs should come first (lower distance)
	if results[0].Score > results[1].Score {
		t.Error("Euclidean results not sorted correctly (ascending)")
	}
}
