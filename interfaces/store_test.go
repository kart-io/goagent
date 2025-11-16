package interfaces

import (
	"context"
	"testing"
)

// TestDocumentStructure verifies Document struct is properly defined
func TestDocumentStructure(t *testing.T) {
	doc := &Document{
		ID:          "doc-123",
		PageContent: "This is a sample document about AI agents.",
		Metadata: map[string]interface{}{
			"source":     "/path/to/document.txt",
			"title":      "AI Agents Guide",
			"author":     "Alice",
			"created_at": "2024-01-15",
			"tags":       []string{"ai", "agents", "guide"},
		},
		Embedding: []float64{0.1, 0.2, 0.3, 0.4, 0.5},
		Score:     0.95,
	}

	if doc.ID != "doc-123" {
		t.Errorf("Expected ID 'doc-123', got '%s'", doc.ID)
	}
	if doc.PageContent != "This is a sample document about AI agents." {
		t.Errorf("Expected specific page content, got '%s'", doc.PageContent)
	}
	if doc.Metadata["source"] != "/path/to/document.txt" {
		t.Errorf("Expected metadata source, got '%v'", doc.Metadata["source"])
	}
	if len(doc.Embedding) != 5 {
		t.Errorf("Expected 5 embedding dimensions, got %d", len(doc.Embedding))
	}
	if doc.Score != 0.95 {
		t.Errorf("Expected score 0.95, got %f", doc.Score)
	}
}

// TestDocumentWithoutOptionalFields verifies Document works without optional fields
func TestDocumentWithoutOptionalFields(t *testing.T) {
	doc := &Document{
		ID:          "minimal-doc",
		PageContent: "Minimal document content",
	}

	if doc.ID != "minimal-doc" {
		t.Errorf("Expected ID 'minimal-doc', got '%s'", doc.ID)
	}
	if doc.Metadata != nil {
		t.Errorf("Expected nil metadata, got %v", doc.Metadata)
	}
	if doc.Embedding != nil {
		t.Errorf("Expected nil embedding, got %v", doc.Embedding)
	}
	if doc.Score != 0 {
		t.Errorf("Expected score 0, got %f", doc.Score)
	}
}

// mockVectorStore is a minimal test implementation of VectorStore
type mockVectorStore struct {
	documents map[string]*Document
}

func newMockVectorStore() *mockVectorStore {
	return &mockVectorStore{
		documents: make(map[string]*Document),
	}
}

func (m *mockVectorStore) SimilaritySearch(ctx context.Context, query string, topK int) ([]*Document, error) {
	// Simple mock: return all documents, limited by topK
	if topK <= 0 {
		return []*Document{}, nil
	}
	result := make([]*Document, 0, topK)
	for _, doc := range m.documents {
		docCopy := *doc
		result = append(result, &docCopy)
		if len(result) >= topK {
			break
		}
	}
	return result, nil
}

func (m *mockVectorStore) SimilaritySearchWithScore(ctx context.Context, query string, topK int) ([]*Document, error) {
	// Simple mock: return documents with similarity scores
	result := make([]*Document, 0, topK)
	for _, doc := range m.documents {
		docCopy := *doc
		docCopy.Score = 0.85 // Mock similarity score
		result = append(result, &docCopy)
		if len(result) >= topK {
			break
		}
	}
	return result, nil
}

func (m *mockVectorStore) AddDocuments(ctx context.Context, docs []*Document) error {
	for _, doc := range docs {
		m.documents[doc.ID] = doc
	}
	return nil
}

func (m *mockVectorStore) Delete(ctx context.Context, ids []string) error {
	for _, id := range ids {
		delete(m.documents, id)
	}
	return nil
}

// Ensure mockVectorStore implements VectorStore interface
var _ VectorStore = (*mockVectorStore)(nil)

// TestVectorStoreInterface verifies the VectorStore interface works correctly
func TestVectorStoreInterface(t *testing.T) {
	ctx := context.Background()
	store := newMockVectorStore()

	// Test AddDocuments
	docs := []*Document{
		{
			ID:          "doc-1",
			PageContent: "First document about AI",
			Metadata:    map[string]interface{}{"category": "ai"},
			Embedding:   []float64{0.1, 0.2, 0.3},
		},
		{
			ID:          "doc-2",
			PageContent: "Second document about ML",
			Metadata:    map[string]interface{}{"category": "ml"},
			Embedding:   []float64{0.2, 0.3, 0.4},
		},
		{
			ID:          "doc-3",
			PageContent: "Third document about agents",
			Metadata:    map[string]interface{}{"category": "agents"},
			Embedding:   []float64{0.3, 0.4, 0.5},
		},
	}

	err := store.AddDocuments(ctx, docs)
	if err != nil {
		t.Fatalf("AddDocuments failed: %v", err)
	}

	if len(store.documents) != 3 {
		t.Errorf("Expected 3 documents in store, got %d", len(store.documents))
	}

	// Test SimilaritySearch
	results, err := store.SimilaritySearch(ctx, "AI agents", 2)
	if err != nil {
		t.Fatalf("SimilaritySearch failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results (topK=2), got %d", len(results))
	}

	// Test SimilaritySearchWithScore
	resultsWithScore, err := store.SimilaritySearchWithScore(ctx, "machine learning", 3)
	if err != nil {
		t.Fatalf("SimilaritySearchWithScore failed: %v", err)
	}

	if len(resultsWithScore) != 3 {
		t.Errorf("Expected 3 results, got %d", len(resultsWithScore))
	}

	for i, doc := range resultsWithScore {
		if doc.Score == 0 {
			t.Errorf("Document %d should have a non-zero score", i)
		}
	}

	// Test Delete
	err = store.Delete(ctx, []string{"doc-1", "doc-3"})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if len(store.documents) != 1 {
		t.Errorf("Expected 1 document after deletion, got %d", len(store.documents))
	}

	if _, exists := store.documents["doc-2"]; !exists {
		t.Error("Expected doc-2 to still exist")
	}
}

// mockStore is a minimal test implementation of Store
type mockStore struct {
	data map[string]interface{}
}

func newMockStore() *mockStore {
	return &mockStore{
		data: make(map[string]interface{}),
	}
}

func (m *mockStore) Get(ctx context.Context, key string) (interface{}, error) {
	return m.data[key], nil
}

func (m *mockStore) Set(ctx context.Context, key string, value interface{}) error {
	m.data[key] = value
	return nil
}

func (m *mockStore) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockStore) Clear(ctx context.Context) error {
	m.data = make(map[string]interface{})
	return nil
}

// Ensure mockStore implements Store interface
var _ Store = (*mockStore)(nil)

// TestStoreInterface verifies the Store interface works correctly
func TestStoreInterface(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()

	// Test Set
	err := store.Set(ctx, "key1", "value1")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	err = store.Set(ctx, "key2", 42)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	err = store.Set(ctx, "key3", map[string]interface{}{"nested": "data"})
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Test Get
	val1, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val1 != "value1" {
		t.Errorf("Expected 'value1', got '%v'", val1)
	}

	val2, err := store.Get(ctx, "key2")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val2 != 42 {
		t.Errorf("Expected 42, got '%v'", val2)
	}

	val3, err := store.Get(ctx, "key3")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val3 == nil {
		t.Error("Expected non-nil value for key3")
	}

	// Test Delete
	err = store.Delete(ctx, "key1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	deleted, _ := store.Get(ctx, "key1")
	if deleted != nil {
		t.Errorf("Expected nil after deletion, got '%v'", deleted)
	}

	// Test Clear
	err = store.Clear(ctx)
	if err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if len(store.data) != 0 {
		t.Errorf("Expected 0 items after clear, got %d", len(store.data))
	}
}

// TestStoreIdempotentDelete verifies Delete is idempotent
func TestStoreIdempotentDelete(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()

	// Delete non-existent key should not error
	err := store.Delete(ctx, "non-existent-key")
	if err != nil {
		t.Errorf("Delete of non-existent key should not error, got: %v", err)
	}

	// Delete same key twice should not error
	store.Set(ctx, "test-key", "test-value")
	err = store.Delete(ctx, "test-key")
	if err != nil {
		t.Fatalf("First delete failed: %v", err)
	}

	err = store.Delete(ctx, "test-key")
	if err != nil {
		t.Errorf("Second delete should not error, got: %v", err)
	}
}

// TestVectorStoreWithEmptyResults verifies VectorStore handles empty results correctly
func TestVectorStoreWithEmptyResults(t *testing.T) {
	ctx := context.Background()
	store := newMockVectorStore()

	// Search in empty store
	results, err := store.SimilaritySearch(ctx, "anything", 10)
	if err != nil {
		t.Fatalf("SimilaritySearch on empty store should not error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results from empty store, got %d", len(results))
	}
}

// TestVectorStoreTopKBoundary verifies topK parameter boundary conditions
func TestVectorStoreTopKBoundary(t *testing.T) {
	ctx := context.Background()
	store := newMockVectorStore()

	// Add 5 documents
	docs := []*Document{
		{ID: "1", PageContent: "Doc 1"},
		{ID: "2", PageContent: "Doc 2"},
		{ID: "3", PageContent: "Doc 3"},
		{ID: "4", PageContent: "Doc 4"},
		{ID: "5", PageContent: "Doc 5"},
	}
	store.AddDocuments(ctx, docs)

	// Test topK = 0 (should return 0 results)
	results, _ := store.SimilaritySearch(ctx, "test", 0)
	if len(results) != 0 {
		t.Errorf("Expected 0 results with topK=0, got %d", len(results))
	}

	// Test topK = 3 (should return 3 results)
	results, _ = store.SimilaritySearch(ctx, "test", 3)
	if len(results) != 3 {
		t.Errorf("Expected 3 results with topK=3, got %d", len(results))
	}

	// Test topK > available documents (should return all available)
	results, _ = store.SimilaritySearch(ctx, "test", 10)
	if len(results) != 5 {
		t.Errorf("Expected 5 results (all available) with topK=10, got %d", len(results))
	}
}

// TestDocumentMetadataFlexibility verifies Document metadata can hold various types
func TestDocumentMetadataFlexibility(t *testing.T) {
	doc := &Document{
		ID:          "flexible-doc",
		PageContent: "Content",
		Metadata: map[string]interface{}{
			"string_field": "text",
			"int_field":    123,
			"float_field":  45.67,
			"bool_field":   true,
			"array_field":  []string{"a", "b", "c"},
			"object_field": map[string]interface{}{"nested": "value"},
			"nil_field":    nil,
		},
	}

	// Verify all field types
	if doc.Metadata["string_field"] != "text" {
		t.Error("String field not preserved")
	}
	if doc.Metadata["int_field"] != 123 {
		t.Error("Int field not preserved")
	}
	if doc.Metadata["float_field"] != 45.67 {
		t.Error("Float field not preserved")
	}
	if doc.Metadata["bool_field"] != true {
		t.Error("Bool field not preserved")
	}
	if doc.Metadata["array_field"] == nil {
		t.Error("Array field not preserved")
	}
	if doc.Metadata["object_field"] == nil {
		t.Error("Object field not preserved")
	}
	if doc.Metadata["nil_field"] != nil {
		t.Error("Nil field should be nil")
	}
}
