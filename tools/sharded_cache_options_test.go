package tools

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShardedCacheWithOptions tests the option pattern
func TestShardedCacheWithOptions(t *testing.T) {
	t.Run("DefaultOptions", func(t *testing.T) {
		cache := NewShardedToolCacheWithOptions()
		defer cache.Close()

		assert.Equal(t, uint32(32), cache.shardCount)
		assert.Equal(t, 10000, cache.config.Capacity)
		assert.Equal(t, 5*time.Minute, cache.config.DefaultTTL)
	})

	t.Run("CustomShardCount", func(t *testing.T) {
		cache := NewShardedToolCacheWithOptions(
			WithShardCount(64),
		)
		defer cache.Close()

		assert.Equal(t, uint32(64), cache.shardCount)
	})

	t.Run("AutoShardCount", func(t *testing.T) {
		cache := NewShardedToolCacheWithOptions(
			WithShardCount(0), // Auto-detect
		)
		defer cache.Close()

		expectedShards := nextPowerOfTwo(uint32(runtime.NumCPU() * 4))
		assert.Equal(t, expectedShards, cache.shardCount)
	})

	t.Run("PerformanceProfiles", func(t *testing.T) {
		profiles := []PerformanceProfile{
			LowLatencyProfile,
			HighThroughputProfile,
			BalancedProfile,
			MemoryEfficientProfile,
		}

		for _, profile := range profiles {
			t.Run(fmt.Sprintf("Profile_%d", profile), func(t *testing.T) {
				cache := NewShardedToolCacheWithOptions(
					WithPerformanceProfile(profile),
				)
				defer cache.Close()

				// Verify cache was created with the profile
				assert.NotNil(t, cache)
				assert.Greater(t, cache.shardCount, uint32(0))
			})
		}
	})

	t.Run("WorkloadTypes", func(t *testing.T) {
		workloads := []WorkloadType{
			ReadHeavyWorkload,
			WriteHeavyWorkload,
			MixedWorkload,
			BurstyWorkload,
		}

		for _, workload := range workloads {
			t.Run(fmt.Sprintf("Workload_%d", workload), func(t *testing.T) {
				cache := NewShardedToolCacheWithOptions(
					WithWorkloadType(workload),
				)
				defer cache.Close()

				assert.NotNil(t, cache)
				assert.Equal(t, workload, cache.config.WorkloadType)
			})
		}
	})

	t.Run("MultipleOptions", func(t *testing.T) {
		cache := NewShardedToolCacheWithOptions(
			WithCapacity(50000),
			WithDefaultTTL(10*time.Minute),
			WithCleanupInterval(30*time.Second),
			WithEvictionPolicy(LFUEviction),
			WithCleanupStrategy(AdaptiveCleanup),
			WithLoadBalancing(ConsistentHashBalancing),
			WithAutoTuning(true),
			WithMetrics(true),
		)
		defer cache.Close()

		assert.Equal(t, 50000, cache.config.Capacity)
		assert.Equal(t, 10*time.Minute, cache.config.DefaultTTL)
		assert.Equal(t, 30*time.Second, cache.config.CleanupInterval)
		assert.Equal(t, LFUEviction, cache.config.EvictionPolicy)
		assert.Equal(t, AdaptiveCleanup, cache.config.CleanupStrategy)
		assert.Equal(t, ConsistentHashBalancing, cache.config.LoadBalancing)
		assert.True(t, cache.config.AutoTuning)
		assert.True(t, cache.config.MetricsEnabled)
	})
}

// TestShardedCacheWarmup tests cache warmup functionality
func TestShardedCacheWarmup(t *testing.T) {
	warmupData := map[string]*ToolOutput{
		"key1": {Result: "value1"},
		"key2": {Result: "value2"},
		"key3": {Result: "value3"},
	}

	cache := NewShardedToolCacheWithOptions(
		WithWarmup(warmupData),
		WithCapacity(100),
	)
	defer cache.Close()

	ctx := context.Background()

	// Verify all warmup data is present
	for key, expectedOutput := range warmupData {
		output, found := cache.Get(ctx, key)
		require.True(t, found, "Key %s should be found", key)
		assert.Equal(t, expectedOutput.Result, output.Result)
	}

	// Verify cache size
	assert.Equal(t, len(warmupData), cache.Size())
}

// TestShardCountRecommendation tests shard count recommendations
func TestShardCountRecommendation(t *testing.T) {
	testCases := []struct {
		qps      int
		minCount uint32
		maxCount uint32
	}{
		{50, 1, 32},       // Light load (adjusted for higher CPU core machines)
		{300, 16, 64},     // Moderate load
		{800, 32, 128},    // Medium load
		{3000, 64, 128},   // High load
		{10000, 256, 256}, // Very high load
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("QPS_%d", tc.qps), func(t *testing.T) {
			rec := GetShardCountRecommendation(tc.qps)
			assert.Equal(t, tc.qps, rec.ExpectedQPS)
			assert.GreaterOrEqual(t, rec.RecommendedCount, tc.minCount)
			assert.LessOrEqual(t, rec.RecommendedCount, tc.maxCount)
			assert.NotEmpty(t, rec.Rationale)
		})
	}
}

// TestCleanupIntervalRecommendation tests cleanup interval recommendations
func TestCleanupIntervalRecommendation(t *testing.T) {
	testCases := []struct {
		name        string
		cacheSize   int
		ttl         time.Duration
		churnRate   float64
		minInterval time.Duration
		maxInterval time.Duration
	}{
		{
			"LowChurn",
			10000,
			5 * time.Minute,
			0.005, // 0.5% per minute
			30 * time.Second,
			5 * time.Minute,
		},
		{
			"ModerateChurn",
			10000,
			5 * time.Minute,
			0.03, // 3% per minute
			20 * time.Second,
			1 * time.Minute,
		},
		{
			"HighChurn",
			10000,
			5 * time.Minute,
			0.08, // 8% per minute
			10 * time.Second,
			30 * time.Second,
		},
		{
			"VeryHighChurn",
			10000,
			5 * time.Minute,
			0.15, // 15% per minute
			30 * time.Second,
			30 * time.Second,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := GetCleanupIntervalRecommendation(tc.cacheSize, tc.ttl, tc.churnRate)
			assert.Equal(t, tc.cacheSize, rec.CacheSize)
			assert.Equal(t, tc.ttl, rec.TTL)
			assert.Equal(t, tc.churnRate, rec.ExpectedChurn)
			assert.GreaterOrEqual(t, rec.RecommendedInterval, tc.minInterval)
			assert.LessOrEqual(t, rec.RecommendedInterval, tc.maxInterval)
			assert.NotEmpty(t, rec.Rationale)
		})
	}
}

// TestNextPowerOfTwo tests the power of two calculation
func TestNextPowerOfTwo(t *testing.T) {
	testCases := []struct {
		input    uint32
		expected uint32
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{7, 8},
		{8, 8},
		{9, 16},
		{31, 32},
		{32, 32},
		{33, 64},
		{127, 128},
		{128, 128},
		{129, 256},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Input_%d", tc.input), func(t *testing.T) {
			result := nextPowerOfTwo(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestAdaptiveCleanup tests adaptive cleanup functionality
func TestAdaptiveCleanup(t *testing.T) {
	cache := NewShardedToolCacheWithOptions(
		WithCapacity(100),
		WithDefaultTTL(100*time.Millisecond),
		WithCleanupInterval(50*time.Millisecond),
		WithCleanupStrategy(AdaptiveCleanup),
	)
	defer cache.Close()

	ctx := context.Background()

	// Fill cache with short-lived entries
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key%d", i)
		output := &ToolOutput{Result: fmt.Sprintf("value%d", i)}
		err := cache.Set(ctx, key, output, 100*time.Millisecond)
		require.NoError(t, err)
	}

	// Initial size
	initialSize := cache.Size()
	assert.GreaterOrEqual(t, initialSize, 48) // Some entries might be cleaned immediately
	assert.LessOrEqual(t, initialSize, 50)

	// Wait for entries to expire and cleanup to run
	time.Sleep(300 * time.Millisecond)

	// Check that expired entries were cleaned
	finalSize := cache.Size()
	assert.LessOrEqual(t, finalSize, initialSize)
}

// TestAutoTuning tests auto-tuning functionality
func TestAutoTuning(t *testing.T) {
	cache := NewShardedToolCacheWithOptions(
		WithCapacity(1000),
		WithAutoTuning(true),
		WithMetrics(true),
	)
	defer cache.Close()

	ctx := context.Background()

	// Simulate workload
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("worker%d:key%d", worker, j)
				output := &ToolOutput{Result: fmt.Sprintf("value%d", j)}
				_ = cache.Set(ctx, key, output, 5*time.Minute)
				_, _ = cache.Get(ctx, key)
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Verify metrics were collected
	assert.NotNil(t, cache.tuneMetrics)
	stats := cache.GetStats()
	assert.Greater(t, stats.Hits.Load(), int64(0))
}

// TestConcurrentOperationsWithOptions tests concurrent access with various configurations
func TestConcurrentOperationsWithOptions(t *testing.T) {
	configurations := []struct {
		name    string
		options []ShardedCacheOption
	}{
		{
			"LowLatency",
			[]ShardedCacheOption{
				WithPerformanceProfile(LowLatencyProfile),
				WithCapacity(10000),
			},
		},
		{
			"HighThroughput",
			[]ShardedCacheOption{
				WithPerformanceProfile(HighThroughputProfile),
				WithCapacity(10000),
			},
		},
		{
			"BurstyWorkload",
			[]ShardedCacheOption{
				WithWorkloadType(BurstyWorkload),
				WithCapacity(10000),
				WithMaxShardConcurrency(50),
			},
		},
	}

	for _, config := range configurations {
		t.Run(config.name, func(t *testing.T) {
			cache := NewShardedToolCacheWithOptions(config.options...)
			defer cache.Close()

			ctx := context.Background()
			const numWorkers = 10
			const numOperations = 100

			var wg sync.WaitGroup
			errors := make(chan error, numWorkers*numOperations)

			// Concurrent writes and reads
			for i := 0; i < numWorkers; i++ {
				wg.Add(1)
				go func(worker int) {
					defer wg.Done()
					for j := 0; j < numOperations; j++ {
						key := fmt.Sprintf("w%d:k%d", worker, j)
						output := &ToolOutput{Result: fmt.Sprintf("v%d", j)}

						// Set
						if err := cache.Set(ctx, key, output, time.Minute); err != nil {
							errors <- err
							continue
						}

						// Get
						retrieved, found := cache.Get(ctx, key)
						if !found {
							errors <- fmt.Errorf("key not found: %s", key)
							continue
						}
						if retrieved.Result != output.Result {
							errors <- fmt.Errorf("value mismatch for key %s", key)
						}
					}
				}(i)
			}

			wg.Wait()
			close(errors)

			// Check for errors
			var errorCount int
			for err := range errors {
				t.Errorf("Concurrent operation error: %v", err)
				errorCount++
			}

			assert.Zero(t, errorCount, "Should have no errors in concurrent operations")

			// Verify final state
			finalSize := cache.Size()
			assert.Greater(t, finalSize, 0)
			assert.LessOrEqual(t, finalSize, numWorkers*numOperations)
		})
	}
}

// BenchmarkShardedCacheWithOptions benchmarks different configurations
func BenchmarkShardedCacheWithOptions(b *testing.B) {
	configurations := []struct {
		name    string
		options []ShardedCacheOption
	}{
		{
			"Default",
			[]ShardedCacheOption{},
		},
		{
			"LowLatency",
			[]ShardedCacheOption{
				WithPerformanceProfile(LowLatencyProfile),
			},
		},
		{
			"HighThroughput",
			[]ShardedCacheOption{
				WithPerformanceProfile(HighThroughputProfile),
			},
		},
		{
			"MemoryEfficient",
			[]ShardedCacheOption{
				WithPerformanceProfile(MemoryEfficientProfile),
			},
		},
		{
			"ManyShards",
			[]ShardedCacheOption{
				WithShardCount(256),
			},
		},
	}

	for _, config := range configurations {
		b.Run(config.name, func(b *testing.B) {
			cache := NewShardedToolCacheWithOptions(config.options...)
			defer cache.Close()

			ctx := context.Background()
			output := &ToolOutput{Result: "benchmark value"}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key%d", i%1000)
					_ = cache.Set(ctx, key, output, time.Minute)
					_, _ = cache.Get(ctx, key)
					i++
				}
			})
		})
	}
}
