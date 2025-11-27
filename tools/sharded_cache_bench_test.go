package tools

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

// BenchmarkHashStringVsFNV 对比内联哈希和 FNV 哈希的性能
func BenchmarkHashStringVsFNV(b *testing.B) {
	keys := []string{
		"tool_search_query=golang",
		"tool_calculate_input=123+456",
		"tool_translate_text=hello world",
		"tool_weather_city=Beijing",
		"tool_stock_symbol=AAPL",
	}

	b.Run("InlineHash", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := keys[i%len(keys)]
			_ = hashString(key)
		}
	})

	b.Run("FNVHash", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := keys[i%len(keys)]
			// 模拟原来的 fnv.New32a().Write([]byte(key))
			h := uint32(2166136261)
			data := []byte(key)
			for j := 0; j < len(data); j++ {
				h ^= uint32(data[j])
				h *= 16777619
			}
			_ = h
		}
	})
}

// BenchmarkLRUCustomVsContainerList 对比自定义 LRU 和 container/list 的性能
func BenchmarkLRUCustomVsContainerList(b *testing.B) {
	ctx := context.Background()

	b.Run("CustomLRU", func(b *testing.B) {
		cache := NewShardedToolCache(ShardedCacheConfig{
			ShardCount: 32,
			Capacity:   10000,
			DefaultTTL: 5 * time.Minute,
		})
		defer cache.Close()

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key_%d", i%1000)
			output := &interfaces.ToolOutput{Result: "value"}

			_ = cache.Set(ctx, key, output, 5*time.Minute)
			_, _ = cache.Get(ctx, key)
		}
	})

	b.Run("ContainerList", func(b *testing.B) {
		cache := NewMemoryToolCache(MemoryCacheConfig{
			Capacity:   10000,
			DefaultTTL: 5 * time.Minute,
		})
		defer cache.Close()

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key_%d", i%1000)
			output := &interfaces.ToolOutput{Result: "value"}

			_ = cache.Set(ctx, key, output, 5*time.Minute)
			_, _ = cache.Get(ctx, key)
		}
	})
}

// BenchmarkShardedCacheConcurrency 并发性能测试
func BenchmarkShardedCacheConcurrency(b *testing.B) {
	concurrencyLevels := []int{1, 4, 16, 64, 256}
	ctx := context.Background()

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Goroutines_%d", concurrency), func(b *testing.B) {
			cache := NewShardedToolCache(ShardedCacheConfig{
				ShardCount: 32,
				Capacity:   100000,
				DefaultTTL: 5 * time.Minute,
			})
			defer cache.Close()

			b.ReportAllocs()
			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key_%d", i%10000)
					output := &interfaces.ToolOutput{Result: fmt.Sprintf("value_%d", i)}

					if i%2 == 0 {
						_ = cache.Set(ctx, key, output, 5*time.Minute)
					} else {
						_, _ = cache.Get(ctx, key)
					}
					i++
				}
			})
		})
	}
}

// BenchmarkShardedVsNonSharded 对比分片缓存和非分片缓存在高并发下的性能
func BenchmarkShardedVsNonSharded(b *testing.B) {
	ctx := context.Background()
	goroutines := 64

	b.Run("ShardedCache", func(b *testing.B) {
		cache := NewShardedToolCache(ShardedCacheConfig{
			ShardCount: 32,
			Capacity:   100000,
			DefaultTTL: 5 * time.Minute,
		})
		defer cache.Close()

		b.ReportAllocs()
		b.ResetTimer()

		var wg sync.WaitGroup
		opsPerGoroutine := b.N / goroutines

		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(gid int) {
				defer wg.Done()
				for i := 0; i < opsPerGoroutine; i++ {
					key := fmt.Sprintf("key_%d_%d", gid, i%1000)
					output := &interfaces.ToolOutput{Result: "value"}

					if i%3 == 0 {
						_ = cache.Set(ctx, key, output, 5*time.Minute)
					} else {
						_, _ = cache.Get(ctx, key)
					}
				}
			}(g)
		}
		wg.Wait()
	})

	b.Run("NonShardedCache", func(b *testing.B) {
		cache := NewMemoryToolCache(MemoryCacheConfig{
			Capacity:   100000,
			DefaultTTL: 5 * time.Minute,
		})
		defer cache.Close()

		b.ReportAllocs()
		b.ResetTimer()

		var wg sync.WaitGroup
		opsPerGoroutine := b.N / goroutines

		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(gid int) {
				defer wg.Done()
				for i := 0; i < opsPerGoroutine; i++ {
					key := fmt.Sprintf("key_%d_%d", gid, i%1000)
					output := &interfaces.ToolOutput{Result: "value"}

					if i%3 == 0 {
						_ = cache.Set(ctx, key, output, 5*time.Minute)
					} else {
						_, _ = cache.Get(ctx, key)
					}
				}
			}(g)
		}
		wg.Wait()
	})
}

// BenchmarkMemoryAllocation 对比内存分配
func BenchmarkMemoryAllocation(b *testing.B) {
	ctx := context.Background()

	b.Run("ShardedCache_ZeroAlloc", func(b *testing.B) {
		cache := NewShardedToolCache(ShardedCacheConfig{
			ShardCount: 32,
			Capacity:   10000,
			DefaultTTL: 5 * time.Minute,
		})
		defer cache.Close()

		// Pre-populate
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("key_%d", i)
			_ = cache.Set(ctx, key, &interfaces.ToolOutput{Result: "value"}, 5*time.Minute)
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key_%d", i%1000)
			_, _ = cache.Get(ctx, key)
		}
	})

	b.Run("MemoryCache_StandardAlloc", func(b *testing.B) {
		cache := NewMemoryToolCache(MemoryCacheConfig{
			Capacity:   10000,
			DefaultTTL: 5 * time.Minute,
		})
		defer cache.Close()

		// Pre-populate
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("key_%d", i)
			_ = cache.Set(ctx, key, &interfaces.ToolOutput{Result: "value"}, 5*time.Minute)
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key_%d", i%1000)
			_, _ = cache.Get(ctx, key)
		}
	})
}
