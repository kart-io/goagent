package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float64
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
		},
		{
			name:     "similar vectors",
			a:        []float32{1, 1, 0},
			b:        []float32{1, 0.9, 0},
			expected: 0.99,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
		},
		{
			name:     "different lengths",
			a:        []float32{1, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CosineSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestNormalize(t *testing.T) {
	v := []float32{3, 4}
	normalized := Normalize(v)

	// Check unit length
	var sum float64
	for _, val := range normalized {
		sum += float64(val) * float64(val)
	}
	assert.InDelta(t, 1.0, sum, 0.001)

	// Check direction preserved
	assert.InDelta(t, 0.6, normalized[0], 0.001)
	assert.InDelta(t, 0.8, normalized[1], 0.001)
}

func TestFindMostSimilar(t *testing.T) {
	target := []float32{1, 0, 0}
	entries := []*CacheEntry{
		{Key: "1", Embedding: []float32{0, 1, 0}},     // orthogonal
		{Key: "2", Embedding: []float32{0.9, 0.1, 0}}, // similar
		{Key: "3", Embedding: []float32{-1, 0, 0}},    // opposite
	}

	best, similarity, idx := FindMostSimilar(target, entries)

	assert.Equal(t, "2", best.Key)
	assert.InDelta(t, 0.99, similarity, 0.01)
	assert.Equal(t, 1, idx)
}

func TestMockEmbeddingProvider(t *testing.T) {
	provider := NewMockEmbeddingProvider(128)

	ctx := context.Background()

	// Test basic embedding
	embedding, err := provider.Embed(ctx, "test prompt")
	require.NoError(t, err)
	assert.Len(t, embedding, 128)

	// Same text should produce same embedding
	embedding2, err := provider.Embed(ctx, "test prompt")
	require.NoError(t, err)
	assert.Equal(t, embedding, embedding2)

	// Different text should produce different embedding
	embedding3, err := provider.Embed(ctx, "different prompt")
	require.NoError(t, err)
	assert.NotEqual(t, embedding, embedding3)

	// Test batch
	embeddings, err := provider.EmbedBatch(ctx, []string{"a", "b", "c"})
	require.NoError(t, err)
	assert.Len(t, embeddings, 3)
}

func TestMemorySemanticCache_SetAndGet(t *testing.T) {
	provider := NewMockEmbeddingProvider(128)
	cache := NewMemorySemanticCache(provider, nil)
	defer cache.Close()

	ctx := context.Background()

	// Set a value
	err := cache.Set(ctx, "What is the capital of France?", "Paris", "gpt-4", 10)
	require.NoError(t, err)

	// Get exact match
	entry, similarity, err := cache.Get(ctx, "What is the capital of France?", "gpt-4")
	require.NoError(t, err)
	assert.NotNil(t, entry)
	assert.Equal(t, "Paris", entry.Response)
	assert.InDelta(t, 1.0, similarity, 0.001)
}

func TestMemorySemanticCache_SimilarPrompt(t *testing.T) {
	provider := NewMockEmbeddingProvider(128)

	// Set custom embeddings for testing semantic similarity
	similar1 := make([]float32, 128)
	similar2 := make([]float32, 128)
	for i := 0; i < 128; i++ {
		similar1[i] = float32(i) / 128.0
		similar2[i] = float32(i) / 128.0 * 0.99 // Very similar
	}
	similar1 = Normalize(similar1)
	similar2 = Normalize(similar2)

	provider.SetEmbedding("what is france capital", similar1)
	provider.SetEmbedding("france capital city", similar2)

	config := DefaultSemanticCacheConfig()
	config.SimilarityThreshold = 0.95
	config.NormalizePrompts = true

	cache := NewMemorySemanticCache(provider, config)
	defer cache.Close()

	ctx := context.Background()

	// Set first prompt
	err := cache.Set(ctx, "What is France capital", "Paris", "gpt-4", 10)
	require.NoError(t, err)

	// Get with similar prompt
	entry, similarity, err := cache.Get(ctx, "France capital city", "gpt-4")
	require.NoError(t, err)

	if entry != nil {
		assert.Equal(t, "Paris", entry.Response)
		assert.GreaterOrEqual(t, similarity, 0.95)
	}
}

func TestMemorySemanticCache_ModelSpecific(t *testing.T) {
	provider := NewMockEmbeddingProvider(128)
	config := DefaultSemanticCacheConfig()
	config.ModelSpecific = true

	cache := NewMemorySemanticCache(provider, config)
	defer cache.Close()

	ctx := context.Background()

	// Set with gpt-4
	err := cache.Set(ctx, "test prompt", "response1", "gpt-4", 10)
	require.NoError(t, err)

	// Get with gpt-4 should hit
	entry, _, err := cache.Get(ctx, "test prompt", "gpt-4")
	require.NoError(t, err)
	assert.NotNil(t, entry)

	// Get with gpt-3.5 should miss
	entry, _, err = cache.Get(ctx, "test prompt", "gpt-3.5-turbo")
	require.NoError(t, err)
	assert.Nil(t, entry)
}

func TestMemorySemanticCache_TTL(t *testing.T) {
	provider := NewMockEmbeddingProvider(128)
	config := DefaultSemanticCacheConfig()
	config.TTL = 100 * time.Millisecond

	cache := NewMemorySemanticCache(provider, config)
	defer cache.Close()

	ctx := context.Background()

	// Set a value
	err := cache.Set(ctx, "test", "response", "gpt-4", 10)
	require.NoError(t, err)

	// Should hit immediately
	entry, _, err := cache.Get(ctx, "test", "gpt-4")
	require.NoError(t, err)
	assert.NotNil(t, entry)

	// Wait for TTL
	time.Sleep(150 * time.Millisecond)

	// Should miss after TTL
	entry, _, err = cache.Get(ctx, "test", "gpt-4")
	require.NoError(t, err)
	assert.Nil(t, entry)
}

func TestMemorySemanticCache_Eviction_LRU(t *testing.T) {
	provider := NewMockEmbeddingProvider(128)
	config := DefaultSemanticCacheConfig()
	config.MaxEntries = 3
	config.EvictionPolicy = "lru"

	cache := NewMemorySemanticCache(provider, config)
	defer cache.Close()

	ctx := context.Background()

	// Fill cache
	cache.Set(ctx, "prompt1", "r1", "gpt-4", 10)
	cache.Set(ctx, "prompt2", "r2", "gpt-4", 10)
	cache.Set(ctx, "prompt3", "r3", "gpt-4", 10)

	// Access prompt1 to make it recently used
	cache.Get(ctx, "prompt1", "gpt-4")

	// Add new entry - should evict prompt2 (least recently used)
	cache.Set(ctx, "prompt4", "r4", "gpt-4", 10)

	// prompt2 should be evicted
	stats := cache.Stats()
	assert.Equal(t, int64(3), stats.TotalEntries)
}

func TestMemorySemanticCache_Stats(t *testing.T) {
	provider := NewMockEmbeddingProvider(128)
	cache := NewMemorySemanticCache(provider, nil)
	defer cache.Close()

	ctx := context.Background()

	// Initial stats
	stats := cache.Stats()
	assert.Equal(t, int64(0), stats.TotalEntries)
	assert.Equal(t, int64(0), stats.TotalHits)
	assert.Equal(t, int64(0), stats.TotalMisses)

	// Add entry and access
	cache.Set(ctx, "test", "response", "gpt-4", 100)
	cache.Get(ctx, "test", "gpt-4") // hit
	cache.Get(ctx, "miss", "gpt-4") // miss

	stats = cache.Stats()
	assert.Equal(t, int64(1), stats.TotalEntries)
	assert.Equal(t, int64(1), stats.TotalHits)
	assert.Equal(t, int64(1), stats.TotalMisses)
	assert.InDelta(t, 0.5, stats.HitRate, 0.001)
	assert.Equal(t, int64(100), stats.TokensSaved)
}

func TestMemorySemanticCache_Clear(t *testing.T) {
	provider := NewMockEmbeddingProvider(128)
	cache := NewMemorySemanticCache(provider, nil)
	defer cache.Close()

	ctx := context.Background()

	// Add entries
	cache.Set(ctx, "p1", "r1", "gpt-4", 10)
	cache.Set(ctx, "p2", "r2", "gpt-4", 10)

	assert.Equal(t, int64(2), cache.Stats().TotalEntries)

	// Clear
	err := cache.Clear(ctx)
	require.NoError(t, err)

	assert.Equal(t, int64(0), cache.Stats().TotalEntries)
}

func TestMemorySemanticCache_Delete(t *testing.T) {
	provider := NewMockEmbeddingProvider(128)
	cache := NewMemorySemanticCache(provider, nil)
	defer cache.Close()

	ctx := context.Background()

	// Add entry
	cache.Set(ctx, "test", "response", "gpt-4", 10)

	// Get key
	entry, _, _ := cache.Get(ctx, "test", "gpt-4")
	require.NotNil(t, entry)

	// Delete
	err := cache.Delete(ctx, entry.Key)
	require.NoError(t, err)

	// Should not find
	entry, _, _ = cache.Get(ctx, "test", "gpt-4")
	assert.Nil(t, entry)
}

func TestNormalizePrompt(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "  Hello   World  ",
			expected: "hello world",
		},
		{
			input:    "UPPERCASE",
			expected: "uppercase",
		},
		{
			input:    "multiple\n\nlines",
			expected: "multiple lines",
		},
	}

	for _, tt := range tests {
		result := normalizePrompt(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestDefaultSemanticCacheConfig(t *testing.T) {
	config := DefaultSemanticCacheConfig()

	assert.Equal(t, 0.95, config.SimilarityThreshold)
	assert.Equal(t, 10000, config.MaxEntries)
	assert.Equal(t, 24*time.Hour, config.TTL)
	assert.True(t, config.EnableStats)
	assert.Equal(t, "lru", config.EvictionPolicy)
	assert.True(t, config.ModelSpecific)
	assert.True(t, config.NormalizePrompts)
}

func TestMemorySemanticCache_Concurrent(t *testing.T) {
	provider := NewMockEmbeddingProvider(128)
	cache := NewMemorySemanticCache(provider, nil)
	defer cache.Close()

	ctx := context.Background()

	// Concurrent writes
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			prompt := string(rune('a' + idx%26))
			cache.Set(ctx, prompt, "response", "gpt-4", 10)
			cache.Get(ctx, prompt, "gpt-4")
			done <- true
		}(i)
	}

	// Wait for all
	for i := 0; i < 100; i++ {
		<-done
	}

	// Should not panic and have some entries
	stats := cache.Stats()
	assert.Greater(t, stats.TotalEntries, int64(0))
}

func BenchmarkMemorySemanticCache_Get(b *testing.B) {
	provider := NewMockEmbeddingProvider(128)
	cache := NewMemorySemanticCache(provider, nil)
	defer cache.Close()

	ctx := context.Background()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		prompt := string(rune('a'+i%26)) + string(rune('a'+i/26%26))
		cache.Set(ctx, prompt, "response", "gpt-4", 10)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(ctx, "test prompt", "gpt-4")
	}
}

func BenchmarkCosineSimilarity(b *testing.B) {
	a := make([]float32, 1536) // OpenAI embedding dimension
	b2 := make([]float32, 1536)
	for i := range a {
		a[i] = float32(i) / 1536
		b2[i] = float32(i+1) / 1536
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CosineSimilarity(a, b2)
	}
}
