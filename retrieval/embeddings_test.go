package retrieval

import (
	"context"
	"testing"
)

func TestSimpleEmbedder(t *testing.T) {
	ctx := context.Background()

	embedder := NewSimpleEmbedder(100)

	t.Run("Embed single text", func(t *testing.T) {
		texts := []string{"Hello world"}
		vectors, err := embedder.Embed(ctx, texts)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(vectors) != 1 {
			t.Fatalf("Expected 1 vector, got %d", len(vectors))
		}

		if len(vectors[0]) != 100 {
			t.Fatalf("Expected vector dimension 100, got %d", len(vectors[0]))
		}
	})

	t.Run("Embed multiple texts", func(t *testing.T) {
		texts := []string{
			"Machine learning is amazing",
			"Artificial intelligence is the future",
			"Deep learning networks",
		}

		vectors, err := embedder.Embed(ctx, texts)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(vectors) != 3 {
			t.Fatalf("Expected 3 vectors, got %d", len(vectors))
		}

		for i, vec := range vectors {
			if len(vec) != 100 {
				t.Errorf("Vector %d: expected dimension 100, got %d", i, len(vec))
			}
		}
	})

	t.Run("EmbedQuery", func(t *testing.T) {
		query := "What is machine learning?"
		vector, err := embedder.EmbedQuery(ctx, query)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(vector) != 100 {
			t.Fatalf("Expected vector dimension 100, got %d", len(vector))
		}
	})

	t.Run("Dimensions", func(t *testing.T) {
		if embedder.Dimensions() != 100 {
			t.Errorf("Expected dimensions 100, got %d", embedder.Dimensions())
		}
	})
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		vec1     []float32
		vec2     []float32
		expected float32
		wantErr  bool
	}{
		{
			name:     "Identical vectors",
			vec1:     []float32{1.0, 0, 0},
			vec2:     []float32{1.0, 0, 0},
			expected: 1.0,
			wantErr:  false,
		},
		{
			name:     "Orthogonal vectors",
			vec1:     []float32{1.0, 0, 0},
			vec2:     []float32{0, 1.0, 0},
			expected: 0.0,
			wantErr:  false,
		},
		{
			name:     "Different length vectors",
			vec1:     []float32{1.0, 0},
			vec2:     []float32{1.0, 0, 0},
			expected: 0.0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sim, err := cosineSimilarity(tt.vec1, tt.vec2)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}

			// 允许浮点误差
			if sim < tt.expected-0.01 || sim > tt.expected+0.01 {
				t.Errorf("Expected similarity %.2f, got %.2f", tt.expected, sim)
			}
		})
	}
}

func TestMemoryVectorStore(t *testing.T) {
	ctx := context.Background()

	config := MemoryVectorStoreConfig{
		Embedder:       NewSimpleEmbedder(50),
		DistanceMetric: DistanceMetricCosine,
	}

	store := NewMemoryVectorStore(config)

	t.Run("Add and retrieve documents", func(t *testing.T) {
		docs := []*Document{
			NewDocument("Machine learning is a subset of AI", map[string]interface{}{"source": "wiki"}),
			NewDocument("Deep learning uses neural networks", map[string]interface{}{"source": "book"}),
			NewDocument("Natural language processing is important", map[string]interface{}{"source": "paper"}),
		}

		err := store.AddDocuments(ctx, docs)
		if err != nil {
			t.Fatalf("Failed to add documents: %v", err)
		}

		if store.Count() != 3 {
			t.Errorf("Expected 3 documents, got %d", store.Count())
		}
	})

	t.Run("Search documents", func(t *testing.T) {
		results, err := store.Search(ctx, "machine learning", 2)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Expected 2 results, got %d", len(results))
		}

		// 结果应该按分数排序
		for i := 0; i < len(results)-1; i++ {
			if results[i].Score < results[i+1].Score {
				t.Error("Results not sorted by score")
			}
		}
	})

	t.Run("Delete documents", func(t *testing.T) {
		docs := store.GetNodes()
		if len(docs) == 0 {
			t.Skip("No documents to delete")
		}

		firstDocID := docs[0].ID
		err := store.Delete(ctx, []string{firstDocID})
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		if store.Count() != 2 {
			t.Errorf("Expected 2 documents after deletion, got %d", store.Count())
		}
	})

	t.Run("Clear store", func(t *testing.T) {
		store.Clear()

		if store.Count() != 0 {
			t.Errorf("Expected 0 documents after clear, got %d", store.Count())
		}
	})
}

// GetNodes 辅助方法，用于测试
func (m *MemoryVectorStore) GetNodes() []*Document {
	m.mu.RLock()
	defer m.mu.RUnlock()

	docs := make([]*Document, 0, len(m.documents))
	for _, docWithVec := range m.documents {
		docs = append(docs, docWithVec.Document)
	}
	return docs
}

func TestRAGRetriever(t *testing.T) {
	ctx := context.Background()

	// 创建向量存储
	embedder := NewSimpleEmbedder(50)
	store := NewMemoryVectorStore(MemoryVectorStoreConfig{
		Embedder:       embedder,
		DistanceMetric: DistanceMetricCosine,
	})

	// 添加测试文档
	docs := []*Document{
		NewDocument("Python is a programming language", map[string]interface{}{"type": "programming"}),
		NewDocument("Machine learning models need data", map[string]interface{}{"type": "ml"}),
		NewDocument("Neural networks have layers", map[string]interface{}{"type": "dl"}),
	}
	_ = store.AddDocuments(ctx, docs)

	// 创建 RAG 检索器
	config := RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             2,
		ScoreThreshold:   0,
		IncludeMetadata:  true,
		MaxContentLength: 1000,
	}

	retriever, err := NewRAGRetriever(config)
	if err != nil {
		t.Fatalf("Failed to create RAG retriever: %v", err)
	}

	t.Run("Retrieve documents", func(t *testing.T) {
		results, err := retriever.Retrieve(ctx, "programming languages")
		if err != nil {
			t.Fatalf("Retrieve failed: %v", err)
		}

		if len(results) > 2 {
			t.Errorf("Expected at most 2 results, got %d", len(results))
		}
	})

	t.Run("RetrieveWithContext", func(t *testing.T) {
		context, err := retriever.RetrieveWithContext(ctx, "What is machine learning?")
		if err != nil {
			t.Fatalf("RetrieveWithContext failed: %v", err)
		}

		if context == "" {
			t.Error("Expected non-empty context")
		}

		// 上下文应该包含查询
		if len(context) < 10 {
			t.Error("Context seems too short")
		}
	})

	t.Run("SetTopK", func(t *testing.T) {
		retriever.SetTopK(1)

		results, err := retriever.Retrieve(ctx, "machine learning")
		if err != nil {
			t.Fatalf("Retrieve failed: %v", err)
		}

		if len(results) > 1 {
			t.Errorf("Expected at most 1 result after SetTopK(1), got %d", len(results))
		}
	})
}

func BenchmarkSimpleEmbedder(b *testing.B) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(100)

	texts := []string{
		"Machine learning is a method of data analysis",
		"Artificial intelligence mimics human intelligence",
		"Deep learning is a subset of machine learning",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = embedder.Embed(ctx, texts)
	}
}

func BenchmarkMemoryVectorStoreSearch(b *testing.B) {
	ctx := context.Background()

	embedder := NewSimpleEmbedder(50)
	store := NewMemoryVectorStore(MemoryVectorStoreConfig{
		Embedder:       embedder,
		DistanceMetric: DistanceMetricCosine,
	})

	// 添加测试文档
	docs := make([]*Document, 100)
	for i := 0; i < 100; i++ {
		docs[i] = NewDocument("This is document content", nil)
	}
	_ = store.AddDocuments(ctx, docs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Search(ctx, "document", 10)
	}
}
