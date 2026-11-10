package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

func TestSimpleCache_GetSet(t *testing.T) {
	cache := NewSimpleCache(1 * time.Minute)
	defer cache.Close()

	ctx := context.Background()

	// Set
	err := cache.Set(ctx, "key1", "value1", 0)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get
	val, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val.(string) != "value1" {
		t.Errorf("Expected value1, got %v", val)
	}
}

func TestSimpleCache_Miss(t *testing.T) {
	cache := NewSimpleCache(1 * time.Minute)
	defer cache.Close()

	ctx := context.Background()

	_, err := cache.Get(ctx, "nonexistent")
	if err != agentErrors.ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss, got %v", err)
	}
}

func TestSimpleCache_TTL(t *testing.T) {
	cache := NewSimpleCache(100 * time.Millisecond)
	defer cache.Close()

	ctx := context.Background()

	// Set with custom TTL
	cache.Set(ctx, "key1", "value1", 50*time.Millisecond)

	// Should exist
	val, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val.(string) != "value1" {
		t.Errorf("Expected value1, got %v", val)
	}

	// Wait for expiration
	time.Sleep(60 * time.Millisecond)

	// Should be expired
	_, err = cache.Get(ctx, "key1")
	if err != agentErrors.ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss after expiration, got %v", err)
	}
}

func TestSimpleCache_Delete(t *testing.T) {
	cache := NewSimpleCache(1 * time.Minute)
	defer cache.Close()

	ctx := context.Background()

	cache.Set(ctx, "key1", "value1", 0)
	cache.Delete(ctx, "key1")

	_, err := cache.Get(ctx, "key1")
	if err != agentErrors.ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss after delete, got %v", err)
	}
}

func TestSimpleCache_Clear(t *testing.T) {
	cache := NewSimpleCache(1 * time.Minute)
	defer cache.Close()

	ctx := context.Background()

	cache.Set(ctx, "key1", "value1", 0)
	cache.Set(ctx, "key2", "value2", 0)
	cache.Clear(ctx)

	_, err := cache.Get(ctx, "key1")
	if err != agentErrors.ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss after clear, got %v", err)
	}

	_, err = cache.Get(ctx, "key2")
	if err != agentErrors.ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss after clear, got %v", err)
	}
}

func TestSimpleCache_Has(t *testing.T) {
	cache := NewSimpleCache(1 * time.Minute)
	defer cache.Close()

	ctx := context.Background()

	cache.Set(ctx, "key1", "value1", 0)

	has, err := cache.Has(ctx, "key1")
	if err != nil {
		t.Fatalf("Has failed: %v", err)
	}
	if !has {
		t.Error("Expected key1 to exist")
	}

	has, err = cache.Has(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Has failed: %v", err)
	}
	if has {
		t.Error("Expected nonexistent to not exist")
	}
}

func TestSimpleCache_Stats(t *testing.T) {
	cache := NewSimpleCache(1 * time.Minute)
	defer cache.Close()

	ctx := context.Background()

	cache.Set(ctx, "key1", "value1", 0)
	cache.Get(ctx, "key1")        // hit
	cache.Get(ctx, "nonexistent") // miss

	stats := cache.GetStats()
	if stats.Hits != 1 {
		t.Errorf("Expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses)
	}
	if stats.HitRate != 0.5 {
		t.Errorf("Expected 0.5 hit rate, got %f", stats.HitRate)
	}
}

func TestSimpleCache_Concurrent(t *testing.T) {
	cache := NewSimpleCache(1 * time.Minute)
	defer cache.Close()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cache.Set(ctx, "key", n, 0)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Get(ctx, "key")
		}()
	}

	wg.Wait()
}

func TestSimpleCache_Cleanup(t *testing.T) {
	cache := NewSimpleCache(50 * time.Millisecond)
	defer cache.Close()

	ctx := context.Background()

	// Add entries with short TTL
	for i := 0; i < 10; i++ {
		cache.Set(ctx, "key", i, 30*time.Millisecond)
	}

	// Wait for cleanup to run
	time.Sleep(100 * time.Millisecond)

	// All should be cleaned up
	_, err := cache.Get(ctx, "key")
	if err != agentErrors.ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss after cleanup, got %v", err)
	}
}

// 设置上限后条目数不超过上限：先回收过期条目，仍超出时淘汰其他条目，刚写入的保留。
func TestSimpleCacheMaxEntries(t *testing.T) {
	ctx := context.Background()
	c := NewSimpleCache(time.Minute, WithoutBackgroundCleanup(), WithMaxEntries(3))
	for i := range 10 {
		key := string(rune('a' + i))
		if err := c.Set(ctx, key, i, 0); err != nil {
			t.Fatal(err)
		}
		if size := c.GetStats().Size; size > 3 {
			t.Fatalf("写入 %s 后有 %d 个条目", key, size)
		}
		if v, err := c.Get(ctx, key); err != nil || v != i {
			t.Fatalf("刚写入的 %s 不应被淘汰: %v %v", key, v, err)
		}
	}
	if err := c.Set(ctx, "j", 99, 0); err != nil || c.GetStats().Size != 3 {
		t.Fatalf("覆盖已有键不应增加条目: size=%d", c.GetStats().Size)
	}
	if err := c.Delete(ctx, "j"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "k", 1, 0); err != nil || c.GetStats().Size != 3 {
		t.Fatalf("删除后计数应同步: size=%d", c.GetStats().Size)
	}
}
