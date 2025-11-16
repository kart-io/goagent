package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockVectorStore implements VectorStore interface for testing
type mockVectorStore struct {
	vectors map[string][]float64
}

func newMockVectorStore() *mockVectorStore {
	return &mockVectorStore{
		vectors: make(map[string][]float64),
	}
}

func (m *mockVectorStore) Add(ctx context.Context, id string, embedding []float64, metadata map[string]interface{}) error {
	m.vectors[id] = embedding
	return nil
}

func (m *mockVectorStore) Search(ctx context.Context, embedding []float64, limit int) ([]*SearchResult, error) {
	results := make([]*SearchResult, 0)
	count := 0
	for id, vec := range m.vectors {
		if count >= limit {
			break
		}
		results = append(results, &SearchResult{
			ID:        id,
			Score:     0.9,
			Embedding: vec,
			Metadata:  make(map[string]interface{}),
		})
		count++
	}
	return results, nil
}

func (m *mockVectorStore) Delete(ctx context.Context, id string) error {
	delete(m.vectors, id)
	return nil
}

func (m *mockVectorStore) Clear(ctx context.Context) error {
	m.vectors = make(map[string][]float64)
	return nil
}

func TestNewHierarchicalMemory(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)

	assert.NotNil(t, hm)
	assert.NotNil(t, hm.shortTerm)
	assert.NotNil(t, hm.longTerm)
	assert.NotNil(t, hm.vectorStore)
	assert.NotNil(t, hm.consolidator)
}

func TestHierarchicalMemory_WithOptions(t *testing.T) {
	vectorStore := newMockVectorStore()

	t.Run("with short-term capacity", func(t *testing.T) {
		hm := NewHierarchicalMemory(vectorStore, WithShortTermCapacity(50))
		assert.Equal(t, 50, hm.shortTermCapacity)
	})

	t.Run("with decay rate", func(t *testing.T) {
		hm := NewHierarchicalMemory(vectorStore, WithDecayRate(0.05))
		assert.Equal(t, 0.05, hm.decayRate)
	})

	t.Run("with multiple options", func(t *testing.T) {
		hm := NewHierarchicalMemory(
			vectorStore,
			WithShortTermCapacity(75),
			WithDecayRate(0.02),
		)
		assert.Equal(t, 75, hm.shortTermCapacity)
		assert.Equal(t, 0.02, hm.decayRate)
	})
}

func TestHierarchicalMemory_Store(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	t.Run("store in short-term memory", func(t *testing.T) {
		err := hm.Store(ctx, "key1", "value1", StoreOptions{
			Tags: []string{"tag1"},
		})
		require.NoError(t, err)
	})

	t.Run("store complex value", func(t *testing.T) {
		value := map[string]interface{}{
			"field1": "value1",
			"field2": 123,
		}
		err := hm.Store(ctx, "key2", value, StoreOptions{
			Tags: []string{"tag2"},
		})
		require.NoError(t, err)
	})
}

func TestHierarchicalMemory_StoreTyped(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	t.Run("store short-term memory", func(t *testing.T) {
		err := hm.StoreTyped(ctx, "st_key", "short term value", MemoryTypeShortTerm, StoreOptions{})
		require.NoError(t, err)

		value, err := hm.Get(ctx, "st_key")
		require.NoError(t, err)
		assert.Equal(t, "short term value", value)
	})

	t.Run("store long-term memory", func(t *testing.T) {
		err := hm.StoreTyped(ctx, "lt_key", "long term value", MemoryTypeLongTerm, StoreOptions{})
		require.NoError(t, err)

		value, err := hm.Get(ctx, "lt_key")
		require.NoError(t, err)
		assert.Equal(t, "long term value", value)
	})

	t.Run("store episodic memory", func(t *testing.T) {
		err := hm.StoreTyped(ctx, "ep_key", "episodic value", MemoryTypeEpisodic, StoreOptions{})
		require.NoError(t, err)
	})

	t.Run("store semantic memory", func(t *testing.T) {
		err := hm.StoreTyped(ctx, "sem_key", "semantic value", MemoryTypeSemantic, StoreOptions{})
		require.NoError(t, err)
	})

	t.Run("store procedural memory", func(t *testing.T) {
		err := hm.StoreTyped(ctx, "proc_key", "procedural value", MemoryTypeProcedural, StoreOptions{})
		require.NoError(t, err)
	})
}

func TestHierarchicalMemory_Get(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Store in short-term
	err := hm.StoreTyped(ctx, "st_key", "short value", MemoryTypeShortTerm, StoreOptions{})
	require.NoError(t, err)

	// Store in long-term
	err = hm.StoreTyped(ctx, "lt_key", "long value", MemoryTypeLongTerm, StoreOptions{})
	require.NoError(t, err)

	t.Run("get from short-term", func(t *testing.T) {
		value, err := hm.Get(ctx, "st_key")
		require.NoError(t, err)
		assert.Equal(t, "short value", value)
	})

	t.Run("get from long-term", func(t *testing.T) {
		value, err := hm.Get(ctx, "lt_key")
		require.NoError(t, err)
		assert.Equal(t, "long value", value)
	})

	t.Run("get non-existent", func(t *testing.T) {
		value, err := hm.Get(ctx, "non_existent")
		assert.Error(t, err)
		assert.Nil(t, value)
	})
}

func TestHierarchicalMemory_Search(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Store test data
	err := hm.StoreTyped(ctx, "k1", "search test content", MemoryTypeShortTerm, StoreOptions{})
	require.NoError(t, err)

	err = hm.StoreTyped(ctx, "k2", "another test", MemoryTypeLongTerm, StoreOptions{})
	require.NoError(t, err)

	t.Run("search memories", func(t *testing.T) {
		// Note: Simple text matching has limitations
		results, err := hm.Search(ctx, "search test content", 10)
		require.NoError(t, err)
		assert.NotNil(t, results)
	})

	t.Run("search with limit", func(t *testing.T) {
		results, err := hm.Search(ctx, "test", 1)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(results), 1)
	})
}

func TestHierarchicalMemory_VectorSearch(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	t.Run("vector search without store", func(t *testing.T) {
		hmNoStore := NewHierarchicalMemory(nil)
		embedding := make([]float32, 128)
		results, err := hmNoStore.VectorSearch(ctx, embedding, 5, 0.5)
		assert.Error(t, err)
		assert.Nil(t, results)
	})

	t.Run("vector search with store", func(t *testing.T) {
		embedding := make([]float32, 128)
		for i := range embedding {
			embedding[i] = 0.1
		}

		results, err := hm.VectorSearch(ctx, embedding, 5, 0.5)
		require.NoError(t, err)
		// Mock store returns empty results, which is expected
		assert.Empty(t, results)
	})
}

func TestHierarchicalMemory_GetByType(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Store different types
	hm.StoreTyped(ctx, "st1", "short1", MemoryTypeShortTerm, StoreOptions{})
	hm.StoreTyped(ctx, "st2", "short2", MemoryTypeShortTerm, StoreOptions{})
	hm.StoreTyped(ctx, "lt1", "long1", MemoryTypeLongTerm, StoreOptions{})
	hm.StoreTyped(ctx, "ep1", "episodic1", MemoryTypeEpisodic, StoreOptions{})

	t.Run("get short-term memories", func(t *testing.T) {
		results, err := hm.GetByType(ctx, MemoryTypeShortTerm, 10)
		require.NoError(t, err)
		assert.Len(t, results, 2)
	})

	t.Run("get long-term memories", func(t *testing.T) {
		results, err := hm.GetByType(ctx, MemoryTypeLongTerm, 10)
		require.NoError(t, err)
		assert.Len(t, results, 1)
	})

	t.Run("get with limit", func(t *testing.T) {
		results, err := hm.GetByType(ctx, MemoryTypeShortTerm, 1)
		require.NoError(t, err)
		assert.Len(t, results, 1)
	})
}

func TestHierarchicalMemory_Consolidate(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Store high-importance short-term memories
	for i := 0; i < 5; i++ {
		entry := &MemoryEntry{
			ID:          "mem" + string(rune(i)),
			Type:        MemoryTypeShortTerm,
			Content:     "content",
			Importance:  0.8,
			AccessCount: 10,
		}
		hm.shortTerm.Store(ctx, entry)
	}

	t.Run("consolidate memories", func(t *testing.T) {
		err := hm.Consolidate(ctx)
		require.NoError(t, err)

		// Important memories should be moved to long-term
		assert.Greater(t, hm.longTerm.Size(), 0)
	})
}

func TestHierarchicalMemory_Forget(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Store memories with different importance
	lowImportance := &MemoryEntry{
		ID:         "low",
		Type:       MemoryTypeShortTerm,
		Content:    "low importance",
		Importance: 0.1,
		LastAccess: time.Now().Add(-24 * time.Hour),
	}
	hm.shortTerm.Store(ctx, lowImportance)

	highImportance := &MemoryEntry{
		ID:         "high",
		Type:       MemoryTypeShortTerm,
		Content:    "high importance",
		Importance: 0.9,
		LastAccess: time.Now(),
	}
	hm.shortTerm.Store(ctx, highImportance)

	t.Run("forget unimportant memories", func(t *testing.T) {
		err := hm.Forget(ctx, 0.5)
		require.NoError(t, err)

		// High importance should remain
		_, err = hm.Get(ctx, "high")
		assert.NoError(t, err)
	})
}

func TestHierarchicalMemory_Associate(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Store two memories
	hm.StoreTyped(ctx, "mem1", "content1", MemoryTypeShortTerm, StoreOptions{})
	hm.StoreTyped(ctx, "mem2", "content2", MemoryTypeShortTerm, StoreOptions{})

	t.Run("create association", func(t *testing.T) {
		err := hm.Associate(ctx, "mem1", "mem2", 0.8)
		require.NoError(t, err)
	})

	t.Run("associate non-existent memories", func(t *testing.T) {
		err := hm.Associate(ctx, "non1", "non2", 0.8)
		assert.Error(t, err)
	})

	t.Run("associate with one non-existent", func(t *testing.T) {
		err := hm.Associate(ctx, "mem1", "non_existent", 0.8)
		assert.Error(t, err)
	})
}

func TestHierarchicalMemory_GetAssociated(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Store and associate memories
	hm.StoreTyped(ctx, "mem1", "content1", MemoryTypeShortTerm, StoreOptions{})
	hm.StoreTyped(ctx, "mem2", "content2", MemoryTypeShortTerm, StoreOptions{})
	hm.StoreTyped(ctx, "mem3", "content3", MemoryTypeShortTerm, StoreOptions{})

	hm.Associate(ctx, "mem1", "mem2", 0.8)
	hm.Associate(ctx, "mem1", "mem3", 0.7)

	t.Run("get associated memories", func(t *testing.T) {
		associated, err := hm.GetAssociated(ctx, "mem1", 10)
		require.NoError(t, err)
		assert.Len(t, associated, 2)
	})

	t.Run("get with limit", func(t *testing.T) {
		associated, err := hm.GetAssociated(ctx, "mem1", 1)
		require.NoError(t, err)
		assert.Len(t, associated, 1)
	})

	t.Run("get for non-existent memory", func(t *testing.T) {
		associated, err := hm.GetAssociated(ctx, "non_existent", 10)
		assert.Error(t, err)
		assert.Nil(t, associated)
	})
}

func TestHierarchicalMemory_GetStats(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Store different types of memories
	hm.StoreTyped(ctx, "st1", "short1", MemoryTypeShortTerm, StoreOptions{})
	hm.StoreTyped(ctx, "st2", "short2", MemoryTypeShortTerm, StoreOptions{})
	hm.StoreTyped(ctx, "lt1", "long1", MemoryTypeLongTerm, StoreOptions{})
	hm.StoreTyped(ctx, "ep1", "episodic1", MemoryTypeEpisodic, StoreOptions{})
	hm.StoreTyped(ctx, "sem1", "semantic1", MemoryTypeSemantic, StoreOptions{})
	hm.StoreTyped(ctx, "proc1", "procedural1", MemoryTypeProcedural, StoreOptions{})

	t.Run("get statistics", func(t *testing.T) {
		stats := hm.GetStats()
		assert.NotNil(t, stats)
		assert.Equal(t, 6, stats.TotalEntries)
		assert.Equal(t, 2, stats.ShortTermCount)
		assert.Greater(t, stats.EpisodicCount, 0)
		assert.Greater(t, stats.SemanticCount, 0)
		assert.Greater(t, stats.ProceduralCount, 0)
	})
}

func TestHierarchicalMemory_Clear(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Store some memories
	hm.StoreTyped(ctx, "st1", "short1", MemoryTypeShortTerm, StoreOptions{})
	hm.StoreTyped(ctx, "lt1", "long1", MemoryTypeLongTerm, StoreOptions{})

	stats := hm.GetStats()
	assert.Greater(t, stats.TotalEntries, 0)

	t.Run("clear all memory", func(t *testing.T) {
		err := hm.Clear(ctx)
		require.NoError(t, err)

		stats := hm.GetStats()
		assert.Equal(t, 0, stats.TotalEntries)
	})
}

func TestHierarchicalMemory_CalculateImportance(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)

	t.Run("small string", func(t *testing.T) {
		importance := hm.calculateImportance("small")
		assert.Greater(t, importance, 0.0)
		assert.LessOrEqual(t, importance, 1.0)
	})

	t.Run("large string", func(t *testing.T) {
		largeString := make([]byte, 1500)
		importance := hm.calculateImportance(string(largeString))
		assert.Equal(t, 1.0, importance) // Capped at 1.0
	})

	t.Run("complex map", func(t *testing.T) {
		complexMap := map[string]interface{}{
			"key1": "value1",
			"key2": 123,
			"key3": []string{"a", "b", "c"},
		}
		importance := hm.calculateImportance(complexMap)
		assert.Greater(t, importance, 0.0)
		assert.LessOrEqual(t, importance, 1.0)
	})

	t.Run("list", func(t *testing.T) {
		list := []interface{}{"item1", "item2", "item3"}
		importance := hm.calculateImportance(list)
		assert.Greater(t, importance, 0.0)
		assert.LessOrEqual(t, importance, 1.0)
	})
}

func TestHierarchicalMemory_UpdateAccess(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)

	entry := &MemoryEntry{
		ID:          "test",
		LastAccess:  time.Now().Add(-1 * time.Hour),
		AccessCount: 5,
		Importance:  0.5,
	}

	oldAccessCount := entry.AccessCount
	oldImportance := entry.Importance

	hm.updateAccess(entry)

	assert.Equal(t, oldAccessCount+1, entry.AccessCount)
	assert.Greater(t, entry.Importance, oldImportance)
	assert.LessOrEqual(t, entry.Importance, 1.0)
	assert.True(t, time.Since(entry.LastAccess) < 1*time.Second)
}

func TestHierarchicalMemory_ApplyDecay(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore, WithDecayRate(0.01))
	ctx := context.Background()

	// Store memory with old access time
	oldEntry := &MemoryEntry{
		ID:         "old",
		Type:       MemoryTypeShortTerm,
		Content:    "old content",
		LastAccess: time.Now().Add(-24 * time.Hour),
		Importance: 0.9,
		Decay:      1.0,
	}
	hm.shortTerm.Store(ctx, oldEntry)

	// Store recent memory
	recentEntry := &MemoryEntry{
		ID:         "recent",
		Type:       MemoryTypeShortTerm,
		Content:    "recent content",
		LastAccess: time.Now(),
		Importance: 0.9,
		Decay:      1.0,
	}
	hm.shortTerm.Store(ctx, recentEntry)

	hm.applyDecay()

	// Old entry should have lower importance due to decay
	oldRetrieved, _ := hm.shortTerm.Get(ctx, "old")
	recentRetrieved, _ := hm.shortTerm.Get(ctx, "recent")

	assert.Less(t, oldRetrieved.Importance, 0.9)
	assert.Greater(t, recentRetrieved.Importance, oldRetrieved.Importance)
}

func TestHierarchicalMemory_FrequentAccess(t *testing.T) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Store in long-term with high access count
	entry := &MemoryEntry{
		ID:          "frequent",
		Type:        MemoryTypeLongTerm,
		Content:     "frequently accessed",
		AccessCount: 15,
	}
	hm.longTerm.Store(ctx, entry)

	// Access it
	value, err := hm.Get(ctx, "frequent")
	require.NoError(t, err)
	assert.Equal(t, "frequently accessed", value)

	// May or may not be promoted to short-term depending on access threshold
	// Just verify the access worked
	assert.NotNil(t, value)
}

// Benchmark tests
func BenchmarkHierarchicalMemory_Store(b *testing.B) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "key" + string(rune(i))
		hm.Store(ctx, key, "value", StoreOptions{})
	}
}

func BenchmarkHierarchicalMemory_Get(b *testing.B) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Prepare data
	for i := 0; i < 100; i++ {
		key := "key" + string(rune(i))
		hm.Store(ctx, key, "value", StoreOptions{})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "key" + string(rune(i%100))
		hm.Get(ctx, key)
	}
}

func BenchmarkHierarchicalMemory_Search(b *testing.B) {
	vectorStore := newMockVectorStore()
	hm := NewHierarchicalMemory(vectorStore)
	ctx := context.Background()

	// Prepare data
	for i := 0; i < 100; i++ {
		key := "key" + string(rune(i))
		hm.Store(ctx, key, "search test value", StoreOptions{})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hm.Search(ctx, "test", 10)
	}
}
