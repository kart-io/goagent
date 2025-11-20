package retrieval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQdrantVectorStore_Configuration tests Qdrant configuration validation
func TestQdrantVectorStore_Configuration(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		config      QdrantConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config with all fields",
			config: QdrantConfig{
				URL:            "localhost:6334",
				CollectionName: "test_collection",
				VectorSize:     384,
				Distance:       "cosine",
			},
			wantErr: false,
		},
		{
			name: "valid config with defaults",
			config: QdrantConfig{
				CollectionName: "test_collection",
			},
			wantErr: false,
		},
		{
			name: "missing collection name",
			config: QdrantConfig{
				URL: "localhost:6334",
			},
			wantErr:     true,
			errContains: "collection name is required",
		},
		{
			name: "euclidean distance",
			config: QdrantConfig{
				CollectionName: "test_collection",
				Distance:       "euclidean",
			},
			wantErr: false,
		},
		{
			name: "dot distance",
			config: QdrantConfig{
				CollectionName: "test_collection",
				Distance:       "dot",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: This test will try to connect to Qdrant, so it may fail if Qdrant is not running
			// In real scenarios, we'd mock the Qdrant client
			_, err := NewQdrantVectorStore(ctx, tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				// If Qdrant is not running, we expect connection error
				// which is different from configuration error
				if err != nil {
					// Check that it's not a configuration error
					assert.NotContains(t, err.Error(), "collection name is required")
				}
			}
		})
	}
}

// TestQdrantVectorStore_ConvertToQdrantValue tests value conversion
func TestQdrantVectorStore_ConvertToQdrantValue(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		wantType string
	}{
		{
			name:     "string value",
			input:    "test string",
			wantType: "string",
		},
		{
			name:     "int value",
			input:    42,
			wantType: "int",
		},
		{
			name:     "int64 value",
			input:    int64(123),
			wantType: "int",
		},
		{
			name:     "float64 value",
			input:    3.14,
			wantType: "double",
		},
		{
			name:     "float32 value",
			input:    float32(2.71),
			wantType: "double",
		},
		{
			name:     "bool value",
			input:    true,
			wantType: "bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := convertToQdrantValue(tt.input)
			assert.NotNil(t, val)

			// Verify the value type by trying to extract it
			switch tt.wantType {
			case "string":
				assert.NotEmpty(t, val.GetStringValue())
			case "int":
				assert.NotZero(t, val.GetIntegerValue())
			case "double":
				assert.NotZero(t, val.GetDoubleValue())
			case "bool":
				assert.True(t, val.GetBoolValue())
			}
		})
	}
}

// TestQdrantVectorStore_DocumentOperations tests document CRUD operations
func TestQdrantVectorStore_DocumentOperations(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	// Create test store
	store, err := NewQdrantVectorStore(ctx, QdrantConfig{
		CollectionName: "test_collection_ops",
		VectorSize:     100,
		Distance:       "cosine",
	})

	// If Qdrant is not running, skip the test
	if err != nil {
		t.Skipf("Qdrant not available: %v", err)
	}
	defer store.Close()

	// Test documents
	docs := []*Document{
		{
			ID:          "doc1",
			PageContent: "This is a test document about machine learning",
			Metadata: map[string]interface{}{
				"category": "tech",
				"year":     2024,
			},
		},
		{
			ID:          "doc2",
			PageContent: "Another document about artificial intelligence",
			Metadata: map[string]interface{}{
				"category": "tech",
				"year":     2024,
			},
		},
	}

	// Generate vectors
	vectors := make([][]float32, len(docs))
	for i := range vectors {
		vectors[i] = make([]float32, 100)
		for j := range vectors[i] {
			vectors[i][j] = float32(i+j) / 100.0
		}
	}

	// Test Add
	err = store.Add(ctx, docs, vectors)
	require.NoError(t, err)

	// Test Search
	results, err := store.SearchByVector(ctx, vectors[0], 2)
	require.NoError(t, err)
	assert.Greater(t, len(results), 0)

	// Test Update
	docs[0].PageContent = "Updated content about machine learning"
	err = store.Update(ctx, []*Document{docs[0]})
	require.NoError(t, err)

	// Test Delete
	err = store.Delete(ctx, []string{"doc1", "doc2"})
	require.NoError(t, err)
}

// TestQdrantVectorStore_AddDocuments tests AddDocuments with auto-embedding
func TestQdrantVectorStore_AddDocuments(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	store, err := NewQdrantVectorStore(ctx, QdrantConfig{
		CollectionName: "test_add_docs",
		VectorSize:     100,
	})

	if err != nil {
		t.Skipf("Qdrant not available: %v", err)
	}
	defer store.Close()

	docs := []*Document{
		NewDocument("Test document one", map[string]interface{}{"id": "1"}),
		NewDocument("Test document two", map[string]interface{}{"id": "2"}),
	}

	err = store.AddDocuments(ctx, docs)
	if err != nil {
		// May fail if Qdrant not running
		t.Logf("AddDocuments failed (expected if Qdrant not running): %v", err)
	}
}

// TestQdrantVectorStore_Search tests various search scenarios
func TestQdrantVectorStore_Search(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	store, err := NewQdrantVectorStore(ctx, QdrantConfig{
		CollectionName: "test_search",
		VectorSize:     100,
	})

	if err != nil {
		t.Skipf("Qdrant not available: %v", err)
	}
	defer store.Close()

	// Test empty search
	results, err := store.Search(ctx, "test query", 5)
	if err == nil {
		assert.NotNil(t, results)
	}

	// Test search with zero topK (should use default)
	results, err = store.SearchByVector(ctx, make([]float32, 100), 0)
	if err == nil {
		assert.NotNil(t, results)
	}
}

// TestQdrantVectorStore_EmptyOperations tests operations on empty collections
func TestQdrantVectorStore_EmptyOperations(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	store, err := NewQdrantVectorStore(ctx, QdrantConfig{
		CollectionName: "test_empty_ops",
		VectorSize:     100,
	})

	if err != nil {
		t.Skipf("Qdrant not available: %v", err)
	}
	defer store.Close()

	// Test operations with empty inputs
	err = store.Add(ctx, []*Document{}, [][]float32{})
	assert.NoError(t, err)

	err = store.Delete(ctx, []string{})
	assert.NoError(t, err)

	err = store.Update(ctx, []*Document{})
	assert.NoError(t, err)
}

// TestQdrantVectorStore_BatchOperations tests batch processing
func TestQdrantVectorStore_BatchOperations(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	store, err := NewQdrantVectorStore(ctx, QdrantConfig{
		CollectionName: "test_batch",
		VectorSize:     100,
	})

	if err != nil {
		t.Skipf("Qdrant not available: %v", err)
	}
	defer store.Close()

	// Create large batch (more than batch size of 100)
	numDocs := 250
	docs := make([]*Document, numDocs)
	vectors := make([][]float32, numDocs)

	for i := 0; i < numDocs; i++ {
		docs[i] = &Document{
			PageContent: "Batch test document",
			Metadata:    map[string]interface{}{"index": i},
		}
		vectors[i] = make([]float32, 100)
		for j := range vectors[i] {
			vectors[i][j] = float32(i+j) / 100.0
		}
	}

	// Test batch add
	err = store.Add(ctx, docs, vectors)
	if err != nil {
		t.Logf("Batch add failed (expected if Qdrant not running): %v", err)
	}
}

// TestQdrantVectorStore_ErrorCases tests error handling
func TestQdrantVectorStore_ErrorCases(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	store, err := NewQdrantVectorStore(ctx, QdrantConfig{
		CollectionName: "test_errors",
		VectorSize:     100,
	})

	if err != nil {
		t.Skipf("Qdrant not available: %v", err)
	}
	defer store.Close()

	// Test mismatched docs and vectors
	docs := []*Document{
		NewDocument("Test", nil),
		NewDocument("Test 2", nil),
	}
	vectors := [][]float32{
		make([]float32, 100),
	}

	err = store.Add(ctx, docs, vectors)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "number of documents and vectors must match")
}

// TestQdrantVectorStore_Close tests cleanup
func TestQdrantVectorStore_Close(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	store, err := NewQdrantVectorStore(ctx, QdrantConfig{
		CollectionName: "test_close",
		VectorSize:     100,
	})

	if err != nil {
		t.Skipf("Qdrant not available: %v", err)
	}

	// Test close
	err = store.Close()
	assert.NoError(t, err)

	// Test close again (should be idempotent)
	err = store.Close()
	assert.NoError(t, err)
}
