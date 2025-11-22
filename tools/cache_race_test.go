package tools

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestCacheRaceCondition performs a stress test to verify there are no race conditions
// in the cache cleanup mechanism under heavy concurrent load.
func TestCacheRaceCondition(t *testing.T) {
	config := MemoryCacheConfig{
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Millisecond, // Very aggressive cleanup
	}

	cache := NewMemoryToolCache(config)
	defer cache.Close()

	ctx := context.Background()

	// Run concurrent operations while cleanup is happening
	var wg sync.WaitGroup
	concurrency := 10
	operationsPerGoroutine := 100

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				// Mix of operations
				switch j % 5 {
				case 0: // Set
					key := "key_" + string(rune('A'+(id%26)))
					_ = cache.Set(ctx, key, &ToolOutput{Result: "data", Success: true}, 50*time.Millisecond)
				case 1: // Get
					key := "key_" + string(rune('A'+(id%26)))
					cache.Get(ctx, key)
				case 2: // Delete
					key := "key_" + string(rune('A'+(id%26)))
					_ = cache.Delete(ctx, key)
				case 3: // Size
					_ = cache.Size()
				case 4: // GetStats
					_ = cache.GetStats()
				}
			}
		}(i)
	}

	// Let cleanup goroutine run during concurrent operations
	time.Sleep(100 * time.Millisecond)

	wg.Wait()

	// Verify cache is still functional after stress test
	testKey := "final_test"
	_ = cache.Set(ctx, testKey, &ToolOutput{Result: "final", Success: true}, 1*time.Minute)
	if output, found := cache.Get(ctx, testKey); !found || output.Result != "final" {
		t.Error("Cache should still be functional after stress test")
	}
}

// TestCleanupDoesNotBlockOperations verifies that cleanup operations
// don't block cache operations for extended periods.
func TestCleanupDoesNotBlockOperations(t *testing.T) {
	config := MemoryCacheConfig{
		Capacity:        1000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 50 * time.Millisecond,
	}

	cache := NewMemoryToolCache(config)
	defer cache.Close()

	ctx := context.Background()

	// Fill cache with items that will expire at different times
	for i := 0; i < 500; i++ {
		key := "key_" + string(rune('A'+(i%26))) + string(rune('0'+(i%10)))
		ttl := time.Duration(i%100) * time.Millisecond
		_ = cache.Set(ctx, key, &ToolOutput{Result: "data", Success: true}, ttl)
	}

	// Monitor operation latency during cleanup
	start := time.Now()
	iterations := 100
	maxLatency := time.Duration(0)

	for i := 0; i < iterations; i++ {
		opStart := time.Now()
		cache.Get(ctx, "key_A0")
		opLatency := time.Since(opStart)

		if opLatency > maxLatency {
			maxLatency = opLatency
		}

		time.Sleep(1 * time.Millisecond)
	}

	totalTime := time.Since(start)
	avgLatency := totalTime / time.Duration(iterations)

	t.Logf("Average Get latency: %v", avgLatency)
	t.Logf("Max Get latency: %v", maxLatency)

	// Verify operations remain fast even during cleanup
	// Using 10ms threshold as a reasonable upper bound
	if maxLatency > 10*time.Millisecond {
		t.Errorf("Operations blocked too long during cleanup: max latency %v", maxLatency)
	}
}

// TestGracefulShutdown verifies that Close() properly waits for cleanup goroutine
func TestGracefulShutdown(t *testing.T) {
	config := MemoryCacheConfig{
		Capacity:        10,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Millisecond,
	}

	cache := NewMemoryToolCache(config)

	// Let cleanup goroutine run a few iterations
	time.Sleep(50 * time.Millisecond)

	// Close should block until cleanup goroutine exits
	closeStart := time.Now()
	cache.Close()
	closeDuration := time.Since(closeStart)

	t.Logf("Close() took %v", closeDuration)

	// Verify Close() completed in reasonable time
	// Should be nearly instant since we signal via context
	if closeDuration > 100*time.Millisecond {
		t.Errorf("Close() took too long: %v", closeDuration)
	}
}
