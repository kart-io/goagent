package tools

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// ExampleShardedCacheUsage 展示如何使用分片缓存的选项模式
func ExampleShardedCacheUsage() {
	// 示例1: 使用默认配置
	defaultCache := NewShardedToolCacheWithOptions()
	defer defaultCache.Close()

	// 示例2: 为低延迟场景优化
	lowLatencyCache := NewShardedToolCacheWithOptions(
		WithPerformanceProfile(LowLatencyProfile),
		WithCapacity(50000),
		WithDefaultTTL(10*time.Minute),
	)
	defer lowLatencyCache.Close()

	// 示例3: 为高吞吐量场景优化
	highThroughputCache := NewShardedToolCacheWithOptions(
		WithPerformanceProfile(HighThroughputProfile),
		WithCapacity(100000),
		WithAutoTuning(true),
	)
	defer highThroughputCache.Close()

	// 示例4: 为读密集型工作负载优化
	readHeavyCache := NewShardedToolCacheWithOptions(
		WithWorkloadType(ReadHeavyWorkload),
		WithCapacity(200000),
		WithDefaultTTL(30*time.Minute),
		WithMetrics(true),
	)
	defer readHeavyCache.Close()

	// 示例5: 自定义配置
	customCache := NewShardedToolCacheWithOptions(
		WithShardCount(64),                    // 64个分片
		WithCapacity(50000),                   // 总容量50000
		WithDefaultTTL(15*time.Minute),        // 15分钟TTL
		WithCleanupInterval(2*time.Minute),    // 2分钟清理间隔
		WithEvictionPolicy(LRUEviction),       // LRU淘汰策略
		WithCleanupStrategy(AdaptiveCleanup),  // 自适应清理
		WithLoadBalancing(HashBasedBalancing), // 哈希负载均衡
		WithAutoTuning(true),                  // 启用自动调优
		WithMetrics(true),                     // 启用指标收集
	)
	defer customCache.Close()

	// 示例6: 根据QPS推荐配置
	expectedQPS := 2000
	recommendation := GetShardCountRecommendation(expectedQPS)
	fmt.Printf("For %d QPS, recommended shard count: %d\n",
		recommendation.ExpectedQPS, recommendation.RecommendedCount)
	fmt.Printf("Rationale: %s\n", recommendation.Rationale)

	qpsOptimizedCache := NewShardedToolCacheWithOptions(
		WithShardCount(recommendation.RecommendedCount),
		WithWorkloadType(MixedWorkload),
		WithCapacity(100000),
		WithAutoTuning(true),
	)
	defer qpsOptimizedCache.Close()

	// 示例7: 内存受限环境
	memoryEfficientCache := NewShardedToolCacheWithOptions(
		WithPerformanceProfile(MemoryEfficientProfile),
		WithCapacity(10000),
		WithMemoryLimit(100*1024*1024), // 100MB内存限制
		WithCompressionThreshold(1024), // 压缩大于1KB的条目
		WithMaxEntrySize(10*1024),      // 单个条目最大10KB
	)
	defer memoryEfficientCache.Close()

	// 示例8: 处理突发流量
	burstyCache := NewShardedToolCacheWithOptions(
		WithWorkloadType(BurstyWorkload),
		WithShardCount(0), // 自动检测（CPU核心数*4）
		WithCapacity(50000),
		WithAutoTuning(true),
		WithMaxShardConcurrency(100), // 限制并发
	)
	defer burstyCache.Close()

	// 示例9: 预热缓存
	warmupData := map[string]*ToolOutput{
		"tool:search:query1": {Result: "cached result 1"},
		"tool:search:query2": {Result: "cached result 2"},
	}
	prewarmedCache := NewShardedToolCacheWithOptions(
		WithCapacity(10000),
		WithWarmup(warmupData),
		WithDefaultTTL(1*time.Hour),
	)
	defer prewarmedCache.Close()

	// 示例10: 获取清理间隔推荐
	cleanupRec := GetCleanupIntervalRecommendation(
		100000,        // 缓存大小
		5*time.Minute, // TTL
		0.05,          // 5%每分钟变化率
	)
	fmt.Printf("Recommended cleanup interval: %v\n", cleanupRec.RecommendedInterval)
	fmt.Printf("Rationale: %s\n", cleanupRec.Rationale)
}

// ExampleCacheOperations 展示基本缓存操作
func ExampleCacheOperations() {
	// 创建一个优化的缓存实例
	cache := NewShardedToolCacheWithOptions(
		WithShardCount(32),
		WithCapacity(10000),
		WithDefaultTTL(5*time.Minute),
		WithAutoTuning(true),
	)
	defer cache.Close()

	ctx := context.Background()

	// 存储数据
	output := &ToolOutput{
		Result: "search results",
	}
	err := cache.Set(ctx, "search:golang", output, 10*time.Minute)
	if err != nil {
		fmt.Printf("Failed to set cache: %v\n", err)
	}

	// 获取数据
	cachedOutput, found := cache.Get(ctx, "search:golang")
	if found {
		fmt.Printf("Cache hit: %v\n", cachedOutput.Result)
	} else {
		fmt.Println("Cache miss")
	}

	// 失效特定工具的所有缓存
	invalidatedCount, err := cache.InvalidateByTool(ctx, "search")
	if err != nil {
		fmt.Printf("Failed to invalidate: %v\n", err)
	} else {
		fmt.Printf("Invalidated %d entries\n", invalidatedCount)
	}

	// 使用正则表达式失效缓存
	count, err := cache.InvalidateByPattern(ctx, "search:.*")
	if err != nil {
		fmt.Printf("Failed to invalidate by pattern: %v\n", err)
	} else {
		fmt.Printf("Invalidated %d entries by pattern\n", count)
	}

	// 获取缓存统计
	stats := cache.GetStats()
	fmt.Printf("Cache stats - Hits: %d, Misses: %d, Evicts: %d\n",
		stats.Hits.Load(), stats.Misses.Load(), stats.Evicts.Load())

	// 获取缓存大小
	size := cache.Size()
	fmt.Printf("Current cache size: %d entries\n", size)
}

// ExampleDynamicConfiguration 展示如何根据系统资源动态配置
func ExampleDynamicConfiguration() {
	// 根据系统资源动态决定配置
	cpuCores := runtime.NumCPU()
	var cacheOptions []ShardedCacheOption

	// 根据CPU核心数调整
	if cpuCores <= 4 {
		// 小型系统
		cacheOptions = append(cacheOptions,
			WithShardCount(16),
			WithCapacity(5000),
			WithPerformanceProfile(MemoryEfficientProfile),
		)
	} else if cpuCores <= 8 {
		// 中型系统
		cacheOptions = append(cacheOptions,
			WithShardCount(32),
			WithCapacity(20000),
			WithPerformanceProfile(BalancedProfile),
		)
	} else {
		// 大型系统
		cacheOptions = append(cacheOptions,
			WithShardCount(0), // 自动检测
			WithCapacity(100000),
			WithPerformanceProfile(HighThroughputProfile),
			WithAutoTuning(true),
		)
	}

	// 添加通用选项
	cacheOptions = append(cacheOptions,
		WithDefaultTTL(10*time.Minute),
		WithMetrics(true),
	)

	cache := NewShardedToolCacheWithOptions(cacheOptions...)
	defer cache.Close()

	fmt.Printf("Cache configured for %d CPU cores with %d shards\n",
		cpuCores, cache.shardCount)
}

// ExampleMonitoringAndTuning 展示监控和调优
func ExampleMonitoringAndTuning() {
	// 创建启用监控和自动调优的缓存
	cache := NewShardedToolCacheWithOptions(
		WithCapacity(50000),
		WithAutoTuning(true),
		WithMetrics(true),
		WithWorkloadType(MixedWorkload),
	)
	defer cache.Close()

	ctx := context.Background()

	// 模拟工作负载
	go func() {
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("key:%d", i%100)
			output := &ToolOutput{Result: fmt.Sprintf("result %d", i)}
			_ = cache.Set(ctx, key, output, 5*time.Minute)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// 定期检查统计信息
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for i := 0; i < 3; i++ {
		<-ticker.C
		stats := cache.GetStats()
		size := cache.Size()

		hitRate := float64(0)
		totalRequests := stats.Hits.Load() + stats.Misses.Load()
		if totalRequests > 0 {
			hitRate = float64(stats.Hits.Load()) / float64(totalRequests) * 100
		}

		fmt.Printf("Stats at %d seconds:\n", (i+1)*5)
		fmt.Printf("  Size: %d entries\n", size)
		fmt.Printf("  Hit Rate: %.2f%%\n", hitRate)
		fmt.Printf("  Evictions: %d\n", stats.Evicts.Load())
		fmt.Printf("  Invalidations: %d\n", stats.Invalidations.Load())
	}
}
