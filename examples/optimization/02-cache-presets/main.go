package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

func main() {
	// 1. High Performance Cache
	hpCache := tools.NewHighPerformanceCache()
	defer hpCache.Close()

	fmt.Println("High Performance Cache created")
	printStats(hpCache)

	// 2. Low Memory Cache
	lmCache := tools.NewLowMemoryCache()
	defer lmCache.Close()

	fmt.Println("Low Memory Cache created")
	printStats(lmCache)

	// Simulate usage
	ctx := context.Background() // Need to import context
	if err := hpCache.Set(ctx, "key1", &interfaces.ToolOutput{Result: "value1"}, time.Minute); err != nil {
		fmt.Printf("Error setting cache: %v\n", err)
	}
	val, _ := hpCache.Get(ctx, "key1")
	fmt.Printf("HP Cache Get: %v\n", val.Result)
}

func printStats(c *tools.ShardedToolCache) {
	hits, misses, evicts, invalidations := c.GetStatsValues()
	fmt.Printf("Stats: hits=%d misses=%d evicts=%d invalidations=%d\n", hits, misses, evicts, invalidations)
}
