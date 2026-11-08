package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

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
	assert.Equal(t, agentErrors.ErrCacheDisabled, err)

	err = cache.Set(ctx, "key", "value", 0)
	assert.Equal(t, agentErrors.ErrCacheDisabled, err)

	err = cache.Delete(ctx, "key")
	assert.Equal(t, agentErrors.ErrCacheDisabled, err)

	err = cache.Clear(ctx)
	assert.Equal(t, agentErrors.ErrCacheDisabled, err)

	has, err := cache.Has(ctx, "key")
	assert.False(t, has)
	assert.Equal(t, agentErrors.ErrCacheDisabled, err)

	stats := cache.GetStats()
	assert.Equal(t, CacheStats{}, stats)
}

func TestDefaultCacheConfig(t *testing.T) {
	config := DefaultCacheConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, 5*time.Minute, config.DefaultTTL)
}

func TestNewCacheFromConfig(t *testing.T) {
	t.Run("启用缓存", func(t *testing.T) {
		configured := NewCacheFromConfig(CacheConfig{Enabled: true, DefaultTTL: 5 * time.Minute})
		cache, ok := configured.(*SimpleCache)
		require.True(t, ok)
		t.Cleanup(cache.Close)
		ctx := context.Background()
		require.NoError(t, configured.Set(ctx, "key", "value", 0))
		value, err := configured.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", value)
	})
	t.Run("禁用缓存", func(t *testing.T) {
		configured := NewCacheFromConfig(CacheConfig{Enabled: false})
		_, ok := configured.(*NoOpCache)
		require.True(t, ok)
		require.ErrorIs(t, configured.Set(context.Background(), "key", "value", 0), agentErrors.ErrCacheDisabled)
	})
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
