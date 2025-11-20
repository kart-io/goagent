package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryCache_GetSet(t *testing.T) {
	cache := NewInMemoryCache(100, 5*time.Minute, 0)
	defer cache.Close()

	ctx := context.Background()

	// Test Set and Get
	err := cache.Set(ctx, "key1", "value1", 0)
	require.NoError(t, err)

	value, err := cache.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)
}

func TestInMemoryCache_Miss(t *testing.T) {
	cache := NewInMemoryCache(100, 5*time.Minute, 0)
	defer cache.Close()

	ctx := context.Background()

	// Test cache miss
	value, err := cache.Get(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Equal(t, ErrCacheMiss, err)
	assert.Nil(t, value)
}

func TestInMemoryCache_Expiration(t *testing.T) {
	cache := NewInMemoryCache(100, 0, 0)
	defer cache.Close()

	ctx := context.Background()

	// Set with short TTL
	err := cache.Set(ctx, "key1", "value1", 10*time.Millisecond)
	require.NoError(t, err)

	// Should exist immediately
	value, err := cache.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	// Wait for expiration
	time.Sleep(15 * time.Millisecond)

	// Should be expired
	value, err = cache.Get(ctx, "key1")
	assert.Error(t, err)
	assert.Equal(t, ErrCacheMiss, err)
	assert.Nil(t, value)
}

func TestInMemoryCache_Delete(t *testing.T) {
	cache := NewInMemoryCache(100, 5*time.Minute, 0)
	defer cache.Close()

	ctx := context.Background()

	// Set and delete
	err := cache.Set(ctx, "key1", "value1", 0)
	require.NoError(t, err)

	err = cache.Delete(ctx, "key1")
	require.NoError(t, err)

	// Should not exist
	value, err := cache.Get(ctx, "key1")
	assert.Error(t, err)
	assert.Nil(t, value)
}

func TestInMemoryCache_Clear(t *testing.T) {
	cache := NewInMemoryCache(100, 5*time.Minute, 0)
	defer cache.Close()

	ctx := context.Background()

	// Set multiple values
	cache.Set(ctx, "key1", "value1", 0)
	cache.Set(ctx, "key2", "value2", 0)
	cache.Set(ctx, "key3", "value3", 0)

	// Clear all
	err := cache.Clear(ctx)
	require.NoError(t, err)

	// All should be gone
	_, err = cache.Get(ctx, "key1")
	assert.Error(t, err)
	_, err = cache.Get(ctx, "key2")
	assert.Error(t, err)
	_, err = cache.Get(ctx, "key3")
	assert.Error(t, err)

	stats := cache.GetStats()
	assert.Equal(t, int64(0), stats.Size)
}

func TestInMemoryCache_Has(t *testing.T) {
	cache := NewInMemoryCache(100, 5*time.Minute, 0)
	defer cache.Close()

	ctx := context.Background()

	// Check non-existent
	has, err := cache.Has(ctx, "key1")
	require.NoError(t, err)
	assert.False(t, has)

	// Set and check
	cache.Set(ctx, "key1", "value1", 0)
	has, err = cache.Has(ctx, "key1")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestInMemoryCache_Stats(t *testing.T) {
	cache := NewInMemoryCache(100, 5*time.Minute, 0)
	defer cache.Close()

	ctx := context.Background()

	// Perform operations
	cache.Set(ctx, "key1", "value1", 0)
	cache.Set(ctx, "key2", "value2", 0)

	cache.Get(ctx, "key1")      // hit
	cache.Get(ctx, "nonexist")  // miss
	cache.Get(ctx, "key2")      // hit
	cache.Get(ctx, "nonexist2") // miss

	cache.Delete(ctx, "key1")

	stats := cache.GetStats()
	assert.Equal(t, int64(2), stats.Hits)
	assert.Equal(t, int64(2), stats.Misses)
	assert.Equal(t, int64(2), stats.Sets)
	assert.Equal(t, int64(1), stats.Deletes)
	assert.Equal(t, 0.5, stats.HitRate) // 2/(2+2)
	assert.Equal(t, int64(1), stats.Size)
}

func TestInMemoryCache_MaxSize(t *testing.T) {
	cache := NewInMemoryCache(3, 5*time.Minute, 0)
	defer cache.Close()

	ctx := context.Background()

	// Add items up to max size
	cache.Set(ctx, "key1", "value1", 0)
	cache.Set(ctx, "key2", "value2", 0)
	cache.Set(ctx, "key3", "value3", 0)

	// Adding one more should evict oldest
	cache.Set(ctx, "key4", "value4", 0)

	stats := cache.GetStats()
	assert.Equal(t, int64(3), stats.Size)
	assert.Equal(t, int64(1), stats.Evictions)

	// key1 should be evicted
	_, err := cache.Get(ctx, "key1")
	assert.Error(t, err)

	// Others should exist
	_, err = cache.Get(ctx, "key4")
	assert.NoError(t, err)
}

func TestInMemoryCache_AutoCleanup(t *testing.T) {
	cache := NewInMemoryCache(100, 0, 50*time.Millisecond)
	defer cache.Close()

	ctx := context.Background()

	// Set items with short TTL
	cache.Set(ctx, "key1", "value1", 30*time.Millisecond)
	cache.Set(ctx, "key2", "value2", 30*time.Millisecond)

	// Wait for cleanup to run
	time.Sleep(100 * time.Millisecond)

	// Items should be cleaned up
	stats := cache.GetStats()
	assert.Greater(t, stats.Evictions, int64(0))
}

func TestCacheEntry_IsExpired(t *testing.T) {
	tests := []struct {
		name         string
		entry        *CacheEntry
		shouldExpire bool
	}{
		{
			name: "not expired",
			entry: &CacheEntry{
				ExpireTime: time.Now().Add(1 * time.Hour),
			},
			shouldExpire: false,
		},
		{
			name: "expired",
			entry: &CacheEntry{
				ExpireTime: time.Now().Add(-1 * time.Hour),
			},
			shouldExpire: true,
		},
		{
			name: "no expiration",
			entry: &CacheEntry{
				ExpireTime: time.Time{},
			},
			shouldExpire: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.shouldExpire, tt.entry.IsExpired())
		})
	}
}

func TestLRUCache_Creation(t *testing.T) {
	cache := NewLRUCache(100, 5*time.Minute, 1*time.Minute)
	require.NotNil(t, cache)
	defer cache.Close()

	ctx := context.Background()

	// Test basic operations work
	cache.Set(ctx, "key1", "value1", 0)
	value, err := cache.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)
}

func TestMultiTierCache_Get(t *testing.T) {
	tier1 := NewInMemoryCache(10, 5*time.Minute, 0)
	tier2 := NewInMemoryCache(100, 5*time.Minute, 0)
	defer tier1.Close()
	defer tier2.Close()

	multiCache := NewMultiTierCache(tier1, tier2)

	ctx := context.Background()

	// Set in tier2 only
	tier2.Set(ctx, "key1", "value1", 0)

	// Get should find in tier2 and backfill to tier1
	value, err := multiCache.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	// Verify backfill
	value, err = tier1.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)
}

func TestMultiTierCache_Set(t *testing.T) {
	tier1 := NewInMemoryCache(10, 5*time.Minute, 0)
	tier2 := NewInMemoryCache(100, 5*time.Minute, 0)
	defer tier1.Close()
	defer tier2.Close()

	multiCache := NewMultiTierCache(tier1, tier2)

	ctx := context.Background()

	// Set through multi-tier
	err := multiCache.Set(ctx, "key1", "value1", 0)
	require.NoError(t, err)

	// Should be in all tiers
	value, err := tier1.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	value, err = tier2.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)
}

func TestMultiTierCache_Delete(t *testing.T) {
	tier1 := NewInMemoryCache(10, 5*time.Minute, 0)
	tier2 := NewInMemoryCache(100, 5*time.Minute, 0)
	defer tier1.Close()
	defer tier2.Close()

	multiCache := NewMultiTierCache(tier1, tier2)

	ctx := context.Background()

	// Set in all tiers
	multiCache.Set(ctx, "key1", "value1", 0)

	// Delete
	err := multiCache.Delete(ctx, "key1")
	require.NoError(t, err)

	// Should be deleted from all tiers
	_, err = tier1.Get(ctx, "key1")
	assert.Error(t, err)
	_, err = tier2.Get(ctx, "key1")
	assert.Error(t, err)
}

func TestMultiTierCache_Clear(t *testing.T) {
	tier1 := NewInMemoryCache(10, 5*time.Minute, 0)
	tier2 := NewInMemoryCache(100, 5*time.Minute, 0)
	defer tier1.Close()
	defer tier2.Close()

	multiCache := NewMultiTierCache(tier1, tier2)

	ctx := context.Background()

	// Set multiple items
	multiCache.Set(ctx, "key1", "value1", 0)
	multiCache.Set(ctx, "key2", "value2", 0)

	// Clear
	err := multiCache.Clear(ctx)
	require.NoError(t, err)

	// Should be cleared from all tiers
	_, err = tier1.Get(ctx, "key1")
	assert.Error(t, err)
	_, err = tier2.Get(ctx, "key1")
	assert.Error(t, err)
}

func TestMultiTierCache_Has(t *testing.T) {
	tier1 := NewInMemoryCache(10, 5*time.Minute, 0)
	tier2 := NewInMemoryCache(100, 5*time.Minute, 0)
	defer tier1.Close()
	defer tier2.Close()

	multiCache := NewMultiTierCache(tier1, tier2)

	ctx := context.Background()

	// Set in tier2 only
	tier2.Set(ctx, "key1", "value1", 0)

	// Should find in tier2
	has, err := multiCache.Has(ctx, "key1")
	require.NoError(t, err)
	assert.True(t, has)

	// Non-existent key
	has, err = multiCache.Has(ctx, "nonexist")
	require.NoError(t, err)
	assert.False(t, has)
}

func TestMultiTierCache_GetStats(t *testing.T) {
	tier1 := NewInMemoryCache(10, 5*time.Minute, 0)
	tier2 := NewInMemoryCache(100, 5*time.Minute, 0)
	defer tier1.Close()
	defer tier2.Close()

	multiCache := NewMultiTierCache(tier1, tier2)

	stats := multiCache.GetStats()
	assert.Equal(t, int64(10), stats.MaxSize) // From tier1
}

func TestCacheKeyGenerator_GenerateKey(t *testing.T) {
	gen := NewCacheKeyGenerator("llm")

	key1 := gen.GenerateKey("hello", map[string]interface{}{"temp": 0.7})
	key2 := gen.GenerateKey("hello", map[string]interface{}{"temp": 0.7})
	key3 := gen.GenerateKey("hello", map[string]interface{}{"temp": 0.8})

	// Same inputs should generate same key
	assert.Equal(t, key1, key2)

	// Different inputs should generate different keys
	assert.NotEqual(t, key1, key3)

	// Should have prefix
	assert.Contains(t, key1, "llm:")
}

func TestCacheKeyGenerator_GenerateKeySimple(t *testing.T) {
	gen := NewCacheKeyGenerator("test")

	key1 := gen.GenerateKeySimple("part1", "part2", "part3")
	key2 := gen.GenerateKeySimple("part1", "part2", "part3")
	key3 := gen.GenerateKeySimple("part1", "part2", "different")

	// Same parts should generate same key
	assert.Equal(t, key1, key2)

	// Different parts should generate different keys
	assert.NotEqual(t, key1, key3)

	// Should have prefix
	assert.Contains(t, key1, "test:")
}

func TestCacheKeyGenerator_NoPrefix(t *testing.T) {
	gen := NewCacheKeyGenerator("")

	key := gen.GenerateKey("test", map[string]interface{}{})
	// Should not have colon
	assert.NotContains(t, key, ":")
}

func TestNoOpCache(t *testing.T) {
	cache := NewNoOpCache()
	ctx := context.Background()

	// All operations should return ErrCacheDisabled
	_, err := cache.Get(ctx, "key")
	assert.Equal(t, ErrCacheDisabled, err)

	err = cache.Set(ctx, "key", "value", 0)
	assert.Equal(t, ErrCacheDisabled, err)

	err = cache.Delete(ctx, "key")
	assert.Equal(t, ErrCacheDisabled, err)

	err = cache.Clear(ctx)
	assert.Equal(t, ErrCacheDisabled, err)

	has, err := cache.Has(ctx, "key")
	assert.False(t, has)
	assert.Equal(t, ErrCacheDisabled, err)

	stats := cache.GetStats()
	assert.Equal(t, CacheStats{}, stats)
}

func TestDefaultCacheConfig(t *testing.T) {
	config := DefaultCacheConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, "memory", config.Type)
	assert.Equal(t, 1000, config.MaxSize)
	assert.Equal(t, 5*time.Minute, config.DefaultTTL)
	assert.Equal(t, 1*time.Minute, config.CleanupInterval)
}

func TestNewCacheFromConfig(t *testing.T) {
	tests := []struct {
		name   string
		config CacheConfig
		check  func(*testing.T, Cache)
	}{
		{
			name: "memory cache",
			config: CacheConfig{
				Enabled:         true,
				Type:            "memory",
				MaxSize:         100,
				DefaultTTL:      5 * time.Minute,
				CleanupInterval: 1 * time.Minute,
			},
			check: func(t *testing.T, cache Cache) {
				_, ok := cache.(*InMemoryCache)
				assert.True(t, ok)
				if c, ok := cache.(*InMemoryCache); ok {
					c.Close()
				}
			},
		},
		{
			name: "LRU cache",
			config: CacheConfig{
				Enabled:         true,
				Type:            "lru",
				MaxSize:         100,
				DefaultTTL:      5 * time.Minute,
				CleanupInterval: 1 * time.Minute,
			},
			check: func(t *testing.T, cache Cache) {
				_, ok := cache.(*LRUCache)
				assert.True(t, ok)
				if c, ok := cache.(*LRUCache); ok {
					c.Close()
				}
			},
		},
		{
			name: "disabled cache",
			config: CacheConfig{
				Enabled: false,
			},
			check: func(t *testing.T, cache Cache) {
				_, ok := cache.(*NoOpCache)
				assert.True(t, ok)
			},
		},
		{
			name: "unknown type defaults to memory",
			config: CacheConfig{
				Enabled:         true,
				Type:            "unknown",
				MaxSize:         100,
				DefaultTTL:      5 * time.Minute,
				CleanupInterval: 1 * time.Minute,
			},
			check: func(t *testing.T, cache Cache) {
				_, ok := cache.(*InMemoryCache)
				assert.True(t, ok)
				if c, ok := cache.(*InMemoryCache); ok {
					c.Close()
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCacheFromConfig(tt.config)
			require.NotNil(t, cache)
			tt.check(t, cache)
		})
	}
}

func TestInMemoryCache_ConcurrentAccess(t *testing.T) {
	cache := NewInMemoryCache(1000, 5*time.Minute, 0)
	defer cache.Close()

	ctx := context.Background()
	iterations := 100

	// Concurrent writes
	done := make(chan bool, iterations)
	for i := 0; i < iterations; i++ {
		go func(n int) {
			cache.Set(ctx, time.Now().String(), n, 0)
			done <- true
		}(i)
	}

	// Wait for all writes
	for i := 0; i < iterations; i++ {
		<-done
	}

	// Cache should have entries
	stats := cache.GetStats()
	assert.Greater(t, stats.Size, int64(0))
}

func TestInMemoryCache_DefaultTTL(t *testing.T) {
	defaultTTL := 50 * time.Millisecond
	cache := NewInMemoryCache(100, defaultTTL, 0)
	defer cache.Close()

	ctx := context.Background()

	// Set with zero TTL should use default
	err := cache.Set(ctx, "key1", "value1", 0)
	require.NoError(t, err)

	// Should exist immediately
	_, err = cache.Get(ctx, "key1")
	require.NoError(t, err)

	// Wait for default TTL to expire
	time.Sleep(60 * time.Millisecond)

	// Should be expired
	_, err = cache.Get(ctx, "key1")
	assert.Error(t, err)
}

func BenchmarkInMemoryCache_Set(b *testing.B) {
	cache := NewInMemoryCache(10000, 5*time.Minute, 0)
	defer cache.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(ctx, "key", i, 0)
	}
}

func BenchmarkInMemoryCache_Get(b *testing.B) {
	cache := NewInMemoryCache(10000, 5*time.Minute, 0)
	defer cache.Close()
	ctx := context.Background()

	cache.Set(ctx, "key", "value", 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(ctx, "key")
	}
}

func BenchmarkCacheKeyGenerator_GenerateKey(b *testing.B) {
	gen := NewCacheKeyGenerator("test")
	params := map[string]interface{}{
		"temperature": 0.7,
		"max_tokens":  100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen.GenerateKey("test prompt", params)
	}
}
