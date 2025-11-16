package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInMemoryVectorStore(t *testing.T) {
	dimension := 128
	store := NewInMemoryVectorStore(dimension)

	assert.NotNil(t, store)
	assert.Equal(t, dimension, store.dimension)
	assert.NotNil(t, store.vectors)
	assert.Equal(t, 0, store.Size())
}

func TestInMemoryVectorStore_Store(t *testing.T) {
	store := NewInMemoryVectorStore(3)
	ctx := context.Background()

	t.Run("store valid vector", func(t *testing.T) {
		vector := []float32{1.0, 2.0, 3.0}
		err := store.Store(ctx, "vec1", vector)
		require.NoError(t, err)
		assert.Equal(t, 1, store.Size())
	})

	t.Run("dimension mismatch", func(t *testing.T) {
		vector := []float32{1.0, 2.0} // Wrong dimension
		err := store.Store(ctx, "vec2", vector)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dimension mismatch")
	})

	t.Run("overwrite existing vector", func(t *testing.T) {
		vector1 := []float32{1.0, 2.0, 3.0}
		vector2 := []float32{4.0, 5.0, 6.0}

		err := store.Store(ctx, "vec3", vector1)
		require.NoError(t, err)

		err = store.Store(ctx, "vec3", vector2)
		require.NoError(t, err)

		// Should still have same size
		assert.Equal(t, 2, store.Size()) // vec1 and vec3
	})
}

func TestInMemoryVectorStore_Search(t *testing.T) {
	store := NewInMemoryVectorStore(3)
	ctx := context.Background()

	// Add test vectors
	vectors := map[string][]float32{
		"vec1": {1.0, 0.0, 0.0},
		"vec2": {0.0, 1.0, 0.0},
		"vec3": {0.0, 0.0, 1.0},
		"vec4": {0.9, 0.1, 0.0}, // Similar to vec1
	}

	for id, vec := range vectors {
		err := store.Store(ctx, id, vec)
		require.NoError(t, err)
	}

	t.Run("search similar vectors", func(t *testing.T) {
		query := []float32{1.0, 0.0, 0.0}
		ids, scores, err := store.Search(ctx, query, 2, 0.5)
		require.NoError(t, err)
		assert.NotEmpty(t, ids)
		assert.NotEmpty(t, scores)
		assert.Equal(t, len(ids), len(scores))
	})

	t.Run("search with threshold", func(t *testing.T) {
		query := []float32{1.0, 0.0, 0.0}
		_, scores, err := store.Search(ctx, query, 10, 0.9)
		require.NoError(t, err)
		// Should return only very similar vectors
		for _, score := range scores {
			assert.GreaterOrEqual(t, score, 0.9)
		}
	})

	t.Run("dimension mismatch", func(t *testing.T) {
		query := []float32{1.0, 0.0} // Wrong dimension
		ids, scores, err := store.Search(ctx, query, 2, 0.5)
		assert.Error(t, err)
		assert.Nil(t, ids)
		assert.Nil(t, scores)
		assert.Contains(t, err.Error(), "dimension mismatch")
	})

	t.Run("limit results", func(t *testing.T) {
		query := []float32{1.0, 0.0, 0.0}
		ids, scores, err := store.Search(ctx, query, 2, 0.0)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(ids), 2)
		assert.LessOrEqual(t, len(scores), 2)
	})
}

func TestInMemoryVectorStore_Delete(t *testing.T) {
	store := NewInMemoryVectorStore(3)
	ctx := context.Background()

	vector := []float32{1.0, 2.0, 3.0}
	err := store.Store(ctx, "vec1", vector)
	require.NoError(t, err)

	t.Run("delete existing vector", func(t *testing.T) {
		err := store.Delete(ctx, "vec1")
		require.NoError(t, err)
		assert.Equal(t, 0, store.Size())
	})

	t.Run("delete non-existent vector", func(t *testing.T) {
		err := store.Delete(ctx, "non-existent")
		require.NoError(t, err) // Should not error
	})
}

func TestInMemoryVectorStore_GenerateEmbedding(t *testing.T) {
	dimension := 10
	store := NewInMemoryVectorStore(dimension)
	ctx := context.Background()

	t.Run("generate embedding for string", func(t *testing.T) {
		content := "test content"
		embedding, err := store.GenerateEmbedding(ctx, content)
		require.NoError(t, err)
		assert.Len(t, embedding, dimension)
	})

	t.Run("generate embedding for map", func(t *testing.T) {
		content := map[string]interface{}{
			"key": "value",
		}
		embedding, err := store.GenerateEmbedding(ctx, content)
		require.NoError(t, err)
		assert.Len(t, embedding, dimension)
	})

	t.Run("consistent embeddings", func(t *testing.T) {
		content := "consistent content"
		embedding1, err := store.GenerateEmbedding(ctx, content)
		require.NoError(t, err)

		embedding2, err := store.GenerateEmbedding(ctx, content)
		require.NoError(t, err)

		// Same content should produce same embedding
		assert.Equal(t, embedding1, embedding2)
	})
}

func TestInMemoryVectorStore_Clear(t *testing.T) {
	store := NewInMemoryVectorStore(3)
	ctx := context.Background()

	// Add multiple vectors
	for i := 0; i < 5; i++ {
		vector := []float32{float32(i), float32(i), float32(i)}
		err := store.Store(ctx, "vec"+string(rune(i)), vector)
		require.NoError(t, err)
	}

	assert.Equal(t, 5, store.Size())

	err := store.Clear(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, store.Size())
}

func TestInMemoryVectorStore_Size(t *testing.T) {
	store := NewInMemoryVectorStore(3)
	ctx := context.Background()

	assert.Equal(t, 0, store.Size())

	vector := []float32{1.0, 2.0, 3.0}
	err := store.Store(ctx, "vec1", vector)
	require.NoError(t, err)
	assert.Equal(t, 1, store.Size())

	err = store.Store(ctx, "vec2", vector)
	require.NoError(t, err)
	assert.Equal(t, 2, store.Size())
}

func TestNormalizeVector(t *testing.T) {
	t.Run("normalize normal vector", func(t *testing.T) {
		vector := []float32{3.0, 4.0}
		normalized := normalizeVector(vector)

		// Check unit length
		magnitude := float32(0)
		for _, v := range normalized {
			magnitude += v * v
		}
		assert.InDelta(t, 1.0, magnitude, 0.0001)
	})

	t.Run("normalize zero vector", func(t *testing.T) {
		vector := []float32{0.0, 0.0, 0.0}
		normalized := normalizeVector(vector)
		assert.Equal(t, vector, normalized) // Should return as-is
	})
}

func TestCosineSimilarity(t *testing.T) {
	t.Run("identical vectors", func(t *testing.T) {
		v1 := []float32{1.0, 0.0, 0.0}
		v2 := []float32{1.0, 0.0, 0.0}
		sim := cosineSimilarity(v1, v2)
		assert.InDelta(t, 1.0, sim, 0.0001)
	})

	t.Run("orthogonal vectors", func(t *testing.T) {
		v1 := []float32{1.0, 0.0}
		v2 := []float32{0.0, 1.0}
		sim := cosineSimilarity(v1, v2)
		assert.InDelta(t, 0.0, sim, 0.0001)
	})

	t.Run("opposite vectors", func(t *testing.T) {
		v1 := []float32{1.0, 0.0}
		v2 := []float32{-1.0, 0.0}
		sim := cosineSimilarity(v1, v2)
		assert.InDelta(t, -1.0, sim, 0.0001)
	})

	t.Run("different dimensions", func(t *testing.T) {
		v1 := []float32{1.0, 0.0}
		v2 := []float32{1.0, 0.0, 0.0}
		sim := cosineSimilarity(v1, v2)
		assert.Equal(t, 0.0, sim)
	})
}

func TestHashBytes(t *testing.T) {
	t.Run("consistent hash", func(t *testing.T) {
		data := []byte("test data")
		hash1 := hashBytes(data, 0)
		hash2 := hashBytes(data, 0)
		assert.Equal(t, hash1, hash2)
	})

	t.Run("different seeds", func(t *testing.T) {
		data := []byte("test data")
		hash1 := hashBytes(data, 0)
		hash2 := hashBytes(data, 1)
		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("different data", func(t *testing.T) {
		data1 := []byte("test data 1")
		data2 := []byte("test data 2")
		hash1 := hashBytes(data1, 0)
		hash2 := hashBytes(data2, 0)
		assert.NotEqual(t, hash1, hash2)
	})
}

func TestSimpleEmbeddingModel(t *testing.T) {
	dimension := 10
	model := NewSimpleEmbeddingModel(dimension)

	t.Run("create model", func(t *testing.T) {
		assert.NotNil(t, model)
		assert.Equal(t, dimension, model.Dimension())
	})

	t.Run("embed text", func(t *testing.T) {
		text := "test text"
		embedding, err := model.Embed(context.Background(), text)
		require.NoError(t, err)
		assert.Len(t, embedding, dimension)
	})

	t.Run("embed batch", func(t *testing.T) {
		texts := []string{"text1", "text2", "text3"}
		embeddings, err := model.EmbedBatch(context.Background(), texts)
		require.NoError(t, err)
		assert.Len(t, embeddings, 3)
		for _, emb := range embeddings {
			assert.Len(t, emb, dimension)
		}
	})

	t.Run("consistent embeddings", func(t *testing.T) {
		text := "consistent text"
		emb1, err := model.Embed(context.Background(), text)
		require.NoError(t, err)

		emb2, err := model.Embed(context.Background(), text)
		require.NoError(t, err)

		assert.Equal(t, emb1, emb2)
	})
}

func TestEmbeddingVectorStore(t *testing.T) {
	dimension := 10
	embedder := NewSimpleEmbeddingModel(dimension)
	store := NewEmbeddingVectorStore(embedder)
	ctx := context.Background()

	t.Run("create store", func(t *testing.T) {
		assert.NotNil(t, store)
		assert.NotNil(t, store.embedder)
	})

	t.Run("generate embedding with model", func(t *testing.T) {
		content := "test content"
		embedding, err := store.GenerateEmbedding(ctx, content)
		require.NoError(t, err)
		assert.Len(t, embedding, dimension)
	})

	t.Run("store and search", func(t *testing.T) {
		// Store vector
		vector := []float32{1.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0}
		err := store.Store(ctx, "test_vec", vector)
		require.NoError(t, err)

		// Search
		query := []float32{1.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0}
		ids, scores, err := store.Search(ctx, query, 1, 0.5)
		require.NoError(t, err)
		assert.NotEmpty(t, ids)
		assert.NotEmpty(t, scores)
	})
}

// Benchmark tests
func BenchmarkInMemoryVectorStore_Store(b *testing.B) {
	store := NewInMemoryVectorStore(128)
	ctx := context.Background()
	vector := make([]float32, 128)
	for i := range vector {
		vector[i] = float32(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Store(ctx, "vec", vector)
	}
}

func BenchmarkInMemoryVectorStore_Search(b *testing.B) {
	store := NewInMemoryVectorStore(128)
	ctx := context.Background()

	// Prepare data
	for i := 0; i < 1000; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = float32(i + j)
		}
		store.Store(ctx, "vec"+string(rune(i)), vector)
	}

	query := make([]float32, 128)
	for i := range query {
		query[i] = float32(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Search(ctx, query, 10, 0.5)
	}
}

func BenchmarkNormalizeVector(b *testing.B) {
	vector := make([]float32, 128)
	for i := range vector {
		vector[i] = float32(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		normalizeVector(vector)
	}
}

func BenchmarkCosineSimilarity(b *testing.B) {
	v1 := make([]float32, 128)
	v2 := make([]float32, 128)
	for i := range v1 {
		v1[i] = float32(i)
		v2[i] = float32(i + 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cosineSimilarity(v1, v2)
	}
}
